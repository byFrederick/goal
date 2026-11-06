-- Standing rules (00075, ADR 0031 decision 7), under row-level security. A rule is never deleted;
-- revoking it sets revoked_at and revoked_by. A rule is keyed by its factory, environment and
-- server (00091; 00133 dropped the connection key).

-- name: StandingRules :many
SELECT r.id, r.factory_id, r.environment, r.server, r.tool, r.agent, r.created_by, r.created_at, r.item_id, r.revoked_by,
       r.revoked_at
FROM standing_rules r
WHERE sqlc.arg(include_revoked)::boolean OR r.revoked_at IS NULL
ORDER BY r.created_at, r.id;

-- name: InsertServerStandingRule :one
-- A rule keyed by factory, environment and server. No row when the key already has an active rule.
INSERT INTO standing_rules (id, factory_id, environment, server, tool, agent, created_by, item_id)
VALUES (sqlc.arg(id), sqlc.arg(factory_id), sqlc.arg(environment), sqlc.arg(server), sqlc.arg(tool), sqlc.arg(agent),
        sqlc.arg(created_by), sqlc.arg(item_id))
ON CONFLICT (org_id, factory_id, environment, server, tool, agent) WHERE revoked_at IS NULL AND server IS NOT NULL DO NOTHING
RETURNING created_at;

-- name: ActiveServerStandingRule :one
SELECT id, created_by, created_at, item_id FROM standing_rules
WHERE factory_id = sqlc.arg(factory_id) AND environment = sqlc.arg(environment) AND server = sqlc.arg(server)
  AND tool = sqlc.arg(tool) AND agent = sqlc.arg(agent) AND revoked_at IS NULL;

-- name: StandingRule :one
-- Rule id, locked for its revocation.
SELECT r.id, r.factory_id, r.environment, r.server, r.tool, r.agent, r.created_by, r.created_at, r.item_id, r.revoked_by,
       r.revoked_at
FROM standing_rules r
WHERE r.id = sqlc.arg(id)
FOR NO KEY UPDATE OF r;

-- name: RevokeStandingRule :one
UPDATE standing_rules SET revoked_by = sqlc.arg(revoked_by), revoked_at = now()
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
RETURNING revoked_at;
