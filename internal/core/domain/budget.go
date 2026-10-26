package domain

import "time"

// BudgetPolicy is what the engine does once a factory's weekly budget is used up.
type BudgetPolicy string

// Budget policies, as written in factory.yaml (runtime.budget.onExhausted).
const (
	// BudgetDrain lets the runs in progress finish and starts no new agent work (the default).
	BudgetDrain BudgetPolicy = "drain"
	// BudgetStop also cancels the runs in progress; their items return to their station's queue.
	BudgetStop BudgetPolicy = "stop"
)

// BudgetWindow is the window a weekly budget covers: the last 7×24 hours, rolling. It needs no
// time zone and has no cliff at midnight Monday. runtime.budget.dailyUsd is display-only.
const BudgetWindow = 7 * 24 * time.Hour

// BudgetSince is where the weekly budget's window starts when it ends at now.
func BudgetSince(now time.Time) time.Time { return now.Add(-BudgetWindow) }

// BudgetStatus is a factory's weekly budget at one moment: what the engine enforces and what
// the screens show ("$18.40 of $60", "Budget used up").
type BudgetStatus struct {
	// WeeklyUSD is the cap; 0 means the factory has no weekly budget.
	WeeklyUSD float64
	// SpentUSD is the cost of the factory's agent runs started at or after Since.
	SpentUSD float64
	// Since is the start of the window, BudgetWindow before the moment measured.
	Since time.Time
	// Policy is what happens while the budget is used up.
	Policy BudgetPolicy
	// Exhausted means the spend is at or over the cap: no new agent work starts.
	Exhausted bool
}

// HasWeeklyBudget reports whether the factory's agent spend is capped per week.
func (r Runtime) HasWeeklyBudget() bool { return r.WeeklyUSD > 0 }

// WeeklyBudget measures spent, the cost of the runs started in the window that ends at now,
// against the weekly budget. Without a budget nothing is ever exhausted.
func (r Runtime) WeeklyBudget(spent float64, now time.Time) BudgetStatus {
	policy := r.OnExhausted
	if policy == "" {
		policy = BudgetDrain
	}

	return BudgetStatus{
		WeeklyUSD: r.WeeklyUSD, SpentUSD: spent, Since: BudgetSince(now), Policy: policy,
		Exhausted: r.HasWeeklyBudget() && spent >= r.WeeklyUSD,
	}
}

// BudgetFor is the weekly budget's share of a window of length d (WeeklyUSD × d / 7 days), as
// the factories overview shows it for 7, 14 or 30 days; 0 without a budget.
func (r Runtime) BudgetFor(d time.Duration) float64 {
	if !r.HasWeeklyBudget() {
		return 0
	}

	return r.WeeklyUSD * float64(d) / float64(BudgetWindow)
}
