-- +goose Up
-- Use secrets (ADR 0037 decision 12): the organization policy's use_credentials takes environment,
-- an agent gets what its environment names, in place of attached_only. The CHECK keeps
-- attached_only, the deprecated alias goald saves as environment, so an older goald still running
-- during a rollout can save; existing rows move to environment and new rows default to it. The
-- column keeps its name.
ALTER TABLE org_policies
  DROP CONSTRAINT org_policies_use_credentials_check,
  ADD CONSTRAINT org_policies_use_credentials_check CHECK (use_credentials IN ('off', 'environment', 'attached_only')),
  ALTER COLUMN use_credentials SET DEFAULT 'environment';

UPDATE org_policies SET use_credentials = 'environment' WHERE use_credentials = 'attached_only';
