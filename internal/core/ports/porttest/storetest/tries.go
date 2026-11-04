package storetest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// triesTests are the PastTries cases (ADR 0039 decisions 9 and 11), registered by Run under Tries_.
var triesTests = []struct {
	name string
	test func(f *fixture)
}{
	{"Tries_QueuedOncePerProposal", testTriesQueuedOnce},
	{"Tries_ThreePerItem", testTriesThreePerItem},
	{"Tries_AgentDoneItemsNewestFirst", testTriesAgentDoneItems},
	{"Tries_ClaimCappedPerFactoryAndLeased", testTriesClaim},
	{"Tries_FinishStoresVerdictAndReason", testTriesGraded},
	{"Tries_TargetIsNewestFirstRunOfAClaim", testTriesTarget},
	{"Tries_InputHasBriefAndEarlierOutputs", testTriesInput},
	{"Tries_TryRunWithoutItemIsJonahs", testTriesRunWithoutItem},
	{"Tries_OtherOrgInvisible", testTriesTenancy},
}

// head is a well-formed recorded commit.
var head = strings.Repeat("c", 40)

// pastRun records a finished station run of it at station started at at, with a branch head
// when first (a claim's first run), and output; it returns the run.
func (f *fixture) pastRun(t *testing.T, it domain.Item, station string, at time.Time, first bool, output string) domain.RunID {
	t.Helper()

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: station, Agent: agWorker, Model: model, StartedAt: at, Kind: domain.RunKindStation,
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	in := domain.RunInput{Brief: "brief at " + at.Format(time.TimeOnly)}
	if first {
		in.BranchHead, in.BaseHead = head, strings.Repeat("b", 40)
	}

	if err := f.Store.RecordRunInput(f.ctx(t), id, in); err != nil {
		t.Fatalf("RecordRunInput: %v", err)
	}

	res := domain.RunResult{Status: domain.RunSucceeded, CostUSD: 1}
	if output != "" {
		res.Output = json.RawMessage(output)
	}

	if err := f.Store.FinishRun(f.ctx(t), id, res, at.Add(time.Minute)); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	return id
}

// triedProposal is a proposal of the fixture's factory, from a finished review.
func (f *fixture) triedProposal(t *testing.T, at time.Time) domain.Proposal {
	t.Helper()

	_, ps := f.finishReview(t, f.factory, at, domain.JonahReviewResult{Proposals: []domain.NewProposal{proposal("tighten", "agents/worker.md")}})

	return ps[0]
}

// newTries queues one try per item of the fixture's factory's proposal p, each of a recorded run.
func (f *fixture) newTries(t *testing.T, p domain.Proposal, items ...domain.Item) []domain.PastTry {
	t.Helper()

	var ns []domain.NewPastTry

	for _, it := range items {
		run := f.pastRun(t, it, stWork, t0, true, "")
		ns = append(ns, domain.NewPastTry{ProposalID: p.ID, ItemID: it.ID, Station: stWork, OriginalRunID: run, Model: model})
	}

	tries, err := f.Tries.QueuePastTries(f.ctx(t), f.factory, p.ID, ns, t1)
	if err != nil {
		t.Fatalf("QueuePastTries: %v", err)
	}

	return tries
}

// A proposal's tries are queued once, all or none, with the proposal's number: a second queue is a
// conflict and changes nothing.
func testTriesQueuedOnce(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	a, b := f.create(t, domain.NewItem{}), f.create(t, domain.NewItem{})

	tries := f.newTries(t, p, a, b)
	if len(tries) != 2 || tries[0].State != domain.PastTryQueued || tries[0].ProposalNumber != p.Number || tries[1].ItemID != b.ID {
		t.Fatalf("QueuePastTries = %+v; want two queued tries of proposal %d, a's then b's", tries, p.Number)
	}

	_, err := f.Tries.QueuePastTries(f.ctx(t), f.factory, p.ID, []domain.NewPastTry{{
		ProposalID: p.ID, ItemID: a.ID, Station: stWork, OriginalRunID: tries[0].OriginalRunID, Model: model,
	}}, t1)
	expect(t, "QueuePastTries of a proposal with tries", err, ports.ErrConflict)

	_, err = f.Tries.QueuePastTries(f.ctx(t), f.factory, p.ID, []domain.NewPastTry{{ProposalID: p.ID}}, t1)
	expect(t, "QueuePastTries of an incomplete try", err, domain.ErrInvalid)

	got, err := f.Tries.ProposalPastTries(f.ctx(t), p.ID)
	if err != nil || len(got) != 2 || got[0].ID != tries[0].ID {
		t.Errorf("ProposalPastTries = %+v, %v; want the two tries, oldest first", got, err)
	}

	one, err := f.Tries.PastTry(f.ctx(t), tries[1].ID)
	if err != nil || one.ItemID != b.ID || one.Station != stWork || one.Model != model || one.ProposalNumber != p.Number {
		t.Errorf("PastTry = %+v, %v; want b's try", one, err)
	}
}

