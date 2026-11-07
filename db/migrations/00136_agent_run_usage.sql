-- +goose Up
-- Each run's usage and box (software#827): cpu_seconds and peak_memory_bytes are the box's CPU
-- time and peak memory as the worker reported them on its finished line, and box_size is the
-- sandbox size goald opened the box with (domain.SandboxSize). All three are NULL when unknown:
-- every run from before this migration, a run whose worker reported no counters, and a run whose
-- caller named no size. Adding nullable columns without a default rewrites no rows.
ALTER TABLE agent_runs
  ADD COLUMN cpu_seconds double precision CHECK (cpu_seconds >= 0),
  ADD COLUMN peak_memory_bytes bigint CHECK (peak_memory_bytes >= 0),
  ADD COLUMN box_size text CHECK (box_size IN ('small', 'medium', 'large'));
