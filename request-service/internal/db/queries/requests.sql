-- name: CreateRequest :one
INSERT INTO purchase_request (item_name, description, category, created_by)
VALUES (@item_name, @description, @category, @created_by)
RETURNING *;

-- R1 with a twist: a second customer asking for the same item is not new demand, it is
-- more of the demand that already exists - so there is nothing to create, and this is
-- what says so. Names are compared on request_item_key, so "Espresso Machine",
-- "espresso machine " and "Espresso-Machine" are one request; the oldest open one wins.
-- Only OPEN requests match - a closed one cannot be joined, so naming it opens a fresh
-- request.
--
-- No FOR UPDATE: creating never writes to the request it finds. Joining is a separate
-- call the customer makes for themselves, and that path takes the lock it needs.
-- name: FindOpenRequestByItemName :one
SELECT * FROM purchase_request
WHERE request_item_key(item_name) = request_item_key(@item_name::text)
  AND status = 'OPEN'
ORDER BY created_at, request_id
LIMIT 1;

-- Held for the length of the transaction, keyed on the normalized name. Two customers
-- naming the same brand-new item at the same instant would otherwise both find nothing
-- above and both create a request; with this the second one waits, then finds the first
-- one's. It locks a name, not a row, so there is nothing to lock before the row exists.
-- name: LockItemName :exec
SELECT pg_advisory_xact_lock(hashtext(request_item_key(@item_name::text)));

-- The names an exact match cannot see, by either of the two signals that catch them.
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
-- name: FindSimilarOpenRequests :many
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
WHERE pr.status = 'OPEN'
  AND (
        (request_item_key(pr.item_name) % request_item_key(@item_name::text)
         AND similarity(request_item_key(pr.item_name), request_item_key(@item_name::text)) >= @min_score::real)
     OR (array_length(request_item_words(pr.item_name), 1) >= 2
         AND request_item_words(@item_name::text) @> request_item_words(pr.item_name))
     OR (array_length(request_item_words(@item_name::text), 1) >= 2
         AND request_item_words(pr.item_name) @> request_item_words(@item_name::text))
      )
ORDER BY exact DESC, contains DESC, score DESC, pr.created_at
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
-- name: RecalculateDemand :one
UPDATE purchase_request pr
SET total_customers = d.total_customers,
    total_quantity  = d.total_quantity,
    updated_at      = now()
FROM (
    SELECT COUNT(*)::int                       AS total_customers,
           COALESCE(SUM(quantity), 0)::bigint  AS total_quantity
    FROM request_participant
    WHERE request_id = @request_id
) d
WHERE pr.request_id = @request_id
RETURNING pr.*;

-- Sets a terminal or in-flight status. Used by the owner closing their own request and,
-- through the internal API, by Admin/Contact once an offer has been approved.
-- name: SetRequestStatus :one
UPDATE purchase_request
SET status = @status, updated_at = now()
WHERE request_id = @request_id
RETURNING *;
