ALTER TABLE purchase_request
    DROP CONSTRAINT purchase_request_status_valid;

ALTER TABLE purchase_request
    ADD CONSTRAINT purchase_request_status_valid
        CHECK (status IN ('OPEN', 'OFFER_PENDING', 'OFFER_APPROVED', 'CLOSED', 'CANCELLED'));
