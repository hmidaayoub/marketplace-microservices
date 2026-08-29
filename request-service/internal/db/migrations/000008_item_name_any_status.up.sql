-- One item, one request - whatever its status.
--
-- A seller can now open demand that has no buyers on it yet: offering against an item
-- nobody has requested creates the request the offer needs, with nobody on it. Such a
-- request is INACTIVE from the moment it exists, because INACTIVE is simply what "no
-- participants" is called.
--
-- Every item-name lookup was restricted to OPEN, which would hide exactly those
-- requests from the one person who should find them. The first customer to want the
-- item would be told nothing exists, open a second request for it, and the seller's
-- offer would sit on demand that never fills - the split the whole exact-name rule
-- exists to prevent.
--
-- The same argument already applied to a request that emptied out. It was defensible
-- while a request could be CLOSED - a closed one genuinely could not be joined - but
-- 000007 removed that: a join revives an INACTIVE request, so there was never anything
-- for a second one to add.
--
-- So the predicate goes from the indexes. They cover every request now, which is what
-- the queries built on them ask for.

DROP INDEX IF EXISTS idx_purchase_request_open_item_key;
DROP INDEX IF EXISTS idx_purchase_request_open_item_key_trgm;
DROP INDEX IF EXISTS idx_purchase_request_open_item_words;

-- Serves the exact-name lookup that turns a second request for the same item into a
-- join, and now also the find-or-create a seller's offer goes through.
CREATE INDEX idx_purchase_request_item_key
    ON purchase_request (request_item_key(item_name));

-- What makes the similarity search an index lookup rather than a scan. GIN over
-- trigrams is what the % operator reads.
CREATE INDEX idx_purchase_request_item_key_trgm
    ON purchase_request USING gin (request_item_key(item_name) gin_trgm_ops);

-- Serves the containment test, which is an array-containment lookup and wants GIN.
CREATE INDEX idx_purchase_request_item_words
    ON purchase_request USING gin (request_item_words(item_name));
