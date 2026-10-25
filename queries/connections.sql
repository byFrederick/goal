-- Connection kinds and connections (00051), under row-level security: every statement sees the
-- transaction's organization only, and the platform's kinds. The platform's kinds are written by
-- goal_sync_platform_kinds, a definer; goal db rekey reseals as goal_owner, in no organization.

-- name: ConnectionKinds :many
-- The platform's kinds and the organization's, each name's own kind first.
SELECT id, org_id, name, spec FROM connection_kinds ORDER BY name, org_id NULLS LAST;

-- name: ConnectionKind :one
-- The kind name as the organization sees it: its own before the platform's.
SELECT id, org_id, name, spec FROM connection_kinds WHERE name = sqlc.arg(name) ORDER BY org_id NULLS LAST LIMIT 1;

-- name: OrgConnectionKind :one
-- The organization's own kind name, locked for its change.
SELECT id, spec_sha256 FROM connection_kinds WHERE org_id = goal_org() AND name = sqlc.arg(name) FOR UPDATE;

-- name: PlatformKindNamed :one
SELECT EXISTS (SELECT 1 FROM connection_kinds WHERE org_id IS NULL AND name = sqlc.arg(name))::boolean AS taken;

-- name: InsertConnectionKind :exec
-- org_id comes from its default, goal_org().
INSERT INTO connection_kinds (id, name, spec, spec_sha256, created_by)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(spec), sqlc.arg(spec_sha256), sqlc.arg(created_by));

-- name: UpdateConnectionKind :exec
UPDATE connection_kinds SET spec = sqlc.arg(spec), spec_sha256 = sqlc.arg(spec_sha256), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: DeleteConnectionKind :one
DELETE FROM connection_kinds WHERE org_id = goal_org() AND name = sqlc.arg(name) RETURNING id;

-- name: SyncPlatformKinds :exec
SELECT goal_sync_platform_kinds(sqlc.arg(kinds)::jsonb);

-- name: Connections :many
SELECT c.id, c.name, k.name AS kind, c.description, c.fields, c.secret_fields, c.created_by, c.created_at, c.updated_by,
       c.updated_at, c.last_used_at, c.last_test_at, c.last_test_ok, c.last_test_error
FROM connections c JOIN connection_kinds k ON k.id = c.kind_id
ORDER BY c.name;

-- name: Connection :one
SELECT c.id, c.name, k.name AS kind, c.description, c.fields, c.secret_fields, c.created_by, c.created_at, c.updated_by,
       c.updated_at, c.last_used_at, c.last_test_at, c.last_test_ok, c.last_test_error
FROM connections c JOIN connection_kinds k ON k.id = c.kind_id
WHERE c.name = sqlc.arg(name);

-- name: SealedConnections :many
-- The named connections with their kinds and sealed values, locked: an update, or a run's use.
SELECT c.id, c.name, k.name AS kind, k.org_id AS kind_org_id, k.spec AS kind_spec, c.description, c.fields, c.secret_fields,
       c.key_id, c.nonce, c.ciphertext, c.created_by, c.created_at, c.updated_by, c.updated_at, c.last_used_at,
       c.last_test_at, c.last_test_ok, c.last_test_error
FROM connections c JOIN connection_kinds k ON k.id = c.kind_id
WHERE c.name = ANY (sqlc.arg(names)::text[])
ORDER BY c.name
FOR NO KEY UPDATE OF c;

-- name: InsertConnection :one
-- org_id comes from its default, goal_org().
INSERT INTO connections (id, name, kind_id, description, fields, secret_fields, key_id, nonce, ciphertext, created_by, updated_by)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(kind_id), sqlc.arg(description), sqlc.arg(fields), sqlc.arg(secret_fields)::text[],
        sqlc.narg(key_id), sqlc.narg(nonce), sqlc.narg(ciphertext), sqlc.arg(created_by), sqlc.arg(created_by))
RETURNING created_at;

-- name: UpdateConnection :one
UPDATE connections
SET name = sqlc.arg(name), description = sqlc.arg(description), fields = sqlc.arg(fields), secret_fields = sqlc.arg(secret_fields)::text[],
    key_id = sqlc.narg(key_id), nonce = sqlc.narg(nonce), ciphertext = sqlc.narg(ciphertext),
    updated_by = sqlc.arg(updated_by), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING updated_at;

-- name: DeleteConnection :one
WITH gone AS (DELETE FROM connections c WHERE c.name = sqlc.arg(connection)::text RETURNING c.id, c.kind_id)
SELECT gone.id, k.name AS kind FROM gone JOIN connection_kinds k ON k.id = gone.kind_id;

-- name: RecordConnectionTest :one
UPDATE connections
SET last_test_at = coalesce(sqlc.narg(at)::timestamptz, now()), last_test_ok = sqlc.arg(ok)::boolean,
    last_test_error = sqlc.narg(error)
WHERE name = sqlc.arg(name)
RETURNING id;

-- name: NotifyConnection :exec
-- The organization's stream event connection on goal_org_events, sent when the change's
-- transaction commits; postgres.Notices reads it back from the change's audit entry.
SELECT pg_notify('goal_org_events', json_build_object(
  'org_id', goal_org(), 'event', 'connection',
  'data', json_build_object('connection_id', sqlc.arg(id)::uuid, 'name', sqlc.arg(name)::text, 'change', sqlc.arg(change)::text))::text);

-- name: MarkConnectionsUsed :exec
UPDATE connections SET last_used_at = now() WHERE id = ANY (sqlc.arg(ids)::uuid[]);

-- name: ConnectionKeyIDs :one
-- The master key ids of every organization's sealed connections.
SELECT goal_connection_key_ids()::text[] AS key_ids;

-- name: StaleSealedConnections :many
-- goal db rekey, as goal_owner: a batch of connections sealed under another key than the active
-- one, in every organization, locked.
SELECT id, org_id, key_id, nonce, ciphertext FROM connections
WHERE key_id IS NOT NULL AND key_id <> sqlc.arg(active)::text
ORDER BY id
LIMIT sqlc.arg(batch)
FOR NO KEY UPDATE;

-- name: ResealConnection :exec
UPDATE connections SET key_id = sqlc.arg(key_id), nonce = sqlc.arg(nonce), ciphertext = sqlc.arg(ciphertext)
WHERE id = sqlc.arg(id);
