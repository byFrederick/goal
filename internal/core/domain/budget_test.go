package domain

import (
	"testing"
	"time"
)

func TestRuntime_WeeklyBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		rt            Runtime
		spent         float64
		wantExhausted bool
		wantPolicy    BudgetPolicy
	}{
		{name: "under the cap", rt: Runtime{WeeklyUSD: 60}, spent: 59.99, wantPolicy: BudgetDrain},
		{name: "at the cap", rt: Runtime{WeeklyUSD: 60, OnExhausted: BudgetStop}, spent: 60, wantExhausted: true, wantPolicy: BudgetStop},
		{name: "over the cap", rt: Runtime{WeeklyUSD: 60}, spent: 75, wantExhausted: true, wantPolicy: BudgetDrain},
		{name: "no budget is never exhausted", rt: Runtime{}, spent: 1e6, wantPolicy: BudgetDrain},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := tc.rt.WeeklyBudget(tc.spent, now)
			if b.Exhausted != tc.wantExhausted || b.Policy != tc.wantPolicy || b.SpentUSD != tc.spent ||
				b.WeeklyUSD != tc.rt.WeeklyUSD || !b.Since.Equal(now.Add(-7*24*time.Hour)) {
				t.Errorf("WeeklyBudget(%v) = %+v; want exhausted %v, policy %s, since 7×24h before now",
					tc.spent, b, tc.wantExhausted, tc.wantPolicy)
			}
		})
	}
}

func TestRuntime_BudgetForAWindowIsItsShareOfTheWeek(t *testing.T) {
	t.Parallel()

	day := 24 * time.Hour
	rt := Runtime{WeeklyUSD: 70}

	for d, want := range map[time.Duration]float64{7 * day: 70, 14 * day: 140, 30 * day: 300, day: 10} {
		if got := rt.BudgetFor(d); got != want {
			t.Errorf("BudgetFor(%v) = %v; want %v", d, got, want)
		}
	}

	if got := (Runtime{}).BudgetFor(7 * day); got != 0 {
		t.Errorf("BudgetFor without a budget = %v; want 0", got)
	}
}

func TestBudgetResumesAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	ago := func(d time.Duration, usd float64) RunCost { return RunCost{StartedAt: now.Add(-d), CostUSD: usd} }

	tests := []struct {
		name   string
		runs   []RunCost
		weekly float64
		want   time.Time // zero: no resumes_at
	}{
		{
			name: "the oldest run's departure is enough", runs: []RunCost{ago(time.Hour, 4), ago(6*day, 4), ago(3*day, 4)}, weekly: 10,
			want: now.Add(day),
		},
		{
			name: "two departures are needed under a low cap", runs: []RunCost{ago(6*day, 6), ago(3*day, 3), ago(time.Hour, 3)}, weekly: 4,
			want: now.Add(4 * day),
		},
		{
			name: "a raised cap resumes earlier", runs: []RunCost{ago(6*day, 6), ago(3*day, 3), ago(time.Hour, 3)}, weekly: 7,
			want: now.Add(day),
		},
		{
			name: "only the newest run's departure gets below the cap", runs: []RunCost{ago(6*day, 1), ago(time.Hour, 10)}, weekly: 10,
			want: now.Add(BudgetWindow - time.Hour),
		},
		{
			name: "runs before the window count for nothing", runs: []RunCost{ago(8*day, 100), ago(2*day, 10)}, weekly: 10,
			want: now.Add(5 * day),
		},
		{
			name: "a run that started exactly a window ago leaves at now", runs: []RunCost{ago(BudgetWindow, 10)}, weekly: 10,
			want: now,
		},
		{name: "a budget not used up has none", runs: []RunCost{ago(day, 9.99)}, weekly: 10},
		{name: "no budget has none", runs: []RunCost{ago(day, 1000)}, weekly: 0},
		{name: "no runs have none", weekly: 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := BudgetResumesAt(tc.runs, tc.weekly, now)
			if ok != !tc.want.IsZero() || !got.Equal(tc.want) {
				t.Errorf("BudgetResumesAt(%v, %v) = %v, %v; want %v", tc.runs, tc.weekly, got, ok, tc.want)
			}
		})
	}
}