// A claim takes the factory's oldest due try while fewer than limit run under a live lease, and
// its finish is fenced by its lease (testTriesFenced).
func testTriesClaim(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	tries := f.newTries(t, p, f.create(t, domain.NewItem{}), f.create(t, domain.NewItem{}), f.create(t, domain.NewItem{}))
	now := t1.Add(time.Hour)

	first, err := f.Tries.ClaimPastTry(f.ctx(t), f.factory, now, ttl, 1)
	if err != nil || first.ID != tries[0].ID || first.State != domain.PastTryRunning || first.Attempts != 1 {
		t.Fatalf("ClaimPastTry = %+v, %v; want the oldest try running on its first attempt", first, err)
	}

	_, err = f.Tries.ClaimPastTry(f.ctx(t), f.factory, now, ttl, 1)
	expect(t, "ClaimPastTry at the factory's cap", err, ports.ErrNotFound)

	_, err = f.Tries.ClaimPastTry(f.ctx(t), f.newFactory(t), now, ttl, 2)
	expect(t, "ClaimPastTry of a factory without tries", err, ports.ErrNotFound)

	second, err := f.Tries.ClaimPastTry(f.ctx(t), f.factory, now, ttl, 2)
	if err != nil || second.ID != tries[1].ID {
		t.Fatalf("ClaimPastTry under a cap of 2 = %+v, %v; want the second try", second, err)
	}

	testTriesFenced(f, first, second, now)
}

// A try whose lease ended is claimed again with one more attempt, and its old claim no longer
// finishes it; a finish stores the verdict error with its reason, or a run's diff, cost and run.
func testTriesFenced(f *fixture, first, second domain.PastTry, now time.Time) {
	t := f.t
	later := now.Add(ttl)

	again, err := f.Tries.ClaimPastTry(f.ctx(t), f.factory, later, ttl, 3)
	if err != nil || again.ID != first.ID || again.Attempts != 2 {
		t.Fatalf("ClaimPastTry after the lease ended = %+v, %v; want the first try on attempt 2", again, err)
	}

	res := domain.PastTryResult{Verdict: domain.PastTryError, Error: "the commit is gone", At: later}

	_, err = f.Tries.FinishPastTry(f.ctx(t), first, res)
	expect(t, "FinishPastTry under a lost lease", err, ports.ErrLeaseLost)

	done, err := f.Tries.FinishPastTry(f.ctx(t), again, res)
	if err != nil || done.State != domain.PastTryDone || done.Verdict != domain.PastTryError || done.Error != res.Error || !done.FinishedAt.Equal(later) {
		t.Fatalf("FinishPastTry = %+v, %v; want done with verdict error", done, err)
	}

	_, err = f.Tries.FinishPastTry(f.ctx(t), again, res)
	expect(t, "FinishPastTry twice", err, ports.ErrLeaseLost)

	_, err = f.Tries.FinishPastTry(f.ctx(t), second, domain.PastTryResult{Verdict: "fail", Error: "x", At: later})
	expect(t, "FinishPastTry with a fail verdict", err, domain.ErrInvalid)

	f.finishRan(second, later)
}

// finishRan finishes the claimed try as a run that ended: done with no verdict, its diff, cost
// and run.
func (f *fixture) finishRan(second domain.PastTry, later time.Time) {
	t := f.t
	run := f.pastRun(t, f.create(t, domain.NewItem{}), stNext, t1, false, "")
	ok := domain.PastTryResult{Diff: "diff --git a/x b/x\n", CostUSD: 0.25, TryRunID: run, At: later}

	got, err := f.Tries.FinishPastTry(f.ctx(t), second, ok)
	if err != nil || got.Verdict != "" || got.Diff != ok.Diff || got.CostUSD != ok.CostUSD || got.TryRunID != run {
		t.Errorf("FinishPastTry of a run that ended = %+v, %v; want done with no verdict, its diff, cost and run", got, err)
	}
}

