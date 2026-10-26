package storetest

import (
	"cmp"
	"reflect"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Every station is measured over the window: lease time clipped to it (a lease span that
// straddles its start counts from the start, a live lease up to its end, one that ended before it
// not at all), the items waiting at its end and a day before, since when the oldest of them
// waits, and the gate verdicts given in it. The lease history begins with the earliest lease,
// inside the window or not. Another organization measures nothing.
func testStationTimes(f *fixture) {
	t := f.t
	since, until := t1, t1.Add(10*time.Hour)
	h := func(n int) time.Time { return t1.Add(time.Duration(n) * time.Hour) }
	nextSt := domain.Station{ID: stNext, Kind: domain.KindAgent, Agent: agWorker, Agents: 1}

	a := f.create(t, domain.NewItem{CreatedAt: h(-20)}) // waits at work a day before until
	d := f.create(t, domain.NewItem{Station: stNext, CreatedAt: h(-10)})

	_, l := f.claim(t, nextSt, 0, h(-9)) // d's lease ends before the window
	f.move(t, ports.Transition{
		ItemID: d.ID, From: domain.Position{Station: stNext, State: domain.StateInProgress},
		To: domain.Position{Station: stNext, State: domain.StateBlocked}, Lease: &l, Outcome: domain.LeaseBlocked, At: h(-8),
	})

	_, l = f.claim(t, work, 0, h(-2)) // a's lease straddles the window's start: 1 h counts
	f.move(t, next(a, l, h(1)))       // then a waits at next

	f.create(t, domain.NewItem{CreatedAt: h(2)})
	f.claim(t, work, 1, h(3)) // still held at until: 7 h count

	c := f.create(t, domain.NewItem{CreatedAt: h(4)}) // waits at work at until

	verdict := func(kind domain.EventKind, at time.Time) domain.Event {
		return domain.Event{FactoryID: f.factory, ItemID: c.ID, At: at, Kind: kind, Station: stWork, Actor: agWorker}
	}

	if err := f.Store.AppendEvents(f.ctx(t),
		verdict(domain.EventGateRejected, h(-1)), // before the window
		verdict(domain.EventGateRejected, h(5)),
		verdict(domain.EventGateApproved, h(6)),
	); err != nil {
		t.Fatal(err)
	}

	got, coverage, err := f.Flow.StationTimes(f.ctx(t), f.factory, since, until)
	if err != nil {
		t.Fatal(err)
	}

	for i := range got {
		got[i].OldestWaiting = got[i].OldestWaiting.UTC()
	}

	slices.SortFunc(got, func(x, y domain.StationTimes) int { return cmp.Compare(x.Station, y.Station) })

	want := []domain.StationTimes{
		// a waits at next from 1 h to the end: 9 h.
		{Station: stNext, WaitingNow: 1, OldestWaiting: h(1), Waiting: 9 * time.Hour, Waiters: 1},
		// At work the item made at 2 h waits 1 h and c from 4 h to the end; a's wait ended
		// before the window.
		{
			Station: stWork, Busy: 8 * time.Hour, Leases: 2, WaitingNow: 1, WaitingDayAgo: 1, OldestWaiting: h(4),
			Waiting: 7 * time.Hour, Waiters: 2, Verdicts: 2, Rejected: 1,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StationTimes =\n  %+v\nwant\n  %+v", got, want)
	}

	if !coverage.Equal(h(-9)) {
		t.Errorf("lease history begins %s, want %s (the earliest lease)", coverage.UTC(), h(-9))
	}

	other := f.NewOrg(t)

	got, coverage, err = f.Flow.StationTimes(domain.WithOrg(t.Context(), other), f.factory, since, until)
	if err != nil || len(got) != 0 || !coverage.IsZero() {
		t.Errorf("another organization measures %+v from %s (%v); want nothing", got, coverage, err)
	}
}

// Every item running under a live lease reports its run's usage events, its newest tool call and
// its newest TodoWrite call; an item leased with no run yet, a finished run, another factory's run
// and another organization report nothing.
func testLiveRuns(f *fixture) {
	t := f.t

	running := f.create(t, domain.NewItem{CreatedAt: t0})
	leased := f.create(t, domain.NewItem{CreatedAt: t0.Add(time.Minute)})
	elsewhere := f.newFactory(t)
	away := f.create(t, domain.NewItem{FactoryID: elsewhere})

	f.claim(t, work, 0, t1)
	f.claim(t, work, 1, t1)

	if _, _, err := f.Store.ClaimNext(f.ctx(t), ports.Claim{
		FactoryID: elsewhere, Station: work, Slot: 0, Now: t1, ExpiresAt: t1.Add(ttl),
	}); err != nil {
		t.Fatal(err)
	}

	start := func(it domain.Item) domain.RunID {
		id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
			FactoryID: it.FactoryID, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model, StartedAt: t1,
		})
		if err != nil {
			t.Fatal(err)
		}

		return id
	}

	event := func(it domain.Item, run domain.RunID, kind domain.EventKind, data map[string]any) domain.Event {
		return domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, RunID: run, At: t1, Kind: kind, Station: stWork, Actor: agWorker, Data: data}
	}
	call := func(tool, input string) map[string]any { return map[string]any{"id": tool, "tool": tool, "input": input} }

	plan := call(domain.ToolTodoWrite, `{"todos":[{"status":"completed"}]}`)
	edit := call("Edit", `{"file_path":"x.go"}`)

	finished, run, other := start(running), start(running), start(away)
	if err := f.Store.FinishRun(f.ctx(t), finished, domain.RunResult{Status: domain.RunFailed}, t1); err != nil {
		t.Fatal(err)
	}

	if err := f.Store.AppendEvents(f.ctx(t),
		event(running, finished, domain.EventUsage, nil), event(running, finished, domain.EventToolCall, call("Bash", "{}")),
		event(running, run, domain.EventUsage, nil),
		event(running, run, domain.EventToolCall, call(domain.ToolTodoWrite, `{"todos":[]}`)),
		event(running, run, domain.EventToolCall, plan),
		event(running, run, domain.EventToolCall, edit),
		event(running, run, domain.EventToolResult, map[string]any{"id": "Edit", "summary": "ok"}),
		event(running, run, domain.EventUsage, nil),
		event(away, other, domain.EventUsage, nil), event(away, other, domain.EventToolCall, edit),
	); err != nil {
		t.Fatal(err)
	}

	got, err := f.Flow.LiveRuns(f.ctx(t), f.factory)
	if err != nil {
		t.Fatal(err)
	}

	want := map[domain.ItemID]domain.LiveRun{running.ID: {Turns: 2, LastToolCall: edit, LastPlan: plan}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LiveRuns = %+v, want %+v (leased %s has no run)", got, want, leased.ID)
	}

	if got, err := f.Flow.LiveRuns(domain.WithOrg(t.Context(), f.NewOrg(t)), f.factory); err != nil || len(got) != 0 {
		t.Errorf("another organization's LiveRuns = %v (%v); want none", got, err)
	}
}
