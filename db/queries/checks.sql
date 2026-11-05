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
          f.missed, f.at, f.check_state, f.run_id, f.cost_usd::float8 AS cost_usd, f.decisions, f.try, f.lease_until;

-- name: HeartbeatTriggerCheck :execrows
-- Extends a running check's lease under its token; no row when the lease is lost.
UPDATE trigger_firings SET lease_until = sqlc.arg(until)
WHERE id = sqlc.arg(id) AND check_state = 'running' AND lease_token = sqlc.arg(token);

-- name: FinishTriggerCheck :one
-- Ends a running check under its token: done with its decisions, or failed with the reason; no
-- row when the lease is lost.
UPDATE trigger_firings
SET check_state = sqlc.arg(state), run_id = sqlc.narg(run_id), cost_usd = sqlc.arg(cost_usd)::float8, decisions = sqlc.narg(decisions),
    applied = sqlc.narg(applied), reason = sqlc.arg(reason), checked_at = sqlc.arg(at), lease_holder = '', lease_token = NULL, lease_until = NULL
WHERE id = sqlc.arg(id) AND check_state = 'running' AND lease_token = sqlc.arg(token)
RETURNING factory_id, trigger;

-- name: RunningTriggerCheck :one
-- A running check under its token, its row locked: its factory, trigger and whether it is a try.
SELECT factory_id, trigger, try FROM trigger_firings
WHERE id = sqlc.arg(id) AND check_state = 'running' AND lease_token = sqlc.arg(token)
FOR UPDATE;

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
-- The trigger's newest done check's decisions, tries left out (trigger_firings_check_done_idx).
SELECT decisions FROM trigger_firings
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND check_state = 'done' AND NOT try
ORDER BY checked_at DESC, id DESC
LIMIT 1;

-- name: CheckOpenItems :many
-- The trigger's open items, newest first, each with its key (its trigger_keys row's, else the
-- dedupe value of the firing that filed it) and the text of its newest note.
SELECT w.number, w.title, w.state,
       coalesce((SELECT k.key FROM trigger_keys k WHERE k.item_id = w.id LIMIT 1),
                (SELECT f.dedupe FROM trigger_firings f WHERE f.item_id = w.id ORDER BY f.at LIMIT 1), '')::text AS key,
       coalesce((SELECT e.data->>'text' FROM item_events e WHERE e.item_id = w.id AND e.kind IN ('note', 'item_note')
                 ORDER BY e.id DESC LIMIT 1), '')::text AS last_note
FROM work_items w
WHERE w.factory_id = sqlc.arg(factory_id) AND w.trigger = sqlc.arg(trigger) AND w.state NOT IN ('done', 'cancelled')
ORDER BY w.created_at DESC, w.id DESC
LIMIT sqlc.arg(max_items);

-- name: CheckClosedItems :many
-- The trigger's items that closed at or after since, newest first, each with its key (as
-- CheckOpenItems reads it) and the reason of its closing event.
SELECT w.number, w.state, coalesce(w.done_at, w.updated_at)::timestamptz AS closed_at,
       coalesce((SELECT k.key FROM trigger_keys k WHERE k.item_id = w.id LIMIT 1),
                (SELECT f.dedupe FROM trigger_firings f WHERE f.item_id = w.id ORDER BY f.at LIMIT 1), '')::text AS key,
       coalesce((SELECT e.data->>'reason' FROM item_events e WHERE e.item_id = w.id AND e.kind IN ('done', 'cancelled')
                 ORDER BY e.id DESC LIMIT 1), '')::text AS why
FROM work_items w
WHERE w.factory_id = sqlc.arg(factory_id) AND w.trigger = sqlc.arg(trigger) AND w.state IN ('done', 'cancelled')
  AND coalesce(w.done_at, w.updated_at) >= sqlc.arg(since)
ORDER BY closed_at DESC, w.id DESC
LIMIT sqlc.arg(max_items);

-- name: TriggerKeysOf :many
-- The trigger's keys among keys, each with its last item and last note.
SELECT key, item_id, last_note_at, last_note_text FROM trigger_keys
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND key = ANY(sqlc.arg(keys)::text[]);

