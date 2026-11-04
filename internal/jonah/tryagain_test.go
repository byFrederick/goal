package jonah_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/jonah"
)

// againPolicy is Jonah's policy with share: at 10, two past items that cost $100 each leave $2000
// of Jonah's budget.
func againPolicy(share float64) domain.JonahPolicy {
	p := domain.DefaultJonahPolicy()
	p.Share = share

	return p
}

// actor is org's context as the local user.
func (w *tryWorld) actor(org domain.OrgID) context.Context {
	return domain.WithActor(w.ctx(org), domain.LocalUser)
}

// tryAgain is Try again over w's store and definitions.
func (w *tryWorld) tryAgain(a *audits) *jonah.TryAgain {
	return jonah.NewTryAgain(jonah.TryAgainDeps{Store: w.store, Definitions: w.defs, Audit: a, Clock: w.clock})
}

// triedTwice is w's one org with a proposal of agents/writer.md that cites two past items, each
// tried once by the sweep.
func (w *tryWorld) triedTwice() (domain.OrgID, domain.Proposal, domain.Item, domain.Item) {
	w.t.Helper()

	org := w.orgs[0]
	a, _ := w.pastItem(org, rt0.Add(-2*time.Hour), true)
	b, _ := w.pastItem(org, rt0.Add(-time.Hour), true)
	p := w.propose(org, "agents/writer.md", a, b)
	w.sweep()

	if got := w.tries(org, p); len(got) != 2 {
		w.t.Fatalf("the sweep queued %d tries, want 2", len(got))
	}

	return org, p, a, b
}

func TestTryAgain_EstimateQueuesNothing(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org, p, a, b := w.triedTwice()
	a2 := &audits{}

	est, err := w.tryAgain(a2).Try(w.actor(org), w.factories[org], p.Number, againPolicy(10), false)
	if err != nil {
		t.Fatalf("Try estimate: %v", err)
	}

	if est.Number != p.Number || est.Tries != 2 || est.EstimateUSD != 200 || est.BudgetLeftUSD != 2000 || len(est.Items) != 2 {
		t.Fatalf("estimate = %+v; want 2 tries at $200 with $2000 left", est)
	}

	for i, it := range []domain.Item{b, a} { // the sweep tries the newest first
		if g := est.Items[i]; g.ItemID != it.ID || g.Station != stBuild || g.Tries != 1 || g.RunCostUSD != 100 {
			t.Errorf("estimate item %d = %+v; want one try of %s at %s priced $100", i, g, it.ID, stBuild)
		}
	}

	if got := w.tries(org, p); len(got) != 2 || len(a2.actions()) != 0 {
		t.Errorf("an estimate left %d tries and audited %v; want 2 and nothing", len(got), a2.actions())
	}
}

func TestTryAgain_ConfirmAddsAttempts(t *testing.T) {
	t.Parallel()

	t.Run("review proposal", func(t *testing.T) {
		t.Parallel()

		w := newTryWorld(t, 1)
		org, p, a, b := w.triedTwice()
		au := &audits{}
		again := w.tryAgain(au)

		for range 2 {
			if _, err := again.Try(w.actor(org), w.factories[org], p.Number, againPolicy(10), true); err != nil {
				t.Fatalf("Try: %v", err)
			}
		}

		expectAttempts(t, w.tries(org, p), map[domain.ItemID][]int{a.ID: {1, 2, 3}, b.ID: {1, 2, 3}})

		if got := au.actions(); len(got) != 2 || got[0] != domain.AuditProposalTried {
			t.Errorf("audit = %v, want two %s", got, domain.AuditProposalTried)
		}
	})

	t.Run("model proposal", func(t *testing.T) {
		t.Parallel()

		w := newModelWorld(t)
		a := w.doneItem(rt0.Add(-2*time.Hour), 1, 0)
		b := w.doneItem(rt0.Add(-time.Hour), 2, 0)

		_, p, err := w.propose(true)
		if err != nil {
			t.Fatalf("Propose: %v", err)
		}

		again := jonah.NewTryAgain(jonah.TryAgainDeps{
			Store: w.store, Definitions: &tryDefs{lines: map[domain.OrgID]domain.Line{decideOrg: w.defs.line}}, Audit: w.audit, Clock: w.clock,
		})

		est, err := again.Try(w.ctx(), factory, p.Number, againPolicy(10), true)
		if err != nil {
			t.Fatalf("Try: %v", err)
		}

		if est.Tries != 2*domain.ModelTriesPerItem || est.EstimateUSD != 9 || est.Items[0].Tries != domain.ModelTriesPerItem {
			t.Errorf("estimate = %+v; want three tries per item at $9", est)
		}

		tries, err := w.store.ProposalPastTries(w.ctx(), p.ID)
		if err != nil {
			t.Fatal(err)
		}

		expectAttempts(t, tries, map[domain.ItemID][]int{a.ID: {1, 2, 3, 4, 5, 6}, b.ID: {1, 2, 3, 4, 5, 6}})

		for _, tr := range tries {
			if tr.Model != modelNew || tr.Station != stBuild {
				t.Errorf("try %+v, want %s at %s", tr, modelNew, stBuild)
			}
		}
	})
}

