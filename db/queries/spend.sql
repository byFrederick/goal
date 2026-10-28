-- The organization's spend (ports.Spend.OrgSpend). Row-level security scopes every read to the
-- organization of the transaction; agent_runs_org_started_idx and work_items_org_done_at_idx
-- (migration 00067) serve its ranges. Days are UTC days whatever the session's time zone.

-- name: OrgSpendByDay :many
-- The cost of the runs started in [since, until), by UTC day and factory; a running run counts
-- with its cost so far.
SELECT date_trunc('day', started_at, 'UTC')::timestamptz AS day, factory_id, sum(cost_usd)::float8 AS spend_usd
FROM agent_runs
WHERE started_at >= sqlc.arg(since)::timestamptz AND started_at < sqlc.arg(until)::timestamptz
GROUP BY 1, 2;

-- name: OrgSpendByAgent :many
-- The cost of the runs started in [since, until), by factory and agent name.
SELECT factory_id, agent, sum(cost_usd)::float8 AS spend_usd
FROM agent_runs
WHERE started_at >= sqlc.arg(since)::timestamptz AND started_at < sqlc.arg(until)::timestamptz
GROUP BY factory_id, agent;

-- name: OrgSpendByModel :many
-- The cost of the runs started in [since, until), by model.
SELECT model, sum(cost_usd)::float8 AS spend_usd
FROM agent_runs
WHERE started_at >= sqlc.arg(since)::timestamptz AND started_at < sqlc.arg(until)::timestamptz
GROUP BY model;

-- name: OrgSpendByAgentModel :many
-- The cost of the factories' runs started in [since, until), by factory, agent and model, ordered
-- by bytes (COLLATE "C", as Go compares strings) and after the key (after_factory, after_agent,
-- after_model) when paged, for the spend export (ports.Spend.EachAgentModelSpend). The key is the
-- grouping, so filtering runs by it before grouping keeps exactly the groups after it.
SELECT factory_id, agent, model, sum(cost_usd)::float8 AS spend_usd
FROM agent_runs
WHERE factory_id = ANY (sqlc.arg(factories)::uuid[])
  AND started_at >= sqlc.arg(since)::timestamptz AND started_at < sqlc.arg(until)::timestamptz
  AND (NOT sqlc.arg(paged)::boolean
       OR (factory_id, agent COLLATE "C", model COLLATE "C")
          > (sqlc.arg(after_factory)::uuid, sqlc.arg(after_agent)::text COLLATE "C", sqlc.arg(after_model)::text COLLATE "C"))
GROUP BY factory_id, agent, model
ORDER BY factory_id, agent COLLATE "C", model COLLATE "C"
LIMIT sqlc.arg(max_rows)::bigint;

-- name: CountAgentModelSpend :one
-- How many rows OrgSpendByAgentModel has over all its pages, counted up to max_rows.
SELECT count(*) FROM (
  SELECT DISTINCT factory_id, agent, model
  FROM agent_runs
  WHERE factory_id = ANY (sqlc.arg(factories)::uuid[])
    AND started_at >= sqlc.arg(since)::timestamptz AND started_at < sqlc.arg(until)::timestamptz
  LIMIT sqlc.arg(max_rows)::bigint
) keys;

-- name: OrgReworkSpend :one
-- The cost of the runs started in [since, until) once their item had entered a rework round of
-- 1 or more (item_spans_item_idx): an item reopened after done keeps its round.
SELECT coalesce(sum(r.cost_usd), 0)::float8 AS spend_usd
FROM agent_runs r
WHERE r.started_at >= sqlc.arg(since)::timestamptz AND r.started_at < sqlc.arg(until)::timestamptz
  AND EXISTS (
    SELECT 1 FROM item_spans s
    WHERE s.item_id = r.item_id AND s.rework_round >= 1 AND s.entered_at <= r.started_at
  );

-- name: OrgMergedByDay :many
-- The items done in [since, until) and still in state done, by UTC day of done_at.
SELECT date_trunc('day', done_at, 'UTC')::timestamptz AS day, count(*) AS merged
FROM work_items
WHERE done_at >= sqlc.arg(since)::timestamptz AND done_at < sqlc.arg(until)::timestamptz AND state = 'done'
GROUP BY 1;
