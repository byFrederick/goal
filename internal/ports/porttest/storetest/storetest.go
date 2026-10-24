// Package storetest is the conformance suite for ports.Store. The in-memory fake in porttest
// and every Store adapter run it, so the engine's tests on the fake hold for the real store:
// same pull order, same claim and slot rules, same lease fencing, same compare-and-set, and the
// same tenancy: every call carries an organization and sees only that organization's rows.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Harness is one store under test and what the suite needs to see beyond the port.
type Harness struct {
	Store ports.Store
	// Flow reads the same store's flow statistics.
	Flow ports.FlowStats
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
	// NewFactory returns a factory of org the store accepts items for.
	NewFactory func(t *testing.T, org domain.OrgID) domain.FactoryID
	// SetSlotCaps sets org's slot cap and those of the named factories of org; 0 clears a cap.
	SetSlotCaps func(t *testing.T, org domain.OrgID, maxSlots int, factories map[domain.FactoryID]int)
	// LeaseSpans returns the lease spans of an item of org in the order they ended; no port reads
	// them one item at a time.
	LeaseSpans func(t *testing.T, org domain.OrgID, id domain.ItemID) []domain.LeaseSpan
}

// Run runs the suite. Tests run in parallel and call newHarness once each; every harness must
// be isolated from the others, because RequeueExpiredLeases spans all factories.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"CreateItem_NumbersEachFactoryFromOne", testCreateItem},
		{"UnknownIDs_NotFound", testNotFound},
		{"Waiting_PullOrder", testWaiting},
		{"ClaimNext_TakesFirstInPullOrder", testClaim},
		{"ClaimNext_LeasesToTheLaneAgent", testClaimRoute},
		{"ClaimNext_NoWork", testClaimNoWork},
		{"ClaimNext_SlotTaken", testClaimSlotTaken},
		{"HeartbeatLease_FencedByAcquiredAt", testHeartbeat},
		{"Transition_MovesAndReleases", testTransition},
		{"Transition_ConflictChangesNothing", testTransitionConflict},
		{"Transition_LeaseLostChangesNothing", testTransitionLeaseLost},
		{"RequeueExpiredLeases_RequeuesAndFailsRuns", testRequeue},
		{"RequeueExpiredLeases_FencesTheOldHolder", testRequeueFences},
		{"RequeueExpiredLeases_OldestFirstUpToLimit", testRequeueLimit},
		{"Runs_FinishOnceAndAddCost", testRuns},
		{"Runs_OldestFirstUpToLimitAndOneByID", testRunReads},
		{"AppendEvents_InOrder", testAppendEvents},
		{"Notes_AppendToOpenItemsReadSinceTheLatestRun", testNotes},
		{"Notes_RefusedOnClosedItems", testNotesOnClosedItems},
		{"ItemEvents_PagesInAppendOrder", testItemEvents},
		{"ItemByNumber_PerFactory", testItemByNumber},
		{"ListItems_NewestFirstFilteredAndPaged", testListItems},
		{"StationLoads_CountWaitingAndInProgress", testStationLoads},
		{"FlowMetrics_DoneLeadTimeReworkWIPAndSpend", testFlowMetrics},
		{"SpendSince_WindowBoundsAndFactoryIsolation", testSpendSince},
		{"Attention_CountsWaitingBlockedAndTheOpenAndon", testAttention},
		{"InboxItems_DecisionsBlockedAndExpediteNewestChangeFirst", testInboxItems},
		{"Concurrent_CreateItemNumbersAreUnique", testConcurrentCreate},
		{"Concurrent_ClaimsNeverShareASlotOrAnItem", testConcurrentClaims},
		{"Concurrent_RequeueOrTransitionWinsNeverBoth", testRequeueRace},
		{"Tenancy_NoOrgIsAnError", testNoOrg},
		{"Tenancy_OtherOrgSeesNothing", testOtherOrg},
		{"CreateItem_FactoryOfAnotherOrgNotFound", testFactoryOfAnotherOrg},
		{"RequeueExpiredLeases_OnlyTheCurrentOrg", testRequeueOneOrg},
		{"SpendSince_OnlyTheCurrentOrg", testSpendSinceOneOrg},
		{"ClaimNext_OrgAndFactorySlotCaps", testSlotCaps},
		{"Concurrent_ClaimsNeverExceedTheOrgCap", testConcurrentClaimsCapped},
		{"ItemSpans_FollowEveryTransition", testSpansFollowEveryTransition},
		{"ItemSpans_OnlyOnChangeAndOnlyTheOrgs", testSpansOnlyOnChangeAndOnlyTheOrgs},
		{"CurrentRun_RunningRunWithSlotAndTurns", testCurrentRun},
		{"Transition_RecordsTheLeaseSpanWithItsOutcome", testLeaseSpansOnTransition},
		{"RequeueExpiredLeases_RecordsExpiredLeaseSpans", testLeaseSpansOnRequeue},
		{"StationTimes_LeasesWaitingAndVerdictsInTheWindow", testStationTimes},
		{"LiveTurns_UsageOfEachRunningRun", testLiveTurns},
		{"FlowMetrics_ByTypeWithLongWaits", testFlowMetricsByTypeWithLongWaits},
		{"DoneByBucket_EqualBucketsEndingAtUntil", testDoneByBucket},
		{"AgentStats_TasksSendBacksRunsAndDenials", testAgentStats},
		{"FlowStats_NeedAnOrgAndSeeOnlyItsItems", testFlowStatsTenancy},
		{"Transition_TakeLeaseRemovesAnyLease", testTakeLease},
		{"ItemTimes_LeadTimeAndComponentsOfItemsDone", testItemTimes},
		{"Andon_OneOpenPullPerFactoryClearedWithANote", testAndonPullAndClear},
		{"Andon_PullsAreHistoryNewestFirst", testAndonHistory},
		{"Andon_OnlyTheOrgs", testAndonOnlyTheOrgs},
		{"Concurrent_AndonPullsOneOpens", testConcurrentAndonPulls},
		{"GateDecisions_LastVerdictsAfterAClear", testGateDecisions},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			org := h.NewOrg(t)
			tc.test(&fixture{Harness: h, t: t, org: org, factory: h.NewFactory(t, org)})
		})
	}
}

