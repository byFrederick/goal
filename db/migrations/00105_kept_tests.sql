-- +goose Up
-- Kept tests (ADR 0039 decision 12): a person keeps a past item, at a station it passed the first
-- time, as a test the item must keep passing there. A failing kept test warns at publish and
-- never blocks one. kept_by is the actor ref of who kept it (domain.Actor.Ref).
--
-- A test goes with its item and its factory: both keys cascade, so goal_purge_org (00042) and
-- goal_delete_factory (00061) remove kept tests and need no change, as for tries (00102).
-- goal_app keeps and removes tests and never updates one.
CREATE TABLE kept_tests (
  id         uuid PRIMARY KEY,                                        -- UUIDv7 from goald
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id uuid NOT NULL,
  item_id    uuid NOT NULL,
  station    text NOT NULL CHECK (length(station) BETWEEN 1 AND 100),
  kept_by    text NOT NULL CHECK (length(kept_by) BETWEEN 1 AND 300),
  kept_at    timestamptz NOT NULL,
  -- An item is kept at a station once.
  CONSTRAINT kept_tests_key UNIQUE (org_id, item_id, station),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE
);

-- A factory's tests, newest first; the item key's cascade uses kept_tests_key.
CREATE INDEX kept_tests_factory_idx ON kept_tests (org_id, factory_id, kept_at DESC, id DESC);

ALTER TABLE kept_tests ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON kept_tests
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON kept_tests TO goal_app;
