-- Request Service schema (spec section 10).
--
-- total_customers and total_quantity are stored on purchase_request because the spec
-- lists them as fields of the aggregate, but they are never written by hand: every
-- participant mutation recomputes them from request_participant inside the same
-- transaction (see RecalculateDemand). The participant table stays the source of truth,
-- so the two columns cannot drift from it.

CREATE TABLE purchase_request (
    request_id      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    item_name       TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    category        TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'OPEN',
    total_customers INTEGER     NOT NULL DEFAULT 0,
    total_quantity  BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT purchase_request_status_valid
        CHECK (status IN ('OPEN', 'OFFER_PENDING', 'OFFER_APPROVED', 'CLOSED', 'CANCELLED')),
    CONSTRAINT purchase_request_item_name_not_blank
        CHECK (length(btrim(item_name)) > 0),
    CONSTRAINT purchase_request_totals_non_negative
        CHECK (total_customers >= 0 AND total_quantity >= 0)
);

CREATE TABLE request_participant (
    participant_id UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id     UUID        NOT NULL REFERENCES purchase_request (request_id) ON DELETE CASCADE,
    customer_id    UUID        NOT NULL,
    quantity       INTEGER     NOT NULL,
    joined_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- R2/R3: a customer participates in a given request at most once, with one quantity.
    -- Enforced here rather than only in code so a race cannot create a double join.
    CONSTRAINT request_participant_unique_per_customer UNIQUE (request_id, customer_id),
    CONSTRAINT request_participant_quantity_positive CHECK (quantity > 0)
);

-- Serves GET /api/requests/me and the internal participants lookup.
CREATE INDEX idx_request_participant_customer ON request_participant (customer_id);
CREATE INDEX idx_request_participant_request ON request_participant (request_id);

-- Serves the default browse: open requests, newest first.
CREATE INDEX idx_purchase_request_status_created ON purchase_request (status, created_at DESC);