// Names and times the suite uses. Times are whole seconds: stores may keep microseconds only.
const (
	stWork   = "work"
	stNext   = "next"
	agWorker = "worker"
	// laneBackend routes to agBackend at a routed station.
	laneBackend = "backend"
	agBackend   = "backend-engineer"
	ttl         = time.Minute
	model       = "model-a"
	typeTask    = "task"
)

var (
	t0   = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	t1   = t0.Add(time.Hour)
	work = domain.Station{ID: stWork, Kind: domain.KindAgent, Agent: agWorker, Agents: 2}
)

// missing are well-formed ids that name nothing.
const (
	missingItem domain.ItemID = "00000000-0000-7000-8000-ffffffffffff"
	missingRun  domain.RunID  = "00000000-0000-7000-8000-fffffffffffe"
)

// fixture is one test's store, organization and factory. It holds the test's T so the test
// bodies are not mistaken for helpers.
type fixture struct {
	Harness

	t       *testing.T
	org     domain.OrgID
	factory domain.FactoryID
}

// ctx is the test's context bound to the fixture's organization.
func (f *fixture) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), f.org)
}

// newFactory returns another factory of the fixture's organization.
func (f *fixture) newFactory(t *testing.T) domain.FactoryID {
	t.Helper()

	return f.NewFactory(t, f.org)
}

// Events returns all of an item's events through the port, in append order.
func (f *fixture) Events(t *testing.T, id domain.ItemID) []domain.Event {
	t.Helper()

	events, err := f.Store.ItemEvents(f.ctx(t), id, 0, 10_000)
	if err != nil {
		t.Fatal(err)
	}

	return events
}

// runs returns an item's runs through the port, oldest first.
func (f *fixture) runs(t *testing.T, id domain.ItemID) []domain.Run {
	t.Helper()

	runs, err := f.Store.Runs(f.ctx(t), id, 100)
	if err != nil {
		t.Fatal(err)
	}

	return runs
}

// create adds a queued standard item at the work station unless n says otherwise.
func (f *fixture) create(t *testing.T, n domain.NewItem) domain.Item {
	t.Helper()

	if n.FactoryID == "" {
		n.FactoryID = f.factory
	}

	if n.Station == "" {
		n.Station = stWork
	}

	if n.State == "" {
		n.State = domain.StateQueued
	}

	if n.Class == "" {
		n.Class = domain.ClassStandard
	}

	if n.Type == "" {
		n.Type = typeTask
	}

	if n.Title == "" {
		n.Title = "an item"
	}

	if n.CreatedAt.IsZero() {
		n.CreatedAt = t0
	}

	it, err := f.Store.CreateItem(f.ctx(t), n)
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	return it
}

func (f *fixture) claim(t *testing.T, st domain.Station, slot int, now time.Time) (domain.Item, domain.Lease) {
	t.Helper()

	it, l, err := f.Store.ClaimNext(f.ctx(t), ports.Claim{FactoryID: f.factory, Station: st, Slot: slot, Now: now, ExpiresAt: now.Add(ttl)})
	if err != nil {
		t.Fatalf("ClaimNext slot %d: %v", slot, err)
	}

	return it, l
}

func (f *fixture) item(t *testing.T, id domain.ItemID) domain.Item {
	t.Helper()

	it, err := f.Store.Item(f.ctx(t), id)
	if err != nil {
		t.Fatalf("Item: %v", err)
	}

	return it
}

func (f *fixture) leases(t *testing.T, station string) []domain.Lease {
	t.Helper()

	ls, err := f.Store.Leases(f.ctx(t), f.factory, station)
	if err != nil {
		t.Fatalf("Leases: %v", err)
	}

	return ls
}

// next is a transition that finishes a claimed item: on to the next station, lease released as
// advanced.
func next(it domain.Item, l domain.Lease, at time.Time) ports.Transition {
	return ports.Transition{
		ItemID: it.ID, From: domain.Position{Station: stWork, State: domain.StateInProgress},
		To: domain.Position{Station: stNext, State: domain.StateQueued}, Lease: &l, Outcome: domain.LeaseAdvanced, At: at,
	}
}

