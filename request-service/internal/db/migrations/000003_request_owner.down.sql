DROP INDEX IF EXISTS idx_purchase_request_created_by;
ALTER TABLE purchase_request DROP COLUMN IF EXISTS created_by;
