-- name: InsertEvents :batchexec
-- One INSERT per event, sent as one batch in order (COPY is refused under row-level security).
-- org_id comes from its default, the transaction's organization.
INSERT INTO item_events (factory_id, item_id, run_id, at, kind, station, actor, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ItemEvents :many
-- One page of an item's history in append order, after a known event id (0 for the start).
SELECT * FROM item_events
WHERE item_id = $1 AND id > sqlc.arg(after)::bigint
ORDER BY id
LIMIT sqlc.arg(max_events)::bigint;

-- name: InsertNote :one
-- A person's note, returned with its id.
INSERT INTO item_events (factory_id, item_id, run_id, at, kind, station, actor, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: NotesSinceLastRun :many
-- An item's notes after its latest run_started event, both on item_events_item_idx: what the
-- next run's brief carries.
SELECT n.* FROM item_events n
WHERE n.item_id = sqlc.arg(item_id) AND n.kind = 'note'
  AND n.id > coalesce((SELECT max(r.id) FROM item_events r
                       WHERE r.item_id = sqlc.arg(item_id) AND r.kind = 'run_started'), 0)
ORDER BY n.id;