func testCreateItem(f *fixture) {
	t := f.t

	a := f.create(t, domain.NewItem{Title: "a", Lane: laneBackend, Priority: 3, Body: "details"})
	b := f.create(t, domain.NewItem{Title: "b", ParentID: a.ID, Class: domain.ClassExpedite})
	c := f.create(t, domain.NewItem{FactoryID: f.newFactory(t), Title: "c"})

	if a.Number != 1 || b.Number != 2 || c.Number != 1 {
		t.Errorf("numbers = %d, %d, %d; want 1, 2 and 1 in the other factory", a.Number, b.Number, c.Number)
	}

	want := domain.Item{
		ID: a.ID, OrgID: f.org, FactoryID: f.factory, Number: 1, Type: typeTask, Lane: laneBackend, Title: "a", Body: "details",
		Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard, Priority: 3,
		Outputs: map[string]json.RawMessage{}, CreatedAt: t0, UpdatedAt: t0,
	}
	sameItem(t, a, want)
	sameItem(t, f.item(t, a.ID), want)

	if got := f.item(t, b.ID); got.ParentID != a.ID || got.Class != domain.ClassExpedite {
		t.Errorf("child = parent %q class %q; want parent %q, expedite", got.ParentID, got.Class, a.ID)
	}

	sameEvents(t, f.Events(t, a.ID), []domain.Event{{
		FactoryID: f.factory, ItemID: a.ID, At: t0, Kind: domain.EventCreated, Station: stWork, Actor: domain.ActorEngine,
	}})
}

func testNotFound(f *fixture) {
	t := f.t

	if _, err := f.Store.Item(f.ctx(t), missingItem); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Item = %v; want ErrNotFound", err)
	}

	_, err := f.Store.Transition(f.ctx(t), ports.Transition{
		ItemID: missingItem, From: domain.Position{Station: stWork, State: domain.StateQueued},
		To: domain.Position{Station: stNext, State: domain.StateQueued}, At: t1,
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Transition = %v; want ErrNotFound", err)
	}

	if err := f.Store.FinishRun(f.ctx(t), missingRun, domain.RunResult{Status: domain.RunSucceeded}, t1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("FinishRun = %v; want ErrNotFound", err)
	}
}

func testWaiting(f *fixture) {
	t := f.t

	sec := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }

	f.create(t, domain.NewItem{Title: "std-old", CreatedAt: sec(0)})
	f.create(t, domain.NewItem{Title: "std-high", Priority: 5, CreatedAt: sec(2)})
	f.create(t, domain.NewItem{Title: "expedite", Class: domain.ClassExpedite, CreatedAt: sec(3)})
	f.create(t, domain.NewItem{Title: "std-new", CreatedAt: sec(1)})
	f.create(t, domain.NewItem{Title: "tie-1", CreatedAt: sec(4)})
	f.create(t, domain.NewItem{Title: "tie-2", CreatedAt: sec(4)})
	f.create(t, domain.NewItem{Title: "ready", State: domain.StateReady, CreatedAt: sec(5)})
	// Not waiting here: other states, another station, another factory.
	f.create(t, domain.NewItem{Title: "busy", State: domain.StateInProgress, Priority: 9})
	f.create(t, domain.NewItem{Title: "blocked", State: domain.StateBlocked, Priority: 9})
	f.create(t, domain.NewItem{Title: "elsewhere", Station: stNext, Priority: 9})
	f.create(t, domain.NewItem{Title: "other factory", FactoryID: f.newFactory(t), Priority: 9})

	want := []string{"expedite", "std-high", "std-old", "std-new", "tie-1", "tie-2", "ready"}

	got, err := f.Store.Waiting(f.ctx(t), f.factory, stWork, 100)
	if err != nil {
		t.Fatal(err)
	}

	if titles := titlesOf(got); !reflect.DeepEqual(titles, want) {
		t.Errorf("Waiting = %v; want %v", titles, want)
	}

	got, err = f.Store.Waiting(f.ctx(t), f.factory, stWork, 2)
	if err != nil {
		t.Fatal(err)
	}

	if titles := titlesOf(got); !reflect.DeepEqual(titles, want[:2]) {
		t.Errorf("Waiting limit 2 = %v; want %v", titles, want[:2])
	}
}

func testClaim(f *fixture) {
	t := f.t

	f.create(t, domain.NewItem{Title: "standard"})
	exp := f.create(t, domain.NewItem{Title: "expedite", Class: domain.ClassExpedite, CreatedAt: t0.Add(time.Second)})

	it, l := f.claim(t, work, 1, t1)

	want := exp
	want.State, want.UpdatedAt = domain.StateInProgress, t1
	sameItem(t, it, want)
	sameItem(t, f.item(t, exp.ID), want)

	wantLease := domain.Lease{
		ItemID: exp.ID, FactoryID: f.factory, Station: stWork, Agent: agWorker, Slot: 1,
		AcquiredAt: t1, HeartbeatAt: t1, ExpiresAt: t1.Add(ttl),
	}
	if l != wantLease {
		t.Errorf("lease = %+v; want %+v", l, wantLease)
	}

	if ls := f.leases(t, stWork); len(ls) != 1 || ls[0] != wantLease {
		t.Errorf("Leases = %+v; want [%+v]", ls, wantLease)
	}

	events := f.Events(t, exp.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: exp.ID, At: t1, Kind: domain.EventClaimed, Station: stWork,
		Actor: domain.ActorEngine, Data: map[string]any{"slot": 1, "agent": agWorker},
	}})
}

