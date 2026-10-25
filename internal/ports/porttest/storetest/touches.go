package storetest

import (
	"cmp"
	"errors"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// keyText is the data key of a note's text.
const keyText = "text"

// People and an API key of the touch tests, as actor refs and names.
var (
	ada = domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}
	bob = domain.Actor{Kind: domain.ActorUser, ID: "bob", Name: "Bob"}
	key = domain.Actor{Kind: domain.ActorService, ID: "ci", Name: "ci (API key)"}
)

// by is an event of kind at it's station, at `at`, by a person or key (data.actor), with data.
func by(it domain.Item, a domain.Actor, kind domain.EventKind, at time.Time, data map[string]any) domain.Event {
	d := map[string]any{"actor": a.Ref()}
	maps.Copy(d, data)

	return domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: it.Station, Actor: a.Name, Data: d}
}

// agentEvent is an event an agent or the engine writes: no data.actor.
func agentEvent(it domain.Item, actor string, kind domain.EventKind, at time.Time) domain.Event {
	return domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: it.Station, Actor: actor}
}

func (f *fixture) appendEvents(t *testing.T, events ...domain.Event) {
	t.Helper()

	if err := f.Store.AppendEvents(f.ctx(t), events...); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
}

func (f *fixture) itemTouches(t *testing.T, id domain.ItemID) []domain.Touch {
	t.Helper()

	got, err := f.Flow.ItemTouches(f.ctx(t), id)
	if err != nil {
		t.Fatalf("ItemTouches: %v", err)
	}

	return got
}

// touchID is the event id of the touch of kind in touches, failing when there is none.
func touchID(t *testing.T, touches []domain.Touch, kind domain.EventKind) int64 {
	t.Helper()

	for _, tc := range touches {
		if tc.Kind == kind {
			return tc.EventID
		}
	}

	t.Fatalf("no %s touch in %+v", kind, touches)

	return 0
}

// eventID is the id of the item's first event of kind.
func (f *fixture) eventID(t *testing.T, id domain.ItemID, kind domain.EventKind) int64 {
	t.Helper()

	for _, e := range f.Events(t, id) {
		if e.Kind == kind {
			return e.ID
		}
	}

	t.Fatalf("item %s has no %s event", id, kind)

	return 0
}

