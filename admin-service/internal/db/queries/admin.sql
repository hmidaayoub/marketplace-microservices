-- name: RecordDecision :one
INSERT INTO offer_decision (offer_id, admin_user_id, decision, reason)
VALUES (@offer_id, @admin_user_id, @decision, @reason)
RETURNING *;

-- name: GetDecisionByOffer :one
SELECT * FROM offer_decision
WHERE offer_id = @offer_id;

-- name: GrantContactAccess :one
INSERT INTO contact_access (seller_id, customer_id, request_id, offer_id, granted_by, expires_at)
VALUES (@seller_id, @customer_id, @request_id, @offer_id, @granted_by, sqlc.narg('expires_at'))
RETURNING *;

-- name: GetContactAccess :one
SELECT * FROM contact_access
WHERE access_id = @access_id;

-- R9: what a seller is allowed to read for one request. A grant counts only while it
-- is GRANTED and unexpired, so revoking or expiring one takes effect on the next call
-- rather than needing a sweep.
-- name: ListGrantedForSellerRequest :many
SELECT * FROM contact_access
WHERE seller_id = @seller_id
  AND request_id = @request_id
  AND status = 'GRANTED'
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY granted_at;

-- Backs the internal permission check: may this seller reach this customer?
-- name: CountEffectiveAccess :one
SELECT COUNT(*) FROM contact_access
WHERE seller_id = @seller_id
  AND customer_id = @customer_id
  AND (sqlc.narg('request_id')::uuid IS NULL OR request_id = sqlc.narg('request_id')::uuid)
  AND status = 'GRANTED'
  AND (expires_at IS NULL OR expires_at > now());

-- name: ListContactAccess :many
SELECT * FROM contact_access
WHERE (sqlc.narg('seller_id')::uuid IS NULL OR seller_id = sqlc.narg('seller_id')::uuid)
  AND (sqlc.narg('request_id')::uuid IS NULL OR request_id = sqlc.narg('request_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY granted_at DESC
LIMIT @result_limit OFFSET @result_offset;

-- Revoking is a status change, not a delete: the grant is part of the audit history of
-- who was allowed to reach whom, and deleting the row would erase that.
-- name: RevokeContactAccess :one
UPDATE contact_access
SET status = 'REVOKED'
WHERE access_id = @access_id AND status = 'GRANTED'
RETURNING *;
