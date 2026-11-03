-- +goose Up
-- Secrets (ADR 0037 decisions 1, 2 and 18): 00065's credentials become secrets, the only place a
-- value lives. A row keeps its id, name, scope, expiry, reminder, use and sealed value: the
-- associated data binds organization, id and key id and is the same bytes as before
-- (domain.SecretAAD), so every value still opens. source records where a value comes from; every
-- value is pasted or imported today ('value'), and a later source gets its own ADR. The type label
-- and the agent list go away: a secret is what it is used as, and an environment file grants it
-- (decision 5). The credentials API reads this table until the secrets API replaces it.
--
-- goal_connection_key_ids reads secrets in place of credentials. It still reads connections and
-- webhook endpoints, which hold sealed values too, until the conversion of decision 19 drops the
-- connection tables. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

ALTER TABLE credentials RENAME TO secrets;
ALTER TABLE secrets RENAME CONSTRAINT credentials_pkey TO secrets_pkey;
ALTER TABLE secrets RENAME CONSTRAINT credentials_org_id_name_key TO secrets_org_id_name_key;
ALTER TABLE secrets RENAME CONSTRAINT credentials_org_id_fkey TO secrets_org_id_fkey;

ALTER TABLE secrets ADD COLUMN source text NOT NULL DEFAULT 'value' CHECK (source IN ('value'));
ALTER TABLE secrets ALTER COLUMN source DROP DEFAULT;

ALTER TABLE secrets DROP COLUMN type, DROP COLUMN agents;

-- Dropping a column drops its grants; the rest are granted again by name, so the list says what
-- goal_app may change. source is not among them: a secret keeps its source.
REVOKE UPDATE ON secrets FROM goal_app;
GRANT UPDATE (name, description, all_factories, factory_ids, key_id, nonce, ciphertext, expires_at, remind_days,
              last_used_at, updated_by, updated_at) ON secrets TO goal_app;

-- goal_connection_key_ids are the master key ids some secret, connection or webhook endpoint is
-- sealed under, in every organization: goald refuses to start without each of them. Ids only,
-- never a sealed value. Replacing it keeps 00051's grants.
CREATE OR REPLACE FUNCTION goal_connection_key_ids() RETURNS text[]
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(array_agg(DISTINCT k.key_id ORDER BY k.key_id), '{}')
  FROM (SELECT key_id FROM secrets
        UNION ALL
        SELECT key_id FROM connections WHERE key_id IS NOT NULL
        UNION ALL
        SELECT key_id FROM webhook_endpoints) k
$$;