// expectAttempts fails t unless tried, as TriedOf groups tries, has each item's attempts in want,
// in order.
func expectAttempts(t *testing.T, tries []domain.PastTry, want map[domain.ItemID][]int) {
	t.Helper()

	tried, ok := jonah.TriedOf(tries, domain.JonahBudget{BudgetUSD: 1})
	if !ok || len(tried.Items) != len(want) {
		t.Fatalf("tried = %+v; want %d items", tried, len(want))
	}

	for _, it := range tried.Items {
		var got []int
		for _, tr := range it.Tries {
			got = append(got, tr.Attempt)
		}

		if !slices.Equal(got, want[it.ItemID]) {
			t.Errorf("attempts of %s = %v, want %v", it.ItemID, got, want[it.ItemID])
		}
	}
}

func TestTryAgain_Refusals(t *testing.T) {
	t.Parallel()

	t.Run("budget", func(t *testing.T) {
		t.Parallel()

		w := newTryWorld(t, 1)
		org, p, _, _ := w.triedTwice()

		est, err := w.tryAgain(&audits{}).Try(w.actor(org), w.factories[org], p.Number, againPolicy(0.5), true)
		if !errors.Is(err, domain.ErrLearningBudgetExhausted) || est.Tries != 2 || est.EstimateUSD != 200 || est.BudgetLeftUSD != 100 {
			t.Errorf("Try over the budget = %+v, %v; want ErrLearningBudgetExhausted with the estimate", est, err)
		}

		if got := w.tries(org, p); len(got) != 2 {
			t.Errorf("a refused Try left %d tries, want 2", len(got))
		}
	})

	t.Run("decided", func(t *testing.T) {
		t.Parallel()

		w := newTryWorld(t, 1)
		org, p, _, _ := w.triedTwice()

		if _, err := w.store.DecideProposal(w.ctx(org), w.factories[org], p.Number, domain.ProposalDecision{
			State: domain.ProposalRejected, RejectReason: "no", By: domain.LocalUser, At: rt0,
		}); err != nil {
			t.Fatal(err)
		}

		_, err := w.tryAgain(&audits{}).Try(w.actor(org), w.factories[org], p.Number, againPolicy(10), true)
		expectErr(t, "Try of a rejected proposal", err, ports.ErrProposalDecided)
	})

	for _, c := range []struct {
		name    string
		reached []string
	}{{"memory only", []string{}}, {"human station only", []string{stSpec}}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			w := newTryWorld(t, 1)
			org := w.orgs[0]
			it, _ := w.pastItem(org, rt0.Add(-time.Hour), true)
			w.defs.reached["memory.yaml"] = c.reached
			p := w.propose(org, "memory.yaml", it)

			_, err := w.tryAgain(&audits{}).Try(w.actor(org), w.factories[org], p.Number, againPolicy(10), false)
			expectErr(t, "Try of a proposal that reaches no triable station", err, domain.ErrProposalNotTriable)
		})
	}

	t.Run("not tried", func(t *testing.T) {
		t.Parallel()

		w := newTryWorld(t, 1)
		org := w.orgs[0]
		it, _ := w.pastItem(org, rt0.Add(-time.Hour), true)
		p := w.propose(org, "agents/writer.md", it)

		_, err := w.tryAgain(&audits{}).Try(w.actor(org), w.factories[org], p.Number, againPolicy(10), false)
		expectErr(t, "Try of a proposal the sweep has not tried", err, domain.ErrProposalNotTried)
	})

	t.Run("stale", func(t *testing.T) {
		t.Parallel()

		w := newTryWorld(t, 1)
		org, p, _, _ := w.triedTwice()
		w.defs.stale = true

		_, err := w.tryAgain(&audits{}).Try(w.actor(org), w.factories[org], p.Number, againPolicy(10), false)
		expectErr(t, "Try of a proposal whose edits no longer apply", err, domain.ErrProposalStale)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		w := newTryWorld(t, 1)
		org := w.orgs[0]

		_, err := w.tryAgain(&audits{}).Try(w.actor(org), w.factories[org], 99, againPolicy(10), false)
		expectErr(t, "Try of no such proposal", err, ports.ErrNotFound)
	})
}

func expectErr(t *testing.T, what string, err, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Errorf("%s = %v, want %v", what, err, want)
	}
}
