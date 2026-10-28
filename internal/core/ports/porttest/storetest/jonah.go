package storetest

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// jonahTests are the JonahStore cases (ADR 0034), registered by Run under Jonah_.
var jonahTests = []struct {
	name string
	test func(f *fixture)
}{
	{"Jonah_SecondReviewInFlightRefused", testJonahOneInFlight},
	// A Jonah run without an item, beside the Runs_ cases it is named with.
	{"Runs_JonahWithoutItem", testRunsJonahWithoutItem},
	{"Jonah_ExpiredLeaseReclaimed", testJonahLeaseReclaimed},
	{"Jonah_ProposalsNumberedPerFactory", testJonahNumbers},
	{"Jonah_PathOutsideAllowlistRefused", testJonahPathRefused},
	{"Jonah_DecideProposalOnlyOnce", testJonahDecide},
	{"Jonah_OtherOrgInvisible", testJonahTenancy},
	{"Jonah_SpendSinceSumsJonahRuns", testJonahSpend},
	{"Jonah_SentBackSinceCountsRejections", testJonahSentBack},
	{"Jonah_SentBackBetweenListsWindow", testJonahSentBackBetween},
	{"Jonah_StationFlowMeasuresStation", testJonahStationFlow},
}

// sha is a well-formed base_sha256.
var sha = strings.Repeat("ab", 32)

// queueReview queues a manual review of factory on a new version and fails the test on an error.
func (f *fixture) queueReview(t *testing.T, factory domain.FactoryID, at time.Time) domain.JonahReview {
	t.Helper()

	r, err := f.Jonah.QueueJonahReview(f.ctx(t), domain.NewJonahReview{
		FactoryID: factory, VersionID: f.NewVersion(t, f.org, factory), Trigger: domain.JonahManual,
		WindowStart: at.Add(-time.Hour), WindowEnd: at, By: ada, At: at,
	})
	if err != nil {
		t.Fatalf("QueueJonahReview: %v", err)
	}

	return r
}

func (f *fixture) claimReview(t *testing.T, now time.Time) domain.JonahReview {
	t.Helper()

	r, err := f.Jonah.ClaimJonahReview(f.ctx(t), now, ttl)
	if err != nil {
		t.Fatalf("ClaimJonahReview: %v", err)
	}

	return r
}

// finishReview queues, claims and finishes a review of factory with proposals and observations.
func (f *fixture) finishReview(t *testing.T, factory domain.FactoryID, at time.Time, res domain.JonahReviewResult) (domain.JonahReview, []domain.Proposal) {
	t.Helper()

	f.queueReview(t, factory, at)
	claimed := f.claimReview(t, at)

	res.State, res.At = domain.JonahDone, at.Add(time.Minute)

	r, ps, err := f.Jonah.FinishJonahReview(f.ctx(t), claimed, res)
	if err != nil {
		t.Fatalf("FinishJonahReview: %v", err)
	}

	return r, ps
}

func proposal(title string, paths ...string) domain.NewProposal {
	p := domain.NewProposal{Title: title, Summary: "why " + title}
	for _, path := range paths {
		p.Files = append(p.Files, domain.ProposalFile{Path: path, BaseSHA256: sha, Content: "new " + path})
	}

	return p
}

func numbers(ps []domain.Proposal) []int64 {
	out := make([]int64, len(ps))
	for i, p := range ps {
		out[i] = p.Number
	}

	return out
}

func (f *fixture) proposals(t *testing.T, q ports.ProposalQuery) []int64 {
	t.Helper()

	ps, err := f.Jonah.Proposals(f.ctx(t), q)
	if err != nil {
		t.Fatalf("Proposals: %v", err)
	}

	return numbers(ps)
}

