-- +goose Up
-- Credentials (ADR 0017 decisions 1, 3 and 4): an organization's named secrets, each with a type,
-- a factory scope (all factories, ones created later included, or the listed ids), agents per
-- factory, an expiry and a rotation reminder. A credential is an org row under row-level security;
-- its value is sealed with AES-256-GCM under the platform master key (key_id, nonce, ciphertext;
-- the associated data binds organization, credential id and key id, so a ciphertext copied to
-- another row does not open). No column holds a value in the clear. goal_connection_key_ids now
-- returns credentials' key ids too, so goald refuses to start without a key a credential is
-- sealed under (00051 stays as it was applied). Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE credentials (
  id            uuid PRIMARY KEY,                                          -- UUIDv7 from goald
  org_id        uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  name          text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  type          text NOT NULL CHECK (type IN ('api_key', 'oauth_token', 'ssh_key', 'cloud_role', 'app_key')),
  description   text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  all_factories boolean NOT NULL DEFAULT false,
  factory_ids   uuid[] NOT NULL DEFAULT '{}' CHECK (cardinality(factory_ids) <= 1000),
  CHECK (NOT all_factories OR cardinality(factory_ids) = 0),
  -- Factory id to agent names: {"<factory id>": ["backend", …]}.
  agents        jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(agents) = 'object' AND octet_length(agents::text) <= 65536),
  key_id        text NOT NULL,
  nonce         bytea NOT NULL CHECK (octet_length(nonce) = 12),
  ciphertext    bytea NOT NULL CHECK (octet_length(ciphertext) <= 65536 + 16),   -- a 64 KiB value and its tag
  expires_at    timestamptz,
  remind_days   integer NOT NULL DEFAULT 0 CHECK (remind_days >= 0 AND (remind_days = 0 OR expires_at IS NOT NULL)),
  last_used_at  timestamptz,
  created_by    text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),     -- Actor.Ref()
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_by    text NOT NULL CHECK (updated_by ~ '^(user|service|agent|system):.+$'),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, name)                                                    -- also the index on org_id
);

ALTER TABLE credentials ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON credentials
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON credentials TO goal_app;
GRANT UPDATE (name, type, description, all_factories, factory_ids, agents, key_id, nonce, ciphertext, expires_at,
              remind_days, last_used_at, updated_by, updated_at) ON credentials TO goal_app;

-- goal_connection_key_ids are the master key ids some connection or credential is sealed under,
-- in every organization: goald refuses to start without each of them (spec 4 §5.2). Ids only,
-- never a sealed value. Replacing it keeps 00051's grants.
CREATE OR REPLACE FUNCTION goal_connection_key_ids() RETURNS text[]
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(array_agg(DISTINCT k.key_id ORDER BY k.key_id), '{}')
  FROM (SELECT key_id FROM connections WHERE key_id IS NOT NULL
        UNION ALL
        SELECT key_id FROM credentials) k
$$;
