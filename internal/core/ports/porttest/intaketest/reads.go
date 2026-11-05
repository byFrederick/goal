package intaketest

import (
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// readsTests are the cases of what Intake shows for ADR 0040's screens (decision 22): items by
// origin, the items that haven't started and the follow-ups for the architect.
var readsTests = []struct {
	name string
	test func(f *fixture)
}{
	{"Origins_CountedOverAWindow", testOrigins},
	{"ComingUp_NotStartedSoonestFirst", testComingUp},
	{"Triage_CollectingItemAndWaitingSources", testTriageQueue},
}

// The suite's agent, and the factory name its line has.
const (
	worker      = "worker"
	factoryName = "dev"
)

// filed creates an open item of the fixture's factory at its first station, as n says.
func (f *fixture) filed(n domain.NewItem) domain.Item {
	f.t.Helper()

	n.FactoryID, n.Type, n.Station, n.State, n.Class = f.factory, typeFix, station, domain.StateQueued, domain.ClassStandard
	if n.CreatedAt.IsZero() {
		n.CreatedAt = t0
	}

	it, err := f.Store.CreateItem(f.ctx(), n)
	if err != nil {
		f.t.Fatalf("CreateItem %s: %v", n.Title, err)
	}

	return it
}

// keep keeps follow-ups on it, as an accepted run at its station would.
func (f *fixture) keep(it domain.Item, fs ...domain.FollowUp) {
	f.t.Helper()

	for i := range fs {
		fs[i].Station, fs[i].Agent, fs[i].State = station, worker, domain.FollowUpWaiting
	}

	if _, err := f.Store.Transition(f.ctx(), ports.Transition{ItemID: it.ID, From: it.Position(), To: it.Position(), At: t0, FollowUps: fs}); err != nil {
		f.t.Fatalf("keep follow-ups: %v", err)
	}
}

// landAndFile moves it to done and runs the follow-up sweep on the fixture's line.
func (f *fixture) landAndFile(it domain.Item) {
	f.t.Helper()

	f.close(it, domain.StateDone)

	line := domain.Line{FactoryID: f.factory, Name: factoryName, Stations: []domain.Station{{ID: station, Kind: domain.KindAgent, Agent: worker}}}
	if _, err := f.Store.FileFollowUps(f.ctx(), []domain.Line{line}, t0.Add(time.Hour), 20); err != nil {
		f.t.Fatalf("FileFollowUps: %v", err)
	}
}

// OriginTally counts the items created in the window by origin, one entry per origin with items;
// another organization counts none.
func testOrigins(f *fixture) {
	t := f.t

	for _, n := range []struct {
		origin domain.Origin
		at     time.Time
	}{
		{domain.OriginPerson, t0},
		{domain.OriginPerson, t0.Add(time.Hour)},
		{domain.OriginPlan, t0},
		{domain.OriginTrigger, t0},
		{domain.OriginFollowUp, t0},
		{domain.OriginTrigger, t0.Add(-48 * time.Hour)},
	} {
		f.filed(domain.NewItem{Title: string(n.origin), Origin: n.origin, CreatedAt: n.at})
	}

	got, err := f.in.OriginTally(f.ctx(), f.factory, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.OriginCount{
		{Origin: domain.OriginFollowUp, Items: 1},
		{Origin: domain.OriginPerson, Items: 2},
		{Origin: domain.OriginPlan, Items: 1},
		{Origin: domain.OriginTrigger, Items: 1},
	}
	if !slices.Equal(got, want) {
		t.Errorf("OriginTally = %v; want %v", got, want)
	}

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))
	if got, err := f.in.OriginTally(labs, f.factory, t0.Add(-time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("labs OriginTally = %v, %v; want none", got, err)
	}
}

// ComingUp lists the open items that haven't started and have a start or an open planned or
// linked need: those with a start soonest first, then the rest oldest first, each with its open
// needs and the item whose follow-up filed it. Started, closed and unconstrained items are left
// out, a need that is done no longer holds an item back, and the total counts past the limit.
func testComingUp(f *fixture) {
	t := f.t

	need := f.filed(domain.NewItem{Title: "the need"})
	doneNeed := f.filed(domain.NewItem{Title: "a done need"})
	f.filed(domain.NewItem{Title: "later", StartAt: t0.Add(72 * time.Hour)})
	soon := f.filed(domain.NewItem{Title: "soon", StartAt: t0.Add(24 * time.Hour), After: []domain.ItemID{need.ID}})
	f.filed(domain.NewItem{Title: "waits", CreatedAt: t0.Add(time.Minute), After: []domain.ItemID{need.ID}})
	f.filed(domain.NewItem{Title: "started", StartAt: t0.Add(time.Hour), StartedAt: t0})
	f.filed(domain.NewItem{Title: "met", After: []domain.ItemID{doneNeed.ID}})
	f.close(doneNeed, domain.StateDone)

	src := f.filed(domain.NewItem{Title: "source"})
	f.keep(src, domain.FollowUp{Text: "Check it in a week", Target: domain.FollowUpStart, Start: &domain.FollowUpPlan{In: "7d", Title: "From a follow-up"}})
	f.landAndFile(src)

	got, total, err := f.in.ComingUp(f.ctx(), f.factory, 10)
	if err != nil {
		t.Fatal(err)
	}

	var lines []string
	for _, c := range got {
		lines = append(lines, fmt.Sprintf("%s needs %v from %s#%d", c.Item.Title, c.Needs, c.FromFactory, c.FromNumber))
	}

	want := []string{
		fmt.Sprintf("soon needs [%d] from #0", need.Number),
		"later needs [] from #0",
		fmt.Sprintf("From a follow-up needs [] from %s#%d", f.factory, src.Number),
		fmt.Sprintf("waits needs [%d] from #0", need.Number),
	}
	if !slices.Equal(lines, want) || total != len(want) {
		t.Errorf("ComingUp = %d in all\n%v\nwant %d\n%v", total, lines, len(want), want)
	}

	if len(got) > 0 && (got[0].Item.ID != soon.ID || !got[0].Item.StartAt.Equal(soon.StartAt)) {
		t.Errorf("ComingUp's first = %+v; want soon with its start", got[0].Item)
	}

	f.comingUpLimited(soon.ID, len(want))
}

// comingUpLimited checks that ComingUp limited to 1 answers first, of total in all, and that
// another organization has none coming up.
func (f *fixture) comingUpLimited(first domain.ItemID, total int) {
	t := f.t

	if got, n, err := f.in.ComingUp(f.ctx(), f.factory, 1); err != nil || len(got) != 1 || got[0].Item.ID != first || n != total {
		t.Errorf("ComingUp limit 1 = %v, %d, %v; want %s of %d", got, n, err, first, total)
	}

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))
	if got, n, err := f.in.ComingUp(labs, f.factory, 10); err != nil || len(got) != 0 || n != 0 {
		t.Errorf("labs ComingUp = %v, %d, %v; want none", got, n, err)
	}
}

