-- name: InsertRun :exec
-- A run records the definition version it starts on; NULL only for runs from before versions.
-- kind is station for a line's run, jonah for a review's.
INSERT INTO agent_runs (id, factory_id, definition_version_id, item_id, station, agent, model, status, started_at, kind)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.narg(definition_version_id), sqlc.arg(item_id), sqlc.arg(station),
        sqlc.arg(agent), sqlc.arg(model), 'running', sqlc.arg(started_at), sqlc.arg(kind));

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

-- name: ListOrgRuns :many
-- The organization's run list: the runs of the factories that started in [since, until), newest
-- first, after the cursor (after_started_at, after_id) when paged. agent_runs_factory_started_idx
-- serves the range per factory; status and the pattern (already escaped, with \ as the escape
-- character, matched against the item's title, the agent and the model, or the item's number,
-- of number_of's factory when set)
-- filter within it. Row-level security scopes it to the organization.
SELECT sqlc.embed(r), w.number AS item_number, w.title AS item_title
FROM agent_runs r
JOIN work_items w ON w.id = r.item_id
WHERE r.factory_id = ANY (sqlc.arg(factories)::uuid[])
  AND r.started_at >= sqlc.arg(since) AND r.started_at < sqlc.arg(until)
  AND (sqlc.arg(status)::text = '' OR r.status = sqlc.arg(status)::text)
  AND (sqlc.arg(pattern)::text = ''
       OR w.title ILIKE '%' || sqlc.arg(pattern)::text || '%' ESCAPE '\'
       OR r.agent ILIKE '%' || sqlc.arg(pattern)::text || '%' ESCAPE '\'
       OR r.model ILIKE '%' || sqlc.arg(pattern)::text || '%' ESCAPE '\'
       OR (sqlc.arg(number)::bigint > 0 AND w.number = sqlc.arg(number)::bigint
           AND (sqlc.narg(number_of)::uuid IS NULL OR r.factory_id = sqlc.narg(number_of)::uuid)))
  AND (NOT sqlc.arg(paged)::boolean
       OR (r.started_at, r.id) < (sqlc.arg(after_started_at)::timestamptz, sqlc.arg(after_id)::uuid))
ORDER BY r.started_at DESC, r.id DESC
LIMIT sqlc.arg(max_runs)::bigint;

-- name: OrgRunsSummary :many
-- Each factory's runs that started in [since, until), whatever their status, for the run list's
-- summary; agent_runs_factory_started_idx serves the range. Factories without a run have no row.
SELECT factory_id,
       count(*)                                                  AS total,
       count(*) FILTER (WHERE status = 'running')                AS running,
       count(*) FILTER (WHERE status = 'failed')                 AS failed,
       count(*) FILTER (WHERE status IN ('succeeded', 'cancelled')) AS finished,
       coalesce(sum(cost_usd), 0)::float8                        AS spend_usd
FROM agent_runs
WHERE factory_id = ANY (sqlc.arg(factories)::uuid[])
  AND started_at >= sqlc.arg(since) AND started_at < sqlc.arg(until)
GROUP BY factory_id
ORDER BY factory_id;

-- name: GetRun :one
SELECT * FROM agent_runs WHERE id = $1;

-- name: LockLostRuns :many
-- Running runs whose lease is gone and ended before ended_before, the earliest end first. A run's
-- lease is the item's lease at the run's station acquired no later than the run started: none of
-- those is live, and the latest of their spans ended before the cutoff (or, when they left no
-- span, the run started before it). Skips runs whose item another transaction holds (a
-- transition or a finish in flight); agent_runs_running_idx keeps the read on running runs.
WITH lost AS (
  SELECT r.id, coalesce((
    SELECT max(s.ended_at) FROM lease_spans s
    WHERE s.item_id = r.item_id AND s.station = r.station AND s.acquired_at <= r.started_at
  ), r.started_at)::timestamptz AS lease_ended_at
  FROM agent_runs r
  WHERE r.status = 'running' AND NOT EXISTS (
    SELECT 1 FROM leases l WHERE l.item_id = r.item_id AND l.station = r.station AND l.acquired_at <= r.started_at
  )
)
SELECT r.id FROM lost
JOIN agent_runs r ON r.id = lost.id
JOIN work_items w ON w.id = r.item_id
WHERE lost.lease_ended_at < sqlc.arg(ended_before)::timestamptz AND r.status = 'running'
ORDER BY lost.lease_ended_at, r.started_at, r.id
LIMIT sqlc.arg(max_runs)::bigint
FOR UPDATE OF r SKIP LOCKED
FOR NO KEY UPDATE OF w SKIP LOCKED;

-- name: RunsOver :many
-- The runs among ids that are over: no longer running and no longer holding their lease (the
-- item's lease at the run's station acquired no later than the run started). Looked up by
-- primary key.
SELECT r.id FROM agent_runs r
WHERE r.id = ANY (sqlc.arg(ids)::uuid[]) AND r.status <> 'running' AND NOT EXISTS (
  SELECT 1 FROM leases l WHERE l.item_id = r.item_id AND l.station = r.station AND l.acquired_at <= r.started_at
);

-- name: CancelLostRuns :many
UPDATE agent_runs SET status = 'cancelled', error = sqlc.arg(error), ended_at = sqlc.arg(ended_at)
WHERE id = ANY (sqlc.arg(ids)::uuid[]) AND status = 'running'
RETURNING *;
