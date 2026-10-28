-- Jonah reviews, proposals and observations (00075), under row-level security: every statement
-- sees the transaction's organization only.

-- name: InsertJonahReview :one
-- org_id comes from its default, goal_org(). No row when the factory has a review in flight.
INSERT INTO jonah_reviews (id, factory_id, version_id, trigger, window_start, window_end, created_by, created_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(version_id), sqlc.arg(trigger), sqlc.arg(window_start),
        sqlc.arg(window_end), sqlc.arg(created_by), sqlc.arg(created_at))
ON CONFLICT (org_id, factory_id) WHERE state IN ('pending', 'running') DO NOTHING
RETURNING *;

-- name: InFlightJonahReview :one
SELECT * FROM jonah_reviews WHERE factory_id = sqlc.arg(factory_id) AND state IN ('pending', 'running');

-- name: ClaimJonahReview :one
-- The oldest pending review, or running one whose lease ended by now, that no other transaction
-- holds: running until until, one more attempt.
UPDATE jonah_reviews r SET state = 'running', lease_until = sqlc.arg(until)::timestamptz, attempts = r.attempts + 1
WHERE r.id = (
  SELECT d.id FROM jonah_reviews d
  WHERE d.state = 'pending' OR (d.state = 'running' AND d.lease_until <= sqlc.arg(now)::timestamptz)
  ORDER BY d.created_at, d.id
  LIMIT 1
  FOR UPDATE SKIP LOCKED)
RETURNING r.*;

-- name: FinishJonahReview :one
-- Ends a running review under the lease its claim returned. No row when the lease is lost.
UPDATE jonah_reviews
SET state = sqlc.arg(state), error = sqlc.arg(error), run_id = sqlc.narg(run_id), finished_at = sqlc.arg(finished_at)::timestamptz,
    lease_until = NULL
WHERE id = sqlc.arg(id) AND state = 'running' AND lease_until = sqlc.arg(lease_until)::timestamptz
RETURNING *;

-- name: LatestJonahReviews :many
SELECT * FROM jonah_reviews WHERE factory_id = sqlc.arg(factory_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(max_reviews);

-- name: LastProposalNumber :one
-- The factory's highest proposal number, 0 before its first. Only one review of a factory is in
-- flight, and its finish is fenced by its lease, so two finishes do not number at once; the
-- number key catches it if they do.
SELECT coalesce(max(number), 0)::bigint FROM jonah_proposals WHERE factory_id = sqlc.arg(factory_id);

-- name: InsertProposal :one
INSERT INTO jonah_proposals (id, factory_id, number, review_id, version_id, title, summary, files, evidence, estimate,
                             paths, created_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(number), sqlc.arg(review_id), sqlc.arg(version_id), sqlc.arg(title),
        sqlc.arg(summary), sqlc.arg(files), sqlc.arg(evidence), sqlc.arg(estimate), sqlc.arg(paths)::text[],
        sqlc.arg(created_at))
RETURNING *;

-- name: InsertObservation :one
INSERT INTO jonah_observations (id, factory_id, review_id, summary, detail, item_ids, count, state, proposal_id, created_at)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(review_id), sqlc.arg(summary), sqlc.arg(detail),
        sqlc.arg(item_ids)::uuid[], sqlc.arg(count), sqlc.arg(state), sqlc.narg(proposal_id), sqlc.arg(created_at))
RETURNING *;

-- name: Proposals :many
-- A factory's proposals, newest first, in state when one is given, below number before when one
-- is given (the keyset of the next page).
SELECT * FROM jonah_proposals
WHERE factory_id = sqlc.arg(factory_id)
  AND (sqlc.narg(state)::text IS NULL OR state = sqlc.narg(state)::text)
  AND (sqlc.narg(before)::bigint IS NULL OR number < sqlc.narg(before)::bigint)
ORDER BY number DESC
LIMIT sqlc.arg(max_proposals);

-- name: ProposalByNumber :one
SELECT * FROM jonah_proposals WHERE factory_id = sqlc.arg(factory_id) AND number = sqlc.arg(number);

-- name: DecideProposal :one
-- Decides a proposal still proposed. No row when there is none or it was decided.
UPDATE jonah_proposals
SET state = sqlc.arg(state), published_version_id = sqlc.narg(published_version_id), reject_reason = sqlc.arg(reject_reason),
    decided_by = sqlc.arg(decided_by), decided_at = sqlc.arg(decided_at)::timestamptz
WHERE factory_id = sqlc.arg(factory_id) AND number = sqlc.arg(number) AND state = 'proposed'
RETURNING *;

-- name: LatestObservations :many
SELECT * FROM jonah_observations WHERE factory_id = sqlc.arg(factory_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(max_observations);

-- name: JonahSpendSince :one
-- The cost of the factory's review runs started at or after since, on agent_runs_jonah_idx.
SELECT coalesce(sum(cost_usd), 0)::float8 AS spend_usd
FROM agent_runs
WHERE factory_id = sqlc.arg(factory_id) AND kind = 'jonah' AND started_at >= sqlc.arg(since);

-- name: SentBackSince :one
-- The factory's sent-back touches (a person's rejected or gate_rejected) at or after since.
SELECT count(*)::bigint FROM item_touches
WHERE factory_id = sqlc.arg(factory_id) AND kind IN ('rejected', 'gate_rejected') AND at >= sqlc.arg(since);
