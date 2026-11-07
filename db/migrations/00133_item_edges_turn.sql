-- +goose Up
-- Turn edges (ADR 0043): at a merge station that takes turns, the engine makes each waiting item
-- need the item that holds its merge target's turn, so one item per target merges at a time.
-- Widens item_edges' kind check (00058) to 'turn'; existing rows all pass it.
ALTER TABLE item_edges DROP CONSTRAINT item_edges_kind_check;
ALTER TABLE item_edges ADD CONSTRAINT item_edges_kind_check CHECK (kind IN ('planned', 'linked', 'overlap', 'turn'));
