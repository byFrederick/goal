package engine_test

import (
	"fmt"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// reject is the reviewer's verdict that sends an item back to build.
const reject = `{"decision":"reject","reason":"needs another look"}`

// turnLine is build (an agent) → merge (a gate with four slots whose approval merges, with
// merges) → done.
func turnLine(merges domain.MergesPolicy) domain.Line {
	land := agentStation(stReview, agReviewer, 4, 4)
	land.Gate = &domain.Gate{OnReject: stBuild, MaxRework: 3}
	land.Action = domain.ActionMerge
	land.Merges = merges

	l := lineOf(agentStation(stBuild, agWorker, 4, 4), land)
	l.Connections = []string{connGitHub, connGitLab}

	return l
}

// partsTurnLine is turnLine with a repository, in integration mode branch, so a part's merge
// target is its assembly's integration branch.
func partsTurnLine() domain.Line {
	l := turnLine("")
	l.Runtime.Repository = domain.Repository{URL: repoURL, Connection: connGit}
	l.Flow.Assemblies.Integration = domain.IntegrationPolicy{Mode: domain.IntegrationModeBranch, MinParts: 1}

	return l
}

// turnHarness is a harness on l whose reviewer approves each item with its own pull request,
// open on the code host, unless set otherwise.
func turnHarness(t *testing.T, l domain.Line) (*harness, *scripted) {
	t.Helper()

	s := newScripted()

	return newHarness(t, l, s.agent), s
}

// queueForTurn submits an item and queues it at the merge station, its reviewer approving with its
// own pull request.
func (h *harness) queueForTurn(t *testing.T, s *scripted, n domain.NewItem) domain.Item {
	t.Helper()

	return h.queueAtMerge(t, s, h.submit(t, n))
}

func (h *harness) queueAtMerge(t *testing.T, s *scripted, it domain.Item) domain.Item {
	t.Helper()

	url := prOf(it)
	h.codehost.Add(prFrom(url, it))
	s.set(it.ID, stReview, `{"decision":"approve","pull_request":"`+url+`"}`)

	return h.at(t, it, stReview, domain.StateQueued)
}

// prOf is the pull request queueAtMerge opened for it.
func prOf(it domain.Item) string {
	return fmt.Sprintf("https://github.com/acme/app/pull/%d", 100+it.Number)
}

// mergeRuns counts the runs of an item at the merge station: its claims there.
func (h *harness) mergeRuns(id domain.ItemID) int {
	n := 0

	for _, r := range h.worker.Runs() {
		if r.Sandbox.ItemID == id && r.Sandbox.Station == stReview {
			n++
		}
	}

	return n
}

// partsOf makes an assembly titled title with n parts and queues each part at the merge station.
func (h *harness) partsOf(t *testing.T, s *scripted, title string, n int) []domain.Item {
	t.Helper()

	it := h.at(t, h.submit(t, domain.NewItem{Title: title}), stBuild, domain.StateReady)

	plan := make([]domain.NewPart, 0, n)
	for i := range n {
		plan = append(plan, domain.NewPart{Key: fmt.Sprintf("part-%d", i), Title: fmt.Sprintf("%s %d", title, i)})
	}

	_, parts, err := h.store.Assemble(inOrg(t), ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stBuild, State: domain.StateAssembly}, At: h.clock.Now(),
		},
		Parts: plan,
	})
	if err != nil {
		t.Fatal(err)
	}

	out := make([]domain.Item, 0, n)
	for _, p := range parts.Items {
		out = append(out, h.queueAtMerge(t, s, p))
	}

	return out
}

// expectTurnOn fails unless id's only turn edge is on holder, by the engine.
func (h *harness) expectTurnOn(t *testing.T, id, holder domain.ItemID) {
	t.Helper()

	if got := h.edgesOf(t, id, domain.EdgeTurn); len(got) != 1 || got[0].Needs != holder || got[0].By != domain.EngineActor.Ref() {
		t.Errorf("item %s's turn edges = %+v, want one on %s by %s", id, got, holder, domain.EngineActor.Ref())
	}
}

// expectPosition fails unless the item is at pos.
func (h *harness) expectPosition(t *testing.T, id domain.ItemID, pos domain.Position) {
	t.Helper()

	if got := h.item(t, id); got.Position() != pos {
		t.Fatalf("item %s at %+v, want %+v (events %v, blocked %v)", id, got.Position(), pos, h.kinds(id), h.eventsOf(id, domain.EventBlocked))
	}
}

