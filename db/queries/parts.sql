-- Assemblies and their parts (ports.Store Assemble, Parts; migration 00059). Lock order: the
-- assembly's row before its parts' inserts in Assemble; a part's row before its assembly's when
-- a part closes (Transition), and parts are created only under their assembly's lock, so the two
-- never wait on each other's rows in opposite orders. AddPart locks the factory's row, then the
-- assembly's, the order of AddEdge (item_edges.sql).

-- name: InsertPart :one
INSERT INTO work_items (
  id, factory_id, number, parent_id, part_key, part_paths, paths, type, lane, title, body, station,
  state, class_of_service, priority, created_at, updated_at
) VALUES (
  sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(number), sqlc.arg(parent_id)::uuid, sqlc.arg(part_key)::text,
  sqlc.arg(part_paths)::text[], sqlc.arg(part_paths)::text[], sqlc.arg(type), sqlc.narg(lane),
  sqlc.arg(title), sqlc.arg(body), sqlc.arg(station), 'queued', sqlc.arg(class_of_service),
  sqlc.arg(priority)::bigint, sqlc.arg(created_at), sqlc.arg(created_at)
)
RETURNING *;

-- name: ItemParts :many
-- An assembly's parts in plan order: they are numbered in that order (work_items_parent_part_key_idx).
SELECT * FROM work_items WHERE parent_id = sqlc.arg(parent_id)::uuid AND part_key IS NOT NULL ORDER BY number;

-- name: PartCounts :one
-- How many of an assembly's parts are still open, done, and in all.
SELECT count(*) FILTER (WHERE state NOT IN ('done', 'cancelled')) AS open,
       count(*) FILTER (WHERE state = 'done')                      AS done,
       count(*)                                                    AS total
FROM work_items WHERE parent_id = sqlc.arg(parent_id)::uuid AND part_key IS NOT NULL;

-- name: LastDonePartStation :one
-- The station of an assembly's most recently done part, the line's done station: where a done
-- assembly closes (migration 00062 backfills with the same order).
SELECT station FROM work_items
WHERE parent_id = sqlc.arg(parent_id)::uuid AND part_key IS NOT NULL AND state = 'done'
ORDER BY done_at DESC NULLS LAST, number DESC
LIMIT 1;
