-- +goose Up
-- Watches (ADR 0039 decisions 14 and 15): every published change is watched at each station it
-- reaches. A row compares one metric at one station before and after the change (version_id):
-- a Jonah proposal's own number (proposal_id set), or for a person's publish the send-back rate
-- and the cost per item. Each item's outcome at a station is credited to the definition version
-- of its run there (agent_runs.definition_version_id); before covers the versions since the last
-- change that reached the station, after the versions until the next one. goald's watch sweep
-- upserts a row while it is open and closes it once its verdict is final (closed_at), after
-- which nothing changes it.
--
-- An undo proposal (Undo this change) is a proposal goald writes, not a review: undoes names the
-- proposal it reverses, and review_id is null only then. At most one undo of a proposal is
-- proposed, published or applied (jonah_proposals_undoes_key); a rejected one may be written
-- again.
--
-- Rows are never deleted but by the factory's or the organization's purge: the factory and
-- version keys cascade, and both purges delete a factory's definition_versions before the
-- factory, so goal_purge_org (00042) and goal_delete_factory (00061) need no change.
-- +goose StatementBegin
DO $$ BEGIN
  PERFORM set_config('search_path', quote_ident(current_schema()) || ', pg_temp', true);
END $$;
-- +goose StatementEnd

CREATE TABLE watches (
  id            uuid PRIMARY KEY,                                          -- UUIDv7 from goald
  org_id        uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id    uuid NOT NULL,
  version_id    uuid NOT NULL,                                             -- the change watched
  proposal_id   uuid,                                                      -- the Jonah proposal it published
  station       text NOT NULL CHECK (length(station) BETWEEN 1 AND 100),
  metric        text NOT NULL CHECK (metric IN ('sent_back_rate', 'cost_per_item', 'lead_time_p85')),
  before_n      integer NOT NULL CHECK (before_n >= 0),                    -- items counted before
  before_value  float8 NOT NULL,
  after_n       integer NOT NULL CHECK (after_n >= 0),                     -- items counted after
  after_value   float8 NOT NULL,
  interval_low  float8,                                                    -- 90% interval of after - before
  interval_high float8,
  verdict       text NOT NULL CHECK (verdict IN ('too_early', 'helped', 'worse', 'no_change', 'no_clear_change')),
  opened_at     timestamptz NOT NULL,                                      -- the change's publish
  closed_at     timestamptz,                                               -- set once the verdict is final
  updated_at    timestamptz NOT NULL,
  CHECK ((interval_low IS NULL) = (interval_high IS NULL)),
  CHECK (interval_low IS NULL OR interval_low <= interval_high),
  CONSTRAINT watches_key UNIQUE (org_id, factory_id, version_id, station, metric),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (factory_id, version_id) REFERENCES definition_versions (factory_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, proposal_id) REFERENCES jonah_proposals (org_id, id) ON DELETE SET NULL (proposal_id)
);

-- The open watches the sweep recomputes, a proposal's watch, and the keys' cascades.
CREATE INDEX watches_open_idx     ON watches (org_id, factory_id) WHERE closed_at IS NULL;
CREATE INDEX watches_proposal_idx ON watches (org_id, proposal_id) WHERE proposal_id IS NOT NULL;
CREATE INDEX watches_version_idx  ON watches (factory_id, version_id);

ALTER TABLE watches ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON watches
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON watches TO goal_app;
GRANT UPDATE (before_n, before_value, after_n, after_value, interval_low, interval_high, verdict, closed_at, updated_at)
  ON watches TO goal_app;

ALTER TABLE jonah_proposals ADD COLUMN undoes uuid;
ALTER TABLE jonah_proposals ALTER COLUMN review_id DROP NOT NULL;
ALTER TABLE jonah_proposals ADD CONSTRAINT jonah_proposals_undoes_fkey
  FOREIGN KEY (org_id, undoes) REFERENCES jonah_proposals (org_id, id) ON DELETE CASCADE;
ALTER TABLE jonah_proposals ADD CONSTRAINT jonah_proposals_review_check
  CHECK (review_id IS NOT NULL OR undoes IS NOT NULL OR agent IS NOT NULL);
CREATE UNIQUE INDEX jonah_proposals_undoes_key ON jonah_proposals (org_id, undoes)
  WHERE undoes IS NOT NULL AND state IN ('proposed', 'published', 'applied');
