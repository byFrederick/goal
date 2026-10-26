package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// leaveSandbox puts a sandbox of run, labelled with org, on the harness's backend, as a goald
// that died would leave it.
func (h *harness) leaveSandbox(t *testing.T, run domain.RunID, org domain.OrgID) {
	t.Helper()

	if _, err := h.sandboxes.Create(t.Context(), ports.SandboxSpec{OrgID: org, RunID: run, Image: agentImage}); err != nil {
		t.Fatal(err)
	}
}

// startDeadRun claims an item of line l as an instance that then dies would, records its run and
// leaves the run's sandbox.
func (h *harness) startDeadRun(t *testing.T, l domain.Line, it domain.Item) domain.RunID {
	t.Helper()

	h.claimDead(t, l, it, stWork)

	run, err := h.store.StartRun(domain.WithOrg(t.Context(), l.OrgID), ports.NewRun{
		FactoryID: l.FactoryID, ItemID: it.ID, Station: stWork, Agent: agWorker, StartedAt: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	h.leaveSandbox(t, run, l.OrgID)

	return run
}

// Two goald instances share one sandbox backend: a reap by either, at its start or later, leaves
// the other's run's sandbox alone, because the run holds its lease.
func TestReapOrphans_LeavesTheRunsOfAnotherLiveInstance(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Block(release, `{}`))
	h.submit(t, domain.NewItem{})

	if err := h.e.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	box := h.worker.Runs()[0].Sandbox
	if box.OrgID != testOrg {
		t.Errorf("sandbox organization = %q; want %q, the label the reaper asks", box.OrgID, testOrg)
	}

	restarted := h.engine(t, h.store) // a second goald starts next to the first

	for i, e := range []*engine.Engine{restarted, h.e} {
		if n, err := e.ReapOrphans(t.Context()); err != nil || n != 0 {
			t.Errorf("instance %d reaped %d runs (%v); want none", i+1, n, err)
		}
	}

	if !h.sandboxes.Has(box.RunID) {
		t.Fatal("the live run's sandbox was reaped")
	}

	close(release)
	h.wait(t, h.e)

	if got := h.item(t, box.ItemID); got.State != domain.StateDone {
		t.Errorf("item is %s/%s; want done: the run was not disturbed", got.Station, got.State)
	}
}

// A dead instance's run keeps its sandbox while its lease is live (the instance may be slow, not
// dead); once the lease expired and was swept, any instance reaps it.
func TestReapOrphans_ReapsADeadInstancesRunOnceItsLeaseIsSwept(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
	run := h.startDeadRun(t, h.line, h.submit(t, domain.NewItem{}))

	if n, err := h.e.ReapOrphans(t.Context()); err != nil || n != 0 || !h.sandboxes.Has(run) {
		t.Fatalf("reap under a live lease = %d (%v), sandbox kept %t; want 0, kept", n, err, h.sandboxes.Has(run))
	}

	h.clock.Advance(ttl)

	if n, err := h.e.SweepLeases(inOrg(t)); err != nil || n != 1 {
		t.Fatalf("sweep = %d (%v); want the expired lease requeued", n, err)
	}

	if n, err := h.e.ReapOrphans(t.Context()); err != nil || n != 1 {
		t.Errorf("reap after the sweep = %d (%v); want the dead run reaped", n, err)
	}

	if h.sandboxes.Has(run) {
		t.Error("the dead run's sandbox is still there")
	}
}

// A sandbox is looked up in the organization it is labelled with: another organization's dead
// run keeps its sandbox while its lease is live and loses it once swept.
func TestReapOrphans_AsksTheSandboxsOrganization(t *testing.T) {
	t.Parallel()

	h, other, org := twoOrgs(t, porttest.Succeed(`{}`))
	e := h.engineWith(t, h.store, 0, h.line, other)

	theirs, err := e.Submit(domain.WithOrg(t.Context(), org), domain.NewItem{FactoryID: otherFactory, Title: anItem, Type: typeFeature})
	if err != nil {
		t.Fatal(err)
	}

	run := h.startDeadRun(t, other, theirs)

	if n, err := e.ReapOrphans(t.Context()); err != nil || n != 0 || !h.sandboxes.Has(run) {
		t.Fatalf("reap under a live lease = %d (%v), sandbox kept %t; want 0, kept", n, err, h.sandboxes.Has(run))
	}

	h.clock.Advance(ttl)

	if n, err := e.SweepLeases(t.Context()); err != nil || n != 1 {
		t.Fatalf("sweep = %d (%v); want the other organization's expired lease requeued", n, err)
	}

	if n, err := e.ReapOrphans(t.Context()); err != nil || n != 1 || h.sandboxes.Has(run) {
		t.Errorf("reap after the sweep = %d (%v), sandbox kept %t; want 1, removed", n, err, h.sandboxes.Has(run))
	}
}

// Another deployment may share the sandbox backend: a sandbox of a run the store does not know
// stays, labelled with an active organization, with one the store does not have, or with none.
func TestReapOrphans_LeavesRunsTheStoreDoesNotKnow(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))

	strangers := map[domain.RunID]domain.OrgID{
		"00000000-0000-7000-8000-00000000000a": testOrg,
		"00000000-0000-7000-8000-00000000000b": "00000000-0000-7000-8000-0000000000ff",
		"00000000-0000-7000-8000-00000000000c": "",
	}
	for run, org := range strangers {
		h.leaveSandbox(t, run, org)
	}

	if n, err := h.e.ReapOrphans(t.Context()); err != nil || n != 0 {
		t.Errorf("reap = %d (%v); want none", n, err)
	}

	for run := range strangers {
		if !h.sandboxes.Has(run) {
			t.Errorf("the sandbox of run %s, which the store does not know, was reaped", run)
		}
	}
}

