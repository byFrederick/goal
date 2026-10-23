-- name: InsertEvents :copyfrom
-- COPY fires the item_events_notify trigger per row, like INSERT.
INSERT INTO item_events (factory_id, item_id, run_id, at, kind, station, actor, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ItemEvents :many
-- One page of an item's history in append order, after a known event id (0 for the start).
SELECT * FROM item_events
WHERE item_id = $1 AND id > sqlc.arg(after)::bigint
ORDER BY id
LIMIT sqlc.arg(max_events)::bigint;
