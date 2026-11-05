-- Tries of past items (00102, ADR 0039 decisions 9 and 11). Row-level security scopes every query
-- to the organization.

-- name: LockProposal :one
-- The proposal whose tries are queued, locked so two sweeps queue its tries once and two Try
-- agains number their attempts in turn.
SELECT id, number, state FROM jonah_proposals WHERE factory_id = sqlc.arg(factory_id) AND id = sqlc.arg(id) FOR UPDATE;

-- name: ProposalHasTries :one
SELECT EXISTS (SELECT 1 FROM tries WHERE proposal_id = sqlc.arg(proposal_id))::bool;

-- name: LatestItemTry :one
-- The proposal's try of the cited item with the highest attempt: what Try again copies. A
-- kept-test try of the same item is not one (00108).
SELECT * FROM tries
WHERE proposal_id = sqlc.arg(proposal_id) AND item_id = sqlc.arg(item_id) AND kept_test_id IS NULL
ORDER BY attempt DESC LIMIT 1;

-- name: InsertTry :one
-- One try of a proposal: of an item it cites, or of a kept test with the content it runs on (00108).
INSERT INTO tries (id, factory_id, proposal_id, item_id, attempt, station, original_run_id, model, kept_test_id, content_key, created_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(proposal_id), sqlc.arg(item_id), sqlc.arg(attempt), sqlc.arg(station),
        sqlc.arg(original_run_id), sqlc.arg(model), sqlc.narg(kept_test_id), sqlc.narg(content_key), sqlc.arg(created_at))
RETURNING *;

-- name: InsertPublishTry :one
-- One kept-test try of a publish (00108): no proposal, its version and edits its own. No row when
-- the test has a publish's try of that content and attempt already (tries_publish_key).
INSERT INTO tries (id, factory_id, item_id, attempt, station, original_run_id, model, kept_test_id, content_key, version_id, edits, created_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(item_id), sqlc.arg(attempt), sqlc.arg(station), sqlc.arg(original_run_id),
        sqlc.arg(model), sqlc.arg(kept_test_id), sqlc.arg(content_key), sqlc.arg(version_id), sqlc.arg(edits), sqlc.arg(created_at))
ON CONFLICT (org_id, kept_test_id, content_key, attempt) WHERE proposal_id IS NULL DO NOTHING
RETURNING *;

-- name: KeptTestTries :many
-- The factory's kept-test tries of content, from any proposal or publish, oldest first (tries_content_idx).
SELECT sqlc.embed(t), coalesce(p.number, 0)::bigint AS proposal_number FROM tries t
LEFT JOIN jonah_proposals p ON p.id = t.proposal_id
WHERE t.factory_id = sqlc.arg(factory_id) AND t.content_key = sqlc.arg(content_key) AND t.kept_test_id IS NOT NULL
ORDER BY t.created_at, t.id;

-- name: ProposalTries :many
SELECT sqlc.embed(t), p.number AS proposal_number FROM tries t
JOIN jonah_proposals p ON p.id = t.proposal_id
WHERE t.proposal_id = sqlc.arg(proposal_id)
ORDER BY t.created_at, t.id;

-- name: TryByID :one
-- A try with its proposal's number, 0 for a publish's try.
SELECT sqlc.embed(t), coalesce(p.number, 0)::bigint AS proposal_number FROM tries t
LEFT JOIN jonah_proposals p ON p.id = t.proposal_id
WHERE t.id = sqlc.arg(id);

-- name: LockFactoryTries :exec
-- Serializes the factory's claims, so two claims never both see a free place under the cap.
SELECT pg_advisory_xact_lock(hashtextextended('goal.tries:' || sqlc.arg(factory_id)::uuid::text, 0));

-- name: ClaimTry :one
-- The factory's oldest queued try, or running one whose lease ended by now, that no other
-- transaction holds, a publish's (no proposal) before a proposal's, while fewer than max_running
-- of its tries run under a live lease: running until until, one more attempt (tries_due_idx).
UPDATE tries t SET state = 'running', lease_until = sqlc.arg(until)::timestamptz, attempts = t.attempts + 1
WHERE t.id = (
  SELECT d.id FROM tries d
  WHERE d.factory_id = sqlc.arg(factory_id)
    AND (d.state = 'queued' OR (d.state = 'running' AND d.lease_until <= sqlc.arg(now)::timestamptz))
    AND (SELECT count(*) FROM tries l
         WHERE l.factory_id = sqlc.arg(factory_id) AND l.state = 'running' AND l.lease_until > sqlc.arg(now)::timestamptz)
        < sqlc.arg(max_running)::bigint
  ORDER BY (d.proposal_id IS NOT NULL), d.created_at, d.id
  LIMIT 1
  FOR UPDATE SKIP LOCKED)
RETURNING t.id;

