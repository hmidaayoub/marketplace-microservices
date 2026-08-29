// Package requests implements the purchase-request domain (spec section 10).
package requests

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/events"
	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/store"
)

// RequestExistsError refuses a create because the item already has an open request, and
// carries that request so the caller has somewhere to go. It is a value rather than a
// sentinel because the request is the point: "this already exists" without saying what
// leaves the customer with nothing to do about it.
//
// This is the only thing that stops a create. A merely similar name does not: whether a
// close spelling is the same product is a judgement about products, and the service has
// only the spelling to go on, so those are put to the customer as suggestions and
// decided by them. The same name is not a judgement - it is the state the platform
// exists to prevent, and there is nothing for a second request to add.
//
// The request it carries may have no buyers on it - one a seller opened by offering
// against the item, or one everybody has left. That is still the request to join rather
// than one to duplicate: a join is what makes it demand again, and it may already have
// offers waiting on it.
type RequestExistsError struct {
	Existing store.PurchaseRequest
}

func (e *RequestExistsError) Error() string {
	return "an open request already carries this item name"
}

var (
	ErrRequestNotFound    = errors.New("request not found")
	ErrNotParticipant     = errors.New("caller is not a participant in this request")
	ErrAlreadyParticipant = errors.New("caller has already joined this request")
)

// A request describes demand, and demand does not end - it only stops having anyone
// behind it. So there are two statuses and neither is terminal: OPEN while at least one
// customer wants the item, INACTIVE once the last one leaves, and a join makes it OPEN
// again. Both are written by RecalculateDemand from the participant count, which is why
// nothing here sets a status and no API accepts one.
const (
	StatusOpen     = "OPEN"
	StatusInactive = "INACTIVE"
)

// How close two item names have to be before the platform offers one as a match.
//
// Suggestions only: nothing here refuses a create or joins a request. A customer who
// means to open their own request opens it, and being shown demand they did not want
// costs them a glance - so this floor sits low, where it catches a typo or an extra
// word without needing to be right about it.
const (
	SuggestSimilarity float32 = 0.3

	// Enough to recognise the item among; past that it is a list nobody reads.
	maxSimilar int32 = 5
)

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

