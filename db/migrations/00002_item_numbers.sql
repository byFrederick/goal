-- +goose Up
-- Per-factory item numbers come from a counter on the factory row. Creating an item increments
-- it in the same transaction, so concurrent creates never collide and a rolled-back create
-- leaves no gap.
ALTER TABLE factories ADD COLUMN last_item_number bigint NOT NULL DEFAULT 0 CHECK (last_item_number >= 0);
