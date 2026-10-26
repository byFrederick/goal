-- Item dependencies (ports.Store AddEdge, RemoveEdge, Edges, OpenNeeds; migration 00058). Lock order: the
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

-- name: OpenNeeds :many
-- The needs of those of the factory's items among ids whose needed item is neither done nor
-- cancelled, each with that item's number and paths, by item then oldest edge first (the
-- primary key). Both ends of an edge are of one factory, so filtering the needed item is enough.
SELECT e.item_id, e.kind, n.id AS needs_id, n.number, n.paths
FROM item_edges e
JOIN work_items n ON n.id = e.needs_id
WHERE e.item_id = ANY (sqlc.arg(ids)::uuid[]) AND n.factory_id = sqlc.arg(factory_id)
  AND n.state NOT IN ('done', 'cancelled')
ORDER BY e.item_id, e.created_at, e.needs_id;
