-- +goose Up
-- Phase 2.2 A1: migrations run as goal_owner, which owns every object, and goald runs as goal_app,
-- which owns nothing (internal/adapters/postgres/roles.sql creates both; `goal db init` lets them
-- log in). There are no default privileges: every migration grants goal_app exactly what goald
-- needs on the objects it adds, so a forgotten grant fails loudly instead of leaving a table open.
-- These are the grants for the objects of 00001-00003. SELECT ... FOR UPDATE needs UPDATE.
GRANT SELECT, INSERT, UPDATE ON factories, work_items, agent_runs TO goal_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON leases, andon_pulls TO goal_app;
-- Append-only: goal_app never updates or deletes an event.
GRANT SELECT, INSERT ON item_events TO goal_app;
GRANT SELECT ON station_wip TO goal_app;
