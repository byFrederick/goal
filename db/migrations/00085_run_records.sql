-- +goose Up
-- Run records (ADR 0039, decision 8): what a later try needs to rerun a run. brief is the brief
-- the agent was given (the item's work and the run's feedback), output the run's own output (the
-- item keeps only each station's latest), branch_head and base_head the item branch's and the
-- base's commits before prepare brought the branch up to date, and pushed_head the item branch's
-- commit on the remote when the agent ended. Only a claim's first run prepares its box, so a
-- retry has no branch_head or base_head; a run without a repository has no heads at all.
--
-- The columns are nullable with no default and no backfill: runs from before them have none of
-- this. They are columns of agent_runs, so its row-level security covers them and the purges
-- that delete its rows delete them too.
ALTER TABLE agent_runs
  ADD COLUMN brief       text,
  ADD COLUMN output      jsonb,
  ADD COLUMN branch_head text,
  ADD COLUMN base_head   text,
  ADD COLUMN pushed_head text;
