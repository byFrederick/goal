package storetest

import (
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Stations of the span history beyond work and next.
const (
	stReview = "review"
	stDone   = "done"
)

// at is t0 plus n minutes.
func at(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }

// spans reads an item's spans through FlowStats.
func (f *fixture) spans(t *testing.T, id domain.ItemID) []domain.Span {
	t.Helper()

	spans, err := f.Flow.ItemSpans(f.ctx(t), id)
	if err != nil {
		t.Fatalf("ItemSpans: %v", err)
	}

	return spans
}

func (f *fixture) move(t *testing.T, tr ports.Transition) domain.Item {
	t.Helper()

	it, err := f.Store.Transition(f.ctx(t), tr)
	if err != nil {
		t.Fatalf("Transition %s → %s %s: %v", tr.From.Station, tr.To.Station, tr.To.State, err)
	}

	return it
}

// Every change of station, state or rework round closes the open span at the change's time and
// opens the next one; a rework round names the station that sent the item back and stays on
// every span of the round; done closes the last span and opens none. The history covers every
// kind of change the engine and people make: create, claim, advance, gate reject, block, retry,
// requeue of an expired lease, escalation to a human, human approve and reject, done.
func testSpansFollowEveryTransition(f *fixture) {
	t := f.t
	nextSt := domain.Station{ID: stNext, Kind: domain.KindAgent, Agent: agWorker, Agents: 1}
	pos := func(station string, state domain.ItemState) domain.Position {
		return domain.Position{Station: station, State: state}
	}

	it := f.create(t, domain.NewItem{CreatedAt: at(0)})
	_, l := f.claim(t, work, 0, at(1))
	f.move(t, next(it, l, at(2)))

	_, l = f.claim(t, nextSt, 0, at(3))
	f.move(t, ports.Transition{ // the gate at next rejects: back to work, round 1
		ItemID: it.ID, From: pos(stNext, domain.StateInProgress), To: pos(stWork, domain.StateQueued),
		Lease: &l, Outcome: domain.LeaseRejected, Rework: true, At: at(4),
	})

	_, l = f.claim(t, work, 0, at(5))
	f.move(t, ports.Transition{ // blocked
		ItemID: it.ID, From: pos(stWork, domain.StateInProgress), To: pos(stWork, domain.StateBlocked), Lease: &l, Outcome: domain.LeaseBlocked, At: at(6),
	})
	f.move(t, ports.Transition{ // a person retries
		ItemID: it.ID, From: pos(stWork, domain.StateBlocked), To: pos(stWork, domain.StateQueued), At: at(7),
	})

	f.claim(t, work, 0, at(8)) // its lease expires at 9 and is requeued at 10

	if _, err := f.Store.RequeueExpiredLeases(f.ctx(t), at(10), 10); err != nil {
		t.Fatal(err)
	}

	_, l = f.claim(t, work, 0, at(11))
	f.move(t, next(it, l, at(12)))
	_, l = f.claim(t, nextSt, 0, at(13))
	f.move(t, ports.Transition{ // rejected past its rework allowance: a person decides at next
		ItemID: it.ID, From: pos(stNext, domain.StateInProgress), To: pos(stNext, domain.StateWaitingHuman), Lease: &l, Outcome: domain.LeaseEscalated, At: at(14),
	})
	f.move(t, ports.Transition{ // approved on to review
		ItemID: it.ID, From: pos(stNext, domain.StateWaitingHuman), To: pos(stReview, domain.StateWaitingHuman), At: at(15),
	})
	f.move(t, ports.Transition{ // review sends it back: round 2, by review
		ItemID: it.ID, From: pos(stReview, domain.StateWaitingHuman), To: pos(stWork, domain.StateQueued), Rework: true, At: at(16),
	})

	open := f.spans(t, it.ID)
	if len(open) == 0 || !open[len(open)-1].LeftAt.IsZero() {
		t.Fatalf("spans of an open item = %+v; want the last one open", open)
	}

	f.move(t, ports.Transition{
		ItemID: it.ID, From: pos(stWork, domain.StateQueued), To: pos(stDone, domain.StateDone), At: at(17), DoneAt: at(17),
	})

	span := func(station string, state domain.ItemState, round int, by string, from, to int) domain.Span {
		return domain.Span{Station: station, State: state, ReworkRound: round, ReworkBy: by, EnteredAt: at(from), LeftAt: at(to)}
	}

	want := []domain.Span{
		span(stWork, domain.StateQueued, 0, "", 0, 1),
		span(stWork, domain.StateInProgress, 0, "", 1, 2),
		span(stNext, domain.StateQueued, 0, "", 2, 3),
		span(stNext, domain.StateInProgress, 0, "", 3, 4),
		span(stWork, domain.StateQueued, 1, stNext, 4, 5),
		span(stWork, domain.StateInProgress, 1, stNext, 5, 6),
		span(stWork, domain.StateBlocked, 1, stNext, 6, 7),
		span(stWork, domain.StateQueued, 1, stNext, 7, 8),
		span(stWork, domain.StateInProgress, 1, stNext, 8, 10),
		span(stWork, domain.StateQueued, 1, stNext, 10, 11),
		span(stWork, domain.StateInProgress, 1, stNext, 11, 12),
		span(stNext, domain.StateQueued, 1, stNext, 12, 13),
		span(stNext, domain.StateInProgress, 1, stNext, 13, 14),
		span(stNext, domain.StateWaitingHuman, 1, stNext, 14, 15),
		span(stReview, domain.StateWaitingHuman, 1, stNext, 15, 16),
		span(stWork, domain.StateQueued, 2, stReview, 16, 17),
	}

	sameSpans(t, f.spans(t, it.ID), want)
	stillOpen := want[len(want)-1]
	stillOpen.LeftAt = time.Time{}
	sameSpans(t, open, append(want[:len(want)-1:len(want)-1], stillOpen))
}

// A transition that leaves station, state and rework round as they are opens no span; spans of
// another organization's item, or of no item, are not there.
func testSpansOnlyOnChangeAndOnlyTheOrgs(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{CreatedAt: at(0)})
	f.move(t, ports.Transition{ // a lane change in place, as an output can make
		ItemID: it.ID, From: it.Position(), To: it.Position(), Lane: laneBackend, At: at(1),
	})

	sameSpans(t, f.spans(t, it.ID), []domain.Span{{Station: stWork, State: domain.StateQueued, EnteredAt: at(0)}})

	other := domain.WithOrg(t.Context(), f.NewOrg(t))
	for _, id := range []domain.ItemID{it.ID, missingItem} {
		if spans, err := f.Flow.ItemSpans(other, id); err != nil || len(spans) != 0 {
			t.Errorf("ItemSpans(%s) in another org = %+v, %v; want none", id, spans, err)
		}
	}
}

