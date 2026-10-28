-- The notification outbox (migration 00081; ADR 0023, decision 3), under row-level security:
-- every statement runs in the transaction of the event's organization.

-- name: QueueNotifications :execrows
-- Queues one pending row per recipient and per channel that is on, for event event_id of kind
-- event at factory. The recipients are the organization's owners and admins plus the members
-- holding one of factory_roles on factory (members come from goal_members, since goal_app cannot
-- read schema auth), never the actor (an Actor ref; '' excludes nobody). A person without a saved
-- preference for the event has the defaults, which Go passes (domain.DefaultNotificationPreferences).
-- Slack is on only while the organization has a connection of a kind named slack (decision 5).
WITH recipients AS (
  SELECT (m ->> 'user_id')::uuid AS user_id
  FROM jsonb_array_elements(goal_members()) AS m
  WHERE (m ->> 'role' IN ('owner', 'admin') OR EXISTS (
          SELECT 1 FROM factory_roles fr
          WHERE fr.factory_id = sqlc.arg(factory_id)::uuid AND fr.user_id = (m ->> 'user_id')::uuid
            AND fr.role = ANY (sqlc.arg(factory_roles)::text[])))
    AND 'user:' || (m ->> 'user_id') <> sqlc.arg(actor)::text
),
slack AS (
  SELECT EXISTS (SELECT 1 FROM connections c JOIN connection_kinds k ON k.id = c.kind_id WHERE k.name = 'slack') AS connected
)
INSERT INTO notification_outbox (event_id, event, user_id, channel, payload, next_attempt_at, created_at)
SELECT sqlc.arg(event_id)::text, sqlc.arg(event)::text, r.user_id, ch.channel, sqlc.arg(payload)::jsonb,
       sqlc.arg(at)::timestamptz, sqlc.arg(at)::timestamptz
FROM recipients r
CROSS JOIN slack
LEFT JOIN notification_preferences p ON p.user_id = r.user_id AND p.event = sqlc.arg(event)::text
CROSS JOIN LATERAL (VALUES
  ('email', coalesce(p.email, sqlc.arg(default_email)::boolean)),
  ('slack', coalesce(p.slack, sqlc.arg(default_slack)::boolean) AND slack.connected)
) AS ch (channel, enabled)
WHERE ch.enabled
ON CONFLICT (event_id, user_id, channel) DO NOTHING;

-- name: LatestItemEventID :one
-- The newest event of kind on item: the one this transaction just appended.
SELECT id FROM item_events WHERE item_id = sqlc.arg(item_id) AND kind = sqlc.arg(kind) ORDER BY id DESC LIMIT 1;

-- name: LockBudgetNotification :exec
-- Serializes the budget crossings of one factory across goald instances until the commit.
SELECT pg_advisory_xact_lock(hashtextextended('goal.notify.budget.' || sqlc.arg(factory_id)::uuid::text, 0));

-- name: BudgetNotificationSince :one
-- Whether the factory's budget crossing was queued at or after since.
SELECT EXISTS (
  SELECT 1 FROM notification_outbox
  WHERE event = 'budget' AND created_at >= sqlc.arg(since)::timestamptz
    AND event_id LIKE 'budget/' || sqlc.arg(factory_id)::uuid::text || '/%'
) AS queued;
