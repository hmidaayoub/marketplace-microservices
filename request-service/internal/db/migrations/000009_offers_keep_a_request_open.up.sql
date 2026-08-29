-- A request is dormant only when nobody wants the item and nobody is selling it.
--
-- INACTIVE was derived from the participant count alone, which made the last buyer to
-- leave able to mark a request dormant out from under a seller whose offer was still
-- standing on it. That offer is live demand-side activity: it is under review, or it has
-- been approved and contact details released against it. Calling the request inactive
-- while that is true says something false about it.
--
-- So the status now reads from two counts, and this is the second. It is written by
-- offer-service through PUT /internal/requests/{id}/offers/count rather than computed
-- here, because offers live in another service's database and nothing in this one may
-- read it. The count is the absolute number of live offers - PENDING or APPROVED - and
-- not a delta, so a retried or duplicated call lands on the same answer instead of
-- drifting.
--
-- Existing rows start at zero and cannot be backfilled from here for the same reason:
-- this database does not know what offers exist. offer-service pushes the true count on
-- the next write touching each request, so a request carrying offers reads 0 until then.
-- Nothing is lost by it - the count only ever holds a request open, never closes one -
-- and status is recomputed the moment the count arrives.

ALTER TABLE purchase_request
    ADD COLUMN total_offers INTEGER NOT NULL DEFAULT 0;

ALTER TABLE purchase_request
    ADD CONSTRAINT purchase_request_total_offers_non_negative
        CHECK (total_offers >= 0);

-- Whatever is OPEN stays OPEN: adding a count that starts at zero can only be the
-- difference between INACTIVE and OPEN once a real count arrives, never the reverse.
