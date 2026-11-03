-- +goose Up
-- The organization's notification settings (ADR 0037 decision 14 and decision 19 step 5, and its
-- Consequences on ADR 0023): one row per organization naming the secret that holds the Slack bot
-- token. No row, or a null secret, means Slack is off: the outbox and the digests queue no Slack
-- rows. An organization without a row has Slack off, so this migration writes no rows.
--
-- The setting refers to the secret by id, so renaming the secret keeps the reference. NO ACTION,
-- checked at the statement's end, as 00089's knowledge source key: an organization's purge removes
-- its secrets and its settings in one cascade in whatever order; alone, the secret the setting
-- uses cannot be deleted (ports.OrgSecrets.Delete: ErrConflict).
CREATE TABLE org_notification_settings (
  org_id          uuid PRIMARY KEY DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  slack_secret_id uuid,
  updated_by      text NOT NULL CHECK (updated_by ~ '^(user|service|agent|system):.+$'),   -- Actor.Ref()
  updated_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT org_notification_settings_slack_secret_fkey FOREIGN KEY (org_id, slack_secret_id) REFERENCES secrets (org_id, id)
);

ALTER TABLE org_notification_settings ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON org_notification_settings
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON org_notification_settings TO goal_app;
GRANT UPDATE (slack_secret_id, updated_by, updated_at) ON org_notification_settings TO goal_app;
