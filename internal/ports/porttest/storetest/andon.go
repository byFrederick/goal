package storetest

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// pullAndon pulls a manual andon on factory at `at` as the local user.
func (f *fixture) pullAndon(t *testing.T, factory domain.FactoryID, reason string, at time.Time) domain.AndonPull {
	t.Helper()

	p, err := f.Store.PullAndon(f.ctx(t), domain.AndonPull{FactoryID: factory, Kind: domain.AndonManual, Reason: reason, PulledAt: at}, domain.LocalUser)
	if err != nil {
		t.Fatalf("PullAndon: %v", err)
	}

	return p
}

func (f *fixture) openAndon(t *testing.T, factory domain.FactoryID) *domain.AndonPull {
	t.Helper()

	p, err := f.Store.OpenAndon(f.ctx(t), factory)
	if err != nil {
		t.Fatalf("OpenAndon: %v", err)
	}

	return p
}

func sameAndon(t *testing.T, what string, got, want domain.AndonPull) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %+v; want %+v", what, got, want)
	}
}

// A factory has at most one open pull; clearing it records who, when and why, and the next pull
// opens a new one. Pulls are read newest first.
func testAndonPullAndClear(f *fixture) {
	t := f.t

	if p := f.openAndon(t, f.factory); p != nil {
		t.Fatalf("OpenAndon of a running line = %+v; want nil", p)
	}

	pulled := f.pullAndon(t, f.factory, "Bad answers reach customers", t0)
	want := domain.AndonPull{
		ID: pulled.ID, FactoryID: f.factory, Kind: domain.AndonManual, Reason: "Bad answers reach customers",
		PulledBy: domain.LocalUser.Ref(), PulledAt: t0, Data: map[string]any{},
	}

	if pulled.ID == 0 {
		t.Error("PullAndon returned no id")
	}

	sameAndon(t, "PullAndon", pulled, want)

	if p := f.openAndon(t, f.factory); p == nil {
		t.Fatal("OpenAndon after a pull = nil")
	} else {
		sameAndon(t, "OpenAndon", *p, want)
	}

	_, err := f.Store.PullAndon(f.ctx(t), domain.AndonPull{FactoryID: f.factory, Kind: domain.AndonManual, Reason: "again", PulledAt: t1}, domain.LocalUser)
	expect(t, "a second PullAndon", err, ports.ErrConflict)

	// Another factory's line stops on its own.
	other := f.newFactory(t)
	f.pullAndon(t, other, "elsewhere", t0)

	cleared, err := f.Store.ClearAndon(f.ctx(t), f.factory, domain.LocalUser, "Fixed the prompt", t1)
	if err != nil {
		t.Fatal(err)
	}

	want.ClearedBy, want.ClearedAt, want.Note = domain.LocalUser.Ref(), t1, "Fixed the prompt"
	sameAndon(t, "ClearAndon", cleared, want)

	if p := f.openAndon(t, f.factory); p != nil {
		t.Errorf("OpenAndon after the clear = %+v; want nil", p)
	}

	if f.openAndon(t, other) == nil {
		t.Error("clearing one factory's pull cleared another's")
	}

	_, err = f.Store.ClearAndon(f.ctx(t), f.factory, domain.LocalUser, "", t1)
	expect(t, "ClearAndon with none open", err, ports.ErrNotFound)
}

