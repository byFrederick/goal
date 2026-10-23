-- name: InsertRun :exec
INSERT INTO agent_runs (id, factory_id, item_id, station, agent, model, status, started_at)
VALUES ($1, $2, $3, $4, $5, $6, 'running', $7);

-- name: RunItem :one
SELECT item_id FROM agent_runs WHERE id = $1;

-- name: FinishRun :one
-- Only a running run finishes; finishing it again matches nothing.
UPDATE agent_runs SET
  status = sqlc.arg(status), session_id = sqlc.narg(session_id), turns = sqlc.arg(turns)::bigint,
  input_tokens = sqlc.arg(input_tokens), output_tokens = sqlc.arg(output_tokens),
  cost_usd = sqlc.arg(cost_usd), error = sqlc.narg(error), ended_at = sqlc.arg(ended_at)
WHERE id = sqlc.arg(id) AND status = 'running'
RETURNING factory_id, item_id, station, agent;

-- name: FailRunningRuns :exec
UPDATE agent_runs SET status = 'failed', error = sqlc.arg(error), ended_at = sqlc.arg(ended_at)
WHERE item_id = ANY (sqlc.arg(item_ids)::uuid[]) AND status = 'running';

-- name: Spend :one
-- The cost of the runs a factory started in [since, until).
SELECT coalesce(sum(cost_usd), 0)::float8 AS spend_usd
FROM agent_runs
WHERE factory_id = sqlc.arg(factory_id) AND started_at >= sqlc.arg(since) AND started_at < sqlc.arg(until);
