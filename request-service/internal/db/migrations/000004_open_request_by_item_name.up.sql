-- Serves the lookup that turns a second request for the same item into a join.
--
-- Creating a request now first asks whether an open one already exists under the same
-- name, so that lookup runs on the write path of every create and has to be an index
-- hit. It is expressed exactly as the query is - lower(btrim(item_name)), restricted to
-- OPEN - because a functional index is only used when it matches the expression, and a
-- partial one keeps closed and cancelled demand out of a structure nothing reads.
CREATE INDEX idx_purchase_request_open_item_name
    ON purchase_request (lower(btrim(item_name)))
    WHERE status = 'OPEN';
