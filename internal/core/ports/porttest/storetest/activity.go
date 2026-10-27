package storetest

import (
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// activity reads the fixture factory's activity through the port.
func (f *fixture) activity(t *testing.T, limit int) []domain.ActivityEvent {
	t.Helper()

	got, err := f.Store.FactoryActivity(f.ctx(t), f.factory, limit)
	if err != nil {
		t.Fatal(err)
	}

	return got
}

func testFactoryActivity(f *fixture) {
	t := f.t

	a := f.create(t, domain.NewItem{Title: "a"})
	b := f.create(t, domain.NewItem{Title: "b"})
	other := f.create(t, domain.NewItem{FactoryID: f.newFactory(t), Title: "elsewhere"})

	ev := func(it domain.Item, kind domain.EventKind, at time.Time) domain.Event {
		return domain.Event{
			FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: stWork, Actor: agWorker,
			Data: map[string]any{"kind": string(kind)},
		}
	}

	f.appendEvents(t,
		ev(a, domain.EventMoved, t1),
		// Same at as a's move and appended after it: listed before it.
		ev(b, domain.EventGateApproved, t1),
		ev(a, "tool_call", t1.Add(time.Second)),
		ev(a, "tool_result", t1.Add(time.Second)),
		ev(a, "agent_message", t1.Add(time.Second)),
		ev(a, domain.EventUsage, t1.Add(time.Second)),
		ev(a, domain.EventClaimed, t1.Add(2*time.Second)),
		ev(b, domain.EventDone, t1.Add(3*time.Second)),
		ev(other, domain.EventMoved, t1.Add(4*time.Second)),
	)

	type entry struct {
		item  domain.ItemID
		kind  domain.EventKind
		title string
		num   int64
	}

	want := []entry{
		{b.ID, domain.EventDone, "b", b.Number},
		{b.ID, domain.EventGateApproved, "b", b.Number},
		{a.ID, domain.EventMoved, "a", a.Number},
		{b.ID, domain.EventCreated, "b", b.Number},
		{a.ID, domain.EventCreated, "a", a.Number},
	}

	check := func(got []domain.ActivityEvent, want []entry) {
		t.Helper()

		if len(got) != len(want) {
			t.Fatalf("FactoryActivity = %d events %v; want %d", len(got), got, len(want))
		}

		for i, w := range want {
			g := got[i]
			if g.ItemID != w.item || g.Kind != w.kind || g.Title != w.title || g.Number != w.num || g.FactoryID != f.factory {
				t.Errorf("event %d = %s %s %q #%d of %s; want %s %s %q #%d", i, g.ItemID, g.Kind, g.Title, g.Number, g.FactoryID, w.item, w.kind, w.title, w.num)
			}

			if i > 0 && g.At.Equal(got[i-1].At) && g.ID >= got[i-1].ID {
				t.Errorf("event %d (id %d) at the same time as the one before (id %d) is not older by id", i, g.ID, got[i-1].ID)
			}

			if g.Kind != domain.EventCreated && g.Data["kind"] != string(g.Kind) {
				t.Errorf("event %d data = %v; want the stored data", i, g.Data)
			}
		}
	}

	check(f.activity(t, 50), want)
	check(f.activity(t, 2), want[:2])
}
