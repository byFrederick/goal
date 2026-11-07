package storetest

import (
	"math"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// A used-up weekly budget keeps the since it was first marked with, whoever marks it again,
// until it is cleared; a mark after the clear takes its own time. Each factory has its own mark,
// another organization sees none, and every call needs an organization.
func testBudgetExhaustion(f *fixture) {
	t := f.t
	other := f.newFactory(t)

	if err := f.Store.ClearBudgetExhausted(f.ctx(t), f.factory); err != nil {
		t.Fatalf("ClearBudgetExhausted of a factory not marked: %v", err)
	}

	f.budgetSince(t, f.factory, time.Time{})

	for _, at := range []time.Time{t1, t1.Add(time.Minute)} {
		if err := f.Store.MarkBudgetExhausted(f.ctx(t), f.factory, at); err != nil {
			t.Fatalf("MarkBudgetExhausted(%s): %v", at, err)
		}
	}

	f.budgetSince(t, f.factory, t1)
	f.budgetSince(t, other, time.Time{})

	elsewhere := f.otherOrg(t)
	elsewhere.budgetSince(t, f.factory, time.Time{})

	if err := elsewhere.Store.ClearBudgetExhausted(elsewhere.ctx(t), f.factory); err != nil {
		t.Fatalf("ClearBudgetExhausted from another organization: %v", err)
	}

	f.budgetSince(t, f.factory, t1)

	if err := f.Store.ClearBudgetExhausted(f.ctx(t), f.factory); err != nil {
		t.Fatalf("ClearBudgetExhausted: %v", err)
	}

	f.budgetSince(t, f.factory, time.Time{})

	later := t1.Add(time.Hour)
	if err := f.Store.MarkBudgetExhausted(f.ctx(t), f.factory, later); err != nil {
		t.Fatalf("MarkBudgetExhausted after the clear: %v", err)
	}

	f.budgetSince(t, f.factory, later)

	bare := t.Context()
	expect(t, "MarkBudgetExhausted without an organization", f.Store.MarkBudgetExhausted(bare, f.factory, t1), domain.ErrNoOrg)
	expect(t, "ClearBudgetExhausted without an organization", f.Store.ClearBudgetExhausted(bare, f.factory), domain.ErrNoOrg)

	_, _, err := f.Store.BudgetExhaustedSince(bare, f.factory)
	expect(t, "BudgetExhaustedSince without an organization", err, domain.ErrNoOrg)
}

// budgetSince fails the test unless the factory's budget is marked used up since want, or not
// marked when want is zero.
func (f *fixture) budgetSince(t *testing.T, factory domain.FactoryID, want time.Time) {
	t.Helper()

	since, ok, err := f.Store.BudgetExhaustedSince(f.ctx(t), factory)
	if err != nil || ok != !want.IsZero() || !since.Equal(want) {
		t.Errorf("BudgetExhaustedSince(%s) = %v, %v, %v; want %v", factory, since, ok, err, want)
	}
}

// RunCostsSince returns, oldest first, the runs whose costs SpendSince sums for the same
// window: from since, the bound included, never Jonah's runs or tries, nor another factory's.
func testRunCostsSince(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})

	f.spent(t, it, t1.Add(48*time.Hour), 1.5)
	f.spent(t, it, t1.Add(-time.Second), 7)
	f.spent(t, it, t1, 0.25)
	f.kindRun(t, it, domain.RunKindJonah, t1.Add(time.Hour), 5)
	f.kindRun(t, it, domain.RunKindTry, t1.Add(time.Hour), 3)
	f.spent(t, f.create(t, domain.NewItem{FactoryID: f.newFactory(t)}), t1, 9)

	got, err := f.Store.RunCostsSince(f.ctx(t), f.factory, t1)
	if err != nil {
		t.Fatalf("RunCostsSince: %v", err)
	}

	want := []domain.RunCost{{StartedAt: t1, CostUSD: 0.25}, {StartedAt: t1.Add(48 * time.Hour), CostUSD: 1.5}}
	if len(got) != len(want) {
		t.Fatalf("RunCostsSince = %v; want %v", got, want)
	}

	var sum float64

	for i := range want {
		if !got[i].StartedAt.Equal(want[i].StartedAt) || math.Abs(got[i].CostUSD-want[i].CostUSD) > 1e-9 {
			t.Errorf("RunCostsSince[%d] = %+v; want %+v", i, got[i], want[i])
		}

		sum += got[i].CostUSD
	}

	if spend := f.spendSince(t, f.factory, t1); math.Abs(sum-spend) > 1e-9 {
		t.Errorf("RunCostsSince sums to %v; SpendSince of the same window is %v", sum, spend)
	}

	_, err = f.Store.RunCostsSince(t.Context(), f.factory, t1)
	expect(t, "RunCostsSince without an organization", err, domain.ErrNoOrg)
}
