DROP INDEX IF EXISTS idx_purchase_request_item_key;
DROP INDEX IF EXISTS idx_purchase_request_item_key_trgm;
DROP INDEX IF EXISTS idx_purchase_request_item_words;

CREATE INDEX idx_purchase_request_open_item_key
    ON purchase_request (request_item_key(item_name))
    WHERE status = 'OPEN';

CREATE INDEX idx_purchase_request_open_item_key_trgm
    ON purchase_request USING gin (request_item_key(item_name) gin_trgm_ops)
    WHERE status = 'OPEN';

CREATE INDEX idx_purchase_request_open_item_words
    ON purchase_request USING gin (request_item_words(item_name))
    WHERE status = 'OPEN';
