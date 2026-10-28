package storetest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// A transition that releases a lease records it with the outcome it gives and the item's rework
// count while the lease was held; a refused transition records nothing; a lease released without
// an outcome is refused and nothing changes.
func testLeaseSpansOnTransition(f *fixture) {
	t := f.t
	t2, t3, t4 := t1.Add(2*time.Minute), t1.Add(3*time.Minute), t1.Add(5*time.Minute)

	it := f.create(t, domain.NewItem{})
	_, l := f.claim(t, work, 1, t1)

	missing := next(it, l, t2)
	missing.Outcome = ""

	if _, err := f.Store.Transition(f.ctx(t), missing); err == nil {
		t.Fatal("Transition releasing a lease without an outcome succeeded; want it refused")
	}

	stale := next(it, l, t2)
	stale.From.State = domain.StateQueued

	if _, err := f.Store.Transition(f.ctx(t), stale); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("Transition from the wrong position = %v; want ErrConflict", err)
	}

	if ls := f.leases(t, stWork); len(ls) != 1 || f.item(t, it.ID).State != domain.StateInProgress {
		t.Fatalf("after refused transitions: leases %+v, item %s; want the lease held, in progress", ls, f.item(t, it.ID).State)
	}

	rejected := next(it, l, t2) // the gate at work sends it on for rework: round 0 → 1
	rejected.Outcome, rejected.Rework = domain.LeaseRejected, true
	f.move(t, rejected)

	nextSt := domain.Station{ID: stNext, Kind: domain.KindAgent, Agent: agWorker, Agents: 1}
	_, l = f.claim(t, nextSt, 0, t3)
	f.move(t, ports.Transition{
		ItemID: it.ID, From: domain.Position{Station: stNext, State: domain.StateInProgress},
		To: domain.Position{Station: stNext, State: domain.StateBlocked}, Lease: &l, Outcome: domain.LeaseBlocked, At: t4,
	})

	sameLeaseSpans(t, f.LeaseSpans(t, f.org, it.ID), []domain.LeaseSpan{
		{ItemID: it.ID, Station: stWork, Agent: agWorker, Slot: 1, AcquiredAt: t1, EndedAt: t2, Outcome: domain.LeaseRejected},
		{ItemID: it.ID, Station: stNext, Agent: agWorker, Slot: 0, ReworkRound: 1, AcquiredAt: t3, EndedAt: t4, Outcome: domain.LeaseBlocked},
	})
}

// Requeuing an expired lease records it as expired, ended when it was requeued.
func testLeaseSpansOnRequeue(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	f.claim(t, work, 0, t1)

	now := t1.Add(ttl + 30*time.Second)
	if _, err := f.Store.RequeueExpiredLeases(f.ctx(t), now, 10); err != nil {
		t.Fatal(err)
	}

	sameLeaseSpans(t, f.LeaseSpans(t, f.org, it.ID), []domain.LeaseSpan{
		{ItemID: it.ID, Station: stWork, Agent: agWorker, AcquiredAt: t1, EndedAt: now, Outcome: domain.LeaseExpired},
	})

	if spans := f.LeaseSpans(t, f.NewOrg(t), it.ID); len(spans) != 0 {
		t.Errorf("another organization sees lease spans %+v; want none", spans)
	}
}

// A person's transition takes the item's lease whatever its AcquiredAt, recording it as taken;
// it is refused like any transition when the item is not where From says, and needs an outcome.
// Taking from an item without a lease records nothing.
func testTakeLease(f *fixture) {
	t := f.t
	t2, t3 := t1.Add(2*time.Minute), t1.Add(3*time.Minute)

	it := f.create(t, domain.NewItem{})
	f.claim(t, work, 1, t1)

	take := ports.Transition{
		ItemID: it.ID, From: domain.Position{Station: stWork, State: domain.StateInProgress},
		To: domain.Position{Station: stWork, State: domain.StateBlocked}, TakeLease: true, Outcome: domain.LeaseTaken, At: t2,
	}

	noOutcome := take
	noOutcome.Outcome = ""

	if _, err := f.Store.Transition(f.ctx(t), noOutcome); err == nil {
		t.Fatal("Transition taking a lease without an outcome succeeded; want it refused")
	}

	stale := take
	stale.From.State = domain.StateQueued

	if _, err := f.Store.Transition(f.ctx(t), stale); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("taking from the wrong position = %v; want ErrConflict", err)
	}

	if ls := f.leases(t, stWork); len(ls) != 1 {
		t.Fatalf("after refused takes: leases %+v; want the lease still held", ls)
	}

	if got := f.move(t, take); got.Position() != take.To {
		t.Fatalf("item at %+v after the take; want %+v", got.Position(), take.To)
	}

	if ls := f.leases(t, stWork); len(ls) != 0 {
		t.Errorf("leases %+v after the take; want none", ls)
	}

	f.move(t, ports.Transition{ // nothing to take: a blocked item holds no lease
		ItemID: it.ID, From: take.To, To: domain.Position{Station: stWork, State: domain.StateQueued},
		TakeLease: true, Outcome: domain.LeaseTaken, At: t3,
	})

	sameLeaseSpans(t, f.LeaseSpans(t, f.org, it.ID), []domain.LeaseSpan{
		{ItemID: it.ID, Station: stWork, Agent: agWorker, Slot: 1, AcquiredAt: t1, EndedAt: t2, Outcome: domain.LeaseTaken},
	})
}

// An agent's question (ADR 0018) or Ask-first tool call (ADR 0031) takes its own lease: the span
// ends with the outcome asked or awaiting_approval, which the store must accept, and the item
// waits for a person at its station.
func testTakeLeaseAsked(f *fixture) {
	for _, outcome := range []domain.LeaseOutcome{domain.LeaseAsked, domain.LeaseAwaitingApproval} {
		t := f.t
		t2 := t1.Add(2 * time.Minute)

		it := f.create(t, domain.NewItem{})
		f.claim(t, work, 1, t1)

		ask := ports.Transition{
			ItemID: it.ID, From: domain.Position{Station: stWork, State: domain.StateInProgress},
			To: domain.Position{Station: stWork, State: domain.StateWaitingHuman}, TakeLease: true, Outcome: outcome, At: t2,
		}

		if got := f.move(t, ask); got.Position() != ask.To {
			t.Fatalf("%s: item at %+v after the ask; want %+v", outcome, got.Position(), ask.To)
		}

		sameLeaseSpans(t, f.LeaseSpans(t, f.org, it.ID), []domain.LeaseSpan{
			{ItemID: it.ID, Station: stWork, Agent: agWorker, Slot: 1, AcquiredAt: t1, EndedAt: t2, Outcome: outcome},
		})
	}
}

func sameLeaseSpans(t *testing.T, got, want []domain.LeaseSpan) {
	t.Helper()

	for i := range got {
		got[i].AcquiredAt, got[i].EndedAt = got[i].AcquiredAt.UTC(), got[i].EndedAt.UTC()
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("lease spans =\n  %+v\nwant\n  %+v", got, want)
	}
}