var (
	queuedAtMerge = domain.Position{Station: stReview, State: domain.StateQueued}
	isDone        = domain.Position{Station: stDone, State: domain.StateDone}
)

// Two standalone items share the default branch: of the two waiting at a merge station with four
// slots, only the first is claimed, and the second gets a turn edge on it and is claimed once it
// is done.
func TestEngine_TurnOneStandaloneItemAtATime(t *testing.T) {
	t.Parallel()

	h, s := turnHarness(t, turnLine(""))

	a := h.queueForTurn(t, s, domain.NewItem{Title: "a"})
	b := h.queueForTurn(t, s, domain.NewItem{Title: "b"})

	h.step(t)

	h.expectPosition(t, a.ID, isDone)
	h.expectPosition(t, b.ID, queuedAtMerge)

	if n := h.mergeRuns(b.ID); n != 0 {
		t.Fatalf("b ran %d times at merge while a held the turn, want 0", n)
	}

	h.expectTurnOn(t, b.ID, a.ID)

	h.step(t)

	h.expectPosition(t, b.ID, isDone)
}

// A holder sent back to an earlier station gives up its turn: the waiting item's turn edge is
// removed with the reason and the item is claimed.
func TestEngine_TurnReleasedWhenTheHolderIsSentBack(t *testing.T) {
	t.Parallel()

	h, s := turnHarness(t, turnLine(domain.MergesSerialize))

	a := h.queueForTurn(t, s, domain.NewItem{Title: "a"})
	b := h.queueForTurn(t, s, domain.NewItem{Title: "b"})
	s.set(a.ID, stReview, reject)

	h.step(t) // a is rejected back to build

	h.expectPosition(t, a.ID, domain.Position{Station: stBuild, State: domain.StateQueued})
	h.expectTurnOn(t, b.ID, a.ID)

	h.step(t) // a is built again, and b takes the turn

	h.expectPosition(t, b.ID, isDone)

	removed := h.eventsOf(b.ID, domain.EventEdgeRemoved)
	if len(removed) != 1 || removed[0].Data["needs"] != string(a.ID) || removed[0].Data["reason"] != "needed item left the station" {
		t.Errorf("b's edge_removed events %+v, want one on a, the needed item having left the station", removed)
	}
}

// Parts of two assemblies merge into two integration branches, so both are claimed at once.
func TestEngine_TurnPartsOfDifferentAssembliesRunTogether(t *testing.T) {
	t.Parallel()

	h, s := turnHarness(t, partsTurnLine())

	p := h.partsOf(t, s, "first request", 1)[0]
	q := h.partsOf(t, s, "second request", 1)[0]
	s.set(p.ID, stReview, reject)
	s.set(q.ID, stReview, reject)

	h.step(t)

	if np, nq := h.mergeRuns(p.ID), h.mergeRuns(q.ID); np != 1 || nq != 1 {
		t.Errorf("merge runs %d and %d, want one each: their targets differ", np, nq)
	}

	for _, id := range []domain.ItemID{p.ID, q.ID} {
		if got := h.edgesOf(t, id, domain.EdgeTurn); len(got) != 0 {
			t.Errorf("part %s's turn edges = %+v, want none", id, got)
		}
	}
}

// Two parts of one assembly merge into its integration branch, so they take turns.
func TestEngine_TurnPartsOfOneAssemblyTakeTurns(t *testing.T) {
	t.Parallel()

	h, s := turnHarness(t, partsTurnLine())

	parts := h.partsOf(t, s, assemblyTitle, 2)
	p, q := parts[0], parts[1]
	s.set(p.ID, stReview, reject)
	s.set(q.ID, stReview, reject)

	h.step(t)

	if np, nq := h.mergeRuns(p.ID), h.mergeRuns(q.ID); np+nq != 1 {
		t.Fatalf("merge runs %d and %d, want one between them: they share a target", np, nq)
	}

	first, second := p, q
	if h.mergeRuns(q.ID) == 1 {
		first, second = q, p
	}

	h.expectTurnOn(t, second.ID, first.ID)

	h.step(t) // first is back at build; second takes the turn

	if n := h.mergeRuns(second.ID); n != 1 {
		t.Errorf("second part ran %d times at merge after the first was sent back, want 1", n)
	}
}

