-- Queries used by goald, compiled to Go with sqlc (pgx/v5).
--
-- Lock order, to rule out deadlocks: a work item before its lease or runs. Locks on items use
-- FOR NO KEY UPDATE so foreign-key checks from concurrent event inserts never wait on them.
-- Go ints are passed as bigint: PostgreSQL rejects a value out of a column's range instead of
-- letting it wrap.

-- name: NextItemNumber :one
UPDATE factories SET last_item_number = last_item_number + 1 WHERE id = $1 RETURNING last_item_number;

-- name: InsertItem :one
INSERT INTO work_items (
  id, factory_id, number, parent_id, type, lane, title, body, station, state, class_of_service,
  priority, created_at, updated_at
) VALUES (
  sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(number), sqlc.narg(parent_id), sqlc.arg(type),
  sqlc.narg(lane), sqlc.arg(title), sqlc.arg(body), sqlc.arg(station), sqlc.arg(state),
  sqlc.arg(class_of_service), sqlc.arg(priority)::bigint, sqlc.arg(created_at), sqlc.arg(created_at)
)
RETURNING *;

-- name: GetItem :one
SELECT * FROM work_items WHERE id = $1;

-- name: LockItem :one
SELECT * FROM work_items WHERE id = $1 FOR NO KEY UPDATE;

-- name: WaitingItems :many
-- Pull order: expedite first, then priority (highest first), then age, then number.
SELECT * FROM work_items
WHERE factory_id = $1 AND station = $2 AND state IN ('queued', 'ready')
ORDER BY (class_of_service = 'expedite') DESC, priority DESC, created_at, number
LIMIT sqlc.arg(max_items)::bigint;

-- name: LockNextWaitingItem :one
-- Pull, never push: one agent slot takes the next item at its station. SKIP LOCKED lets many
-- slots claim concurrently without blocking each other or taking the same item. An item is
-- skipped while any item it needs is neither done nor cancelled (item_edges, by its primary key).
-- WaitingItems lists it all the same: only queue stations read that, and they hold nothing.
SELECT * FROM work_items w
WHERE w.factory_id = $1 AND w.station = $2 AND w.state IN ('queued', 'ready')
  AND NOT EXISTS (
    SELECT 1 FROM item_edges e JOIN work_items n ON n.id = e.needs_id
    WHERE e.item_id = w.id AND n.state NOT IN ('done', 'cancelled')
  )
ORDER BY (w.class_of_service = 'expedite') DESC, w.priority DESC, w.created_at, w.number
LIMIT 1
FOR NO KEY UPDATE OF w SKIP LOCKED;

-- name: StartItem :one
UPDATE work_items SET state = 'in_progress', updated_at = $2 WHERE id = $1 RETURNING *;

-- name: MoveItem :one
UPDATE work_items SET
  station = sqlc.arg(station),
  state = sqlc.arg(state),
  updated_at = sqlc.arg(updated_at),
  lane = coalesce(sqlc.narg(lane), lane),
  paths = coalesce(sqlc.narg(paths)::text[], paths),
  outputs = CASE
    WHEN sqlc.narg(output)::jsonb IS NULL THEN outputs
    ELSE outputs || jsonb_build_object(sqlc.arg(output_station)::text, sqlc.narg(output)::jsonb)
  END,
  rework_count = rework_count + sqlc.arg(rework)::integer,
  done_at = CASE WHEN sqlc.arg(set_done_at)::boolean THEN sqlc.narg(done_at) ELSE done_at END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: AddItemCost :exec
UPDATE work_items SET cost_usd = cost_usd + $2 WHERE id = $1;

-- name: GetItemByNumber :one
SELECT * FROM work_items WHERE factory_id = $1 AND number = $2;

-- name: ListItems :many
-- Newest first, paged by number: the unique (factory_id, number) index serves the order.
-- Empty states and station and a non-positive before mean no filter.
SELECT * FROM work_items
WHERE factory_id = sqlc.arg(factory_id)
  AND (cardinality(sqlc.arg(states)::text[]) = 0 OR state = ANY (sqlc.arg(states)::text[]))
  AND (sqlc.arg(station)::text = '' OR station = sqlc.arg(station)::text)
  AND (sqlc.arg(before)::bigint <= 0 OR number < sqlc.arg(before)::bigint)
ORDER BY number DESC
LIMIT sqlc.arg(max_items)::bigint;

-- name: StationLoads :many
-- The open items of a factory by station: waiting to be pulled, in progress, waiting for a
-- person's decision. (The station_wip view counts only the first two.)
SELECT station,
       count(*) FILTER (WHERE state IN ('queued', 'ready')) AS waiting,
       count(*) FILTER (WHERE state = 'in_progress')        AS in_progress,
       count(*) FILTER (WHERE state = 'waiting_human')      AS waiting_human
FROM work_items
WHERE factory_id = $1 AND state NOT IN ('done', 'cancelled')
GROUP BY station;

-- name: AssemblyParts :many
-- The parts of those of the factory's items among ids that have any (work_items_parent_id_idx),
-- each with its assembly's number and title, by assembly then part number. A part is a child
-- with a part_key, as Parts counts them (ADR 0012).
SELECT a.id AS assembly_id, a.number AS assembly_number, a.title AS assembly_title,
       p.id, p.number, p.station, p.state
FROM work_items p
JOIN work_items a ON a.id = p.parent_id
WHERE p.parent_id = ANY (sqlc.arg(ids)::uuid[]) AND p.part_key IS NOT NULL AND a.factory_id = sqlc.arg(factory_id)
ORDER BY a.number, p.number;
