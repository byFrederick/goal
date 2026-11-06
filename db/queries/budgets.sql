-- Weekly budgets used up (ports.Store; migration 00133), under row-level security: every
-- statement sees the transaction's organization only.

-- name: MarkBudgetExhausted :exec
-- org_id comes from its default, goal_org(); the first since is kept. A factory the organization
-- does not have violates the foreign key.
INSERT INTO factory_budget_exhaustions (factory_id, since)
VALUES (sqlc.arg(factory_id), sqlc.arg(since))
ON CONFLICT (org_id, factory_id) DO NOTHING;

-- name: ClearBudgetExhausted :exec
DELETE FROM factory_budget_exhaustions WHERE factory_id = sqlc.arg(factory_id);

-- name: BudgetExhaustedSince :many
-- At most one row: the key is (org_id, factory_id).
SELECT since FROM factory_budget_exhaustions WHERE factory_id = sqlc.arg(factory_id);
