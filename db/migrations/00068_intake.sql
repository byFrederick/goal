-- +goose Up
-- Intake triggers' state (ADR 0026 decisions 2 to 4): webhook endpoints with their sealed signing
-- secrets, the deliveries each endpoint received (kept 30 days), and the log of trigger firings,
-- unique on (factory, trigger, idempotency key) so a second attempt is a no-op. All three are org
-- rows under row-level security with composite keys to their factory and item, since foreign keys
-- ignore row-level security (review 2.2 M3). A secret is sealed with AES-256-GCM under the platform
-- master key; the associated data binds organization, endpoint id and key id
-- (domain.WebhookSecretAAD), so a ciphertext copied to another row does not open. A delivery's
-- headers never hold a signature. work_items gains the trigger that created an item and where it
-- came from. goal_connection_key_ids now returns endpoints' key ids too (00065 stays as it was
-- applied). Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE webhook_endpoints (
  id         uuid PRIMARY KEY,                                             -- UUIDv7 from goald; the address /hooks/{id}
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id uuid NOT NULL,
  name       text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  kind       text NOT NULL CHECK (kind IN ('generic', 'github')),
  key_id     text NOT NULL,
  nonce      bytea NOT NULL CHECK (octet_length(nonce) = 12),
  ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) <= 64 + 16),  -- a 32-byte secret and its tag, with room
  created_at timestamptz NOT NULL DEFAULT now(),
  rotated_at timestamptz,
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  UNIQUE (org_id, factory_id, name),                                       -- also the index on org_id
  UNIQUE (org_id, id)
);

ALTER TABLE webhook_endpoints ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON webhook_endpoints
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON webhook_endpoints TO goal_app;
GRANT UPDATE (key_id, nonce, ciphertext, rotated_at) ON webhook_endpoints TO goal_app;

CREATE TABLE webhook_deliveries (
  id          uuid PRIMARY KEY,                                            -- UUIDv7 from goald
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  endpoint_id uuid NOT NULL,
  received_at timestamptz NOT NULL,
  -- Header names lower-cased to their values; a signature is the request's authentication and is
  -- never kept.
  headers     jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(headers) = 'object' AND octet_length(headers::text) <= 65536
                                                 AND NOT headers ?| ARRAY['x-goal-signature', 'x-hub-signature-256', 'x-hub-signature']),
  body        bytea NOT NULL DEFAULT '' CHECK (octet_length(body) <= 1048576),
  outcome     text NOT NULL CHECK (outcome IN ('refused', 'accepted', 'created')),
  reason      text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
  item_id     uuid CHECK (item_id IS NULL OR outcome = 'created'),
  FOREIGN KEY (org_id, endpoint_id) REFERENCES webhook_endpoints (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE SET NULL (item_id)
);

-- An endpoint's log, newest first; and the 30-day prune across organizations.
CREATE INDEX webhook_deliveries_endpoint_idx ON webhook_deliveries (endpoint_id, received_at DESC, id DESC);
CREATE INDEX webhook_deliveries_received_idx ON webhook_deliveries (received_at);
CREATE INDEX webhook_deliveries_org_idx ON webhook_deliveries (org_id, endpoint_id);

ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON webhook_deliveries
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON webhook_deliveries TO goal_app;                    -- the prune is goal_prune_webhook_deliveries()

CREATE TABLE trigger_firings (
  id              uuid PRIMARY KEY,                                        -- UUIDv7 from goald
  org_id          uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id      uuid NOT NULL,
  trigger         text NOT NULL CHECK (trigger ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  source          text NOT NULL CHECK (source IN ('schedule', 'webhook', 'github')),
  event_key       text NOT NULL CHECK (length(event_key) BETWEEN 1 AND 200),
  -- The event's id, or the scheduled instant: one firing per (factory, trigger, key).
  idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 500),
  -- The value of the trigger's only.dedupe field in the event, '' without one.
  dedupe          text NOT NULL DEFAULT '' CHECK (length(dedupe) <= 500),
  outcome         text NOT NULL CHECK (outcome IN ('created', 'skipped')),
  reason          text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
  item_id         uuid CHECK (item_id IS NULL OR outcome = 'created'),
  at              timestamptz NOT NULL,
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE SET NULL (item_id),
  UNIQUE (org_id, factory_id, trigger, idempotency_key)                    -- also the index on org_id
);

-- perDay's count and the trigger page's recent events, newest first; dedupe's lookup.
CREATE INDEX trigger_firings_trigger_at_idx ON trigger_firings (factory_id, trigger, at DESC);
CREATE INDEX trigger_firings_dedupe_idx ON trigger_firings (factory_id, trigger, dedupe) WHERE outcome = 'created' AND dedupe <> '';

ALTER TABLE trigger_firings ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON trigger_firings
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON trigger_firings TO goal_app;

-- Where an item came from, for Intake; NULL until its creator says. Each check is added NOT VALID
-- and then validated, so the large table is never locked for a scan.
ALTER TABLE work_items ADD COLUMN trigger text, ADD COLUMN source text;
ALTER TABLE work_items
  ADD CONSTRAINT work_items_trigger_check CHECK (trigger ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$') NOT VALID,
  ADD CONSTRAINT work_items_source_check CHECK (source IN ('people', 'cli', 'api', 'schedule', 'webhook', 'github')) NOT VALID;
ALTER TABLE work_items VALIDATE CONSTRAINT work_items_trigger_check;
ALTER TABLE work_items VALIDATE CONSTRAINT work_items_source_check;
-- A trigger's items over a window (TriggerStats).
CREATE INDEX work_items_trigger_idx ON work_items (factory_id, trigger, created_at) WHERE trigger IS NOT NULL;

-- goal_connection_key_ids are the master key ids some connection, credential or webhook endpoint
-- is sealed under, in every organization: goald refuses to start without each of them (spec 4
-- §5.2). Ids only, never a sealed value. Replacing it keeps 00051's grants.
CREATE OR REPLACE FUNCTION goal_connection_key_ids() RETURNS text[]
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  SELECT coalesce(array_agg(DISTINCT k.key_id ORDER BY k.key_id), '{}')
  FROM (SELECT key_id FROM connections WHERE key_id IS NOT NULL
        UNION ALL
        SELECT key_id FROM credentials
        UNION ALL
        SELECT key_id FROM webhook_endpoints) k
$$;

-- goal_webhook_endpoint_org is the organization of endpoint p_id, NULL for none: POST /hooks/{id}
-- carries no session, so goald learns the organization from the endpoint before it reads anything
-- under row-level security. The id only, never a row.
CREATE FUNCTION goal_webhook_endpoint_org(p_id uuid) RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER SET search_path FROM CURRENT
AS $$ SELECT org_id FROM webhook_endpoints WHERE id = p_id $$;

-- goal_prune_webhook_deliveries deletes the deliveries of every organization received before
-- p_before (ADR 0026: kept 30 days) and returns how many.
CREATE FUNCTION goal_prune_webhook_deliveries(p_before timestamptz) RETURNS bigint
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  WITH gone AS (DELETE FROM webhook_deliveries WHERE received_at < p_before RETURNING 1)
  SELECT count(*) FROM gone
$$;

REVOKE ALL ON FUNCTION goal_webhook_endpoint_org(uuid), goal_prune_webhook_deliveries(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_webhook_endpoint_org(uuid), goal_prune_webhook_deliveries(timestamptz) TO goal_app;
