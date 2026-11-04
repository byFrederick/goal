-- +goose Up
-- Checks of the grader (ADR 0039 decision 10). Before its grades of a proposal's tries count, the
-- grader is checked on each tried item twice: on the item's original result, which it should read
-- as not_fixed, and on its final accepted result, which it should read as fixed. When it misreads
-- either one, that item's tries are unknown; every proposal shows both hit rates. A check is per
-- proposal and item, not per try: a model proposal's three tries of an item share its two checks.
--
-- run_id is the grade run that answered, when there is one. Rows are never updated or deleted but
-- by the purges: the factory, proposal and item keys cascade and the run's sets null, as for tries
-- (00102), so goal_purge_org (00042) and goal_delete_factory (00061) remove them and need no change.
-- 00109, as 00107 is taken by two parts of the same assembly in flight.
CREATE TABLE grader_checks (
  id          uuid PRIMARY KEY,                                        -- UUIDv7 from goald
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id  uuid NOT NULL,
  proposal_id uuid NOT NULL,
  item_id     uuid NOT NULL,
  result      text NOT NULL CHECK (result IN ('original', 'final')),
  answer      text NOT NULL CHECK (answer IN ('fixed', 'not_fixed', 'unknown')),
  reason      text NOT NULL CHECK (length(btrim(reason)) > 0 AND length(reason) <= 2000),
  run_id      uuid,
  cost_usd    numeric(12, 4) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
  checked_at  timestamptz NOT NULL,
  -- One check per proposal, item and result; the proposal key's cascade and its list use it.
  CONSTRAINT grader_checks_key UNIQUE (org_id, proposal_id, item_id, result),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, proposal_id) REFERENCES jonah_proposals (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, run_id) REFERENCES agent_runs (org_id, id) ON DELETE SET NULL (run_id)
);

-- A proposal's checks oldest first, and the factory, item and run keys' cascades.
CREATE INDEX grader_checks_proposal_idx ON grader_checks (org_id, proposal_id, checked_at, id);
CREATE INDEX grader_checks_factory_idx  ON grader_checks (org_id, factory_id);
CREATE INDEX grader_checks_item_idx     ON grader_checks (org_id, item_id);
CREATE INDEX grader_checks_run_idx      ON grader_checks (org_id, run_id) WHERE run_id IS NOT NULL;

ALTER TABLE grader_checks ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON grader_checks
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON grader_checks TO goal_app;
