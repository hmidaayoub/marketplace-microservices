-- Near-duplicate demand: matching item names exactly is not enough.
--
-- "Espresso Machine 2024", "espreso machine" and "Espresso-Machine" are all the same
-- product as "Espresso Machine", and each one opened its own request - splitting the
-- total a seller bids against, which is the number the whole platform exists to build.
-- Exact matching cannot see any of them, so creating a request now also looks for
-- names that are merely close, and makes the customer decide.
--
-- Trigrams are the right tool here because the failure modes are typos and extra words,
-- both of which leave most three-character sequences intact.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- The key both the exact match and the similarity score are computed on: case-folded,
-- with every run of punctuation or whitespace flattened to one space. It makes
-- "Espresso-Machine" and "espresso  machine" the same key outright, so those never
-- reach the fuzzy path at all.
--
-- IMMUTABLE because an expression index demands it, and it genuinely is: same text in,
-- same key out, forever. Changing this function's body would silently invalidate every
-- index built on it - it must be replaced by a migration that reindexes, not edited.
CREATE FUNCTION request_item_key(item_name text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    RETURN btrim(regexp_replace(lower(item_name), '[^a-z0-9]+', ' ', 'g'));

-- Replaces the exact-match index from 000004: the lookup it served now normalizes
-- punctuation too, and an index is only used when it matches the expression exactly.
DROP INDEX IF EXISTS idx_purchase_request_open_item_name;

CREATE INDEX idx_purchase_request_open_item_key
    ON purchase_request (request_item_key(item_name))
    WHERE status = 'OPEN';

-- What makes the similarity search an index lookup rather than a scan of all open
-- demand. GIN over trigrams is what the % operator reads.
CREATE INDEX idx_purchase_request_open_item_key_trgm
    ON purchase_request USING gin (request_item_key(item_name) gin_trgm_ops)
    WHERE status = 'OPEN';

-- The % operator compares against a session GUC, and the queries lean on it as their
-- index-backed pre-filter before applying their own stricter score. Pinning it on the
-- database means the floor is a property of the schema rather than of whatever a
-- session inherited - 0.3 is the built-in default, stated here so it cannot drift.
DO $$
BEGIN
    EXECUTE format('ALTER DATABASE %I SET pg_trgm.similarity_threshold = 0.3', current_database());
END
$$;
