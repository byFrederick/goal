-- +goose Up
-- A refused factory conversion is tried again at each goald start (ADR 0037 decision 19): moving
-- over tests the converted version's repository token and remote MCP servers before it publishes,
-- and a check that fails is refused until its cause is fixed. The next try replaces the refused
-- row (db/queries/conversions.sql, InsertFactoryConversion's ON CONFLICT ... WHERE to_version IS
-- NULL); a converted row is never replaced. The org_isolation policy already covers UPDATE.
GRANT UPDATE ON factory_conversions TO goal_app;
