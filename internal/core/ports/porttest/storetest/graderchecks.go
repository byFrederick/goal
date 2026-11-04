package storetest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// graderChecksTests are the grader's checks (ADR 0039 decision 10), registered by Run under
// GraderChecks_.
var graderChecksTests = []struct {
	name string
	test func(f *fixture)
}{
	{"GraderChecks_RecordedOncePerItemAndResult", testGraderChecksOnce},
	{"GraderChecks_ListedOldestFirst", testGraderChecksOrder},
	{"GraderChecks_HitRatesCountUnknownAsAMiss", testGraderChecksHitRates},
	{"GraderChecks_GoWithTheirProposal", testGraderChecksGoWithProposal},
	{"GraderChecks_OtherOrgInvisible", testGraderChecksTenancy},
}

// graderCheck is a check of it's result on p read as answer, at minutes after t1.
func graderCheck(p domain.Proposal, it domain.Item, result domain.GraderCheckResult, answer domain.PastTryVerdict, at int) domain.NewGraderCheck {
	return domain.NewGraderCheck{
		ProposalID: p.ID, ItemID: it.ID, Result: result, Answer: answer, Reason: "the grader read the diff",
		CostUSD: 0.25, CheckedAt: t1.Add(time.Duration(at) * time.Minute),
	}
}

// check records n and fails the test on an error.
func (f *fixture) check(t *testing.T, n domain.NewGraderCheck) domain.GraderCheck {
	t.Helper()

	c, err := f.Tries.RecordGraderCheck(f.ctx(t), n)
	if err != nil {
		t.Fatalf("RecordGraderCheck: %v", err)
	}

	return c
}

// A proposal has one check per item and result: a second is a conflict, and an invalid, unknown
// or foreign proposal or item records nothing.
func testGraderChecksOnce(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	it := f.create(t, domain.NewItem{})
	run := f.pastRun(t, it, stWork, t0, true, "")

	n := graderCheck(p, it, domain.GraderCheckOriginal, domain.PastTryNotFixed, 1)
	n.RunID = run

	c := f.check(t, n)
	if c.ID == "" || c.FactoryID != f.factory || c.ProposalID != p.ID || c.ItemID != it.ID || c.Result != domain.GraderCheckOriginal ||
		c.Answer != domain.PastTryNotFixed || c.Reason != n.Reason || c.RunID != run || c.CostUSD != 0.25 || !c.CheckedAt.Equal(n.CheckedAt) {
		t.Errorf("RecordGraderCheck = %+v; want the check of %+v in factory %s", c, n, f.factory)
	}

	again := graderCheck(p, it, domain.GraderCheckOriginal, domain.PastTryFixed, 2)
	_, err := f.Tries.RecordGraderCheck(f.ctx(t), again)
	expect(t, "RecordGraderCheck of the same item and result", err, ports.ErrConflict)

	f.check(t, graderCheck(p, it, domain.GraderCheckFinal, domain.PastTryFixed, 2))

	bad := graderCheck(p, it, "latest", domain.PastTryFixed, 3)
	_, err = f.Tries.RecordGraderCheck(f.ctx(t), bad)
	expect(t, "RecordGraderCheck of an invalid result", err, domain.ErrInvalid)

	missing := graderCheck(p, domain.Item{ID: missingItem}, domain.GraderCheckOriginal, domain.PastTryNotFixed, 3)
	_, err = f.Tries.RecordGraderCheck(f.ctx(t), missing)
	expect(t, "RecordGraderCheck of an unknown item", err, ports.ErrNotFound)

	elsewhere := f.create(t, domain.NewItem{FactoryID: f.newFactory(t)})
	_, err = f.Tries.RecordGraderCheck(f.ctx(t), graderCheck(p, elsewhere, domain.GraderCheckOriginal, domain.PastTryNotFixed, 3))
	expect(t, "RecordGraderCheck of another factory's item", err, ports.ErrNotFound)

	noProposal := graderCheck(domain.Proposal{ID: domain.ProposalID(missingItem)}, it, domain.GraderCheckOriginal, domain.PastTryNotFixed, 3)
	_, err = f.Tries.RecordGraderCheck(f.ctx(t), noProposal)
	expect(t, "RecordGraderCheck of an unknown proposal", err, ports.ErrNotFound)

	got, err := f.Tries.ProposalGraderChecks(f.ctx(t), p.ID)
	if err != nil || len(got) != 2 || got[0].ID != c.ID || got[0].RunID != run {
		t.Errorf("ProposalGraderChecks = %+v, %v; want the original then the final check", got, err)
	}
}

