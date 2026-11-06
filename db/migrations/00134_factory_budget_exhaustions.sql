-- +goose Up
-- Since when each factory's weekly budget is used up (ports.Store.MarkBudgetExhausted): one row
-- per factory while its spend in the rolling window is at or over runtime.budget.weeklyUsd. The
-- engine inserts the row with its tick's time the first time it finds the budget used up, and a
-- second instance or tick keeps that first since (ON CONFLICT DO NOTHING). It deletes the row
-- once the budget has room again, through a lower spend, a raised cap or a removed budget.
-- When the line resumes is computed on read (domain.BudgetResumesAt), never stored. Rows go with
-- the factory or the organization (ON DELETE CASCADE), so goal_purge_org and goal_delete_factory
-- need no change.
CREATE TABLE factory_budget_exhaustions (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id uuid NOT NULL,
  since      timestamptz NOT NULL,
  PRIMARY KEY (org_id, factory_id),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);

ALTER TABLE factory_budget_exhaustions ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON factory_budget_exhaustions
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT, DELETE ON factory_budget_exhaustions TO goal_app;
