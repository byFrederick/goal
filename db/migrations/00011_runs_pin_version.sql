-- +goose Up
-- Phase 2.2 B3 (spec 2.2-B §6.1, §4.3): an agent run records the definition version it started on
-- and finishes on it. The engine starts writing it in B7; runs from before keep NULL ("version
-- unknown"), and no backfill pretends otherwise. The key is composite, so a run can only name a
-- version of its own factory (and so of its own organization).
ALTER TABLE agent_runs ADD COLUMN definition_version_id uuid,
  ADD CONSTRAINT agent_runs_definition_version_fkey
    FOREIGN KEY (factory_id, definition_version_id) REFERENCES definition_versions (factory_id, id);
CREATE INDEX agent_runs_version_idx ON agent_runs (definition_version_id) WHERE definition_version_id IS NOT NULL;
