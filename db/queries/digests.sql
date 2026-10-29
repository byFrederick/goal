-- The daily digests (ports.Digests; migration 00084; ADR 0023, decision 6). The digests table
-- belongs to the person and has no row-level security: its statements bind no organization and
-- name the rows they touch. The recipients and each digest's content are read per organization,
-- under its row-level security, in a transaction bound to it.

-- name: DigestRecipients :many
-- The members of the transaction's organization with the digest on for email or Slack (their
-- saved choice, else the defaults Go passes), with their time zone (UTC without a saved one) and
-- whether the organization has a connection of a kind named slack (decision 5).
WITH members AS (
  SELECT DISTINCT (m ->> 'user_id')::uuid AS user_id FROM jsonb_array_elements(goal_members()) AS m
),
slack AS (
  SELECT EXISTS (SELECT 1 FROM connections c JOIN connection_kinds k ON k.id = c.kind_id WHERE k.name = 'slack') AS connected
)
SELECT r.user_id, r.time_zone, r.email, r.slack, slack.connected AS slack_connected
FROM (
  SELECT m.user_id, coalesce(ns.time_zone, 'UTC')::text AS time_zone,
         coalesce(p.email, sqlc.arg(default_email)::boolean)::boolean AS email,
         coalesce(p.slack, sqlc.arg(default_slack)::boolean)::boolean AS slack
  FROM members m
  LEFT JOIN notification_settings ns ON ns.user_id = m.user_id
  LEFT JOIN notification_preferences p ON p.user_id = m.user_id AND p.event = 'digest'
) r
CROSS JOIN slack
WHERE r.email OR r.slack
ORDER BY r.user_id;

-- name: QueueDigests :execrows
-- Queues one pending digest per element of digests, a JSON array of objects with user_id,
-- local_date, channel and slack_org ('' for the email digest),
-- due at at and covering [period_start, at): period_start is the end of the person's last sent
-- digest of the same channel and organization, or 24 hours before at. A digest already queued for
-- the same person, local date, channel and organization stays as it is.
INSERT INTO digests (user_id, local_date, channel, slack_org_id, period_start, period_end, next_attempt_at, created_at)
SELECT d.user_id, d.local_date::date, d.channel, nullif(d.slack_org, '')::uuid,
       coalesce((
         SELECT max(s.period_end) FROM digests s
         WHERE s.user_id = d.user_id AND s.channel = d.channel AND s.slack_org_id IS NOT DISTINCT FROM nullif(d.slack_org, '')::uuid
           AND s.state = 'sent' AND s.period_end < sqlc.arg(at)::timestamptz
       ), sqlc.arg(at)::timestamptz - interval '24 hours'),
       sqlc.arg(at)::timestamptz, sqlc.arg(at)::timestamptz, sqlc.arg(at)::timestamptz
FROM jsonb_to_recordset(sqlc.arg(digests)::jsonb) AS d (user_id uuid, local_date text, channel text, slack_org text)
ON CONFLICT ON CONSTRAINT digests_once_per_date DO NOTHING;

-- name: ClaimDigests :many
-- The digest job's claim, as the outbox's (decision 3): at most max due rows of channel (pending,
-- next attempt at or before now, no lease live at now), oldest due first, that no other
-- transaction holds, each leased until until with one more attempt.
WITH due AS (
  SELECT d.id FROM digests d
  WHERE d.channel = sqlc.arg(channel)::text AND d.state = 'pending' AND d.next_attempt_at <= sqlc.arg(now)::timestamptz
    AND (d.lease_until IS NULL OR d.lease_until <= sqlc.arg(now)::timestamptz)
  ORDER BY d.next_attempt_at, d.id
  LIMIT sqlc.arg(max)::int
  FOR UPDATE SKIP LOCKED
)
UPDATE digests g SET lease_until = sqlc.arg(until)::timestamptz, attempts = g.attempts + 1
FROM due WHERE g.id = due.id
RETURNING g.id, g.user_id, g.local_date::text AS local_date, g.channel, g.slack_org_id, g.period_start, g.period_end,
          g.attempts, g.next_attempt_at;

-- name: FinishDigest :execrows
-- Ends the claim of digest id that made attempt attempts, as FinishNotification does.
UPDATE digests
SET state = sqlc.arg(state)::text, sent_at = sqlc.narg(sent_at)::timestamptz, last_error = sqlc.narg(last_error)::text,
    next_attempt_at = coalesce(sqlc.narg(next_attempt_at)::timestamptz, next_attempt_at), lease_until = NULL
WHERE id = sqlc.arg(id) AND state = 'pending' AND attempts = sqlc.arg(attempts)::int;

-- name: DigestMember :many
-- The person as a member of the transaction's organization: their organization role and their
-- address (the notification email, else the sign-in email), with the organization's slug and
-- name. No row when they are no member.
SELECT (m ->> 'role')::text AS role, coalesce(nullif(ns.email, ''), m ->> 'email', '')::text AS address,
       org.slug::text AS org_slug, org.name::text AS org_name
FROM jsonb_array_elements(goal_members()) AS m
JOIN organizations org ON org.id = goal_org()
LEFT JOIN notification_settings ns ON ns.user_id = (m ->> 'user_id')::uuid
WHERE (m ->> 'user_id')::uuid = sqlc.arg(user_id)::uuid
LIMIT 1;

-- name: DigestNeedsYou :one
-- What Needs you lists for the person now (ADR 0023, decision 2): the items in waiting_human
-- they may decide on (factory role approver or editor) and the open andon pulls of the factories
-- where they are an editor. every_factory is true for an owner or an admin, who holds every role.
SELECT
  (SELECT count(*) FROM work_items w
   WHERE w.state = 'waiting_human'
     AND (sqlc.arg(every_factory)::boolean OR EXISTS (
       SELECT 1 FROM factory_roles fr
       WHERE fr.factory_id = w.factory_id AND fr.user_id = sqlc.arg(user_id)::uuid AND fr.role IN ('approver', 'editor'))))::bigint AS decisions,
  (SELECT count(*) FROM andon_pulls a
   WHERE a.cleared_at IS NULL
     AND (sqlc.arg(every_factory)::boolean OR EXISTS (
       SELECT 1 FROM factory_roles fr
       WHERE fr.factory_id = a.factory_id AND fr.user_id = sqlc.arg(user_id)::uuid AND fr.role = 'editor')))::bigint AS andons;

-- name: DigestActivity :one
-- The organization's items done and andons pulled in [since, until).
SELECT
  (SELECT count(*) FROM work_items
   WHERE done_at >= sqlc.arg(since)::timestamptz AND done_at < sqlc.arg(until)::timestamptz)::bigint AS done,
  (SELECT count(*) FROM andon_pulls
   WHERE pulled_at >= sqlc.arg(since)::timestamptz AND pulled_at < sqlc.arg(until)::timestamptz)::bigint AS pulled;

-- name: DigestSpend :many
-- The cost of the runs started in [since, until), by factory, the largest first; factories that
-- spent nothing are left out.
SELECT f.name::text AS factory_name, coalesce(f.title, f.name)::text AS factory_title, sum(r.cost_usd)::float8 AS spend_usd
FROM agent_runs r
JOIN factories f ON f.id = r.factory_id
WHERE r.started_at >= sqlc.arg(since)::timestamptz AND r.started_at < sqlc.arg(until)::timestamptz
GROUP BY f.id, f.name, f.title
HAVING sum(r.cost_usd) > 0
ORDER BY 3 DESC, 1;
