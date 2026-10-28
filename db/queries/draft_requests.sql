-- Draft requests (ports.DraftRequests; migration 00078), under row-level security: every statement
-- sees the transaction's organization only, but the claim and the requeue, which run 00078's
-- definers in no organization.

-- name: InsertDraftRequest :one
-- org_id comes from its default, goal_org(); a second open request of the factory violates
-- draft_requests_one_open_idx.
INSERT INTO draft_requests (id, factory_id, requester, text, agent)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(requester), sqlc.arg(text), sqlc.narg(agent))
RETURNING *;

-- name: DraftRequest :one
SELECT * FROM draft_requests WHERE id = sqlc.arg(id);

-- name: DraftRequests :many
-- A factory's latest requests, newest first.
SELECT * FROM draft_requests
WHERE factory_id = sqlc.arg(factory_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(max_rows);

-- name: ClaimDraftRequest :one
-- The oldest queued request in any live organization, leased under a new holder (00078), in no
-- organization: the caller binds the transaction to the one returned.
SELECT c.org_id::uuid AS org_id, c.id::uuid AS id, c.lease_holder::uuid AS lease_holder
FROM goal_claim_draft_request(sqlc.arg(now)::timestamptz, sqlc.arg(until)::timestamptz) c;

-- name: HeartbeatDraftRequest :execrows
-- Nothing once the lease ended or another claim drew a new holder.
UPDATE draft_requests SET lease_expires_at = sqlc.arg(until)::timestamptz
WHERE id = sqlc.arg(id) AND lease_holder = sqlc.arg(lease_holder)::uuid AND status = 'running'
  AND lease_expires_at > sqlc.arg(now)::timestamptz;

-- name: FinishDraftRequest :execrows
-- Nothing once another claim drew a new holder or the request is not running.
UPDATE draft_requests
SET status = sqlc.arg(status), summary = sqlc.arg(summary), changed_paths = sqlc.arg(changed_paths)::text[],
    cost_usd = sqlc.arg(cost_usd), turns = sqlc.arg(turns), error = sqlc.arg(error),
    finished_at = sqlc.arg(finished_at)::timestamptz, lease_holder = NULL, lease_expires_at = NULL
WHERE id = sqlc.arg(id) AND lease_holder = sqlc.arg(lease_holder)::uuid AND status = 'running';

-- name: RequeueDraftRequests :one
-- Every organization's running requests whose lease ended by now, queued again (00078).
SELECT goal_requeue_draft_requests(sqlc.arg(now)::timestamptz)::integer AS requeued;