// CurrentRun is the run in progress under the item's lease, with its slot and the usage events
// it reported; nothing before the claim or after the lease is released.
func testCurrentRun(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	if run, err := f.Flow.CurrentRun(f.ctx(t), it.ID); err != nil || run != nil {
		t.Fatalf("CurrentRun of a queued item = %+v, %v; want none", run, err)
	}

	_, l := f.claim(t, work, 1, t1)

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: f.factory, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model, StartedAt: t1,
	})
	if err != nil {
		t.Fatal(err)
	}

	usage := domain.Event{FactoryID: f.factory, ItemID: it.ID, RunID: id, At: t1, Kind: domain.EventUsage, Station: stWork, Actor: agWorker}
	tool := usage
	tool.Kind = "tool_call"

	if err := f.Store.AppendEvents(f.ctx(t), usage, tool, usage); err != nil {
		t.Fatal(err)
	}

	got, err := f.Flow.CurrentRun(f.ctx(t), it.ID)
	if err != nil || got == nil {
		t.Fatalf("CurrentRun = %+v, %v; want the run", got, err)
	}

	want := domain.Run{
		ID: id, FactoryID: f.factory, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model,
		Result: domain.RunResult{Status: domain.RunRunning}, StartedAt: t1,
	}
	sameRuns(t, []domain.Run{got.Run}, []domain.Run{want})

	if got.Slot != 1 || got.Turns != 2 {
		t.Errorf("CurrentRun slot %d, turns %d; want slot 1, 2 turns", got.Slot, got.Turns)
	}

	if err := f.Store.FinishRun(f.ctx(t), id, domain.RunResult{Status: domain.RunSucceeded}, t1.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	f.move(t, next(it, l, t1.Add(time.Minute)))

	if run, err := f.Flow.CurrentRun(f.ctx(t), it.ID); err != nil || run != nil {
		t.Errorf("CurrentRun after the run = %+v, %v; want none", run, err)
	}
}

func sameSpans(t *testing.T, got, want []domain.Span) {
	t.Helper()

	norm := func(spans []domain.Span) []domain.Span {
		out := make([]domain.Span, 0, len(spans))
		for _, s := range spans {
			s.EnteredAt, s.LeftAt = s.EnteredAt.UTC(), s.LeftAt.UTC()
			out = append(out, s)
		}

		return out
	}

	if g, w := norm(got), norm(want); !reflect.DeepEqual(g, w) {
		t.Errorf("spans =\n  %+v\nwant\n  %+v", g, w)
	}
}
