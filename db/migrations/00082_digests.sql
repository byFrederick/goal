-- +goose Up
-- The daily digests (ADR 0023, decision 6): one row per person, local date and channel, and per
-- organization for Slack, since a Slack workspace belongs to one organization. The digest job of
-- one goald instance at a time (a session advisory lock) queues a person's rows once 08:00 has
-- passed in their time zone (notification_settings.time_zone), and claims and sends them like
-- outbox rows (decision 3). The unique key makes a second queue of the same date insert nothing,
-- across rounds, instances and restarts.
--
-- A row belongs to the person, not to an organization: the email digest covers every
-- organization they are a member of. So, like notification_preferences, the table has no org_id
-- and no row-level security; goald reaches it as goal_app and the store reads each digest's
-- content per organization, under that organization's row-level security. slack_org_id is the
-- organization of a Slack digest and is NULL for the email digest. A row covers
-- [period_start, period_end): from the end of the person's last sent digest of the same channel
-- and organization, or 24 hours before period_end for the first.
CREATE TABLE digests (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id         uuid NOT NULL REFERENCES auth."user" (id) ON DELETE CASCADE,
  local_date      date NOT NULL,
  channel         text NOT NULL CHECK (channel IN ('email', 'slack')),
  slack_org_id    uuid REFERENCES organizations (id) ON DELETE CASCADE,
  period_start    timestamptz NOT NULL,
  period_end      timestamptz NOT NULL,
  state           text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'sent', 'failed', 'skipped')),
  attempts        integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  lease_until     timestamptz,
  last_error      text CHECK (length(last_error) <= 500),
  created_at      timestamptz NOT NULL DEFAULT now(),
  sent_at         timestamptz,
  CHECK ((channel = 'slack') = (slack_org_id IS NOT NULL)),
  CHECK (period_start < period_end),
  CHECK ((state = 'sent') = (sent_at IS NOT NULL)),
  CONSTRAINT digests_once_per_date UNIQUE NULLS NOT DISTINCT (user_id, local_date, channel, slack_org_id)
);
-- The digest job's claim: due pending rows, oldest first.
CREATE INDEX digests_due_idx ON digests (next_attempt_at) WHERE state = 'pending';
-- A new row's period_start: the person's last sent digest of the channel and organization.
CREATE INDEX digests_last_sent_idx ON digests (user_id, channel, slack_org_id, period_end) WHERE state = 'sent';
-- The cascade from organizations.
CREATE INDEX digests_slack_org_idx ON digests (slack_org_id) WHERE slack_org_id IS NOT NULL;
GRANT SELECT, INSERT, UPDATE, DELETE ON digests TO goal_app;
