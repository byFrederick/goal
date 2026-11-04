-- +goose Up
-- Follow-ups filed when work lands (ADR 0040 decisions 8, 9, 10 and 21). Each goald tick, after
-- delivering notes, takes the waiting start and triage follow-ups of one landed root item (an
-- item that is not a part, done, with its parts) at a time, FOR UPDATE SKIP LOCKED:
--   start   becomes an item through domain.File (origin follow-up, created_by NULL: the engine),
--           or is filed onto an open item already filed from the same source with that title;
--   triage  joins the factory's collecting triage item, which goald files when there is none.
-- A follow-up of a cancelled item or part is dropped, and one whose source is generation 3 or
-- deeper is dropped too. A receiving factory takes 20 follow-up items a day that the engine filed
-- (triage items aside), counted under an advisory lock on the factory; past it they go to triage.
--
-- triage marks a triage item. It collects while it is neither done nor cancelled and has no run;
-- the first claim ends collecting, and the next follow-ups open another. A follow-up that joined
-- one is filed with filed_item_id the triage item and keeps target triage, so "one in a closed
-- triage item" is a filed triage row whose item is done or cancelled: a person may File it again.
ALTER TABLE work_items ADD COLUMN triage boolean NOT NULL DEFAULT false;

-- The collecting triage item of a factory (FindTriageItem).
CREATE INDEX work_items_triage_idx ON work_items (factory_id, created_at)
  WHERE triage AND state NOT IN ('done', 'cancelled');

-- The follow-up items the engine filed into a factory by time, for the daily cap.
CREATE INDEX work_items_follow_up_cap_idx ON work_items (factory_id, created_at)
  WHERE origin = 'follow-up' AND created_by IS NULL AND NOT triage;

-- The waiting start and triage follow-ups, oldest first, for the sweep's claim.
CREATE INDEX follow_ups_waiting_filing_idx ON follow_ups (org_id, created_at, id)
  WHERE state = 'waiting' AND target IN ('start', 'triage');

-- The follow-ups filed onto an item: a triage item's, and an item's for the dedupe by title.
CREATE INDEX follow_ups_filed_item_idx ON follow_ups (org_id, filed_item_id) WHERE filed_item_id IS NOT NULL;
