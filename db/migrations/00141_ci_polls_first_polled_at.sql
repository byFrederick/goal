-- +goose Up
-- When a commit's poll first read its checks (ADR 0044 decision 7, amended 2026-11-08). Every CI
-- wait on the commit counts its grace from it, so a commit that showed no check for longer than a
-- station's grace ends that station's wait at its first poll instead of waiting the grace again.
-- RecordCIPoll sets it once, as it does first_check_seen_at. A poll recorded before this
-- migration keeps NULL until its next poll, and its waits count the grace from their own start.
ALTER TABLE ci_polls ADD COLUMN first_polled_at timestamptz;

GRANT UPDATE (first_polled_at) ON ci_polls TO goal_app;
