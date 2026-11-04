-- +goose Up
-- Webhooks belong to the organization (ADR 0040 decisions 12, 15, 22 and 24). Expand only: the
-- factory routes and the endpoints they made stay for one release, and the conversion in goald
-- (decision 15) moves those endpoints over.
--
-- webhook_endpoints: a row with a null factory_id is the organization's webhook. Its name is
-- unique in the organization and is none of the sender names, schedule or webhook; rows that
-- still have a factory keep 00071's per-factory names, so this migration renames nothing. A
-- webhook names its sender and the organization secret that signs its deliveries. The secret is
-- referred to by id, so renaming it keeps the reference; ON UPDATE CASCADE follows its key if it
-- ever changes. A delete is NO ACTION, checked at the statement's end, as 00094's Slack setting:
-- an organization's purge removes its secrets and its webhooks in one cascade in whatever order,
-- and alone, a secret a webhook uses cannot be deleted. enabled turns a webhook off without
-- deleting it. The token sender's header, event path and id path are its own columns. The sealed
-- key_id, nonce and ciphertext are left to the factory endpoints, so they become nullable, and
-- kind becomes nullable beside sender.
--
-- webhook_openings: which factories a webhook is open to, each optionally limited to some
-- repositories (null: every one). A new webhook is open to none.
--
-- secrets: a secret a webhook uses keeps its previous value for 24 hours after a replace, sealed
-- in its own columns under domain.SecretAAD like the current one. goal_connection_key_ids lists
-- the previous key ids too, and skips the webhooks' null key ids, as 00086 does for connections.
-- Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

ALTER TABLE webhook_endpoints
  ALTER COLUMN factory_id DROP NOT NULL,
  ALTER COLUMN kind DROP NOT NULL,
  ALTER COLUMN key_id DROP NOT NULL,
  ALTER COLUMN nonce DROP NOT NULL,
  ALTER COLUMN ciphertext DROP NOT NULL,
  ADD COLUMN sender           text CHECK (sender IN ('github', 'gitlab', 'standard', 'pagerduty', 'sentry', 'token')),
  ADD COLUMN secret_id        uuid,
  ADD COLUMN enabled          bool NOT NULL DEFAULT true,
  ADD COLUMN token_header     text CHECK (token_header ~ '^[A-Za-z0-9-]{1,100}$'),
  ADD COLUMN token_event_path text CHECK (length(token_event_path) BETWEEN 1 AND 200),
  ADD COLUMN token_id_path    text CHECK (length(token_id_path) BETWEEN 1 AND 200),
  ADD CONSTRAINT webhook_endpoints_secret_fkey FOREIGN KEY (org_id, secret_id) REFERENCES secrets (org_id, id) ON UPDATE CASCADE,
  -- The sealed value is whole or absent, and something signs every row.
  ADD CONSTRAINT webhook_endpoints_sealed_check CHECK (num_nulls(key_id, nonce, ciphertext) IN (0, 3)),
  ADD CONSTRAINT webhook_endpoints_signed_check CHECK (key_id IS NOT NULL OR secret_id IS NOT NULL),
  ADD CONSTRAINT webhook_endpoints_kind_or_sender_check CHECK (kind IS NOT NULL OR sender IS NOT NULL),
  -- The organization's webhooks have a sender and a secret, and none of the reserved names.
  ADD CONSTRAINT webhook_endpoints_org_check CHECK (factory_id IS NOT NULL OR (sender IS NOT NULL AND secret_id IS NOT NULL)),
  ADD CONSTRAINT webhook_endpoints_reserved_name_check CHECK (
    factory_id IS NOT NULL OR name NOT IN ('github', 'gitlab', 'standard', 'pagerduty', 'sentry', 'token', 'schedule', 'webhook')),
  ADD CONSTRAINT webhook_endpoints_token_check CHECK (
    sender = 'token' OR num_nulls(token_header, token_event_path, token_id_path) = 3);

CREATE UNIQUE INDEX webhook_endpoints_org_name_key ON webhook_endpoints (org_id, name) WHERE factory_id IS NULL;
CREATE INDEX webhook_endpoints_secret_idx ON webhook_endpoints (org_id, secret_id) WHERE secret_id IS NOT NULL;

GRANT UPDATE (name, enabled) ON webhook_endpoints TO goal_app;

CREATE TABLE webhook_openings (
  org_id       uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  webhook_id   uuid NOT NULL,
  factory_id   uuid NOT NULL,
  repositories text[] CHECK (cardinality(repositories) BETWEEN 1 AND 100),   -- null: every repository
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, webhook_id, factory_id),                            -- also the index on org_id
  FOREIGN KEY (org_id, webhook_id) REFERENCES webhook_endpoints (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);

-- A factory's webhooks.
CREATE INDEX webhook_openings_factory_idx ON webhook_openings (org_id, factory_id);

ALTER TABLE webhook_openings ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON webhook_openings
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON webhook_openings TO goal_app;
GRANT UPDATE (repositories) ON webhook_openings TO goal_app;

ALTER TABLE secrets
  ADD COLUMN previous_key_id     text,
  ADD COLUMN previous_nonce      bytea CHECK (octet_length(previous_nonce) = 12),
  ADD COLUMN previous_ciphertext bytea CHECK (octet_length(previous_ciphertext) <= 65536 + 16),
  ADD COLUMN previous_until      timestamptz,
  ADD CONSTRAINT secrets_previous_check CHECK (num_nulls(previous_key_id, previous_nonce, previous_ciphertext, previous_until) IN (0, 4));

GRANT UPDATE (previous_key_id, previous_nonce, previous_ciphertext, previous_until) ON secrets TO goal_app;

-- goal_connection_key_ids are the master key ids some secret (its value or its previous one),
-- connection or webhook endpoint is sealed under, in every organization: goald refuses to start
-- without each of them. Ids only, never a sealed value. Replacing it keeps 00051's grants.
CREATE OR REPLACE FUNCTION goal_connection_key_ids() RETURNS text[]
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(array_agg(DISTINCT k.key_id ORDER BY k.key_id), '{}')
  FROM (SELECT key_id FROM secrets
        UNION ALL
        SELECT previous_key_id FROM secrets WHERE previous_key_id IS NOT NULL
        UNION ALL
        SELECT key_id FROM connections WHERE key_id IS NOT NULL
        UNION ALL
        SELECT key_id FROM webhook_endpoints WHERE key_id IS NOT NULL) k
$$;
