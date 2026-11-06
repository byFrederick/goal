-- +goose Up
-- The CI check of a repository test (ADR 0044 decision 10; 00131's repository_tests): whether
-- the token can read the commit checks (GitHub) or the pipelines (GitLab) a station with
-- waitFor: ci waits on, with the host's one-line reason, scrubbed of the token like the other
-- reasons. NULL ci_ok is a test with no CI check: one kept before this migration, until the
-- repository is tested again, and every owner pattern's. The check never changes ok.
ALTER TABLE repository_tests
  ADD COLUMN ci_ok boolean,
  ADD COLUMN ci_reason text CHECK (char_length(ci_reason) <= 2000);

GRANT UPDATE (ci_ok, ci_reason) ON repository_tests TO goal_app;
