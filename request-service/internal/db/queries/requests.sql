-- name: CreateRequest :one
INSERT INTO purchase_request (item_name, description, category)
VALUES (@item_name, @description, @category)
RETURNING *;

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