// A factory has at most one review pending or running: a second gets the first with
// ErrConflict; another factory queues its own, and a finished review makes room.
func testJonahOneInFlight(f *fixture) {
	t := f.t
	first := f.queueReview(t, f.factory, t0)

	if first.State != domain.JonahPending || first.Trigger != domain.JonahManual || first.CreatedBy != ada.Ref() || first.Attempts != 0 {
		t.Fatalf("queued %+v: want pending manual by %s, no attempts", first, ada.Ref())
	}

	again := domain.NewJonahReview{
		FactoryID: f.factory, VersionID: first.VersionID, Trigger: domain.JonahThreshold, WindowStart: t0, WindowEnd: t1,
		By: domain.Actor{Kind: domain.ActorSystem, ID: "jonah"}, At: t1,
	}

	got, err := f.Jonah.QueueJonahReview(f.ctx(t), again)
	if !errors.Is(err, ports.ErrConflict) || got.ID != first.ID {
		t.Fatalf("second pending review: got %s, %v; want %s with ErrConflict", got.ID, err, first.ID)
	}

	other := f.queueReview(t, f.newFactory(t), t0)
	claimed := f.claimReview(t, t1)

	if _, err := f.Jonah.QueueJonahReview(f.ctx(t), again); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("review while one runs: %v, want ErrConflict", err)
	}

	if _, _, err := f.Jonah.FinishJonahReview(f.ctx(t), claimed, domain.JonahReviewResult{State: domain.JonahFailed, Error: "box lost", At: t1}); err != nil {
		t.Fatalf("FinishJonahReview: %v", err)
	}

	next, err := f.Jonah.QueueJonahReview(f.ctx(t), again)
	if err != nil || next.ID == first.ID || next.ID == other.ID {
		t.Fatalf("review after the first finished: %+v, %v; want a new one", next, err)
	}

	if _, err := f.Jonah.QueueJonahReview(f.ctx(t), domain.NewJonahReview{FactoryID: f.factory}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("review without trigger, version or actor: %v, want ErrInvalid", err)
	}
}

// A claimed review's lease, once ended, lets another claim take it with one more attempt; the
// first claim's finish is then refused.
func testJonahLeaseReclaimed(f *fixture) {
	t := f.t
	queued := f.queueReview(t, f.factory, t0)

	first := f.claimReview(t, t0)
	if first.ID != queued.ID || first.State != domain.JonahRunning || first.Attempts != 1 || !first.LeaseUntil.Equal(t0.Add(ttl)) {
		t.Fatalf("claim: %+v; want %s running until %s, attempt 1", first, queued.ID, t0.Add(ttl))
	}

	if _, err := f.Jonah.ClaimJonahReview(f.ctx(t), t0.Add(ttl/2), ttl); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("claim under a live lease: %v, want ErrNotFound", err)
	}

	second := f.claimReview(t, t0.Add(ttl))
	if second.ID != queued.ID || second.Attempts != 2 || !second.LeaseUntil.Equal(t0.Add(2*ttl)) {
		t.Fatalf("reclaim: %+v; want %s until %s, attempt 2", second, queued.ID, t0.Add(2*ttl))
	}

	f.finishUnderNewLease(t, first, second)
}

// finishUnderNewLease finishes a review claimed first and then second: the first claim's finish
// is refused, the second's ends the review, and nothing is claimable after.
func (f *fixture) finishUnderNewLease(t *testing.T, first, second domain.JonahReview) {
	t.Helper()

	done := domain.JonahReviewResult{State: domain.JonahDone, At: t0.Add(ttl + time.Second)}

	if _, _, err := f.Jonah.FinishJonahReview(f.ctx(t), first, done); !errors.Is(err, ports.ErrLeaseLost) {
		t.Fatalf("finish under the lost lease: %v, want ErrLeaseLost", err)
	}

	r, _, err := f.Jonah.FinishJonahReview(f.ctx(t), second, done)
	if err != nil || r.State != domain.JonahDone || !r.LeaseUntil.IsZero() || !r.FinishedAt.Equal(done.At) {
		t.Fatalf("finish: %+v, %v; want done at %s without a lease", r, err, done.At)
	}

	if _, err := f.Jonah.ClaimJonahReview(f.ctx(t), t1, ttl); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("claim after the finish: %v, want ErrNotFound", err)
	}
}

