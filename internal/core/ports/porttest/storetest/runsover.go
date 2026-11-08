package storetest

import (
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// A run is over once it no longer runs and no longer holds its lease (its checks run under the
// lease after the agent task ended): released and finished, taken and finished as lost, expired
// and swept. A newer lease of the item is not the run's, except for a succeeded run that is still
// the item's latest (testRunsOverResumed). Ids no run of the organization has, another
// organization's runs included, are never over.
func testRunsOver(f *fixture) {
	t := f.t
	st := domain.Station{ID: stWork, Kind: domain.KindAgent, Agent: agWorker, Agents: 4}
	t2, t3 := t1.Add(time.Minute), t1.Add(10*time.Minute)
	inProgress := domain.Position{Station: stWork, State: domain.StateInProgress}

	start := func(slot int) (domain.Item, domain.Lease, domain.RunID) {
		f.create(t, domain.NewItem{CreatedAt: t0.Add(time.Duration(slot) * time.Second)})
		it, l := f.claim(t, st, slot, t1)

		return it, l, f.startRun(t, it.ID, t1)
	}

	finish := func(run domain.RunID, at time.Time) {
		if err := f.Store.FinishRun(f.ctx(t), run, domain.RunResult{Status: domain.RunSucceeded}, at); err != nil {
			t.Fatalf("FinishRun: %v", err)
		}
	}

	_, checkingLease, checking := start(0)
	finish(checking, t2)

	released, releasedLease, releasedRun := start(1)
	f.move(t, ports.Transition{
		ItemID: released.ID, From: inProgress, To: domain.Position{Station: stNext, State: domain.StateQueued},
		Lease: &releasedLease, Outcome: domain.LeaseAdvanced, At: t2,
	})

	taken, _, takenRun := start(2)
	f.move(t, ports.Transition{
		ItemID: taken.ID, From: inProgress, To: domain.Position{Station: stWork, State: domain.StateBlocked},
		TakeLease: true, Outcome: domain.LeaseTaken, At: t2,
	})

	_, _, expiredRun := start(3)

	all := []domain.RunID{checking, releasedRun, takenRun, expiredRun, missingRun, "not-a-run"}

	f.expectOver(t, "with every lease held or every run running", all)

	finish(releasedRun, t2)

	if _, err := f.Store.FinishLostRuns(f.ctx(t), t2.Add(time.Second), t3, 100); err != nil {
		t.Fatalf("FinishLostRuns: %v", err)
	}

	// The checking item heartbeats; the other lease expires and is swept.
	if _, err := f.Store.HeartbeatLease(f.ctx(t), checkingLease, t2, t3.Add(time.Hour)); err != nil {
		t.Fatalf("HeartbeatLease: %v", err)
	}

	if _, err := f.Store.RequeueExpiredLeases(f.ctx(t), t3, 100); err != nil {
		t.Fatalf("RequeueExpiredLeases: %v", err)
	}

	f.expectOver(t, "after release and finish, loss and sweep", all, releasedRun, takenRun, expiredRun)

	// The expired item is claimed again: the new lease is not the swept run's.
	f.claim(t, st, 3, t3)
	f.expectOver(t, "after a new claim of the swept item", all, releasedRun, takenRun, expiredRun)

	testRunsOverResumed(f, st, t3)

	other := f.otherOrg(t)
	theirs := other.create(t, domain.NewItem{})
	_, theirLease := other.claim(t, work, 0, t1)
	theirRun := other.startRun(t, theirs.ID, t1)

	other.move(t, ports.Transition{
		ItemID: theirs.ID, From: inProgress, To: domain.Position{Station: stNext, State: domain.StateQueued},
		Lease: &theirLease, Outcome: domain.LeaseAdvanced, At: t2,
	})

	if err := other.Store.FinishRun(other.ctx(t), theirRun, domain.RunResult{Status: domain.RunSucceeded}, t2); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	f.expectOver(t, "another organization's run", []domain.RunID{theirRun})
	other.expectOver(t, "its own organization's run", []domain.RunID{theirRun}, theirRun)
}

// A succeeded run whose claim was released (a shutdown during its checks) is not over while a
// newer lease holds its item at its station and it is still the item's latest run: that claim
// runs its checks again in a box named after it. Once the lease ends, or a newer run of the item
// starts, it is over.
func testRunsOverResumed(f *fixture, st domain.Station, at time.Time) {
	t := f.t
	queued := domain.Position{Station: stWork, State: domain.StateQueued}
	inProgress := domain.Position{Station: stWork, State: domain.StateInProgress}

	f.create(t, domain.NewItem{CreatedAt: t0.Add(time.Minute)})
	it, l := f.claim(t, st, 1, at)
	run := f.startRun(t, it.ID, at)

	if err := f.Store.FinishRun(f.ctx(t), run, domain.RunResult{Status: domain.RunSucceeded}, at.Add(time.Minute)); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	release := func(l domain.Lease, when time.Time) {
		f.move(t, ports.Transition{ItemID: it.ID, From: inProgress, To: queued, Lease: &l, Outcome: domain.LeaseReleased, At: when})
	}

	release(l, at.Add(2*time.Minute))
	f.expectOver(t, "after its claim was released", []domain.RunID{run}, run)

	_, l = f.claim(t, st, 1, at.Add(3*time.Minute))
	f.expectOver(t, "while a newer claim holds the item and the run is its latest", []domain.RunID{run})

	release(l, at.Add(4*time.Minute))
	f.expectOver(t, "once that claim was released too", []domain.RunID{run}, run)

	f.claim(t, st, 1, at.Add(5*time.Minute))
	f.expectOver(t, "when claimed again", []domain.RunID{run})

	f.startRun(t, it.ID, at.Add(6*time.Minute))
	f.expectOver(t, "once a newer run of the item started", []domain.RunID{run}, run)
}

// expectOver fails the test unless RunsOver(of) returns want, in any order.
func (f *fixture) expectOver(t *testing.T, when string, of []domain.RunID, want ...domain.RunID) {
	t.Helper()

	got, err := f.Store.RunsOver(f.ctx(t), of)
	if err != nil {
		t.Fatalf("RunsOver %s: %v", when, err)
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf("RunsOver %s = %v; want %v", when, got, want)
	}
}
