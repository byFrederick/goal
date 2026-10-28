-- +goose Up
-- Jonah (ADR 0034): a review is one agent run per factory, of run kind jonah, that reads the
-- factory's sent-back touches since the last review and outputs observations and numbered
-- proposals. A review is queued pending, claimed running under lease_until (FOR UPDATE SKIP
-- LOCKED; a dead instance's review is claimed again with attempts + 1) and finished done or
-- failed. At most one review is pending or running per factory (jonah_reviews_in_flight_key).
-- Proposals are numbered per factory from 1; each edits only factory.yaml, agents/, skills/,
-- rules/ or contracts/ (decision 4), which goal_jonah_paths_allowed checks on paths, the edits'
-- paths in order (goal_jonah_files_valid ties them to files).
--
-- Rows are never deleted but by the factory's or the organization's purge. The factory keys
-- cascade; the version keys cascade, or set published_version_id null, and both purges delete a
-- factory's definition_versions before the factory; run_id sets null, as both purges delete
-- agent_runs first. So goal_purge_org (00042) and goal_delete_factory (00061) remove the rows
-- and need no change. goal_app inserts and updates the columns a claim, a finish and a decision
-- change, and never deletes. Definers pin their search_path as 00036 explains.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

-- A run is a line's (station) or a Jonah review's (jonah). The new column's default fills every
-- row; the check is added NOT VALID and then validated, as 00043 does.
ALTER TABLE agent_runs ADD COLUMN kind text NOT NULL DEFAULT 'station';
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_kind_check CHECK (kind IN ('station', 'jonah')) NOT VALID;
ALTER TABLE agent_runs VALIDATE CONSTRAINT agent_runs_kind_check;
-- JonahSpendSince's sum of a factory's review runs over the last week.
CREATE INDEX agent_runs_jonah_idx ON agent_runs (org_id, factory_id, started_at) WHERE kind = 'jonah';

CREATE TABLE jonah_reviews (
  id           uuid PRIMARY KEY,                                           -- UUIDv7 from goald
  org_id       uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id   uuid NOT NULL,
  version_id   uuid NOT NULL,                                              -- the version the review reads
  trigger      text NOT NULL CHECK (trigger IN ('manual', 'threshold')),
  state        text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'running', 'done', 'failed')),
  lease_until  timestamptz,
  attempts     integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  run_id       uuid,                                                       -- the review's agent run
  window_start timestamptz NOT NULL,                                       -- the sent-back touches it reads
  window_end   timestamptz NOT NULL,
  error        text NOT NULL DEFAULT '' CHECK (length(error) <= 2000),
  created_by   text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),   -- Actor.Ref()
  created_at   timestamptz NOT NULL,
  finished_at  timestamptz,
  CHECK (window_end >= window_start),
  CHECK ((state = 'running') = (lease_until IS NOT NULL)),
  CHECK ((state IN ('done', 'failed')) = (finished_at IS NOT NULL)),
  UNIQUE (org_id, id),                                                     -- proposals' and observations' key
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (factory_id, version_id) REFERENCES definition_versions (factory_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, run_id) REFERENCES agent_runs (org_id, id) ON DELETE SET NULL (run_id)
);

-- One review in flight per factory (ADR 0034 decision 2): QueueJonahReview's conflict.
CREATE UNIQUE INDEX jonah_reviews_in_flight_key ON jonah_reviews (org_id, factory_id) WHERE state IN ('pending', 'running');
-- A factory's reviews newest first, the claim's oldest due review, and the run key's set null.
CREATE INDEX jonah_reviews_factory_idx ON jonah_reviews (org_id, factory_id, created_at DESC);
CREATE INDEX jonah_reviews_due_idx     ON jonah_reviews (org_id, created_at, id) WHERE state IN ('pending', 'running');
CREATE INDEX jonah_reviews_run_idx     ON jonah_reviews (org_id, run_id) WHERE run_id IS NOT NULL;
CREATE INDEX jonah_reviews_version_idx ON jonah_reviews (factory_id, version_id);

ALTER TABLE jonah_reviews ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON jonah_reviews
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON jonah_reviews TO goal_app;
GRANT UPDATE (state, lease_until, attempts, run_id, error, finished_at) ON jonah_reviews TO goal_app;

