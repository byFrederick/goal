-- +goose Up
-- Tries of past items (ADR 0039 decisions 9 and 11): before anyone sees a proposal, Jonah reruns
-- the station it reaches on items it cites, from the commit and brief their run recorded (00085),
-- in a box with nothing but the model key. A try's own run is of kind try and works no item, so it
-- adds nothing to the past item's cost or history; its cost is Jonah's (JonahSpendSince), never
-- the line's (FactorySpendSince).
--
-- A try is queued, claimed running under lease_until (FOR UPDATE SKIP LOCKED; a dead instance's
-- try is claimed again with attempts + 1) and finished done. A done try has no verdict until it is
-- graded, or error when its run could not be carried out: a commit that is gone or any other
-- infrastructure failure, never fail. Grading (fixed, not_fixed, unknown) is a later migration's.
--
-- Rows are never deleted but by the purges: the factory, proposal, item and original run keys
-- cascade, and the try run's sets null, so goal_purge_org (00042) and goal_delete_factory (00061)
-- remove them and need no change. goal_app inserts and updates the columns a claim and a finish
-- change, and never deletes.
ALTER TABLE agent_runs DROP CONSTRAINT agent_runs_kind_check;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_kind_check CHECK (kind IN ('station', 'jonah', 'try')) NOT VALID;
ALTER TABLE agent_runs VALIDATE CONSTRAINT agent_runs_kind_check;
ALTER TABLE agent_runs DROP CONSTRAINT agent_runs_item_check;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_item_check CHECK (item_id IS NOT NULL OR kind IN ('jonah', 'try')) NOT VALID;
ALTER TABLE agent_runs VALIDATE CONSTRAINT agent_runs_item_check;

CREATE TABLE tries (
  id              uuid PRIMARY KEY,                                        -- UUIDv7 from goald
  org_id          uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id      uuid NOT NULL,
  proposal_id     uuid NOT NULL,                                           -- the proposal tried
  item_id         uuid NOT NULL,                                           -- the past item it reruns
  station         text NOT NULL CHECK (length(station) BETWEEN 1 AND 100),
  original_run_id uuid NOT NULL,                                           -- the item's run it reruns
  try_run_id      uuid,                                                    -- the try's own run, kind try
  model           text NOT NULL CHECK (length(model) BETWEEN 1 AND 200),
  state           text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done')),
  verdict         text CHECK (verdict IN ('error')),
  error           text NOT NULL DEFAULT '' CHECK (length(error) <= 2000),
  diff            text NOT NULL DEFAULT '' CHECK (octet_length(diff) <= 1048576),
  cost_usd        numeric(12, 4) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
  lease_until     timestamptz,
  attempts        integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  created_at      timestamptz NOT NULL,
  finished_at     timestamptz,
  CHECK ((state = 'running') = (lease_until IS NOT NULL)),
  CHECK ((state = 'done') = (finished_at IS NOT NULL)),
  CHECK (verdict IS NULL OR state = 'done'),
  CHECK (verdict IS DISTINCT FROM 'error' OR length(btrim(error)) > 0),
  -- One try per proposal and item: the sweep queues a proposal's tries once.
  CONSTRAINT tries_key UNIQUE (org_id, proposal_id, item_id),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, proposal_id) REFERENCES jonah_proposals (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, original_run_id) REFERENCES agent_runs (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, try_run_id) REFERENCES agent_runs (org_id, id) ON DELETE SET NULL (try_run_id)
);

-- The claim's oldest due try per factory and the running count per factory, and the keys' cascades.
CREATE INDEX tries_due_idx      ON tries (org_id, factory_id, created_at, id) WHERE state IN ('queued', 'running');
CREATE INDEX tries_item_idx     ON tries (org_id, item_id);
CREATE INDEX tries_original_idx ON tries (org_id, original_run_id);
CREATE INDEX tries_run_idx      ON tries (org_id, try_run_id) WHERE try_run_id IS NOT NULL;

ALTER TABLE tries ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON tries
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON tries TO goal_app;
GRANT UPDATE (state, verdict, error, diff, cost_usd, try_run_id, lease_until, attempts, finished_at) ON tries TO goal_app;

-- The newest finished first run of a claim at a station, per item: a try's original run.
CREATE INDEX agent_runs_try_target_idx ON agent_runs (org_id, item_id, station, started_at DESC)
  WHERE kind = 'station' AND branch_head IS NOT NULL;
