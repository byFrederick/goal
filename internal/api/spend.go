package api

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The spend read (tag spend): what the organization's agent runs cost (ports.Spend), over the
// factories the caller sees, and each factory's budget from its current version.

func (s *server) GetOrgSpend(ctx context.Context, req gen.GetOrgSpendRequestObject) (gen.GetOrgSpendResponseObject, error) {
	period := domain.SpendThisWeek
	if req.Params.Window != nil {
		period = domain.SpendPeriod(*req.Params.Window)
	}

	now := s.clock.Now().UTC()
	w := domain.SpendWindow{Period: period, Now: now}

	if err := w.Check(); err != nil {
		return nil, fmt.Errorf("%w: window must be week, month or 90d", errInvalid)
	}

	sp, err := s.items.OrgSpend(ctx, w)
	if err != nil {
		return nil, fmt.Errorf("read the organization's spend: %w", err)
	}

	out := s.spendOut(ctx, sp)

	out.Budgets, err = s.spendBudgets(ctx, sp, now)
	if err != nil {
		return nil, err
	}

	return gen.GetOrgSpend200JSONResponse(out), nil
}

// spendOut is sp with only the factories the caller sees. When a factory it does not see spent,
// the answer is not complete: totals are summed again from the rows it may see, and the figures
// the store does not split by factory are left out, so none shows what a hidden factory spent.
func (s *server) spendOut(ctx context.Context, sp domain.OrgSpend) gen.OrgSpend {
	seen := map[domain.FactoryID]bool{}
	sees := func(f domain.FactoryID) bool {
		v, ok := seen[f]
		if !ok {
			v = s.visible(ctx, f)
			seen[f] = v
		}

		return v
	}

	complete := true
	for _, f := range sp.Factories {
		complete = complete && sees(f.FactoryID)
	}

	out := gen.OrgSpend{
		Window: gen.SpendWindow(sp.Period), Since: sp.Since, Until: sp.Until, Complete: complete,
		Days: make([]gen.SpendDay, 0, len(sp.Days)), ByAgent: []gen.AgentSpend{}, ByModel: []gen.ModelSpend{}, Weekly: []gen.WeekCost{},
		Budgets: []gen.SpendBudget{},
	}

	for _, d := range sp.Days {
		day := gen.SpendDay{Day: openapi_types.Date{Time: d.Day}, ByFactory: []gen.FactorySpend{}}

		for _, f := range d.ByFactory {
			if sees(f.FactoryID) {
				day.ByFactory = append(day.ByFactory, gen.FactorySpend{FactoryId: string(f.FactoryID), Usd: f.USD})
				day.Usd += f.USD
			}
		}

		out.TotalUsd += day.Usd
		out.Days = append(out.Days, day)
	}

	for _, a := range sp.ByAgent {
		if sees(a.FactoryID) {
			out.ByAgent = append(out.ByAgent, gen.AgentSpend{FactoryId: string(a.FactoryID), Agent: a.Agent, Usd: a.USD})
		}
	}

	if !complete {
		return out
	}

	out.TotalUsd = sp.TotalUSD
	out.ThisMonthUsd, out.LastMonthUsd = &sp.ThisMonthUSD, &sp.LastMonthUSD
	out.Merged, out.CostPerMergedUsd = &sp.Merged, &sp.CostPerMergedUSD
	out.ReworkUsd, out.ReworkShare = &sp.ReworkUSD, &sp.ReworkShare

	for _, m := range sp.ByModel {
		out.ByModel = append(out.ByModel, gen.ModelSpend{Model: m.Model, Usd: m.USD})
	}

	for _, wk := range sp.Weekly {
		out.Weekly = append(out.Weekly, gen.WeekCost{
			WeekStart: openapi_types.Date{Time: wk.WeekStart}, SpendUsd: wk.SpendUSD, Merged: wk.Merged, CostPerMergedUsd: wk.CostPerMergedUSD,
		})
	}

	return out
}

