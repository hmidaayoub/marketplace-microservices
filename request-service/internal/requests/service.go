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

	"github.com/hmidaayoub/marketplace-microservices/request-service/internal/store"
)

var (
	ErrRequestNotFound    = errors.New("request not found")
	ErrNotParticipant     = errors.New("caller is not a participant in this request")
	ErrAlreadyParticipant = errors.New("caller has already joined this request")
	ErrRequestNotOpen     = errors.New("request is not open")
)

// StatusOpen is the only status in which participants may be added, changed or
// removed. Once an offer is in flight the demand an offer was made against must stop
// moving, so every participant mutation is gated on it.
const StatusOpen = "OPEN"

const uniqueViolation = "23505"

type Service struct {
	pool    *pgxpool.Pool
	queries *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: store.New(pool)}
}

type CreateInput struct {
	ItemName    string
	Description string
	Category    string
	Quantity    int32
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

	err := s.inTx(ctx, func(q *store.Queries) error {
		request, err := q.CreateRequest(ctx, store.CreateRequestParams{
			ItemName:    in.ItemName,
			Description: in.Description,
			Category:    in.Category,
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
		return nil
	})

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
func (s *Service) Join(ctx context.Context, requestID, customerID uuid.UUID, quantity int32) (store.PurchaseRequest, error) {
	return s.mutateParticipants(ctx, requestID, func(q *store.Queries) error {
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
	})
}

// UpdateQuantity changes the caller's own quantity. The caller is identified by the
// token, so one customer can never edit another's line.
func (s *Service) UpdateQuantity(ctx context.Context, requestID, customerID uuid.UUID, quantity int32) (store.PurchaseRequest, error) {
	return s.mutateParticipants(ctx, requestID, func(q *store.Queries) error {
		_, err := q.UpdateParticipantQuantity(ctx, store.UpdateParticipantQuantityParams{
			RequestID:  requestID,
			CustomerID: customerID,
			Quantity:   quantity,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotParticipant
		}
		return err
	})
}

func (s *Service) Leave(ctx context.Context, requestID, customerID uuid.UUID) (store.PurchaseRequest, error) {
	return s.mutateParticipants(ctx, requestID, func(q *store.Queries) error {
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
	})
}

// mutateParticipants runs a participant change and the demand recalculation it implies
// as one unit. The request row is locked first so concurrent joins on the same request
// serialize: without it two transactions could each recompute totals from a snapshot
// that misses the other's insert (R4).
func (s *Service) mutateParticipants(
	ctx context.Context,
	requestID uuid.UUID,
	mutate func(*store.Queries) error,
) (store.PurchaseRequest, error) {
	var updated store.PurchaseRequest

	err := s.inTx(ctx, func(q *store.Queries) error {
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

		if err := mutate(q); err != nil {
			return err
		}

		updated, err = q.RecalculateDemand(ctx, requestID)
		if err != nil {
			return fmt.Errorf("recalculating demand: %w", err)
		}
		return nil
	})

	return updated, err
}

func (s *Service) inTx(ctx context.Context, fn func(*store.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	// No-op once the transaction commits; guarantees rollback on every early return.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(s.queries.WithTx(tx)); err != nil {
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
