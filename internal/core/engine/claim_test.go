package engine_test

import (
	"errors"
	"maps"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const otherFactory domain.FactoryID = "5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01"

// twoOrgs is a harness whose runs do run, with a second organization that owns otherFactory;
// both factories have one agent station with five slots.
func twoOrgs(t *testing.T, run porttest.AgentFunc) (*harness, domain.Line, domain.OrgID) {
	t.Helper()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 5, 5)), run)
	org := h.newOrg(t, "example-labs")

	other := lineOf(agentStation(stWork, agWorker, 5, 5))
	other.OrgID, other.FactoryID = org, otherFactory

	return h, other, org
}

// submitTo submits n items to a factory of org.
func (h *harness) submitTo(t *testing.T, e *engine.Engine, org domain.OrgID, factory domain.FactoryID, n int) {
	t.Helper()

	for range n {
		if _, err := e.Submit(domain.WithOrg(t.Context(), org), domain.FileRequest{Origin: domain.OriginPerson, FactoryID: factory, Title: anItem, Type: typeFeature}); err != nil {
			t.Fatal(err)
		}
	}
}

// startedBy waits until n runs started and counts them per factory.
func (h *harness) startedBy(t *testing.T, n int) map[domain.FactoryID]int {
	t.Helper()

	if err := h.worker.WaitRuns(bounded(t), n); err != nil {
		t.Fatal(err)
	}

	by := map[domain.FactoryID]int{}
	for _, run := range h.worker.Runs() {
		by[run.Sandbox.FactoryID]++
	}

	return by
}

// held counts the leases at the work station of a factory of org: the runs claimed.
func (h *harness) held(t *testing.T, org domain.OrgID, factory domain.FactoryID) int {
	t.Helper()

	leases, err := h.store.Leases(domain.WithOrg(t.Context(), org), factory, stWork)
	if err != nil {
		t.Fatal(err)
	}

	return len(leases)
}

func TestTick_QuietOrgStartsBeforeABusyOrgFillsTheInstance(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h, other, org := twoOrgs(t, porttest.Block(release, `{}`))
	e := h.engineWith(t, h.store, 2, h.line, other)

	h.submitTo(t, e, testOrg, factoryID, 5)
	h.submitTo(t, e, org, otherFactory, 1)

	for range 2 { // the cap holds across ticks while the runs last
		if err := e.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	if a, b := h.held(t, testOrg, factoryID), h.held(t, org, otherFactory); a != 1 || b != 1 {
		t.Errorf("claimed %d and %d, want one item of each organization within MaxRuns 2", a, b)
	}

	if got := h.startedBy(t, 2); got[factoryID] != 1 || got[otherFactory] != 1 {
		t.Errorf("started %v, want one run of each organization", got)
	}

	close(release)
	h.wait(t, e)
}

func TestTick_WithoutACapEveryOrgFillsItsOwnSlots(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h, other, org := twoOrgs(t, porttest.Block(release, `{}`))
	e := h.engineWith(t, h.store, 0, h.line, other)

	h.submitTo(t, e, testOrg, factoryID, 7)
	h.submitTo(t, e, org, otherFactory, 2)

	if err := e.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}

	if a, b := h.held(t, testOrg, factoryID), h.held(t, org, otherFactory); a != 5 || b != 2 {
		t.Errorf("claimed %d and %d, want 5 (every slot) and 2 (every item)", a, b)
	}

	if got := h.startedBy(t, 7); got[factoryID] != 5 || got[otherFactory] != 2 {
		t.Errorf("started %v, want 5 and 2", got)
	}

	close(release)
	h.wait(t, e)
}

func TestTick_TakesTheOrgOfEachLineNotOfItsContext(t *testing.T) {
	t.Parallel()

	h, other, org := twoOrgs(t, porttest.Succeed(`{}`))
	e := h.engineWith(t, h.store, 0, h.line, other)

	h.submitTo(t, e, testOrg, factoryID, 1)
	h.submitTo(t, e, org, otherFactory, 1)

	// The fake store answers only within the org of each call, as row-level security does.
	if err := e.Tick(domain.WithOrg(t.Context(), org)); err != nil {
		t.Fatal(err)
	}

	h.wait(t, e)

	for _, f := range []struct {
		org     domain.OrgID
		factory domain.FactoryID
	}{{testOrg, factoryID}, {org, otherFactory}} {
		items, err := h.store.ListItems(domain.WithOrg(t.Context(), f.org), ports.ItemQuery{FactoryID: f.factory, Limit: 10})
		if err != nil || len(items) != 1 || items[0].State != domain.StateDone {
			t.Errorf("items of %s = %v (%v), want one done", f.factory, items, err)
		}

		for _, it := range items {
			if runs := h.runs(t, f.org, it.ID); len(runs) != 1 || runs[0].Result.Status != domain.RunSucceeded {
				t.Errorf("runs of %s = %v, want one that succeeded", it.ID, runs)
			}
		}
	}
}

