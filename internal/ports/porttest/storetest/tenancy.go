package storetest

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// otherOrg is a fixture on the same store in a new organization with a factory of its own.
func (f *fixture) otherOrg(t *testing.T) *fixture {
	t.Helper()

	o := &fixture{Harness: f.Harness, t: t, org: f.NewOrg(t)}
	o.factory = o.newFactory(t)

	return o
}

// expect fails the test unless err wraps want.
func expect(t *testing.T, what string, err, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Errorf("%s = %v; want %v", what, err, want)
	}
}

func testNoOrg(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})
	_, l := f.claim(t, work, 0, t1)
	run := f.startRun(t, it.ID, t1)
	ctx := t.Context() // no organization

	_, err := f.Store.CreateItem(ctx, domain.NewItem{
		FactoryID: f.factory, Type: typeTask, Title: "x", Station: stWork,
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: t0,
	})
	expect(t, "CreateItem", err, domain.ErrNoOrg)

	_, err = f.Store.Item(ctx, it.ID)
	expect(t, "Item", err, domain.ErrNoOrg)

	_, err = f.Store.Waiting(ctx, f.factory, stWork, 10)
	expect(t, "Waiting", err, domain.ErrNoOrg)

	_, err = f.Store.Leases(ctx, f.factory, stWork)
	expect(t, "Leases", err, domain.ErrNoOrg)

	_, _, err = f.Store.ClaimNext(ctx, ports.Claim{FactoryID: f.factory, Station: work, Slot: 1, Now: t1, ExpiresAt: t1.Add(ttl)})
	expect(t, "ClaimNext", err, domain.ErrNoOrg)

	_, err = f.Store.HeartbeatLease(ctx, l, t1, t1.Add(ttl))
	expect(t, "HeartbeatLease", err, domain.ErrNoOrg)

	_, err = f.Store.RequeueExpiredLeases(ctx, t1.Add(time.Hour), 10)
	expect(t, "RequeueExpiredLeases", err, domain.ErrNoOrg)

	_, err = f.Store.StartRun(ctx, ports.NewRun{FactoryID: f.factory, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model, StartedAt: t1})
	expect(t, "StartRun", err, domain.ErrNoOrg)
	expect(t, "FinishRun", f.Store.FinishRun(ctx, run, domain.RunResult{Status: domain.RunSucceeded}, t1), domain.ErrNoOrg)

	_, err = f.Store.Runs(ctx, it.ID, 10)
	expect(t, "Runs", err, domain.ErrNoOrg)

	_, err = f.Store.Run(ctx, run)
	expect(t, "Run", err, domain.ErrNoOrg)

	_, err = f.Store.Transition(ctx, next(it, l, t1))
	expect(t, "Transition", err, domain.ErrNoOrg)
	expect(t, "AppendEvents", f.Store.AppendEvents(ctx, domain.Event{
		FactoryID: f.factory, ItemID: it.ID, At: t1, Kind: "note", Actor: domain.ActorEngine,
	}), domain.ErrNoOrg)

	_, err = f.Store.ItemEvents(ctx, it.ID, 0, 10)
	expect(t, "ItemEvents", err, domain.ErrNoOrg)

	_, err = f.Store.AppendNote(ctx, note(it, "x", t1), domain.LocalUser)
	expect(t, "AppendNote", err, domain.ErrNoOrg)

	_, err = f.Store.NotesSinceLastRun(ctx, it.ID)
	expect(t, "NotesSinceLastRun", err, domain.ErrNoOrg)

	_, err = f.Store.ItemByNumber(ctx, f.factory, it.Number)
	expect(t, "ItemByNumber", err, domain.ErrNoOrg)

	_, err = f.Store.ListItems(ctx, ports.ItemQuery{FactoryID: f.factory, Limit: 10})
	expect(t, "ListItems", err, domain.ErrNoOrg)

	_, err = f.Store.StationLoads(ctx, f.factory)
	expect(t, "StationLoads", err, domain.ErrNoOrg)

	_, err = f.Store.FlowMetrics(ctx, f.factory, t0, t1)
	expect(t, "FlowMetrics", err, domain.ErrNoOrg)

	_, err = f.Store.SpendSince(ctx, f.factory, t0)
	expect(t, "SpendSince", err, domain.ErrNoOrg)

	_, err = f.Store.Attention(ctx)
	expect(t, "Attention", err, domain.ErrNoOrg)

	_, err = f.Store.WaitingHuman(ctx, []domain.FactoryID{f.factory}, 10)
	expect(t, "WaitingHuman", err, domain.ErrNoOrg)

	if got := f.item(t, it.ID); got.State != domain.StateInProgress || got.OrgID != f.org {
		t.Errorf("item = %+v; want it unchanged, in progress in org %s", got, f.org)
	}
}

