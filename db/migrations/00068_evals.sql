-- +goose Up
-- Eval runs (ADR 0025 decision 4, amending ADR 0002): an eval run runs a factory's cases on one
-- definition, named by its content hash (domain.ManifestSHA256) and, once published, its version;
-- a publish runs them on the draft first. Each case's tries are eval_results that goald instances
-- claim with FOR UPDATE SKIP LOCKED under lease_until, so a dead instance's try is claimed again.
-- The shape is spec 5 §6's 00062 with one version per run: no candidate or baseline, no
-- version_id in the result key, try for trial, trigger manual or publish. Rows are never deleted
-- but by the factory's or the organization's purge; goal_purge_org deletes agent_runs first,
-- which clears eval_results.run_id. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE eval_runs (
  id             uuid PRIMARY KEY,                                         -- UUIDv7 from goald
  org_id         uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id     uuid NOT NULL,
  version_id     uuid,                                                     -- NULL: a draft's content
  content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
  trigger        text NOT NULL CHECK (trigger IN ('manual', 'publish')),
  cases          text CHECK (cases ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),   -- NULL: every case
  state          text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done', 'failed', 'cancelled')),
  estimate_usd   numeric(12, 4) NOT NULL DEFAULT 0 CHECK (estimate_usd >= 0),
  cost_usd       numeric(12, 4) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
  created_by     text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),    -- Actor.Ref()
  created_at     timestamptz NOT NULL DEFAULT now(),
  finished_at    timestamptz,
  CHECK ((finished_at IS NOT NULL) = (state IN ('done', 'failed', 'cancelled'))),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  -- A discarded draft's id goes; its run stays, named by its content.
  FOREIGN KEY (factory_id, version_id) REFERENCES definition_versions (factory_id, id) ON DELETE SET NULL (version_id),
  UNIQUE (org_id, id)                                                      -- eval_results' key; also the index on org_id
);

-- The publish gate's latest run for a content hash, and a factory's runs newest first.
CREATE INDEX eval_runs_content_idx ON eval_runs (org_id, factory_id, content_sha256, created_at DESC);

ALTER TABLE eval_runs ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON eval_runs
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON eval_runs TO goal_app;
GRANT UPDATE (state, cost_usd, finished_at) ON eval_runs TO goal_app;

CREATE TABLE eval_results (
  eval_run_id uuid NOT NULL,
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  case_name   text NOT NULL CHECK (case_name ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  case_sha256 text NOT NULL CHECK (case_sha256 ~ '^[0-9a-f]{64}$'),
  try         integer NOT NULL CHECK (try >= 1),
  state       text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'running', 'pass', 'fail', 'error')),
  lease_until timestamptz,
  CHECK ((lease_until IS NOT NULL) = (state = 'running')),
  run_id      uuid,                                                        -- the agent run of the try
  checked_by  text NOT NULL DEFAULT '' CHECK (length(checked_by) <= 200),   -- contract, judge:<slug>, …
  detail      jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(detail) = 'object' AND octet_length(detail::text) <= 262144),
  cost_usd    numeric(12, 4) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
  turns       integer NOT NULL DEFAULT 0 CHECK (turns >= 0),
  PRIMARY KEY (eval_run_id, case_name, try),
  FOREIGN KEY (org_id, eval_run_id) REFERENCES eval_runs (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, run_id) REFERENCES agent_runs (org_id, id) ON DELETE SET NULL (run_id)
);

-- The org's index (the purge's delete by org) and the key's lookup from a run.
CREATE INDEX eval_results_org_idx ON eval_results (org_id, eval_run_id);
-- What ClaimResult scans: tries not finished yet.
CREATE INDEX eval_results_claimable_idx ON eval_results (org_id, lease_until) WHERE state IN ('pending', 'running');
-- Deleting an agent run clears run_id here without a scan.
CREATE INDEX eval_results_run_idx ON eval_results (org_id, run_id) WHERE run_id IS NOT NULL;

ALTER TABLE eval_results ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON eval_results
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON eval_results TO goal_app;
GRANT UPDATE (state, lease_until, run_id, checked_by, detail, cost_usd, turns) ON eval_results TO goal_app;
