// Package events publishes notification events to the broker (see docs/events.md).
//
// Publishing is deliberately best-effort and always happens after the business
// transaction has committed. A broker that is down must not roll back a request that
// was genuinely joined: the customer joined it, and only the "you joined" message is
// missing. Every failure here is logged and swallowed.
//
// Closing that gap needs a transactional outbox in each producer, which is noted in
// docs/events.md rather than built.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	Exchange           = "marketplace.events"
	DeadLetterExchange = "marketplace.events.dlx"
	Queue              = "notification.events"
	DeadLetterQueue    = "notification.events.dlq"
	BindingKey         = "#"
)

// Routing keys. The semantic event lives here; the notification type travels in the
// payload, because one event can produce notifications of more than one type.
const (
	KeyRequestJoined        = "request.joined"
	KeyRequestClosed        = "request.closed"
	KeyOfferCreated         = "offer.created"
	KeyOfferApproved        = "offer.approved"
	KeyOfferRejected        = "offer.rejected"
	KeyContactAccessGranted = "contact.access.granted"
)

// Notification mirrors notification-service's create schema exactly, so the AMQP path
// and its HTTP internal API cannot drift.
type Notification struct {
	UserID  uuid.UUID `json:"userId"`
	Type    string    `json:"type"`
	Channel string    `json:"channel,omitempty"`
	Title   string    `json:"title"`
	Message string    `json:"message"`
}

type envelope struct {
	EventID       uuid.UUID      `json:"eventId"`
	OccurredAt    time.Time      `json:"occurredAt"`
	Source        string         `json:"source"`
	Notifications []Notification `json:"notifications"`
}

// publishTimeout bounds how long a business call can be delayed by the broker, dialling
// included. A local publish is sub-millisecond; this exists for the case where the
// broker is gone, which must not hold a request open.
//
// dialTimeout has to be set explicitly: amqp.Dial defaults to 30 seconds, and a stopped
// container on a Docker network blackholes the connection rather than refusing it, so
// the default turned every request into a 16-second wait for a broker nobody was
// waiting on.
const (
	publishTimeout = 2 * time.Second
	dialTimeout    = 700 * time.Millisecond
)

// Publisher owns one lazily-established connection.
//
// Lazy rather than dialled at startup because the service must come up whether or not
// the broker is there, and reconnecting on demand covers a broker that restarts
// underneath a running service. The mutex serialises reconnects, not publishes.
type Publisher struct {
	url    string
	source string

	mu      sync.Mutex
	conn    *amqp.Connection
	channel *amqp.Channel
}

func NewPublisher(url, source string) *Publisher {
	return &Publisher{url: url, source: source}
}

// Publish sends one event carrying one or more notifications. The error is returned for
// logging, never for propagating to a caller: see the package comment.
func (p *Publisher) Publish(ctx context.Context, routingKey string, notifications ...Notification) error {
	if len(notifications) == 0 {
		return nil
	}

	body, err := json.Marshal(envelope{
		EventID:       uuid.New(),
		OccurredAt:    time.Now().UTC(),
		Source:        p.source,
		Notifications: notifications,
	})
	if err != nil {
		return fmt.Errorf("encoding event: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()

	// Two attempts, because the first publish after a broker restart is expected to
	// fail: the cached connection looks alive until it is used, and only the attempt
	// itself reveals otherwise. Without the retry every broker blip silently costs one
	// notification - the failure is discovered by the publish that should have carried
	// it. The second attempt redials, so it is the one that gets through.
	var lastErr error
	for range 2 {
		// The whole call is bounded, retry included: a second attempt is only worth
		// making if there is time left for it to succeed.
		if ctx.Err() != nil {
			break
		}

		channel, err := p.acquire()
		if err != nil {
			lastErr = err
			p.discard()
			continue
		}

		err = channel.PublishWithContext(ctx, Exchange, routingKey, false, false, amqp.Publishing{
			ContentType: "application/json",
			// Persistent, so an event already accepted by the broker survives a restart
			// of it. The queue and exchange are durable for the same reason.
			DeliveryMode: amqp.Persistent,
			MessageId:    uuid.NewString(),
			Timestamp:    time.Now().UTC(),
			Body:         body,
		})
		if err == nil {
			return nil
		}

		lastErr = err
		// The connection is dead; drop it so the next attempt redials rather than
		// failing again against the same stale channel.
		p.discard()
	}
	return fmt.Errorf("publishing %s: %w", routingKey, lastErr)
}

// PublishOrLog is what handlers call. It makes the best-effort contract impossible to
// get wrong by accident: there is no error to ignore.
func (p *Publisher) PublishOrLog(ctx context.Context, routingKey string, notifications ...Notification) {
	if err := p.Publish(ctx, routingKey, notifications...); err != nil {
		slog.ErrorContext(ctx, "publishing notification event failed; continuing",
			"routingKey", routingKey, "error", err)
	}
}

func (p *Publisher) acquire() (*amqp.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.channel != nil && !p.channel.IsClosed() {
		return p.channel, nil
	}

	conn, err := amqp.DialConfig(p.url, amqp.Config{
		Dial:      amqp.DefaultDial(dialTimeout),
		Heartbeat: 10 * time.Second,
		Locale:    "en_US",
	})
	if err != nil {
		return nil, fmt.Errorf("dialling broker: %w", err)
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("opening channel: %w", err)
	}
	if err := DeclareTopology(channel); err != nil {
		_ = conn.Close()
		return nil, err
	}

	p.conn, p.channel = conn, channel
	return channel, nil
}

func (p *Publisher) discard() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.conn, p.channel = nil, nil
}

func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return nil
	}
	err := p.conn.Close()
	p.conn, p.channel = nil, nil
	return err
}

// DeclareTopology declares the exchange, queue and bindings.
//
// Every side declares the same thing, idempotently, so a fresh broker needs no setup
// step and it does not matter whether a producer or the consumer connects first. A
// producer that only published would still want this: publishing to an exchange that
// does not exist yet is silently dropped.
func DeclareTopology(channel *amqp.Channel) error {
	if err := channel.ExchangeDeclare(DeadLetterExchange, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declaring dead-letter exchange: %w", err)
	}
	if _, err := channel.QueueDeclare(DeadLetterQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declaring dead-letter queue: %w", err)
	}
	if err := channel.QueueBind(DeadLetterQueue, "", DeadLetterExchange, false, nil); err != nil {
		return fmt.Errorf("binding dead-letter queue: %w", err)
	}

	if err := channel.ExchangeDeclare(Exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declaring exchange: %w", err)
	}
	if _, err := channel.QueueDeclare(Queue, true, false, false, false,
		amqp.Table{"x-dead-letter-exchange": DeadLetterExchange}); err != nil {
		return fmt.Errorf("declaring queue: %w", err)
	}
	if err := channel.QueueBind(Queue, BindingKey, Exchange, false, nil); err != nil {
		return fmt.Errorf("binding queue: %w", err)
	}
	return nil
}
