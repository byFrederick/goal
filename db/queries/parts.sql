-- Assemblies and their parts (ports.Store Assemble, Parts; migration 00059). Lock order: the
-- assembly's row before its parts' inserts in Assemble; a part's row before its assembly's when
-- a part closes (Transition), and parts are created only under their assembly's lock, so the two
-- never wait on each other's rows in opposite orders. AddPart locks the factory's row, then the
-- assembly's, the order of AddEdge (item_edges.sql).

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

-- name: SetIntegrationBranch :execrows
-- Sets an assembly's integration branch and landing station once: a row that has a branch keeps
-- it, so a second call changes nothing (no rows).
UPDATE work_items SET
  integration_branch = sqlc.arg(integration_branch)::text,
  integration_land_at = sqlc.arg(integration_land_at)::text,
  updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND integration_branch IS NULL;