// A holder whose merge is deferred goes back to the queue and keeps its turn, though another item
// comes first in pull order now.
func TestEngine_TurnDeferredHolderKeepsItsTurn(t *testing.T) {
	t.Parallel()

	h, s := turnHarness(t, turnLine(""))

	x := h.at(t, h.submit(t, domain.NewItem{Title: "x"}), stBuild, domain.StateBlocked)
	a := h.queueForTurn(t, s, domain.NewItem{Title: "a"})
	b := h.queueForTurn(t, s, domain.NewItem{Title: "b", Priority: 5})

	// b goes first in pull order, but a person's link on x holds it: a takes the turn.
	if _, err := h.store.AddEdge(inOrg(t), domain.NewEdge{Item: b.ID, Needs: x.ID, Kind: domain.EdgeLinked, By: human, At: h.clock.Now()}); err != nil {
		t.Fatal(err)
	}

	h.codehost.RefuseMerges(prOf(a), 1)

	h.step(t)

	h.expectDeferredFor(t, a.ID, prOf(a), 1, "required checks")
	h.expectTurnOn(t, b.ID, a.ID)

	if err := h.store.RemoveEdge(inOrg(t), b.ID, x.ID, human, h.clock.Now(), ""); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	h.expectPosition(t, a.ID, isDone)

	if n := h.mergeRuns(b.ID); n != 0 {
		t.Fatalf("b ran %d times at merge while the deferred a held the turn, want 0", n)
	}

	h.step(t)

	h.expectPosition(t, b.ID, isDone)
}

// A holder held for CI after its push (ADR 0044 decision 3) waits in the queue and keeps its
// turn: an item that arrives after the hold and comes first in pull order is not claimed and
// does not merge until the holder's held approval has merged.
func TestEngine_TurnCIHeldHolderKeepsItsTurn(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine(domain.ActionMerge, domain.DefaultCIRounds))

	a := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	h.codehost.Add(prFrom(prOf(a), a))
	p.set(a.ID, stReview, pushedTo(`{"decision":"approve","pull_request":"`+prOf(a)+`"}`, pushSHA1))

	h.step(t)
	h.expectHeld(t, a.ID, pushSHA1, 1, 1)

	b := h.at(t, h.submit(t, domain.NewItem{Title: "b", Priority: 5}), stReview, domain.StateQueued)
	h.codehost.Add(prFrom(prOf(b), b))
	p.set(b.ID, stReview, ok(`{"decision":"approve","pull_request":"`+prOf(b)+`"}`))

	h.step(t)

	if n := h.mergeRuns(b.ID); n != 0 {
		t.Fatalf("b ran %d times at merge while a waited for CI, want 0", n)
	}

	h.expectPosition(t, b.ID, queuedAtMerge)
	h.expectTurnOn(t, b.ID, a.ID)

	if edges := h.edgesOf(t, a.ID, domain.EdgeTurn); len(edges) != 0 {
		t.Errorf("a's turn edges = %+v, want none: a holds the turn", edges)
	}

	h.endCI(t, pushSHA1, checksOf(ports.CheckPassed, ports.CheckFinished))
	h.step(t)

	h.expectPosition(t, a.ID, isDone)
	h.expectPosition(t, b.ID, queuedAtMerge)

	if removed := h.eventsOf(b.ID, domain.EventEdgeRemoved); len(removed) != 0 && removed[0].Data["reason"] != "needed item left the station" {
		t.Errorf("b's edge_removed events %+v, want only a leaving the station", removed)
	}

	h.step(t)

	h.expectPosition(t, b.ID, isDone)

	if m := h.codehost.Merges(); len(m) != 2 || m[0].URL != prOf(a) || m[1].URL != prOf(b) {
		t.Errorf("merges = %+v; want a's, then b's", m)
	}
}

