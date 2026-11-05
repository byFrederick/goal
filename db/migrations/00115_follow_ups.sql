-- +goose Up
-- Follow-ups (ADR 0040 decisions 5 and 6): the work an agent noticed outside its item, kept from
-- every accepted run of the item in the transaction that accepts its output, so a rerun no
-- longer erases them. A row records its source (item, run, station, agent), its text, where it
-- goes (target) and its state:
--   waiting   with its target: note (for_factory, for_number: an open item <factory>#<number>),
--             start (start: {at, in, after, title, type, lane, repositories, factory}, checked
--             but not resolved, as in counts from when the work lands) or triage;
--   delivered as a note;
--   filed     as filed_item_id;
--   dropped   with its reason.
-- A triage row may carry the reason it could not go where it said. text_key is the text
-- trimmed, case folded and its whitespace collapsed (domain.FollowUpKey): an item keeps each
-- text once (follow_ups_text_key), whichever run wrote it.
--
-- A follow-up goes with its item and its factory: both keys cascade, so goal_purge_org (00042)
-- and goal_delete_factory (00061) remove follow-ups and need no change. filed_item_id has no key:
-- the item may be another factory's, which its own purge deletes.
CREATE TABLE follow_ups (
  id            uuid PRIMARY KEY,                                        -- UUIDv7 from goald
  org_id        uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id    uuid NOT NULL,
  item_id       uuid NOT NULL,                                           -- the source item
  run_id        uuid,                                                    -- the run that wrote it
  station       text NOT NULL CHECK (length(station) BETWEEN 1 AND 100),
  agent         text NOT NULL CHECK (length(agent) BETWEEN 1 AND 100),
  text          text NOT NULL CHECK (length(text) BETWEEN 1 AND 4000),
  text_key      text NOT NULL CHECK (length(text_key) >= 1),
  target        text CHECK (target IN ('note', 'start', 'triage')),
  for_factory   text CHECK (length(for_factory) BETWEEN 1 AND 100),
  for_number    bigint CHECK (for_number >= 1),
  start         jsonb CHECK (jsonb_typeof(start) = 'object'),
  state         text NOT NULL CHECK (state IN ('waiting', 'delivered', 'filed', 'dropped')),
  reason        text CHECK (length(reason) BETWEEN 1 AND 1000),
  filed_item_id uuid,
  created_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL,
  CHECK (state <> 'waiting' OR target IS NOT NULL),
  CHECK (state <> 'filed' OR filed_item_id IS NOT NULL),
  CHECK (state <> 'dropped' OR reason IS NOT NULL),
  CHECK ((target = 'note') = (for_factory IS NOT NULL AND for_number IS NOT NULL)),
  CHECK ((for_factory IS NULL) = (for_number IS NULL)),
  CHECK ((target = 'start') = (start IS NOT NULL)),
  CONSTRAINT follow_ups_text_key UNIQUE (org_id, item_id, text_key),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, run_id) REFERENCES agent_runs (org_id, id) ON DELETE SET NULL (run_id)
);

-- A factory's follow-ups by state, for the sweeps that deliver and file them and for the
-- factory key's cascade; an item's are follow_ups_text_key; a run's for the run key's.
CREATE INDEX follow_ups_factory_idx ON follow_ups (org_id, factory_id, state, created_at);
CREATE INDEX follow_ups_run_idx     ON follow_ups (org_id, run_id) WHERE run_id IS NOT NULL;

ALTER TABLE follow_ups ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON follow_ups
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON follow_ups TO goal_app;
GRANT UPDATE (target, for_factory, for_number, start, state, reason, filed_item_id, updated_at) ON follow_ups TO goal_app;
