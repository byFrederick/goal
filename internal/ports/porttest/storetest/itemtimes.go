package storetest

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ItemTimes gives every item of the type done in the window its lead time and its time in
// each component of the lead-time bridge, from its spans.
func testItemTimes(f *fixture) {
	t := f.t
	pos := func(station string, state domain.ItemState) domain.Position {
		return domain.Position{Station: station, State: state}
	}

	// Waits at work 1 h, is blocked 1 h, waits again 1 h, waits on a person at next 2 h, is
	// sent back by next and waits 1 h in that round, then is done: 6 h in all.
	it := f.create(t, domain.NewItem{CreatedAt: hours(0)})
	for _, tr := range []ports.Transition{
		{From: pos(stWork, domain.StateQueued), To: pos(stWork, domain.StateBlocked), At: hours(1)},
		{From: pos(stWork, domain.StateBlocked), To: pos(stWork, domain.StateQueued), At: hours(2)},
		{From: pos(stWork, domain.StateQueued), To: pos(stNext, domain.StateWaitingHuman), At: hours(3)},
		{From: pos(stNext, domain.StateWaitingHuman), To: pos(stWork, domain.StateQueued), Rework: true, At: hours(5)},
		{From: pos(stWork, domain.StateQueued), To: pos(stDone, domain.StateDone), At: hours(6), DoneAt: hours(6)},
	} {
		tr.ItemID = it.ID
		f.move(t, tr)
	}

	// Not in the result: another type, done before the window, still open, another factory.
	f.finish(t, f.create(t, domain.NewItem{CreatedAt: hours(1), Type: typeOther}), hours(2), false)
	f.finish(t, f.create(t, domain.NewItem{CreatedAt: hours(-5)}), hours(-1), false)
	f.create(t, domain.NewItem{CreatedAt: hours(1)})
	f.finish(t, f.create(t, domain.NewItem{FactoryID: f.newFactory(t), CreatedAt: hours(1)}), hours(2), false)

	q := ports.FlowQuery{FactoryID: f.factory, Since: hours(0), Until: hours(24), Type: typeTask}

	got, err := f.Flow.ItemTimes(f.ctx(t), q)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.ItemTime{{
		Item: it.ID, LeadTime: 6 * time.Hour,
		Times: map[domain.Component]time.Duration{
			{Kind: domain.ComponentWaiting, Station: stWork}: 2 * time.Hour,
			{Kind: domain.ComponentBlocked, Station: stWork}: time.Hour,
			{Kind: domain.ComponentWaitingOnYou}:             2 * time.Hour,
			{Kind: domain.ComponentRework, Station: stNext}:  time.Hour,
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ItemTimes =\n  %+v\nwant\n  %+v", got, want)
	}

	q.Type = ""
	if every, err := f.Flow.ItemTimes(f.ctx(t), q); err != nil || len(every) != 2 {
		t.Errorf("ItemTimes of every type = %+v, %v; want the two items done in the window", every, err)
	}

	if _, err := f.Flow.ItemTimes(context.Background(), q); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("ItemTimes without an organization = %v; want ErrNoOrg", err)
	}

	other := f.otherOrg(t)
	if seen, err := other.Flow.ItemTimes(other.ctx(t), q); err != nil || len(seen) != 0 {
		t.Errorf("ItemTimes from another organization = %+v, %v; want none", seen, err)
	}
}
