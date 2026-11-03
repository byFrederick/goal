-- +goose Up
-- The organization policy's MCP tool mode cap (ADR 0037 decision 12): a tool name or glob (*, ?)
-- mapped to allow, ask or off, as domain.ServerToolModes takes its limit. The stricter of a tool's
-- mode and the cap's wins (decision 8). The empty object is no cap, so every existing policy, and
-- the platform default of an organization without a row, keeps today's behaviour. The CHECK is
-- 00075's on connections.tool_modes; domain.OrgPolicy.Validate checks each pattern.
ALTER TABLE org_policies
  ADD COLUMN mcp_tool_modes jsonb NOT NULL DEFAULT '{}'
    CONSTRAINT org_policies_mcp_tool_modes_check CHECK (
      jsonb_typeof(mcp_tool_modes) = 'object' AND octet_length(mcp_tool_modes::text) <= 65536
      AND NOT jsonb_path_exists(mcp_tool_modes, '$.* ? (@.type() != "string" || (@ != "allow" && @ != "ask" && @ != "off"))'));

-- 00069 grants UPDATE on named columns only; goal_app saves the cap with the rest of the policy.
GRANT UPDATE (mcp_tool_modes) ON org_policies TO goal_app;
