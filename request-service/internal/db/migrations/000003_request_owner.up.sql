-- Records who created a request, so it has an owner who can close it.
--
-- R1 says a customer creates a purchase request, and closing one is the owner's
-- decision - but until now nothing recorded which participant that was. The creator was
-- only implicitly the earliest row in request_participant, which stops being true the
-- moment they leave.

ALTER TABLE purchase_request ADD COLUMN created_by UUID;

-- Backfill from the earliest participant, which is who Create enrolled first.
UPDATE purchase_request pr
SET created_by = (
    SELECT rp.customer_id
    FROM request_participant rp
    WHERE rp.request_id = pr.request_id
    ORDER BY rp.joined_at
    LIMIT 1
);

-- Deliberately left nullable rather than NOT NULL. A request whose participants have
-- all left has no row to infer an owner from, and inventing one would be worse than
-- admitting it is unknown: such a request simply cannot be closed by an owner, and it
-- has nobody left to notify anyway. Every request created from now on carries one.
CREATE INDEX idx_purchase_request_created_by ON purchase_request (created_by);
