-- +goose Up
-- Clear on a check trigger's page (ADR 0040 decision 19, "Where it shows") deletes the trigger's
-- saved state, so its next check starts with none. trigger_keys keeps its rows: they stop a key
-- whose item closed from being filed again. Row-level security (00122) keeps the delete inside
-- the organization.
GRANT DELETE ON trigger_state TO goal_app;
