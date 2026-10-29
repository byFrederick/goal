-- The notification outbox (migration 00083; ADR 0023, decision 3), under row-level security:
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

-- name: ClaimNotifications :many
-- The sender job's claim (decision 3): at most max due rows of channel (pending, next attempt at
-- or before now, no lease live at now), oldest due first, that no other transaction holds, each
-- leased until until with one more attempt. Each row comes with what its message needs and the
-- facts that say whether its recipient still qualifies: their organization role (blank when no
-- longer a member), their role on the event's factory (blank: none) and their saved channels for the event
-- (NULL without a saved preference). The address is the notification email, else the sign-in
-- email.
WITH due AS (
  SELECT d.id FROM notification_outbox d
  WHERE d.channel = sqlc.arg(channel)::text AND d.state = 'pending' AND d.next_attempt_at <= sqlc.arg(now)::timestamptz
    AND (d.lease_until IS NULL OR d.lease_until <= sqlc.arg(now)::timestamptz)
  ORDER BY d.next_attempt_at, d.id
  LIMIT sqlc.arg(max)::int
  FOR UPDATE SKIP LOCKED
),
claimed AS (
  UPDATE notification_outbox o SET lease_until = sqlc.arg(until)::timestamptz, attempts = o.attempts + 1
  FROM due WHERE o.id = due.id
  RETURNING o.*
),
members AS (
  SELECT (m ->> 'user_id')::uuid AS user_id, m ->> 'email' AS email, m ->> 'role' AS role
  FROM jsonb_array_elements(goal_members()) AS m
)
SELECT c.id, c.event_id, c.event, c.user_id, c.channel, c.payload, c.attempts, c.created_at,
       coalesce(m.role, '')::text AS org_role,
       coalesce(nullif(ns.email, ''), m.email, '')::text AS address,
       coalesce(fr.role, '')::text AS factory_role,
       p.email AS prefers_email, p.slack AS prefers_slack,
       org.slug::text AS org_slug,
       coalesce(f.name, '')::text AS factory_name, coalesce(f.title, f.name, '')::text AS factory_title,
       coalesce(w.number, 0)::bigint AS item_number, coalesce(w.title, '')::text AS item_title
FROM claimed c
JOIN organizations org ON org.id = c.org_id
LEFT JOIN members m ON m.user_id = c.user_id
LEFT JOIN notification_settings ns ON ns.user_id = c.user_id
LEFT JOIN factories f ON f.id = (c.payload ->> 'factory_id')::uuid
LEFT JOIN factory_roles fr ON fr.factory_id = f.id AND fr.user_id = c.user_id
LEFT JOIN work_items w ON w.id = (c.payload ->> 'item_id')::uuid
LEFT JOIN notification_preferences p ON p.user_id = c.user_id AND p.event = c.event
ORDER BY c.next_attempt_at, c.id;

-- name: FinishNotification :execrows
-- Ends the claim of row id that made attempt attempts: sent at sent_at, back to pending until
-- next_attempt_at, or failed or skipped with last_error; the lease is released. No row when a
-- later claim took it.
UPDATE notification_outbox
SET state = sqlc.arg(state)::text, sent_at = sqlc.narg(sent_at)::timestamptz, last_error = sqlc.narg(last_error)::text,
    next_attempt_at = coalesce(sqlc.narg(next_attempt_at)::timestamptz, next_attempt_at), lease_until = NULL
WHERE id = sqlc.arg(id) AND state = 'pending' AND attempts = sqlc.arg(attempts)::int;