// overRun records a run that is over (failed, never under a lease) of an item no line claims, and
// leaves its sandbox.
func (h *harness) overRun(t *testing.T) domain.RunID {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: factoryID, Station: "elsewhere", State: domain.StateQueued, Class: domain.ClassStandard, Type: typeFeature,
		Title: anItem, CreatedAt: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{FactoryID: factoryID, ItemID: it.ID, Station: "elsewhere", Agent: agWorker, StartedAt: h.clock.Now()})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(inOrg(t), run, domain.RunResult{Status: domain.RunFailed}, h.clock.Now()); err != nil {
		t.Fatal(err)
	}

	h.leaveSandbox(t, run, testOrg)

	return run
}

// failingRunsOver is a store that cannot tell which runs are over.
type failingRunsOver struct{ ports.Store }

var errStoreDown = errors.New("store down")

func (failingRunsOver) RunsOver(context.Context, []domain.RunID) ([]domain.RunID, error) {
	return nil, errStoreDown
}

// When the store cannot say whether a run is over, its sandbox stays.
func TestReapOrphans_KeepsSandboxesWhenTheStoreCannotTell(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
	e := h.engine(t, failingRunsOver{h.store})
	run := h.overRun(t)

	if n, err := e.ReapOrphans(t.Context()); !errors.Is(err, errStoreDown) || n != 0 {
		t.Errorf("reap = %d (%v); want 0 and the store's error", n, err)
	}

	if !h.sandboxes.Has(run) {
		t.Error("a sandbox was reaped although the store could not tell whether its run is over")
	}
}

// Run reaps when it starts and again every ReapEvery.
func TestRun_ReapsAtStartAndEveryReapEvery(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
	first := h.overRun(t)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)

	go func() { done <- h.e.Run(ctx) }()

	if err := h.sandboxes.Await(bounded(t), first, false); err != nil {
		t.Fatalf("the orphan left before the start was not reaped: %v", err)
	}

	if err := h.clock.WaitTickers(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	second := h.overRun(t)
	h.clock.Advance(reapEvery)

	if err := h.sandboxes.Await(bounded(t), second, false); err != nil {
		t.Errorf("the orphan left later was not reaped after ReapEvery: %v", err)
	}

	cancel()

	if err := <-done; err != nil {
		t.Errorf("Run returned %v after cancel, want nil", err)
	}
}
