-- +goose Up
-- The organization policy (ADR 0024 decisions 1 and 2): one row per organization, a ceiling over
-- every factory definition. An organization without a row has the platform default, which goald
-- computes from GOAL_MODELS, so this migration writes no rows. Each tool default is text with the
-- values ADR 0024 lists; revision counts the saves and fences a save against a concurrent one.

CREATE TABLE org_policies (
  org_id                 uuid PRIMARY KEY DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  allowed_models         text[] NOT NULL
                         CHECK (cardinality(allowed_models) BETWEEN 1 AND 100 AND array_position(allowed_models, NULL) IS NULL),
  default_model          text NOT NULL CHECK (default_model = ANY (allowed_models)),
  -- Claude Code Bash permission patterns, such as Bash(terraform apply:*).
  denied_commands        text[] NOT NULL DEFAULT '{}'
                         CHECK (cardinality(denied_commands) <= 1000 AND array_position(denied_commands, NULL) IS NULL
                                AND NOT ('' = ANY (denied_commands))),
  write_outside_branch   text NOT NULL DEFAULT 'ask' CHECK (write_outside_branch IN ('off', 'ask', 'allowed')),
  merge_pull_request     text NOT NULL DEFAULT 'review_approval' CHECK (merge_pull_request IN ('off', 'review_approval')),
  read_web               text NOT NULL DEFAULT 'allowed' CHECK (read_web IN ('off', 'ask', 'allowed')),
  use_credentials        text NOT NULL DEFAULT 'attached_only' CHECK (use_credentials IN ('off', 'attached_only')),
  box_time_limit_minutes integer NOT NULL DEFAULT 180 CHECK (box_time_limit_minutes > 0),
  revision               bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  updated_by             text NOT NULL CHECK (updated_by ~ '^(user|service|agent|system):.+$'),   -- Actor.Ref()
  updated_at             timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE org_policies ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON org_policies
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON org_policies TO goal_app;
GRANT UPDATE (allowed_models, default_model, denied_commands, write_outside_branch, merge_pull_request, read_web,
              use_credentials, box_time_limit_minutes, revision, updated_by, updated_at) ON org_policies TO goal_app;
