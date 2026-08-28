// Package admin implements the Admin/Contact domain (spec section 12): who decided
// what about an offer, and which seller may reach which customer as a result.
package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/clients"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/events"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/store"
)

var (
	ErrOfferNotFound   = errors.New("offer not found")
	ErrOfferNotPending = errors.New("offer is no longer pending")

	// Both are about the request an offer points at, not the offer itself. They exist
	// because reporting either as ErrOfferNotFound told an admin the offer in front of
	// them did not exist, which is the one thing they could see was untrue.
	ErrRequestGone     = errors.New("the request behind this offer no longer exists")
	ErrRequestNoBuyers = errors.New("the request behind this offer has no buyers left")
	ErrAlreadyDecided  = errors.New("offer has already been decided")
	ErrAccessNotFound  = errors.New("contact access not found")
	ErrAlreadyRevoked  = errors.New("contact access is not currently granted")
	ErrNoContactAccess = errors.New("no contact access has been granted for this request")
)

const (
	DecisionApproved = "APPROVED"
	DecisionRejected = "REJECTED"

	statusPending = "PENDING"
	statusGranted = "GRANTED"

	// What request-service is told once an offer against it has been approved.

	uniqueViolation = "23505"
)

// offerClient, requestClient and the identity clients are declared as interfaces so
// the tests can drive the domain without five HTTP stubs standing behind it.
type offerClient interface {
	Get(ctx context.Context, offerID uuid.UUID) (clients.Offer, error)
	ListPending(ctx context.Context, limit, offset int) ([]clients.Offer, error)
	SetStatus(ctx context.Context, offerID uuid.UUID, status string) (clients.Offer, error)
}

type requestClient interface {
	ParticipantCustomerIDs(ctx context.Context, requestID uuid.UUID) ([]uuid.UUID, error)
}

type sellerResolver interface {
	ResolveSellerID(ctx context.Context, userID uuid.UUID) (uuid.UUID, error)
	ResolveUserID(ctx context.Context, sellerID uuid.UUID) (uuid.UUID, error)
}

type customerResolver interface {
	ResolveUserID(ctx context.Context, customerID uuid.UUID) (uuid.UUID, error)
	Resolve(ctx context.Context, customerID uuid.UUID) (clients.Customer, error)
}

type phoneReader interface {
	Phone(ctx context.Context, userID uuid.UUID) (string, error)
}

type Deps struct {
	Offers    offerClient
	Requests  requestClient
	Sellers   sellerResolver
	Customers customerResolver
	Auth      phoneReader
}

// outboxWaker lets the service nudge the relay the moment an event is committed, so a
// notification is not held for the poll interval when the broker is healthy.
type outboxWaker interface{ Wake() }

type Service struct {
	pool    *pgxpool.Pool
	queries *store.Queries
	deps    Deps
	relay   outboxWaker
}

func NewService(pool *pgxpool.Pool, deps Deps, relay outboxWaker) *Service {
	return &Service{pool: pool, queries: store.New(pool), deps: deps, relay: relay}
}

func (s *Service) wakeRelay() {
	if s.relay != nil {
		s.relay.Wake()
	}
}

type DecideInput struct {
	OfferID     uuid.UUID
	AdminUserID uuid.UUID
	Decision    string
	Reason      string
}

// DecideResult carries the audit record, the number of grants it produced, and the
// offer's seller and request - which the handler needs to address the notification
// events that follow (spec flow 3, step 7).
type DecideResult struct {
	Decision        store.OfferDecision
	ContactsGranted int
	SellerID        uuid.UUID
	RequestID       uuid.UUID
}

