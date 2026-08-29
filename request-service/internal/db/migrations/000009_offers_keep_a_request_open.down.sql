-- Back to the participant count alone. Any request held open only by its offers becomes
-- INACTIVE again, which is what the status meant before this column existed.
UPDATE purchase_request pr
SET status = 'INACTIVE'
WHERE pr.status = 'OPEN'
  AND (SELECT count(*) FROM request_participant rp WHERE rp.request_id = pr.request_id) = 0;

ALTER TABLE purchase_request
    DROP CONSTRAINT IF EXISTS purchase_request_total_offers_non_negative;

ALTER TABLE purchase_request
    DROP COLUMN total_offers;
