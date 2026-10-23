-- Queries used by goald, compiled to Go with sqlc (pgx/v5).

-- name: ClaimNextItem :one
-- Pull, never push: one agent slot takes the next item at its station. SKIP LOCKED lets many
-- slots claim concurrently without blocking each other or taking the same item.
WITH next AS (
  SELECT id
  FROM work_items
  WHERE factory_id = $1 AND station = $2 AND state IN ('queued', 'ready')
  ORDER BY (class_of_service = 'expedite') DESC, priority DESC, created_at
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
UPDATE work_items w
SET state = 'in_progress', updated_at = now()
FROM next
WHERE w.id = next.id
RETURNING w.*;

-- name: ExpiredLeases :many
SELECT item_id, factory_id, station FROM leases WHERE expires_at < now() FOR UPDATE SKIP LOCKED;

-- name: StationWIP :many
SELECT station, in_progress, waiting, oldest_waiting_since FROM station_wip WHERE factory_id = $1;
