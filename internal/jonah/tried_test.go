package jonah_test

import (
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/jonah"
)

func TestTries_TriedState(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	try := func(item domain.ItemID, attempt int, st domain.PastTryState) domain.PastTry {
		p := domain.PastTry{ItemID: item, Attempt: attempt, Station: "spec", State: st, CreatedAt: at, CostUSD: 0.25}
		if st == domain.PastTryDone {
			p.Verdict, p.Reason, p.FinishedAt = domain.PastTryFixed, "it fixed it", at.Add(time.Duration(attempt)*time.Minute)
		}

		return p
	}
	left := domain.JonahBudget{BudgetUSD: 2, SpentUSD: 1}
	used := domain.JonahBudget{BudgetUSD: 2, SpentUSD: 2}

	cases := []struct {
		name   string
		tries  []domain.PastTry
		budget domain.JonahBudget
		want   jonah.TriedState
	}{
		{"queued with budget left is trying", []domain.PastTry{try("a", 1, domain.PastTryQueued)}, left, jonah.TriedTrying},
		{"running is trying", []domain.PastTry{try("a", 1, domain.PastTryDone), try("b", 1, domain.PastTryRunning)}, used, jonah.TriedTrying},
		{"queued with the budget used", []domain.PastTry{try("a", 1, domain.PastTryDone), try("b", 1, domain.PastTryQueued)}, used, jonah.TriedBudgetUsed},
		{"queued beside running with the budget used", []domain.PastTry{try("a", 1, domain.PastTryRunning), try("b", 1, domain.PastTryQueued)}, used,
			jonah.TriedBudgetUsed},
		{"all done", []domain.PastTry{try("a", 1, domain.PastTryDone), try("b", 1, domain.PastTryDone)}, used, jonah.TriedDone},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, ok := jonah.TriedOf(c.tries, c.budget)
			if !ok || got.State != c.want {
				t.Fatalf("TriedOf: %v, %v; want state %s", got.State, ok, c.want)
			}

			if done := c.want == jonah.TriedDone; (got.Duration > 0) != done {
				t.Errorf("duration %s with state %s", got.Duration, got.State)
			}
		})
	}
}

func TestTries_TriedGroupsByItem(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	done := func(item domain.ItemID, attempt int, cost float64, end time.Duration) domain.PastTry {
		return domain.PastTry{
			ItemID: item, Attempt: attempt, Station: "spec", State: domain.PastTryDone, Verdict: domain.PastTryError, Error: "lost box",
			CostUSD: cost, CreatedAt: at, FinishedAt: at.Add(end),
		}
	}

	// Oldest first as the store reads them, with item b queued before item a.
	tries := []domain.PastTry{done("b", 2, 0.5, time.Minute), done("b", 1, 0.25, 3*time.Minute), done("a", 1, 1, 2*time.Minute)}

	got, ok := jonah.TriedOf(tries, domain.JonahBudget{})
	if !ok {
		t.Fatal("TriedOf: no tried for three tries")
	}

	if len(got.Items) != 2 || got.Items[0].ItemID != "b" || got.Items[1].ItemID != "a" {
		t.Fatalf("items %+v, want b then a", got.Items)
	}

	if b := got.Items[0].Tries; len(b) != 2 || b[0].Attempt != 1 || b[1].Attempt != 2 {
		t.Errorf("item b's tries %+v, want attempts 1 then 2", b)
	}

	if got.CostUSD != 1.75 || got.Duration != 3*time.Minute || got.State != jonah.TriedDone {
		t.Errorf("cost %v, duration %s, state %s; want 1.75, 3m0s, done", got.CostUSD, got.Duration, got.State)
	}

	if _, ok := jonah.TriedOf(nil, domain.JonahBudget{}); ok {
		t.Error("TriedOf of no tries: want none")
	}
}
