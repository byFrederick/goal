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

-- name: SentBackBetween :many
-- The factory's sent-back touches in [since, until), oldest first, at most max_events: each with
-- its item's number and title, its event's data.reason and the agent of the item's last lease
-- span that ended by then. The items are those with a rejection event in the window
-- (item_events_factory_at_idx), so item_touches is read per item.
SELECT t.event_id, t.item_id, w.number AS item_number, w.title AS item_title, coalesce(t.station, '')::text AS station, t.kind,
       coalesce(e.data->>'reason', '')::text AS reason, t.at, coalesce(ls.agent, '')::text AS agent
FROM item_touches t
JOIN item_events e ON e.id = t.event_id
JOIN work_items w ON w.id = t.item_id
LEFT JOIN LATERAL (
  SELECT l.agent FROM lease_spans l
  WHERE l.item_id = t.item_id AND l.ended_at <= t.at
  ORDER BY l.ended_at DESC, l.id DESC
  LIMIT 1
) ls ON true
WHERE t.item_id IN (
    SELECT r.item_id FROM item_events r
    WHERE r.factory_id = sqlc.arg(factory_id) AND r.kind IN ('rejected', 'gate_rejected')
      AND r.at >= sqlc.arg(since)::timestamptz AND r.at < sqlc.arg(until)::timestamptz)
  AND t.factory_id = sqlc.arg(factory_id) AND t.kind IN ('rejected', 'gate_rejected')
  AND t.at >= sqlc.arg(since)::timestamptz AND t.at < sqlc.arg(until)::timestamptz
ORDER BY t.at, t.event_id
LIMIT sqlc.arg(max_events);

-- name: StationFlowSpans :one
-- The items with a span at the station that left in [since, until), and the 85th percentile of
-- those spans' durations in seconds, interpolated between ranks (item_spans_window_idx).
SELECT count(DISTINCT item_id) AS passed,
       coalesce(percentile_cont(0.85) WITHIN GROUP (ORDER BY extract(epoch FROM left_at - entered_at)::float8), 0)::float8 AS p85_seconds
FROM item_spans
WHERE factory_id = sqlc.arg(factory_id) AND station = sqlc.arg(station)::text
  AND left_at >= sqlc.arg(since)::timestamptz AND left_at < sqlc.arg(until)::timestamptz;

-- name: StationSentBack :one
-- The sent-back touches given at the station in [since, until), read per item as SentBackBetween.
SELECT count(*)::bigint FROM item_touches t
WHERE t.item_id IN (
    SELECT r.item_id FROM item_events r
    WHERE r.factory_id = sqlc.arg(factory_id) AND r.station = sqlc.arg(station)::text
      AND r.kind IN ('rejected', 'gate_rejected')
      AND r.at >= sqlc.arg(since)::timestamptz AND r.at < sqlc.arg(until)::timestamptz)
  AND t.factory_id = sqlc.arg(factory_id) AND t.station = sqlc.arg(station)::text
  AND t.kind IN ('rejected', 'gate_rejected')
  AND t.at >= sqlc.arg(since)::timestamptz AND t.at < sqlc.arg(until)::timestamptz;
