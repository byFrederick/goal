-- +goose Up
-- DRAFT: Jonah, Goal's evaluation and self-remediation subsystem. See docs/jonah/README.md.
--
-- This file lives in docs/, not db/migrations/. When phase J1 starts, it is split into numbered,
-- forward-only migrations (00002_jonah_*.sql, ...). It applies on top of db/migrations/00001_init.sql
-- on PostgreSQL 18 (uuidv7()).
--
-- Conventions (same as 00001_init.sql):
--   * Names that come from YAML (suite, grader, judge, agent, station, failure mode) are TEXT,
--     never enums, so a new factory or suite never needs a schema change.
--   * Vocabularies owned by the engine (trial status, autonomy level, lever) are TEXT + CHECK.
--   * Every foreign key has an index. timestamptz everywhere. UUIDv7 keys, generated in Go
--     (ADR 0003); the uuidv7() defaults are a convenience for manual SQL.
--   * Evidence is append-only: case versions, comparisons once decided, and the audit log
--     reject UPDATE/DELETE with a trigger.

-- ---------------------------------------------------------------------------------------------
-- Configuration history. Every run, trial and proposal points at the exact config it used.
-- ---------------------------------------------------------------------------------------------

-- Every factory.yaml version goald has loaded, plus candidate versions built from proposals.
-- factories.definition (00001) keeps only the current one.
CREATE TABLE factory_definitions (
  factory_id uuid NOT NULL REFERENCES factories (id),
  sha256     text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  source     text NOT NULL CHECK (source IN ('git', 'proposal')),
  git_commit text CHECK (git_commit ~ '^[0-9a-f]{40}$'),
  definition jsonb NOT NULL,                 -- parsed factory.yaml
  files      jsonb NOT NULL DEFAULT '{}',    -- path → sha256 of prompt, skill and contract files it references
  loaded_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (factory_id, sha256)
);

-- Production runs record which definition produced them (attribution and rollback).
ALTER TABLE agent_runs ADD COLUMN definition_sha256 text;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_definition_fkey
  FOREIGN KEY (factory_id, definition_sha256) REFERENCES factory_definitions (factory_id, sha256);
CREATE INDEX agent_runs_definition_idx ON agent_runs (factory_id, definition_sha256)
  WHERE definition_sha256 IS NOT NULL;

-- Every evals.yaml version goald has loaded.
CREATE TABLE eval_configs (
  factory_id uuid NOT NULL REFERENCES factories (id),
  sha256     text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  git_commit text CHECK (git_commit ~ '^[0-9a-f]{40}$'),
  config     jsonb NOT NULL,
  loaded_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (factory_id, sha256)
);

CREATE TABLE eval_suites (
  factory_id    uuid NOT NULL REFERENCES factories (id),
  name          text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,62}[a-z0-9]$'),
  level         text NOT NULL CHECK (level IN ('agent', 'station')), -- factory level is measured online (flow)
  purpose       text NOT NULL CHECK (purpose IN ('capability', 'regression')),
  agent         text,
  station       text,
  definition    jsonb NOT NULL,              -- the suite block from evals.yaml
  config_sha256 text NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  retired_at    timestamptz,
  PRIMARY KEY (factory_id, name),
  FOREIGN KEY (factory_id, config_sha256) REFERENCES eval_configs (factory_id, sha256),
  CHECK (level <> 'agent' OR agent IS NOT NULL),
  CHECK (level <> 'station' OR station IS NOT NULL)
);
CREATE INDEX eval_suites_config_idx ON eval_suites (factory_id, config_sha256);

-- ---------------------------------------------------------------------------------------------
-- Cases and fixtures. Visible cases live in the definitions bucket (factories/<id>/evals/cases/,
-- ADR 0005); goald syncs them here by content hash. The sealed holdout lives only here, never in
-- the bucket. J1 revises the proposal columns below (base_commit, merged) for object versions.
-- ---------------------------------------------------------------------------------------------

CREATE TABLE eval_cases (
  id              uuid PRIMARY KEY,           -- metadata.id from the case file; uuidv7() for Postgres-only cases
  factory_id      uuid NOT NULL REFERENCES factories (id),
  name            text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,78}[a-z0-9]$'),
  agent           text,
  station         text,
  purpose         text NOT NULL CHECK (purpose IN ('capability', 'regression')),
  split           text NOT NULL CHECK (split IN ('tune', 'check', 'holdout')),
  state           text NOT NULL DEFAULT 'draft'
                  CHECK (state IN ('candidate', 'draft', 'active', 'quarantined', 'retired')),
  origin          text NOT NULL CHECK (origin IN ('manual', 'production', 'synthetic', 'incident')),
  origin_item_id  uuid REFERENCES work_items (id),
  origin_run_id   uuid REFERENCES agent_runs (id),
  tags            text[] NOT NULL DEFAULT '{}',
  storage         text NOT NULL CHECK (storage IN ('git', 'postgres')),
  path            text,                       -- repository path when storage = 'git'
  current_sha256  text,                       -- active version; FK added below
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (factory_id, name),
  CHECK ((storage = 'git') = (path IS NOT NULL)),
  -- Sealed: the holdout is never written to the repository, where agents could read it.
  CHECK (split <> 'holdout' OR storage = 'postgres'),
  CHECK (state = 'candidate' OR current_sha256 IS NOT NULL)
);
CREATE INDEX eval_cases_factory_state_idx ON eval_cases (factory_id, state, split);
CREATE INDEX eval_cases_origin_item_idx ON eval_cases (origin_item_id) WHERE origin_item_id IS NOT NULL;
CREATE INDEX eval_cases_origin_run_idx ON eval_cases (origin_run_id) WHERE origin_run_id IS NOT NULL;
CREATE INDEX eval_cases_tags_idx ON eval_cases USING gin (tags);

