-- Checks (ADR 0040 decision 19): a checked firing's check, queued, leased, finished or expired,
-- and what the trigger's next check is given. Row-level security scopes each to the
-- transaction's organization.

-- name: LiveTriggerCheck :one
-- Whether the trigger has a check queued or running (trigger_firings_check_live_idx).
SELECT EXISTS (
  SELECT 1 FROM trigger_firings
  WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND check_state IN ('queued', 'running')
) AS live;

-- name: ClaimTriggerCheck :one
-- The factory's oldest queued check that no other transaction holds, now running under the lease.
UPDATE trigger_firings f
SET check_state = 'running', lease_holder = sqlc.arg(holder), lease_token = sqlc.arg(token), lease_until = sqlc.arg(until)
WHERE f.id = (
  SELECT q.id FROM trigger_firings q
  WHERE q.factory_id = sqlc.arg(factory_id) AND q.check_state = 'queued'
  ORDER BY q.at, q.id
  LIMIT 1
  FOR UPDATE SKIP LOCKED)
RETURNING f.id, f.factory_id, f.trigger, f.source, f.event_key, f.idempotency_key, f.dedupe, f.outcome, f.reason, f.item_id,
          f.missed, f.at, f.check_state, f.run_id, f.cost_usd::float8 AS cost_usd, f.decisions, f.lease_until;

-- name: HeartbeatTriggerCheck :execrows
-- Extends a running check's lease under its token; no row when the lease is lost.
UPDATE trigger_firings SET lease_until = sqlc.arg(until)
WHERE id = sqlc.arg(id) AND check_state = 'running' AND lease_token = sqlc.arg(token);

-- name: FinishTriggerCheck :one
-- Ends a running check under its token: done with its decisions, or failed with the reason; no
-- row when the lease is lost.
UPDATE trigger_firings
SET check_state = sqlc.arg(state), run_id = sqlc.narg(run_id), cost_usd = sqlc.arg(cost_usd)::float8, decisions = sqlc.narg(decisions),
    reason = sqlc.arg(reason), checked_at = sqlc.arg(at), lease_holder = '', lease_token = NULL, lease_until = NULL
WHERE id = sqlc.arg(id) AND check_state = 'running' AND lease_token = sqlc.arg(token)
RETURNING factory_id, trigger;

-- name: SaveTriggerState :exec
-- The state a done check saved for the trigger's next one.
INSERT INTO trigger_state (factory_id, trigger, state, updated_at)
VALUES (sqlc.arg(factory_id), sqlc.arg(trigger), sqlc.arg(state), sqlc.arg(updated_at))
ON CONFLICT (org_id, factory_id, trigger) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at;

-- name: ExpireTriggerChecks :execrows
-- Fails the factory's running checks whose lease ended by now.
UPDATE trigger_firings
SET check_state = 'failed', reason = sqlc.arg(reason), checked_at = sqlc.arg(now), lease_holder = '', lease_token = NULL, lease_until = NULL
WHERE factory_id = sqlc.arg(factory_id) AND check_state = 'running' AND lease_until <= sqlc.arg(now);

-- name: TriggerState :one
SELECT state FROM trigger_state WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger);

-- name: LastCheckDecisions :one
-- The trigger's newest done check's decisions (trigger_firings_check_done_idx).
SELECT decisions FROM trigger_firings
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND check_state = 'done'
ORDER BY checked_at DESC, id DESC
LIMIT 1;

-- name: CheckOpenItems :many
-- The trigger's open items, newest first, each with the dedupe value of the firing that filed it
-- and the text of its newest note.
SELECT w.number, w.title, w.state,
       coalesce((SELECT f.dedupe FROM trigger_firings f WHERE f.item_id = w.id ORDER BY f.at LIMIT 1), '')::text AS key,
       coalesce((SELECT e.data->>'text' FROM item_events e WHERE e.item_id = w.id AND e.kind IN ('note', 'item_note')
                 ORDER BY e.id DESC LIMIT 1), '')::text AS last_note
FROM work_items w
WHERE w.factory_id = sqlc.arg(factory_id) AND w.trigger = sqlc.arg(trigger) AND w.state NOT IN ('done', 'cancelled')
ORDER BY w.created_at DESC, w.id DESC
LIMIT sqlc.arg(max_items);

-- name: CheckClosedItems :many
-- The trigger's items that closed at or after since, newest first, each with its key and the
-- reason of its closing event.
SELECT w.number, w.state, coalesce(w.done_at, w.updated_at)::timestamptz AS closed_at,
       coalesce((SELECT f.dedupe FROM trigger_firings f WHERE f.item_id = w.id ORDER BY f.at LIMIT 1), '')::text AS key,
       coalesce((SELECT e.data->>'reason' FROM item_events e WHERE e.item_id = w.id AND e.kind IN ('done', 'cancelled')
                 ORDER BY e.id DESC LIMIT 1), '')::text AS why
FROM work_items w
WHERE w.factory_id = sqlc.arg(factory_id) AND w.trigger = sqlc.arg(trigger) AND w.state IN ('done', 'cancelled')
  AND coalesce(w.done_at, w.updated_at) >= sqlc.arg(since)
ORDER BY closed_at DESC, w.id DESC
LIMIT sqlc.arg(max_items);
