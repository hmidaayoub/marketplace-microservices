DO $$
BEGIN
    EXECUTE format('ALTER DATABASE %I RESET pg_trgm.similarity_threshold', current_database());
END
$$;

DROP INDEX IF EXISTS idx_purchase_request_open_item_key_trgm;
DROP INDEX IF EXISTS idx_purchase_request_open_item_key;
DROP FUNCTION IF EXISTS request_item_key(text);

-- Restores what 000004 built, so stepping back one version leaves the exact-match
-- lookup on the index it expects.
CREATE INDEX idx_purchase_request_open_item_name
    ON purchase_request (lower(btrim(item_name)))
    WHERE status = 'OPEN';
