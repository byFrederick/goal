package storetest

import (
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// A run still running after its lease ended (a person took the item while the instance running
// it was dead) is finished as cancelled once the lease ended before the cutoff, with a
// run_finished event; a run under a live lease is not, even when the same item holds a newer
// lease at the station, and a finished run is not finished again.
func testFinishLostRuns(f *fixture) {
	t := f.t
	t2, t3, t4 := t1.Add(2*time.Minute), t1.Add(3*time.Minute), t1.Add(10*time.Minute)

	lost := f.create(t, domain.NewItem{Title: "lost"})
	held := f.create(t, domain.NewItem{Title: "held", CreatedAt: t0.Add(time.Second)})

	f.claim(t, work, 0, t1)
	f.claim(t, work, 1, t1)
	lostRun := f.startRun(t, lost.ID, t1)
	heldRun := f.startRun(t, held.ID, t1)

	f.move(t, ports.Transition{
		ItemID: lost.ID, From: domain.Position{Station: stWork, State: domain.StateInProgress},
		To: domain.Position{Station: stWork, State: domain.StateQueued}, TakeLease: true, Outcome: domain.LeaseTaken, At: t2,
	})

	if got := f.finishLostRuns(t, t2, t4); len(got) != 0 {
		t.Errorf("FinishLostRuns before the grace = %+v; want nothing (the lease ended at the cutoff)", got)
	}

	// The item is claimed again at the same station: the new lease is not the lost run's.
	f.claim(t, work, 0, t3)
	newRun := f.startRun(t, lost.ID, t3)

	got := f.finishLostRuns(t, t2.Add(time.Second), t4)

	want := domain.Run{
		ID: lostRun, FactoryID: f.factory, ItemID: lost.ID, Station: stWork, Agent: agWorker, Model: model,
		Result: domain.RunResult{Status: domain.RunCancelled, Error: domain.RunLostError}, StartedAt: t1, EndedAt: t4,
	}
	sameRuns(t, got, []domain.Run{want})

	running := domain.RunResult{Status: domain.RunRunning}
	sameRuns(t, f.runs(t, lost.ID), []domain.Run{want, {
		ID: newRun, FactoryID: f.factory, ItemID: lost.ID, Station: stWork, Agent: agWorker, Model: model, Result: running, StartedAt: t3,
	}})
	sameRuns(t, f.runs(t, held.ID), []domain.Run{{
		ID: heldRun, FactoryID: f.factory, ItemID: held.ID, Station: stWork, Agent: agWorker, Model: model, Result: running, StartedAt: t1,
	}})

	var finished []domain.Event

	for _, e := range f.Events(t, lost.ID) {
		if e.Kind == domain.EventRunFinished {
			finished = append(finished, e)
		}
	}

	sameEvents(t, finished, []domain.Event{{
		FactoryID: f.factory, ItemID: lost.ID, RunID: lostRun, At: t4, Kind: domain.EventRunFinished, Station: stWork,
		Actor: agWorker, Data: map[string]any{"status": "cancelled", "error": domain.RunLostError, "cost_usd": 0, "turns": 0},
	}})

	if got := f.finishLostRuns(t, t4, t4.Add(time.Minute)); len(got) != 0 {
		t.Errorf("FinishLostRuns again = %+v; want nothing", got)
	}
}

// FinishLostRuns finishes at most limit runs, those whose lease ended first.
func testFinishLostRunsLimit(f *fixture) {
	t := f.t
	t4 := t1.Add(10 * time.Minute)

	var runs []domain.RunID

	for i := range 2 {
		it := f.create(t, domain.NewItem{CreatedAt: t0.Add(time.Duration(i) * time.Second)})
		f.claim(t, work, i, t1)
		runs = append(runs, f.startRun(t, it.ID, t1))
		f.move(t, ports.Transition{
			ItemID: it.ID, From: domain.Position{Station: stWork, State: domain.StateInProgress},
			To: domain.Position{Station: stWork, State: domain.StateBlocked}, TakeLease: true, Outcome: domain.LeaseTaken,
			At: t1.Add(time.Duration(2-i) * time.Minute), // the second item's lease ended first
		})
	}

	got, err := f.Store.FinishLostRuns(f.ctx(t), t4, t4, 1)
	if err != nil || len(got) != 1 || got[0].ID != runs[1] {
		t.Fatalf("FinishLostRuns limit 1 = %+v, %v; want the run whose lease ended first (%s)", got, err, runs[1])
	}

	if got := f.finishLostRuns(t, t4, t4); len(got) != 1 || got[0].ID != runs[0] {
		t.Errorf("FinishLostRuns next = %+v; want %s", got, runs[0])
	}
}

// Another organization's lost runs are left to its own sweep.
func testFinishLostRunsOneOrg(f *fixture) {
	t := f.t
	other := f.otherOrg(t)
	t4 := t1.Add(10 * time.Minute)

	theirs := other.create(t, domain.NewItem{})
	other.claim(t, work, 0, t1)
	run := other.startRun(t, theirs.ID, t1)
	other.move(t, ports.Transition{
		ItemID: theirs.ID, From: domain.Position{Station: stWork, State: domain.StateInProgress},
		To: domain.Position{Station: stWork, State: domain.StateBlocked}, TakeLease: true, Outcome: domain.LeaseTaken, At: t1,
	})

	if got := f.finishLostRuns(t, t4, t4); len(got) != 0 {
		t.Errorf("FinishLostRuns = %+v; want nothing of the other org", got)
	}

	if got := other.finishLostRuns(t, t4, t4); len(got) != 1 || got[0].ID != run {
		t.Errorf("the other org's FinishLostRuns = %+v; want its run %s", got, run)
	}
}

func (f *fixture) finishLostRuns(t *testing.T, endedBefore, now time.Time) []domain.Run {
	t.Helper()

	got, err := f.Store.FinishLostRuns(f.ctx(t), endedBefore, now, 100)
	if err != nil {
		t.Fatalf("FinishLostRuns: %v", err)
	}

	return got
}