// Proposals are numbered per factory from 1 and keep what the review said; observations link to
// the proposal they became.
func testJonahNumbers(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})

	p1 := proposal("Ask for a screenshot", "agents/frontend-engineer.md", "rules/evidence.md")
	p1.Evidence = []domain.ProposalEvidence{{ItemID: it.ID, EventID: 7, Station: stNext, Quote: "no screenshot"}}
	p1.Estimate = domain.ProposalEstimate{Metric: domain.MetricSentBackRate, Station: stNext, Before: 0.4, After: 0.1, CostPerItemUSD: 0.02}

	review, ps := f.finishReview(t, f.factory, t0, domain.JonahReviewResult{
		Proposals: []domain.NewProposal{p1, proposal("Name the checks", "factory.yaml")},
		Observations: []domain.NewObservation{
			{Summary: "UI changes without a screenshot", Detail: "4 of 9", ItemIDs: []domain.ItemID{it.ID}, Count: 4, Proposal: 1},
			{Summary: "Missing tests", Count: 3},
		},
	})

	if got := numbers(ps); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("first review's numbers %v, want [1 2]", got)
	}

	f.expectProposal(t, ps[0], review, p1)
	f.expectObservations(t, review, ps[0].ID, it.ID)

	_, ps = f.finishReview(t, f.factory, t1, domain.JonahReviewResult{Proposals: []domain.NewProposal{proposal("Again", "skills/x/SKILL.md")}})
	if got := numbers(ps); !reflect.DeepEqual(got, []int64{3}) {
		t.Errorf("second review's numbers %v, want [3]", got)
	}

	_, ps = f.finishReview(t, f.newFactory(t), t1, domain.JonahReviewResult{Proposals: []domain.NewProposal{proposal("Other", "contracts/c.json")}})
	if got := numbers(ps); !reflect.DeepEqual(got, []int64{1}) {
		t.Errorf("another factory's numbers %v, want [1]", got)
	}

	f.expectPages(t)
}

// expectProposal checks that stored, of review, reads back as the review said: n.
func (f *fixture) expectProposal(t *testing.T, stored domain.Proposal, review domain.JonahReview, n domain.NewProposal) {
	t.Helper()

	got, err := f.Jonah.Proposal(f.ctx(t), f.factory, stored.Number)
	if err != nil {
		t.Fatalf("Proposal %d: %v", stored.Number, err)
	}

	if got.ID != stored.ID || got.ReviewID != review.ID || got.VersionID != review.VersionID || got.State != domain.ProposalProposed {
		t.Errorf("proposal %d = %+v, want %s of review %s on %s, proposed", stored.Number, got, stored.ID, review.ID, review.VersionID)
	}

	if got.Title != n.Title || got.Summary != n.Summary || !reflect.DeepEqual(got.Files, n.Files) ||
		!reflect.DeepEqual(got.Evidence, n.Evidence) || got.Estimate != n.Estimate || !got.CreatedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("proposal %d = %+v, want what the review said: %+v", stored.Number, got, n)
	}

	if !reflect.DeepEqual(got.Paths(), n.Paths()) {
		t.Errorf("paths %v, want %v", got.Paths(), n.Paths())
	}
}

// expectObservations checks review's two observations: one proposed as proposal, on item, and
// one watching.
func (f *fixture) expectObservations(t *testing.T, review domain.JonahReview, proposal domain.ProposalID, item domain.ItemID) {
	t.Helper()

	obs, err := f.Jonah.Observations(f.ctx(t), f.factory, 10)
	if err != nil || len(obs) != 2 {
		t.Fatalf("Observations: %+v, %v; want 2", obs, err)
	}

	byState := map[domain.ObservationState]domain.Observation{}
	for _, o := range obs {
		byState[o.State] = o
	}

	if o := byState[domain.ObservationProposed]; o.ProposalID != proposal || o.Count != 4 || o.ReviewID != review.ID ||
		!reflect.DeepEqual(o.ItemIDs, []domain.ItemID{item}) || o.Detail != "4 of 9" {
		t.Errorf("proposed observation %+v, want proposal %s, count 4, item %s", o, proposal, item)
	}

	if o := byState[domain.ObservationWatching]; o.ProposalID != "" || o.Count != 3 || len(o.ItemIDs) != 0 {
		t.Errorf("watching observation %+v, want no proposal, count 3, no items", o)
	}
}

