package storetest

import (
	"cmp"
	"context"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ItemTimes gives every item of the type done in the window its lead time and its time in
// each component of the lead-time bridge, from its spans.
func testItemTimes(f *fixture) {
	t := f.t
	pos := func(station string, state domain.ItemState) domain.Position {
		return domain.Position{Station: station, State: state}
	}

	// Waits 2 h at the inbox, which is not lead time, then waits at work 1 h, is blocked 1 h,
	// waits again 1 h, waits on a person at next 2 h, is sent back by next and waits 1 h in that
	// round, then is done: 6 h in all from when it left the inbox.
	it := f.create(t, domain.NewItem{Station: stInbox, CreatedAt: hours(-2)})
	for _, tr := range []ports.Transition{
		{From: pos(stInbox, domain.StateQueued), To: pos(stWork, domain.StateQueued), At: hours(0)},
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

	got, err := f.Flow.ItemTimes(f.ctx(t), q, 10)
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
	if every, err := f.Flow.ItemTimes(f.ctx(t), q, 10); err != nil || len(every) != 2 {
		t.Errorf("ItemTimes of every type = %+v, %v; want the two items done in the window", every, err)
	}

	// The limit keeps the items done last (the read is bounded however long the window).
	if latest, err := f.Flow.ItemTimes(f.ctx(t), q, 1); err != nil || len(latest) != 1 || latest[0].Item != it.ID {
		t.Errorf("ItemTimes with limit 1 = %+v, %v; want only %s, done last", latest, err, it.ID)
	}

	if _, err := f.Flow.ItemTimes(context.Background(), q, 10); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("ItemTimes without an organization = %v; want ErrNoOrg", err)
	}

	other := f.otherOrg(t)
	if seen, err := other.Flow.ItemTimes(other.ctx(t), q, 10); err != nil || len(seen) != 0 {
		t.Errorf("ItemTimes from another organization = %+v, %v; want none", seen, err)
	}
}

// ItemTimes counts the time an item waited at a station for CI on its commits as ci at that
// station, not waiting: the overlap of each first-round waiting span with the union of the
// item's CI waits there, an open wait counting up to the span's end (ADR 0044 decision 10).
func testItemTimesCIWaits(f *fixture) {
	t := f.t
	pos := func(station string, state domain.ItemState) domain.Position {
		return domain.Position{Station: station, State: state}
	}

	// visit leaves the inbox at 0, waits at work until working, then works 1 h and is done.
	visit := func(working int) domain.ItemID {
		it := f.create(t, domain.NewItem{Station: stInbox, CreatedAt: hours(-1)})
		for _, tr := range []ports.Transition{
			{From: pos(stInbox, domain.StateQueued), To: pos(stWork, domain.StateQueued), At: hours(0)},
			{From: pos(stWork, domain.StateQueued), To: pos(stWork, domain.StateInProgress), At: hours(working)},
			{From: pos(stWork, domain.StateInProgress), To: pos(stDone, domain.StateDone), At: hours(working + 1), DoneAt: hours(working + 1)},
		} {
			tr.ItemID = it.ID
			f.move(t, tr)
		}

		return it.ID
	}
	wait := func(item domain.ItemID, repo string, from, to int) {
		w := f.openCIWait(t, domain.NewCIWait{
			Item: item, Station: stWork, Visit: ciVisit, Repo: repo, SHA: ciSHA, Phase: domain.CIPhaseBefore, Round: 1, StartedAt: hours(from),
		})

		if to < 0 {
			return // still open
		}

		if n, err := f.Store.EndCIWaits(f.ctx(t), []domain.CIWaitID{w.ID}, domain.CIPassed, hours(to)); err != nil || n != 1 {
			t.Fatalf("EndCIWaits = %d, %v; want 1, nil", n, err)
		}
	}

	// One wait, 1 h of its 3 h waiting at work.
	one := visit(3)
	wait(one, ciRepo, 1, 2)

	// Two waits on two repositories, 1–3 h and 2–4 h, count their union of 3 h once.
	two := visit(5)
	wait(two, ciRepo, 1, 3)
	wait(two, "github.com/acme/web", 2, 4)

	// A wait still open when the item started work counts until its waiting span ended.
	open := visit(3)
	wait(open, ciRepo, 1, -1)

	// No CI wait: the same times as without CI waits.
	never := visit(2)

	got, err := f.Flow.ItemTimes(f.ctx(t), ports.FlowQuery{FactoryID: f.factory, Since: hours(0), Until: hours(24), Type: typeTask}, 10)
	if err != nil {
		t.Fatal(err)
	}

	waiting := domain.Component{Kind: domain.ComponentWaiting, Station: stWork}
	ci := domain.Component{Kind: domain.ComponentCI, Station: stWork}
	working := domain.Component{Kind: domain.ComponentWorking, Station: stWork}
	want := []domain.ItemTime{
		{Item: never, LeadTime: 3 * time.Hour, Times: map[domain.Component]time.Duration{waiting: 2 * time.Hour, working: time.Hour}},
		{Item: one, LeadTime: 4 * time.Hour, Times: map[domain.Component]time.Duration{waiting: 2 * time.Hour, ci: time.Hour, working: time.Hour}},
		{Item: open, LeadTime: 4 * time.Hour, Times: map[domain.Component]time.Duration{waiting: time.Hour, ci: 2 * time.Hour, working: time.Hour}},
		{Item: two, LeadTime: 6 * time.Hour, Times: map[domain.Component]time.Duration{waiting: 2 * time.Hour, ci: 3 * time.Hour, working: time.Hour}},
	}

	// Items done at the same time come in id order.
	slices.SortStableFunc(want, func(a, b domain.ItemTime) int {
		return cmp.Or(cmp.Compare(a.LeadTime, b.LeadTime), cmp.Compare(a.Item, b.Item))
	})

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ItemTimes =\n  %+v\nwant\n  %+v", got, want)
	}
}