// A finish stores a graded verdict with its reason, and ProposalPastTries and PastTry read both
// back; a grade without its reason is refused.
func testTriesGraded(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	tries := f.newTries(t, p, f.create(t, domain.NewItem{}), f.create(t, domain.NewItem{}))
	now := t1.Add(time.Hour)
	run := f.pastRun(t, f.create(t, domain.NewItem{}), stNext, t1, false, "")

	for i, v := range []domain.PastTryVerdict{domain.PastTryNotFixed, domain.PastTryFixed} {
		f.finishGraded(tries[i], v, run, now)
	}

	got, err := f.Tries.ProposalPastTries(f.ctx(t), p.ID)
	if err != nil || len(got) != 2 || got[0].Verdict != domain.PastTryNotFixed || got[1].Verdict != domain.PastTryFixed ||
		got[0].Reason != "check `make test` exited 2" || got[0].Error != "" {
		t.Errorf("ProposalPastTries = %+v, %v; want not fixed then fixed, each with its reason", got, err)
	}

	one, err := f.Tries.PastTry(f.ctx(t), tries[1].ID)
	if err != nil || one.Verdict != domain.PastTryFixed || one.Reason != "check `make test` exited 2" {
		t.Errorf("PastTry = %+v, %v; want fixed with its reason", one, err)
	}
}

// finishGraded claims want, the factory's next due try, and finishes it graded v with a reason,
// after a finish of v without its reason is refused.
func (f *fixture) finishGraded(want domain.PastTry, v domain.PastTryVerdict, run domain.RunID, now time.Time) {
	t := f.t

	claimed, err := f.Tries.ClaimPastTry(f.ctx(t), f.factory, now, ttl, 2)
	if err != nil || claimed.ID != want.ID {
		t.Fatalf("ClaimPastTry = %+v, %v; want try %s", claimed, err, want.ID)
	}

	_, err = f.Tries.FinishPastTry(f.ctx(t), claimed, domain.PastTryResult{Verdict: v, At: now})
	expect(t, "FinishPastTry of a grade without its reason", err, domain.ErrInvalid)

	res := domain.PastTryResult{Verdict: v, Reason: "check `make test` exited 2", Diff: "diff", CostUSD: 0.5, TryRunID: run, At: now}

	done, err := f.Tries.FinishPastTry(f.ctx(t), claimed, res)
	if err != nil || done.Verdict != v || done.Reason != res.Reason {
		t.Fatalf("FinishPastTry = %+v, %v; want verdict %s with its reason", done, err, v)
	}
}

// A try reruns the item's newest finished station run at the station that was a claim's first:
// not a retry (no branch head), a running run, another station's or another item's.
func testTriesTarget(f *fixture) {
	t := f.t
	it, other := f.create(t, domain.NewItem{}), f.create(t, domain.NewItem{})

	f.pastRun(t, it, stWork, t0, true, "")
	want := f.pastRun(t, it, stWork, t0.Add(time.Hour), true, "")
	f.pastRun(t, it, stWork, t0.Add(2*time.Hour), false, "")
	f.pastRun(t, it, stNext, t0.Add(3*time.Hour), true, "")
	f.pastRun(t, other, stWork, t0.Add(4*time.Hour), true, "")

	running, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model, StartedAt: t0.Add(5 * time.Hour),
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if err := f.Store.RecordRunInput(f.ctx(t), running, domain.RunInput{BranchHead: head}); err != nil {
		t.Fatalf("RecordRunInput: %v", err)
	}

	got, err := f.Tries.PastTryTarget(f.ctx(t), it.ID, stWork)
	if err != nil || got.ID != want || got.Input.BranchHead != head {
		t.Errorf("PastTryTarget = %+v, %v; want run %s", got, err, want)
	}

	_, err = f.Tries.PastTryTarget(f.ctx(t), it.ID, stReview)
	expect(t, "PastTryTarget at a station without runs", err, ports.ErrNotFound)
}

