-- +goose Up
-- What happened to each run's build cache (software#934), the Go module and build caches and the
-- pnpm store its box restores while it prepares: hit restored a matching cache, miss found none
-- and the box saved one, off is a factory that turned it off, error is a restore that failed, so
-- the run went on cold. NULL means the run had no Go or pnpm repository, or it ran before the
-- cache existed. Adding a nullable column without a default rewrites no rows.
ALTER TABLE agent_runs
  ADD COLUMN build_cache text CHECK (build_cache IN ('hit', 'miss', 'off', 'error'));
