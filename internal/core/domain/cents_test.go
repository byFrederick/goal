package domain

import "testing"

// Amounts that don't come out even in binary round to the exact cent a person reads.
func TestRoundCents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		usd  float64
		want float64
	}{
		{name: "even", usd: 25, want: 25},
		{name: "0.1+0.2", usd: 0.1 + 0.2, want: 0.3},
		{name: "a share of the spend", usd: 50*0.05 - 0.000000000000004, want: 2.5},
		{name: "a fraction of a cent", usd: 0.123456, want: 0.12},
		{name: "2.675, stored as 2.67499…", usd: 2.675, want: 2.68},
		{name: "negative", usd: -0.30000000000000004, want: -0.3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RoundCents(tt.usd); got != tt.want {
				t.Errorf("RoundCents(%v) = %v, want %v", tt.usd, got, tt.want)
			}
		})
	}
}

// What Jonah's budget has left is in whole cents, and never below 0.
func TestJonahBudget_LeftUSD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		b    JonahBudget
		want float64
	}{
		{name: "5% of $500 less $0.000000000000004", b: JonahBudget{BudgetUSD: 0.05 * 500, SpentUSD: 0.000000000000004}, want: 25},
		{name: "0.3 less 0.1", b: JonahBudget{BudgetUSD: 0.1 + 0.2, SpentUSD: 0.1}, want: 0.2},
		{name: "a fraction of a cent", b: JonahBudget{BudgetUSD: 1, SpentUSD: 0.3333}, want: 0.67},
		{name: "overspent", b: JonahBudget{BudgetUSD: 1, SpentUSD: 1.25}, want: 0},
		{name: "nothing", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.b.LeftUSD(); got != tt.want {
				t.Errorf("%+v.LeftUSD() = %v, want %v", tt.b, got, tt.want)
			}
		})
	}
}
