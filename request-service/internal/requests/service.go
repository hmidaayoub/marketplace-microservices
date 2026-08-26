// Package requests implements the purchase-request domain (spec section 10).
package requests

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

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/events"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/store"
)

var (
	ErrRequestNotFound    = errors.New("request not found")
	ErrNotRequestOwner    = errors.New("caller did not create this request")
	ErrRequestNotClosable = errors.New("request can no longer be closed")
	ErrInvalidStatus      = errors.New("status is not one this API may set")
	ErrNotParticipant     = errors.New("caller is not a participant in this request")
	ErrAlreadyParticipant = errors.New("caller has already joined this request")
	ErrRequestNotOpen     = errors.New("request is not open")
)

// StatusOpen is the only status in which participants may be added, changed or
// removed. Once an offer is in flight the demand an offer was made against must stop
// moving, so every participant mutation is gated on it.
const (
	StatusOpen          = "OPEN"
	StatusOfferPending  = "OFFER_PENDING"
	StatusOfferApproved = "OFFER_APPROVED"
	StatusClosed        = "CLOSED"
	StatusCancelled     = "CANCELLED"
)

// A request can still be closed once offers are in flight - the demand is the owner's
// to withdraw - but a decided or already-terminal request has nothing left to close.
var closableStatuses = map[string]bool{StatusOpen: true, StatusOfferPending: true}

// What Admin/Contact may set through the internal API. It decides offers, not demand,
// so it can move a request forward but cannot reopen one.
var internalSettableStatuses = map[string]bool{
	StatusOfferPending:  true,
	StatusOfferApproved: true,
	StatusClosed:        true,
	StatusCancelled:     true,
}

const uniqueViolation = "23505"

// outboxWaker lets the service nudge the relay the moment an event is committed, so a
// notification is not held for the poll interval when the broker is healthy.
type outboxWaker interface{ Wake() }

type Service struct {
	pool    *pgxpool.Pool
	queries *store.Queries
	relay   outboxWaker
}

func NewService(pool *pgxpool.Pool, relay outboxWaker) *Service {
	return &Service{pool: pool, queries: store.New(pool), relay: relay}
}

func (s *Service) wakeRelay() {
	if s.relay != nil {
		s.relay.Wake()
	}
}

type CreateInput struct {
	ItemName    string
	Description string
	Category    string
	Quantity    int32

	// The token subject of the customer acting. Notification-service is addressed by
	// global userId and never resolves an identity, so the producer supplies the one it
	// already holds - and it has to reach this far in, because the event is written
	// inside the same transaction as the request itself.
	ActorUserID uuid.UUID
}

type ListFilter struct {
	ItemName string
	Category string
	Status   string
	Limit    int32
	Offset   int32
}

// Create opens a request and enrolls its creator as the first participant, in one
// transaction. R1 and R3 travel together: a customer creates a request because they
// want some quantity of the item, so a request never exists with nobody wanting it.
func (s *Service) Create(ctx context.Context, customerID uuid.UUID, in CreateInput) (store.PurchaseRequest, error) {
	var created store.PurchaseRequest

	err := s.inTx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		request, err := q.CreateRequest(ctx, store.CreateRequestParams{
			ItemName:    in.ItemName,
			Description: in.Description,
			Category:    in.Category,
			// R1: the request has an owner from the moment it exists, and that is who
			// may later close it.
			CreatedBy: pgtype.UUID{Bytes: customerID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}

		if _, err := q.AddParticipant(ctx, store.AddParticipantParams{
			RequestID:  request.RequestID,
			CustomerID: customerID,
			Quantity:   in.Quantity,
		}); err != nil {
			return fmt.Errorf("adding creator as participant: %w", err)
		}

		created, err = q.RecalculateDemand(ctx, request.RequestID)
		if err != nil {
			return fmt.Errorf("recalculating demand: %w", err)
		}

		// Flow 1, step 8 - in the same transaction, so the request and the promise to
		// tell someone about it either both exist or neither does.
		return events.Enqueue(ctx, tx, events.KeyRequestJoined, events.Notification{
			UserID: in.ActorUserID,
			Type:   "REQUEST_JOINED",
			Title:  "Your request is open",
			Message: fmt.Sprintf(
				"Your request for %s is open. You are its first participant, wanting %d.",
				created.ItemName, in.Quantity),
		})
	})

	s.wakeRelay()
	return created, err
}