func testClaimRoute(f *fixture) {
	t := f.t

	routed := domain.Station{ID: stWork, Kind: domain.KindAgent, Route: map[string]string{laneBackend: agBackend}, Agents: 2}

	f.create(t, domain.NewItem{Title: "routed", Lane: laneBackend})
	f.create(t, domain.NewItem{Title: "unrouted", Lane: "mobile", CreatedAt: t0.Add(time.Second)})

	if _, l := f.claim(t, routed, 0, t1); l.Agent != agBackend {
		t.Errorf("routed lease agent = %q; want backend-engineer", l.Agent)
	}

	if _, l := f.claim(t, routed, 1, t1); l.Agent != "" {
		t.Errorf("unrouted lease agent = %q; want empty", l.Agent)
	}
}

func testClaimNoWork(f *fixture) {
	t := f.t

	f.create(t, domain.NewItem{State: domain.StateBlocked})
	f.create(t, domain.NewItem{Station: stNext})

	_, _, err := f.Store.ClaimNext(f.ctx(t), ports.Claim{FactoryID: f.factory, Station: work, Slot: 0, Now: t1, ExpiresAt: t1.Add(ttl)})
	if !errors.Is(err, ports.ErrNoWork) {
		t.Fatalf("ClaimNext = %v; want ErrNoWork", err)
	}

	if ls := f.leases(t, stWork); len(ls) != 0 {
		t.Errorf("Leases = %+v; want none", ls)
	}
}

func testClaimSlotTaken(f *fixture) {
	t := f.t

	f.create(t, domain.NewItem{Title: "first"})
	second := f.create(t, domain.NewItem{Title: "second", CreatedAt: t0.Add(time.Second)})

	f.claim(t, work, 0, t1)

	claim := func(factory domain.FactoryID, st domain.Station, slot int) error {
		_, _, err := f.Store.ClaimNext(f.ctx(t), ports.Claim{FactoryID: factory, Station: st, Slot: slot, Now: t1, ExpiresAt: t1.Add(ttl)})
		return err
	}

	if err := claim(f.factory, work, 0); !errors.Is(err, ports.ErrSlotTaken) {
		t.Fatalf("second claim on slot 0 = %v; want ErrSlotTaken", err)
	}

	if got := f.item(t, second.ID); got.State != domain.StateQueued {
		t.Errorf("after a refused claim the next item is %s; want queued", got.State)
	}

	f.claim(t, work, 1, t1)

	// A taken slot is reported before the empty queue.
	if err := claim(f.factory, work, 0); !errors.Is(err, ports.ErrSlotTaken) {
		t.Errorf("claim on a taken slot with no work = %v; want ErrSlotTaken", err)
	}

	// Slots belong to one station of one factory.
	f.create(t, domain.NewItem{Station: stNext})

	if err := claim(f.factory, domain.Station{ID: stNext, Kind: domain.KindAgent, Agent: agWorker}, 0); err != nil {
		t.Errorf("slot 0 at another station = %v; want a claim", err)
	}

	other := f.newFactory(t)
	f.create(t, domain.NewItem{FactoryID: other})

	if err := claim(other, work, 0); err != nil {
		t.Errorf("slot 0 in another factory = %v; want a claim", err)
	}
}

func testHeartbeat(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	_, l := f.claim(t, work, 0, t1)

	beat, err := f.Store.HeartbeatLease(f.ctx(t), l, t1.Add(20*time.Second), t1.Add(80*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	want := l
	want.HeartbeatAt, want.ExpiresAt = t1.Add(20*time.Second), t1.Add(80*time.Second)

	if beat != want {
		t.Errorf("HeartbeatLease = %+v; want %+v", beat, want)
	}

	if ls := f.leases(t, stWork); len(ls) != 1 || ls[0] != want {
		t.Errorf("Leases = %+v; want [%+v]", ls, want)
	}

	stale := l
	stale.AcquiredAt = l.AcquiredAt.Add(-time.Second)

	if _, err := f.Store.HeartbeatLease(f.ctx(t), stale, t1, t1.Add(ttl)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("heartbeat with another AcquiredAt = %v; want ErrLeaseLost", err)
	}

	if _, err := f.Store.Transition(f.ctx(t), next(it, l, t1)); err != nil {
		t.Fatal(err)
	}

	if _, err := f.Store.HeartbeatLease(f.ctx(t), l, t1, t1.Add(ttl)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("heartbeat after release = %v; want ErrLeaseLost", err)
	}
}

func testTransition(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	_, l := f.claim(t, work, 0, t1)
	t2, t3 := t1.Add(time.Minute), t1.Add(2*time.Minute)

	tr := next(it, l, t2)
	tr.Lane, tr.OutputStation, tr.Output, tr.Rework = laneBackend, stWork, json.RawMessage(`{"lane": "backend", "n": 1}`), true
	tr.Events = []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t2, Kind: domain.EventMoved, Station: stWork, Actor: agWorker,
		Data: map[string]any{"from": stWork, "to": stNext},
	}}

	moved, err := f.Store.Transition(f.ctx(t), tr)
	if err != nil {
		t.Fatal(err)
	}

	want := it
	want.Station, want.State, want.Lane, want.ReworkCount, want.UpdatedAt = stNext, domain.StateQueued, laneBackend, 1, t2
	want.Outputs = map[string]json.RawMessage{stWork: tr.Output}
	sameItem(t, moved, want)
	sameItem(t, f.item(t, it.ID), want)

	if ls := f.leases(t, stWork); len(ls) != 0 {
		t.Errorf("Leases after transition = %+v; want none", ls)
	}

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-1:], tr.Events)

	// No lease, no lane, no output: those stay; reaching done stores DoneAt.
	done, err := f.Store.Transition(f.ctx(t), ports.Transition{
		ItemID: it.ID, From: domain.Position{Station: stNext, State: domain.StateQueued},
		To: domain.Position{Station: "done", State: domain.StateDone}, At: t3, DoneAt: t3,
	})
	if err != nil {
		t.Fatal(err)
	}

	want.Station, want.State, want.UpdatedAt, want.DoneAt = "done", domain.StateDone, t3, t3
	sameItem(t, done, want)
}

