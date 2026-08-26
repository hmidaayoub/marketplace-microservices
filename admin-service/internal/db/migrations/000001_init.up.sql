-- Admin/Contact Service schema (spec section 12).
--
-- This service holds two things: the audit record of every admin decision, and the
-- explicit permission that lets a seller reach a customer's phone number. R8 is the
-- reason they are separate tables - approving an offer does not by itself expose a
-- phone number, it creates contact permission, and that permission is what
-- /api/contacts/requests/{requestId} checks before any number is fetched.

CREATE TABLE offer_decision (
    decision_id   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    offer_id      UUID        NOT NULL,
    admin_user_id UUID        NOT NULL,
    decision      TEXT        NOT NULL,
    reason        TEXT        NOT NULL DEFAULT '',
    decided_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- R7: an offer is decided once. The unique constraint, not a prior read, is what
    -- makes a double decision impossible - two concurrent approvals would both pass a
    -- read-then-write check, and the second would silently overwrite the audit record
    -- of the first.
    CONSTRAINT offer_decision_one_per_offer UNIQUE (offer_id),
    CONSTRAINT offer_decision_valid
        CHECK (decision IN ('APPROVED', 'REJECTED'))
);

CREATE TABLE contact_access (
    access_id   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_id   UUID        NOT NULL,
    customer_id UUID        NOT NULL,
    request_id  UUID        NOT NULL,
    offer_id    UUID        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'GRANTED',
    granted_by  UUID        NOT NULL,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ,

    -- One grant per customer per approved offer. Re-approving cannot fan out duplicate
    -- permissions, and revoking one row cannot leave a second live row behind it.
    CONSTRAINT contact_access_unique_per_offer_customer UNIQUE (offer_id, customer_id),
    CONSTRAINT contact_access_status_valid
        CHECK (status IN ('GRANTED', 'REVOKED', 'EXPIRED'))
);

-- Serves the seller's own contact lookup (R9): seller + request, filtered on status.
CREATE INDEX idx_contact_access_seller_request ON contact_access (seller_id, request_id);

-- Serves the admin listing and the internal permission check.
CREATE INDEX idx_contact_access_seller_customer ON contact_access (seller_id, customer_id);
CREATE INDEX idx_contact_access_granted_at ON contact_access (granted_at DESC);