// A try's input is its original run with its brief and heads, and the item's latest output of each
// station among the runs that ended before the original started.
func testTriesInput(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})

	f.pastRun(t, it, stNext, t0, true, `{"n":1}`)
	f.pastRun(t, it, stNext, t0.Add(time.Hour), true, `{"n":2}`)
	f.pastRun(t, it, stWork, t0.Add(2*time.Hour), true, `{"w":1}`)
	orig := f.pastRun(t, it, stWork, t0.Add(3*time.Hour), true, `{"w":2}`)
	f.pastRun(t, it, stNext, t0.Add(4*time.Hour), true, `{"n":3}`)

	in, err := f.Tries.PastTryInput(f.ctx(t), orig)
	if err != nil {
		t.Fatalf("PastTryInput: %v", err)
	}

	if in.Run.ID != orig || in.Run.Input.Brief == "" || in.Run.Input.BranchHead != head || in.Run.Station != stWork {
		t.Errorf("PastTryInput run = %+v; want the original run with its brief and heads", in.Run)
	}

	if len(in.Outputs) != 2 || string(in.Outputs[stNext]) != `{"n": 2}` && string(in.Outputs[stNext]) != `{"n":2}` ||
		string(in.Outputs[stWork]) != `{"w": 1}` && string(in.Outputs[stWork]) != `{"w":1}` {
		t.Errorf("PastTryInput outputs = %s; want next's second and work's first", in.Outputs)
	}

	_, err = f.Tries.PastTryInput(f.ctx(t), missingRun)
	expect(t, "PastTryInput of a missing run", err, ports.ErrNotFound)
}

// A try's own run works no item: it adds nothing to an item's cost or events, and its cost is
// Jonah's, never the line's.
func testTriesRunWithoutItem(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})
	f.kindRun(t, it, domain.RunKindStation, t1, 3)

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{FactoryID: f.factory, Station: stWork, Agent: agWorker, Model: model, StartedAt: t1, Kind: domain.RunKindTry})
	if err != nil {
		t.Fatalf("StartRun of a try without an item: %v", err)
	}

	if err := f.Store.FinishRun(f.ctx(t), id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.5}, t1.Add(time.Minute)); err != nil {
		t.Fatalf("FinishRun of a try: %v", err)
	}

	if s, err := f.Jonah.JonahSpendSince(f.ctx(t), f.factory, t1); err != nil || roundTo(s.JonahUSD, 4) != 0.5 || roundTo(s.LineUSD, 4) != 3 {
		t.Errorf("JonahSpendSince = %+v, %v; want the try's $0.5 as Jonah's and $3 as the line's", s, err)
	}

	if all := f.spendSince(t, f.factory, t1); roundTo(all, 4) != 3 {
		t.Errorf("SpendSince = %v, want 3: a try never counts toward onExhausted", all)
	}

	got, err := f.Store.Item(f.ctx(t), it.ID)
	if err != nil || roundTo(got.CostUSD, 4) != 3 {
		t.Errorf("item cost = %v, %v; want 3, the try's cost on no item", got.CostUSD, err)
	}
}

// Another organization sees none of the tries and claims none of them.
func testTriesTenancy(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	tries := f.newTries(t, p, f.create(t, domain.NewItem{}))
	other := f.otherOrg(t)

	_, err := f.Tries.PastTry(other.ctx(t), tries[0].ID)
	expect(t, "PastTry of another org's try", err, ports.ErrNotFound)

	if got, err := f.Tries.ProposalPastTries(other.ctx(t), p.ID); err != nil || len(got) != 0 {
		t.Errorf("ProposalPastTries of another org = %+v, %v; want none", got, err)
	}

	_, err = f.Tries.ClaimPastTry(other.ctx(t), f.factory, t1, ttl, 2)
	expect(t, "ClaimPastTry of another org's factory", err, ports.ErrNotFound)

	_, err = f.Tries.PastTryInput(other.ctx(t), tries[0].OriginalRunID)
	expect(t, "PastTryInput of another org's run", err, ports.ErrNotFound)

	_, err = f.Tries.ClaimPastTry(t.Context(), f.factory, t1, ttl, 2)
	if !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("ClaimPastTry without an org = %v, want ErrNoOrg", err)
	}
}

