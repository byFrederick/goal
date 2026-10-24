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
-- Deletes the lease the holder names and records it as a lease span with its outcome and the
-- item's rework count before the transition (the item row is not updated yet). It never ends
-- before it was acquired, whatever the caller's clock says. No row: the lease was lost.
WITH gone AS (
  DELETE FROM leases l WHERE l.item_id = sqlc.arg(item_id)::uuid AND l.acquired_at = sqlc.arg(acquired_at)::timestamptz RETURNING l.*
)
INSERT INTO lease_spans (org_id, factory_id, item_id, station, agent, slot, rework_round, acquired_at, ended_at, outcome)
SELECT g.org_id, g.factory_id, g.item_id, g.station, g.agent, g.slot, w.rework_count, g.acquired_at,
       greatest(sqlc.arg(ended_at)::timestamptz, g.acquired_at), sqlc.arg(outcome)::text
FROM gone g JOIN work_items w ON w.id = g.item_id;

-- name: TakeLease :execrows
-- A person's action on an item in progress: deletes the item's lease whatever its acquired_at
-- and records it as a lease span with the outcome (taken). No row: the item holds no lease.
WITH gone AS (
  DELETE FROM leases l WHERE l.item_id = sqlc.arg(item_id)::uuid RETURNING l.*
)
INSERT INTO lease_spans (org_id, factory_id, item_id, station, agent, slot, rework_round, acquired_at, ended_at, outcome)
SELECT g.org_id, g.factory_id, g.item_id, g.station, g.agent, g.slot, w.rework_count, g.acquired_at,
       greatest(sqlc.arg(ended_at)::timestamptz, g.acquired_at), sqlc.arg(outcome)::text
FROM gone g JOIN work_items w ON w.id = g.item_id;

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
-- Deletes the leases, records each as a lease span that expired now, and returns their items to
-- the queue at the same station.
WITH gone AS (
  DELETE FROM leases WHERE item_id = ANY (sqlc.arg(item_ids)::uuid[]) RETURNING *
), spans AS (
  INSERT INTO lease_spans (org_id, factory_id, item_id, station, agent, slot, rework_round, acquired_at, ended_at, outcome)
  SELECT g.org_id, g.factory_id, g.item_id, g.station, g.agent, g.slot, w.rework_count, g.acquired_at,
         greatest(sqlc.arg(now)::timestamptz, g.acquired_at), 'expired'
  FROM gone g JOIN work_items w ON w.id = g.item_id
)
UPDATE work_items w SET state = 'queued', updated_at = sqlc.arg(now)
FROM gone
WHERE w.id = gone.item_id AND w.state = 'in_progress' AND w.station = gone.station;
