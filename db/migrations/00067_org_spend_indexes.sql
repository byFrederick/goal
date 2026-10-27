-- +goose Up
-- The organization's spend screen (ports.Spend.OrgSpend, db/queries/spend.sql) reads every run
-- an organization started in a window, and every item it finished in one, across its factories.
-- Row-level security adds org_id = goal_org() to those reads; the existing indexes lead with
-- factory_id (agent_runs_factory_started_idx, work_items_done_at_idx) or cover running runs only
-- (agent_runs_running_idx), so the reads need indexes that lead with org_id.
CREATE INDEX agent_runs_org_started_idx ON agent_runs (org_id, started_at);
CREATE INDEX work_items_org_done_at_idx ON work_items (org_id, done_at) WHERE done_at IS NOT NULL;
