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
