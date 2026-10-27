-- Eval runs and their tries (00067), under row-level security: every statement sees the
-- transaction's organization only. A store method locks a try before its run (ClaimEvalResult,
-- RecordEvalResult, then the run's update), so two methods never wait on each other in a cycle.

-- name: InsertEvalRun :one
INSERT INTO eval_runs (id, factory_id, version_id, content_sha256, trigger, cases, estimate_usd, created_by)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.narg(version_id), sqlc.arg(content_sha256), sqlc.arg(trigger),
        sqlc.narg(cases), sqlc.arg(estimate_usd), sqlc.arg(created_by))
RETURNING *;

-- name: InsertEvalResults :exec
-- One pending try per case and try number: case i gets tries 1 to tries[i].
INSERT INTO eval_results (eval_run_id, case_name, case_sha256, try)
SELECT sqlc.arg(eval_run_id)::uuid, c.name, c.sha256, t.try
FROM (SELECT unnest(sqlc.arg(case_names)::text[]) AS name, unnest(sqlc.arg(case_sha256s)::text[]) AS sha256,
             unnest(sqlc.arg(tries)::integer[]) AS tries) c
CROSS JOIN LATERAL generate_series(1, c.tries) AS t (try);

-- name: ClaimEvalResult :one
-- The oldest claimable try: pending, or running with its lease ended, of a run not finished.
-- Tries another transaction holds are skipped.
WITH next AS (
  SELECT r.eval_run_id, r.case_name, r.try
  FROM eval_results r JOIN eval_runs e ON e.id = r.eval_run_id
  WHERE r.state IN ('pending', 'running') AND (r.state = 'pending' OR r.lease_until <= sqlc.arg(now)::timestamptz)
    AND e.state IN ('queued', 'running')
  ORDER BY e.created_at, e.id, r.case_name, r.try
  LIMIT 1
  FOR UPDATE OF r SKIP LOCKED
)
UPDATE eval_results r SET state = 'running', lease_until = sqlc.arg(lease_until)::timestamptz
FROM next
WHERE r.eval_run_id = next.eval_run_id AND r.case_name = next.case_name AND r.try = next.try
RETURNING r.*;

-- name: StartEvalRun :exec
UPDATE eval_runs SET state = 'running' WHERE id = sqlc.arg(id) AND state = 'queued';

-- name: RecordEvalResult :execrows
-- Fenced by the claim's lease_until: no row when the try was claimed again or already recorded.
UPDATE eval_results
SET state = sqlc.arg(state), lease_until = NULL, run_id = sqlc.narg(run_id), checked_by = sqlc.arg(checked_by),
    detail = sqlc.arg(detail), cost_usd = sqlc.arg(cost_usd), turns = sqlc.arg(turns)
WHERE eval_run_id = sqlc.arg(eval_run_id) AND case_name = sqlc.arg(case_name) AND try = sqlc.arg(try)
  AND state = 'running' AND lease_until = sqlc.arg(lease_until)::timestamptz;

-- name: AddEvalRunCost :exec
UPDATE eval_runs SET cost_usd = cost_usd + sqlc.arg(cost_usd) WHERE id = sqlc.arg(id);

-- name: LockEvalRun :one
SELECT * FROM eval_runs WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: FinishEvalRun :one
UPDATE eval_runs SET state = sqlc.arg(state), finished_at = sqlc.arg(finished_at)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: EvalRun :one
SELECT * FROM eval_runs WHERE id = sqlc.arg(id);

-- name: EvalResults :many
SELECT * FROM eval_results WHERE eval_run_id = sqlc.arg(eval_run_id) ORDER BY case_name, try;

-- name: LatestEvalRunForContent :one
SELECT * FROM eval_runs
WHERE factory_id = sqlc.arg(factory_id) AND content_sha256 = sqlc.arg(content_sha256)
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: EvalCaseTries :many
-- Each case's last finished tries in the factory's runs, newest first (n from 1).
SELECT t.eval_run_id, t.case_name, t.case_sha256, t.try, t.state, t.lease_until, t.run_id, t.checked_by, t.detail,
       t.cost_usd, t.turns, t.n::bigint AS n
FROM (
  SELECT r.*, row_number() OVER (PARTITION BY r.case_name ORDER BY e.created_at DESC, e.id DESC, r.try DESC) AS n
  FROM eval_results r JOIN eval_runs e ON e.id = r.eval_run_id
  WHERE e.factory_id = sqlc.arg(factory_id) AND r.state IN ('pass', 'fail', 'error')
) t
WHERE t.n <= sqlc.arg(tries)::bigint
ORDER BY t.case_name, t.n;

-- name: EvalCaseLatestCosts :many
-- Each case's cost in the latest of the factory's runs that finished a try of it.
SELECT DISTINCT ON (c.case_name) c.case_name, c.eval_run_id, c.cost_usd::numeric(12, 4) AS cost_usd
FROM (
  SELECT r.case_name, r.eval_run_id, e.created_at, sum(r.cost_usd) AS cost_usd
  FROM eval_results r JOIN eval_runs e ON e.id = r.eval_run_id
  WHERE e.factory_id = sqlc.arg(factory_id) AND r.state IN ('pass', 'fail', 'error')
  GROUP BY r.case_name, r.eval_run_id, e.created_at
) c
ORDER BY c.case_name, c.created_at DESC, c.eval_run_id DESC;
