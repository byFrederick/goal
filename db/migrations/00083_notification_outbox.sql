-- +goose Up
-- The notification outbox (ADR 0023, decision 3): one row per recipient and channel (email or
-- Slack) of an event someone may hear about outside the app. The store writes the rows in the
-- transaction that records the event (a waiting_human or question_asked item event, an andon
-- pull) and the budget crossing the engine notices; the sender job of the email and Slack parts
-- claims and sends them. In Goal needs no row: the stream already drives the badge and toast.
--
-- event_id names the event: 'event/<item_events.id>', 'andon/<andon_pulls.id>' or
-- 'budget/<factory id>/<crossing time>'. Every insert is ON CONFLICT DO NOTHING on
-- (event_id, user_id, channel), so writing an event twice queues it once. payload holds ids and
-- the event's facts only, never an address or a secret.
CREATE TABLE notification_outbox (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id          uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  event_id        text NOT NULL CHECK (event_id ~ '^(event|andon|budget)/.+$' AND length(event_id) <= 200),
  event           text NOT NULL CHECK (event IN ('approval', 'question', 'andon', 'budget')),
  user_id         uuid NOT NULL REFERENCES auth."user" (id) ON DELETE CASCADE,
  channel         text NOT NULL CHECK (channel IN ('email', 'slack')),
  payload         jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 4096),
  state           text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'sent', 'failed', 'skipped')),
  attempts        integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  lease_until     timestamptz,
  last_error      text CHECK (length(last_error) <= 500),
  created_at      timestamptz NOT NULL DEFAULT now(),
  sent_at         timestamptz,
  CHECK ((state = 'sent') = (sent_at IS NOT NULL)),
  UNIQUE (event_id, user_id, channel)
);
-- The sender job's claim: due pending rows, oldest first.
CREATE INDEX notification_outbox_due_idx ON notification_outbox (next_attempt_at) WHERE state = 'pending';
-- The organization's rows by event: the budget's once-per-window check and the 30-day cleanup.
CREATE INDEX notification_outbox_org_event_idx ON notification_outbox (org_id, event, created_at);
ALTER TABLE notification_outbox ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON notification_outbox
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT, UPDATE, DELETE ON notification_outbox TO goal_app;
