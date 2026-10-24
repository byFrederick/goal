-- name: Principal :one
-- Who the session token signs in, with their role in the organization (NULL: unknown or expired).
SELECT goal_principal(sqlc.arg(token)::text, sqlc.narg(org_id)::uuid)::jsonb AS principal;

-- name: OrgsFor :one
SELECT goal_orgs_for(sqlc.arg(user_id)::uuid)::jsonb AS orgs;

-- name: DeviceRequest :one
SELECT goal_device_request(sqlc.arg(user_code)::text)::jsonb AS request;

-- name: IdentityReady :one
SELECT goal_identity_ready()::boolean AS ready;

-- name: PlatformAdmins :one
-- The install's platform admins, earliest first.
SELECT goal_platform_admins()::jsonb AS admins;
