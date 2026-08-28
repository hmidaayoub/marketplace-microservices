-- A request no longer ends. It is OPEN while anyone wants the item and INACTIVE when
-- the last participant leaves, and a join revives it - so the status is derived from
-- the buyer count rather than commanded by anything.
--
-- OFFER_APPROVED and CLOSED went with that. An approval decides which seller may reach
-- the buyers; it says nothing about whether they still want the item, and having it end
-- the request meant demand that outlived one deal could not carry on being demand.
UPDATE purchase_request pr
SET status = CASE
        WHEN (SELECT count(*) FROM request_participant rp
              WHERE rp.request_id = pr.request_id) = 0 THEN 'INACTIVE'
        ELSE 'OPEN'
    END
WHERE pr.status <> 'OPEN';

ALTER TABLE purchase_request
    DROP CONSTRAINT purchase_request_status_valid;

ALTER TABLE purchase_request
    ADD CONSTRAINT purchase_request_status_valid
        CHECK (status IN ('OPEN', 'INACTIVE'));