func testTransitionConflict(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	claimed, l := f.claim(t, work, 0, t1)
	before := len(f.Events(t, it.ID))

	tr := next(it, l, t1.Add(time.Minute))
	tr.From.State = domain.StateQueued
	tr.Events = []domain.Event{{FactoryID: f.factory, ItemID: it.ID, At: t1, Kind: domain.EventMoved, Actor: agWorker}}

	if _, err := f.Store.Transition(f.ctx(t), tr); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("Transition from the wrong state = %v; want ErrConflict", err)
	}

	sameItem(t, f.item(t, it.ID), claimed)

	if ls := f.leases(t, stWork); len(ls) != 1 || ls[0] != l {
		t.Errorf("Leases = %+v; want the lease still held", ls)
	}

	if n := len(f.Events(t, it.ID)); n != before {
		t.Errorf("events = %d; want %d (none appended)", n, before)
	}
}

func testTransitionLeaseLost(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	claimed, l := f.claim(t, work, 0, t1)

	stale := l
	stale.AcquiredAt = l.AcquiredAt.Add(-time.Second)

	if _, err := f.Store.Transition(f.ctx(t), next(it, stale, t1)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Fatalf("Transition with another AcquiredAt = %v; want ErrLeaseLost", err)
	}

	// The lease is checked before the position.
	tr := next(it, stale, t1)
	tr.From.Station = stNext

	if _, err := f.Store.Transition(f.ctx(t), tr); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("Transition with a lost lease and a wrong position = %v; want ErrLeaseLost", err)
	}

	sameItem(t, f.item(t, it.ID), claimed)

	if ls := f.leases(t, stWork); len(ls) != 1 || ls[0] != l {
		t.Errorf("Leases = %+v; want the lease still held", ls)
	}
}

func testRequeue(f *fixture) {
	t := f.t

	a := f.create(t, domain.NewItem{Title: "a"})
	b := f.create(t, domain.NewItem{Title: "b", CreatedAt: t0.Add(time.Second)})
	f.create(t, domain.NewItem{Title: "c", CreatedAt: t0.Add(2 * time.Second)})

	_, la := f.claim(t, work, 0, t1)
	_, lb := f.claim(t, work, 1, t1.Add(30*time.Second))
	runA := f.startRun(t, a.ID, t1)
	f.startRun(t, b.ID, t1)

	now := t1.Add(ttl) // a's lease expires exactly now

	got, err := f.Store.RequeueExpiredLeases(f.ctx(t), now, 100)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0] != la {
		t.Fatalf("RequeueExpiredLeases = %+v; want [%+v]", got, la)
	}

	if it := f.item(t, a.ID); it.State != domain.StateQueued || it.Station != stWork || !it.UpdatedAt.Equal(now) {
		t.Errorf("requeued item is %s at %s, updated %s; want queued at %s, updated %s", it.State, it.Station, it.UpdatedAt, stWork, now)
	}

	if ls := f.leases(t, stWork); len(ls) != 1 || ls[0] != lb {
		t.Errorf("Leases = %+v; want only b's", ls)
	}

	sameRuns(t, f.runs(t, a.ID), []domain.Run{{
		ID: runA, FactoryID: f.factory, ItemID: a.ID, Station: stWork, Agent: agWorker, Model: model,
		Result: domain.RunResult{Status: domain.RunFailed, Error: "lease expired"}, StartedAt: t1, EndedAt: now,
	}})

	if runs := f.runs(t, b.ID); len(runs) != 1 || runs[0].Result.Status != domain.RunRunning {
		t.Errorf("b's runs = %+v; want still running", runs)
	}

	events := f.Events(t, a.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: a.ID, At: now, Kind: domain.EventLeaseExpired, Station: stWork,
		Actor: domain.ActorEngine, Data: map[string]any{"agent": agWorker},
	}})
}