// expectPages checks the keyset pages and the state filter over the factory's proposals 1 to 3,
// and that its newest review comes first.
func (f *fixture) expectPages(t *testing.T) {
	t.Helper()

	if got := f.proposals(t, ports.ProposalQuery{Factory: f.factory, Limit: 2}); !reflect.DeepEqual(got, []int64{3, 2}) {
		t.Errorf("first page %v, want [3 2]", got)
	}

	if got := f.proposals(t, ports.ProposalQuery{Factory: f.factory, Before: 2, Limit: 2}); !reflect.DeepEqual(got, []int64{1}) {
		t.Errorf("next page %v, want [1]", got)
	}

	f.decide(t, 2, domain.ProposalDecision{State: domain.ProposalRejected, RejectReason: "too broad", By: ada, At: t1})

	if got := f.proposals(t, ports.ProposalQuery{Factory: f.factory, State: domain.ProposalProposed, Limit: 10}); !reflect.DeepEqual(got, []int64{3, 1}) {
		t.Errorf("proposed %v, want [3 1]", got)
	}

	reviews, err := f.Jonah.JonahReviews(f.ctx(t), f.factory, 1)
	if err != nil || len(reviews) != 1 || !reviews[0].CreatedAt.Equal(t1) {
		t.Errorf("JonahReviews limit 1: %+v, %v; want the newest, of %s", reviews, err, t1)
	}
}

// A proposal editing a path outside the allowlist (evals/, a .. segment) is refused, and the
// review stores nothing and keeps running.
func testJonahPathRefused(f *fixture) {
	t := f.t

	for _, path := range []string{"evals/cases/a.yaml", "agents/../evals/x.md", "README.md", "factory.yaml.bak"} {
		f.queueReview(t, f.factory, t0)
		claimed := f.claimReview(t, t0)

		_, _, err := f.Jonah.FinishJonahReview(f.ctx(t), claimed, domain.JonahReviewResult{
			State: domain.JonahDone, At: t0.Add(time.Second),
			Proposals:    []domain.NewProposal{proposal("ok", "rules/a.md"), proposal("bad", path)},
			Observations: []domain.NewObservation{{Summary: "seen", Count: 1, Proposal: 2}},
		})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("proposal of %s: %v, want ErrInvalid", path, err)
		}

		if got := f.proposals(t, ports.ProposalQuery{Factory: f.factory, Limit: 10}); len(got) != 0 {
			t.Errorf("proposals after refusing %s: %v, want none", path, got)
		}

		if obs, err := f.Jonah.Observations(f.ctx(t), f.factory, 10); err != nil || len(obs) != 0 {
			t.Errorf("observations after refusing %s: %+v, %v; want none", path, obs, err)
		}

		reviews, err := f.Jonah.JonahReviews(f.ctx(t), f.factory, 1)
		if err != nil || len(reviews) != 1 || reviews[0].State != domain.JonahRunning {
			t.Fatalf("review after refusing %s: %+v, %v; want it running", path, reviews, err)
		}

		if _, _, err := f.Jonah.FinishJonahReview(f.ctx(t), claimed, domain.JonahReviewResult{State: domain.JonahFailed, Error: "bad path", At: t0.Add(time.Second)}); err != nil {
			t.Fatalf("fail the review: %v", err)
		}
	}
}

func (f *fixture) decide(t *testing.T, number int64, d domain.ProposalDecision) domain.Proposal {
	t.Helper()

	p, err := f.Jonah.DecideProposal(f.ctx(t), f.factory, number, d)
	if err != nil {
		t.Fatalf("DecideProposal %d: %v", number, err)
	}

	return p
}

