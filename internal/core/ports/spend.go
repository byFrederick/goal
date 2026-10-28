package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Spend reads what the organization in ctx's agent runs cost (agent_runs.cost_usd), for the
// spend screen. Budgets are not here: they stay in each factory's definition
// (runtime.budget dailyUsd and weeklyUsd), and the API sets OrgSpend.Factories against them.
// Box compute time is not recorded, so it is not counted.
type Spend interface {
	// OrgSpend measures w (w.Check must pass; domain.ErrInvalid otherwise), laid out by
	// domain.NewOrgSpend. A run counts on the UTC day it started, at or after a bound's start and
	// before w.Now, with its cost so far; an item counts as merged on the UTC day of its done_at
	// while it is in state done. A run counts as rework once its item entered a rework round of 1
	// or more at or before the run started, which an item reopened after done keeps.
	OrgSpend(ctx context.Context, w domain.SpendWindow) (domain.OrgSpend, error)

	// EachAgentModelSpend calls each with the cost of the runs of factories that started in w's
	// bounds (w.Check must pass; domain.ErrInvalid otherwise), one row per factory, agent and
	// model, ordered by factory id, agent and model (by bytes), without holding them all at once.
	// When there are more than max rows it returns ErrTooMany before it calls each; an error from
	// each stops it and is returned. No factory finds nothing.
	EachAgentModelSpend(ctx context.Context, w domain.SpendWindow, factories []domain.FactoryID, maxRows int,
		each func(domain.AgentModelSpend) error) error
}
