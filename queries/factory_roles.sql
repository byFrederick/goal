-- Members' factory roles (00043), under row-level security: every statement sees and writes the
-- transaction's organization only.

-- name: LockFactoryRole :one
-- A member's role on a factory, locked until the change commits.
SELECT role FROM factory_roles
WHERE factory_id = sqlc.arg(factory_id) AND user_id = sqlc.arg(user_id)
FOR UPDATE;

-- name: PutFactoryRole :exec
INSERT INTO factory_roles (factory_id, user_id, role, granted_by)
VALUES (sqlc.arg(factory_id), sqlc.arg(user_id), sqlc.arg(role), sqlc.arg(granted_by))
ON CONFLICT (org_id, factory_id, user_id)
DO UPDATE SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, granted_at = now();

-- name: DeleteFactoryRole :exec
DELETE FROM factory_roles WHERE factory_id = sqlc.arg(factory_id) AND user_id = sqlc.arg(user_id);

-- name: FactoryExists :one
SELECT EXISTS (SELECT 1 FROM factories WHERE id = sqlc.arg(factory_id)) AS found;