// A proposal is decided once: a rejection needs a reason, and a decided proposal refuses another
// decision with ErrProposalDecided.
func testJonahDecide(f *fixture) {
	t := f.t
	f.finishReview(t, f.factory, t0, domain.JonahReviewResult{Proposals: []domain.NewProposal{proposal("a", "rules/a.md"), proposal("b", "rules/b.md")}})

	if _, err := f.Jonah.DecideProposal(f.ctx(t), f.factory, 1, domain.ProposalDecision{State: domain.ProposalRejected, By: ada, At: t1}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("reject without a reason: %v, want ErrInvalid", err)
	}

	p := f.decide(t, 1, domain.ProposalDecision{State: domain.ProposalRejected, RejectReason: "covered by rules/evidence.md", By: ada, At: t1})
	if p.State != domain.ProposalRejected || p.RejectReason != "covered by rules/evidence.md" || p.DecidedBy != ada.Ref() || !p.DecidedAt.Equal(t1) {
		t.Errorf("rejected %+v, want rejected by %s at %s with the reason", p, ada.Ref(), t1)
	}

	version := f.NewVersion(t, f.org, f.factory)

	_, err := f.Jonah.DecideProposal(f.ctx(t), f.factory, 1, domain.ProposalDecision{State: domain.ProposalPublished, PublishedVersionID: version, By: bob, At: t1})
	if !errors.Is(err, ports.ErrProposalDecided) {
		t.Fatalf("publish a rejected proposal: %v, want ErrProposalDecided", err)
	}

	if got, _ := f.Jonah.Proposal(f.ctx(t), f.factory, 1); got.State != domain.ProposalRejected || got.DecidedBy != ada.Ref() {
		t.Errorf("after the refused decision %+v, want still rejected by %s", got, ada.Ref())
	}

	p = f.decide(t, 2, domain.ProposalDecision{State: domain.ProposalPublished, PublishedVersionID: version, By: bob, At: t1})
	if p.State != domain.ProposalPublished || p.PublishedVersionID != version || p.RejectReason != "" {
		t.Errorf("published %+v, want published as %s", p, version)
	}

	f.expectNoProposal(t, 9)
}

// expectNoProposal checks that the factory has no proposal of number to read or decide.
func (f *fixture) expectNoProposal(t *testing.T, number int64) {
	t.Helper()

	if _, err := f.Jonah.DecideProposal(f.ctx(t), f.factory, number, domain.ProposalDecision{State: domain.ProposalApplied, By: ada, At: t1}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("decide proposal %d: %v, want ErrNotFound", number, err)
	}

	if _, err := f.Jonah.Proposal(f.ctx(t), f.factory, number); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Proposal %d: %v, want ErrNotFound", number, err)
	}
}

// Another organization sees none of the factory's reviews, proposals or observations, and
// cannot claim or decide them.
func testJonahTenancy(f *fixture) {
	t := f.t
	f.finishReview(t, f.factory, t0, domain.JonahReviewResult{
		Proposals:    []domain.NewProposal{proposal("a", "rules/a.md")},
		Observations: []domain.NewObservation{{Summary: "seen", Count: 1, Proposal: 1}},
	})
	f.queueReview(t, f.factory, t1)

	other := domain.WithOrg(t.Context(), f.NewOrg(t))

	if rs, err := f.Jonah.JonahReviews(other, f.factory, 10); err != nil || len(rs) != 0 {
		t.Errorf("other org's reviews: %+v, %v; want none", rs, err)
	}

	if ps, err := f.Jonah.Proposals(other, ports.ProposalQuery{Factory: f.factory, Limit: 10}); err != nil || len(ps) != 0 {
		t.Errorf("other org's proposals: %+v, %v; want none", ps, err)
	}

	if obs, err := f.Jonah.Observations(other, f.factory, 10); err != nil || len(obs) != 0 {
		t.Errorf("other org's observations: %+v, %v; want none", obs, err)
	}

	if _, err := f.Jonah.Proposal(other, f.factory, 1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("other org's Proposal 1: %v, want ErrNotFound", err)
	}

	if _, err := f.Jonah.DecideProposal(other, f.factory, 1, domain.ProposalDecision{State: domain.ProposalApplied, By: ada, At: t1}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("other org's DecideProposal: %v, want ErrNotFound", err)
	}

	if _, err := f.Jonah.ClaimJonahReview(other, t1, ttl); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("other org's claim: %v, want ErrNotFound", err)
	}

	if _, err := f.Jonah.JonahReviews(t.Context(), f.factory, 10); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("reviews without an org: %v, want ErrNoOrg", err)
	}

	f.expectFlowInvisible(other, t)
}

