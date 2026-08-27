DROP INDEX IF EXISTS idx_purchase_request_open_item_words;
DROP INDEX IF EXISTS idx_purchase_request_open_item_key_trgm;
DROP INDEX IF EXISTS idx_purchase_request_open_item_key;
DROP FUNCTION IF EXISTS request_item_words(text);

-- Back to the 000005 definition: normalization only, no aliases and no noise list.
CREATE OR REPLACE FUNCTION request_item_key(item_name text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    RETURN btrim(regexp_replace(lower(item_name), '[^a-z0-9]+', ' ', 'g'));

CREATE INDEX idx_purchase_request_open_item_key
    ON purchase_request (request_item_key(item_name))
    WHERE status = 'OPEN';

CREATE INDEX idx_purchase_request_open_item_key_trgm
    ON purchase_request USING gin (request_item_key(item_name) gin_trgm_ops)
    WHERE status = 'OPEN';