func TestSweep_CoversEveryActiveOrg(t *testing.T) {
	t.Parallel()

	h, other, org := twoOrgs(t, porttest.Succeed(`{}`))
	e := h.engineWith(t, h.store, 0, h.line) // otherFactory is no longer loaded; its lease is swept anyway

	h.claimAsDeadInstance(t, h.submit(t, domain.NewItem{}), stWork)

	it, err := h.engineWith(t, h.store, 0, other).Submit(domain.WithOrg(t.Context(), org),
		domain.FileRequest{Origin: domain.OriginPerson, FactoryID: otherFactory, Title: anItem, Type: typeFeature})
	if err != nil {
		t.Fatal(err)
	}

	h.claimDead(t, other, it, stWork)

	h.clock.Advance(ttl)

	if n, err := e.SweepLeases(t.Context()); err != nil || n != 2 {
		t.Errorf("sweep requeued %d (%v), want the expired lease of each organization", n, err)
	}
}

func TestSubmit_IntoAFactoryOfAnotherOrgIsNotFound(t *testing.T) {
	t.Parallel()

	h, _, org := twoOrgs(t, porttest.Succeed(`{}`))

	_, err := h.e.Submit(domain.WithOrg(t.Context(), org), domain.FileRequest{Origin: domain.OriginPerson, FactoryID: factoryID, Title: "x", Type: typeFeature})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Submit from another organization = %v, want ErrNotFound", err)
	}

	if _, err := h.e.Submit(t.Context(), domain.FileRequest{Origin: domain.OriginPerson, FactoryID: factoryID, Title: "x", Type: typeFeature}); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("Submit without an organization = %v, want ErrNoOrg", err)
	}
}

func TestDecisions_OnAnItemOfAnotherOrgAreNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(domain.Station{ID: stApprove, Kind: domain.KindHuman}), porttest.Succeed(`{}`))
	org := h.newOrg(t, "example-labs")
	it := h.submit(t, domain.NewItem{})
	h.step(t)

	ctx := domain.WithOrg(t.Context(), org)

	if _, err := h.e.Approve(ctx, it.ID, human); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Approve from another organization = %v, want ErrNotFound", err)
	}

	if _, err := h.e.Retry(ctx, it.ID, human); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Retry from another organization = %v, want ErrNotFound", err)
	}

	if got := h.item(t, it.ID); got.State != domain.StateWaitingHuman {
		t.Errorf("item is %s, want still waiting_human", got.State)
	}
}

func TestEngine_ClaimWaitsForNeeds(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 2, 2)), porttest.Succeed(`{}`))

	part := h.submit(t, domain.NewItem{Title: "part"})
	need := h.submit(t, domain.NewItem{Title: "need"})

	if _, err := h.store.AddEdge(inOrg(t), domain.NewEdge{
		Item: part.ID, Needs: need.ID, Kind: domain.EdgePlanned, By: domain.LocalUser, At: t0,
	}); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if got := h.item(t, need.ID); got.State != domain.StateDone {
		t.Fatalf("need = %s at %s; want done", got.State, got.Station)
	}

	// The inbox holds it too, so it has not started (ADR 0040 decision 4).
	if got := h.item(t, part.ID); got.Station != stInbox || got.State != domain.StateQueued {
		t.Fatalf("part = %s at %s; want queued at %s while its need is open", got.State, got.Station, stInbox)
	}

	if runs := h.runs(t, testOrg, part.ID); len(runs) != 0 {
		t.Fatalf("part ran %d times before its need was done", len(runs))
	}

	h.step(t)

	if got := h.item(t, part.ID); got.State != domain.StateDone {
		t.Errorf("part = %s at %s on the tick after its need is done; want done", got.State, got.Station)
	}
}

// A lane whose route entry keeps slots uses only those, and other lanes never take them (ADR
// 0012 decision 9): backend keeps 1 of 3 slots, so 2 frontend items take the shared slots, 1
// backend item the kept one, and the second backend item waits though slot 0 is the only one
// a frontend item could otherwise have filled.
func TestEngine_ClaimKeepsSlotsForALane(t *testing.T) {
	t.Parallel()

	build := domain.Station{
		ID: stWork, Kind: domain.KindAgent, Agents: 3,
		Route:       map[string]string{laneBack: agBackend, laneFront: agFrontend},
		RouteAgents: map[string]int{laneBack: 1},
	}

	release := make(chan struct{})
	h := newHarness(t, lineOf(build), porttest.Block(release, `{}`))

	t.Cleanup(func() {
		close(release)
		h.wait(t, h.e)
	})

	// The backend items go first in pull order, so slots that ignored lanes would take them both.
	lane := map[domain.ItemID]string{}

	for i, l := range []string{laneBack, laneBack, laneFront, laneFront, laneFront, laneFront} {
		it := h.submit(t, domain.NewItem{Title: l, Lane: l, Priority: 6 - i})
		lane[it.ID] = l
	}

	h.tick(t)

	if err := h.worker.WaitRuns(bounded(t), 3); err != nil {
		t.Fatal(err)
	}

	leases, err := h.store.Leases(inOrg(t), factoryID, stWork)
	if err != nil {
		t.Fatal(err)
	}

	got := map[int]string{}

	for _, l := range leases {
		got[l.Slot] = lane[l.ItemID]
	}

	want := map[int]string{0: laneBack, 1: laneFront, 2: laneFront}
	if !maps.Equal(got, want) {
		t.Fatalf("slots hold lanes %v; want %v", got, want)
	}

	waiting, err := h.store.Waiting(inOrg(t), factoryID, stWork, 10)
	if err != nil {
		t.Fatal(err)
	}

	var backend int

	for _, it := range waiting {
		if it.Lane == laneBack {
			backend++
		}
	}

	if backend != 1 {
		t.Errorf("%d backend items wait at %s; want 1", backend, stWork)
	}
}
