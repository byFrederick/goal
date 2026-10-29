-- name: SetOrg :exec
-- Binds the rest of the transaction to one organization for row-level security (goal_org()).
SELECT set_config('goal.org_id', sqlc.arg(org_id)::text, true);

-- name: Orgs :many
SELECT id, slug, name, mark, max_slots, created_at FROM goal_orgs()
ORDER BY name, id;

-- name: CountFactories :one
-- The factories of the transaction's organization (row-level security).
SELECT count(*) FROM factories;

-- name: OrgByRef :one
SELECT id, slug, name, mark, max_slots, created_at FROM goal_org_by_ref(sqlc.arg(ref)::text);

-- name: ActiveOrgs :many
SELECT id::uuid FROM goal_active_orgs() AS id;

-- name: DeletedOrgs :many
SELECT id::uuid FROM goal_deleted_orgs(sqlc.arg(before)::timestamptz, sqlc.arg(max_orgs)::integer) AS id;

-- name: PurgeOrg :one
-- False when the organization is not deleted before the cutoff or another purge holds it.
SELECT goal_purge_org(sqlc.arg(org_id)::uuid, sqlc.arg(before)::timestamptz)::boolean AS purged;

-- name: ProvisionOrg :one
-- The organization, its auth.organization row and (with an owner) the owner's membership, with
-- org.created by the actor ref, in this transaction (00042).
SELECT id, slug, name, mark, max_slots, created_at
FROM goal_provision_org(sqlc.arg(id)::uuid, sqlc.arg(slug)::text, sqlc.arg(name)::text, sqlc.arg(mark)::text,
                        sqlc.narg(owner)::uuid, sqlc.narg(owner_member)::uuid, sqlc.arg(by)::text,
                        sqlc.narg(request_id)::text);

-- name: CurrentOrg :one
-- The live organization of the transaction, locked for an update.
SELECT * FROM organizations WHERE id = goal_org() AND deleted_at IS NULL FOR NO KEY UPDATE;

-- name: UpdateOrg :one
UPDATE organizations SET slug = sqlc.arg(slug), name = sqlc.arg(name), mark = sqlc.arg(mark),
  max_slots = sqlc.narg(max_slots), updated_at = now()
WHERE id = goal_org() RETURNING *;

-- name: SoftDeleteOrg :exec
UPDATE organizations SET deleted_at = $1, updated_at = now() WHERE id = goal_org();

-- name: NotifyOrgDeleted :exec
-- Tells every goald, on goal_definitions, that the transaction's organization was deleted (kind
-- org): its lines stop at once.
SELECT pg_notify('goal_definitions', json_build_object('org_id', goal_org(), 'kind', 'org')::text);

-- name: SignupDomain :one
-- The transaction's organization's sign-up domain (ADR 0023, decision 7).
SELECT domain, join_role, set_by, set_at FROM org_signup_domains WHERE org_id = goal_org();

-- name: SetSignupDomain :exec
-- Sets (a domain) or clears (null) the transaction's organization's sign-up domain for the owner
-- acting_user, audited (goal_set_signup_domain, 00081).
SELECT goal_set_signup_domain(sqlc.arg(acting_user)::uuid, sqlc.narg(domain)::text, sqlc.arg(join_role)::text,
                              sqlc.arg(by_ref)::text, sqlc.narg(request_id)::text);
