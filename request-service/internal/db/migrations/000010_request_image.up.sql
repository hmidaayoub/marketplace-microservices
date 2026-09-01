-- An optional picture of the item a request is for.
--
-- The bytes live in their own table rather than in a column on purchase_request, and
-- that is not a stylistic choice: CreateRequest and RecalculateDemand both RETURNING *,
-- so a bytea on the request row would be read back on every create, every join and
-- every browse page - a megabyte per row, to render a list that shows none of it.
-- Here nothing reads them but the endpoint that serves the image.
--
-- What does live on the request is the media type, because "is there a picture" is part
-- of reading a request: the browse list needs it to decide whether to render an <img>,
-- and it is one small column rather than a join.
ALTER TABLE purchase_request
    ADD COLUMN image_type TEXT NOT NULL DEFAULT '';

-- Only the three formats a browser renders everywhere, and the empty string for the
-- requests - the overwhelming majority - that carry no picture at all.
ALTER TABLE purchase_request
    ADD CONSTRAINT purchase_request_image_type_valid
        CHECK (image_type IN ('', 'image/jpeg', 'image/png', 'image/webp'));

CREATE TABLE request_image (
    request_id UUID        PRIMARY KEY REFERENCES purchase_request (request_id) ON DELETE CASCADE,
    image_data BYTEA       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The service caps uploads well below this. The constraint is here as well so a
    -- future caller that bypasses the handler cannot turn this table into blob storage.
    CONSTRAINT request_image_within_size_cap CHECK (length(image_data) <= 2097152),
    CONSTRAINT request_image_not_empty       CHECK (length(image_data) > 0)
);
