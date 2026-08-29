-- name: CreateRequest :one
INSERT INTO purchase_request (item_name, description, category, created_by)
VALUES (@item_name, @description, @category, @created_by)
RETURNING *;

-- R1 with a twist: a second customer asking for the same item is not new demand, it is
-- more of the demand that already exists - so there is nothing to create, and this is
-- what says so. Names are compared on request_item_key, so "Espresso Machine",
-- "espresso machine " and "Espresso-Machine" are one request.
--
-- Every status matches, not just OPEN. INACTIVE is what a request with nobody on it is
-- called - the one a seller opened by offering against an item, and the one whose last
-- participant left - and a join revives either. A second request would have nothing to
-- add to one and would split the demand of the other, so both are found here.
--
-- OPEN wins when both exist, which only old data can produce: live demand is the better
-- thing to hand a customer, and the oldest breaks any remaining tie.
--
-- No FOR UPDATE: creating never writes to the request it finds. Joining is a separate
-- call the customer makes for themselves, and that path takes the lock it needs.
-- name: FindRequestByItemName :one
SELECT * FROM purchase_request
WHERE request_item_key(item_name) = request_item_key(@item_name::text)
ORDER BY (status = 'OPEN') DESC, created_at, request_id
LIMIT 1;

-- Held for the length of the transaction, keyed on the normalized name. Two customers
-- naming the same brand-new item at the same instant would otherwise both find nothing
-- above and both create a request; with this the second one waits, then finds the first
-- one's. It locks a name, not a row, so there is nothing to lock before the row exists.
-- name: LockItemName :exec
SELECT pg_advisory_xact_lock(hashtext(request_item_key(@item_name::text)));

-- The names an exact match cannot see, by either of the two signals that catch them.
--
-- Every status is searched, for the same reason the exact match searches every status: a
-- request with no buyers on it is the one most worth suggesting, since joining it is
-- what makes it demand again. The status travels with each row, so a caller can say
-- which it is offering.
--
-- score is trigram distance on the normalized key, which is what finds a typo. contains
-- is whole-word containment, which is what finds the same product with more said about
-- it - and it is a separate signal rather than a lower threshold because scoring cannot
-- express it: "Espresso Machine" reaches only .515 against "Espresso Machine Pro Deluxe
-- 2024", below the .538 that "Laptop" reaches against the genuinely different "Laptop
-- Stand". No single number separates those two; containment does.
--
-- Containment demands two words on the shorter side, which is the whole difference
-- between the case worth catching and the case worth leaving alone. Both are one name
-- inside another: "espresso machine" in "espresso machine pro deluxe 2024", "laptop" in
-- "laptop stand". The first is a product described at more length; the second is a
-- different product that happens to be named after the first. A single word is a
-- category far more often than it is an item, so one word never triggers this.
--
-- The % operator and @> do the narrowing, because those are the forms the GIN indexes
-- serve; min_score then applies the caller's own floor on top of the 0.3 the schema
-- pins % to. A min_score below 0.3 filters nothing extra - the operator has already
-- dropped those rows - and rows found only by containment are returned whatever they
-- score, which is the point of having a second signal at all.
-- name: FindSimilarRequests :many
SELECT sqlc.embed(pr),
       similarity(request_item_key(pr.item_name), request_item_key(@item_name::text)) AS score,
       -- The same name, not merely a close one. Carried so a caller can tell the two
       -- refusals apart: a near-match may be overridden, an exact one may not.
       (request_item_key(pr.item_name) = request_item_key(@item_name::text)) AS exact,
       -- coalesced because array_length is NULL for an empty array, and a key that
       -- normalized to nothing must read as "does not contain", not as unknown.
       COALESCE(
           (array_length(request_item_words(pr.item_name), 1) >= 2
            AND request_item_words(@item_name::text) @> request_item_words(pr.item_name))
        OR (array_length(request_item_words(@item_name::text), 1) >= 2
            AND request_item_words(pr.item_name) @> request_item_words(@item_name::text)),
           false
       )::boolean AS contains