// testRequeueFences checks that the holder of a requeued lease is fenced off, before and after
// the item is claimed again.
func testRequeueFences(f *fixture) {
	t := f.t

	a := f.create(t, domain.NewItem{})
	_, la := f.claim(t, work, 0, t1)
	now := t1.Add(ttl)

	if got, err := f.Store.RequeueExpiredLeases(f.ctx(t), now, 100); err != nil || len(got) != 1 {
		t.Fatalf("RequeueExpiredLeases = %+v, %v; want one lease", got, err)
	}

	if _, err := f.Store.HeartbeatLease(f.ctx(t), la, now, now.Add(ttl)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("old holder's heartbeat = %v; want ErrLeaseLost", err)
	}

	reclaimed, l2 := f.claim(t, work, 0, now.Add(time.Second))
	if reclaimed.ID != a.ID || l2.AcquiredAt.Equal(la.AcquiredAt) {
		t.Fatalf("reclaimed %s with AcquiredAt %s; want %s with a new AcquiredAt", reclaimed.ID, l2.AcquiredAt, a.ID)
	}

	if _, err := f.Store.Transition(f.ctx(t), next(a, la, now)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("old holder's transition = %v; want ErrLeaseLost", err)
	}
}

func testRequeueLimit(f *fixture) {
	t := f.t

	f.create(t, domain.NewItem{Title: "a"})
	f.create(t, domain.NewItem{Title: "b", CreatedAt: t0.Add(time.Second)})

	_, late := f.claim(t, work, 0, t1.Add(time.Second))
	_, early := f.claim(t, work, 1, t1)

	got, err := f.Store.RequeueExpiredLeases(f.ctx(t), t1.Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0] != early {
		t.Fatalf("RequeueExpiredLeases limit 1 = %+v; want the earliest expiry [%+v]", got, early)
	}

	got, err = f.Store.RequeueExpiredLeases(f.ctx(t), t1.Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0] != late {
		t.Errorf("second sweep = %+v; want [%+v]", got, late)
	}

	if got, err := f.Store.RequeueExpiredLeases(f.ctx(t), t1.Add(time.Hour), 1); err != nil || len(got) != 0 {
		t.Errorf("third sweep = %+v, %v; want nothing", got, err)
	}
}

func (f *fixture) startRun(t *testing.T, item domain.ItemID, at time.Time) domain.RunID {
	t.Helper()

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: f.factory, ItemID: item, Station: stWork, Agent: agWorker, Model: model, StartedAt: at,
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	return id
}

func testRuns(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	f.claim(t, work, 0, t1)
	t2 := t1.Add(time.Minute)

	id := f.startRun(t, it.ID, t1)
	want := domain.Run{
		ID: id, FactoryID: f.factory, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model,
		Result: domain.RunResult{Status: domain.RunRunning}, StartedAt: t1,
	}
	sameRuns(t, f.runs(t, it.ID), []domain.Run{want})

	res := domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.25, Turns: 3, InputTokens: 100, OutputTokens: 50, SessionID: "s-1"}
	if err := f.Store.FinishRun(f.ctx(t), id, res, t2); err != nil {
		t.Fatal(err)
	}

	want.Result, want.EndedAt = res, t2
	sameRuns(t, f.runs(t, it.ID), []domain.Run{want})

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-2:], []domain.Event{
		{FactoryID: f.factory, ItemID: it.ID, RunID: id, At: t1, Kind: domain.EventRunStarted, Station: stWork, Actor: agWorker},
		{
			FactoryID: f.factory, ItemID: it.ID, RunID: id, At: t2, Kind: domain.EventRunFinished, Station: stWork,
			Actor: agWorker, Data: map[string]any{"status": "succeeded", "error": "", "cost_usd": 0.25, "turns": 3},
		},
	})

	// Finishing again changes nothing.
	if err := f.Store.FinishRun(f.ctx(t), id, domain.RunResult{Status: domain.RunFailed, CostUSD: 1}, t2.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	sameRuns(t, f.runs(t, it.ID), []domain.Run{want})

	if n := len(f.Events(t, it.ID)); n != len(events) {
		t.Errorf("events after a second finish = %d; want %d", n, len(events))
	}

	second := f.startRun(t, it.ID, t2)
	if err := f.Store.FinishRun(f.ctx(t), second, domain.RunResult{Status: domain.RunFailed, CostUSD: 0.5, Error: "boom"}, t2); err != nil {
		t.Fatal(err)
	}

	if got := f.item(t, it.ID).CostUSD; got != 0.75 {
		t.Errorf("item cost = %v; want 0.75, the sum of its finished runs", got)
	}
}

func testRunReads(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	other := f.create(t, domain.NewItem{Title: "other"})
	t2 := t1.Add(time.Minute)

	// Started out of order: the later start is recorded first.
	late := f.startRun(t, it.ID, t2)
	early := f.startRun(t, it.ID, t1)
	f.startRun(t, other.ID, t1)

	res := domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.5, Turns: 4, InputTokens: 10, OutputTokens: 20, SessionID: "s-2"}
	if err := f.Store.FinishRun(f.ctx(t), early, res, t2); err != nil {
		t.Fatal(err)
	}

	first := domain.Run{
		ID: early, FactoryID: f.factory, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model,
		Result: res, StartedAt: t1, EndedAt: t2,
	}
	second := domain.Run{
		ID: late, FactoryID: f.factory, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model,
		Result: domain.RunResult{Status: domain.RunRunning}, StartedAt: t2,
	}

	got, err := f.Store.Runs(f.ctx(t), it.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	sameRuns(t, got, []domain.Run{first, second})

	if got, err = f.Store.Runs(f.ctx(t), it.ID, 1); err != nil {
		t.Fatal(err)
	}

	sameRuns(t, got, []domain.Run{first})

	if got, err = f.Store.Runs(f.ctx(t), missingItem, 10); err != nil || len(got) != 0 {
		t.Errorf("Runs of an unknown item = %+v, %v; want none", got, err)
	}

	run, err := f.Store.Run(f.ctx(t), late)
	if err != nil {
		t.Fatal(err)
	}

	sameRuns(t, []domain.Run{run}, []domain.Run{second})

	_, err = f.Store.Run(f.ctx(t), missingRun)
	expect(t, "Run unknown", err, ports.ErrNotFound)
}