-- Every path is one a proposal may edit (decision 4): under the allowlist, without a . or ..
-- segment or a control character, each once, and at least one.
-- +goose StatementBegin
CREATE FUNCTION goal_jonah_paths_allowed(p_paths text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
  SELECT cardinality(p_paths) > 0
     AND cardinality(p_paths) = (SELECT count(DISTINCT p) FROM unnest(p_paths) AS p)
     AND NOT EXISTS (
       SELECT 1 FROM unnest(p_paths) AS p
       WHERE p IS NULL
          OR p !~ '^(factory\.yaml|agents/.+|skills/.+|rules/.+|contracts/.+)$'
          OR p ~ '(^|/)\.\.?(/|$)'
          OR p ~ '[[:cntrl:]]')
$$;
-- +goose StatementEnd

-- files is a non-empty array of {path, base_sha256, content}: base_sha256 is the file's SHA-256
-- in the version the review read, empty for a new file; the contents total at most 256 KiB; and
-- the paths, in order, are p_paths.
-- +goose StatementBegin
CREATE FUNCTION goal_jonah_files_valid(p_files jsonb, p_paths text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
  SELECT CASE WHEN jsonb_typeof(p_files) IS DISTINCT FROM 'array' OR jsonb_array_length(p_files) = 0 THEN false
  ELSE (
    SELECT bool_and(coalesce(jsonb_typeof(f) = 'object'
                             AND jsonb_typeof(f -> 'path') = 'string'
                             AND jsonb_typeof(f -> 'content') = 'string'
                             AND jsonb_typeof(f -> 'base_sha256') = 'string'
                             AND f ->> 'base_sha256' ~ '^([0-9a-f]{64})?$', false))
       AND coalesce(sum(octet_length(f ->> 'content')), 0) <= 262144
       AND array_agg(f ->> 'path' ORDER BY o) = p_paths
    FROM jsonb_array_elements(p_files) WITH ORDINALITY AS e (f, o))
  END
$$;
-- +goose StatementEnd

CREATE TABLE jonah_proposals (
  id                   uuid PRIMARY KEY,                                   -- UUIDv7 from goald
  org_id               uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id           uuid NOT NULL,
  number               bigint NOT NULL CHECK (number >= 1),                -- "Proposal 7", per factory
  review_id            uuid NOT NULL,
  version_id           uuid NOT NULL,                                      -- the version it was made against
  state                text NOT NULL DEFAULT 'proposed'
                       CHECK (state IN ('proposed', 'published', 'applied', 'rejected', 'superseded')),
  title                text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
  summary              text NOT NULL DEFAULT '' CHECK (length(summary) <= 4000),
  files                jsonb NOT NULL,
  evidence             jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(evidence) = 'array' AND octet_length(evidence::text) <= 262144),
  estimate             jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(estimate) = 'object' AND octet_length(estimate::text) <= 4096),
  paths                text[] NOT NULL CHECK (goal_jonah_paths_allowed(paths)),
  published_version_id uuid,                                               -- the version Approve published
  reject_reason        text NOT NULL DEFAULT '' CHECK (length(reject_reason) <= 2000),
  decided_by           text CHECK (decided_by ~ '^(user|service|agent|system):.+$'),
  decided_at           timestamptz,
  created_at           timestamptz NOT NULL,
  CHECK (goal_jonah_files_valid(files, paths)),
  CHECK (state <> 'rejected' OR length(btrim(reject_reason)) > 0),
  CHECK ((state = 'proposed') = (decided_at IS NULL)),
  CHECK ((decided_at IS NULL) = (decided_by IS NULL)),
  CONSTRAINT jonah_proposals_number_key UNIQUE (org_id, factory_id, number),
  UNIQUE (org_id, id),                                                     -- observations' key
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, review_id) REFERENCES jonah_reviews (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (factory_id, version_id) REFERENCES definition_versions (factory_id, id) ON DELETE CASCADE,
  FOREIGN KEY (factory_id, published_version_id) REFERENCES definition_versions (factory_id, id)
    ON DELETE SET NULL (published_version_id)
);

-- A factory's proposals in a state (the overview), and the keys' cascades.
CREATE INDEX jonah_proposals_state_idx     ON jonah_proposals (org_id, factory_id, state);
CREATE INDEX jonah_proposals_review_idx    ON jonah_proposals (org_id, review_id);
CREATE INDEX jonah_proposals_version_idx   ON jonah_proposals (factory_id, version_id);
CREATE INDEX jonah_proposals_published_idx ON jonah_proposals (factory_id, published_version_id) WHERE published_version_id IS NOT NULL;

ALTER TABLE jonah_proposals ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON jonah_proposals
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON jonah_proposals TO goal_app;
GRANT UPDATE (state, published_version_id, reject_reason, decided_by, decided_at) ON jonah_proposals TO goal_app;

CREATE TABLE jonah_observations (
  id          uuid PRIMARY KEY,                                            -- UUIDv7 from goald
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id  uuid NOT NULL,
  review_id   uuid NOT NULL,
  summary     text NOT NULL CHECK (length(summary) BETWEEN 1 AND 500),
  detail      text NOT NULL DEFAULT '' CHECK (length(detail) <= 2000),
  item_ids    uuid[] NOT NULL DEFAULT '{}' CHECK (cardinality(item_ids) <= 1000),   -- the items it rests on
  count       integer NOT NULL CHECK (count >= 0),
  state       text NOT NULL DEFAULT 'watching' CHECK (state IN ('watching', 'proposed')),
  proposal_id uuid,                                                        -- the proposal it became
  created_at  timestamptz NOT NULL,
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, review_id) REFERENCES jonah_reviews (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, proposal_id) REFERENCES jonah_proposals (org_id, id) ON DELETE SET NULL (proposal_id)
);

-- A factory's observations newest first, and the keys' cascades.
CREATE INDEX jonah_observations_factory_idx  ON jonah_observations (org_id, factory_id, created_at DESC);
CREATE INDEX jonah_observations_review_idx   ON jonah_observations (org_id, review_id);
CREATE INDEX jonah_observations_proposal_idx ON jonah_observations (org_id, proposal_id) WHERE proposal_id IS NOT NULL;

ALTER TABLE jonah_observations ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON jonah_observations
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON jonah_observations TO goal_app;
GRANT UPDATE (state, proposal_id) ON jonah_observations TO goal_app;
