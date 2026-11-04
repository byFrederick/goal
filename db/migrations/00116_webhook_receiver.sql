-- +goose Up
-- The receiver of POST /hooks/{webhook id} (ADR 0040 decisions 12, 13 and 15).
--
-- webhook_deliveries: a delivery to an organization's webhook keeps the sender's delivery id
-- (unique per webhook; a refused one keeps none, as nothing in it is trusted), its event, its
-- receipt (accepted, duplicate or refused; null for the rows recorded before the receiver) and how
-- many later deliveries had its id. goal_app may update that count only. A refused delivery keeps
-- metadata only, so no body. The headers CHECK refuses every sender's signature or token header
-- (domain.SignatureHeaders): a token webhook's own header is dropped by the receiver, as its name
-- is the webhook's. The new checks are added NOT VALID and then validated, as 00071 does for
-- work_items.
--
-- webhook_delivery_ids: the ledger, every accepted delivery id of a webhook, without bodies, kept
-- a year (domain.LedgerRetention), so a replay is caught after its 30-day row is gone.
--
-- webhook_delivery_counts: per webhook and UTC day, how many accepted deliveries no enabled trigger
-- named; those keep only their ledger id and this count.
--
-- goal_prune_webhook_ledger deletes the ledger ids and the counts older than an instant, across
-- organizations, as goal_prune_webhook_deliveries does for deliveries. Definers pin their
-- search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

ALTER TABLE webhook_deliveries
  ADD COLUMN delivery_id text,
  ADD COLUMN event       text NOT NULL DEFAULT '',
  ADD COLUMN receipt     text,
  ADD COLUMN duplicates  int NOT NULL DEFAULT 0,
  DROP CONSTRAINT webhook_deliveries_headers_check;

ALTER TABLE webhook_deliveries
  ADD CONSTRAINT webhook_deliveries_delivery_id_check CHECK (length(delivery_id) BETWEEN 1 AND 200) NOT VALID,
  ADD CONSTRAINT webhook_deliveries_event_check CHECK (length(event) <= 200) NOT VALID,
  ADD CONSTRAINT webhook_deliveries_receipt_check CHECK (receipt IN ('accepted', 'duplicate', 'refused')) NOT VALID,
  ADD CONSTRAINT webhook_deliveries_duplicates_check CHECK (duplicates >= 0) NOT VALID,
  -- An accepted delivery has its id; a refused one has no id, event or body.
  ADD CONSTRAINT webhook_deliveries_receipt_id_check CHECK (receipt IS DISTINCT FROM 'accepted' OR delivery_id IS NOT NULL) NOT VALID,
  ADD CONSTRAINT webhook_deliveries_refused_check CHECK (
    receipt IS DISTINCT FROM 'refused' OR (delivery_id IS NULL AND event = '' AND octet_length(body) = 0)) NOT VALID,
  ADD CONSTRAINT webhook_deliveries_headers_check CHECK (
    jsonb_typeof(headers) = 'object' AND octet_length(headers::text) <= 65536
    AND NOT headers ?| ARRAY['x-hub-signature-256', 'x-hub-signature', 'x-goal-signature', 'x-gitlab-token', 'webhook-signature',
                             'x-pagerduty-signature', 'sentry-hook-signature', 'authorization']) NOT VALID;

ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_delivery_id_check;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_event_check;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_receipt_check;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_duplicates_check;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_receipt_id_check;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_refused_check;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_headers_check;

-- One row per delivery id of a webhook; nulls (refused and older rows) are distinct.
CREATE UNIQUE INDEX webhook_deliveries_delivery_id_key ON webhook_deliveries (org_id, endpoint_id, delivery_id);

GRANT UPDATE (duplicates) ON webhook_deliveries TO goal_app;

CREATE TABLE webhook_delivery_ids (
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  webhook_id  uuid NOT NULL,
  delivery_id text NOT NULL CHECK (length(delivery_id) BETWEEN 1 AND 200),
  received_at timestamptz NOT NULL,
  PRIMARY KEY (org_id, webhook_id, delivery_id),                            -- also the index on org_id
  FOREIGN KEY (org_id, webhook_id) REFERENCES webhook_endpoints (org_id, id) ON DELETE CASCADE
);

-- The year's prune across organizations.
CREATE INDEX webhook_delivery_ids_received_idx ON webhook_delivery_ids (received_at);

ALTER TABLE webhook_delivery_ids ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON webhook_delivery_ids
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON webhook_delivery_ids TO goal_app;

CREATE TABLE webhook_delivery_counts (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  webhook_id uuid NOT NULL,
  day        date NOT NULL,                                                  -- the UTC day
  count      int NOT NULL CHECK (count > 0),
  PRIMARY KEY (org_id, webhook_id, day),                                     -- also the index on org_id
  FOREIGN KEY (org_id, webhook_id) REFERENCES webhook_endpoints (org_id, id) ON DELETE CASCADE
);

-- The prune across organizations.
CREATE INDEX webhook_delivery_counts_day_idx ON webhook_delivery_counts (day);

ALTER TABLE webhook_delivery_counts ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON webhook_delivery_counts
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON webhook_delivery_counts TO goal_app;
GRANT UPDATE (count) ON webhook_delivery_counts TO goal_app;

-- goal_prune_webhook_ledger deletes the ledger ids received before p_before and the counts of
-- days before it, in every organization, and returns how many rows went.
CREATE FUNCTION goal_prune_webhook_ledger(p_before timestamptz) RETURNS bigint
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path FROM CURRENT
AS $$
  WITH ids AS (DELETE FROM webhook_delivery_ids WHERE received_at < p_before RETURNING 1),
       counts AS (DELETE FROM webhook_delivery_counts WHERE day < (p_before AT TIME ZONE 'UTC')::date RETURNING 1)
  SELECT (SELECT count(*) FROM ids) + (SELECT count(*) FROM counts)
$$;

REVOKE ALL ON FUNCTION goal_prune_webhook_ledger(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION goal_prune_webhook_ledger(timestamptz) TO goal_app;
