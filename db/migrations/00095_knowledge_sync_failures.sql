-- +goose Up
-- How many syncs of a source failed in a row (ADR 0038 decision 3): a successful sync resets it, and
-- a source at three shows in Needs you, once for each factory that lists it.
ALTER TABLE knowledge_sources ADD COLUMN sync_failures integer NOT NULL DEFAULT 0 CHECK (sync_failures >= 0);
GRANT UPDATE (sync_failures) ON knowledge_sources TO goal_app;
