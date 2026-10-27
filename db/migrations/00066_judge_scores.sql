-- +goose Up
-- Judge scores (ADR 0025 decision 2, ADR 0002): each a judge's score of one finished production
-- run, or with no run_id a try of an eval case. A score is queued, claimed under a lease
-- (FOR UPDATE SKIP LOCKED, fenced by lease_token as 00055 fences knowledge syncs) while its judge
-- run works, then scored or error; an error is an infrastructure failure and has no outcome. A
-- run is scored at most once per judge: UNIQUE (judge, run_id), which a try's NULL run_id never
-- meets. Runs belong to one factory and organization, so the key needs neither.
--
-- The factory, item and run keys are bound to the organization, as 00006's are, and cascade, so
-- goal_purge_org (00042) and goal_delete_factory (00061) remove a factory's scores with its runs
-- and items and need no change. version_id names the definition version whose judge file scored
-- the run, with no key: versions go only with their factory. judge_run_id is the judge's own run,
-- with no key either, since judge runs are not production runs. goal_app inserts and updates the
-- columns a claim and a record change, and never deletes.
CREATE TABLE judge_scores (
  id           uuid PRIMARY KEY,                                           -- UUIDv7 from goald
  org_id       uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id   uuid NOT NULL,
  version_id   uuid NOT NULL,
  judge        text NOT NULL CHECK (judge ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  run_id       uuid,
  item_id      uuid CHECK (run_id IS NULL OR item_id IS NOT NULL),
  state        text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'scored', 'error')),
  outcome      text CHECK (length(outcome) BETWEEN 1 AND 100),
  score        double precision CHECK (score >= 0 AND score <= 1),
  passed       boolean,
  reason       text NOT NULL DEFAULT '' CHECK (length(reason) <= 2000),
  cost_usd     numeric(12, 4) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
  judge_run_id uuid,
  lease_until  timestamptz,
  lease_token  uuid,
  created_at   timestamptz NOT NULL,
  scored_at    timestamptz,
  CHECK ((state = 'running') = (lease_until IS NOT NULL AND lease_token IS NOT NULL)),
  CHECK ((state IN ('scored', 'error')) = (scored_at IS NOT NULL)),
  CHECK ((state = 'scored') = (outcome IS NOT NULL AND score IS NOT NULL AND passed IS NOT NULL)),
  CONSTRAINT judge_scores_judge_run_key UNIQUE (judge, run_id),
  CONSTRAINT judge_scores_factory_fkey FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  CONSTRAINT judge_scores_item_fkey FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE,
  CONSTRAINT judge_scores_run_fkey FOREIGN KEY (org_id, run_id) REFERENCES agent_runs (org_id, id) ON DELETE CASCADE
);

-- A factory's scores and one judge's, over the last 30 days (Recent, Summary); the claim's
-- oldest due score; and the run and item keys' cascades.
CREATE INDEX judge_scores_factory_idx ON judge_scores (org_id, factory_id, created_at);
CREATE INDEX judge_scores_judge_idx   ON judge_scores (org_id, factory_id, judge, created_at);
CREATE INDEX judge_scores_due_idx     ON judge_scores (org_id, created_at, id) WHERE state IN ('queued', 'running');
CREATE INDEX judge_scores_run_idx     ON judge_scores (org_id, run_id) WHERE run_id IS NOT NULL;
CREATE INDEX judge_scores_item_idx    ON judge_scores (org_id, item_id) WHERE item_id IS NOT NULL;

ALTER TABLE judge_scores ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON judge_scores
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON judge_scores TO goal_app;
GRANT UPDATE (state, outcome, score, passed, reason, cost_usd, judge_run_id, lease_until, lease_token, scored_at)
  ON judge_scores TO goal_app;