// spendBudgets is one budget per factory goald runs that the caller sees and that is not
// archived, by name: its caps, its spend today (UTC, from sp) and in the rolling window the
// engine enforces the weekly cap over, its state and the model connections that pay for it.
func (s *server) spendBudgets(ctx context.Context, sp domain.OrgSpend, now time.Time) ([]gen.SpendBudget, error) {
	models, err := s.modelConnections(ctx)
	if err != nil {
		return nil, err
	}

	attention, err := s.attention(ctx)
	if err != nil {
		return nil, err
	}

	today := make(map[domain.FactoryID]float64, len(sp.Factories))
	for _, f := range sp.Factories {
		today[f.FactoryID] = f.TodayUSD
	}

	recs, err := s.records(ctx)
	if err != nil {
		return nil, err
	}

	out := []gen.SpendBudget{}

	for _, f := range s.visibleFactories(ctx) {
		id := domain.FactoryID(f.Metadata.ID)
		if recs[id].Archived() {
			continue
		}

		b, err := s.spendBudget(ctx, f, attention[id].Andon, now)
		if err != nil {
			return nil, err
		}

		b.TodayUsd, b.ModelConnections = today[id], append([]gen.ConnectionHit{}, models[id]...)
		out = append(out, b)
	}

	slices.SortFunc(out, func(a, b gen.SpendBudget) int { return cmp.Compare(a.Factory.Name, b.Factory.Name) })

	return out, nil
}

// spendBudget is factory f's caps, its spend in the engine's weekly window and its state: stopped
// while andon is open, over_budget while the weekly cap is used up, else running.
func (s *server) spendBudget(ctx context.Context, f *config.Factory, andon *domain.AndonPull, now time.Time) (gen.SpendBudget, error) {
	l := f.Line()
	out := gen.SpendBudget{
		Factory: gen.FactoryHit{Id: f.Metadata.ID, Name: f.Metadata.Name, Title: cmp.Or(f.Metadata.Title, f.Metadata.Name)},
		State:   gen.SpendBudgetStateRunning,
	}

	if b := f.Spec.Runtime.Budget; b != nil {
		if b.DailyUSD > 0 {
			out.DailyUsd = &b.DailyUSD
		}

		if b.WeeklyUSD > 0 {
			out.WeeklyUsd = &b.WeeklyUSD
		}
	}

	spent, err := s.items.SpendSince(ctx, l.FactoryID, domain.BudgetSince(now))
	if err != nil {
		return gen.SpendBudget{}, fmt.Errorf("read factory %s's weekly spend: %w", l.FactoryID, err)
	}

	out.WeekUsd = spent

	if andon != nil {
		out.State, out.Reason = gen.SpendBudgetStateStopped, &andon.Reason

		return out, nil
	}

	if b := l.Runtime.WeeklyBudget(spent, now); b.Exhausted {
		reason := fmt.Sprintf("spent $%.2f of the $%.2f weekly budget in the last 7 days", b.SpentUSD, b.WeeklyUSD)
		out.State, out.Reason = gen.SpendBudgetStateOverBudget, &reason
	}

	return out, nil
}

// modelConnections are, by factory goald runs that is not archived, the organization's
// connections its current version gets its model from (runtime.modelConnection), with their
// kinds. A name the organization has no connection for is left out.
func (s *server) modelConnections(ctx context.Context) (map[domain.FactoryID][]gen.ConnectionHit, error) {
	users, err := s.usersBy(ctx, func(f *config.Factory) []string {
		if m := f.Spec.Runtime.ModelConnection; m != "" {
			return []string{m}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	conns, err := s.connections.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the organization's connections: %w", err)
	}

	out := map[domain.FactoryID][]gen.ConnectionHit{}

	for _, c := range conns {
		for _, f := range users[c.Name] {
			out[f.id] = append(out[f.id], gen.ConnectionHit{Name: c.Name, Kind: c.Kind})
		}
	}

	return out, nil
}