// One action by a person counts once, named by its most specific event; created, lease_taken,
// agents, the engine and API keys never count; a decision's data.planned says whether the line
// required it, and older approvals and merges count as planned; planned touches start with the
// reason required_gate (the label rows the store adds per person event).
func testTouchesOneActionOnce(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }

	f.appendEvents(t,
		by(it, ada, domain.EventCreated, at(0), nil),
		agentEvent(it, agWorker, domain.EventGateRejected, at(1)),
		agentEvent(it, domain.ActorEngine, domain.EventWaitingHuman, at(1)),
		by(it, key, domain.EventApproved, at(2), nil),
		// A send-back of an item in progress: the lease taken and the rejection, one touch.
		by(it, ada, domain.EventLeaseTaken, at(3), map[string]any{"action": "reject"}),
		by(it, ada, domain.EventRejected, at(3), map[string]any{keyReason: "wrong file", domain.KeyPlanned: false}),
		agentEvent(it, ada.Name, domain.EventClaimed, at(3)),
		// Two people at the same instant: two touches.
		by(it, bob, domain.EventNote, at(3), map[string]any{keyText: "see the docs"}),
		// A move and a pause in one transition: named by the move.
		by(it, ada, domain.EventPaused, at(4), nil),
		by(it, ada, domain.EventMoved, at(4), map[string]any{"from": stWork, "to": stNext}),
		// An escalation's approval is unplanned; an older approval (no data.planned) is planned.
		by(it, bob, domain.EventApproved, at(5), map[string]any{domain.KeyPlanned: false}),
		by(it, bob, domain.EventApproved, at(6), nil),
		// A merge records merged and approved: one planned touch named merged.
		by(it, ada, domain.EventMerged, at(7), map[string]any{"pull_request": "https://example.com/pr/1", domain.KeyPlanned: true}),
		by(it, ada, domain.EventApproved, at(7), map[string]any{domain.KeyPlanned: true}),
		by(it, bob, domain.EventRetried, at(8), nil),
	)

	type touch struct {
		kind    domain.EventKind
		actor   string
		planned bool
		reason  domain.TouchReason
	}

	var got []touch

	for _, tc := range f.itemTouches(t, it.ID) {
		if tc.ItemID != it.ID || tc.FactoryID != f.factory || tc.Station != stWork || tc.EventID == 0 {
			t.Errorf("touch %+v: want item %s of factory %s at %s with its event", tc, it.ID, f.factory, stWork)
		}

		got = append(got, touch{tc.Kind, tc.Actor, tc.Planned, tc.Reason})
	}

	want := []touch{
		{domain.EventRejected, ada.Ref(), false, ""},
		{domain.EventNote, bob.Ref(), false, ""},
		{domain.EventMoved, ada.Ref(), false, ""},
		{domain.EventApproved, bob.Ref(), false, ""},
		{domain.EventApproved, bob.Ref(), true, domain.ReasonRequiredGate},
		{domain.EventMerged, ada.Ref(), true, domain.ReasonRequiredGate},
		{domain.EventRetried, bob.Ref(), false, ""},
	}
	// Ada's and Bob's touches at minute 3 tie on time: the naming event's id orders them.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ItemTouches = %+v\nwant %+v", got, want)
	}

	counts, err := f.Flow.TouchCounts(f.ctx(t), []domain.ItemID{it.ID, missingItem})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(counts, map[domain.ItemID]int{it.ID: len(want)}) {
		t.Errorf("TouchCounts = %v; want %s: %d and nothing for an unknown item", counts, it.ID, len(want))
	}
}

// touchItem is one item the metrics test finishes: its touches, its runs' versions (the last
// one counts), its type and when it is done (zero: still open).
type touchItem struct {
	typ      string
	touches  []domain.Event
	versions []domain.DefinitionVersionID
	doneAt   time.Time
}

// hour is t0 plus h hours.
func hour(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }

// touchNote is a's note at stWork at hour h.
func touchNote(a domain.Actor, h int) domain.Event {
	return by(domain.Item{Station: stWork}, a, domain.EventNote, hour(h), map[string]any{keyText: "x"})
}

// finishItem creates ti in factory with its runs and touches, and finishes it unless it stays open.
func (f *fixture) finishItem(t *testing.T, factory domain.FactoryID, ti touchItem) {
	t.Helper()

	it := f.create(t, domain.NewItem{FactoryID: factory, Type: ti.typ})

	for i, v := range ti.versions {
		if _, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
			FactoryID: factory, DefinitionVersionID: v, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model,
			StartedAt: t0.Add(time.Duration(i+1) * time.Minute),
		}); err != nil {
			t.Fatalf("StartRun: %v", err)
		}
	}

	for _, e := range ti.touches {
		e.FactoryID, e.ItemID = factory, it.ID
		f.appendEvents(t, e)
	}

	if !ti.doneAt.IsZero() {
		f.finish(t, it, ti.doneAt, false)
	}
}

