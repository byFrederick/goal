-- +goose Up
-- Phase 2.2 E10 follow-up: the lease sweep finishes, as cancelled, the runs that lost their
-- instance after a person took their lease (Store.FinishLostRuns). It reads the organization's
-- running runs only; the partial index keeps that read on those few rows, whatever the size of
-- the run history. Row-level security still binds the read to the organization.
CREATE INDEX agent_runs_running_idx ON agent_runs (org_id, started_at) WHERE status = 'running';
