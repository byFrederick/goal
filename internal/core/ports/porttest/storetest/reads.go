package storetest

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Reads for the board, item lists and flow metrics.

func testItemByNumber(f *fixture) {
	t := f.t

	other := f.newFactory(t)
	a := f.create(t, domain.NewItem{Title: "a"})
	b := f.create(t, domain.NewItem{Title: "b"})
	f.create(t, domain.NewItem{FactoryID: other, Title: "other a"})

	got, err := f.Store.ItemByNumber(f.ctx(t), f.factory, 2)
	if err != nil {
		t.Fatal(err)
	}

	sameItem(t, got, b)

	if got, err := f.Store.ItemByNumber(f.ctx(t), other, 1); err != nil || got.Title != "other a" || got.ID == a.ID {
		t.Errorf("other factory's #1 = %+v, %v; want its own item", got, err)
	}

	for _, n := range []int64{0, 3} {
		if _, err := f.Store.ItemByNumber(f.ctx(t), f.factory, n); !errors.Is(err, ports.ErrNotFound) {
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

	f.create(t, domain.NewItem{FactoryID: f.newFactory(t)})

	if claimed, _ := f.claim(t, work, 0, t1); claimed.Number != 1 {
		t.Fatalf("claimed #%d; want #1, the first in pull order", claimed.Number)
	}

	four, err := f.Store.ItemByNumber(f.ctx(t), f.factory, 4)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.Store.Transition(f.ctx(t), ports.Transition{
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

		items, err := f.Store.ListItems(f.ctx(t), q)
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
	f.create(t, domain.NewItem{FactoryID: f.newFactory(t)})

	f.claim(t, work, 0, t1)
	it, l := f.claim(t, work, 1, t1)

	// A blocked item is open but neither waiting nor in progress; a done one is not counted.
	if _, err := f.Store.Transition(f.ctx(t), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stWork, State: domain.StateBlocked}, Lease: &l, Outcome: domain.LeaseBlocked, At: t1,
	}); err != nil {
		t.Fatal(err)
	}

	f.finish(t, f.create(t, domain.NewItem{}), t1, false)

	loads, err := f.Store.StationLoads(f.ctx(t), f.factory)
	if err != nil {
		t.Fatal(err)
	}

	slices.SortFunc(loads, func(a, b domain.StationLoad) int { return cmp.Compare(a.Station, b.Station) })

	want := []domain.StationLoad{
		{Station: stNext, Waiting: 1, WaitingHuman: 1},
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
	f.create(t, domain.NewItem{FactoryID: f.newFactory(t), CreatedAt: since})

	// Spend counts the runs started in the window.
	for _, r := range []struct {
		at   time.Time
		cost float64
	}{{since, 0.25}, {since.Add(time.Hour), 1.5}, {since.Add(-time.Minute), 7}, {until, 9}} {
		run := f.startRun(t, open.ID, r.at)
		if err := f.Store.FinishRun(f.ctx(t), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: r.cost}, r.at); err != nil {
			t.Fatal(err)
		}
	}

	m, err := f.Flow.FlowMetrics(f.ctx(t), ports.FlowQuery{FactoryID: f.factory, Since: since, Until: until}, "")
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

	empty, err := f.Flow.FlowMetrics(f.ctx(t), ports.FlowQuery{FactoryID: f.factory, Since: until.Add(time.Hour), Until: until.Add(2 * time.Hour)}, "")
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

		it, err = f.Store.Transition(f.ctx(t), ports.Transition{
			ItemID: it.ID, From: it.Position(), To: it.Position(), Rework: true, At: doneAt,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err := f.Store.Transition(f.ctx(t), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: "done", State: domain.StateDone},
		At: doneAt, DoneAt: doneAt,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testAttention(f *fixture) {
	t := f.t

	busy, idle := f.newFactory(t), f.newFactory(t)

	// The fixture's factory: three decisions of two types, one blocked item, an open andon pull,
	// and items in other states, which count for nothing.
	f.create(t, domain.NewItem{State: domain.StateWaitingHuman, Type: "fix"})
	f.create(t, domain.NewItem{State: domain.StateWaitingHuman, Type: "change"})
	f.create(t, domain.NewItem{State: domain.StateWaitingHuman, Type: "fix"})
	f.create(t, domain.NewItem{State: domain.StateBlocked})
	f.create(t, domain.NewItem{State: domain.StateInProgress})
	f.create(t, domain.NewItem{})
	f.finish(t, f.create(t, domain.NewItem{State: domain.StateWaitingHuman}), t1, false)

	pull := f.pullAndon(t, f.factory, "rework rate over 40%", t1)

	// busy: one blocked item and only a cleared pull; idle: nothing at all.
	f.create(t, domain.NewItem{FactoryID: busy, State: domain.StateBlocked})
	f.pullAndon(t, busy, "cleared", t0)

	if _, err := f.Store.ClearAndon(f.ctx(t), busy, domain.LocalUser, "", t1); err != nil {
		t.Fatal(err)
	}

	// Another organization's factory with work and an open pull is never counted.
	other := f.otherOrg(t)
	other.create(t, domain.NewItem{State: domain.StateWaitingHuman})
	other.pullAndon(t, other.factory, "theirs", t1)

	got, err := f.Store.Attention(f.ctx(t))
	if err != nil {
		t.Fatal(err)
	}

	if !slices.IsSortedFunc(got, func(a, b domain.Attention) int { return cmp.Compare(a.Factory, b.Factory) }) {
		t.Errorf("Attention = %+v; want it ordered by factory id", got)
	}

	want := map[domain.FactoryID]domain.Attention{
		f.factory: {
			Factory: f.factory, WaitingHuman: 3, Blocked: 1, Andon: &pull,
			WaitingByType: []domain.TypeCount{{Type: "change", Count: 1}, {Type: "fix", Count: 2}},
		},
		busy: {Factory: busy, Blocked: 1},
		idle: {Factory: idle},
	}

	if len(got) != len(want) {
		t.Fatalf("Attention = %+v; want one entry for each of the org's %d factories", got, len(want))
	}

	for _, a := range got {
		w := want[a.Factory]
		if !reflect.DeepEqual(a, w) {
			t.Errorf("Attention of %s = %+v (andon %+v); want %+v (andon %+v)", a.Factory, a, a.Andon, w, w.Andon)
		}
	}
}

// Titles of the inbox test's items.
const (
	inLane       = "in the expedite lane"
	titleBlocked = "blocked by checks"
	titlePaused  = "paused by a person"
	titleMoved   = "changed last"
)

func testInboxItems(f *fixture) {
	t := f.t

	const secondTitle = "in the second factory"

	at := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }
	second, third := f.newFactory(t), f.newFactory(t)

	f.create(t, domain.NewItem{Title: "oldest", State: domain.StateWaitingHuman, CreatedAt: at(0)})
	f.create(t, domain.NewItem{Title: "tie-a", State: domain.StateWaitingHuman, CreatedAt: at(1)})
	f.create(t, domain.NewItem{Title: "tie-b", State: domain.StateWaitingHuman, CreatedAt: at(1)})
	f.create(t, domain.NewItem{FactoryID: second, Title: secondTitle, State: domain.StateWaitingHuman, CreatedAt: at(2)})
	f.create(t, domain.NewItem{Title: inLane, Class: domain.ClassExpedite, State: domain.StateInProgress, CreatedAt: at(3)})
	blocked := f.create(t, domain.NewItem{Title: titleBlocked, State: domain.StateBlocked, CreatedAt: at(4)})
	paused := f.create(t, domain.NewItem{Title: titlePaused, State: domain.StateBlocked, CreatedAt: at(6)})
	// Not waiting on a person, or not in the factories asked for.
	f.create(t, domain.NewItem{Title: "queued", CreatedAt: at(9)})
	f.create(t, domain.NewItem{Title: "done expedite", Class: domain.ClassExpedite, Station: stDone, State: domain.StateDone, CreatedAt: at(9)})
	f.create(t, domain.NewItem{FactoryID: third, Title: "third", State: domain.StateWaitingHuman, CreatedAt: at(9)})

	block := func(it domain.Item, kind domain.EventKind, reason string, m int) domain.Event {
		data := map[string]any{}
		if reason != "" {
			data["reason"] = reason
		}

		return domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: at(m), Kind: kind, Station: stWork, Actor: domain.ActorEngine, Data: data}
	}

	// The latest blocking event says why: the run's failure, then a person's pause over it.
	if err := f.Store.AppendEvents(f.ctx(t),
		block(blocked, domain.EventBlocked, "an older failure", 4), block(blocked, domain.EventBlocked, "checks failed", 5),
		block(paused, domain.EventBlocked, "first", 6), block(paused, domain.EventPaused, "", 7),
	); err != nil {
		t.Fatal(err)
	}

	// A decision that changed last comes first, though it was created first.
	moved := f.create(t, domain.NewItem{Title: titleMoved, State: domain.StateWaitingHuman, CreatedAt: at(0)})
	if _, err := f.Store.Transition(f.ctx(t), ports.Transition{
		ItemID: moved.ID, From: moved.Position(), To: domain.Position{Station: stNext, State: domain.StateWaitingHuman}, At: at(8),
	}); err != nil {
		t.Fatal(err)
	}

	// Another organization's decision is unreachable even when its factory is asked for.
	other := f.otherOrg(t)
	other.create(t, domain.NewItem{Title: "other org", State: domain.StateWaitingHuman, CreatedAt: at(9)})

	factories := []domain.FactoryID{f.factory, second, other.factory}

	tests := []struct {
		name      string
		factories []domain.FactoryID
		limit     int
		want      []string
	}{
		{
			name: "newest change first, ties by id", factories: factories, limit: 20,
			want: []string{titleMoved, titlePaused, titleBlocked, inLane, secondTitle, "tie-a", "tie-b", "oldest"},
		},
		{name: "up to limit", factories: factories, limit: 2, want: []string{titleMoved, titlePaused}},
		{name: "one factory", factories: []domain.FactoryID{second}, limit: 10, want: []string{secondTitle}},
		{name: "no factories", factories: nil, limit: 10, want: []string{}},
	}

	for _, tt := range tests {
		got, err := f.Flow.InboxItems(f.ctx(t), tt.factories, tt.limit)
		if err != nil {
			t.Fatal(err)
		}

		titles := []string{}
		for _, a := range got {
			titles = append(titles, a.Item.Title)
		}

		if !reflect.DeepEqual(titles, tt.want) {
			t.Errorf("%s: InboxItems = %v; want %v", tt.name, titles, tt.want)
		}
	}

	got, err := f.Flow.InboxItems(f.ctx(t), factories, 20)
	if err != nil {
		t.Fatal(err)
	}

	why := map[string]string{}
	for _, a := range got {
		why[a.Item.Title] = fmt.Sprintf("%q paused=%v", a.BlockedReason, a.Paused)
	}

	want := map[string]string{titleBlocked: `"checks failed" paused=false`, titlePaused: `"" paused=true`, titleMoved: `"" paused=false`}
	for title, w := range want {
		if why[title] != w {
			t.Errorf("%s: blocked reason %s; want %s", title, why[title], w)
		}
	}
}

// spent records a finished run of the item's factory, started at at, that cost usd.
func (f *fixture) spent(t *testing.T, it domain.Item, at time.Time, usd float64) {
	t.Helper()

	run, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model, StartedAt: at,
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	res := domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}
	if err := f.Store.FinishRun(f.ctx(t), run, res, at.Add(time.Minute)); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
}

// spendSince is SpendSince in the fixture's organization; an error fails the test.
func (f *fixture) spendSince(t *testing.T, factory domain.FactoryID, since time.Time) float64 {
	t.Helper()

	usd, err := f.Store.SpendSince(f.ctx(t), factory, since)
	if err != nil {
		t.Fatalf("SpendSince: %v", err)
	}

	return usd
}

// SpendSince sums the cost of the factory's runs started at or after since, the bound included;
// a run still running costs nothing yet, and other factories' runs do not count.
func testSpendSince(f *fixture) {
	t := f.t
	since := t1
	it := f.create(t, domain.NewItem{})

	f.spent(t, it, since.Add(-time.Second), 7)
	f.spent(t, it, since, 0.25)
	f.spent(t, it, since.Add(48*time.Hour), 1.5)
	f.startRun(t, it.ID, since.Add(time.Hour))
	f.spent(t, f.create(t, domain.NewItem{FactoryID: f.newFactory(t)}), since, 9)

	tests := []struct {
		name    string
		factory domain.FactoryID
		since   time.Time
		want    float64
	}{
		{"from the bound on", f.factory, since, 1.75},
		{"every run", f.factory, since.Add(-time.Hour), 8.75},
		{"after every run", f.factory, since.Add(72 * time.Hour), 0},
		{"a factory with no runs", f.newFactory(t), t0, 0},
	}

	for _, tt := range tests {
		if got := f.spendSince(t, tt.factory, tt.since); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("%s: SpendSince = %v; want %v", tt.name, got, tt.want)
		}
	}
}
