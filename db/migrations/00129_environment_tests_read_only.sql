-- +goose Up
-- The names of the tools each environment test's server marked read-only (ADR 0041 decision 15;
-- ports.EnvironmentTests): the tools whose annotations.readOnlyHint was true in its tools/list.
-- MCP's default for a missing hint is false, so every other tool of the row is not read-only. The
-- publish warning (decision 14) judges an agent's GitHub tools by them. Each name is one of the
-- row's tools. Rows kept before this migration have none, until their server is tested again.
ALTER TABLE environment_tests
  ADD COLUMN read_only_tools text[] NOT NULL DEFAULT '{}' CHECK (read_only_tools <@ tools);

GRANT UPDATE (read_only_tools) ON environment_tests TO goal_app;
