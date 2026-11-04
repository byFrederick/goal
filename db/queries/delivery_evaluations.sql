-- Delivery evaluations (00120, ADR 0040 decision 13). Row-level security scopes every query to
-- the organization.

-- name: InsertDeliveryEvaluation :exec
-- A pending row has no outcome; one skipped at once is done with its reason.
INSERT INTO delivery_evaluations (
  id, delivery_row, factory_id, triggers, state, next_attempt_at, outcome, reason, created_at, updated_at
) VALUES (
  sqlc.arg(id), sqlc.arg(delivery_row), sqlc.arg(factory_id), sqlc.arg(triggers)::text[], sqlc.arg(state),
  sqlc.arg(at), sqlc.narg(outcome), sqlc.arg(reason), sqlc.arg(at), sqlc.arg(at)
);

-- name: ClaimDeliveryEvaluations :many
-- At most max of the organization's pending rows due at now (next_attempt_at at or before it, no
-- lease live at it), soonest first, that no other transaction holds, each leased to holder until
-- until with one more attempt, with its delivery's webhook, id, event and body.
WITH due AS (
  SELECT e.id FROM delivery_evaluations e
  WHERE e.state = 'pending' AND e.next_attempt_at <= sqlc.arg(now)::timestamptz
    AND (e.lease_until IS NULL OR e.lease_until <= sqlc.arg(now)::timestamptz)
  ORDER BY e.next_attempt_at, e.id
  LIMIT sqlc.arg(max)::int
  FOR UPDATE SKIP LOCKED
),
claimed AS (
  UPDATE delivery_evaluations e
  SET lease_holder = sqlc.arg(holder)::text, lease_until = sqlc.arg(until)::timestamptz, attempts = e.attempts + 1,
      updated_at = sqlc.arg(now)::timestamptz
  FROM due WHERE e.id = due.id
  RETURNING e.*
)
SELECT c.id, c.delivery_row, c.factory_id, c.triggers, c.state, c.attempts, c.next_attempt_at, c.lease_holder, c.lease_until,
       c.created_at, d.endpoint_id, coalesce(d.delivery_id, '')::text AS delivery_id, d.event, d.body
FROM claimed c
JOIN webhook_deliveries d ON d.id = c.delivery_row
ORDER BY c.next_attempt_at, c.id;

-- name: FinishDeliveryEvaluation :execrows
-- Ends the claim of row id by holder at its attempts-th attempt: done or failed with its outcome,
-- or pending again at next_attempt_at; the lease is released. No row when a later claim took it.
UPDATE delivery_evaluations
SET state = sqlc.arg(state)::text, outcome = sqlc.narg(outcome)::text, reason = sqlc.arg(reason)::text,
    item_id = sqlc.narg(item_id), next_attempt_at = coalesce(sqlc.narg(next_attempt_at)::timestamptz, next_attempt_at),
    lease_holder = NULL, lease_until = NULL, updated_at = sqlc.arg(at)::timestamptz
WHERE id = sqlc.arg(id) AND state = 'pending' AND lease_holder = sqlc.arg(holder)::text AND attempts = sqlc.arg(attempts)::int;

-- name: DeliveryEvaluations :many
-- A delivery's evaluations, by factory.
SELECT e.*, d.endpoint_id, coalesce(d.delivery_id, '')::text AS delivery_id, d.event
FROM delivery_evaluations e
JOIN webhook_deliveries d ON d.id = e.delivery_row
WHERE e.delivery_row = sqlc.arg(delivery_row)
ORDER BY e.factory_id;

-- name: LockTriggerFiring :exec
-- Serializes the firings of one trigger of a factory (ADR 0040 decision 16), so perDay, perHour
-- and dedupe hold across goald instances: taken first in the firing's transaction, per schema as
-- the schedule lock is, so test schemas don't block each other.
SELECT pg_advisory_xact_lock(hashtextextended(
  'goal.trigger:' || current_schema() || ':' || sqlc.arg(factory_id)::uuid::text || ':' || sqlc.arg(trigger)::text, 0));