-- Immutable, content-addressed versions. A trial always points at the exact version it ran.
CREATE TABLE eval_case_versions (
  case_id    uuid NOT NULL REFERENCES eval_cases (id),
  sha256     text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  content    jsonb NOT NULL,                  -- the case file (or Postgres-only case) as JSON
  git_commit text CHECK (git_commit ~ '^[0-9a-f]{40}$'),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (case_id, sha256)
);
ALTER TABLE eval_cases ADD CONSTRAINT eval_cases_current_version_fkey
  FOREIGN KEY (id, current_sha256) REFERENCES eval_case_versions (case_id, sha256)
  DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX eval_cases_current_version_idx ON eval_cases (id, current_sha256);

-- Fixtures: recorded tool responses (cassettes), knowledge snapshots, small files, repo snapshots.
-- Small content is stored inline so runners never need a shared disk; large content goes to object
-- storage and is served through the MCP gateway by hash.
CREATE TABLE eval_fixtures (
  sha256     text PRIMARY KEY CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  factory_id uuid NOT NULL REFERENCES factories (id),
  kind       text NOT NULL CHECK (kind IN ('cassette', 'knowledge', 'file', 'repo-snapshot')),
  media_type text NOT NULL,
  size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
  content    bytea,
  uri        text,                             -- s3://… when too large to inline
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (num_nonnulls(content, uri) = 1),
  CHECK (content IS NULL OR size_bytes <= 8 * 1024 * 1024)
);
CREATE INDEX eval_fixtures_factory_idx ON eval_fixtures (factory_id);

CREATE TABLE eval_case_version_fixtures (
  case_id        uuid NOT NULL,
  case_sha256    text NOT NULL,
  fixture_sha256 text NOT NULL REFERENCES eval_fixtures (sha256),
  role           text NOT NULL,              -- cassette, knowledge, patch, file:<path>
  PRIMARY KEY (case_id, case_sha256, role),
  FOREIGN KEY (case_id, case_sha256) REFERENCES eval_case_versions (case_id, sha256)
);
CREATE INDEX eval_case_version_fixtures_fixture_idx ON eval_case_version_fixtures (fixture_sha256);

-- ---------------------------------------------------------------------------------------------
-- Offline runs and trials. A run is one arm (baseline or candidate) of one suite.
-- ---------------------------------------------------------------------------------------------

CREATE TABLE eval_runs (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id         uuid NOT NULL REFERENCES factories (id),
  suite              text NOT NULL,
  purpose            text NOT NULL
                     CHECK (purpose IN ('scheduled', 'ci', 'remediation', 'holdout-gate', 'calibration', 'audit', 'adhoc', 'dry-run')),
  arm                text NOT NULL DEFAULT 'baseline' CHECK (arm IN ('baseline', 'candidate')),
  definition_sha256  text NOT NULL,          -- factory config under test
  eval_config_sha256 text NOT NULL,          -- graders, judges and thresholds used
  proposal_id        uuid,                   -- FK added below
  git_commit         text CHECK (git_commit ~ '^[0-9a-f]{40}$'),
  trials_per_case    smallint NOT NULL CHECK (trials_per_case BETWEEN 1 AND 20),
  status             text NOT NULL DEFAULT 'planned'
                     CHECK (status IN ('planned', 'running', 'succeeded', 'failed', 'cancelled')),
  estimated_usd      numeric(12, 4),
  cost_usd           numeric(12, 4) NOT NULL DEFAULT 0,
  requested_by       text NOT NULL,          -- scheduler, ci, jonah, human:<name>
  summary            jsonb NOT NULL DEFAULT '{}', -- cache of pass@k, pass^k, CIs; always recomputable from trials
  created_at         timestamptz NOT NULL DEFAULT now(),
  started_at         timestamptz,
  ended_at           timestamptz,
  FOREIGN KEY (factory_id, suite) REFERENCES eval_suites (factory_id, name),
  FOREIGN KEY (factory_id, definition_sha256) REFERENCES factory_definitions (factory_id, sha256),
  FOREIGN KEY (factory_id, eval_config_sha256) REFERENCES eval_configs (factory_id, sha256)
);
CREATE INDEX eval_runs_suite_created_idx ON eval_runs (factory_id, suite, created_at DESC);
CREATE INDEX eval_runs_definition_idx ON eval_runs (factory_id, definition_sha256);
CREATE INDEX eval_runs_eval_config_idx ON eval_runs (factory_id, eval_config_sha256);
CREATE INDEX eval_runs_proposal_idx ON eval_runs (proposal_id) WHERE proposal_id IS NOT NULL;