func (s *Service) Get(ctx context.Context, requestID uuid.UUID) (store.PurchaseRequest, error) {
	request, err := s.queries.GetRequest(ctx, requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.PurchaseRequest{}, ErrRequestNotFound
	}
	return request, err
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]store.PurchaseRequest, error) {
	return s.queries.ListRequests(ctx, store.ListRequestsParams{
		ItemName:     optionalText(f.ItemName),
		Category:     optionalText(f.Category),
		Status:       optionalText(f.Status),
		ResultLimit:  f.Limit,
		ResultOffset: f.Offset,
	})
}

func (s *Service) ListForCustomer(ctx context.Context, customerID uuid.UUID) ([]store.PurchaseRequest, error) {
	return s.queries.ListRequestsByCustomer(ctx, customerID)
}

func (s *Service) ParticipantCustomerIDs(ctx context.Context, requestID uuid.UUID) ([]uuid.UUID, error) {
	if _, err := s.Get(ctx, requestID); err != nil {
		return nil, err
	}
	return s.queries.ListParticipantCustomerIDs(ctx, requestID)
}

// Join adds a customer to an open request (R2, R3).
func (s *Service) Join(
	ctx context.Context, requestID, customerID, actorUserID uuid.UUID, quantity int32,
) (store.PurchaseRequest, error) {
	joined, err := s.mutateParticipants(ctx, requestID,
		func(q *store.Queries, tx pgx.Tx) error {
			_, err := q.AddParticipant(ctx, store.AddParticipantParams{
				RequestID:  requestID,
				CustomerID: customerID,
				Quantity:   quantity,
			})

			// The unique constraint, not a prior read, is what makes a double join
			// impossible: two concurrent requests would both pass a read-then-write check.
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
				return ErrAlreadyParticipant
			}
			return err
		},
		// Enqueued inside the same transaction, after the totals have been recomputed,
		// so the joiner is told the demand they actually became part of.
		func(updated store.PurchaseRequest) []events.Notification {
			return []events.Notification{{
				UserID: actorUserID,
				Type:   "REQUEST_JOINED",
				Title:  "You joined a request",
				Message: fmt.Sprintf(
					"You joined the request for %s, wanting %d. It now has %d customers wanting %d in total.",
					updated.ItemName, quantity, updated.TotalCustomers, updated.TotalQuantity),
			}}
		})

	s.wakeRelay()
	return joined, err
}

// UpdateQuantity changes the caller's own quantity. The caller is identified by the
// token, so one customer can never edit another's line.
func (s *Service) UpdateQuantity(ctx context.Context, requestID, customerID uuid.UUID, quantity int32) (store.PurchaseRequest, error) {
	return s.mutateParticipants(ctx, requestID, func(q *store.Queries, tx pgx.Tx) error {
		_, err := q.UpdateParticipantQuantity(ctx, store.UpdateParticipantQuantityParams{
			RequestID:  requestID,
			CustomerID: customerID,
			Quantity:   quantity,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotParticipant
		}
		return err
	}, nil)
}

func (s *Service) Leave(ctx context.Context, requestID, customerID uuid.UUID) (store.PurchaseRequest, error) {
	return s.mutateParticipants(ctx, requestID, func(q *store.Queries, tx pgx.Tx) error {
		affected, err := q.DeleteParticipant(ctx, store.DeleteParticipantParams{
			RequestID:  requestID,
			CustomerID: customerID,
		})
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrNotParticipant
		}
		return nil
	}, nil)
}

// mutateParticipants runs a participant change and the demand recalculation it implies
// as one unit. The request row is locked first so concurrent joins on the same request
// serialize: without it two transactions could each recompute totals from a snapshot
// that misses the other's insert (R4).
func (s *Service) mutateParticipants(
	ctx context.Context,
	requestID uuid.UUID,
	mutate func(*store.Queries, pgx.Tx) error,
	notify func(store.PurchaseRequest) []events.Notification,
) (store.PurchaseRequest, error) {
	var updated store.PurchaseRequest

	err := s.inTx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		request, err := q.LockRequest(ctx, requestID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRequestNotFound
		}
		if err != nil {
			return fmt.Errorf("locking request: %w", err)
		}
		if request.Status != StatusOpen {
			return fmt.Errorf("%w: status is %s", ErrRequestNotOpen, request.Status)
		}

		if err := mutate(q, tx); err != nil {
			return err
		}

		updated, err = q.RecalculateDemand(ctx, requestID)
		if err != nil {
			return fmt.Errorf("recalculating demand: %w", err)
		}

		if notify == nil {
			return nil
		}
		return events.Enqueue(ctx, tx, events.KeyRequestJoined, notify(updated)...)
	})

	return updated, err
}

