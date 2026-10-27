-- Judge scores (00066), under row-level security: every statement sees the transaction's
-- organization only. Recent and Summary read production scores (a run_id) only; a try belongs to
-- its eval run.

-- name: InsertJudgeScore :one
-- org_id comes from its default, goal_org(). No row when the judge already has a score of the run.
INSERT INTO judge_scores (id, factory_id, version_id, judge, run_id, item_id, created_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(version_id), sqlc.arg(judge), sqlc.narg(run_id), sqlc.narg(item_id),
        sqlc.arg(created_at))
ON CONFLICT (judge, run_id) DO NOTHING
RETURNING *;

-- name: JudgeScoreOfRun :one
SELECT * FROM judge_scores WHERE judge = sqlc.arg(judge) AND run_id = sqlc.arg(run_id)::uuid;

-- name: ClaimJudgeScore :one
-- The oldest queued score, or running one whose lease ended by now, that no other transaction
-- holds: leased until until, as running, under a new token.
UPDATE judge_scores s SET state = 'running', lease_until = sqlc.arg(until)::timestamptz, lease_token = gen_random_uuid()
WHERE s.id = (
  SELECT d.id FROM judge_scores d
  WHERE d.state = 'queued' OR (d.state = 'running' AND d.lease_until <= sqlc.arg(now)::timestamptz)
  ORDER BY d.created_at, d.id
  LIMIT 1
  FOR UPDATE SKIP LOCKED)
RETURNING s.*;

-- name: RecordJudgeScore :one
-- Ends a running score under its lease's token, unless the lease ended by scored_at. No row when
-- the lease is lost.
UPDATE judge_scores
SET state = sqlc.arg(state), outcome = sqlc.narg(outcome), score = sqlc.narg(score), passed = sqlc.narg(passed),
    reason = sqlc.arg(reason), cost_usd = sqlc.arg(cost_usd), judge_run_id = sqlc.narg(judge_run_id),
    scored_at = sqlc.arg(scored_at)::timestamptz, lease_until = NULL, lease_token = NULL
WHERE id = sqlc.arg(id) AND state = 'running' AND lease_token = sqlc.arg(lease_token)::uuid
  AND lease_until > sqlc.arg(scored_at)::timestamptz
RETURNING *;

-- name: RecentJudgeScores :many
SELECT sqlc.embed(s), w.number AS item_number, w.title AS item_title
FROM judge_scores s
JOIN work_items w ON w.id = s.item_id
WHERE s.factory_id = sqlc.arg(factory_id) AND s.judge = sqlc.arg(judge) AND s.run_id IS NOT NULL
ORDER BY s.created_at DESC, s.id DESC
LIMIT sqlc.arg(max_scores)::bigint;

-- name: JudgeScoreDays :many
-- Each judge's ended production scores of the factory queued from since to until, by UTC day:
-- how many were scored and passed, and what every judge run that ended cost (errors included).
SELECT judge, date_trunc('day', created_at, 'UTC')::timestamptz AS day,
       count(*) FILTER (WHERE state = 'scored') AS scored,
       count(*) FILTER (WHERE state = 'scored' AND passed) AS passed,
       sum(cost_usd)::numeric AS cost_usd
FROM judge_scores
WHERE factory_id = sqlc.arg(factory_id) AND run_id IS NOT NULL AND state IN ('scored', 'error')
  AND created_at >= sqlc.arg(since) AND created_at <= sqlc.arg(until)
GROUP BY judge, day
ORDER BY judge, day;