// expectFlowInvisible checks that other, another organization, sees none of the factory's
// sent-back touches or spans, and that both reads refuse a ctx without an org.
func (f *fixture) expectFlowInvisible(other context.Context, t *testing.T) {
	t.Helper()

	it := f.create(t, domain.NewItem{CreatedAt: t0})
	_, l := f.claim(t, work, 0, t0)
	f.move(t, next(it, l, t1))
	f.appendEvents(t, by(it, ada, domain.EventRejected, t1, map[string]any{keyReason: "no screenshot"}))

	until := t1.Add(time.Hour)

	if got, err := f.Jonah.SentBackBetween(f.ctx(t), f.factory, t0, until, 10); err != nil || len(got) != 1 {
		t.Fatalf("own SentBackBetween: %+v, %v; want 1", got, err)
	}

	if got, err := f.Jonah.SentBackBetween(other, f.factory, t0, until, 10); err != nil || len(got) != 0 {
		t.Errorf("other org's SentBackBetween: %+v, %v; want none", got, err)
	}

	if got, err := f.Jonah.StationFlow(other, f.factory, stWork, t0, until); err != nil || got != (domain.StationFlow{}) {
		t.Errorf("other org's StationFlow: %+v, %v; want nothing measured", got, err)
	}

	if _, err := f.Jonah.SentBackBetween(t.Context(), f.factory, t0, until, 10); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("SentBackBetween without an org: %v, want ErrNoOrg", err)
	}

	if _, err := f.Jonah.StationFlow(t.Context(), f.factory, stWork, t0, until); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("StationFlow without an org: %v, want ErrNoOrg", err)
	}
}

// kindRun records a finished run of kind on it, started at at, that cost usd.
func (f *fixture) kindRun(t *testing.T, it domain.Item, kind domain.RunKind, at time.Time, usd float64) {
	t.Helper()

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model, StartedAt: at, Kind: kind,
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if err := f.Store.FinishRun(f.ctx(t), id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at.Add(time.Minute)); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
}

// JonahSpendSince sums the factory's jonah runs from since, the bound included: not station runs,
// runs before since or another factory's.
func testJonahSpend(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})
	elsewhere := f.create(t, domain.NewItem{FactoryID: f.newFactory(t)})

	f.kindRun(t, it, domain.RunKindJonah, t1.Add(-time.Second), 5)
	f.kindRun(t, it, domain.RunKindJonah, t1, 1.5)
	f.kindRun(t, it, domain.RunKindJonah, t1.Add(time.Hour), 0.25)
	f.kindRun(t, it, domain.RunKindStation, t1, 3)
	f.kindRun(t, it, "", t1, 4)
	f.kindRun(t, elsewhere, domain.RunKindJonah, t1, 8)

	usd, err := f.Jonah.JonahSpendSince(f.ctx(t), f.factory, t1)
	if err != nil || roundTo(usd, 4) != 1.75 {
		t.Errorf("JonahSpendSince = %v, %v; want 1.75", usd, err)
	}

	if all := f.spendSince(t, f.factory, t1); roundTo(all, 4) != 8.75 {
		t.Errorf("SpendSince = %v, want 8.75: every kind counts there", all)
	}
}