// seedTouchMetrics finishes the metrics test's items in [t0, t0+48h) and around it, and pulls its
// andons; it returns the two versions they ran on and what Touches of the window must say.
func (f *fixture) seedTouchMetrics(t *testing.T) (v1 domain.DefinitionVersionID, want domain.TouchStats) {
	t.Helper()

	v1, v2 := f.NewVersion(t, f.org, f.factory), f.NewVersion(t, f.org, f.factory)
	approve := by(domain.Item{Station: stNext}, bob, domain.EventApproved, hour(2), map[string]any{domain.KeyPlanned: true})

	f.finishItem(t, f.factory, touchItem{
		typ: typeTask, versions: []domain.DefinitionVersionID{v1, v2}, touches: []domain.Event{touchNote(ada, 1), approve}, doneAt: hour(3),
	})
	f.finishItem(t, f.factory, touchItem{typ: typeTask, versions: []domain.DefinitionVersionID{v1}, doneAt: hour(30)})
	f.finishItem(t, f.factory, touchItem{typ: "bug", touches: []domain.Event{touchNote(bob, 4), touchNote(ada, 5), touchNote(ada, 6)}, doneAt: hour(40)})
	// Outside the window, or never finished: not counted, whatever their touches.
	f.finishItem(t, f.factory, touchItem{typ: typeTask, touches: []domain.Event{touchNote(ada, -3)}, doneAt: hour(-1)})
	f.finishItem(t, f.factory, touchItem{typ: typeTask, touches: []domain.Event{touchNote(ada, 50)}, doneAt: hour(48)})
	f.finishItem(t, f.factory, touchItem{typ: typeTask, touches: []domain.Event{touchNote(ada, 2)}})

	// Line stops by people in the window; the engine's pull and an earlier one do not count.
	f.pullAndon(t, f.factory, "before", hour(-2))
	f.clearAndon(t, hour(-2))
	f.pullAndon(t, f.factory, "stop", hour(10))
	f.clearAndon(t, hour(11))

	if _, err := f.Store.PullAndon(f.ctx(t), domain.AndonPull{
		FactoryID: f.factory, Kind: domain.AndonManual, Reason: "rework rate", PulledAt: hour(12),
	}, domain.Actor{Kind: domain.ActorSystem, ID: domain.ActorEngine, Name: domain.ActorEngine}); err != nil {
		t.Fatalf("PullAndon by the engine: %v", err)
	}

	unlabelled := ""

	return v1, domain.TouchStats{
		Finished: 3, Touchless: 1, Touches: 5, Planned: 1, Median: 2,
		ByStation: []domain.TouchCount{{Key: stWork, Touches: 4}, {Key: stNext, Touches: 1, Planned: 1}},
		ByKind:    []domain.TouchCount{{Key: string(domain.EventNote), Touches: 4}, {Key: string(domain.EventApproved), Touches: 1, Planned: 1}},
		ByReason:  []domain.TouchCount{{Key: unlabelled, Touches: 4}, {Key: string(domain.ReasonRequiredGate), Touches: 1, Planned: 1}},
		ByVersion: versionsSorted([]domain.VersionTouches{
			{VersionID: "", Finished: 1, Touches: 3},
			{VersionID: v1, Finished: 1, Touchless: 1},
			{VersionID: v2, Finished: 1, Touches: 2, Planned: 1},
		}),
		LineStops: 1,
	}
}

func (f *fixture) touches(t *testing.T, q ports.FlowQuery) domain.TouchStats {
	t.Helper()

	got, err := f.Flow.Touches(f.ctx(t), q)
	if err != nil {
		t.Fatalf("Touches: %v", err)
	}

	return got
}

// The metrics count the items finished in the window by when they finished: touches per item,
// planned and unplanned, the touchless share, by station, kind, reason and the definition version
// of each item's last run, the window's line stops by people; a version or a type narrows them.
func testTouchesMetrics(f *fixture) {
	t := f.t

	v1, want := f.seedTouchMetrics(t)
	q := ports.FlowQuery{FactoryID: f.factory, Since: t0, Until: hour(48)}

	if got := f.touches(t, q); !reflect.DeepEqual(got, want) {
		t.Errorf("Touches =\n%+v\nwant\n%+v", got, want)
	}

	byVersion, byType := q, q
	byVersion.VersionID, byType.Type = v1, "bug"

	if got := f.touches(t, byVersion); got.Finished != 1 || got.Touchless != 1 || got.Touches != 0 {
		t.Errorf("Touches of v1 = %+v; want the one touchless item that last ran on v1", got)
	}

	if got := f.touches(t, byType); got.Finished != 1 || got.Touches != 3 {
		t.Errorf("Touches of type bug = %+v; want the one bug and its 3 touches", got)
	}
}

