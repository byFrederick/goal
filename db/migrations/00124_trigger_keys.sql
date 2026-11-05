-- +goose Up
-- A check's answer applied (ADR 0040 decision 19, "What goald enforces"): in one transaction per
-- check under the trigger's lock, a file with a new key files an item, a file or a note whose key
-- has an open item becomes an item_note on it, and a key whose item closed is filed again only
-- with reopen. trigger_keys keeps each key's last item and its last note, so notes are kept to
-- one per key an hour and repeats are dropped. How and when the item ended is read from the item
-- itself, never copied here. Rows go with their factory's key, so goal_purge_org (00042) and
-- goal_delete_factory (00061) need no change; an item that is deleted sets its key's item null.
CREATE TABLE trigger_keys (
  org_id         uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id     uuid NOT NULL,
  trigger        text NOT NULL CHECK (trigger ~ '^[a-z][a-z0-9-]{1,38}[a-z0-9]$'),
  -- The source's own id for what the item is about, as the check answered it.
  key            text NOT NULL CHECK (length(key) BETWEEN 1 AND 200),
  item_id        uuid,
  last_note_at   timestamptz,
  last_note_text text NOT NULL DEFAULT '' CHECK (length(last_note_text) <= 4000),
  PRIMARY KEY (org_id, factory_id, trigger, key),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE SET NULL (item_id)
);

-- The item's key, which CheckInput gives with its open items; and the item key's cascade.
CREATE INDEX trigger_keys_item_idx ON trigger_keys (org_id, item_id) WHERE item_id IS NOT NULL;

ALTER TABLE trigger_keys ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON trigger_keys
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON trigger_keys TO goal_app;
GRANT UPDATE (item_id, last_note_at, last_note_text) ON trigger_keys TO goal_app;

-- A try check (Try on the trigger's page) runs like any check, but its finish writes only what it
-- would do into applied: no item, note, key, state or limit count. applied is what a done check
-- did: the items it filed, the notes it wrote and what it skipped, with reasons.
ALTER TABLE trigger_firings
  ADD COLUMN try     boolean NOT NULL DEFAULT false,
  ADD COLUMN applied jsonb CHECK (applied IS NULL OR jsonb_typeof(applied) = 'object');
ALTER TABLE trigger_firings ADD CONSTRAINT trigger_firings_try_check CHECK (NOT try OR outcome = 'checked') NOT VALID;
ALTER TABLE trigger_firings VALIDATE CONSTRAINT trigger_firings_try_check;

GRANT UPDATE (applied) ON trigger_firings TO goal_app;

-- The trigger's finished checks that are not tries, newest first: the failures in a row Needs you
-- shows, and the checks of the day that went over the limit.
CREATE INDEX trigger_firings_check_finished_idx ON trigger_firings (org_id, factory_id, trigger, checked_at DESC)
  WHERE check_state IN ('done', 'failed') AND NOT try;