-- name: LockCheckItems :many
-- The keys' items, locked in id order, each with how it stands: its number, station and state,
-- when it closed and the reason of its closing event.
SELECT w.id, w.number, w.station, w.state, coalesce(w.done_at, w.updated_at)::timestamptz AS closed_at,
       coalesce((SELECT e.data->>'reason' FROM item_events e WHERE e.item_id = w.id AND e.kind IN ('done', 'cancelled')
                 ORDER BY e.id DESC LIMIT 1), '')::text AS why
FROM work_items w
WHERE w.id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY w.id
FOR UPDATE OF w;

-- name: CheckFiledSince :one
-- How many items the trigger filed at or after since: a check's perDay.
SELECT count(*) FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND origin = 'trigger' AND created_at >= sqlc.arg(since);

-- name: KeepTriggerKey :exec
-- The key's last item is the one just filed, with no note yet.
INSERT INTO trigger_keys (factory_id, trigger, key, item_id)
VALUES (sqlc.arg(factory_id), sqlc.arg(trigger), sqlc.arg(key), sqlc.arg(item_id))
ON CONFLICT (org_id, factory_id, trigger, key) DO UPDATE SET item_id = excluded.item_id, last_note_at = NULL, last_note_text = '';

-- name: NoteTriggerKey :exec
-- The key's last note.
UPDATE trigger_keys SET last_note_at = sqlc.arg(at), last_note_text = sqlc.arg(text)
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND key = sqlc.arg(key);

-- name: TriggerChecks :many
-- The trigger's checked firings, tries among them, newest first.
SELECT id, factory_id, trigger, source, event_key, idempotency_key, dedupe, outcome, reason, item_id, missed, at,
       check_state, run_id, cost_usd::float8 AS cost_usd, decisions, try, applied, checked_at
FROM trigger_firings
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND outcome = 'checked'
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(max_rows);

-- name: FailingTriggerChecks :many
-- The factories' triggers whose last three finished checks, tries left out, all failed, with the
-- newest one's time and reason (trigger_firings_check_finished_idx).
SELECT factory_id, trigger, max(checked_at)::timestamptz AS at,
       (array_agg(reason ORDER BY checked_at DESC, id DESC))[1]::text AS reason
FROM (
  SELECT factory_id, trigger, check_state, checked_at, reason, id,
         row_number() OVER (PARTITION BY factory_id, trigger ORDER BY checked_at DESC, id DESC) AS n
  FROM trigger_firings
  WHERE factory_id = ANY(sqlc.arg(factories)::uuid[]) AND check_state IN ('done', 'failed') AND NOT try
) last
WHERE n <= 3
GROUP BY factory_id, trigger
HAVING count(*) = 3 AND bool_and(check_state = 'failed')
ORDER BY factory_id, trigger;

-- name: LimitedTriggerChecks :many
-- The factories' triggers with a done check at or after since, tries left out, that skipped
-- something over the limit, with the first such check's time.
SELECT factory_id, trigger, min(checked_at)::timestamptz AS at
FROM trigger_firings
WHERE factory_id = ANY(sqlc.arg(factories)::uuid[]) AND check_state = 'done' AND NOT try AND checked_at >= sqlc.arg(since)
  AND applied->'skipped' @> '[{"reason": "over the limit"}]'::jsonb
GROUP BY factory_id, trigger
ORDER BY factory_id, trigger;

-- name: CheckOpenCount :one
-- How many of the trigger's items are open, as CheckOpenItems reads them.
SELECT count(*) FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND state NOT IN ('done', 'cancelled');

-- name: CheckClosedCount :one
-- How many of the trigger's items closed at or after since, as CheckClosedItems reads them.
SELECT count(*) FROM work_items
WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger) AND state IN ('done', 'cancelled')
  AND coalesce(done_at, updated_at) >= sqlc.arg(since);

-- name: TriggerStateAt :one
-- The trigger's saved state and when the check that saved it finished.
SELECT state, updated_at FROM trigger_state WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger);

-- name: ClearTriggerState :exec
-- Deletes the trigger's saved state; its trigger_keys rows stay.
DELETE FROM trigger_state WHERE factory_id = sqlc.arg(factory_id) AND trigger = sqlc.arg(trigger);
