-- name: SlotTaken :one
SELECT EXISTS (
  SELECT 1 FROM leases
  WHERE factory_id = sqlc.arg(factory_id) AND station = sqlc.arg(station) AND slot = sqlc.arg(slot)::bigint
);

-- name: InsertLease :one
-- The unique (factory_id, station, slot) index rejects a second lease on a slot; that is what
-- keeps WIP at or under capacity across instances.
INSERT INTO leases (item_id, factory_id, station, agent, slot, acquired_at, heartbeat_at, expires_at)
VALUES (sqlc.arg(item_id), sqlc.arg(factory_id), sqlc.arg(station), sqlc.arg(agent), sqlc.arg(slot)::bigint,
        sqlc.arg(acquired_at), sqlc.arg(acquired_at), sqlc.arg(expires_at))
ON CONFLICT (item_id) DO UPDATE SET
  factory_id = EXCLUDED.factory_id, station = EXCLUDED.station, agent = EXCLUDED.agent, slot = EXCLUDED.slot,
  acquired_at = EXCLUDED.acquired_at, heartbeat_at = EXCLUDED.heartbeat_at, expires_at = EXCLUDED.expires_at
RETURNING *;

-- name: StationLeases :many
SELECT * FROM leases WHERE factory_id = $1 AND station = $2 ORDER BY slot;

-- name: HeartbeatLease :one
-- acquired_at fences the lease: a holder whose claim was requeued and re-claimed matches nothing.
UPDATE leases SET heartbeat_at = sqlc.arg(heartbeat_at), expires_at = sqlc.arg(expires_at)
WHERE item_id = sqlc.arg(item_id) AND acquired_at = sqlc.arg(acquired_at)
RETURNING *;

-- name: ReleaseLease :execrows
DELETE FROM leases WHERE item_id = $1 AND acquired_at = $2;

-- name: LockExpiredLeases :many
-- Skips leases whose item another transaction holds (a transition in flight); the next sweep
-- sees them again if they are still expired.
SELECT l.* FROM leases l JOIN work_items w ON w.id = l.item_id
WHERE l.expires_at <= sqlc.arg(now)
ORDER BY l.expires_at
LIMIT sqlc.arg(max_leases)::bigint
FOR UPDATE OF l SKIP LOCKED
FOR NO KEY UPDATE OF w SKIP LOCKED;

-- name: RequeueLeases :exec
-- Deletes the leases and returns their items to the queue at the same station.
WITH gone AS (
  DELETE FROM leases WHERE item_id = ANY (sqlc.arg(item_ids)::uuid[]) RETURNING item_id, station
)
UPDATE work_items w SET state = 'queued', updated_at = sqlc.arg(now)
FROM gone
WHERE w.id = gone.item_id AND w.state = 'in_progress' AND w.station = gone.station;
