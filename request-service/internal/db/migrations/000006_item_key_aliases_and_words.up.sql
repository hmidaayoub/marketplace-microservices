-- Three more ways two names mean one product, none of which a similarity score can see.
--
-- The scores measured against real pairs say plainly that scoring alone is finished:
-- "Espresso Machine" against "Espresso Machine Pro Deluxe 2024" reaches only .515,
-- while "Laptop" against "Laptop Stand" - two genuinely different products - reaches
-- .538. A real duplicate scoring below a real non-duplicate is not a threshold that
-- wants tuning, it is the wrong axis. So this adds signals instead of moving a number:
--
--   1. Filler and model years leave the key entirely, which turns a near-match into an
--      exact one: "Espresso Machine (brand new)" becomes the same key as "Espresso
--      Machine" and joins outright, with no fuzzy path involved.
--   2. Abbreviations resolve to what they abbreviate. "PS5" and "PlayStation 5" share
--      no trigram worth the name - they score .059 - and no threshold reaches that.
--   3. Whole words, not characters: a name that contains every word of another is about
--      the same product with more said about it.
--
-- The word list and the aliases are held inside the function rather than in a table
-- because an expression index demands an IMMUTABLE function, and a function that reads
-- a table is not one. Extending either means a migration that drops the indexes,
-- replaces the function and builds them again - as this one does.

DROP INDEX IF EXISTS idx_purchase_request_open_item_key_trgm;
DROP INDEX IF EXISTS idx_purchase_request_open_item_key;

-- Replaces the 000005 definition. Same contract - text in, comparison key out - with
-- aliases resolved and noise dropped along the way.
--
-- The coalesce is not decoration: a name made of nothing but noise ("New", "2024")
-- would otherwise key to the empty string and match every other such name. When
-- stripping empties a name, the plain normalized form is kept instead.
CREATE OR REPLACE FUNCTION request_item_key(item_name text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
AS $fn$
    SELECT coalesce(
        nullif(btrim((
            SELECT string_agg(word, ' ' ORDER BY ord)
            FROM (
                SELECT w.ord, coalesce(a.canonical, w.word) AS word
                FROM regexp_split_to_table(
                         btrim(regexp_replace(lower(item_name), '[^a-z0-9]+', ' ', 'g')), ' ')
                     WITH ORDINALITY AS w(word, ord)

                -- Only pairs that are the same thing said two ways. Anything arguable -
                -- "mobile" for "phone", "pc" for "computer" - is left out: this list
                -- merges requests outright, so it earns its entries conservatively.
                LEFT JOIN (VALUES
                    ('ps5', 'playstation 5'),
                    ('ps4', 'playstation 4'),
                    ('ps3', 'playstation 3'),
                    ('tv', 'television'),
                    ('macbook', 'mac book'),
                    ('airpods', 'air pods'),
                    ('fridge', 'refrigerator')
                ) AS a(alias, canonical) ON a.alias = w.word

                WHERE w.word <> ''
                  -- Condition and packaging, which the platform does not model, plus
                  -- the articles that survive normalization. Deliberately short: a word
                  -- removed here can no longer tell two products apart, so "pro",
                  -- "max", "mini" and every other model qualifier stay.
                  AND w.word NOT IN ('a','an','the','of','for',
                                     'new','brand','original','genuine','authentic',
                                     'sealed','unopened','unused','boxed','used')
                  -- A model year describes an edition, not a different product.
                  AND w.word !~ '^(19|20)[0-9][0-9]$'
            ) kept
        )), ''),
        btrim(regexp_replace(lower(item_name), '[^a-z0-9]+', ' ', 'g'))
    )
$fn$;

-- The same key as words, which is what containment is asked in terms of.
CREATE FUNCTION request_item_words(item_name text) RETURNS text[]
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    RETURN string_to_array(request_item_key(item_name), ' ');

CREATE INDEX idx_purchase_request_open_item_key
    ON purchase_request (request_item_key(item_name))
    WHERE status = 'OPEN';

CREATE INDEX idx_purchase_request_open_item_key_trgm
    ON purchase_request USING gin (request_item_key(item_name) gin_trgm_ops)
    WHERE status = 'OPEN';

-- Serves the containment test, which is an array-containment lookup and wants GIN.
CREATE INDEX idx_purchase_request_open_item_words
    ON purchase_request USING gin (request_item_words(item_name))
    WHERE status = 'OPEN';
