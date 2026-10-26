-- An organization's API keys (00046), under row-level security: every statement sees the
-- transaction's organization only. A presented key is resolved by goal_api_key, a definer.

-- name: APIKey :one
-- The key whose hash is hash while it is unrevoked, unexpired and of a live organization; NULL
-- otherwise. Records its use at most once a minute.
SELECT goal_api_key(sqlc.arg(hash)::text)::jsonb AS key;

-- name: InsertAPIKey :one
-- org_id comes from its default, goal_org(). The expiry is counted from the database's clock, as
-- the table's check is.
INSERT INTO api_keys (id, name, hint, hash, scopes, role_ceiling, all_factories, created_by, expires_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(hint), sqlc.arg(hash), sqlc.arg(scopes)::text[], sqlc.arg(role_ceiling),
        sqlc.arg(all_factories), sqlc.arg(created_by), now() + make_interval(days => sqlc.arg(expires_in_days)::int))
RETURNING created_at, expires_at;

-- name: InsertAPIKeyFactories :exec
INSERT INTO api_key_factories (key_id, factory_id)
SELECT sqlc.arg(key_id)::uuid, unnest(sqlc.arg(factory_ids)::uuid[]);

-- name: APIKeys :many
-- Live keys and those revoked in the last 30 days: live ones first, newest first. Who created and
-- revoked a key are named as their audit entries name them (a person's name at that moment); a
-- revocation by the organization's deletion has no entry ('').
SELECT k.id, k.name, k.hint, k.scopes, k.role_ceiling, k.all_factories, k.created_by, k.created_at, k.expires_at,
       k.revoked_at, k.revoked_by, k.last_used_at,
       coalesce((SELECT array_agg(f.factory_id ORDER BY f.factory_id) FROM api_key_factories f WHERE f.key_id = k.id),
                '{}')::uuid[] AS factory_ids,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'api_key' AND a.target_id = k.id::text AND a.action = 'api_key.created'
                 ORDER BY a.id LIMIT 1), '')::text AS created_by_name,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'api_key' AND a.target_id = k.id::text AND a.action = 'api_key.revoked'
                 ORDER BY a.id LIMIT 1), '')::text AS revoked_by_name
FROM api_keys k
WHERE k.revoked_at IS NULL OR k.revoked_at > now() - interval '30 days'
ORDER BY k.revoked_at IS NOT NULL, k.created_at DESC, k.id DESC;

-- name: RevokeAPIKey :one
-- Revokes a live key; a key already revoked keeps its first revocation. The row is returned
-- either way (revoked: whether this statement revoked it); no row: no such key.
WITH revoked AS (
  UPDATE api_keys SET revoked_at = now(), revoked_by = sqlc.arg(revoked_by)
  WHERE id = sqlc.arg(id) AND revoked_at IS NULL
  RETURNING id, name
)
SELECT k.id, k.name, EXISTS (SELECT 1 FROM revoked) AS revoked
FROM api_keys k WHERE k.id = sqlc.arg(id);