// TriageQueue is the collecting triage item with the follow-ups that joined it, and the trusted
// items with waiting triage follow-ups; once an agent claims the item, it no longer collects.
func testTriageQueue(f *fixture) {
	t := f.t

	if q, err := f.in.TriageQueue(f.ctx(), f.factory, 10); err != nil || q.Item != nil || len(q.Waiting) != 0 {
		t.Errorf("TriageQueue without follow-ups = %+v, %v; want empty", q, err)
	}

	landed := f.filed(domain.NewItem{Title: "landed"})
	f.keep(landed, domain.FollowUp{Text: "Tidy the docs", Target: domain.FollowUpTriage})
	f.landAndFile(landed)

	open := f.filed(domain.NewItem{Title: "still open"})
	f.keep(open, domain.FollowUp{Text: "One", Target: domain.FollowUpTriage}, domain.FollowUp{Text: "Two", Target: domain.FollowUpTriage})

	untrusted := f.filed(domain.NewItem{Title: "untrusted", Untrusted: true})
	f.keep(untrusted, domain.FollowUp{Text: "From outside", Target: domain.FollowUpTriage})

	q, err := f.in.TriageQueue(f.ctx(), f.factory, 10)
	if err != nil {
		t.Fatal(err)
	}

	if q.Item == nil || !q.Item.Triage || q.FollowUps != 1 {
		t.Fatalf("TriageQueue's item = %+v with %d follow-ups; want the triage item with 1", q.Item, q.FollowUps)
	}

	want := []domain.TriageSource{{ID: open.ID, Number: open.Number, Title: open.Title, FollowUps: 2}}
	if !slices.Equal(q.Waiting, want) {
		t.Errorf("TriageQueue's waiting = %+v; want %+v", q.Waiting, want)
	}

	f.claimed(q.Item.ID)
}

// claimed starts a run on the triage item: it no longer collects, its sources still wait, and
// another organization has neither.
func (f *fixture) claimed(item domain.ItemID) {
	t := f.t

	if _, err := f.Store.StartRun(f.ctx(), ports.NewRun{
		FactoryID: f.factory, ItemID: item, Station: station, Agent: worker, Model: "claude-haiku-5-5", StartedAt: t0.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if q, err := f.in.TriageQueue(f.ctx(), f.factory, 10); err != nil || q.Item != nil || len(q.Waiting) != 1 {
		t.Errorf("TriageQueue after a claim = %+v, %v; want no item and the waiting source", q, err)
	}

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))
	if q, err := f.in.TriageQueue(labs, f.factory, 10); err != nil || q.Item != nil || len(q.Waiting) != 0 {
		t.Errorf("labs TriageQueue = %+v, %v; want empty", q, err)
	}
}
