-- +goose Up
-- Deliveries evaluated for each factory that names them (ADR 0040 decisions 13 and 16).
--
-- delivery_evaluations: the receiver adds one row, in the receipt's transaction, for each factory
-- a webhook is open to with an enabled trigger that names the delivery's webhook or sender and
-- event, whose opening admits its repository. A delivery from a repository the factory doesn't
-- have is done and skipped at once, with the reason (ADR 0041 decision 13). Every goald's loop
-- claims due pending rows with FOR UPDATE SKIP LOCKED and a lease (lease_holder, lease_until), as
-- the notification outbox does, and evaluates each: done with its outcome (created with the item,
-- or skipped with the reason), or pending again after a backoff, and failed with the error after
-- five attempts. A row whose lease ran out is claimed again. Rows go with their delivery after 30
-- days (goal_prune_webhook_deliveries).
--
-- goal_app gains UPDATE on delivery_evaluations, for the claim and its end, and DELETE on
-- trigger_firings, for pruning (software#654).
CREATE TABLE delivery_evaluations (
  id              uuid PRIMARY KEY,                                        -- UUIDv7 from goald
  org_id          uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  delivery_row    uuid NOT NULL REFERENCES webhook_deliveries (id) ON DELETE CASCADE,
  factory_id      uuid NOT NULL,
  -- The triggers that named the delivery when it came, for the log.
  triggers        text[] NOT NULL CHECK (cardinality(triggers) BETWEEN 1 AND 100),
  state           text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'done', 'failed')),
  attempts        int NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL,
  lease_holder    text CHECK (length(lease_holder) BETWEEN 1 AND 100),
  lease_until     timestamptz,
  outcome         text CHECK (outcome IN ('created', 'skipped', 'failed')),
  -- Why it was skipped or failed, or the last attempt's error while it is pending.
  reason          text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
  item_id         uuid,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  -- A pending row has no outcome; a done one is created or skipped, a failed one failed.
  CHECK ((state = 'pending' AND outcome IS NULL)
      OR (state = 'done' AND outcome IN ('created', 'skipped'))
      OR (state = 'failed' AND outcome = 'failed')),
  CHECK (item_id IS NULL OR outcome = 'created'),
  CHECK ((lease_holder IS NULL) = (lease_until IS NULL)),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE SET NULL (item_id),
  UNIQUE (org_id, delivery_row, factory_id)                                -- also the index on org_id
);

-- The loop's claim: an organization's due pending rows, soonest first.
CREATE INDEX delivery_evaluations_due_idx ON delivery_evaluations (org_id, next_attempt_at, id) WHERE state = 'pending';

ALTER TABLE delivery_evaluations ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON delivery_evaluations
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, UPDATE ON delivery_evaluations TO goal_app;
GRANT DELETE ON trigger_firings TO goal_app;
