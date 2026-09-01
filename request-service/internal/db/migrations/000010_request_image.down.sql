DROP TABLE IF EXISTS request_image;

ALTER TABLE purchase_request
    DROP CONSTRAINT IF EXISTS purchase_request_image_type_valid;

ALTER TABLE purchase_request
    DROP COLUMN IF EXISTS image_type;