// An item with its events, lease and run in one organization does not exist for another, even
// when the other names its factory and ids.
func testOtherOrg(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{Title: "claimed"})
	f.create(t, domain.NewItem{Title: "waiting", CreatedAt: t0.Add(time.Second)})
	f.finish(t, f.create(t, domain.NewItem{Title: "finished", CreatedAt: t0.Add(2 * time.Second)}), t1, false)

	claimed, l := f.claim(t, work, 0, t1)
	run := f.startRun(t, claimed.ID, t1)
	other := f.otherOrg(t)
	ctx := other.ctx(t)

	if claimed.ID != it.ID || claimed.OrgID != f.org {
		t.Fatalf("claimed %+v; want %s in org %s", claimed, it.ID, f.org)
	}

	_, err := f.Store.Item(ctx, it.ID)
	expect(t, "Item from another org", err, ports.ErrNotFound)

	_, err = f.Store.ItemByNumber(ctx, f.factory, it.Number)
	expect(t, "ItemByNumber from another org", err, ports.ErrNotFound)

	_, _, err = f.Store.ClaimNext(ctx, ports.Claim{FactoryID: f.factory, Station: work, Slot: 1, Now: t1, ExpiresAt: t1.Add(ttl)})
	expect(t, "ClaimNext from another org", err, ports.ErrNoWork)

	_, err = f.Store.Transition(ctx, next(it, l, t1))
	expect(t, "Transition from another org", err, ports.ErrNotFound)

	_, err = f.Store.HeartbeatLease(ctx, l, t1, t1.Add(ttl))
	expect(t, "HeartbeatLease from another org", err, ports.ErrLeaseLost)
	expect(t, "FinishRun from another org", f.Store.FinishRun(ctx, run, domain.RunResult{Status: domain.RunSucceeded}, t1), ports.ErrNotFound)

	_, err = other.Store.Run(ctx, run)
	expect(t, "Run from another org", err, ports.ErrNotFound)

	_, err = f.Store.AppendNote(ctx, note(it, "x", t1), domain.LocalUser)
	expect(t, "AppendNote from another org", err, ports.ErrNotFound)

	other.seesNothingOf(t, f.factory, it.ID)

	if got := f.item(t, it.ID); got.State != domain.StateInProgress || len(f.runs(t, it.ID)) != 1 {
		t.Errorf("item = %+v; want it untouched by the other org", got)
	}
}

// seesNothingOf checks every read of the fixture's org finds nothing of factory and item.
func (f *fixture) seesNothingOf(t *testing.T, factory domain.FactoryID, item domain.ItemID) {
	t.Helper()

	f.seesNoItemsOf(t, factory)
	f.seesNoFlowOf(t, factory, item)
}

