-- +goose Up
-- Which organizations goald seeded the code hosts of (ADR 0041 decision 19): once, at the
-- move-over, with the hosts their published versions and knowledge repository sources already
-- sent tokens to. A row means the seed ran, so a host an owner removes later is never added back.
-- Rows go with their organization (ON DELETE CASCADE), so goal_purge_org (00042) needs no change.
CREATE TABLE code_hosts_seeded (
  org_id    uuid PRIMARY KEY DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  seeded_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE code_hosts_seeded ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON code_hosts_seeded
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON code_hosts_seeded TO goal_app;
