-- Members' personal tokens (00047), under row-level security: every statement sees the
-- transaction's organization only. A presented token is resolved by goal_personal_token, a
-- definer; owners' names come from goal_members(), since goal_app cannot read schema auth.

-- name: PersonalToken :one
-- The token whose hash is hash while it is unrevoked, unexpired, of a live organization and of a
-- member of it, with the owner's current roles; NULL otherwise. Records its use at most once a
-- minute.
SELECT goal_personal_token(sqlc.arg(hash)::text)::jsonb AS token;

-- name: InsertPersonalToken :one
-- org_id comes from its default, goal_org(); the owner must be its member (the foreign key). The
-- expiry is counted from the database's clock, as the table's check is.
INSERT INTO personal_tokens (id, user_id, name, hint, hash, scopes, expires_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(name), sqlc.arg(hint), sqlc.arg(hash), sqlc.arg(scopes)::text[],
        now() + make_interval(days => sqlc.arg(expires_in_days)::int))
RETURNING created_at, expires_at;

-- name: PersonalTokens :many
-- Live tokens and those revoked in the last 30 days, of owner (NULL: every member's), live ones
-- first, newest first, with their owner as the members list names them. Who revoked one is named
-- as its personal_token.revoked entry names them; a revocation by the organization's deletion has
-- no entry ('').
SELECT t.id, t.user_id, t.name, t.hint, t.scopes, t.created_at, t.expires_at, t.revoked_at, t.revoked_by, t.last_used_at,
       m.name::text AS owner_name, m.email::text AS owner_email,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'personal_token' AND a.target_id = t.id::text AND a.action = 'personal_token.revoked'
                 ORDER BY a.id LIMIT 1), '')::text AS revoked_by_name
FROM personal_tokens t
JOIN jsonb_to_recordset(goal_members()) AS m (user_id uuid, name text, email text) ON m.user_id = t.user_id
WHERE (sqlc.narg(owner)::uuid IS NULL OR t.user_id = sqlc.narg(owner)::uuid)
  AND (t.revoked_at IS NULL OR t.revoked_at > now() - interval '30 days')
ORDER BY t.revoked_at IS NOT NULL, t.created_at DESC, t.id DESC;

-- name: RevokePersonalToken :one
-- Revokes a live token of owner (NULL: any member's); a token already revoked keeps its first
-- revocation. The row is returned either way (revoked: whether this statement revoked it); no
-- row: no such token, or another member's.
WITH revoked AS (
  UPDATE personal_tokens SET revoked_at = now(), revoked_by = sqlc.arg(revoked_by)
  WHERE id = sqlc.arg(id) AND revoked_at IS NULL
    AND (sqlc.narg(owner)::uuid IS NULL OR user_id = sqlc.narg(owner)::uuid)
  RETURNING id
)
SELECT t.id, t.user_id, t.name, EXISTS (SELECT 1 FROM revoked) AS revoked
FROM personal_tokens t
WHERE t.id = sqlc.arg(id) AND (sqlc.narg(owner)::uuid IS NULL OR t.user_id = sqlc.narg(owner)::uuid);