// inTx hands the callback both the generated queries and the raw transaction: the
// domain writes go through sqlc, and the outbox insert needs the tx itself, but they
// have to be the same transaction for the outbox to mean anything.
// CloseInput carries the recipients of REQUEST_CLOSED alongside the caller.
type CloseInput struct {
	RequestID  uuid.UUID
	CustomerID uuid.UUID

	// customerId -> userId for every participant, resolved by the caller before the
	// transaction opens. Resolution is a network call per participant and holding a row
	// lock across those is how a slow dependency becomes a database outage.
	Recipients map[uuid.UUID]uuid.UUID
	ItemName   string
}

// Close ends a request and tells everyone who wanted the item (spec section 18).
//
// Only the customer who created it may close it: the other participants joined someone
// else's demand and withdrawing it is not theirs to do. The notification goes to all of
// them, which is the one fan-out in the platform - and it is written to the outbox in
// the same transaction as the status change, so nobody is told about a close that
// rolled back and nobody is left uninformed about one that did not.
func (s *Service) Close(ctx context.Context, in CloseInput) (store.PurchaseRequest, error) {
	var closed store.PurchaseRequest

	err := s.inTx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		request, err := q.LockRequest(ctx, in.RequestID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRequestNotFound
		}
		if err != nil {
			return fmt.Errorf("locking request: %w", err)
		}

		// An unknown owner - a pre-migration request whose participants all left - can
		// be closed by nobody, which is the honest answer rather than by anybody.
		if !request.CreatedBy.Valid || uuid.UUID(request.CreatedBy.Bytes) != in.CustomerID {
			return ErrNotRequestOwner
		}
		if !closableStatuses[request.Status] {
			return fmt.Errorf("%w: status is %s", ErrRequestNotClosable, request.Status)
		}

		closed, err = q.SetRequestStatus(ctx, store.SetRequestStatusParams{
			RequestID: in.RequestID,
			Status:    StatusClosed,
		})
		if err != nil {
			return fmt.Errorf("closing request: %w", err)
		}

		// Read inside the transaction, behind the same lock joins take, so the set
		// cannot change under us. A participant we could not resolve a userId for is
		// skipped rather than blocking the close.
		customerIDs, err := q.ListParticipantCustomerIDs(ctx, in.RequestID)
		if err != nil {
			return fmt.Errorf("reading participants: %w", err)
		}

		notifications := make([]events.Notification, 0, len(customerIDs))
		for _, customerID := range customerIDs {
			userID, ok := in.Recipients[customerID]
			if !ok {
				slog.WarnContext(ctx, "no userId for participant; not notifying them",
					"requestId", in.RequestID, "customerId", customerID)
				continue
			}
			notifications = append(notifications, events.Notification{
				UserID: userID,
				Type:   "REQUEST_CLOSED",
				Title:  "A request you joined was closed",
				Message: fmt.Sprintf(
					"The request for %s has been closed by the customer who created it.",
					closed.ItemName),
			})
		}

		return events.Enqueue(ctx, tx, events.KeyRequestClosed, notifications...)
	})

	s.wakeRelay()
	return closed, err
}

// SetStatus backs the internal status API, which is how Admin/Contact moves a request
// to OFFER_APPROVED once it has approved an offer against it. No notification: the
// seller is told by Admin/Contact, and the customers have not lost anything yet.
func (s *Service) SetStatus(ctx context.Context, requestID uuid.UUID, status string) (store.PurchaseRequest, error) {
	if !internalSettableStatuses[status] {
		return store.PurchaseRequest{}, fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}

	var updated store.PurchaseRequest
	err := s.inTx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		if _, err := q.LockRequest(ctx, requestID); errors.Is(err, pgx.ErrNoRows) {
			return ErrRequestNotFound
		} else if err != nil {
			return fmt.Errorf("locking request: %w", err)
		}

		var err error
		updated, err = q.SetRequestStatus(ctx, store.SetRequestStatusParams{
			RequestID: requestID,
			Status:    status,
		})
		return err
	})
	return updated, err
}

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

// optionalText maps an empty filter value to SQL NULL, which the queries read as
// "no filter" rather than "match the empty string".
func optionalText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}
