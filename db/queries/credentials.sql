-- Credentials (00065), under row-level security: every statement sees the transaction's
-- organization only. Who created and last changed a credential are named as their audit entries
-- name them (a person's name at that moment), as api_keys.sql does. goal db rekey reseals as
-- goal_owner, in no organization.

-- name: Credentials :many
SELECT c.id, c.name, c.type, c.description, c.all_factories, c.factory_ids, c.agents, c.expires_at, c.remind_days,
       c.last_used_at, c.created_by, c.created_at, c.updated_by, c.updated_at,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'credential' AND a.target_id = c.id::text AND a.action = 'credential.created'
                 ORDER BY a.id LIMIT 1), '')::text AS created_by_name,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'credential' AND a.target_id = c.id::text
                   AND a.action IN ('credential.created', 'credential.updated', 'credential.value_replaced')
                 ORDER BY a.id DESC LIMIT 1), '')::text AS updated_by_name
FROM credentials c
ORDER BY c.name;

-- name: Credential :one
SELECT c.id, c.name, c.type, c.description, c.all_factories, c.factory_ids, c.agents, c.expires_at, c.remind_days,
       c.last_used_at, c.created_by, c.created_at, c.updated_by, c.updated_at,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'credential' AND a.target_id = c.id::text AND a.action = 'credential.created'
                 ORDER BY a.id LIMIT 1), '')::text AS created_by_name,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'credential' AND a.target_id = c.id::text
                   AND a.action IN ('credential.created', 'credential.updated', 'credential.value_replaced')
                 ORDER BY a.id DESC LIMIT 1), '')::text AS updated_by_name
FROM credentials c
WHERE c.name = sqlc.arg(name);

-- name: CredentialNames :many
-- Which of names are credentials, for an import's dry run.
SELECT name FROM credentials WHERE name = ANY (sqlc.arg(names)::text[]);

-- name: SealedCredentials :many
-- The named credentials with their sealed values, locked: a patch, or a run's use. Locked in id
-- order, as goal db rekey locks them (StaleSealedCredentials), so the two never deadlock.
SELECT c.id, c.name, c.type, c.description, c.all_factories, c.factory_ids, c.agents, c.expires_at, c.remind_days,
       c.last_used_at, c.created_by, c.created_at, c.updated_by, c.updated_at,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'credential' AND a.target_id = c.id::text AND a.action = 'credential.created'
                 ORDER BY a.id LIMIT 1), '')::text AS created_by_name,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind = 'credential' AND a.target_id = c.id::text
                   AND a.action IN ('credential.created', 'credential.updated', 'credential.value_replaced')
                 ORDER BY a.id DESC LIMIT 1), '')::text AS updated_by_name,
       c.key_id, c.nonce, c.ciphertext
FROM credentials c
WHERE c.name = ANY (sqlc.arg(names)::text[])
ORDER BY c.id
FOR NO KEY UPDATE OF c;

-- name: InsertCredential :one
-- org_id comes from its default, goal_org().
INSERT INTO credentials (id, name, type, description, all_factories, factory_ids, agents, key_id, nonce, ciphertext,
                         expires_at, remind_days, created_by, updated_by)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(type), sqlc.arg(description), sqlc.arg(all_factories),
        sqlc.arg(factory_ids)::uuid[], sqlc.arg(agents), sqlc.arg(key_id), sqlc.arg(nonce), sqlc.arg(ciphertext),
        sqlc.narg(expires_at), sqlc.arg(remind_days), sqlc.arg(created_by), sqlc.arg(created_by))
RETURNING created_at;

-- name: UpdateCredential :one
UPDATE credentials
SET name = sqlc.arg(name), type = sqlc.arg(type), description = sqlc.arg(description), all_factories = sqlc.arg(all_factories),
    factory_ids = sqlc.arg(factory_ids)::uuid[], agents = sqlc.arg(agents), key_id = sqlc.arg(key_id), nonce = sqlc.arg(nonce),
    ciphertext = sqlc.arg(ciphertext), expires_at = sqlc.narg(expires_at), remind_days = sqlc.arg(remind_days),
    updated_by = sqlc.arg(updated_by), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING updated_at;

-- name: LockCredential :one
-- The credential name, locked for its deletion: connection writes that refer to it
-- (ShareReferencedCredentials) wait for it, and it waits for them.
SELECT id FROM credentials WHERE name = sqlc.arg(name) FOR UPDATE;

-- name: CredentialUsers :many
-- The connections that refer to credential name as {{name}} (00066's secret_refs), by name.
SELECT c.name FROM connections c
WHERE EXISTS (SELECT 1 FROM jsonb_each_text(c.secret_refs) AS r (field, credential) WHERE r.credential = sqlc.arg(name)::text)
ORDER BY c.name;

-- name: CredentialUserCounts :many
-- How many connections refer to each of names, for an import's dry run; a name no connection
-- refers to has no row.
SELECT r.credential::text AS name, count(DISTINCT c.id)::integer AS connections
FROM connections c CROSS JOIN LATERAL jsonb_each_text(c.secret_refs) AS r (field, credential)
WHERE r.credential = ANY (sqlc.arg(names)::text[])
GROUP BY r.credential;

-- name: DeleteCredential :one
DELETE FROM credentials WHERE name = sqlc.arg(name) RETURNING id;

-- name: MarkCredentialsUsed :many
-- Sets the credentials' last use, the transaction's now() for each.
UPDATE credentials SET last_used_at = now() WHERE id = ANY (sqlc.arg(ids)::uuid[]) RETURNING last_used_at;

-- name: StaleSealedCredentials :many
-- goal db rekey, as goal_owner: a batch of credentials sealed under another key than the active
-- one, in every organization, locked.
SELECT id, org_id, key_id, nonce, ciphertext FROM credentials
WHERE key_id <> sqlc.arg(active)::text
ORDER BY id
LIMIT sqlc.arg(batch)
FOR NO KEY UPDATE;

-- name: ResealCredential :exec
UPDATE credentials SET key_id = sqlc.arg(key_id), nonce = sqlc.arg(nonce), ciphertext = sqlc.arg(ciphertext)
WHERE id = sqlc.arg(id);