-- One trial = one attempt at one case in a fresh container. Trials are their own work queue
-- (FOR UPDATE SKIP LOCKED), separate from production leases.
CREATE TABLE eval_trials (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  run_id           uuid NOT NULL REFERENCES eval_runs (id),
  factory_id       uuid NOT NULL REFERENCES factories (id),
  case_id          uuid NOT NULL,
  case_sha256      text NOT NULL,
  trial            smallint NOT NULL CHECK (trial >= 0),
  order_key        integer NOT NULL,          -- random interleaving of arms and cases
  status           text NOT NULL DEFAULT 'queued'
                   CHECK (status IN ('queued', 'running', 'graded', 'error', 'cancelled')),
  passed           boolean,                   -- all required graders passed
  score            numeric(5, 4) CHECK (score BETWEEN 0 AND 1), -- partial credit, reported beside passed
  error_class      text,                      -- rate-limited, infra, timeout, unmatched-fixture, harness
  slot             integer CHECK (slot >= 0), -- eval-lane slot while running
  lease_expires_at timestamptz,
  container        text,
  model            text,
  turns            integer NOT NULL DEFAULT 0,
  input_tokens     bigint NOT NULL DEFAULT 0,
  output_tokens    bigint NOT NULL DEFAULT 0,
  cost_usd         numeric(12, 4) NOT NULL DEFAULT 0,
  duration_ms      integer,
  queued_at        timestamptz NOT NULL DEFAULT now(),
  started_at       timestamptz,
  ended_at         timestamptz,
  UNIQUE (run_id, case_id, trial),
  FOREIGN KEY (case_id, case_sha256) REFERENCES eval_case_versions (case_id, sha256),
  -- An infrastructure error is never scored as an agent failure.
  CHECK (status <> 'graded' OR passed IS NOT NULL),
  CHECK (status <> 'error' OR (passed IS NULL AND error_class IS NOT NULL)),
  CHECK (status <> 'running' OR (slot IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE INDEX eval_trials_factory_idx ON eval_trials (factory_id);
CREATE INDEX eval_trials_case_idx ON eval_trials (case_id, case_sha256);
-- The pull query for the eval lane.
CREATE INDEX eval_trials_queue_idx ON eval_trials (factory_id, order_key) WHERE status = 'queued';
CREATE INDEX eval_trials_lease_idx ON eval_trials (lease_expires_at) WHERE status = 'running';
-- The eval lane's WIP limit, enforced across every goald instance: one running trial per slot.
CREATE UNIQUE INDEX eval_trials_slot_key ON eval_trials (factory_id, slot) WHERE status = 'running';

-- ---------------------------------------------------------------------------------------------
-- Online evaluation: sampled production runs.
-- ---------------------------------------------------------------------------------------------

CREATE TABLE online_samples (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id        uuid NOT NULL REFERENCES factories (id),
  agent_run_id      uuid NOT NULL UNIQUE REFERENCES agent_runs (id),
  item_id           uuid NOT NULL REFERENCES work_items (id),
  station           text NOT NULL,
  agent             text NOT NULL,
  definition_sha256 text,
  strategy          text NOT NULL CHECK (strategy IN ('random', 'outlier', 'failure', 'uncertainty', 'stratified')),
  sampled_at        timestamptz NOT NULL DEFAULT now(),
  graded_at         timestamptz,             -- null = waiting for graders; claimed with SKIP LOCKED
  FOREIGN KEY (factory_id, definition_sha256) REFERENCES factory_definitions (factory_id, sha256)
);
CREATE INDEX online_samples_factory_at_idx ON online_samples (factory_id, sampled_at DESC);
CREATE INDEX online_samples_ungraded_idx ON online_samples (sampled_at) WHERE graded_at IS NULL;
CREATE INDEX online_samples_item_idx ON online_samples (item_id);
CREATE INDEX online_samples_definition_idx ON online_samples (factory_id, definition_sha256)
  WHERE definition_sha256 IS NOT NULL;

-- ---------------------------------------------------------------------------------------------
-- Jonah's own agent tasks (diagnose, propose). They run through the same Runner as production.
-- ---------------------------------------------------------------------------------------------

CREATE TABLE remediation_signals (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id    uuid NOT NULL REFERENCES factories (id),
  kind          text NOT NULL CHECK (kind IN ('online-drop', 'regression-failure', 'andon', 'kaizen', 'gate-drift',
                                              'cost-drift', 'model-change', 'flow-target', 'manual')),
  fingerprint   text NOT NULL,              -- dedup key, e.g. online-drop:investigator:diagnosis-claims-supported
  subject       text NOT NULL,              -- agent, station or suite the signal is about
  severity      smallint NOT NULL DEFAULT 2 CHECK (severity BETWEEN 1 AND 4),
  detail        jsonb NOT NULL DEFAULT '{}',
  andon_pull_id bigint REFERENCES andon_pulls (id),
  state         text NOT NULL DEFAULT 'open'
                CHECK (state IN ('open', 'diagnosing', 'proposed', 'resolved', 'dismissed')),
  created_at    timestamptz NOT NULL DEFAULT now(),
  resolved_at   timestamptz
);
-- One open signal per fingerprint.
CREATE UNIQUE INDEX remediation_signals_open_key ON remediation_signals (factory_id, fingerprint)
  WHERE state IN ('open', 'diagnosing', 'proposed');
CREATE INDEX remediation_signals_andon_idx ON remediation_signals (andon_pull_id) WHERE andon_pull_id IS NOT NULL;

CREATE TABLE jonah_tasks (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id       uuid NOT NULL REFERENCES factories (id),
  signal_id        uuid NOT NULL REFERENCES remediation_signals (id),
  kind             text NOT NULL CHECK (kind IN ('diagnose', 'propose')),
  status           text NOT NULL DEFAULT 'queued'
                   CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
  slot             integer CHECK (slot >= 0),
  lease_expires_at timestamptz,
  model            text,
  turns            integer NOT NULL DEFAULT 0,
  input_tokens     bigint NOT NULL DEFAULT 0,
  output_tokens    bigint NOT NULL DEFAULT 0,
  cost_usd         numeric(12, 4) NOT NULL DEFAULT 0,
  output           jsonb,                    -- must satisfy the diagnosis or proposal contract
  error            text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  started_at       timestamptz,
  ended_at         timestamptz
);
CREATE INDEX jonah_tasks_factory_idx ON jonah_tasks (factory_id, created_at DESC);
CREATE INDEX jonah_tasks_signal_idx ON jonah_tasks (signal_id);
CREATE INDEX jonah_tasks_queue_idx ON jonah_tasks (factory_id, created_at) WHERE status = 'queued';

-- ---------------------------------------------------------------------------------------------
-- Transcripts: the full record of a production run, an eval trial or a Jonah task.
-- Stored compressed in Postgres so any goald instance can serve them; no local disk.
-- ---------------------------------------------------------------------------------------------

CREATE TABLE transcripts (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id    uuid NOT NULL REFERENCES factories (id),
  agent_run_id  uuid REFERENCES agent_runs (id),
  eval_trial_id uuid REFERENCES eval_trials (id),
  jonah_task_id uuid REFERENCES jonah_tasks (id),
  format        text NOT NULL DEFAULT 'claude-stream-json',
  encoding      text NOT NULL DEFAULT 'zstd' CHECK (encoding IN ('zstd', 'identity')),
  content       bytea NOT NULL,
  sha256        text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes    bigint NOT NULL CHECK (size_bytes >= 0),
  redacted      boolean NOT NULL DEFAULT false,  -- secret/PII scrubbing applied at ingest
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz,                     -- retention; null = keep (eval evidence)
  CHECK (num_nonnulls(agent_run_id, eval_trial_id, jonah_task_id) = 1)
);
CREATE INDEX transcripts_factory_idx ON transcripts (factory_id, created_at DESC);
CREATE UNIQUE INDEX transcripts_agent_run_key ON transcripts (agent_run_id) WHERE agent_run_id IS NOT NULL;
CREATE UNIQUE INDEX transcripts_eval_trial_key ON transcripts (eval_trial_id) WHERE eval_trial_id IS NOT NULL;
CREATE UNIQUE INDEX transcripts_jonah_task_key ON transcripts (jonah_task_id) WHERE jonah_task_id IS NOT NULL;
CREATE INDEX transcripts_expires_idx ON transcripts (expires_at) WHERE expires_at IS NOT NULL;

-- ---------------------------------------------------------------------------------------------
-- Judges (model graders) and their calibration against human labels.
-- ---------------------------------------------------------------------------------------------

CREATE TABLE judges (
  factory_id uuid NOT NULL REFERENCES factories (id),
  name       text NOT NULL,
  criterion  text NOT NULL,                  -- the one failure mode this judge checks
  status     text NOT NULL DEFAULT 'draft'
             CHECK (status IN ('draft', 'calibrating', 'gating', 'advisory', 'retired')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (factory_id, name)
);

CREATE TABLE judge_versions (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id     uuid NOT NULL,
  judge          text NOT NULL,
  prompt_sha256  text NOT NULL CHECK (prompt_sha256 ~ '^[0-9a-f]{64}$'),
  prompt         text NOT NULL,
  model          text NOT NULL,              -- configured alias, e.g. claude-opus-5-5
  resolved_model text,                       -- model id the API reported; a change forces recalibration
  effort         text,
  created_at     timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (factory_id, judge) REFERENCES judges (factory_id, name),
  UNIQUE (factory_id, judge, prompt_sha256, model)
);

CREATE TABLE judge_calibrations (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  judge_version_id uuid NOT NULL REFERENCES judge_versions (id),
  split            text NOT NULL CHECK (split IN ('dev', 'test')),
  tp               integer NOT NULL CHECK (tp >= 0),
  fn               integer NOT NULL CHECK (fn >= 0),
  tn               integer NOT NULL CHECK (tn >= 0),
  fp               integer NOT NULL CHECK (fp >= 0),
  tpr              numeric(5, 4) GENERATED ALWAYS AS (round(tp::numeric / NULLIF(tp + fn, 0), 4)) STORED,
  tnr              numeric(5, 4) GENERATED ALWAYS AS (round(tn::numeric / NULLIF(tn + fp, 0), 4)) STORED,
  unknowns         integer NOT NULL DEFAULT 0, -- judge answered unknown; counted as misses in the decision
  decision         text CHECK (decision IN ('gating', 'advisory', 'rejected')),
  ran_at           timestamptz NOT NULL DEFAULT now(),
  ran_by           text NOT NULL,
  CHECK ((split = 'test') = (decision IS NOT NULL))
);
-- The sealed test split is used exactly once per judge version.
CREATE UNIQUE INDEX judge_calibrations_test_once_key ON judge_calibrations (judge_version_id) WHERE split = 'test';
CREATE INDEX judge_calibrations_version_idx ON judge_calibrations (judge_version_id);

-- ---------------------------------------------------------------------------------------------
-- Grader results for both offline trials and online samples.
-- ---------------------------------------------------------------------------------------------

CREATE TABLE grader_results (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id       uuid NOT NULL REFERENCES factories (id),
  eval_trial_id    uuid REFERENCES eval_trials (id),
  online_sample_id uuid REFERENCES online_samples (id),
  grader           text NOT NULL,            -- name from evals.yaml
  kind             text NOT NULL,            -- contract, command, judge, … (registry in goald)
  grader_sha256    text NOT NULL,            -- hash of the grader config used
  judge_version_id uuid REFERENCES judge_versions (id),
  required         boolean NOT NULL,         -- false = advisory (uncalibrated judge, flaky check)
  verdict          text NOT NULL CHECK (verdict IN ('pass', 'fail', 'unknown', 'error')),
  score            numeric(5, 4) CHECK (score BETWEEN 0 AND 1),
  rationale        text,                     -- judge critique or code grader message
  evidence         jsonb NOT NULL DEFAULT '{}',
  model            text,
  cost_usd         numeric(12, 4) NOT NULL DEFAULT 0,
  duration_ms      integer,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (num_nonnulls(eval_trial_id, online_sample_id) = 1),
  CHECK (kind NOT IN ('judge', 'pairwise') OR judge_version_id IS NOT NULL)
);
CREATE INDEX grader_results_factory_idx ON grader_results (factory_id, created_at DESC);
CREATE UNIQUE INDEX grader_results_trial_key ON grader_results (eval_trial_id, grader) WHERE eval_trial_id IS NOT NULL;
CREATE UNIQUE INDEX grader_results_sample_key ON grader_results (online_sample_id, grader) WHERE online_sample_id IS NOT NULL;
CREATE INDEX grader_results_judge_version_idx ON grader_results (judge_version_id) WHERE judge_version_id IS NOT NULL;

-- ---------------------------------------------------------------------------------------------
-- Human review: the labeling queue and the labels it produces.
-- ---------------------------------------------------------------------------------------------

-- The failure taxonomy that error analysis produces. Jonah may propose; only a human confirms.
CREATE TABLE failure_modes (
  factory_id   uuid NOT NULL REFERENCES factories (id),
  name         text NOT NULL CHECK (name ~ '^[a-z][a-z0-9-]{1,62}[a-z0-9]$'),
  definition   text NOT NULL,
  lever        text NOT NULL CHECK (lever IN ('prompt', 'skill', 'tool-policy', 'connection', 'model', 'flow-config',
                                              'contract', 'knowledge', 'grader', 'case', 'harness', 'unknown')),
  status       text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed', 'confirmed', 'retired')),
  proposed_by  text NOT NULL,
  confirmed_by text CHECK (confirmed_by LIKE 'human:%'),
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (factory_id, name),
  CHECK (status <> 'confirmed' OR confirmed_by IS NOT NULL)
);

CREATE TABLE review_queue_items (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id       uuid NOT NULL REFERENCES factories (id),
  eval_trial_id    uuid REFERENCES eval_trials (id),
  online_sample_id uuid REFERENCES online_samples (id),
  case_id          uuid REFERENCES eval_cases (id),       -- a candidate case to triage
  reason           text NOT NULL CHECK (reason IN ('error-analysis', 'calibration', 'spot-check', 'judge-unknown',
                                                    'judge-disagreement', 'case-candidate', 'proposal-review')),
  priority         smallint NOT NULL DEFAULT 0,
  state            text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'claimed', 'done', 'skipped')),
  claimed_by       text,
  claimed_at       timestamptz,
  done_at          timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (num_nonnulls(eval_trial_id, online_sample_id, case_id) = 1)
);
CREATE INDEX review_queue_open_idx ON review_queue_items (factory_id, priority DESC, created_at) WHERE state = 'open';
CREATE INDEX review_queue_trial_idx ON review_queue_items (eval_trial_id) WHERE eval_trial_id IS NOT NULL;
CREATE INDEX review_queue_sample_idx ON review_queue_items (online_sample_id) WHERE online_sample_id IS NOT NULL;
CREATE INDEX review_queue_case_idx ON review_queue_items (case_id) WHERE case_id IS NOT NULL;

-- Trace-level labels. Corrections are new rows; the latest label per subject and labeler wins.
CREATE TABLE human_labels (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id        uuid NOT NULL REFERENCES factories (id),
  eval_trial_id     uuid REFERENCES eval_trials (id),
  online_sample_id  uuid REFERENCES online_samples (id),
  queue_item_id     uuid REFERENCES review_queue_items (id),
  labeler           text NOT NULL CHECK (labeler LIKE 'human:%'),
  verdict           text NOT NULL CHECK (verdict IN ('pass', 'fail', 'defer')),
  first_failure     text,                    -- open coding: the first thing that went wrong, observed not explained
  failure_modes     text[] NOT NULL DEFAULT '{}',
  notes             text,
  -- Assigned once, at random and stratified by verdict, when the label is first used for calibration.
  calibration_split text CHECK (calibration_split IN ('train', 'dev', 'test')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  CHECK (num_nonnulls(eval_trial_id, online_sample_id) = 1),
  CHECK (verdict <> 'pass' OR cardinality(failure_modes) = 0)
);
CREATE INDEX human_labels_factory_idx ON human_labels (factory_id, created_at DESC);
CREATE INDEX human_labels_trial_idx ON human_labels (eval_trial_id) WHERE eval_trial_id IS NOT NULL;
CREATE INDEX human_labels_sample_idx ON human_labels (online_sample_id) WHERE online_sample_id IS NOT NULL;
CREATE INDEX human_labels_queue_idx ON human_labels (queue_item_id) WHERE queue_item_id IS NOT NULL;
CREATE INDEX human_labels_failure_modes_idx ON human_labels USING gin (failure_modes);

-- ---------------------------------------------------------------------------------------------
-- Self-remediation: diagnosis → proposal → comparison → rollout.
-- ---------------------------------------------------------------------------------------------

CREATE TABLE diagnoses (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id         uuid NOT NULL REFERENCES factories (id),
  signal_id          uuid NOT NULL REFERENCES remediation_signals (id),
  task_id            uuid REFERENCES jonah_tasks (id),
  constraint_station text,                   -- the drum when the diagnosis was made
  failure_mode       text NOT NULL,
  lever              text NOT NULL CHECK (lever IN ('prompt', 'skill', 'tool-policy', 'connection', 'model', 'flow-config',
                                                    'contract', 'knowledge', 'grader', 'case', 'harness', 'unknown')),
  frequency          numeric(5, 4) CHECK (frequency BETWEEN 0 AND 1),
  impact             jsonb NOT NULL DEFAULT '{}', -- e.g. constraint hours lost per week, rework items
  hypothesis         text NOT NULL,
  evidence           jsonb NOT NULL,          -- transcript ids + short quotes, tune split and production only
  created_at         timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (factory_id, failure_mode) REFERENCES failure_modes (factory_id, name)
);
CREATE INDEX diagnoses_signal_idx ON diagnoses (signal_id);
CREATE INDEX diagnoses_task_idx ON diagnoses (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX diagnoses_failure_mode_idx ON diagnoses (factory_id, failure_mode);

-- Jonah may only touch these files. Evals, graders, judges, schemas, migrations and its own
-- code and policy never match. The application checks this too; the database is the backstop.
-- +goose StatementBegin
CREATE FUNCTION jonah_path_allowed(path text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT path ~ '^factories/[a-z][a-z0-9-]*[a-z0-9]/(factory\.yaml|agents/[A-Za-z0-9_.-]+\.md|skills/[a-z0-9-]+/SKILL\.md|contracts/[A-Za-z0-9_.-]+\.schema\.json)$'
     AND path !~ '\.\.'
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION jonah_paths_allowed(paths text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT cardinality(paths) > 0 AND bool_and(jonah_path_allowed(p)) FROM unnest(paths) AS p
$$;
-- +goose StatementEnd

CREATE TABLE remediation_proposals (
  id                           uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id                   uuid NOT NULL REFERENCES factories (id),
  diagnosis_id                 uuid NOT NULL REFERENCES diagnoses (id),
  lever                        text NOT NULL CHECK (lever IN ('prompt', 'skill', 'tool-policy', 'connection', 'model',
                                                              'flow-config', 'contract')),
  risk                         text NOT NULL CHECK (risk IN ('low', 'medium', 'high')),
  autonomy                     text NOT NULL CHECK (autonomy IN ('L1', 'L2', 'L3')), -- level in force when created
  base_definition_sha256       text NOT NULL,
  candidate_definition_sha256  text NOT NULL,
  base_commit                  text NOT NULL CHECK (base_commit ~ '^[0-9a-f]{40}$'),
  diff                         text NOT NULL,   -- unified diff against base_commit
  diff_sha256                  text NOT NULL CHECK (diff_sha256 ~ '^[0-9a-f]{64}$'),
  files                        text[] NOT NULL CHECK (cardinality(files) BETWEEN 1 AND 10 AND jonah_paths_allowed(files)),
  rationale                    text NOT NULL,
  state                        text NOT NULL DEFAULT 'draft'
                               CHECK (state IN ('draft', 'verifying', 'rejected', 'ready', 'shipped', 'merged',
                                                'watching', 'kept', 'rolled-back', 'withdrawn')),
  comparison_id                uuid,
  comparison_decision          text,
  work_item_id                 uuid REFERENCES work_items (id), -- the goal factory item that carries the PR
  pull_request                 text,
  budget_usd                   numeric(12, 4) NOT NULL CHECK (budget_usd >= 0),
  spent_usd                    numeric(12, 4) NOT NULL DEFAULT 0 CHECK (spent_usd >= 0),
  created_at                   timestamptz NOT NULL DEFAULT now(),
  updated_at                   timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (factory_id, base_definition_sha256) REFERENCES factory_definitions (factory_id, sha256),
  FOREIGN KEY (factory_id, candidate_definition_sha256) REFERENCES factory_definitions (factory_id, sha256),
  CHECK (base_definition_sha256 <> candidate_definition_sha256),
  -- No change without eval evidence: past verification, a proposal must reference a passing comparison.
  CHECK (state IN ('draft', 'verifying', 'rejected', 'withdrawn')
         OR (comparison_id IS NOT NULL AND comparison_decision = 'pass')),
  CHECK (state NOT IN ('shipped', 'merged', 'watching', 'kept', 'rolled-back') OR work_item_id IS NOT NULL),
  CHECK (spent_usd <= budget_usd)
);
CREATE INDEX remediation_proposals_factory_state_idx ON remediation_proposals (factory_id, state, created_at DESC);
CREATE INDEX remediation_proposals_diagnosis_idx ON remediation_proposals (diagnosis_id);
CREATE INDEX remediation_proposals_base_def_idx ON remediation_proposals (factory_id, base_definition_sha256);
CREATE INDEX remediation_proposals_candidate_def_idx ON remediation_proposals (factory_id, candidate_definition_sha256);
CREATE INDEX remediation_proposals_comparison_idx ON remediation_proposals (comparison_id, comparison_decision)
  WHERE comparison_id IS NOT NULL;
CREATE INDEX remediation_proposals_work_item_idx ON remediation_proposals (work_item_id) WHERE work_item_id IS NOT NULL;
-- One proposal in flight per factory: changes must be attributable.
CREATE UNIQUE INDEX remediation_proposals_one_open_key ON remediation_proposals (factory_id)
  WHERE state IN ('verifying', 'ready', 'shipped', 'merged', 'watching');

ALTER TABLE eval_runs ADD CONSTRAINT eval_runs_proposal_fkey
  FOREIGN KEY (proposal_id) REFERENCES remediation_proposals (id);

-- Candidate vs baseline. Decided once; never edited afterwards.
CREATE TABLE eval_comparisons (
  id                       uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id               uuid NOT NULL REFERENCES factories (id),
  proposal_id              uuid REFERENCES remediation_proposals (id),
  baseline_run_id          uuid NOT NULL REFERENCES eval_runs (id),
  candidate_run_id         uuid NOT NULL REFERENCES eval_runs (id),
  holdout_baseline_run_id  uuid REFERENCES eval_runs (id),
  holdout_candidate_run_id uuid REFERENCES eval_runs (id),
  rule                     jsonb NOT NULL,     -- alpha, minEffect, margin, costTolerance, holdout: required|skip
  stats                    jsonb NOT NULL DEFAULT '{}', -- effect, CI, p, per-suite non-inferiority, cost ratio
  decision                 text NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending', 'pass', 'fail', 'inconclusive')),
  decided_at               timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, decision),
  CHECK (baseline_run_id <> candidate_run_id),
  CHECK ((decision = 'pending') = (decided_at IS NULL)),
  CHECK (decision <> 'pass' OR rule ->> 'holdout' = 'skip'
         OR (holdout_baseline_run_id IS NOT NULL AND holdout_candidate_run_id IS NOT NULL))
);
CREATE INDEX eval_comparisons_factory_idx ON eval_comparisons (factory_id, created_at DESC);
CREATE INDEX eval_comparisons_proposal_idx ON eval_comparisons (proposal_id) WHERE proposal_id IS NOT NULL;
CREATE INDEX eval_comparisons_baseline_idx ON eval_comparisons (baseline_run_id);
CREATE INDEX eval_comparisons_candidate_idx ON eval_comparisons (candidate_run_id);
CREATE INDEX eval_comparisons_holdout_baseline_idx ON eval_comparisons (holdout_baseline_run_id) WHERE holdout_baseline_run_id IS NOT NULL;
CREATE INDEX eval_comparisons_holdout_candidate_idx ON eval_comparisons (holdout_candidate_run_id) WHERE holdout_candidate_run_id IS NOT NULL;

-- The decision travels to the proposal (ON UPDATE CASCADE), so the proposal CHECK above sees it.
ALTER TABLE remediation_proposals ADD CONSTRAINT remediation_proposals_comparison_fkey
  FOREIGN KEY (comparison_id, comparison_decision) REFERENCES eval_comparisons (id, decision) ON UPDATE CASCADE;

-- +goose StatementBegin
CREATE FUNCTION eval_comparisons_decide_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.decision <> 'pending' THEN
    RAISE EXCEPTION 'eval comparison % is decided (%); it cannot change', OLD.id, OLD.decision;
  END IF;
  IF NEW.baseline_run_id <> OLD.baseline_run_id OR NEW.candidate_run_id <> OLD.candidate_run_id OR NEW.rule <> OLD.rule THEN
    RAISE EXCEPTION 'eval comparison %: runs and rule are fixed at creation', OLD.id;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER eval_comparisons_decide_once BEFORE UPDATE ON eval_comparisons
  FOR EACH ROW EXECUTE FUNCTION eval_comparisons_decide_once();

CREATE TABLE remediation_rollouts (
  id                     uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id             uuid NOT NULL REFERENCES factories (id),
  proposal_id            uuid NOT NULL UNIQUE REFERENCES remediation_proposals (id),
  from_definition_sha256 text NOT NULL,
  to_definition_sha256   text NOT NULL,
  merged_at              timestamptz NOT NULL,
  watch_until            timestamptz NOT NULL,
  watch_items            integer NOT NULL CHECK (watch_items > 0),
  state                  text NOT NULL DEFAULT 'watching' CHECK (state IN ('watching', 'kept', 'rolled-back')),
  rollback_reason        text,
  rollback_work_item_id  uuid REFERENCES work_items (id),
  decided_at             timestamptz,
  FOREIGN KEY (factory_id, from_definition_sha256) REFERENCES factory_definitions (factory_id, sha256),
  FOREIGN KEY (factory_id, to_definition_sha256) REFERENCES factory_definitions (factory_id, sha256),
  CHECK (watch_until > merged_at),
  CHECK (state <> 'rolled-back' OR rollback_reason IS NOT NULL),
  CHECK ((state = 'watching') = (decided_at IS NULL))
);
CREATE INDEX remediation_rollouts_watching_idx ON remediation_rollouts (watch_until) WHERE state = 'watching';
CREATE INDEX remediation_rollouts_factory_idx ON remediation_rollouts (factory_id);
CREATE INDEX remediation_rollouts_from_def_idx ON remediation_rollouts (factory_id, from_definition_sha256);
CREATE INDEX remediation_rollouts_to_def_idx ON remediation_rollouts (factory_id, to_definition_sha256);
CREATE INDEX remediation_rollouts_rollback_item_idx ON remediation_rollouts (rollback_work_item_id)
  WHERE rollback_work_item_id IS NOT NULL;

-- Automatic rollback: goald runs the pinned definition instead of the newest one until a human
-- releases the pin (normally when the revert pull request merges). Works on any instance, no disk.
CREATE TABLE factory_config_pins (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id        uuid NOT NULL REFERENCES factories (id),
  definition_sha256 text NOT NULL,
  rollout_id        uuid REFERENCES remediation_rollouts (id),
  reason            text NOT NULL,
  pinned_by         text NOT NULL,             -- jonah (rollback trigger) or human:<name>
  pinned_at         timestamptz NOT NULL DEFAULT now(),
  released_by       text CHECK (released_by LIKE 'human:%'),
  released_at       timestamptz,
  FOREIGN KEY (factory_id, definition_sha256) REFERENCES factory_definitions (factory_id, sha256),
  CHECK ((released_at IS NULL) = (released_by IS NULL))
);
CREATE UNIQUE INDEX factory_config_pins_active_key ON factory_config_pins (factory_id) WHERE released_at IS NULL;
CREATE INDEX factory_config_pins_definition_idx ON factory_config_pins (factory_id, definition_sha256);
CREATE INDEX factory_config_pins_rollout_idx ON factory_config_pins (rollout_id) WHERE rollout_id IS NOT NULL;

-- Human-only override that caps Jonah's autonomy (the kill switch is max_autonomy = 'L0').
CREATE TABLE jonah_overrides (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  factory_id   uuid NOT NULL REFERENCES factories (id),
  max_autonomy text NOT NULL CHECK (max_autonomy IN ('L0', 'L1', 'L2', 'L3')),
  reason       text NOT NULL,
  set_by       text NOT NULL CHECK (set_by LIKE 'human:%'),
  set_at       timestamptz NOT NULL DEFAULT now(),
  cleared_at   timestamptz
);
CREATE UNIQUE INDEX jonah_overrides_active_key ON jonah_overrides (factory_id) WHERE cleared_at IS NULL;

-- ---------------------------------------------------------------------------------------------
-- Metrics, quota and audit.
-- ---------------------------------------------------------------------------------------------

-- Rollups for dashboards and drift detection. Always recomputable from the tables above.
CREATE TABLE metric_snapshots (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  factory_id        uuid NOT NULL REFERENCES factories (id),
  level             text NOT NULL CHECK (level IN ('agent', 'station', 'factory', 'suite', 'judge')),
  subject           text NOT NULL,           -- agent, station, suite or judge name; '*' for the factory
  metric            text NOT NULL,           -- pass_at_1, pass_pow_3, first_pass_yield, lead_time_p85_h, …
  window_start      timestamptz NOT NULL,
  window_end        timestamptz NOT NULL,
  value             double precision NOT NULL,
  n                 integer NOT NULL CHECK (n >= 0),
  ci_low            double precision,
  ci_high           double precision,
  definition_sha256 text,
  computed_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (factory_id, level, subject, metric, window_start, window_end),
  CHECK (window_end > window_start),
  CHECK (ci_low IS NULL OR ci_low <= value AND value <= ci_high)
);
CREATE INDEX metric_snapshots_series_idx ON metric_snapshots (factory_id, level, subject, metric, window_end DESC);

-- Rate-limit and usage-limit events from any lane. The eval lane backs off on these.
CREATE TABLE quota_events (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  factory_id    uuid REFERENCES factories (id),
  at            timestamptz NOT NULL DEFAULT now(),
  auth          text NOT NULL CHECK (auth IN ('claude-subscription', 'anthropic-api-key', 'bedrock')),
  lane          text NOT NULL CHECK (lane IN ('production', 'eval', 'judge', 'jonah')),
  kind          text NOT NULL CHECK (kind IN ('rate-limited', 'usage-warning', 'recovered')),
  retry_after_s integer CHECK (retry_after_s >= 0),
  detail        jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX quota_events_at_idx ON quota_events (at DESC);
CREATE INDEX quota_events_factory_idx ON quota_events (factory_id) WHERE factory_id IS NOT NULL;

-- Model spend per lane per day, for the quota policy and the dashboard.
CREATE VIEW model_usage_daily AS
SELECT factory_id, 'production'::text AS lane, date_trunc('day', started_at) AS day,
       sum(cost_usd) AS cost_usd, sum(input_tokens + output_tokens) AS tokens, count(*) AS calls
FROM agent_runs GROUP BY 1, 2, 3
UNION ALL
SELECT factory_id, 'eval', date_trunc('day', queued_at), sum(cost_usd), sum(input_tokens + output_tokens), count(*)
FROM eval_trials GROUP BY 1, 2, 3
UNION ALL
SELECT factory_id, 'judge', date_trunc('day', created_at), sum(cost_usd), NULL::numeric, count(*)
FROM grader_results WHERE judge_version_id IS NOT NULL GROUP BY 1, 2, 3
UNION ALL
SELECT factory_id, 'jonah', date_trunc('day', created_at), sum(cost_usd), sum(input_tokens + output_tokens), count(*)
FROM jonah_tasks GROUP BY 1, 2, 3;

-- Every Jonah decision, human approval and override, append-only. Mirrors item_events.
CREATE TABLE jonah_audit (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  factory_id   uuid NOT NULL REFERENCES factories (id),
  at           timestamptz NOT NULL DEFAULT now(),
  actor        text NOT NULL,                -- jonah, scheduler, engine, human:<name>
  kind         text NOT NULL,                -- signal.opened, diagnosis.made, proposal.verified, rollout.rolled_back, …
  subject_kind text NOT NULL,
  subject_id   uuid,
  data         jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX jonah_audit_factory_at_idx ON jonah_audit (factory_id, at DESC);
CREATE INDEX jonah_audit_subject_idx ON jonah_audit (subject_kind, subject_id);

-- +goose StatementBegin
CREATE FUNCTION jonah_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION '% is append-only (% rejected)', TG_TABLE_NAME, TG_OP;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER jonah_audit_append_only BEFORE UPDATE OR DELETE ON jonah_audit
  FOR EACH ROW EXECUTE FUNCTION jonah_append_only();
CREATE TRIGGER eval_case_versions_append_only BEFORE UPDATE OR DELETE ON eval_case_versions
  FOR EACH ROW EXECUTE FUNCTION jonah_append_only();
CREATE TRIGGER human_labels_append_only BEFORE UPDATE OR DELETE ON human_labels
  FOR EACH ROW EXECUTE FUNCTION jonah_append_only();

-- Realtime for the Jonah screens: every goald instance LISTENs on jonah_events and fans out over SSE.
-- +goose StatementBegin
CREATE FUNCTION notify_jonah_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('jonah_events', json_build_object(
    'id', NEW.id, 'factory_id', NEW.factory_id, 'kind', NEW.kind,
    'subject_kind', NEW.subject_kind, 'subject_id', NEW.subject_id
  )::text);
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER jonah_audit_notify AFTER INSERT ON jonah_audit FOR EACH ROW EXECUTE FUNCTION notify_jonah_audit();
