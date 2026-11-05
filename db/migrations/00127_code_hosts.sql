-- +goose Up
-- The organization's code hosts (ADR 0041 decisions 1 and 18): the GitHub Enterprise and
-- self-managed GitLab hosts an owner added, by name in lower case with the API they speak.
-- github.com and gitlab.com are built in, always listed and never stored. goald checks each host
-- (domain.CheckCodeHost) before it writes one; the CHECKs below keep the shape and the built-in
-- names out whatever writes. Rows go with their organization (ON DELETE CASCADE), so
-- goal_purge_org (00042) needs no change.
CREATE TABLE code_hosts (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  host       text NOT NULL CHECK (host = lower(host) AND host ~ '^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$'
                                  AND host LIKE '%.%' AND host NOT IN ('github.com', 'gitlab.com')),
  kind       text NOT NULL CHECK (kind IN ('github', 'gitlab')),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),   -- Actor.Ref()
  PRIMARY KEY (org_id, host)
);

ALTER TABLE code_hosts ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON code_hosts
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON code_hosts TO goal_app;
GRANT UPDATE (kind) ON code_hosts TO goal_app;