// Decide records an admin's verdict on an offer and, when it is an approval, grants
// contact access to every customer behind the request (flow 3).
//
// R7 lives here: this is the only place in the platform that decides an offer, and it
// runs behind an ADMIN-only route. Offer-service holds the resulting status but not
// the authority to set it.
//
// R8 is why an approval writes rows rather than flipping a flag: approving does not
// expose phone numbers by itself, it creates one ContactAccess per customer, and that
// is what a later contact lookup checks.
//
// Ordering is deliberate. The offer is read and the participants fetched before
// anything is written, so a dependency being down fails the call cleanly with nothing
// recorded. The local rows are then written and the remote status flipped inside one
// transaction, committing last: if offer-service rejects the change the whole thing
// rolls back, and we never leave an approved offer with no grants behind it. The one
// window that remains is a commit failing after offer-service accepted the PATCH,
// which needs an outbox to close properly and is noted in the README.
func (s *Service) Decide(ctx context.Context, in DecideInput) (DecideResult, error) {
	offer, err := s.deps.Offers.Get(ctx, in.OfferID)
	switch {
	case errors.Is(err, clients.ErrNotFound):
		return DecideResult{}, ErrOfferNotFound
	case err != nil:
		return DecideResult{}, err
	}

	if offer.Status != statusPending {
		return DecideResult{}, fmt.Errorf("%w: status is %s", ErrOfferNotPending, offer.Status)
	}

	// Resolved before the transaction opens, because it is a network call and holding a
	// row lock across one is how a slow dependency becomes a database outage.
	//
	// The offer holds a sellerId, but notification-service is addressed by global userId
	// and never resolves an identity, so the hop is made here. If it fails the decision
	// still stands and only the notification is skipped - a seller-service blip must not
	// be able to block an admin from deciding an offer.
	sellerUserID, err := s.deps.Sellers.ResolveUserID(ctx, offer.SellerID)
	if err != nil {
		slog.WarnContext(ctx, "cannot address decision notification; deciding without it",
			"sellerId", offer.SellerID, "error", err)
		sellerUserID = uuid.Nil
	}

	// Only an approval needs the customers behind the request; a rejection grants
	// nothing, so it never reads participant identity at all.
	var customerIDs []uuid.UUID
	if in.Decision == DecisionApproved {
		customerIDs, err = s.deps.Requests.ParticipantCustomerIDs(ctx, offer.RequestID)
		if errors.Is(err, clients.ErrNotFound) {
			return DecideResult{}, ErrRequestGone
		}
		if err != nil {
			return DecideResult{}, err
		}

		// An approval exists to release the buyers' numbers, so approving a request
		// nobody is left in would record a decision, grant nothing, and still tell the
		// seller their access was granted. Every participant can leave - the creator is
		// added as one and has no exemption - so this is reachable, not defensive.
		if len(customerIDs) == 0 {
			return DecideResult{}, ErrRequestNoBuyers
		}
	}

	var result DecideResult

	err = s.inTx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		decision, err := q.RecordDecision(ctx, store.RecordDecisionParams{
			OfferID:     in.OfferID,
			AdminUserID: in.AdminUserID,
			Decision:    in.Decision,
			Reason:      in.Reason,
		})
		// The unique constraint on offer_id, not a prior read, is what makes a double
		// decision impossible: two concurrent approvals would both pass a check-then-act.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrAlreadyDecided
		}
		if err != nil {
			return fmt.Errorf("recording decision: %w", err)
		}

		for _, customerID := range customerIDs {
			if _, err := q.GrantContactAccess(ctx, store.GrantContactAccessParams{
				SellerID:   offer.SellerID,
				CustomerID: customerID,
				RequestID:  offer.RequestID,
				OfferID:    offer.OfferID,
				GrantedBy:  in.AdminUserID,
				ExpiresAt:  pgtype.Timestamptz{},
			}); err != nil {
				return fmt.Errorf("granting contact access: %w", err)
			}
		}

		// Last thing before the commit: if offer-service refuses, everything above
		// rolls back rather than leaving a decision recorded against a PENDING offer.
		if _, err := s.deps.Offers.SetStatus(ctx, in.OfferID, in.Decision); err != nil {
			if errors.Is(err, clients.ErrConflict) {
				return ErrAlreadyDecided
			}
			if errors.Is(err, clients.ErrNotFound) {
				return ErrOfferNotFound
			}
			return err
		}

		// Flow 3, step 7 - written in the same transaction as the decision it reports,
		// so a broker outage delays the seller hearing about it rather than losing it.
		if sellerUserID != uuid.Nil {
			if err := enqueueDecisionEvents(ctx, tx, sellerUserID, in, len(customerIDs)); err != nil {
				return err
			}
		}

		result = DecideResult{
			Decision:        decision,
			ContactsGranted: len(customerIDs),
			SellerID:        offer.SellerID,
			RequestID:       offer.RequestID,
		}
		return nil
	})

	s.wakeRelay()
	if err != nil {
		return result, err
	}

	// An approval used to mark the request OFFER_APPROVED, which stopped further offers
	// and ended it. It no longer touches the request at all: this service decides which
	// seller may reach the buyers, and whether those buyers still want the item is not
	// the same question. Several offers on one request can each be approved, and each
	// approval grants its seller the contacts of whoever is on the request then.
	return result, nil
}

// enqueueDecisionEvents writes the outcome and, when the approval granted contact
// permission, the fact that the seller may now reach the customers behind the request.
//
// Two events rather than one, because they are two facts: R8 is precisely that
// approving an offer and granting contact access are not the same thing.
func enqueueDecisionEvents(
	ctx context.Context, tx pgx.Tx, sellerUserID uuid.UUID, in DecideInput, contactsGranted int,
) error {
	approved := in.Decision == DecisionApproved

	key, notificationType := events.KeyOfferRejected, "OFFER_REJECTED"
	title, message := "Your offer was not approved", "An admin reviewed your offer and did not approve it."
	if approved {
		key, notificationType = events.KeyOfferApproved, "OFFER_APPROVED"
		title, message = "Your offer was approved", "An admin approved your offer."
	}
	if in.Reason != "" {
		message += " Reason: " + in.Reason
	}

	if err := events.Enqueue(ctx, tx, key, events.Notification{
		UserID:  sellerUserID,
		Type:    notificationType,
		Title:   title,
		Message: message,
	}); err != nil {
		return err
	}

	if !approved || contactsGranted == 0 {
		return nil
	}
	return events.Enqueue(ctx, tx, events.KeyContactAccessGranted, events.Notification{
		UserID: sellerUserID,
		Type:   "CONTACT_ACCESS_GRANTED",
		Title:  "You can now contact the customers",
		Message: fmt.Sprintf(
			"You have been granted contact access to %d customer(s) on this request.",
			contactsGranted),
	})
}