// Pulls stay as history, the newest first; a rework pull keeps its station and data.
func testAndonHistory(f *fixture) {
	t := f.t

	f.pullAndon(t, f.factory, "first", t0)

	cleared, err := f.Store.ClearAndon(f.ctx(t), f.factory, domain.LocalUser, "", t1)
	if err != nil {
		t.Fatal(err)
	}

	rework, err := f.Store.PullAndon(f.ctx(t), domain.AndonPull{
		FactoryID: f.factory, Kind: domain.AndonRework, Station: stNext, Reason: "next sent back 4 of the last 10",
		PulledAt: t1.Add(time.Minute), Data: map[string]any{"rejected": float64(4), "window": float64(10)},
	}, domain.Actor{Kind: domain.ActorSystem, ID: "engine", Name: "engine"})
	if err != nil {
		t.Fatal(err)
	}

	if rework.PulledBy != "system:engine" || rework.Station != stNext || rework.Data["rejected"] != float64(4) {
		t.Errorf("rework pull = %+v; want the engine's, at %s, with its data", rework, stNext)
	}

	pulls, err := f.Store.AndonPulls(f.ctx(t), f.factory, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(pulls) != 2 || pulls[0].ID != rework.ID || pulls[1].ID != cleared.ID {
		t.Errorf("AndonPulls = %+v; want the rework pull, then the cleared one", pulls)
	}

	if pulls, _ := f.Store.AndonPulls(f.ctx(t), f.factory, 1); len(pulls) != 1 || pulls[0].ID != rework.ID {
		t.Errorf("AndonPulls(limit 1) = %+v; want the newest only", pulls)
	}
}

// Another organization cannot pull, clear or read a factory's andon.
func testAndonOnlyTheOrgs(f *fixture) {
	t := f.t

	other := f.otherOrg(t)
	pulled := other.pullAndon(t, other.factory, "theirs", t0)

	_, err := f.Store.PullAndon(f.ctx(t), domain.AndonPull{FactoryID: other.factory, Kind: domain.AndonManual, Reason: "x", PulledAt: t0}, domain.LocalUser)
	expect(t, "PullAndon on another org's factory", err, ports.ErrNotFound)

	_, err = f.Store.ClearAndon(f.ctx(t), other.factory, domain.LocalUser, "", t1)
	expect(t, "ClearAndon on another org's factory", err, ports.ErrNotFound)

	if p := f.openAndon(t, other.factory); p != nil {
		t.Errorf("OpenAndon of another org's factory = %+v; want nil", p)
	}

	if pulls, err := f.Store.AndonPulls(f.ctx(t), other.factory, 10); err != nil || len(pulls) != 0 {
		t.Errorf("AndonPulls of another org's factory = %+v, %v; want none", pulls, err)
	}

	if p := other.openAndon(t, other.factory); p == nil || p.ID != pulled.ID {
		t.Errorf("the owner's OpenAndon = %+v; want its pull", p)
	}
}

// Concurrent pulls on one factory: exactly one opens, the others conflict.
func testConcurrentAndonPulls(f *fixture) {
	t := f.t

	const pullers = 8

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		won  int
		errs []error
	)

	for range pullers {
		wg.Go(func() {
			_, err := f.Store.PullAndon(f.ctx(t), domain.AndonPull{FactoryID: f.factory, Kind: domain.AndonManual, Reason: "stop", PulledAt: t0}, domain.LocalUser)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				won++
			case !errors.Is(err, ports.ErrConflict):
				errs = append(errs, err)
			}
		})
	}

	wg.Wait()

	if won != 1 || len(errs) > 0 {
		t.Errorf("%d of %d concurrent pulls opened (errors %v); want exactly one, the rest ErrConflict", won, pullers, errs)
	}
}

// verdict is a gate verdict event at stNext on it.
func verdict(it domain.Item, rejected bool, reason string, at time.Time) domain.Event {
	e := domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: domain.EventGateApproved, Station: stNext, Actor: agWorker}
	if rejected {
		e.Kind, e.Data = domain.EventGateRejected, map[string]any{"reason": reason}
	}

	return e
}

// GateDecisions counts the last n verdicts of one gate station after a time, newest first, with
// the most frequent rejection category or reason.
func testGateDecisions(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }

	events := []domain.Event{
		verdict(it, true, "Old reason", at(0)),
		verdict(it, true, "No evidence", at(1)),
		verdict(it, false, "", at(2)),
		verdict(it, true, "no evidence ", at(3)),
		verdict(it, true, "Wrong service", at(4)),
		verdict(it, false, "", at(5)),
		// Not verdicts of this gate: another station, another kind.
		{FactoryID: it.FactoryID, ItemID: it.ID, At: at(6), Kind: domain.EventGateRejected, Station: stWork, Data: map[string]any{"reason": "x"}},
		{FactoryID: it.FactoryID, ItemID: it.ID, At: at(6), Kind: domain.EventRejected, Station: stNext, Data: map[string]any{"reason": "x"}},
	}
	categorized := verdict(it, true, "whatever", at(7))
	categorized.Data["category"] = "evidence"
	events = append(events, categorized)

	if err := f.Store.AppendEvents(f.ctx(t), events...); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		after time.Time
		n     int
		want  domain.GateTally
	}{
		{name: "every verdict", n: 10, want: domain.GateTally{Station: stNext, Verdicts: 7, Rejected: 5, TopReason: "no evidence"}},
		{name: "the last n", n: 3, want: domain.GateTally{Station: stNext, Verdicts: 3, Rejected: 2, TopReason: "evidence"}},
		{name: "after the last clear", after: at(2), n: 10, want: domain.GateTally{Station: stNext, Verdicts: 4, Rejected: 3, TopReason: "evidence"}},
	}

	for _, tt := range tests {
		got, err := f.Flow.GateDecisions(f.ctx(t), f.factory, stNext, tt.after, tt.n)
		if err != nil {
			t.Fatal(err)
		}

		if got != tt.want {
			t.Errorf("%s: GateDecisions = %+v; want %+v", tt.name, got, tt.want)
		}
	}

	other := f.otherOrg(t)
	if got, err := other.Flow.GateDecisions(other.ctx(t), f.factory, stNext, time.Time{}, 10); err != nil || got.Verdicts != 0 {
		t.Errorf("another org's GateDecisions = %+v, %v; want none", got, err)
	}
}
