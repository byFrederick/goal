-- Secrets (00085, ADR 0037), under row-level security: every statement sees the transaction's
-- organization only. Who created and last changed a secret are named as their audit entries name
-- them (a person's name at that moment), as api_keys.sql does; a secret renamed from a credential
-- keeps its id, so its credential.* entries name them until a secret.* entry does. The credentials
-- API reads and writes this table too (credentials.go), until the secrets API replaces it. goal db
-- rekey reseals as goal_owner, in no organization.

-- name: Secrets :many
SELECT s.id, s.name, s.source, s.description, s.all_factories, s.factory_ids, s.expires_at, s.remind_days,
       s.last_used_at, s.created_by, s.created_at, s.updated_by, s.updated_at,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind IN ('secret', 'credential') AND a.target_id = s.id::text
                   AND a.action IN ('secret.created', 'credential.created')
                 ORDER BY a.id LIMIT 1), '')::text AS created_by_name,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind IN ('secret', 'credential') AND a.target_id = s.id::text
                   AND a.action IN ('secret.created', 'secret.updated', 'secret.value_replaced',
                                    'credential.created', 'credential.updated', 'credential.value_replaced')
                 ORDER BY a.id DESC LIMIT 1), '')::text AS updated_by_name
FROM secrets s
ORDER BY s.name;

-- name: Secret :one
SELECT s.id, s.name, s.source, s.description, s.all_factories, s.factory_ids, s.expires_at, s.remind_days,
       s.last_used_at, s.created_by, s.created_at, s.updated_by, s.updated_at,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind IN ('secret', 'credential') AND a.target_id = s.id::text
                   AND a.action IN ('secret.created', 'credential.created')
                 ORDER BY a.id LIMIT 1), '')::text AS created_by_name,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind IN ('secret', 'credential') AND a.target_id = s.id::text
                   AND a.action IN ('secret.created', 'secret.updated', 'secret.value_replaced',
                                    'credential.created', 'credential.updated', 'credential.value_replaced')
                 ORDER BY a.id DESC LIMIT 1), '')::text AS updated_by_name
FROM secrets s
WHERE s.name = sqlc.arg(name);

-- name: SecretNames :many
-- Which of names are secrets, for an import's dry run.
SELECT name FROM secrets WHERE name = ANY (sqlc.arg(names)::text[]);

-- name: SealedSecrets :many
-- The named secrets with their sealed values, locked: a patch, or a run's use. Locked in id order,
-- as goal db rekey locks them (StaleSealedSecrets), so the two never deadlock.
SELECT s.id, s.name, s.source, s.description, s.all_factories, s.factory_ids, s.expires_at, s.remind_days,
       s.last_used_at, s.created_by, s.created_at, s.updated_by, s.updated_at,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind IN ('secret', 'credential') AND a.target_id = s.id::text
                   AND a.action IN ('secret.created', 'credential.created')
                 ORDER BY a.id LIMIT 1), '')::text AS created_by_name,
       coalesce((SELECT a.actor_name FROM audit_log a
                 WHERE a.target_kind IN ('secret', 'credential') AND a.target_id = s.id::text
                   AND a.action IN ('secret.created', 'secret.updated', 'secret.value_replaced',
                                    'credential.created', 'credential.updated', 'credential.value_replaced')
                 ORDER BY a.id DESC LIMIT 1), '')::text AS updated_by_name,
       s.key_id, s.nonce, s.ciphertext
FROM secrets s
WHERE s.name = ANY (sqlc.arg(names)::text[])
ORDER BY s.id
FOR NO KEY UPDATE OF s;

-- name: InsertSecret :one
-- org_id comes from its default, goal_org().
INSERT INTO secrets (id, name, source, description, all_factories, factory_ids, key_id, nonce, ciphertext,
                     expires_at, remind_days, created_by, updated_by)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(source), sqlc.arg(description), sqlc.arg(all_factories),
        sqlc.arg(factory_ids)::uuid[], sqlc.arg(key_id), sqlc.arg(nonce), sqlc.arg(ciphertext),
        sqlc.narg(expires_at), sqlc.arg(remind_days), sqlc.arg(created_by), sqlc.arg(created_by))
RETURNING created_at;

-- name: UpdateSecret :one
-- A secret's metadata and sealed value; its source stays.
UPDATE secrets
SET name = sqlc.arg(name), description = sqlc.arg(description), all_factories = sqlc.arg(all_factories),
    factory_ids = sqlc.arg(factory_ids)::uuid[], key_id = sqlc.arg(key_id), nonce = sqlc.arg(nonce),
    ciphertext = sqlc.arg(ciphertext), expires_at = sqlc.narg(expires_at), remind_days = sqlc.arg(remind_days),
    updated_by = sqlc.arg(updated_by), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING updated_at;

-- name: LockSecret :one
-- The secret name, locked for its deletion: connection writes that refer to it
-- (ShareReferencedCredentials) wait for it, and it waits for them.
SELECT id FROM secrets WHERE name = sqlc.arg(name) FOR UPDATE;

-- name: DeleteSecret :one
DELETE FROM secrets WHERE name = sqlc.arg(name) RETURNING id;

-- name: MarkSecretsUsed :many
-- Sets the secrets' last use, the transaction's now() for each.
UPDATE secrets SET last_used_at = now() WHERE id = ANY (sqlc.arg(ids)::uuid[]) RETURNING last_used_at;

-- name: StaleSealedSecrets :many
-- goal db rekey, as goal_owner: a batch of secrets sealed under another key than the active one,
-- in every organization, locked.
SELECT id, org_id, key_id, nonce, ciphertext FROM secrets
WHERE key_id <> sqlc.arg(active)::text
ORDER BY id
LIMIT sqlc.arg(batch)
FOR NO KEY UPDATE;

-- name: ResealSecret :exec
UPDATE secrets SET key_id = sqlc.arg(key_id), nonce = sqlc.arg(nonce), ciphertext = sqlc.arg(ciphertext)
WHERE id = sqlc.arg(id);