func testAppendEvents(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	run := f.startRun(t, it.ID, t1)

	events := []domain.Event{
		{
			FactoryID: f.factory, ItemID: it.ID, RunID: run, At: t1, Kind: "tool_call", Station: stWork, Actor: agWorker,
			Data: map[string]any{"tool": "search", "args": map[string]any{"q": "acme", "n": 2}},
		},
		{FactoryID: f.factory, ItemID: it.ID, At: t1.Add(time.Second), Kind: domain.EventBlocked, Actor: "human"},
	}

	if err := f.Store.AppendEvents(f.ctx(t), events...); err != nil {
		t.Fatal(err)
	}

	got := f.Events(t, it.ID)
	sameEvents(t, got[len(got)-2:], events)

	if got[len(got)-2].ID >= got[len(got)-1].ID {
		t.Errorf("event ids %d, %d; want increasing in append order", got[len(got)-2].ID, got[len(got)-1].ID)
	}
}

func testItemEvents(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	other := f.create(t, domain.NewItem{})

	for i := range 5 {
		ev := domain.Event{FactoryID: f.factory, ItemID: it.ID, At: t1, Kind: "note", Actor: agWorker, Data: map[string]any{"i": i}}
		if err := f.Store.AppendEvents(f.ctx(t), ev, domain.Event{FactoryID: f.factory, ItemID: other.ID, At: t1, Kind: "note", Actor: agWorker}); err != nil {
			t.Fatal(err)
		}
	}

	all := f.Events(t, it.ID) // created, then the five notes
	if len(all) != 6 {
		t.Fatalf("%d events, want 6 (created and five notes)", len(all))
	}

	page, err := f.Store.ItemEvents(f.ctx(t), it.ID, all[2].ID, 2)
	if err != nil {
		t.Fatal(err)
	}

	sameEvents(t, page, all[3:5])

	for _, e := range page {
		if e.ID <= all[2].ID {
			t.Errorf("event id %d is not after %d", e.ID, all[2].ID)
		}
	}

	rest, err := f.Store.ItemEvents(f.ctx(t), it.ID, all[5].ID, 10)
	if err != nil || len(rest) != 0 {
		t.Errorf("events after the last = %v, %v; want none", rest, err)
	}

	none, err := f.Store.ItemEvents(f.ctx(t), missingItem, 0, 10)
	if err != nil || len(none) != 0 {
		t.Errorf("events of an unknown item = %v, %v; want none and no error", none, err)
	}
}

func testConcurrentCreate(f *fixture) {
	t := f.t

	const n = 20

	numbers := make(chan int64, n)

	var wg sync.WaitGroup

	for range n {
		wg.Go(func() {
			it, err := f.Store.CreateItem(f.ctx(t), domain.NewItem{
				FactoryID: f.factory, Type: typeTask, Title: "x", Station: stWork, State: domain.StateQueued,
				Class: domain.ClassStandard, CreatedAt: t0,
			})
			if err != nil {
				t.Error(err)
				return
			}

			numbers <- it.Number
		})
	}

	wg.Wait()
	close(numbers)

	seen := map[int64]bool{}
	for num := range numbers {
		seen[num] = true
	}

	for num := int64(1); num <= n; num++ {
		if !seen[num] {
			t.Errorf("numbers = %v; want 1..%d each once", seen, n)
			break
		}
	}
}