// SentBackSince counts a person's rejected and gate_rejected touches of the factory from since:
// not an agent's send-back, other touches, earlier ones or another factory's.
func testJonahSentBack(f *fixture) {
	t := f.t
	a := f.create(t, domain.NewItem{})
	b := f.create(t, domain.NewItem{})
	elsewhere := f.create(t, domain.NewItem{FactoryID: f.newFactory(t)})

	f.appendEvents(t,
		by(a, ada, domain.EventRejected, t1.Add(-time.Second), map[string]any{keyReason: "early"}),
		by(a, ada, domain.EventRejected, t1, map[string]any{keyReason: "wrong file"}),
		by(b, bob, domain.EventGateRejected, t1.Add(time.Minute), nil),
		by(b, bob, domain.EventNote, t1.Add(2*time.Minute), map[string]any{keyText: "x"}),
		agentEvent(b, agWorker, domain.EventGateRejected, t1.Add(3*time.Minute)),
		by(elsewhere, ada, domain.EventRejected, t1, nil),
	)

	n, err := f.Jonah.SentBackSince(f.ctx(t), f.factory, t1)
	if err != nil || n != 2 {
		t.Errorf("SentBackSince = %d, %v; want 2", n, err)
	}
}

// eventAt is the id of item's event of kind at `at`.
func (f *fixture) eventAt(t *testing.T, item domain.ItemID, kind domain.EventKind, at time.Time) int64 {
	t.Helper()

	for _, e := range f.Events(t, item) {
		if e.Kind == kind && e.At.Equal(at) {
			return e.ID
		}
	}

	t.Fatalf("no %s event of item %s at %s", kind, item, at)

	return 0
}

// SentBackBetween lists a person's rejected and gate_rejected touches of the factory in
// [since, until), oldest first, at most limit, each with its item, the event's reason and the
// agent of the item's last lease that ended by then: not touches outside the window, other
// touches, an agent's send-back or another factory's.
func testJonahSentBackBetween(f *fixture) {
	t := f.t
	since, until := t1, t1.Add(time.Hour)

	a := f.create(t, domain.NewItem{Title: "Fix the login", CreatedAt: t0})
	_, l := f.claim(t, work, 0, t0)
	f.move(t, next(a, l, t0.Add(10*time.Minute))) // the worker's lease on a ends before the window

	b := f.create(t, domain.NewItem{Title: "Add the export", CreatedAt: t0.Add(time.Minute)})
	elsewhere := f.create(t, domain.NewItem{FactoryID: f.newFactory(t)})

	f.appendEvents(t,
		by(a, ada, domain.EventRejected, since.Add(-time.Second), map[string]any{keyReason: "early"}),
		by(a, ada, domain.EventRejected, since, map[string]any{keyReason: "missed the test"}),
		by(b, bob, domain.EventGateRejected, since.Add(time.Minute), nil),
		by(b, bob, domain.EventNote, since.Add(2*time.Minute), map[string]any{keyText: "x"}),
		agentEvent(b, agWorker, domain.EventGateRejected, since.Add(3*time.Minute)),
		by(a, ada, domain.EventGateRejected, since.Add(4*time.Minute), map[string]any{keyReason: "still wrong"}),
		by(a, ada, domain.EventRejected, until, map[string]any{keyReason: "late"}),
		by(elsewhere, ada, domain.EventRejected, since, nil),
	)

	want := []domain.SentBack{
		{
			EventID: f.eventAt(t, a.ID, domain.EventRejected, since), ItemID: a.ID, ItemNumber: a.Number, ItemTitle: a.Title,
			Station: stWork, Agent: agWorker, Kind: domain.EventRejected, Reason: "missed the test", At: since,
		},
		{
			EventID: f.eventAt(t, b.ID, domain.EventGateRejected, since.Add(time.Minute)), ItemID: b.ID, ItemNumber: b.Number,
			ItemTitle: b.Title, Station: stWork, Kind: domain.EventGateRejected, At: since.Add(time.Minute),
		},
		{
			EventID: f.eventAt(t, a.ID, domain.EventGateRejected, since.Add(4*time.Minute)), ItemID: a.ID, ItemNumber: a.Number,
			ItemTitle: a.Title, Station: stWork, Agent: agWorker, Kind: domain.EventGateRejected, Reason: "still wrong",
			At: since.Add(4 * time.Minute),
		},
	}

	got := f.sentBackBetween(t, since, until, 10)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SentBackBetween =\n%+v\nwant\n%+v", got, want)
	}

	if got := f.sentBackBetween(t, since, until, 2); !reflect.DeepEqual(got, want[:2]) {
		t.Errorf("SentBackBetween limit 2 =\n%+v\nwant the two oldest\n%+v", got, want[:2])
	}
}

