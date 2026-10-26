-- +goose Up
-- The default branch's head when a station with context: [inflight] last finished an item, the
-- commit its plan was made against (ADR 0015 decision 5; ports.Store Transition.BaseCommit,
-- Assemble and AddPart copy the assembly's to its parts). NULL when none was recorded.
ALTER TABLE work_items ADD COLUMN base_commit text;

-- Store.MergedItems finds the items that merged the commits on the default branch since an item's
-- base commit by the sha of their merged event.
CREATE INDEX item_events_merged_sha_idx ON item_events (factory_id, (data->>'sha')) WHERE kind = 'merged';
