-- +goose Up
-- When an item may start and when it started (ADR 0040 decisions 3 and 4). start_at is the time
-- a person set for it: a queue station does not release it and an agent station does not claim
-- it before then; null starts it as soon as it can. started_at is when it left its line's first
-- station, or when it was filed at a later one; lead time counts from it, so a planned wait does
-- not count against the line's target. Null until then.
--
-- Expand only: old instances running during a deploy neither read nor write either column, so
-- they release an item before its start and leave started_at null on the items they move; lead
-- time then falls back to created_at for those.
--
-- The backfill takes started_at from each item's first span (item_spans_item_idx): when the item
-- left it, or null while it is still open.
ALTER TABLE work_items
  ADD COLUMN start_at   timestamptz,
  ADD COLUMN started_at timestamptz;

UPDATE work_items w SET started_at = s.left_at
FROM (
  SELECT DISTINCT ON (item_id) item_id, left_at FROM item_spans ORDER BY item_id, entered_at, id
) s
WHERE s.item_id = w.id AND s.left_at IS NOT NULL;