func (s *Service) ListPendingOffers(ctx context.Context, limit, offset int) ([]clients.Offer, error) {
	return s.deps.Offers.ListPending(ctx, limit, offset)
}

type AccessFilter struct {
	SellerID  uuid.UUID
	RequestID uuid.UUID
	Status    string
	Limit     int32
	Offset    int32
}

func (s *Service) ListContactAccess(ctx context.Context, f AccessFilter) ([]store.ContactAccess, error) {
	return s.queries.ListContactAccess(ctx, store.ListContactAccessParams{
		SellerID:     optionalUUID(f.SellerID),
		RequestID:    optionalUUID(f.RequestID),
		Status:       optionalText(f.Status),
		ResultLimit:  f.Limit,
		ResultOffset: f.Offset,
	})
}

// Revoke withdraws a grant. It is a status change rather than a delete: the row is
// part of the audit history of who was allowed to reach whom, and removing it would
// erase the very record this service exists to keep.
func (s *Service) Revoke(ctx context.Context, accessID uuid.UUID) (store.ContactAccess, error) {
	revoked, err := s.queries.RevokeContactAccess(ctx, accessID)
	if err == nil {
		return revoked, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.ContactAccess{}, err
	}

	// The update matched nothing: either no such grant, or one that is already
	// revoked or expired. Distinguish them so the caller gets 404 or 409, not both.
	existing, getErr := s.queries.GetContactAccess(ctx, accessID)
	if errors.Is(getErr, pgx.ErrNoRows) {
		return store.ContactAccess{}, ErrAccessNotFound
	}
	if getErr != nil {
		return store.ContactAccess{}, getErr
	}
	return store.ContactAccess{}, fmt.Errorf("%w: status is %s", ErrAlreadyRevoked, existing.Status)
}

// ContactsForRequest answers a seller's contact lookup (flow 4).
//
// R9 is the whole of this method: the grant table is consulted first, and a phone
// number is fetched only for a customer who appears in it with status GRANTED. A
// seller with no grant never reaches auth-service at all.
func (s *Service) ContactsForRequest(ctx context.Context, userID, requestID uuid.UUID) ([]contact, error) {
	sellerID, err := s.deps.Sellers.ResolveSellerID(ctx, userID)
	if err != nil {
		return nil, err
	}

	grants, err := s.queries.ListGrantedForSellerRequest(ctx, store.ListGrantedForSellerRequestParams{
		SellerID:  sellerID,
		RequestID: requestID,
	})
	if err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		return nil, ErrNoContactAccess
	}

	contacts := make([]contact, 0, len(grants))
	for _, grant := range grants {
		// customerId -> userId -> phone. Three hops, none of them skippable: this
		// service knows customerIds, and only auth-service can turn a userId into a
		// number. The first hop already returns the name, so it is carried through
		// rather than fetched again.
		customer, err := s.deps.Customers.Resolve(ctx, grant.CustomerID)
		if err != nil {
			return nil, fmt.Errorf("resolving customer %s: %w", grant.CustomerID, err)
		}
		phone, err := s.deps.Auth.Phone(ctx, customer.UserID)
		if err != nil {
			return nil, fmt.Errorf("reading phone for customer %s: %w", grant.CustomerID, err)
		}
		contacts = append(contacts, contact{
			CustomerID:  grant.CustomerID,
			FirstName:   customer.FirstName,
			LastName:    customer.LastName,
			PhoneNumber: phone,
		})
	}
	return contacts, nil
}

// HasContactAccess backs the internal permission check. requestID may be uuid.Nil to
// ask the broader question: may this seller reach this customer about anything?
func (s *Service) HasContactAccess(ctx context.Context, sellerID, customerID, requestID uuid.UUID) (bool, error) {
	count, err := s.queries.CountEffectiveAccess(ctx, store.CountEffectiveAccessParams{
		SellerID:   sellerID,
		CustomerID: customerID,
		RequestID:  optionalUUID(requestID),
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// inTx hands the callback both the generated queries and the raw transaction: the
// domain writes go through sqlc, and the outbox insert needs the tx itself, but they
// have to be the same transaction for the outbox to mean anything.
func (s *Service) inTx(ctx context.Context, fn func(*store.Queries, pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	// No-op once the transaction commits; guarantees rollback on every early return.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(s.queries.WithTx(tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// optionalUUID and optionalText map a zero filter value to SQL NULL, which the queries
// read as "no filter" rather than "match the zero value".
func optionalUUID(v uuid.UUID) pgtype.UUID {
	if v == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: v, Valid: true}
}

func optionalText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}
