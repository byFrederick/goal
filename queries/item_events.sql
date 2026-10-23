-- name: InsertEvents :copyfrom
-- COPY fires the item_events_notify trigger per row, like INSERT.
INSERT INTO item_events (factory_id, item_id, run_id, at, kind, station, actor, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
