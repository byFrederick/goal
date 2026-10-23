package storetest

import (
	"cmp"
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Reads for the board, item lists and flow metrics.

func testItemByNumber(f *fixture) {
	t := f.t

	other := f.NewFactory(t)
	a := f.create(t, domain.NewItem{Title: "a"})
	b := f.create(t, domain.NewItem{Title: "b"})
	f.create(t, domain.NewItem{FactoryID: other, Title: "other a"})

	got, err := f.Store.ItemByNumber(t.Context(), f.factory, 2)
	if err != nil {
		t.Fatal(err)
	}

	sameItem(t, got, b)

	if got, err := f.Store.ItemByNumber(t.Context(), other, 1); err != nil || got.Title != "other a" || got.ID == a.ID {
		t.Errorf("other factory's #1 = %+v, %v; want its own item", got, err)
	}

	for _, n := range []int64{0, 3} {
		if _, err := f.Store.ItemByNumber(t.Context(), f.factory, n); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("ItemByNumber(%d) = %v; want ErrNotFound", n, err)
		}
	}
}

func testListItems(f *fixture) {
	t := f.t

	// #1..#5 queued at work; #1 is claimed (in progress), #4 moves on to next.
	for range 5 {
		f.create(t, domain.NewItem{})
	}

	f.create(t, domain.NewItem{FactoryID: f.NewFactory(t)})

	if claimed, _ := f.claim(t, work, 0, t1); claimed.Number != 1 {
		t.Fatalf("claimed #%d; want #1, the first in pull order", claimed.Number)
	}

	four, err := f.Store.ItemByNumber(t.Context(), f.factory, 4)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.Store.Transition(t.Context(), ports.Transition{
		ItemID: four.ID, From: four.Position(), To: domain.Position{Station: stNext, State: domain.StateQueued}, At: t1,
	}); err != nil {
		t.Fatal(err)
	}

	list := func(q ports.ItemQuery) []int64 {
		t.Helper()

		q.FactoryID = f.factory
		if q.Limit == 0 {
			q.Limit = 100
		}

		items, err := f.Store.ListItems(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}

		numbers := []int64{}
		for _, it := range items {
			numbers = append(numbers, it.Number)
		}

		return numbers
	}

	tests := []struct {
		name string
		q    ports.ItemQuery
		want []int64
	}{
		{"all, newest first", ports.ItemQuery{}, []int64{5, 4, 3, 2, 1}},
		{"one page", ports.ItemQuery{Limit: 2}, []int64{5, 4}},
		{"the next page", ports.ItemQuery{Limit: 2, Before: 4}, []int64{3, 2}},
		{"by state", ports.ItemQuery{States: []domain.ItemState{domain.StateInProgress}}, []int64{1}},
		{"by states", ports.ItemQuery{States: []domain.ItemState{domain.StateQueued, domain.StateDone}, Before: 4}, []int64{3, 2}},
		{"by station", ports.ItemQuery{Station: stNext}, []int64{4}},
		{"by station and state", ports.ItemQuery{Station: stWork, States: []domain.ItemState{domain.StateQueued}}, []int64{5, 3, 2}},
		{"nothing matches", ports.ItemQuery{States: []domain.ItemState{domain.StateBlocked}}, []int64{}},
	}

	for _, tt := range tests {
		if got := list(tt.q); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %v; want %v", tt.name, got, tt.want)
		}
	}
}

func testStationLoads(f *fixture) {
	t := f.t

	for range 4 {
		f.create(t, domain.NewItem{})
	}

	f.create(t, domain.NewItem{Station: stNext})
	f.create(t, domain.NewItem{Station: stNext, State: domain.StateWaitingHuman})
	f.create(t, domain.NewItem{FactoryID: f.NewFactory(t)})

	f.claim(t, work, 0, t1)
	it, l := f.claim(t, work, 1, t1)

	// A blocked item is open but neither waiting nor in progress; a done one is not counted.
	if _, err := f.Store.Transition(t.Context(), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stWork, State: domain.StateBlocked}, Lease: &l, At: t1,
	}); err != nil {
		t.Fatal(err)
	}

	f.finish(t, f.create(t, domain.NewItem{}), t1, false)

	loads, err := f.Store.StationLoads(t.Context(), f.factory)
	if err != nil {
		t.Fatal(err)
	}

	slices.SortFunc(loads, func(a, b domain.StationLoad) int { return cmp.Compare(a.Station, b.Station) })

	want := []domain.StationLoad{
		{Station: stNext, Waiting: 1},
		{Station: stWork, Waiting: 2, InProgress: 1},
	}
	if !reflect.DeepEqual(loads, want) {
		t.Errorf("loads = %+v; want %+v", loads, want)
	}
}