// EnsureInput describes an item to find or open a request for. There is no quantity:
// nobody is being enrolled, which is the whole difference from CreateInput.
type EnsureInput struct {
	ItemName    string
	Description string
	Category    string
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
//
// Except when that item already has a request. Two requests for "Espresso Machine"
// split the number a seller bids against, which is the number the whole platform exists
// to build, so there is nothing for the second one to create - it is refused and handed
// the request that exists. Joining it is then the customer's own call, made through the
// participants endpoint: being quietly enrolled in a stranger's request because a name
// collided is not what anybody asked for.
//
// That holds whether or not anyone is on it. A request with no buyers is the one a join
// helps most - it is how the item becomes demand again - and it may already carry a
// seller's offer waiting for buyers to arrive.
//
// A merely similar name is not refused. Whether "Espresso Machine Pro" is the same
// product is a judgement this has only the spelling to make, so those reach the customer
// as suggestions while they type - see Similar - and they decide.
func (s *Service) Create(
	ctx context.Context, customerID uuid.UUID, in CreateInput,
) (store.PurchaseRequest, error) {
	var created store.PurchaseRequest

	err := s.inTx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		// Before looking, so the answer cannot go stale between the look and the write:
		// two customers naming the same new item at once would otherwise both find
		// nothing and both create it.
		if err := q.LockItemName(ctx, in.ItemName); err != nil {
			return fmt.Errorf("locking item name: %w", err)
		}

		existing, err := q.FindRequestByItemName(ctx, in.ItemName)
		switch {
		case err == nil:
			return s.refuseExisting(ctx, q, existing, customerID)
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("looking for an open request for the same item: %w", err)
		}

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

// EnsureForItem returns the request an item is carried by, opening one with nobody on it
// if there is none. It reports whether it had to create it.
//
// This is what lets a seller offer against an item nobody has asked for yet. Demand and
// supply do not have to arrive in that order: a seller who already holds the stock is
// worth letting speak first, and the request their offer needs is the thing that gives
// buyers somewhere to arrive. Such a request is INACTIVE, which is not a special case -
// INACTIVE is exactly what "no participants" is called, the same state a request reaches
// when its last customer leaves, and the same join revives either.
//
// It creates nothing when the item already has a request, for the same reason Create
// refuses one: two requests for the same item split the total the platform exists to
// pool. The seller's offer joins whatever demand is already there instead.
//
// No owner and no participants, so:
//   - created_by stays NULL. A seller is not a customer, cannot join, and must not be
//     recorded as the buyer who wanted this.
//   - no REQUEST_JOINED event. Nobody joined, and the seller learns the outcome in the
//     response to the offer they were making.
func (s *Service) EnsureForItem(
	ctx context.Context, in EnsureInput,
) (store.PurchaseRequest, bool, error) {
	var (
		request store.PurchaseRequest
		created bool
	)

	err := s.inTx(ctx, func(q *store.Queries, tx pgx.Tx) error {
		// The same lock Create takes, and it has to be the same one: a customer naming
		// an item at the instant a seller offers against it would otherwise have both
		// find nothing and both create a request for it.
		if err := q.LockItemName(ctx, in.ItemName); err != nil {
			return fmt.Errorf("locking item name: %w", err)
		}

		existing, err := q.FindRequestByItemName(ctx, in.ItemName)
		switch {
		case err == nil:
			request = existing
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("looking for a request for the same item: %w", err)
		}

		opened, err := q.CreateRequest(ctx, store.CreateRequestParams{
			ItemName:    in.ItemName,
			Description: in.Description,
			Category:    in.Category,
			// Left NULL deliberately - see the note above, and 000003, which made the
			// column nullable for exactly the case of a request with no buyer behind it.
			CreatedBy: pgtype.UUID{},
		})
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}

		// Not skipped just because the answer is known: the status is derived from the
		// participant count by one query, and letting this path write 'INACTIVE' itself
		// would be a second place that decides it.
		request, err = q.RecalculateDemand(ctx, opened.RequestID)
		if err != nil {
			return fmt.Errorf("recalculating demand: %w", err)
		}
		created = true
		return nil
	})

	return request, created, err
}

// refuseExisting turns a name collision into the right refusal. A caller who is already
// in that request is told so plainly - "join this instead" is no use to somebody who
// already has - and everyone else is handed it to join.
func (s *Service) refuseExisting(
	ctx context.Context, q *store.Queries, existing store.PurchaseRequest, customerID uuid.UUID,
) error {
	_, err := q.GetParticipant(ctx, store.GetParticipantParams{
		RequestID:  existing.RequestID,
		CustomerID: customerID,
	})
	switch {
	case err == nil:
		return ErrAlreadyParticipant
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("checking existing participation: %w", err)
	}
	return &RequestExistsError{Existing: existing}
}

// Similar backs the suggestions a customer sees while typing an item name. It is a
// plain read - the same projection browsing already serves - so it needs no token and
// takes no lock.
//
// Requests with nobody on them are included, and each row carries its status. One of
// those is often the most useful thing to suggest: an item a seller has already offered
// against is waiting for its first buyer, and joining is what makes it demand.
func (s *Service) Similar(ctx context.Context, itemName string, minScore float32) ([]store.FindSimilarRequestsRow, error) {
	return s.queries.FindSimilarRequests(ctx, store.FindSimilarRequestsParams{
		ItemName:    itemName,
		MinScore:    minScore,
		ResultLimit: maxSimilar,
	})
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
		// The lock still matters - it is what serializes concurrent joins so the totals
		// below cannot be computed from a snapshot missing one - but the row it returns
		// is no longer read: there is no status left to gate on.
		if _, err := q.LockRequest(ctx, requestID); errors.Is(err, pgx.ErrNoRows) {
			return ErrRequestNotFound
		} else if err != nil {
			return fmt.Errorf("locking request: %w", err)
		}
		// Deliberately ungated. An INACTIVE request is one nobody is on rather than one
		// that has ended, so joining it is how it comes back - refusing here would make
		// the last customer to leave the one who closed it for good.
		if err := mutate(q, tx); err != nil {
			return err
		}

		var err error
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

// Nothing sets a status. Close and SetStatus lived here: the owner ended the request
// for everyone, and Admin/Contact marked it approved. Both are gone - an owner is a
// participant like any other and leaves, and an approval decides a seller rather than
// the demand. What is left of either is RecalculateDemand deriving the status from who
// is still on the request.

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

// optionalText maps an empty filter value to SQL NULL, which the queries read as
// "no filter" rather than "match the empty string".
func optionalText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}