FROM purchase_request pr
WHERE (
        (request_item_key(pr.item_name) % request_item_key(@item_name::text)
         AND similarity(request_item_key(pr.item_name), request_item_key(@item_name::text)) >= @min_score::real)
     OR (array_length(request_item_words(pr.item_name), 1) >= 2
         AND request_item_words(@item_name::text) @> request_item_words(pr.item_name))
     OR (array_length(request_item_words(@item_name::text), 1) >= 2
         AND request_item_words(pr.item_name) @> request_item_words(@item_name::text))
      )
-- Status only breaks a tie: a suggestion with buyers already on it is the better one
-- to offer, but never at the cost of a closer name.
ORDER BY exact DESC, contains DESC, score DESC, (pr.status = 'OPEN') DESC, pr.created_at
LIMIT @result_limit;

-- name: GetRequest :one
SELECT * FROM purchase_request
WHERE request_id = @request_id;

-- Serializes concurrent joins/leaves on the same request so demand recalculation
-- cannot interleave and land on a stale total.
-- name: LockRequest :one
SELECT * FROM purchase_request
WHERE request_id = @request_id
FOR UPDATE;

-- name: ListRequests :many
SELECT * FROM purchase_request
WHERE (sqlc.narg('item_name')::text IS NULL
       OR item_name ILIKE '%' || sqlc.narg('item_name')::text || '%')
  AND (sqlc.narg('category')::text IS NULL
       OR category = sqlc.narg('category')::text)
  AND (sqlc.narg('status')::text IS NULL
       OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC
LIMIT @result_limit OFFSET @result_offset;

-- name: ListRequestsByCustomer :many
SELECT pr.* FROM purchase_request pr
JOIN request_participant rp ON rp.request_id = pr.request_id
WHERE rp.customer_id = @customer_id
ORDER BY rp.joined_at DESC;

-- name: AddParticipant :one
INSERT INTO request_participant (request_id, customer_id, quantity)
VALUES (@request_id, @customer_id, @quantity)
RETURNING *;

-- name: GetParticipant :one
SELECT * FROM request_participant
WHERE request_id = @request_id AND customer_id = @customer_id;

-- name: UpdateParticipantQuantity :one
UPDATE request_participant
SET quantity = @quantity
WHERE request_id = @request_id AND customer_id = @customer_id
RETURNING *;

-- name: DeleteParticipant :execrows
DELETE FROM request_participant
WHERE request_id = @request_id AND customer_id = @customer_id;

-- name: ListParticipantCustomerIDs :many
SELECT customer_id FROM request_participant
WHERE request_id = @request_id
ORDER BY joined_at;

-- R4: the service, not the caller, owns totalCustomers and totalQuantity. Recomputed
-- from request_participant in the same transaction as the mutation that changed it.
--
-- The status is recomputed with them, because it is the same fact: a request nobody is
-- on and nobody is selling into is INACTIVE, and either a join or an offer makes it OPEN
-- again. That makes the status derived rather than commanded - there is no call that
-- sets it, so it can never disagree with the counts it describes.
--
-- This is the only statement that decides a status, which is why SetOfferCount writes
-- its column and then leaves the deciding to this: two places computing it would be two
-- places to disagree. total_offers is read rather than recomputed, because the offers it
-- counts live in another service's database.
-- name: RecalculateDemand :one
UPDATE purchase_request pr
SET total_customers = d.total_customers,
    total_quantity  = d.total_quantity,
    status          = CASE
                          WHEN d.total_customers = 0 AND pr.total_offers = 0 THEN 'INACTIVE'
                          ELSE 'OPEN'
                      END,
    updated_at      = now()
FROM (
    SELECT COUNT(*)::int                       AS total_customers,
           COALESCE(SUM(quantity), 0)::bigint  AS total_quantity
    FROM request_participant
    WHERE request_id = @request_id
) d
WHERE pr.request_id = @request_id
RETURNING pr.*;

-- How many live offers stand on this request, as offer-service counts them. An absolute
-- number rather than a delta, so a call that is retried or arrives twice lands on the
-- same answer instead of drifting away from the truth.
--
-- It writes the column and nothing else. The status that depends on it is left to
-- RecalculateDemand, which the service calls next in the same transaction.
-- name: SetOfferCount :exec
UPDATE purchase_request
SET total_offers = @total_offers
WHERE request_id = @request_id;
