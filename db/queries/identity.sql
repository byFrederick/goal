-- name: Principal :one
-- Who the session token signs in, with their role in the organization (NULL: unknown or expired).
SELECT goal_principal(sqlc.arg(token)::text, sqlc.narg(org_id)::uuid)::jsonb AS principal;

-- name: OrgsFor :one
SELECT goal_orgs_for(sqlc.arg(user_id)::uuid)::jsonb AS orgs;

-- name: DeviceRequest :one
SELECT goal_device_request(sqlc.arg(user_code)::text)::jsonb AS request;

-- name: IdentityReady :one
SELECT goal_identity_ready()::boolean AS ready;

-- name: Members :one
-- The members of the transaction's organization, by name (goal_members, 00045).
SELECT goal_members()::jsonb AS members;

-- name: Invitations :one
-- The pending, unexpired invitations of the transaction's organization (goal_invitations, 00045).
SELECT goal_invitations()::jsonb AS invitations;

-- name: TransferOwnership :exec
-- Makes the admin to_user an owner and the owner from_user an admin of the transaction's
-- organization, audited (goal_transfer_ownership, 00042).
SELECT goal_transfer_ownership(sqlc.arg(from_user)::uuid, sqlc.arg(to_user)::uuid, sqlc.arg(by_ref)::text, sqlc.narg(request_id)::text);

-- name: PlatformAdmins :one
-- The install's platform admins, earliest first.
SELECT goal_platform_admins()::jsonb AS admins;
