-- CI waits and their shared commit polls (ADR 0044 decision 9). Row-level security scopes each
-- to the transaction's organization.

-- name: UpsertCIPoll :exec
-- The commit's poll, created due at next_poll_at with the shortest interval; an existing poll is
-- reused as it is.
INSERT INTO ci_polls (host, repo, sha, next_poll_at)
VALUES (sqlc.arg(host), sqlc.arg(repo), sqlc.arg(sha), sqlc.arg(next_poll_at))
ON CONFLICT (org_id, host, repo, sha) DO NOTHING;

-- name: InsertCIWait :one
-- A new open wait; the partial unique key ci_waits_open_key refuses a second open one of the
-- item, station visit and repository.
INSERT INTO ci_waits (id, item_id, station, visit, host, repo, sha, pr_url, phase, round, held, started_at)
VALUES (sqlc.arg(id), sqlc.arg(item_id), sqlc.arg(station), sqlc.arg(visit), sqlc.arg(host), sqlc.arg(repo), sqlc.arg(sha),
        sqlc.narg(pr_url), sqlc.arg(phase), sqlc.arg(round), sqlc.narg(held), sqlc.arg(started_at))
RETURNING *;

-- name: CIWaitItem :one
-- The item of the open wait id, for its lock; no row when the wait is not open.
SELECT item_id FROM ci_waits WHERE id = sqlc.arg(id) AND ended_at IS NULL;

-- name: EndOpenCIWait :one
-- Ends the open wait id with outcome at ended_at and returns it; no row when it is not open.
UPDATE ci_waits SET ended_at = sqlc.arg(ended_at), outcome = sqlc.arg(outcome)
WHERE id = sqlc.arg(id) AND ended_at IS NULL
RETURNING *;

-- name: EndCIWaits :execrows
-- Ends the open waits among ids with outcome at ended_at; ended ones are left as they are.
UPDATE ci_waits SET ended_at = sqlc.arg(ended_at), outcome = sqlc.arg(outcome)
WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND ended_at IS NULL;

-- name: LeaseDueCIPolls :many
-- Up to max_polls polls due by now that have an open wait and no live lease, oldest due first,
-- leased to owner until lease_until. SKIP LOCKED lets instances lease side by side.
UPDATE ci_polls p
SET lease_owner = sqlc.arg(owner), lease_until = sqlc.arg(lease_until)
FROM (
  SELECT q.org_id, q.host, q.repo, q.sha FROM ci_polls q
  WHERE q.next_poll_at <= sqlc.arg(now) AND (q.lease_until IS NULL OR q.lease_until <= sqlc.arg(now))
    AND EXISTS (SELECT 1 FROM ci_waits w
                WHERE w.org_id = q.org_id AND w.host = q.host AND w.repo = q.repo AND w.sha = q.sha AND w.ended_at IS NULL)
  ORDER BY q.next_poll_at, q.host, q.repo, q.sha
  LIMIT sqlc.arg(max_polls)
  FOR UPDATE SKIP LOCKED) due
WHERE p.org_id = due.org_id AND p.host = due.host AND p.repo = due.repo AND p.sha = due.sha
RETURNING p.*;

-- name: RecordCIPoll :execrows
-- Records a poll's result under its lease owner and releases the lease; no row when another
-- owner holds the poll or none does. A NULL checks, an empty etag and a first_check_seen_at
-- already set keep what the poll has.
UPDATE ci_polls
SET checks = coalesce(sqlc.narg(checks), checks),
    etag = CASE WHEN sqlc.arg(etag)::text = '' THEN etag ELSE sqlc.arg(etag)::text END,
    last_error = sqlc.arg(last_error),
    first_check_seen_at = coalesce(first_check_seen_at, sqlc.narg(first_check_seen_at)),
    next_poll_at = sqlc.arg(next_poll_at), interval_seconds = sqlc.arg(interval_seconds),
    lease_owner = NULL, lease_until = NULL
WHERE host = sqlc.arg(host) AND repo = sqlc.arg(repo) AND sha = sqlc.arg(sha) AND lease_owner = sqlc.arg(owner);

-- name: OpenCIWaits :many
-- The item's open waits by start, each with its poll's latest state.
SELECT sqlc.embed(w), sqlc.embed(p)
FROM ci_waits w
JOIN ci_polls p ON p.org_id = w.org_id AND p.host = w.host AND p.repo = w.repo AND p.sha = w.sha
WHERE w.item_id = sqlc.arg(item_id) AND w.ended_at IS NULL
ORDER BY w.started_at, w.station, w.visit, w.repo;

-- name: CIRounds :one
-- How many distinct CI rounds the item had in one visit to the station.
SELECT count(DISTINCT round) FROM ci_waits
WHERE item_id = sqlc.arg(item_id) AND station = sqlc.arg(station) AND visit = sqlc.arg(visit);