// A holder waiting on a person at the merge station (a question or a tool approval) keeps its
// turn: its pull request is still open, so the item waiting behind it is not claimed until the
// holder is answered and done.
func TestEngine_TurnWaitingHumanHolderKeepsItsTurn(t *testing.T) {
	t.Parallel()

	h, s := turnHarness(t, turnLine(""))

	a := h.queueForTurn(t, s, domain.NewItem{Title: "a"})
	b := h.queueForTurn(t, s, domain.NewItem{Title: "b"})

	if _, err := h.store.AddEdge(inOrg(t), domain.NewEdge{Item: b.ID, Needs: a.ID, Kind: domain.EdgeTurn, By: domain.EngineActor, At: h.clock.Now()}); err != nil {
		t.Fatal(err)
	}

	a = h.at(t, a, stReview, domain.StateWaitingHuman)

	h.step(t)

	if n := h.mergeRuns(b.ID); n != 0 {
		t.Fatalf("b ran %d times at merge while a waited on a person, want 0", n)
	}

	h.expectPosition(t, b.ID, queuedAtMerge)
	h.expectTurnOn(t, b.ID, a.ID)

	if removed := h.eventsOf(b.ID, domain.EventEdgeRemoved); len(removed) != 0 {
		t.Errorf("b's edge_removed events %+v, want none while a holds the turn", removed)
	}

	h.at(t, a, stReview, domain.StateQueued) // a is answered and goes on

	h.step(t)

	h.expectPosition(t, a.ID, isDone)
	h.expectPosition(t, b.ID, queuedAtMerge)

	h.step(t)

	h.expectPosition(t, b.ID, isDone)
}

// A turn edge whose reason no longer holds is removed with an edge_removed event that says why:
// the needed item is blocked, has left the station or has another merge target. The waiting items
// then wait for the target's holder instead.
func TestEngine_TurnRemovesAStaleEdge(t *testing.T) {
	t.Parallel()

	h, s := turnHarness(t, partsTurnLine())

	w1 := h.queueForTurn(t, s, domain.NewItem{Title: "w1"})
	w2 := h.queueForTurn(t, s, domain.NewItem{Title: "w2"})
	w3 := h.queueForTurn(t, s, domain.NewItem{Title: "w3"})
	blocked := h.at(t, h.submit(t, domain.NewItem{Title: "blocked"}), stReview, domain.StateBlocked)
	left := h.at(t, h.submit(t, domain.NewItem{Title: "left"}), stBuild, domain.StateBlocked)
	part := h.partsOf(t, s, assemblyTitle, 1)[0]

	for item, needs := range map[domain.ItemID]domain.ItemID{w1.ID: blocked.ID, w2.ID: left.ID, w3.ID: part.ID} {
		if _, err := h.store.AddEdge(inOrg(t), domain.NewEdge{Item: item, Needs: needs, Kind: domain.EdgeTurn, By: domain.EngineActor, At: h.clock.Now()}); err != nil {
			t.Fatal(err)
		}
	}

	h.step(t)

	for item, want := range map[domain.ItemID]struct {
		needs  domain.ItemID
		reason string
	}{
		w1.ID: {blocked.ID, "needed item is blocked"},
		w2.ID: {left.ID, "needed item left the station"},
		w3.ID: {part.ID, "needed item has another merge target"},
	} {
		removed := h.eventsOf(item, domain.EventEdgeRemoved)
		if len(removed) != 1 || removed[0].Data["needs"] != string(want.needs) || removed[0].Data["reason"] != want.reason {
			t.Errorf("item %s's edge_removed events %+v, want one on %s for %q", item, removed, want.needs, want.reason)
		}
	}

	if n := h.mergeRuns(w1.ID); n != 1 {
		t.Errorf("w1 ran %d times at merge, want 1: its stale edge gone, it holds the default branch's turn", n)
	}

	h.expectTurnOn(t, w2.ID, w1.ID)
	h.expectTurnOn(t, w3.ID, w1.ID)

	if n := h.mergeRuns(part.ID); n != 1 {
		t.Errorf("the part ran %d times at merge, want 1: it holds its integration branch's turn", n)
	}
}

// A station with merges: allow claims both items at once and writes no turn edge.
func TestEngine_TurnAllowWritesNoEdge(t *testing.T) {
	t.Parallel()

	h, s := turnHarness(t, turnLine(domain.MergesAllow))

	a := h.queueForTurn(t, s, domain.NewItem{Title: "a"})
	b := h.queueForTurn(t, s, domain.NewItem{Title: "b"})

	h.step(t)

	for _, id := range []domain.ItemID{a.ID, b.ID} {
		h.expectPosition(t, id, isDone)

		if got := h.edgesOf(t, id, domain.EdgeTurn); len(got) != 0 {
			t.Errorf("item %s's turn edges = %+v, want none", id, got)
		}
	}
}
