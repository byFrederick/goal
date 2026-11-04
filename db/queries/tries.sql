-- Tries of past items (00101, ADR 0039 decisions 9 and 11). Row-level security scopes every query
-- to the organization.

-- name: LockProposal :one
-- The proposal whose tries are queued, locked so two sweeps queue its tries once.
SELECT id, number FROM jonah_proposals WHERE factory_id = sqlc.arg(factory_id) AND id = sqlc.arg(id) FOR UPDATE;

-- name: ProposalHasTries :one
SELECT EXISTS (SELECT 1 FROM tries WHERE proposal_id = sqlc.arg(proposal_id))::bool;

-- name: InsertTry :one
INSERT INTO tries (id, factory_id, proposal_id, item_id, station, original_run_id, model, created_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(proposal_id), sqlc.arg(item_id), sqlc.arg(station),
        sqlc.arg(original_run_id), sqlc.arg(model), sqlc.arg(created_at))
RETURNING *;

-- name: ProposalTries :many
SELECT sqlc.embed(t), p.number AS proposal_number FROM tries t
JOIN jonah_proposals p ON p.id = t.proposal_id
WHERE t.proposal_id = sqlc.arg(proposal_id)
ORDER BY t.created_at, t.id;

-- name: TryByID :one
SELECT sqlc.embed(t), p.number AS proposal_number FROM tries t
JOIN jonah_proposals p ON p.id = t.proposal_id
WHERE t.id = sqlc.arg(id);

-- name: LockFactoryTries :exec
-- Serializes the factory's claims, so two claims never both see a free place under the cap.
SELECT pg_advisory_xact_lock(hashtextextended('goal.tries:' || sqlc.arg(factory_id)::uuid::text, 0));

-- name: ClaimTry :one
-- The factory's oldest queued try, or running one whose lease ended by now, that no other
-- transaction holds, while fewer than max_running of its tries run under a live lease: running
-- until until, one more attempt (tries_due_idx).
UPDATE tries t SET state = 'running', lease_until = sqlc.arg(until)::timestamptz, attempts = t.attempts + 1
WHERE t.id = (
  SELECT d.id FROM tries d
  WHERE d.factory_id = sqlc.arg(factory_id)
    AND (d.state = 'queued' OR (d.state = 'running' AND d.lease_until <= sqlc.arg(now)::timestamptz))
    AND (SELECT count(*) FROM tries l
         WHERE l.factory_id = sqlc.arg(factory_id) AND l.state = 'running' AND l.lease_until > sqlc.arg(now)::timestamptz)
        < sqlc.arg(max_running)::bigint
  ORDER BY d.created_at, d.id
  LIMIT 1
  FOR UPDATE SKIP LOCKED)
RETURNING t.id;

-- name: FinishTry :one
-- Ends a running try under the lease its claim returned. No row when the lease is lost.
UPDATE tries
SET state = 'done', verdict = sqlc.narg(verdict), error = sqlc.arg(error), diff = sqlc.arg(diff), cost_usd = sqlc.arg(cost_usd),
    try_run_id = sqlc.narg(try_run_id), finished_at = sqlc.arg(finished_at)::timestamptz, lease_until = NULL
WHERE id = sqlc.arg(id) AND state = 'running' AND lease_until = sqlc.arg(lease_until)::timestamptz
RETURNING id;

-- name: TryTarget :one
-- The item's newest finished station run at station that was a claim's first run: its branch
-- head is recorded (agent_runs_try_target_idx).
SELECT * FROM agent_runs
WHERE item_id = sqlc.arg(item_id) AND station = sqlc.arg(station) AND kind = 'station'
  AND branch_head IS NOT NULL AND status <> 'running'
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: RunByID :one
SELECT * FROM agent_runs WHERE id = sqlc.arg(id);

-- name: OutputsBefore :many
-- The item's latest output of each station among its runs that ended before before.
SELECT DISTINCT ON (station) station, output FROM agent_runs
WHERE item_id = sqlc.arg(item_id) AND ended_at < sqlc.arg(before)::timestamptz AND output IS NOT NULL
ORDER BY station, ended_at DESC, id DESC;