// Buckets split the finished items by when they finished.
func testTouchesByBucket(f *fixture) {
	t := f.t

	f.seedTouchMetrics(t)
	q := ports.FlowQuery{FactoryID: f.factory, Since: t0, Until: hour(48)}

	// Days ending at until: the first item on day 1, the other two on day 2.
	buckets, err := f.Flow.TouchesByBucket(f.ctx(t), q, 24*time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(buckets) != 2 || buckets[0].Finished != 1 || buckets[0].Touches != 2 || buckets[1].Finished != 2 ||
		buckets[1].Touches != 3 || buckets[1].Touchless != 1 || buckets[0].LineStops != 0 {
		t.Errorf("TouchesByBucket = %+v; want day 1: 1 item, 2 touches; day 2: 2 items, 3 touches, 1 touchless", buckets)
	}
}

// The organization view measures each factory it is given and their total; another
// organization's factory counts nothing.
func testOrgTouches(f *fixture) {
	t := f.t

	_, want := f.seedTouchMetrics(t)
	q := ports.FlowQuery{FactoryID: f.factory, Since: t0, Until: hour(48)}
	other := f.newFactory(t)
	f.finishItem(t, other, touchItem{typ: typeTask, touches: []domain.Event{touchNote(ada, 1)}, doneAt: hour(2)})

	foreign := f.otherOrg(t)
	foreign.finishTouched(t, hour(2))

	total, each, err := f.Flow.OrgTouches(f.ctx(t), []domain.FactoryID{f.factory, other, foreign.factory}, q)
	if err != nil {
		t.Fatal(err)
	}

	type sum struct {
		factory                             domain.FactoryID
		finished, touches, touchless, stops int
		median                              float64
	}

	sumOf := func(id domain.FactoryID, st domain.TouchStats) sum {
		return sum{id, st.Finished, st.Touches, st.Touchless, st.LineStops, st.Median}
	}

	got := []sum{sumOf("", total)}
	for _, e := range each {
		got = append(got, sumOf(e.FactoryID, e.TouchStats))
	}

	wantSums := []sum{{"", 4, 6, 1, 1, 1.5}, sumOf(f.factory, want), {other, 1, 1, 0, 0, 1}, {foreign.factory, 0, 0, 0, 0, 0}}
	if !reflect.DeepEqual(got, wantSums) {
		t.Errorf("OrgTouches total, then each = %+v; want %+v", got, wantSums)
	}

	if !reflect.DeepEqual(each[0].TouchStats, want) {
		t.Errorf("OrgTouches of this factory = %+v; want Touches' %+v", each[0].TouchStats, want)
	}
}

// finishTouched creates an item with one touch and finishes it at doneAt.
func (f *fixture) finishTouched(t *testing.T, doneAt time.Time) domain.Item {
	t.Helper()

	it := f.create(t, domain.NewItem{})
	f.appendEvents(t, by(it, ada, domain.EventNote, t0, map[string]any{keyText: "x"}))
	f.finish(t, it, doneAt, false)

	return it
}

func (f *fixture) clearAndon(t *testing.T, at time.Time) {
	t.Helper()

	if _, err := f.Store.ClearAndon(f.ctx(t), f.factory, domain.LocalUser, "", at); err != nil {
		t.Fatalf("ClearAndon: %v", err)
	}
}

// versionsSorted orders versions by id, as TouchStats lists them.
func versionsSorted(v []domain.VersionTouches) []domain.VersionTouches {
	slices.SortFunc(v, func(a, b domain.VersionTouches) int { return cmp.Compare(a.VersionID, b.VersionID) })

	return v
}

// A person sets a touch's reason on its naming event; the metrics count by it. An event that
// names no touch (the approval a merge also records, an agent's event, another organization's)
// is not found, and nothing changes.
func testLabelTouch(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	f.appendEvents(t,
		by(it, ada, domain.EventMerged, t0, map[string]any{domain.KeyPlanned: true}),
		by(it, ada, domain.EventApproved, t0, map[string]any{domain.KeyPlanned: true}),
		by(it, bob, domain.EventNote, t0.Add(time.Minute), map[string]any{keyText: "x"}),
		agentEvent(it, agWorker, domain.EventGateApproved, t0.Add(2*time.Minute)),
	)
	f.finish(t, it, t0.Add(time.Hour), false)

	touches := f.itemTouches(t, it.ID)
	merged, noted := touchID(t, touches, domain.EventMerged), touchID(t, touches, domain.EventNote)

	if err := f.Store.LabelTouch(f.ctx(t), noted, domain.ReasonMissingKnowledge, ada); err != nil {
		t.Fatalf("LabelTouch: %v", err)
	}

	if err := f.Store.LabelTouch(f.ctx(t), merged, domain.ReasonWrongDecision, bob); err != nil {
		t.Fatalf("LabelTouch again: %v", err)
	}

	for _, refused := range []struct {
		what  string
		event int64
		org   *fixture
	}{
		{"the approval a merge records", f.eventID(t, it.ID, domain.EventApproved), f},
		{"an agent's event", f.eventID(t, it.ID, domain.EventGateApproved), f},
		{"no event", 1 << 40, f},
		{"another organization's touch", noted, f.otherOrg(t)},
	} {
		err := f.Store.LabelTouch(refused.org.ctx(t), refused.event, domain.ReasonOther, ada)
		if !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("LabelTouch of %s = %v; want ErrNotFound", refused.what, err)
		}
	}

	got := map[domain.EventKind][2]string{}
	for _, tc := range f.itemTouches(t, it.ID) {
		got[tc.Kind] = [2]string{string(tc.Reason), tc.ReasonBy}
	}

	want := map[domain.EventKind][2]string{
		domain.EventMerged: {string(domain.ReasonWrongDecision), bob.Ref()},
		domain.EventNote:   {string(domain.ReasonMissingKnowledge), ada.Ref()},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reasons after LabelTouch = %v; want %v", got, want)
	}

	stats, err := f.Flow.Touches(f.ctx(t), ports.FlowQuery{FactoryID: f.factory, Since: t0, Until: t1.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	wantReasons := []domain.TouchCount{
		{Key: string(domain.ReasonMissingKnowledge), Touches: 1},
		{Key: string(domain.ReasonWrongDecision), Touches: 1, Planned: 1},
	}
	if !reflect.DeepEqual(stats.ByReason, wantReasons) {
		t.Errorf("ByReason = %+v; want %+v", stats.ByReason, wantReasons)
	}
}

// Touches need an organization and see only its items and factories.
func testTouchesTenancy(f *fixture) {
	t := f.t

	it := f.finishTouched(t, t0.Add(time.Hour))
	other := f.otherOrg(t)
	q := ports.FlowQuery{FactoryID: f.factory, Since: t0, Until: t1.Add(time.Hour)}

	if got := other.itemTouches(t, it.ID); len(got) != 0 {
		t.Errorf("another organization's ItemTouches = %+v; want none", got)
	}

	if counts, err := f.Flow.TouchCounts(other.ctx(t), []domain.ItemID{it.ID}); err != nil || len(counts) != 0 {
		t.Errorf("another organization's TouchCounts = %v, %v; want none", counts, err)
	}

	if got, err := f.Flow.Touches(other.ctx(t), q); err != nil || got.Finished != 0 || got.Touches != 0 {
		t.Errorf("another organization's Touches of this factory = %+v, %v; want nothing", got, err)
	}

	if _, err := f.Flow.Touches(t.Context(), q); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("Touches without an organization = %v; want ErrNoOrg", err)
	}

	if _, err := f.Flow.ItemTouches(t.Context(), it.ID); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("ItemTouches without an organization = %v; want ErrNoOrg", err)
	}

	if err := f.Store.LabelTouch(t.Context(), 1, domain.ReasonOther, ada); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("LabelTouch without an organization = %v; want ErrNoOrg", err)
	}
}
