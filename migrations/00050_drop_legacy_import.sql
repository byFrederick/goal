-- +goose Up
-- Phase 4 (spec 4 §5.7, ADR 0005 amendment): the legacy import of the bucket layout from before
-- versions is gone with object versions, and with it 00039's record of the organizations that
-- still had one to do. goald refuses to migrate a database where one still has (postgres.Migrate
-- checks before this file runs: start the previous release once), so nothing pending is lost
-- here. No function used the table; its queries went with the import.
DROP TABLE legacy_import_pending;