func (f *fixture) seesNoItemsOf(t *testing.T, factory domain.FactoryID) {
	t.Helper()

	ctx := f.ctx(t)

	waiting, err := f.Store.Waiting(ctx, factory, stWork, 10)
	if err != nil || len(waiting) != 0 {
		t.Errorf("Waiting = %+v, %v; want nothing", waiting, err)
	}

	leases, err := f.Store.Leases(ctx, factory, stWork)
	if err != nil || len(leases) != 0 {
		t.Errorf("Leases = %+v, %v; want nothing", leases, err)
	}

	items, err := f.Store.ListItems(ctx, ports.ItemQuery{FactoryID: factory, Limit: 10})
	if err != nil || len(items) != 0 {
		t.Errorf("ListItems = %+v, %v; want nothing", items, err)
	}

	waitingHuman, err := f.Store.WaitingHuman(ctx, []domain.FactoryID{factory}, 10)
	if err != nil || len(waitingHuman) != 0 {
		t.Errorf("WaitingHuman = %+v, %v; want nothing", waitingHuman, err)
	}

	attention, err := f.Store.Attention(ctx)
	if err != nil || slices.ContainsFunc(attention, func(a domain.Attention) bool { return a.Factory == factory }) {
		t.Errorf("Attention = %+v, %v; want nothing of factory %s", attention, err, factory)
	}
}

func (f *fixture) seesNoFlowOf(t *testing.T, factory domain.FactoryID, item domain.ItemID) {
	t.Helper()

	ctx := f.ctx(t)

	loads, err := f.Store.StationLoads(ctx, factory)
	if err != nil || len(loads) != 0 {
		t.Errorf("StationLoads = %+v, %v; want nothing", loads, err)
	}

	events, err := f.Store.ItemEvents(ctx, item, 0, 100)
	if err != nil || len(events) != 0 {
		t.Errorf("ItemEvents = %+v, %v; want nothing", events, err)
	}

	notes, err := f.Store.NotesSinceLastRun(ctx, item)
	if err != nil || len(notes) != 0 {
		t.Errorf("NotesSinceLastRun = %+v, %v; want nothing", notes, err)
	}

	m, err := f.Store.FlowMetrics(ctx, factory, t0, t1.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(m, domain.FlowMetrics{}) {
		t.Errorf("FlowMetrics = %+v, %v; want zero", m, err)
	}

	if runs := f.runs(t, item); len(runs) != 0 {
		t.Errorf("runs = %+v; want nothing", runs)
	}
}

func testFactoryOfAnotherOrg(f *fixture) {
	t := f.t
	other := f.otherOrg(t)

	_, err := f.Store.CreateItem(other.ctx(t), domain.NewItem{
		FactoryID: f.factory, Type: typeTask, Title: "x", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: t0,
	})
	expect(t, "CreateItem in another org's factory", err, ports.ErrNotFound)

	if it := other.create(t, domain.NewItem{}); it.OrgID != other.org || it.FactoryID != other.factory || it.Number != 1 {
		t.Errorf("CreateItem in the org's own factory = %+v; want #1 in org %s", it, other.org)
	}
}

func testRequeueOneOrg(f *fixture) {
	t := f.t
	other := f.otherOrg(t)

	f.create(t, domain.NewItem{})
	other.create(t, domain.NewItem{})

	_, mine := f.claim(t, work, 0, t1)
	_, theirs := other.claim(t, work, 0, t1)

	got, err := f.Store.RequeueExpiredLeases(f.ctx(t), t1.Add(time.Hour), 100)
	if err != nil || len(got) != 1 || got[0] != mine {
		t.Errorf("RequeueExpiredLeases = %+v, %v; want only this org's lease %+v", got, err, mine)
	}

	if ls := other.leases(t, stWork); len(ls) != 1 || ls[0] != theirs {
		t.Errorf("the other org's leases = %+v; want its lease still held", ls)
	}
}

// Another organization's runs are invisible to SpendSince, even when it names their factory.
func testSpendSinceOneOrg(f *fixture) {
	t := f.t
	other := f.otherOrg(t)

	f.spent(t, f.create(t, domain.NewItem{}), t1, 2)
	other.spent(t, other.create(t, domain.NewItem{}), t1, 3)

	mine, theirs, peek := f.spendSince(t, f.factory, t0), other.spendSince(t, other.factory, t0), other.spendSince(t, f.factory, t0)
	if mine != 2 || theirs != 3 || peek != 0 {
		t.Errorf("SpendSince = %v, %v and %v from the other org; want 2, 3 and 0", mine, theirs, peek)
	}
}
