-- +goose Up
-- Checks (ADR 0040 decision 19): a schedule trigger with check: does not file an item when it
-- fires. Its firing queues a check instead (outcome 'checked'), which a loop on every goald
-- leases (FOR UPDATE SKIP LOCKED, fenced by lease_token), runs as an agent run of kind 'check'
-- with no item, and ends done with the answer's decisions or failed with the reason. One check
-- per trigger is in flight at a time: a firing while one is queued or running is skipped, "still
-- checking", decided under the trigger's lock in Fire, and trigger_firings_check_live_idx keeps
-- it so. A check whose lease ran out is failed, never claimed again.
--
-- The saved state a check hands the next one is in trigger_state, one row per trigger, at most
-- 4 KiB. Rows of both tables go with their factory's key, so goal_purge_org (00042) and
-- goal_delete_factory (00061) need no change; a check's run key sets null.
ALTER TABLE agent_runs DROP CONSTRAINT agent_runs_kind_check;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_kind_check CHECK (kind IN ('station', 'jonah', 'try', 'check')) NOT VALID;
ALTER TABLE agent_runs VALIDATE CONSTRAINT agent_runs_kind_check;
ALTER TABLE agent_runs DROP CONSTRAINT agent_runs_item_check;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_item_check CHECK (item_id IS NOT NULL OR kind IN ('jonah', 'try', 'check')) NOT VALID;
ALTER TABLE agent_runs VALIDATE CONSTRAINT agent_runs_item_check;

ALTER TABLE trigger_firings
  ADD COLUMN check_state  text CHECK (check_state IN ('queued', 'running', 'done', 'failed')),
  ADD COLUMN lease_holder text NOT NULL DEFAULT '' CHECK (length(lease_holder) <= 200),
  ADD COLUMN lease_token  uuid,
  ADD COLUMN lease_until  timestamptz,
  ADD COLUMN run_id       uuid,
  ADD COLUMN cost_usd     numeric(12, 4) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
  ADD COLUMN decisions    jsonb CHECK (decisions IS NULL OR jsonb_typeof(decisions) = 'object'),
  ADD COLUMN checked_at   timestamptz,
  ADD CONSTRAINT trigger_firings_run_fkey FOREIGN KEY (org_id, run_id) REFERENCES agent_runs (org_id, id) ON DELETE SET NULL (run_id);

ALTER TABLE trigger_firings DROP CONSTRAINT trigger_firings_outcome_check;
ALTER TABLE trigger_firings ADD CONSTRAINT trigger_firings_outcome_check
  CHECK (outcome IN ('created', 'skipped', 'missed', 'checked')) NOT VALID;
ALTER TABLE trigger_firings VALIDATE CONSTRAINT trigger_firings_outcome_check;

-- A checked firing has a check state and only it; a running one has a lease and only it; a
-- finished one when it finished, a done one its decisions.
ALTER TABLE trigger_firings ADD CONSTRAINT trigger_firings_check_check CHECK (
  (outcome = 'checked') = (check_state IS NOT NULL)
  AND (check_state = 'running') = (lease_until IS NOT NULL AND lease_token IS NOT NULL)
  AND (check_state IN ('done', 'failed')) = (checked_at IS NOT NULL)
  AND (check_state = 'done') = (decisions IS NOT NULL)
) NOT VALID;
ALTER TABLE trigger_firings VALIDATE CONSTRAINT trigger_firings_check_check;

-- One check in flight per trigger; and the claim's queued and running checks of a factory.
CREATE UNIQUE INDEX trigger_firings_check_live_idx ON trigger_firings (org_id, factory_id, trigger)
  WHERE check_state IN ('queued', 'running');
-- The trigger's newest done check, whose decisions the next one gets; the run key's cascade.
CREATE INDEX trigger_firings_check_done_idx ON trigger_firings (org_id, factory_id, trigger, checked_at DESC)
  WHERE check_state = 'done';
CREATE INDEX trigger_firings_run_idx ON trigger_firings (org_id, run_id) WHERE run_id IS NOT NULL;
-- An item's firing, whose dedupe value is the key a check is given it by.
CREATE INDEX trigger_firings_item_idx ON trigger_firings (org_id, item_id) WHERE item_id IS NOT NULL;

GRANT UPDATE (check_state, lease_holder, lease_token, lease_until, run_id, cost_usd, decisions, checked_at, reason)
  ON trigger_firings TO goal_app;

CREATE TABLE trigger_state (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id uuid NOT NULL,
  trigger    text NOT NULL CHECK (trigger ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  -- What the trigger's last done check saved for the next one, at most 4 KiB of JSON.
  state      jsonb NOT NULL CHECK (octet_length(state::text) <= 4096),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (org_id, factory_id, trigger),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);

ALTER TABLE trigger_state ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON trigger_state
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON trigger_state TO goal_app;
GRANT UPDATE (state, updated_at) ON trigger_state TO goal_app;
