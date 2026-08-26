package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Enqueue records an event in the outbox using the caller's transaction.
//
// This is the whole point of the pattern: the event and the business change it
// describes are one write. There is no window in which a request is joined but the
// notification has vanished, because a failure rolls back both.
//
// Deliberately raw SQL rather than a generated query: the table is identical in every
// producer, so keeping it here means one definition rather than one per service's sqlc
// configuration.
func Enqueue(ctx context.Context, tx pgx.Tx, routingKey string, notifications ...Notification) error {
	if len(notifications) == 0 {
		return nil
	}

	payload, err := json.Marshal(notifications)
	if err != nil {
		return fmt.Errorf("encoding outbox payload: %w", err)
	}

	// The eventId is fixed now rather than at publish time. A relay that dies between
	// publishing and marking the row sent will publish it again on restart with the
	// same id, which the consumer's processed_event table recognises - so at-least-once
	// relaying stays exactly-once in effect.
	_, err = tx.Exec(ctx,
		`INSERT INTO notification_outbox (event_id, routing_key, payload) VALUES ($1, $2, $3)`,
		uuid.New(), routingKey, payload)
	if err != nil {
		return fmt.Errorf("enqueuing %s: %w", routingKey, err)
	}
	return nil
}

const (
	relayInterval  = 2 * time.Second
	relayBatchSize = 100
)

// Relay drains the outbox to the broker.
//
// It polls rather than listens: a LISTEN/NOTIFY subscription would be lower latency but
// would also silently miss rows written while the connection was down, which is the one
// case the outbox exists for. A poll cannot miss anything - it re-reads the table - and
// Wake() covers the latency the interval would otherwise add.
type Relay struct {
	pool      *pgxpool.Pool
	publisher *Publisher
	source    string
	wake      chan struct{}
	done      chan struct{}
}

func NewRelay(pool *pgxpool.Pool, publisher *Publisher, source string) *Relay {
	return &Relay{
		pool:      pool,
		publisher: publisher,
		source:    source,
		// Buffered and non-blocking: a wake-up is a hint, and dropping one when the
		// relay is already about to run loses nothing.
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

// Wake asks the relay to drain now instead of waiting for the next tick, so a
// notification is not held for the poll interval when the broker is healthy.
func (r *Relay) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Relay) Start(ctx context.Context) {
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(relayInterval)
		defer ticker.Stop()

		for {
			if n, err := r.drain(ctx); err != nil {
				slog.ErrorContext(ctx, "outbox relay failed; will retry", "error", err)
			} else if n > 0 {
				slog.InfoContext(ctx, "outbox relay published events", "count", n)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-r.wake:
			}
		}
	}()
}

// Wait blocks until the relay loop has stopped, so shutdown does not race a publish.
func (r *Relay) Wait() { <-r.done }

// drain publishes one batch. Each row is marked sent in the same transaction that read
// it, so two replicas cannot publish the same event: SKIP LOCKED hands each row to
// exactly one of them.
func (r *Relay) drain(ctx context.Context) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("beginning relay transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`SELECT outbox_id, event_id, routing_key, payload
		   FROM notification_outbox
		  WHERE published_at IS NULL
		  ORDER BY created_at
		  LIMIT $1
		  FOR UPDATE SKIP LOCKED`, relayBatchSize)
	if err != nil {
		return 0, fmt.Errorf("reading outbox: %w", err)
	}

	type pending struct {
		outboxID   uuid.UUID
		eventID    uuid.UUID
		routingKey string
		payload    []byte
	}

	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.outboxID, &p.eventID, &p.routingKey, &p.payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scanning outbox row: %w", err)
		}
		batch = append(batch, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterating outbox: %w", err)
	}
	if len(batch) == 0 {
		return 0, nil
	}

	published := 0
	for _, p := range batch {
		var notifications []Notification
		if err := json.Unmarshal(p.payload, &notifications); err != nil {
			// Unpublishable however often it is retried. Marking it sent with the error
			// recorded stops it blocking the queue behind it; the row stays for
			// inspection, which is what the dead-letter queue does for the consumer.
			slog.ErrorContext(ctx, "outbox row is undecodable; retiring it",
				"outboxId", p.outboxID, "error", err)
			if err := markFailed(ctx, tx, p.outboxID, err); err != nil {
				return published, err
			}
			continue
		}

		if err := r.publisher.PublishEvent(ctx, p.eventID, p.routingKey, notifications); err != nil {
			// The broker is unreachable. Leave the row pending and stop: the rows behind
			// it would fail too, and the next tick retries from the same place.
			if recordErr := recordAttempt(ctx, tx, p.outboxID, err); recordErr != nil {
				return published, recordErr
			}
			break
		}

		if err := markPublished(ctx, tx, p.outboxID); err != nil {
			return published, err
		}
		published++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing relay transaction: %w", err)
	}
	return published, nil
}

func markPublished(ctx context.Context, tx pgx.Tx, outboxID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE notification_outbox SET published_at = now(), attempts = attempts + 1, last_error = NULL
		  WHERE outbox_id = $1`, outboxID)
	if err != nil {
		return fmt.Errorf("marking outbox row published: %w", err)
	}
	return nil
}

func markFailed(ctx context.Context, tx pgx.Tx, outboxID uuid.UUID, cause error) error {
	_, err := tx.Exec(ctx,
		`UPDATE notification_outbox SET published_at = now(), attempts = attempts + 1, last_error = $2
		  WHERE outbox_id = $1`, outboxID, cause.Error())
	if err != nil {
		return fmt.Errorf("retiring outbox row: %w", err)
	}
	return nil
}

func recordAttempt(ctx context.Context, tx pgx.Tx, outboxID uuid.UUID, cause error) error {
	_, err := tx.Exec(ctx,
		`UPDATE notification_outbox SET attempts = attempts + 1, last_error = $2 WHERE outbox_id = $1`,
		outboxID, cause.Error())
	if err != nil {
		return fmt.Errorf("recording outbox attempt: %w", err)
	}
	return nil
}
