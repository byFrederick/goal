-- The organization's notification settings (00094), under row-level security: every statement
-- sees the transaction's organization only. The Slack secret is read by its name, which follows a
-- rename.

-- name: OrgNotificationSettings :one
-- The organization's settings; no row when it never saved any. slack_secret is '' when Slack is off.
SELECT coalesce(s.name, '')::text AS slack_secret
FROM org_notification_settings n
LEFT JOIN secrets s ON s.org_id = n.org_id AND s.id = n.slack_secret_id;

-- name: LockOrgNotificationSettings :one
-- The organization's Slack secret id, locked for a change; no row when it never saved settings.
SELECT slack_secret_id FROM org_notification_settings FOR UPDATE;

-- name: SecretIDByName :one
-- The id of secret name, locked against its deletion until the commit; no row when there is none.
SELECT id FROM secrets WHERE name = sqlc.arg(name) FOR KEY SHARE;

-- name: PutOrgNotificationSettings :exec
-- org_id comes from its default, goal_org().
INSERT INTO org_notification_settings (slack_secret_id, updated_by)
VALUES (sqlc.narg(slack_secret_id)::uuid, sqlc.arg(updated_by))
ON CONFLICT (org_id) DO UPDATE
SET slack_secret_id = excluded.slack_secret_id, updated_by = excluded.updated_by, updated_at = now();

-- name: SecretNotifiesSlack :one
-- Whether the notification setting sends Slack with secret name: a secret's deletion is refused
-- while it does.
SELECT EXISTS (
  SELECT 1 FROM org_notification_settings n JOIN secrets s ON s.org_id = n.org_id AND s.id = n.slack_secret_id
  WHERE s.name = sqlc.arg(name)
) AS used;
