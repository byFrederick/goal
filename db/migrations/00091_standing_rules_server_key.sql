-- +goose Up
-- Standing rules keyed by factory, environment and MCP server (ADR 0037 decisions 8 and 19, step
-- 4; ports.Connections.CreateServerStandingRule). On secrets, a tool's mode lives in an
-- environment file's mcp.<server>.tools and no connection keys a rule: a rule lets one agent role
-- call one tool of one server of one environment of one factory without asking. A row is keyed by
-- its connection (00075's key, connection_id set and the new columns NULL) or by its server
-- (factory_id, environment and server set and connection_id NULL), never both. Moving a factory
-- over copies its connection-keyed rules onto the new key; the originals stay for the audit and
-- for the runs still on connections, until the migration that drops connections.
--
-- factory_id has no foreign key, as connection_id and item_id have none: a rule outlives its
-- factory, as the audit of who allowed what needs.
ALTER TABLE standing_rules
  ALTER COLUMN connection_id DROP NOT NULL,
  ADD COLUMN factory_id uuid,
  ADD COLUMN environment text CHECK (environment ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  ADD COLUMN server text CHECK (server ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  ADD CONSTRAINT standing_rules_key_check CHECK (
    (connection_id IS NOT NULL AND factory_id IS NULL AND environment IS NULL AND server IS NULL)
    OR (connection_id IS NULL AND factory_id IS NOT NULL AND environment IS NOT NULL AND server IS NOT NULL));

-- At most one active rule per server key; 00075's standing_rules_active_key keeps the rule for
-- connection keys, whose NULL connection_id never conflicts here.
CREATE UNIQUE INDEX standing_rules_active_server_key ON standing_rules (org_id, factory_id, environment, server, tool, agent)
  WHERE revoked_at IS NULL AND server IS NOT NULL;

-- 00075's grants cover the new columns: goal_app reads and inserts whole rows and updates only
-- the revocation.
GRANT SELECT, INSERT ON standing_rules TO goal_app;
GRANT UPDATE (revoked_by, revoked_at) ON standing_rules TO goal_app;