// testConcurrentClaims runs several instances against one station until every item has been
// claimed and moved on, checking that no slot is held twice and no item is claimed twice.
func testConcurrentClaims(f *fixture) {
	t := f.t

	const items, instances = 30, 8

	r := &claimRace{f: f, st: work, items: items, held: make([]atomic.Bool, 3), claimed: map[domain.ItemID]int{}}
	r.st.Agents = len(r.held)

	for i := range items {
		f.create(t, domain.NewItem{CreatedAt: t0.Add(time.Duration(i) * time.Second)})
	}

	ctx, cancel := context.WithTimeout(f.ctx(t), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for range instances {
		wg.Go(func() { r.instance(ctx) })
	}

	wg.Wait()

	if len(r.claimed) != items {
		t.Errorf("claimed %d distinct items; want %d", len(r.claimed), items)
	}

	for id, n := range r.claimed {
		if n != 1 {
			t.Errorf("item %s claimed %d times", id, n)
		}
	}

	if w, err := f.Store.Waiting(f.ctx(t), f.factory, stNext, items+1); err != nil || len(w) != items {
		t.Errorf("Waiting at %s = %d, %v; want all %d items", stNext, len(w), err, items)
	}
}

// claimRace is what the instances of testConcurrentClaims share.
type claimRace struct {
	f     *fixture
	st    domain.Station
	items int64
	// held marks the slots an instance holds a lease on.
	held    []atomic.Bool
	done    atomic.Int64
	mu      sync.Mutex
	claimed map[domain.ItemID]int
}

// instance claims items and moves them on, as one goald instance would, until all are done.
func (r *claimRace) instance(ctx context.Context) {
	t, capacity := r.f.t, len(r.held)

	for slot := 0; r.done.Load() < r.items && ctx.Err() == nil; slot = (slot + 1) % capacity {
		it, l, err := r.f.Store.ClaimNext(ctx, ports.Claim{FactoryID: r.f.factory, Station: r.st, Slot: slot, Now: t1, ExpiresAt: t1.Add(ttl)})

		switch {
		case errors.Is(err, ports.ErrNoWork), errors.Is(err, ports.ErrSlotTaken):
			continue
		case err != nil:
			t.Error(err)
			return
		}

		if !r.held[slot].CompareAndSwap(false, true) {
			t.Errorf("slot %d granted while held", slot)
		}

		r.mu.Lock()
		r.claimed[it.ID]++
		r.mu.Unlock()

		if ls, err := r.f.Store.Leases(ctx, r.f.factory, stWork); err != nil || len(ls) > capacity {
			t.Errorf("Leases = %d, %v; want at most %d", len(ls), err, capacity)
		}

		r.held[slot].Store(false)

		if _, err := r.f.Store.Transition(ctx, next(it, l, t1)); err != nil {
			t.Error(err)
			return
		}

		r.done.Add(1)
	}
}

// testRequeueRace sweeps an expired lease while its holder moves the item on: exactly one wins.
func testRequeueRace(f *fixture) {
	t := f.t

	for i := range 10 {
		created := t0.Add(time.Duration(i) * time.Second)
		f.create(t, domain.NewItem{CreatedAt: created})
		it, l := f.claim(t, work, 0, t1)

		var (
			requeued []domain.Lease
			sweepErr error
			moveErr  error
			wg       sync.WaitGroup
		)

		wg.Go(func() { requeued, sweepErr = f.Store.RequeueExpiredLeases(f.ctx(t), t1.Add(time.Hour), 10) })
		wg.Go(func() { _, moveErr = f.Store.Transition(f.ctx(t), next(it, l, t1)) })
		wg.Wait()

		if sweepErr != nil {
			t.Fatal(sweepErr)
		}

		swept := len(requeued) == 1
		moved := moveErr == nil

		if !moved && !errors.Is(moveErr, ports.ErrLeaseLost) {
			t.Fatalf("Transition = %v; want success or ErrLeaseLost", moveErr)
		}

		if swept == moved {
			t.Fatalf("round %d: swept %v and moved %v; want exactly one", i, swept, moved)
		}

		got := f.item(t, it.ID)
		if (moved && got.Station != stNext) || (swept && got.State != domain.StateQueued) {
			t.Fatalf("round %d: item is %s at %s after swept %v, moved %v", i, got.State, got.Station, swept, moved)
		}

		if swept {
			// Park the requeued item so the next round claims a fresh one.
			_, l := f.claim(t, work, 0, t1)
			if _, err := f.Store.Transition(f.ctx(t), next(it, l, t1)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func titlesOf(items []domain.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}

	return out
}

// sameItem compares items with outputs compared as JSON values: stores may reformat JSON.
func sameItem(t *testing.T, got, want domain.Item) {
	t.Helper()

	gotOut, wantOut := normalJSON(t, got.Outputs), normalJSON(t, want.Outputs)
	got.Outputs, want.Outputs = nil, nil

	if !reflect.DeepEqual(got, want) || gotOut != wantOut {
		t.Errorf("item =\n  %+v outputs %s\nwant\n  %+v outputs %s", got, gotOut, want, wantOut)
	}
}

// sameEvents compares events ignoring store-assigned ids, with data compared as JSON values.
func sameEvents(t *testing.T, got, want []domain.Event) {
	t.Helper()

	norm := func(events []domain.Event) []string {
		out := make([]string, 0, len(events))

		for _, e := range events {
			data := e.Data
			if data == nil {
				data = map[string]any{}
			}

			e.ID, e.Data, e.At = 0, nil, e.At.UTC()

			b, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}

			out = append(out, string(b)+" data "+normalJSON(t, data))
		}

		return out
	}

	if g, w := norm(got), norm(want); !reflect.DeepEqual(g, w) {
		t.Errorf("events =\n  %v\nwant\n  %v", g, w)
	}
}

// sameRuns compares runs without their outputs, which stores do not keep.
func sameRuns(t *testing.T, got, want []domain.Run) {
	t.Helper()

	strip := func(runs []domain.Run) []domain.Run {
		out := make([]domain.Run, 0, len(runs))

		for _, r := range runs {
			r.Result.Output = nil
			out = append(out, r)
		}

		return out
	}

	if g, w := strip(got), strip(want); !reflect.DeepEqual(g, w) {
		t.Errorf("runs =\n  %+v\nwant\n  %+v", g, w)
	}
}

// normalJSON re-encodes v through a generic value, so key order and spacing do not matter.
func normalJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}

	if b, err = json.Marshal(generic); err != nil {
		t.Fatal(err)
	}

	return string(b)
}
