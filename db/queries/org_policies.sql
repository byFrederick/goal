-- The organization policy (00069), under row-level security: every statement sees the
-- transaction's organization only. Who last saved it is named as the audit entry names them (a
-- person's name at that moment), as credentials.sql does.

-- name: OrgPolicy :one
SELECT p.allowed_models, p.default_model, p.denied_commands, p.write_outside_branch, p.merge_pull_request, p.read_web,
       p.use_credentials, p.box_time_limit_minutes, p.revision, p.updated_by, p.updated_at,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'org_policy' AND a.target_id = p.org_id::text AND a.action = 'org_policy.updated'
                 ORDER BY a.id DESC LIMIT 1), '')::text AS updated_by_name
FROM org_policies p;

-- name: InsertOrgPolicy :one
-- The organization's first policy, at revision 1; no row when another save got there first.
INSERT INTO org_policies (allowed_models, default_model, denied_commands, write_outside_branch, merge_pull_request,
                          read_web, use_credentials, box_time_limit_minutes, updated_by)
VALUES (sqlc.arg(allowed_models), sqlc.arg(default_model), sqlc.arg(denied_commands), sqlc.arg(write_outside_branch),
        sqlc.arg(merge_pull_request), sqlc.arg(read_web), sqlc.arg(use_credentials), sqlc.arg(box_time_limit_minutes),
        sqlc.arg(updated_by))
ON CONFLICT (org_id) DO NOTHING
RETURNING revision, updated_at;

-- name: UpdateOrgPolicy :one
-- Saves the policy at the next revision when it is at revision; no row when it is not.
UPDATE org_policies
SET allowed_models = sqlc.arg(allowed_models), default_model = sqlc.arg(default_model),
    denied_commands = sqlc.arg(denied_commands), write_outside_branch = sqlc.arg(write_outside_branch),
    merge_pull_request = sqlc.arg(merge_pull_request), read_web = sqlc.arg(read_web),
    use_credentials = sqlc.arg(use_credentials), box_time_limit_minutes = sqlc.arg(box_time_limit_minutes),
    revision = revision + 1, updated_by = sqlc.arg(updated_by), updated_at = now()
WHERE revision = sqlc.arg(revision)
RETURNING revision, updated_at;
