-- +goose Up
-- Use secrets (ADR 0037 decision 12) renames the organization policy's use_credentials value
-- attached_only to environment: an agent gets what its environment names. off keeps its meaning.
-- The rows are rewritten before the new CHECK, which would refuse the old value. The column keeps
-- its name, so the API's field use_credentials does not change.
ALTER TABLE org_policies DROP CONSTRAINT org_policies_use_credentials_check;

UPDATE org_policies SET use_credentials = 'environment' WHERE use_credentials = 'attached_only';

ALTER TABLE org_policies
  ALTER COLUMN use_credentials SET DEFAULT 'environment',
  ADD CONSTRAINT org_policies_use_credentials_check CHECK (use_credentials IN ('off', 'environment'));