// A proposal's checks come oldest first, whatever order their items and results were recorded in,
// and another proposal's are not among them.
func testGraderChecksOrder(f *fixture) {
	t := f.t
	_, ps := f.finishReview(t, f.factory, t1, domain.JonahReviewResult{Proposals: []domain.NewProposal{
		proposal("tighten", "agents/worker.md"), proposal("loosen", "agents/next.md"),
	}})
	p, other := ps[0], ps[1]
	a, b := f.create(t, domain.NewItem{}), f.create(t, domain.NewItem{})

	late := f.check(t, graderCheck(p, a, domain.GraderCheckFinal, domain.PastTryFixed, 3))
	early := f.check(t, graderCheck(p, b, domain.GraderCheckOriginal, domain.PastTryNotFixed, 1))
	mid := f.check(t, graderCheck(p, a, domain.GraderCheckOriginal, domain.PastTryNotFixed, 2))
	f.check(t, graderCheck(other, a, domain.GraderCheckOriginal, domain.PastTryNotFixed, 0))

	got, err := f.Tries.ProposalGraderChecks(f.ctx(t), p.ID)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.GraderCheckID{early.ID, mid.ID, late.ID}
	if ids := checkIDs(got); !slices.Equal(ids, want) {
		t.Errorf("ProposalGraderChecks = %v; want oldest first %v", ids, want)
	}

	if got, err := f.Tries.ProposalGraderChecks(f.ctx(t), domain.ProposalID(missingItem)); err != nil || len(got) != 0 {
		t.Errorf("ProposalGraderChecks of an unknown proposal = %+v, %v; want none", got, err)
	}
}

// Of two items, one read right both ways and one misread on its final result, the original hit
// rate is 2 of 2 and the final 1 of 2: an unknown answer is a miss.
func testGraderChecksHitRates(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	a, b := f.create(t, domain.NewItem{}), f.create(t, domain.NewItem{})

	if r, err := f.Tries.GraderHitRates(f.ctx(t), p.ID); err != nil || r != (domain.GraderHitRates{}) {
		t.Errorf("GraderHitRates with no checks = %+v, %v; want zero", r, err)
	}

	f.check(t, graderCheck(p, a, domain.GraderCheckOriginal, domain.PastTryNotFixed, 1))
	f.check(t, graderCheck(p, a, domain.GraderCheckFinal, domain.PastTryFixed, 2))
	f.check(t, graderCheck(p, b, domain.GraderCheckOriginal, domain.PastTryNotFixed, 3))
	f.check(t, graderCheck(p, b, domain.GraderCheckFinal, domain.PastTryUnknown, 4))

	want := domain.GraderHitRates{OriginalHits: 2, OriginalTotal: 2, FinalHits: 1, FinalTotal: 2}
	if r, err := f.Tries.GraderHitRates(f.ctx(t), p.ID); err != nil || r != want {
		t.Errorf("GraderHitRates = %+v, %v; want %+v", r, err, want)
	}
}

// Deleting the factory removes its proposals' checks, as goal_delete_factory's cascades do.
func testGraderChecksGoWithProposal(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	it := f.create(t, domain.NewItem{})
	run := f.pastRun(t, it, stWork, t0, true, "")

	n := graderCheck(p, it, domain.GraderCheckOriginal, domain.PastTryNotFixed, 1)
	n.RunID = run
	f.check(t, n)

	f.DeleteFactory(t, f.org, f.factory)

	if got, err := f.Tries.ProposalGraderChecks(f.ctx(t), p.ID); err != nil || len(got) != 0 {
		t.Errorf("ProposalGraderChecks after the factory was deleted = %+v, %v; want none", got, err)
	}

	if r, err := f.Tries.GraderHitRates(f.ctx(t), p.ID); err != nil || r != (domain.GraderHitRates{}) {
		t.Errorf("GraderHitRates after the factory was deleted = %+v, %v; want zero", r, err)
	}
}

// Another organization neither sees a proposal's checks nor records one on it.
func testGraderChecksTenancy(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	it := f.create(t, domain.NewItem{})
	f.check(t, graderCheck(p, it, domain.GraderCheckOriginal, domain.PastTryNotFixed, 1))
	other := f.otherOrg(t)

	if got, err := f.Tries.ProposalGraderChecks(other.ctx(t), p.ID); err != nil || len(got) != 0 {
		t.Errorf("ProposalGraderChecks of another org = %+v, %v; want none", got, err)
	}

	if r, err := f.Tries.GraderHitRates(other.ctx(t), p.ID); err != nil || r != (domain.GraderHitRates{}) {
		t.Errorf("GraderHitRates of another org = %+v, %v; want zero", r, err)
	}

	_, err := f.Tries.RecordGraderCheck(other.ctx(t), graderCheck(p, it, domain.GraderCheckFinal, domain.PastTryFixed, 2))
	expect(t, "RecordGraderCheck on another org's proposal", err, ports.ErrNotFound)

	if _, err := f.Tries.ProposalGraderChecks(t.Context(), p.ID); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("ProposalGraderChecks without an org = %v, want ErrNoOrg", err)
	}

	if got, err := f.Tries.ProposalGraderChecks(f.ctx(t), p.ID); err != nil || len(got) != 1 {
		t.Errorf("ProposalGraderChecks after another org's calls = %+v, %v; want the one check", got, err)
	}
}

func checkIDs(cs []domain.GraderCheck) []domain.GraderCheckID {
	out := make([]domain.GraderCheckID, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}

	return out
}
