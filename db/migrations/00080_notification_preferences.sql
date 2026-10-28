-- +goose Up
-- Each person's notification preferences (ADR 0023, decision 1). They belong to the person and
-- hold in every organization they are a member of, so neither table has org_id or row-level
-- security: goald reaches them directly as goal_app, and the store filters every statement by the
-- person's user id. A missing row reads as the ADR's defaults, in Go (domain), so a person who
-- never saved costs no row. Both go with their user (ON DELETE CASCADE).

-- One row per person and event: the channels the event is sent on. The digest has no In Goal
-- channel.
CREATE TABLE notification_preferences (
  user_id uuid NOT NULL REFERENCES auth."user" (id) ON DELETE CASCADE,
  event   text NOT NULL CHECK (event IN ('approval', 'question', 'andon', 'budget', 'digest')),
  in_goal boolean NOT NULL,
  email   boolean NOT NULL,
  slack   boolean NOT NULL,
  PRIMARY KEY (user_id, event),
  CONSTRAINT notification_preferences_digest_not_in_goal CHECK (event <> 'digest' OR NOT in_goal)
);

-- One row per person: the notification email (blank: the sign-in email) and the IANA time zone
-- the digest goes out in.
CREATE TABLE notification_settings (
  user_id   uuid PRIMARY KEY REFERENCES auth."user" (id) ON DELETE CASCADE,
  email     text NOT NULL DEFAULT '' CHECK (length(email) <= 320),
  time_zone text NOT NULL DEFAULT 'UTC' CHECK (time_zone <> '')
);

GRANT SELECT, INSERT, UPDATE, DELETE ON notification_preferences, notification_settings TO goal_app;
