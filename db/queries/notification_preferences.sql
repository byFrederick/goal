-- Each person's notification preferences (ports.NotificationPreferences; migration 00080). The
-- tables belong to no organization and have no row-level security: every statement filters by
-- the person's user id, and no other statement may read them.

-- name: NotificationPreferences :many
SELECT event, in_goal, email, slack FROM notification_preferences WHERE user_id = sqlc.arg(user_id);

-- name: NotificationSettings :many
-- No row: the defaults.
SELECT email, time_zone FROM notification_settings WHERE user_id = sqlc.arg(user_id);

-- name: PutNotificationPreference :exec
INSERT INTO notification_preferences (user_id, event, in_goal, email, slack)
VALUES (sqlc.arg(user_id), sqlc.arg(event), sqlc.arg(in_goal), sqlc.arg(email), sqlc.arg(slack))
ON CONFLICT (user_id, event) DO UPDATE SET in_goal = EXCLUDED.in_goal, email = EXCLUDED.email, slack = EXCLUDED.slack;

-- name: PutNotificationSettings :exec
INSERT INTO notification_settings (user_id, email, time_zone)
VALUES (sqlc.arg(user_id), sqlc.arg(email), sqlc.arg(time_zone))
ON CONFLICT (user_id) DO UPDATE SET email = EXCLUDED.email, time_zone = EXCLUDED.time_zone;
