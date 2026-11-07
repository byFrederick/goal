-- +goose Up
-- How the worker measured each run's peak memory (software#902): cgroup is the box's own cgroup
-- (memory.peak or memory.current), machine the whole machine's used memory from /proc/meminfo,
-- read when the box's cgroup has no memory files (apps/worker/src/cgroup.ts). NULL is unknown:
-- every run from before this migration and every run with no peak_memory_bytes. Adding a nullable
-- column without a default rewrites no rows.
ALTER TABLE agent_runs
  ADD COLUMN peak_memory_source text CHECK (peak_memory_source IN ('cgroup', 'machine'));
