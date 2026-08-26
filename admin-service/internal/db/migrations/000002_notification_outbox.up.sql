-- Transactional outbox for notification events (see docs/events.md).
--
-- Publishing used to happen after the business transaction committed, which meant a
-- broker that was down cost the notification outright: the request was joined and
-- nobody was ever told. The event is now written here in the same transaction as the
-- change that caused it, so the two either both happen or neither does, and a separate
-- relay moves rows to the broker afterwards.
--
-- That converts "lost while the broker is down" into "delivered late", which is the
-- only honest guarantee available without distributed transactions.

CREATE TABLE notification_outbox (
    outbox_id    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Becomes the envelope's eventId. Generated here rather than at publish time so a
    -- relay that publishes a row twice - it crashed between the publish and the commit
    -- that marks it sent - produces the same id both times, and the consumer's
    -- processed_event table recognises the redelivery instead of duplicating the
    -- notification.
    event_id     UUID        NOT NULL UNIQUE,

    routing_key  TEXT        NOT NULL,
    payload      JSONB       NOT NULL,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    attempts     INTEGER     NOT NULL DEFAULT 0,
    last_error   TEXT,

    CONSTRAINT notification_outbox_routing_key_not_blank
        CHECK (length(btrim(routing_key)) > 0)
);

-- The relay's only query: the oldest unpublished rows. Partial, so it stays small no
-- matter how much history the table accumulates.
CREATE INDEX idx_notification_outbox_pending
    ON notification_outbox (created_at)
    WHERE published_at IS NULL;