// A proposal may have up to three tries of one item, one per attempt: a try without an attempt is
// the first, and an item and attempt given twice is a conflict that queues none.
func testTriesThreePerItem(f *fixture) {
	t := f.t
	p := f.triedProposal(t, t1)
	it := f.create(t, domain.NewItem{})
	run := f.pastRun(t, it, stWork, t0, true, "")
	try := func(attempt int) domain.NewPastTry {
		return domain.NewPastTry{ProposalID: p.ID, ItemID: it.ID, Attempt: attempt, Station: stWork, OriginalRunID: run, Model: model}
	}

	_, err := f.Tries.QueuePastTries(f.ctx(t), f.factory, p.ID, []domain.NewPastTry{try(1), try(1)}, t1)
	expect(t, "QueuePastTries of one attempt twice", err, ports.ErrConflict)

	_, err = f.Tries.QueuePastTries(f.ctx(t), f.factory, p.ID, []domain.NewPastTry{try(1), try(domain.ModelTriesPerItem + 1)}, t1)
	expect(t, "QueuePastTries of a fourth attempt", err, domain.ErrInvalid)

	if _, err := f.Tries.QueuePastTries(f.ctx(t), f.factory, p.ID, []domain.NewPastTry{try(0), try(2), try(3)}, t1); err != nil {
		t.Fatalf("QueuePastTries of three attempts: %v", err)
	}

	got, err := f.Tries.ProposalPastTries(f.ctx(t), p.ID)
	if err != nil || len(got) != 3 {
		t.Fatalf("ProposalPastTries = %+v, %v; want three tries", got, err)
	}

	attempts := map[int]bool{}
	for _, tr := range got {
		attempts[tr.Attempt] = tr.ItemID == it.ID
	}

	if !attempts[1] || !attempts[2] || !attempts[3] {
		t.Errorf("attempts = %v, want 1, 2 and 3 of the item", attempts)
	}
}

// AgentDoneItems lists the done items the agent has a claim's first run of, the most recently done
// first, at most limit, each with that newest run, its send-backs' stations and the item's cost:
// not an open item, one whose run recorded no branch head, or another factory's.
func testTriesAgentDoneItems(f *fixture) {
	t := f.t

	a := f.create(t, domain.NewItem{Title: "older"})
	f.pastRun(t, a, stWork, t0, true, "")
	newest := f.pastRun(t, a, stWork, t0.Add(time.Minute), true, "")
	f.event(t, domain.Event{FactoryID: f.factory, ItemID: a.ID, At: t0.Add(2 * time.Minute), Kind: domain.EventGateRejected, Station: stNext, Actor: agWorker})
	f.finish(t, a, t1, false)

	b := f.create(t, domain.NewItem{Title: "newer"})
	f.pastRun(t, b, stWork, t0, true, "")
	f.finish(t, b, t1.Add(time.Minute), false)

	noHead := f.create(t, domain.NewItem{Title: "retry only"})
	f.pastRun(t, noHead, stWork, t0, false, "")
	f.finish(t, noHead, t1, false)

	open := f.create(t, domain.NewItem{Title: "open"})
	f.pastRun(t, open, stWork, t0, true, "")

	other := f.create(t, domain.NewItem{FactoryID: f.newFactory(t), Title: "another factory's"})
	f.pastRun(t, other, stWork, t0, true, "")
	f.finish(t, other, t1, false)

	got, err := f.Tries.AgentDoneItems(f.ctx(t), f.factory, agWorker, 5)
	if err != nil || len(got) != 2 || got[0].ItemID != b.ID || got[1].ItemID != a.ID {
		t.Fatalf("AgentDoneItems = %+v, %v; want b then a", got, err)
	}

	if g := got[1]; g.Run.ID != newest || g.ItemNumber != a.Number || g.ItemTitle != "older" || len(g.SentBackAt) != 1 ||
		g.SentBackAt[0] != stNext || g.CostUSD != 2 || !g.DoneAt.Equal(t1) {
		t.Errorf("AgentDoneItems[1] = %+v, want a's newest run, one send-back at %s, $2 and done at t1", g, stNext)
	}

	if got, _ := f.Tries.AgentDoneItems(f.ctx(t), f.factory, agWorker, 1); len(got) != 1 || got[0].ItemID != b.ID {
		t.Errorf("AgentDoneItems limit 1 = %+v, want b", got)
	}

	if got, _ := f.Tries.AgentDoneItems(f.ctx(t), f.factory, "nobody", 5); len(got) != 0 {
		t.Errorf("AgentDoneItems of another agent = %+v, want none", got)
	}
}
