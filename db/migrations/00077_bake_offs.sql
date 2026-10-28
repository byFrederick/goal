-- +goose Up
-- Bake-offs (ADR 0029): an eval run of kind bake-off runs one agent, named by its station and
-- agent slug, on two to four model arms, exactly one of them the agent's current model. Every
-- existing run becomes kind eval. Each try records its arm: existing tries and an eval run's get
-- arm 0, a bake-off's arms are 1 to 4, so arm can be part of eval_results' key. Each try also
-- records how long it took. "Two to four arms" and "exactly one current" span rows: the partial
-- unique index refuses a second current arm, and the store checks the rest before it inserts.
-- eval_run_arms goes with its run (ON DELETE CASCADE), so the purge needs no change.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

ALTER TABLE eval_runs
  ADD COLUMN kind           text NOT NULL DEFAULT 'eval' CHECK (kind IN ('eval', 'bake-off')),
  ADD COLUMN station        text CHECK (station ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  ADD COLUMN agent          text CHECK (agent ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  ADD COLUMN tries_per_case smallint CHECK (tries_per_case BETWEEN 1 AND 10),
  -- A bake-off names its agent and tries per case; an eval run names neither.
  ADD CONSTRAINT eval_runs_bake_off_agent CHECK (
    CASE WHEN kind = 'bake-off' THEN station IS NOT NULL AND agent IS NOT NULL AND tries_per_case IS NOT NULL
         ELSE station IS NULL AND agent IS NULL AND tries_per_case IS NULL END);

-- A factory's bake-offs newest first.
CREATE INDEX eval_runs_kind_idx ON eval_runs (org_id, factory_id, kind, created_at DESC);

CREATE TABLE eval_run_arms (
  eval_run_id       uuid NOT NULL,
  org_id            uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  arm               smallint NOT NULL CHECK (arm BETWEEN 1 AND 4),
  model             text NOT NULL CHECK (model ~ '^claude-[a-z0-9-]+$'),
  is_current        boolean NOT NULL DEFAULT false,
  -- The arm's report row (ADR 0029 decision 3), NULL until the engine records it.
  summarized_at     timestamptz,
  tries             integer CHECK (tries >= 0),
  passed            integer CHECK (passed >= 0),
  errors            integer CHECK (errors >= 0),
  cost_per_item_usd numeric(12, 4) CHECK (cost_per_item_usd >= 0),
  ms_per_item       bigint CHECK (ms_per_item >= 0),
  mean_turns        numeric(10, 2) CHECK (mean_turns >= 0),
  judge             text CHECK (judge ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),          -- the advisory judge's slug
  judged_share      numeric(5, 4) CHECK (judged_share BETWEEN 0 AND 1),
  per_case          jsonb CHECK (jsonb_typeof(per_case) = 'object'),               -- tries passed by case name
  CHECK (
    CASE WHEN summarized_at IS NULL
      THEN tries IS NULL AND passed IS NULL AND errors IS NULL AND cost_per_item_usd IS NULL AND ms_per_item IS NULL
           AND mean_turns IS NULL AND judge IS NULL AND judged_share IS NULL AND per_case IS NULL
      ELSE tries IS NOT NULL AND passed IS NOT NULL AND errors IS NOT NULL AND passed + errors <= tries
           AND cost_per_item_usd IS NOT NULL AND ms_per_item IS NOT NULL AND mean_turns IS NOT NULL AND per_case IS NOT NULL
           AND (judge IS NULL) = (judged_share IS NULL) END),
  PRIMARY KEY (eval_run_id, arm),
  UNIQUE (eval_run_id, model),
  FOREIGN KEY (org_id, eval_run_id) REFERENCES eval_runs (org_id, id) ON DELETE CASCADE
);

-- At most one arm of a run is the agent's current model.
CREATE UNIQUE INDEX eval_run_arms_current_idx ON eval_run_arms (eval_run_id) WHERE is_current;
-- The org's index (the purge's delete by org) and a run's arms.
CREATE INDEX eval_run_arms_org_idx ON eval_run_arms (org_id, eval_run_id);

ALTER TABLE eval_run_arms ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON eval_run_arms
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON eval_run_arms TO goal_app;
GRANT UPDATE (summarized_at, tries, passed, errors, cost_per_item_usd, ms_per_item, mean_turns, judge, judged_share, per_case)
  ON eval_run_arms TO goal_app;

-- eval_results is small, and this runs in one transaction: the key's index is rebuilt under its lock.
ALTER TABLE eval_results
  ADD COLUMN arm         smallint NOT NULL DEFAULT 0 CHECK (arm BETWEEN 0 AND 4),
  ADD COLUMN duration_ms bigint NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
  DROP CONSTRAINT eval_results_pkey,
  ADD PRIMARY KEY (eval_run_id, arm, case_name, try);

GRANT UPDATE (duration_ms) ON eval_results TO goal_app;