-- name: FinishTry :one
-- Ends a running try under the lease its claim returned. No row when the lease is lost.
UPDATE tries
SET state = 'done', verdict = sqlc.narg(verdict), error = sqlc.arg(error), reason = sqlc.arg(reason), diff = sqlc.arg(diff), cost_usd = sqlc.arg(cost_usd),
    try_run_id = sqlc.narg(try_run_id), finished_at = sqlc.arg(finished_at)::timestamptz, lease_until = NULL
WHERE id = sqlc.arg(id) AND state = 'running' AND lease_until = sqlc.arg(lease_until)::timestamptz
RETURNING id;

-- name: RecordTryRun :one
-- Stores a running try's run, diff and cost under the lease its claim returned, before it is
-- graded. No row when the lease is lost.
UPDATE tries SET try_run_id = sqlc.arg(try_run_id), diff = sqlc.arg(diff), cost_usd = sqlc.arg(cost_usd)
WHERE id = sqlc.arg(id) AND state = 'running' AND lease_until = sqlc.arg(lease_until)::timestamptz
RETURNING id;

-- name: TryTarget :one
-- The item's newest finished station run at station that was a claim's first run: its branch
-- head is recorded in one of its repositories (agent_runs_try_target_idx, 00131).
SELECT * FROM agent_runs
WHERE item_id = sqlc.arg(item_id) AND station = sqlc.arg(station) AND kind = 'station'
  AND status <> 'running'
  AND EXISTS (SELECT 1 FROM agent_run_repositories rr WHERE rr.run_id = agent_runs.id AND rr.branch_head IS NOT NULL)
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: RunByID :one
SELECT * FROM agent_runs WHERE id = sqlc.arg(id);

-- name: OutputsBefore :many
-- The item's latest output of each station among its runs that ended before before.
SELECT DISTINCT ON (station) station, output FROM agent_runs
WHERE item_id = sqlc.arg(item_id) AND ended_at < sqlc.arg(before)::timestamptz AND output IS NOT NULL
ORDER BY station, ended_at DESC, id DESC;

-- name: AgentDoneItems :many
-- The factory's done items, the most recently done first (work_items_done_at_idx), that the agent
-- has a finished station run of that was a claim's first, with the newest such run, the stations
-- of the item's rejected and gate_rejected events, oldest first, and the item's cost: what Try
-- another model tries (ADR 0039 decision 13).
SELECT sqlc.embed(r), w.number AS item_number, w.title AS item_title, w.cost_usd AS item_cost_usd,
       w.done_at::timestamptz AS done_at,
       coalesce((SELECT array_agg(coalesce(e.station, '') ORDER BY e.at, e.id) FROM item_events e
                 WHERE e.item_id = w.id AND e.kind IN ('rejected', 'gate_rejected')), '{}')::text[] AS sent_back_at
FROM work_items w
JOIN LATERAL (
  SELECT a.id FROM agent_runs a
  WHERE a.item_id = w.id AND a.agent = sqlc.arg(agent) AND a.kind = 'station' AND a.status <> 'running'
    AND EXISTS (SELECT 1 FROM agent_run_repositories rr WHERE rr.run_id = a.id AND rr.branch_head IS NOT NULL)
  ORDER BY a.started_at DESC, a.id DESC
  LIMIT 1
) pick ON true
JOIN agent_runs r ON r.id = pick.id
WHERE w.factory_id = sqlc.arg(factory_id) AND w.state = 'done' AND w.done_at IS NOT NULL
ORDER BY w.done_at DESC, w.id
LIMIT sqlc.arg(max_items);

-- name: InsertGraderCheck :one
-- A check of the grader (00109, ADR 0039 decision 10) in its proposal's factory, on an item of
-- that factory. No row when the proposal is not the organization's or the item not its factory's.
INSERT INTO grader_checks (id, factory_id, proposal_id, item_id, result, answer, reason, run_id, cost_usd, checked_at)
SELECT sqlc.arg(id), p.factory_id, p.id, w.id, sqlc.arg(result), sqlc.arg(answer), sqlc.arg(reason), sqlc.narg(run_id),
       sqlc.arg(cost_usd), sqlc.arg(checked_at)
FROM jonah_proposals p
JOIN work_items w ON w.id = sqlc.arg(item_id) AND w.factory_id = p.factory_id
WHERE p.id = sqlc.arg(proposal_id)
RETURNING *;

-- name: ProposalGraderChecks :many
SELECT * FROM grader_checks WHERE proposal_id = sqlc.arg(proposal_id) ORDER BY checked_at, id;

-- name: GraderHitRates :one
-- The proposal's original checks read as not_fixed and final checks read as fixed, of each.
SELECT count(*) FILTER (WHERE result = 'original' AND answer = 'not_fixed') AS original_hits,
       count(*) FILTER (WHERE result = 'original') AS original_total,
       count(*) FILTER (WHERE result = 'final' AND answer = 'fixed') AS final_hits,
       count(*) FILTER (WHERE result = 'final') AS final_total
FROM grader_checks WHERE proposal_id = sqlc.arg(proposal_id);
