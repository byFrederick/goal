-- name: AddPlatformAdmin :one
-- Makes the user with this email a platform admin (NULL: no such user).
SELECT goal_add_platform_admin(sqlc.arg(email)::text, sqlc.arg(added_by)::text)::jsonb AS admin;

-- name: RemovePlatformAdmin :exec
-- no_data_found for a non-admin; the last one fails at commit (platform_admins_keep_one).
SELECT goal_remove_platform_admin(sqlc.arg(user_id)::uuid, sqlc.arg(removed_by)::text);

-- name: AuthEvents :one
-- One page of the security log, newest first.
SELECT goal_auth_events(sqlc.narg(before)::bigint, sqlc.narg(page_limit)::integer, sqlc.narg(kind)::text)::jsonb AS events;

-- name: TryAuthCleanUpLock :one
-- One goald cleans up at a time; the lock ends with the transaction.
SELECT pg_try_advisory_xact_lock(hashtext('goal.auth.cleanup')) AS locked;

-- name: CleanUpAuth :one
SELECT goal_auth_cleanup(sqlc.arg(now)::timestamptz)::jsonb AS counts;