func testFlowMetrics(f *fixture) {
	t := f.t

	since := t0.Truncate(24 * time.Hour)
	until := since.Add(72 * time.Hour)
	day := func(n int) time.Time { return since.Truncate(24 * time.Hour).Add(time.Duration(n) * 24 * time.Hour) }

	// Lead times 1h, 2h, 4h, 10h; the 2h one was reworked. One done before the window, one
	// after it, one still open, one cancelled.
	done := []struct {
		after   time.Duration
		rework  bool
		dayOver int
	}{{time.Hour, false, 0}, {2 * time.Hour, true, 0}, {4 * time.Hour, false, 1}, {10 * time.Hour, false, 1}}

	for i, d := range done {
		created := day(d.dayOver).Add(time.Duration(2+i) * time.Hour)
		f.finish(t, f.create(t, domain.NewItem{CreatedAt: created}), created.Add(d.after), d.rework)
	}

	f.finish(t, f.create(t, domain.NewItem{CreatedAt: since.Add(-48 * time.Hour)}), since.Add(-time.Second), false)
	f.finish(t, f.create(t, domain.NewItem{CreatedAt: until.Add(-time.Hour)}), until, false)

	open := f.create(t, domain.NewItem{CreatedAt: since})
	f.create(t, domain.NewItem{CreatedAt: since, State: domain.StateCancelled})
	f.create(t, domain.NewItem{FactoryID: f.NewFactory(t), CreatedAt: since})

	// Spend counts the runs started in the window.
	for _, r := range []struct {
		at   time.Time
		cost float64
	}{{since, 0.25}, {since.Add(time.Hour), 1.5}, {since.Add(-time.Minute), 7}, {until, 9}} {
		run := f.startRun(t, open.ID, r.at)
		if err := f.Store.FinishRun(t.Context(), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: r.cost}, r.at); err != nil {
			t.Fatal(err)
		}
	}

	m, err := f.Store.FlowMetrics(t.Context(), f.factory, since, until)
	if err != nil {
		t.Fatal(err)
	}

	// percentile_cont over 1, 2, 4, 10 hours: p50 = 3h, p85 at rank 2.55 = 4h + 0.55 × 6h.
	want := domain.FlowMetrics{
		DonePerDay: []domain.DayCount{{Day: day(0), Count: 2}, {Day: day(1), Count: 2}},
		ItemsDone:  4, Reworked: 1, WIP: 1, SpendUSD: 1.75,
		LeadTimeP50: 3 * time.Hour, LeadTimeP85: 4*time.Hour + 198*time.Minute,
	}

	if math.Abs(m.SpendUSD-want.SpendUSD) > 1e-9 {
		t.Errorf("spend = %v; want %v", m.SpendUSD, want.SpendUSD)
	}

	m.SpendUSD = want.SpendUSD
	for i := range m.DonePerDay {
		m.DonePerDay[i].Day = m.DonePerDay[i].Day.UTC()
	}

	if !reflect.DeepEqual(m, want) {
		t.Errorf("metrics =\n  %+v\nwant\n  %+v", m, want)
	}

	empty, err := f.Store.FlowMetrics(t.Context(), f.factory, until.Add(time.Hour), until.Add(2*time.Hour))
	if err != nil || empty.ItemsDone != 0 || empty.LeadTimeP50 != 0 || len(empty.DonePerDay) != 0 || empty.WIP != 1 {
		t.Errorf("metrics of an empty window = %+v, %v; want nothing done and the line's WIP", empty, err)
	}
}

// finish moves a queued item at the work station straight to done at doneAt, reworked first
// when asked.
func (f *fixture) finish(t *testing.T, it domain.Item, doneAt time.Time, rework bool) {
	t.Helper()

	if rework {
		var err error

		it, err = f.Store.Transition(t.Context(), ports.Transition{
			ItemID: it.ID, From: it.Position(), To: it.Position(), Rework: true, At: doneAt,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err := f.Store.Transition(t.Context(), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: "done", State: domain.StateDone},
		At: doneAt, DoneAt: doneAt,
	})
	if err != nil {
		t.Fatal(err)
	}
}
