-- name: InsertAudit :exec
-- org_id comes from its default, goal_org(): the transaction's organization.
INSERT INTO audit_log (at, actor_kind, actor_id, actor_name, action, target_kind, target_id, factory_id, data, request_id)
VALUES (coalesce(sqlc.narg(at)::timestamptz, now()), sqlc.arg(actor_kind), sqlc.arg(actor_id), sqlc.arg(actor_name),
        sqlc.arg(action), sqlc.arg(target_kind), sqlc.arg(target_id), sqlc.narg(factory_id), sqlc.arg(data),
        sqlc.narg(request_id));

-- name: AuditLog :many
-- Newest first, paged by id; row-level security keeps it to the transaction's organization.
-- Empty filters and a non-positive before mean no filter.
SELECT * FROM audit_log
WHERE (sqlc.arg(before)::bigint <= 0 OR id < sqlc.arg(before)::bigint)
  AND (sqlc.arg(action)::text = '' OR action = sqlc.arg(action)::text)
  AND (sqlc.arg(target_kind)::text = '' OR target_kind = sqlc.arg(target_kind)::text)
  AND (sqlc.arg(target_id)::text = '' OR target_id = sqlc.arg(target_id)::text)
  AND (sqlc.narg(factory_id)::uuid IS NULL OR factory_id = sqlc.narg(factory_id)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(max_entries)::bigint;
