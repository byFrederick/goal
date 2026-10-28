-- Standing rules (00075, ADR 0031 decision 7), under row-level security. A rule is never deleted;
-- revoking it sets revoked_at and revoked_by. Its connection's name comes from connections, NULL
-- once the connection is deleted.

-- name: StandingRules :many
SELECT r.id, r.connection_id, c.name AS connection, r.tool, r.agent, r.created_by, r.created_at, r.item_id, r.revoked_by,
       r.revoked_at
FROM standing_rules r LEFT JOIN connections c ON c.id = r.connection_id
WHERE sqlc.arg(include_revoked)::boolean OR r.revoked_at IS NULL
ORDER BY r.created_at, r.id;

-- name: InsertStandingRule :one
-- org_id comes from its default, goal_org(). No row when the key already has an active rule.
INSERT INTO standing_rules (id, connection_id, tool, agent, created_by, item_id)
VALUES (sqlc.arg(id), sqlc.arg(connection_id), sqlc.arg(tool), sqlc.arg(agent), sqlc.arg(created_by), sqlc.arg(item_id))
ON CONFLICT (org_id, connection_id, tool, agent) WHERE revoked_at IS NULL DO NOTHING
RETURNING created_at;

-- name: ActiveStandingRule :one
SELECT id, created_by, created_at, item_id FROM standing_rules
WHERE connection_id = sqlc.arg(connection_id) AND tool = sqlc.arg(tool) AND agent = sqlc.arg(agent) AND revoked_at IS NULL;

-- name: StandingRule :one
-- Rule id, locked for its revocation.
SELECT r.id, r.connection_id, c.name AS connection, r.tool, r.agent, r.created_by, r.created_at, r.item_id, r.revoked_by,
       r.revoked_at
FROM standing_rules r LEFT JOIN connections c ON c.id = r.connection_id
WHERE r.id = sqlc.arg(id)
FOR NO KEY UPDATE OF r;

-- name: RevokeStandingRule :one
UPDATE standing_rules SET revoked_by = sqlc.arg(revoked_by), revoked_at = now()
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
RETURNING revoked_at;

-- name: RevokeConnectionStandingRules :many
-- A connection's deletion revokes its active rules.
UPDATE standing_rules SET revoked_by = sqlc.arg(revoked_by), revoked_at = now()
WHERE connection_id = sqlc.arg(connection_id) AND revoked_at IS NULL
RETURNING id, tool, agent, item_id;
