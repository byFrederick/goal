-- +goose Up
-- The latest test of each remote MCP server of a factory's environment (ADR 0037 decision 9;
-- ports.EnvironmentTests): POST .../environments/{env}/test calls the server's tools/list with
-- its headers resolved from secrets and keeps one row per factory, environment and server, the
-- newest replacing the one before. A row holds the tool names the server listed and an error
-- scrubbed of every secret value the test opened, never a value. The Access read model
-- (GET /v1/orgs/{org}/access) reads them beside the published versions. Rows go with the factory
-- or the organization.
CREATE TABLE environment_tests (
  org_id      uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id  uuid NOT NULL,
  environment text NOT NULL CHECK (environment ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  server      text NOT NULL CHECK (server ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  at          timestamptz NOT NULL,
  ok          boolean NOT NULL,
  error       text NOT NULL DEFAULT '' CHECK (char_length(error) <= 2000),
  tools       text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tools) <= 1000),
  -- A test that failed says why; one that passed has no error.
  CHECK (ok = (error = '')),
  PRIMARY KEY (org_id, factory_id, environment, server),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);

ALTER TABLE environment_tests ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON environment_tests
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON environment_tests TO goal_app;
GRANT UPDATE (at, ok, error, tools) ON environment_tests TO goal_app;
