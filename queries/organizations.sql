-- name: SetOrg :exec
-- Binds the rest of the transaction to one organization for row-level security (goal_org()).
SELECT set_config('goal.org_id', sqlc.arg(org_id)::text, true);

-- name: Orgs :many
SELECT id, slug, name, mark, max_slots, created_at, goal_org_factory_count(id)::bigint AS factory_count FROM goal_orgs()
ORDER BY name, id;

-- name: OrgByRef :one
SELECT id, slug, name, mark, max_slots, created_at FROM goal_org_by_ref(sqlc.arg(ref)::text);

-- name: ActiveOrgs :many
SELECT id::uuid FROM goal_active_orgs() AS id;

-- name: DeletedOrgs :many
SELECT id::uuid FROM goal_deleted_orgs(sqlc.arg(before)::timestamptz, sqlc.arg(max_orgs)::integer) AS id;

-- name: TryLockOrgPurge :one
-- One purge at a time across goald instances; released when the purge's transaction ends.
SELECT pg_try_advisory_xact_lock(hashtextextended('goal.orgs.purge', 0)) AS locked;

-- name: PurgeOrg :exec
SELECT goal_purge_org(sqlc.arg(org_id)::uuid);

-- name: InsertOrg :one
INSERT INTO organizations (id, slug, name, mark) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: CurrentOrg :one
-- The live organization of the transaction, locked for an update.
SELECT * FROM organizations WHERE id = goal_org() AND deleted_at IS NULL FOR NO KEY UPDATE;

-- name: UpdateOrg :one
UPDATE organizations SET slug = sqlc.arg(slug), name = sqlc.arg(name), mark = sqlc.arg(mark),
  max_slots = sqlc.narg(max_slots), updated_at = now()
WHERE id = goal_org() RETURNING *;

-- name: SoftDeleteOrg :exec
UPDATE organizations SET deleted_at = $1, updated_at = now() WHERE id = goal_org();
