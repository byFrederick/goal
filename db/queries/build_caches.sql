-- Build caches (00143, ADR 0045). Row-level security scopes every query to the organization.

-- name: FindBuildCache :one
-- The ready cache of the key, marked used at.
UPDATE build_caches SET used_at = sqlc.arg(at)
WHERE repository_key = sqlc.arg(repository_key) AND cache_key = sqlc.arg(cache_key) AND state = 'ready'
RETURNING *;

-- name: ReserveBuildCache :execrows
-- Inserts the key saving, or takes over a saving row reserved more than an hour before at, in
-- one statement so concurrent reserves of a key have one winner: 1 row when this caller got it.
INSERT INTO build_caches (repository_key, cache_key, object_key, state, created_at, used_at)
VALUES (sqlc.arg(repository_key), sqlc.arg(cache_key), sqlc.arg(object_key), 'saving', sqlc.arg(at), sqlc.arg(at))
ON CONFLICT (org_id, repository_key, cache_key) DO UPDATE
SET object_key = EXCLUDED.object_key, created_at = EXCLUDED.created_at, used_at = EXCLUDED.used_at
WHERE build_caches.state = 'saving' AND build_caches.created_at < EXCLUDED.created_at - interval '1 hour';

-- name: CommitBuildCache :execrows
-- Marks the saving cache ready, only for the object key it was reserved with.
UPDATE build_caches SET state = 'ready', size_bytes = sqlc.arg(size_bytes), used_at = sqlc.arg(at)
WHERE repository_key = sqlc.arg(repository_key) AND cache_key = sqlc.arg(cache_key)
  AND object_key = sqlc.arg(object_key) AND state = 'saving';

-- name: PruneBuildCaches :many
-- Deletes the repository's ready caches but the newest keep (build_caches_ready_idx) and its
-- saving caches reserved more than an hour before at, returning their object keys.
DELETE FROM build_caches b
WHERE b.repository_key = sqlc.arg(repository_key) AND (
  (b.state = 'saving' AND b.created_at < sqlc.arg(at)::timestamptz - interval '1 hour')
  OR (b.state = 'ready' AND b.cache_key NOT IN (
    SELECT k.cache_key FROM build_caches k
    WHERE k.repository_key = sqlc.arg(repository_key) AND k.state = 'ready'
    ORDER BY k.created_at DESC, k.cache_key DESC
    LIMIT sqlc.arg(keep)::integer
  ))
)
RETURNING b.object_key;
