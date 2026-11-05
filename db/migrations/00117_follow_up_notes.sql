-- +goose Up
-- Follow-up notes delivered (ADR 0040 decision 7): each goald tick takes the waiting notes of
-- follow_ups (target note) oldest first, FOR UPDATE SKIP LOCKED, and turns each into an
-- item_note event on the item its for names, or sends it to triage with the reason. An
-- item_note is the engine's event (actor engine, data.actor system:engine), never a person's
-- touch. A factory takes 50 notes from other factories a day: the count reads the item_note
-- events of the target factory in the last 24 hours.
--
-- No table changes: item_events.kind is free text, and goal_app may already update the columns
-- a delivery or a triage sets (00114).

-- The waiting notes, oldest first, for the sweep's claim.
CREATE INDEX follow_ups_waiting_notes_idx ON follow_ups (org_id, created_at, id)
  WHERE state = 'waiting' AND target = 'note';

-- A factory's item_note events by time, for the daily cap.
CREATE INDEX item_events_item_notes_idx ON item_events (factory_id, at) WHERE kind = 'item_note';