func (f *fixture) sentBackBetween(t *testing.T, since, until time.Time, limit int) []domain.SentBack {
	t.Helper()

	got, err := f.Jonah.SentBackBetween(f.ctx(t), f.factory, since, until, limit)
	if err != nil {
		t.Fatalf("SentBackBetween: %v", err)
	}

	for i := range got {
		got[i].At = got[i].At.UTC()
	}

	return got
}

// StationFlow measures a station over [since, until): the items with a span there that left in
// it, the sent-back touches given there in it, and the p85 of those spans' durations; not a span
// that left before the window or is still open, a rejection at another station or by an agent,
// or another factory's.
func testJonahStationFlow(f *fixture) {
	t := f.t
	since, until := t1, t1.Add(10*time.Hour)
	h := func(n int) time.Time { return t1.Add(time.Duration(n) * time.Hour) }

	a := f.create(t, domain.NewItem{CreatedAt: h(-2)})
	_, l := f.claim(t, work, 0, h(-1)) // a's queued span at work leaves before the window
	f.move(t, next(a, l, h(1)))        // its in-progress span: 2 h

	c := f.create(t, domain.NewItem{CreatedAt: h(2)})
	_, l = f.claim(t, work, 0, h(3)) // c's queued span: 1 h
	f.move(t, next(c, l, h(7)))      // its in-progress span: 4 h

	d := f.create(t, domain.NewItem{CreatedAt: h(8)}) // still at work at until
	elsewhere := f.create(t, domain.NewItem{FactoryID: f.newFactory(t), CreatedAt: h(1)})

	atWork := func(it domain.Item) domain.Item { it.Station = stWork; return it }
	atNext := func(it domain.Item) domain.Item { it.Station = stNext; return it }

	f.appendEvents(t,
		by(atWork(a), ada, domain.EventRejected, h(-1), nil), // before the window
		by(atWork(c), ada, domain.EventRejected, h(4), map[string]any{keyReason: "no tests"}),
		by(atWork(d), bob, domain.EventGateRejected, h(9), nil),
		agentEvent(atWork(c), agWorker, domain.EventGateRejected, h(5)),
		by(atNext(a), ada, domain.EventRejected, h(6), nil),
		by(atWork(elsewhere), ada, domain.EventRejected, h(2), nil),
	)

	got, err := f.Jonah.StationFlow(f.ctx(t), f.factory, stWork, since, until)
	if err != nil {
		t.Fatalf("StationFlow: %v", err)
	}

	// Spans of 1 h, 2 h and 4 h: rank 1.7 lies 0.7 of the way from 2 h to 4 h.
	want := domain.StationFlow{Passed: 2, SentBack: 2, LeadTimeP85: 3*time.Hour + 24*time.Minute}
	if got != want {
		t.Errorf("StationFlow at %s = %+v, want %+v", stWork, got, want)
	}

	if got, err := f.Jonah.StationFlow(f.ctx(t), f.factory, stReview, since, until); err != nil || got != (domain.StationFlow{}) {
		t.Errorf("StationFlow at an idle station = %+v, %v; want nothing measured", got, err)
	}
}
