-- name: InsertRun :exec
-- A run records the definition version it starts on; NULL only for runs from before versions.
INSERT INTO agent_runs (id, factory_id, definition_version_id, item_id, station, agent, model, status, started_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.narg(definition_version_id), sqlc.arg(item_id), sqlc.arg(station),
        sqlc.arg(agent), sqlc.arg(model), 'running', sqlc.arg(started_at));

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

-- name: FactorySpendSince :one
-- The cost of the runs a factory started at or after since, for its weekly budget. Row-level
-- security scopes it to the organization; agent_runs_factory_started_idx serves the range.
SELECT coalesce(sum(cost_usd), 0)::float8 AS spend_usd
FROM agent_runs
WHERE factory_id = sqlc.arg(factory_id) AND started_at >= sqlc.arg(since);

-- name: ItemRuns :many
-- An item's runs, oldest first, on agent_runs_item_id_idx; row-level security scopes it to the
-- organization.
SELECT * FROM agent_runs
WHERE item_id = sqlc.arg(item_id)
ORDER BY started_at, id
LIMIT sqlc.arg(max_runs)::bigint;

-- name: GetRun :one
SELECT * FROM agent_runs WHERE id = $1;
