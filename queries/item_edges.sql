-- Item dependencies (ports.Store AddEdge, RemoveEdge, Edges; migration 00058). Lock order: the
-- factory row (LockFactory) before anything of its items, so edges of one factory are added one
-- at a time and the cycle check of each sees the edges committed before it.

-- name: InsertEdge :one
INSERT INTO item_edges (item_id, needs_id, kind, created_by, created_at)
VALUES (sqlc.arg(item_id), sqlc.arg(needs_id), sqlc.arg(kind), sqlc.arg(created_by), sqlc.arg(created_at))
RETURNING *;

-- name: EdgeReaches :one
-- Whether from needs to, directly or through other items; the walk follows the primary key.
WITH RECURSIVE reach AS (
  SELECT e.needs_id FROM item_edges e WHERE e.item_id = sqlc.arg(from_id)
  UNION
  SELECT e.needs_id FROM item_edges e JOIN reach r ON e.item_id = r.needs_id
)
SELECT count(*) > 0 AS reaches FROM reach WHERE needs_id = sqlc.arg(to_id)::uuid;

-- name: DeleteEdge :one
DELETE FROM item_edges WHERE item_id = $1 AND needs_id = $2 RETURNING *;

-- name: ItemNeeds :many
-- What the item needs, oldest first.
SELECT * FROM item_edges WHERE item_id = $1 ORDER BY created_at, needs_id;

-- name: ItemNeededBy :many
-- What needs the item, oldest first (item_edges_org_needs_idx).
SELECT * FROM item_edges WHERE needs_id = $1 ORDER BY created_at, item_id;
