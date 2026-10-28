-- +goose Up
-- Tool calls that ask first (ADR 0031, decisions 1, 2 and 7; ports.Connections). A connection's
-- tool_modes map a tool's name, as its MCP server lists it, to allow, ask or off; a tool not in the
-- map is allow, so every existing connection keeps its behavior. last_test_tools are the tool names
-- the last test listed, sorted, or NULL when it listed none. A mode for a tool the last test did not
-- list is kept.
ALTER TABLE connections
  ADD COLUMN tool_modes jsonb NOT NULL DEFAULT '{}'
    CONSTRAINT connections_tool_modes_check CHECK (
      jsonb_typeof(tool_modes) = 'object' AND octet_length(tool_modes::text) <= 65536
      AND NOT jsonb_path_exists(tool_modes, '$.* ? (@.type() != "string" || (@ != "allow" && @ != "ask" && @ != "off"))')),
  ADD COLUMN last_test_tools text[];

-- The grant on connections is column by column (00051, 00053, 00070).
GRANT UPDATE (tool_modes, last_test_tools) ON connections TO goal_app;

-- A standing rule lets one agent role (its name in the definition) call one tool of one connection
-- without asking, in every factory of the organization. A rule is never deleted: revoking it sets
-- revoked_at and revoked_by, and deleting its connection revokes it. connection_id and item_id have
-- no foreign key, so a rule outlives its connection, its item and the item's factory, as the audit
-- of who allowed what needs. At most one rule per key is active.
CREATE TABLE standing_rules (
  id            uuid PRIMARY KEY,                                          -- UUIDv7 from goald
  org_id        uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  connection_id uuid NOT NULL,
  tool          text NOT NULL CHECK (tool ~ '^[A-Za-z0-9_.-]{1,128}$'),
  agent         text NOT NULL CHECK (agent ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  created_by    text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),    -- Actor.Ref()
  created_at    timestamptz NOT NULL DEFAULT now(),
  item_id       uuid NOT NULL,                                             -- the item it was approved on
  revoked_by    text CHECK (revoked_by ~ '^(user|service|agent|system):.+$'),
  revoked_at    timestamptz,
  CHECK ((revoked_by IS NULL) = (revoked_at IS NULL))
);
-- The list, a connection's deletion and the purge by organization read this index; the partial one
-- only holds the one-active-rule-per-key rule.
CREATE INDEX standing_rules_org_connection_idx ON standing_rules (org_id, connection_id);
CREATE UNIQUE INDEX standing_rules_active_key ON standing_rules (org_id, connection_id, tool, agent) WHERE revoked_at IS NULL;

ALTER TABLE standing_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON standing_rules
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT ON standing_rules TO goal_app;
GRANT UPDATE (revoked_by, revoked_at) ON standing_rules TO goal_app;
