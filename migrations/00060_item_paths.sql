-- +goose Up
-- The files an item is expected to change, as repository-relative globs (ADR 0012 §9; ports.Store
-- Transition.Paths, Assemble). Every item has them, empty until something sets them: Assemble
-- copies each part's planned paths (part_paths, the plan's record, which stays as it is) and a
-- transition may replace them. The overlap hold at claim time will read them; nothing does yet.
ALTER TABLE work_items ADD COLUMN paths text[] NOT NULL DEFAULT '{}';

-- Existing parts take their planned paths. The span trigger fires only on station, state and
-- rework_count (00030), so this writes no span.
UPDATE work_items SET paths = part_paths WHERE part_key IS NOT NULL AND part_paths <> '{}';
