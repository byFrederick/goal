package intaketest

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// checkTests are the suite's cases for ADR 0040 decision 19: a checked firing queues a check, one
// per trigger at a time, which a claim leases, a heartbeat extends, a finish ends fenced by its
// token, and an expired lease fails; and what the next check is given.
var checkTests = []struct {
	name string
	test func(f *fixture)
}{
	{"Check_OneQueuedOrRunningPerTrigger", testStillChecking},
	{"Check_ClaimLeasesAndFinishIsFenced", testClaimCheck},
	{"Check_ExpiredLeaseFails", testExpireChecks},
	{"Check_InputListsOpenAndClosedItems", testCheckInput},
	{"Check_ApplyFilesNotesAndSkipsByKey", testApplyCheck},
	{"Check_ApplyLimits", testApplyLimits},
	{"Check_TryAppliesNothing", testTryCheck},
	{"Check_AlertsFailuresAndLimits", testCheckAlerts},
	{"OtherOrg_ReadsNoTriggerKey", testIsolatedKeys},
	{"Check_MemoryCountsAndClear", testCheckMemory},
	{"OtherOrg_ClearsNoState", testIsolatedClear},
	{"Check_ItemCheckKeyIsTheKeysLastItem", testItemCheckKey},
}

// checkFiring is a schedule firing of nightly for the instant at, which queues a check.
func (f *fixture) checkFiring(at time.Time) domain.TriggerFiring {
	f.t.Helper()

	got, fresh, err := f.in.Fire(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: nightly, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: at.UTC().Format(time.RFC3339), Outcome: domain.FiringChecked, CheckState: domain.CheckQueued, At: at,
	}, nil, domain.FireLimits{})
	if err != nil || !fresh {
		f.t.Fatalf("Fire a checked firing at %s = %+v, %v, %v", at, got, fresh, err)
	}

	return got
}

// claimCheck claims the factory's next check at now for a minute.
func (f *fixture) claimCheck(now time.Time) (domain.TriggerFiring, domain.CheckLease) {
	f.t.Helper()

	got, l, err := f.in.ClaimCheck(f.ctx(), f.factory, "goald-a", now, time.Minute)
	if err != nil {
		f.t.Fatalf("ClaimCheck: %v", err)
	}

	return got, l
}

// A checked firing is queued; another of the same trigger while it is queued or running is
// skipped "still checking", and once it finished the next one is queued again.
func testStillChecking(f *fixture) {
	t := f.t

	first := f.checkFiring(t0)
	if first.Outcome != domain.FiringChecked || first.CheckState != domain.CheckQueued {
		t.Fatalf("first firing = %+v, want checked and queued", first)
	}

	if second := f.checkFiring(t0.Add(5 * time.Minute)); second.Outcome != domain.FiringSkipped || second.Reason != domain.StillChecking || second.CheckState != "" {
		t.Errorf("firing while queued = %+v, want skipped, %q", second, domain.StillChecking)
	}

	_, l := f.claimCheck(t0.Add(6 * time.Minute))

	if third := f.checkFiring(t0.Add(10 * time.Minute)); third.Outcome != domain.FiringSkipped || third.Reason != domain.StillChecking {
		t.Errorf("firing while running = %+v, want skipped, %q", third, domain.StillChecking)
	}

	if err := f.in.FinishCheck(f.ctx(), l, domain.CheckResult{State: domain.CheckFailed, Reason: "timed out after 5m0s", At: t0.Add(11 * time.Minute)}); err != nil {
		t.Fatalf("FinishCheck: %v", err)
	}

	if next := f.checkFiring(t0.Add(15 * time.Minute)); next.Outcome != domain.FiringChecked || next.CheckState != domain.CheckQueued {
		t.Errorf("firing after the check failed = %+v, want queued", next)
	}
}

// A claim leases the oldest queued check, and none is left to claim; a heartbeat extends it; a
// finish under another token is ErrLeaseLost; a done finish records its run, cost and decisions
// and saves the state, which a later failed check keeps.
func testClaimCheck(f *fixture) {
	t := f.t
	queued := f.checkFiring(t0)

	got, l := f.claimCheck(t0.Add(time.Second))
	if got.ID != queued.ID || got.CheckState != domain.CheckRunning || l.Token == "" || l.FiringID != queued.ID || !l.Until.Equal(t0.Add(time.Second+time.Minute)) {
		t.Fatalf("ClaimCheck = %+v, %+v; want the queued check running for a minute", got, l)
	}

	if _, _, err := f.in.ClaimCheck(f.ctx(), f.factory, "goald-b", t0.Add(2*time.Second), time.Minute); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("second ClaimCheck = %v, want ErrNotFound", err)
	}

	if err := f.in.HeartbeatCheck(f.ctx(), l, t0.Add(3*time.Minute)); err != nil {
		t.Errorf("HeartbeatCheck: %v", err)
	}

	stolen := l
	stolen.Token = "019a0000-0000-7000-8000-000000000001"

	done := domain.CheckResult{
		State: domain.CheckDone, RunID: f.checkRun(), CostUSD: 0.04, Decisions: json.RawMessage(`{"file":[],"note":[],"skip":[]}`),
		Saved: json.RawMessage(`{"since":"2026-10-01T12:00:00Z"}`), At: t0.Add(time.Minute),
	}

	if err := f.in.FinishCheck(f.ctx(), stolen, done); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("FinishCheck under another token = %v, want ErrLeaseLost", err)
	}

	f.finishDone(queued, l, done)
}

// finishDone finishes queued's check under l with done, reads it back done with its run, cost and
// decisions, and checks that a failed check after it keeps its state and decisions as the input.
func (f *fixture) finishDone(queued domain.TriggerFiring, l domain.CheckLease, done domain.CheckResult) {
	t := f.t

	if err := f.in.FinishCheck(f.ctx(), l, done); err != nil {
		t.Fatalf("FinishCheck: %v", err)
	}

	read, ok, err := f.in.Firing(f.ctx(), f.factory, nightly, queued.IdempotencyKey)
	if err != nil || !ok || read.CheckState != domain.CheckDone || read.RunID != done.RunID || read.CostUSD != 0.04 || !sameJSON(read.Decisions, string(done.Decisions)) {
		t.Errorf("Firing after the finish = %+v, %v, %v; want done with its run, cost and decisions", read, ok, err)
	}

	if err := f.in.HeartbeatCheck(f.ctx(), l, t0.Add(4*time.Minute)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("HeartbeatCheck after the finish = %v, want ErrLeaseLost", err)
	}

	f.checkFiring(t0.Add(5 * time.Minute))
	_, l2 := f.claimCheck(t0.Add(5 * time.Minute))

	if err := f.in.FinishCheck(f.ctx(), l2, domain.CheckResult{State: domain.CheckFailed, Reason: "the answer is not JSON", At: t0.Add(6 * time.Minute)}); err != nil {
		t.Fatalf("FinishCheck failed: %v", err)
	}

	in, err := f.in.CheckInput(f.ctx(), f.factory, nightly, t0.Add(7*time.Minute))
	if err != nil || !sameJSON(in.State, string(done.Saved)) || !sameJSON(in.LastDecisions, string(done.Decisions)) {
		t.Errorf("CheckInput = %+v, %v; want the done check's state and decisions, kept through the failed one", in, err)
	}
}

// checkRun starts a check-kind run of the factory, which works no item.
func (f *fixture) checkRun() domain.RunID {
	f.t.Helper()

	id, err := f.Store.StartRun(f.ctx(), ports.NewRun{FactoryID: f.factory, Agent: "watcher", Model: "claude-haiku-5-5", StartedAt: t0, Kind: domain.RunKindCheck})
	if err != nil {
		f.t.Fatalf("StartRun of a check: %v", err)
	}

	return id
}

// ExpireChecks fails a running check whose lease ended, and only it, with "lease expired"; its
// holder's finish is then ErrLeaseLost.
func testExpireChecks(f *fixture) {
	t := f.t
	queued := f.checkFiring(t0)
	_, l := f.claimCheck(t0)

	if n, err := f.in.ExpireChecks(f.ctx(), f.factory, t0.Add(30*time.Second)); err != nil || n != 0 {
		t.Errorf("ExpireChecks within the lease = %d, %v; want 0", n, err)
	}

	if n, err := f.in.ExpireChecks(f.ctx(), f.factory, t0.Add(2*time.Minute)); err != nil || n != 1 {
		t.Fatalf("ExpireChecks after the lease = %d, %v; want 1", n, err)
	}

	got, _, err := f.in.Firing(f.ctx(), f.factory, nightly, queued.IdempotencyKey)
	if err != nil || got.CheckState != domain.CheckFailed || got.Reason != domain.CheckLeaseExpired {
		t.Errorf("expired check = %+v, %v; want failed, %q", got, err, domain.CheckLeaseExpired)
	}

	if err := f.in.FinishCheck(f.ctx(), l, domain.CheckResult{State: domain.CheckFailed, Reason: "late", At: t0.Add(3 * time.Minute)}); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("FinishCheck after it expired = %v, want ErrLeaseLost", err)
	}
}

// CheckInput lists the trigger's open items with their key (the dedupe of the firing that filed
// them) and last note, and the keys whose items closed in the last 7 days with how and why;
// another trigger's items, and items closed before the window, are left out. Without a done
// check, the decisions and the state are null.
func testCheckInput(f *fixture) {
	t := f.t

	open := f.item(nightly, t0)
	f.fire(nightly, "k-open", "INC-1", open.ID, t0)

	if err := f.Store.AppendEvents(f.ctx(), domain.Event{
		FactoryID: f.factory, ItemID: open.ID, At: t0.Add(time.Minute), Kind: domain.EventNote, Actor: "user:jo", Data: map[string]any{"text": "still down"},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	closed := f.item(nightly, t0.Add(-time.Hour))
	f.fire(nightly, "k-closed", "INC-2", closed.ID, t0.Add(-time.Hour))
	f.close(closed, domain.StateCancelled)

	f.item(flaky, t0) // another trigger's

	in, err := f.in.CheckInput(f.ctx(), f.factory, nightly, t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("CheckInput: %v", err)
	}

	if len(in.Open) != 1 || in.Open[0] != (domain.CheckOpenItem{Key: "INC-1", Ref: refOf(open), Title: open.Title, State: string(open.State), LastNote: "still down"}) {
		t.Errorf("open = %+v, want the open item with its key and note", in.Open)
	}

	if len(in.Closed) != 1 || in.Closed[0].Key != "INC-2" || in.Closed[0].How != string(domain.StateCancelled) || !in.Closed[0].At.Equal(t0) {
		t.Errorf("closed = %+v, want INC-2 cancelled at %s", in.Closed, t0)
	}

	if string(in.LastDecisions) != jsonNull || string(in.State) != jsonNull {
		t.Errorf("decisions, state = %s, %s; want null and null", in.LastDecisions, in.State)
	}

	f.expectClosedGone()
}

// expectClosedGone checks that 8 days on the closed key is out of the input and the open item in.
func (f *fixture) expectClosedGone() {
	later, err := f.in.CheckInput(f.ctx(), f.factory, nightly, t0.Add(8*24*time.Hour))
	if err != nil || len(later.Closed) != 0 || len(later.Open) != 1 {
		f.t.Errorf("CheckInput 8 days on = %+v, %v; want no closed key and the open item", later, err)
	}
}

// refOf is it's ref as a check reads it.
func refOf(it domain.Item) string {
	b, _ := json.Marshal(it.Number)

	return "#" + string(b)
}

// sameJSON reports whether two JSON texts hold the same value.
func sameJSON[A, B ~string | ~[]byte](a A, b B) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}

	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)

	return string(xb) == string(yb)
}

// watcher is the check's agent.
const watcher = "watcher"

// jsonNull is a JSON column read back empty: no saved state or decisions.
const jsonNull = "null"

// runCheck fires, claims and finishes a check of nightly queued at the instant given, with answer, its items made for
// each file entry, and returns the firing as it reads back. try queues it as a try check.
func (f *fixture) runCheck(at time.Time, answer string, limits domain.CheckLimits, try bool) domain.TriggerFiring {
	f.t.Helper()

	queued, fresh, err := f.in.Fire(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: nightly, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: at.UTC().Format(time.RFC3339), Outcome: domain.FiringChecked, CheckState: domain.CheckQueued, At: at, Try: try,
	}, nil, domain.FireLimits{})
	if err != nil || !fresh || queued.Try != try {
		f.t.Fatalf("Fire a check at %s = %+v, %v, %v", at, queued, fresh, err)
	}

	_, l := f.claimCheck(at)

	a, err := domain.ParseCheckAnswer(json.RawMessage(answer))
	if err != nil {
		f.t.Fatalf("ParseCheckAnswer: %v", err)
	}

	var items []domain.CheckItem
	for _, file := range a.File {
		items = append(items, domain.CheckItem{Key: file.Key, Item: domain.NewItem{
			FactoryID: f.factory, Type: typeFix, Title: file.Title, Body: file.Brief, Station: station, State: domain.StateQueued,
			Class: domain.ClassStandard, CreatedAt: at, Trigger: nightly, Origin: domain.OriginTrigger,
			CreatedBy: domain.Actor{Kind: domain.ActorAgent, ID: watcher, Name: watcher}, Untrusted: true,
		}})
	}

	if err := f.in.FinishCheck(f.ctx(), l, domain.CheckResult{
		State: domain.CheckDone, RunID: f.checkRun(), Agent: watcher, CostUSD: 0.02, Decisions: a.Decisions(), Saved: a.State,
		Items: items, Limits: limits, At: at.Add(time.Minute),
	}); err != nil {
		f.t.Fatalf("FinishCheck: %v", err)
	}

	got, ok, err := f.in.Firing(f.ctx(), f.factory, nightly, queued.IdempotencyKey)
	if err != nil || !ok || got.Applied == nil || got.CheckState != domain.CheckDone {
		f.t.Fatalf("Firing after the check = %+v, %v, %v; want it done with what it applied", got, ok, err)
	}

	return got
}

// itemByRef is the fixture's factory's item with ref (#<number>).
func (f *fixture) itemByRef(ref string) domain.Item {
	f.t.Helper()

	items, err := f.Store.ListItems(f.ctx(), ports.ItemQuery{FactoryID: f.factory, Limit: 100})
	if err != nil {
		f.t.Fatalf("ListItems: %v", err)
	}

	for _, it := range items {
		if refOf(it) == ref {
			return it
		}
	}

	f.t.Fatalf("no item %s", ref)

	return domain.Item{}
}

// skips are a's skipped entries as key: reason.
func skips(a *domain.CheckApplied) []string {
	var out []string
	for _, s := range a.Skipped {
		out = append(out, s.Key+": "+s.Reason)
	}

	return out
}

// A check files new keys through the door (origin trigger, by its agent, untrusted) and keeps
// each key's item; a file or a note on an open key's item becomes an item_note, one per key an
// hour and never the same text twice; a closed key is skipped unless reopened, when its new item
// links to the old one; the state is saved; CheckInput gives the items with their keys.
func testApplyCheck(f *fixture) {
	t := f.t

	first := f.runCheck(t0, `{"file": [{"key": "INC-1", "title": "API down", "brief": "5xx"}, {"key": "INC-2", "title": "Disk full", "brief": "db-1"}],
		"state": {"since": "a"}}`, domain.CheckLimits{}, false)
	inc1, inc2 := f.expectFiled(first)
	f.close(inc2, domain.StateCancelled)

	second := f.runCheck(t0.Add(2*time.Hour), `{"file": [{"key": "INC-1", "title": "API down", "brief": "still 5xx"}, {"key": "INC-2", "title": "Disk full", "brief": "again"}],
		"note": [{"key": "INC-1", "text": "and slow"}, {"key": "INC-9", "text": "who"}]}`, domain.CheckLimits{}, false)

	wantSkips := []string{"INC-2: closed on Oct 1: cancelled", "INC-1: " + domain.CheckNoteTooSoon, "INC-9: " + domain.CheckNoOpenItem}
	if a := second.Applied; len(a.Filed) != 0 || len(a.Noted) != 1 || a.Noted[0].Ref != refOf(inc1) || !slices.Equal(skips(a), wantSkips) {
		t.Errorf("second check applied %+v; want a note on %s and skips %q", a, refOf(inc1), wantSkips)
	}

	f.expectCheckNote(inc1, "API down\n\nstill 5xx")

	third := f.runCheck(t0.Add(4*time.Hour), `{"file": [{"key": "INC-2", "title": "Disk full", "brief": "back", "reopen": "it came back"}],
		"note": [{"key": "INC-1", "text": "API down\n\nstill 5xx"}]}`, domain.CheckLimits{}, false)
	if a := third.Applied; len(a.Filed) != 1 || a.Filed[0].Reopens != refOf(inc2) || !slices.Equal(skips(a), []string{"INC-1: " + domain.CheckNoteRepeated}) {
		t.Fatalf("third check applied %+v; want INC-2 filed again after %s, and the repeated note dropped", a, refOf(inc2))
	}

	f.expectReopened(f.itemByRef(third.Applied.Filed[0].Ref), inc2)
}

// expectFiled checks that first filed two items through the door and returns them.
func (f *fixture) expectFiled(first domain.TriggerFiring) (domain.Item, domain.Item) {
	f.t.Helper()

	if a := first.Applied; len(a.Filed) != 2 || a.Filed[0].Ref == "" || len(a.Noted) != 0 || len(a.Skipped) != 0 {
		f.t.Fatalf("first check applied %+v; want two items filed", a)
	}

	inc1, inc2 := f.itemByRef(first.Applied.Filed[0].Ref), f.itemByRef(first.Applied.Filed[1].Ref)
	if inc1.Origin != domain.OriginTrigger || inc1.Trigger != nightly || !inc1.Untrusted || inc1.CreatedBy != "agent:"+watcher || inc1.Title != "API down" {
		f.t.Errorf("filed item = %+v; want origin trigger of %s by agent:%s, untrusted", inc1, nightly, watcher)
	}

	return inc1, inc2
}

// expectCheckNote checks that it has one item_note, by the engine, with text, the trigger and key.
func (f *fixture) expectCheckNote(it domain.Item, text string) {
	f.t.Helper()

	events, err := f.Store.ItemEvents(f.ctx(), it.ID, 0, 100)
	if err != nil {
		f.t.Fatalf("ItemEvents: %v", err)
	}

	var notes []domain.Event

	for _, e := range events {
		if e.Kind == domain.EventItemNote {
			notes = append(notes, e)
		}
	}

	if len(notes) != 1 || notes[0].Data["text"] != text || notes[0].Data[domain.KeyNoteTrigger] != nightly || notes[0].Data[domain.KeyNoteKey] != "INC-1" ||
		notes[0].Actor != domain.ActorEngine {
		f.t.Errorf("item notes = %+v; want one by the engine with %q", notes, text)
	}
}

// expectReopened checks that again links to old, and that the input gives again with its key.
func (f *fixture) expectReopened(again, old domain.Item) {
	f.t.Helper()

	edges, err := f.Store.Edges(f.ctx(), again.ID)
	if err != nil || len(edges.Needs) != 1 || edges.Needs[0].Needs != old.ID || edges.Needs[0].Kind != domain.EdgeLinked {
		f.t.Errorf("edges of the item filed again = %+v, %v; want a linked edge to %s", edges, err, old.ID)
	}

	in, err := f.in.CheckInput(f.ctx(), f.factory, nightly, t0.Add(5*time.Hour))
	if err != nil {
		f.t.Fatalf("CheckInput: %v", err)
	}

	keys := map[string]string{}
	for _, o := range in.Open {
		keys[o.Ref] = o.Key
	}

	if keys[refOf(again)] != "INC-2" || len(in.Open) != 2 || !sameJSON(in.State, `{"since": "a"}`) {
		f.t.Errorf("input open = %+v, state %s; want both open items with their keys and the first check's state", in.Open, in.State)
	}
}

// perCheck caps one check's items and perDay the items the trigger filed since the UTC day
// began; what is over them is skipped over the limit.
func testApplyLimits(f *fixture) {
	t := f.t

	files := func(keys ...string) string {
		var parts []string
		for _, k := range keys {
			parts = append(parts, `{"key": "`+k+`", "title": "t `+k+`", "brief": "b"}`)
		}

		return `{"file": [` + strings.Join(parts, ", ") + `]}`
	}

	first := f.runCheck(t0, files("a", "b", "c"), domain.CheckLimits{PerCheck: 2, PerDay: 3}, false)
	if a := first.Applied; len(a.Filed) != 2 || !slices.Equal(skips(a), []string{"c: " + domain.CheckOverLimit}) {
		t.Errorf("first check applied %+v; want 2 filed and c over the limit", a)
	}

	second := f.runCheck(t0.Add(time.Hour), files("c", "d"), domain.CheckLimits{PerCheck: 2, PerDay: 3}, false)
	if a := second.Applied; len(a.Filed) != 1 || a.Filed[0].Key != "c" || !slices.Equal(skips(a), []string{"d: " + domain.CheckOverLimit}) {
		t.Errorf("second check applied %+v; want c filed and d over perDay", a)
	}

	next := f.runCheck(t0.Add(24*time.Hour), files("d"), domain.CheckLimits{PerCheck: 2, PerDay: 3}, false)
	if a := next.Applied; len(a.Filed) != 1 {
		t.Errorf("next day's check applied %+v; want d filed", a)
	}
}

// A try check says what it would do and files, notes and saves nothing; the next check's input
// does not see its decisions; a try while a check is in flight is ErrConflict.
func testTryCheck(f *fixture) {
	t := f.t

	f.runCheck(t0, `{"file": [{"key": "INC-1", "title": "API down", "brief": "5xx"}], "state": {"since": "a"}}`, domain.CheckLimits{}, false)

	try := f.runCheck(t0.Add(2*time.Hour), `{"file": [{"key": "INC-2", "title": "New", "brief": "b"}], "note": [{"key": "INC-1", "text": "slow"}],
		"state": {"since": "b"}}`, domain.CheckLimits{}, true)
	if a := try.Applied; !try.Try || len(a.Filed) != 1 || a.Filed[0].Ref != "" || len(a.Noted) != 1 {
		t.Errorf("try applied %+v; want it would file INC-2 and note INC-1", a)
	}

	f.expectTryLeftNothing()
	f.expectTryRefusedWhileQueued()

	if checks, err := f.in.TriggerChecks(f.ctx(), f.factory, nightly, 10); err != nil || len(checks) != 3 || !checks[1].Try || checks[0].CheckState != domain.CheckQueued {
		t.Errorf("TriggerChecks = %+v, %v; want the queued check, the try and the first, newest first", checks, err)
	}
}

// expectTryLeftNothing checks that only the first check's item exists, and that the next check
// gets the first check's state and decisions and no note.
func (f *fixture) expectTryLeftNothing() {
	f.t.Helper()

	if items, err := f.Store.ListItems(f.ctx(), ports.ItemQuery{FactoryID: f.factory, Limit: 100}); err != nil || len(items) != 1 {
		f.t.Errorf("items after the try = %d, %v; want only the first check's", len(items), err)
	}

	in, err := f.in.CheckInput(f.ctx(), f.factory, nightly, t0.Add(3*time.Hour))
	if err != nil || !sameJSON(in.State, `{"since": "a"}`) || strings.Contains(string(in.LastDecisions), "INC-2") || in.Open[0].LastNote != "" {
		f.t.Errorf("input after the try = %+v, %v; want the first check's state and decisions and no note", in, err)
	}
}

// expectTryRefusedWhileQueued queues a check and checks that a try meanwhile is ErrConflict.
func (f *fixture) expectTryRefusedWhileQueued() {
	f.t.Helper()
	f.checkFiring(t0.Add(4 * time.Hour))

	if _, _, err := f.in.Fire(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: nightly, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: "try-busy", Outcome: domain.FiringChecked, CheckState: domain.CheckQueued, At: t0.Add(4 * time.Hour), Try: true,
	}, nil, domain.FireLimits{}); !errors.Is(err, ports.ErrConflict) {
		f.t.Errorf("try while a check is queued = %v; want ErrConflict", err)
	}
}

// failCheck fires, claims and fails a check of nightly queued at the instant given, a try when
// try is set.
func (f *fixture) failCheck(at time.Time, try bool) {
	f.t.Helper()

	if _, _, err := f.in.Fire(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: nightly, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: at.UTC().Format(time.RFC3339), Outcome: domain.FiringChecked, CheckState: domain.CheckQueued, At: at, Try: try,
	}, nil, domain.FireLimits{}); err != nil {
		f.t.Fatalf("Fire: %v", err)
	}

	_, l := f.claimCheck(at)
	if err := f.in.FinishCheck(f.ctx(), l, domain.CheckResult{State: domain.CheckFailed, Reason: "timed out", At: at.Add(time.Minute)}); err != nil {
		f.t.Fatalf("FinishCheck: %v", err)
	}
}

// alerts are the fixture's factory's check alerts since t0's UTC day began.
func (f *fixture) alerts() []domain.CheckAlert {
	f.t.Helper()

	got, err := f.in.CheckAlerts(f.ctx(), []domain.FactoryID{f.factory}, t0.Truncate(24*time.Hour))
	if err != nil {
		f.t.Fatalf("CheckAlerts: %v", err)
	}

	return got
}

// Three failed checks in a row are an alert, tries left out, until one is done; a check that
// skipped over the limit is one from that UTC day on.
func testCheckAlerts(f *fixture) {
	t := f.t

	f.runCheck(t0, `{"file": [{"key": "a", "title": "a", "brief": "b"}, {"key": "b", "title": "b", "brief": "b"}]}`, domain.CheckLimits{PerCheck: 1}, false)

	if got := f.alerts(); len(got) != 1 || got[0].Kind != domain.CheckLimited || got[0].Trigger != nightly || !got[0].At.Equal(t0.Add(time.Minute)) {
		t.Errorf("alerts after a check over the limit = %+v; want one limit alert", got)
	}

	f.failCheck(t0.Add(time.Hour), false)
	f.failCheck(t0.Add(2*time.Hour), false)
	f.failCheck(t0.Add(3*time.Hour), true)

	if got := f.alerts(); len(got) != 1 {
		t.Errorf("alerts after two failures and a failed try = %+v; want only the limit", got)
	}

	f.failCheck(t0.Add(4*time.Hour), false)

	if got := f.alerts(); len(got) != 2 || got[0].Kind != domain.CheckFailing || got[0].Reason != "timed out" || !got[0].At.Equal(t0.Add(4*time.Hour+time.Minute)) {
		t.Errorf("alerts after three failures = %+v; want failing at the newest, then the limit", got)
	}

	if got, err := f.in.CheckAlerts(f.ctx(), []domain.FactoryID{f.factory}, t0.Add(24*time.Hour)); err != nil || len(got) != 1 || got[0].Kind != domain.CheckFailing {
		t.Errorf("alerts the next day = %+v, %v; want only failing", got, err)
	}
}

// Another organization reads none of a trigger's keys, checks or alerts.
func testIsolatedKeys(f *fixture) {
	t := f.t

	f.runCheck(t0, `{"file": [{"key": "a", "title": "a", "brief": "b"}, {"key": "b", "title": "b", "brief": "b"}]}`, domain.CheckLimits{PerCheck: 1}, false)

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))

	if in, err := f.in.CheckInput(labs, f.factory, nightly, t0.Add(time.Hour)); err != nil || len(in.Open) != 0 || string(in.LastDecisions) != jsonNull {
		t.Errorf("labs CheckInput = %+v, %v; want nothing", in, err)
	}

	if got, err := f.in.TriggerChecks(labs, f.factory, nightly, 10); err != nil || len(got) != 0 {
		t.Errorf("labs TriggerChecks = %+v, %v; want none", got, err)
	}

	if got, err := f.in.CheckAlerts(labs, []domain.FactoryID{f.factory}, t0.Truncate(24*time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("labs CheckAlerts = %+v, %v; want none", got, err)
	}
}

// CheckMemory counts the trigger's open items and closed keys in full and lists the newest
// domain.MaxCheckMemoryItems of each, with the saved state and when the check that saved it
// finished; before any check the lists are empty and the state null. ClearCheckState deletes
// the state and keeps the keys, so the next check gets no state and is still refused a closed
// key; while a check is queued it is ErrConflict.
func testCheckMemory(f *fixture) {
	t := f.t

	f.expectNoMemory()

	first := f.runCheck(t0, `{"file": [{"key": "INC-1", "title": "API down", "brief": "5xx"}, {"key": "INC-2", "title": "Disk full", "brief": "db-1"}],
		"state": {"since": "a"}}`, domain.CheckLimits{}, false)
	_, inc2 := f.expectFiled(first)
	f.close(inc2, domain.StateCancelled)

	var newest domain.Item
	for i := range domain.MaxCheckMemoryItems {
		newest = f.item(nightly, t0.Add(time.Duration(i+1)*time.Second))
	}

	m, err := f.in.CheckMemory(f.ctx(), f.factory, nightly, t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("CheckMemory: %v", err)
	}

	if m.Open != domain.MaxCheckMemoryItems+1 || len(m.OpenItems) != domain.MaxCheckMemoryItems || m.OpenItems[0].Ref != refOf(newest) {
		t.Fatalf("open = %d, listed %+v; want %d counted and %d listed, %s first",
			m.Open, m.OpenItems, domain.MaxCheckMemoryItems+1, domain.MaxCheckMemoryItems, refOf(newest))
	}

	if m.Closed != 1 || len(m.ClosedKeys) != 1 || m.ClosedKeys[0].Key != "INC-2" || m.ClosedKeys[0].How != string(domain.StateCancelled) {
		t.Errorf("closed = %d, %+v; want INC-2 cancelled", m.Closed, m.ClosedKeys)
	}

	if !sameJSON(m.State, `{"since": "a"}`) || !m.CheckedUpTo.Equal(t0.Add(time.Minute)) {
		t.Errorf("state = %s, checked up to %s; want the first check's, at %s", m.State, m.CheckedUpTo, t0.Add(time.Minute))
	}

	f.expectCleared()
}

// expectNoMemory checks nightly's memory before any check: empty lists, never nil, a null state
// and no time.
func (f *fixture) expectNoMemory() {
	m, err := f.in.CheckMemory(f.ctx(), f.factory, nightly, t0)
	if err != nil {
		f.t.Fatalf("CheckMemory before a check: %v", err)
	}

	lists := m.OpenItems != nil && len(m.OpenItems) == 0 && m.ClosedKeys != nil && len(m.ClosedKeys) == 0
	if !lists || m.Open != 0 || m.Closed != 0 || string(m.State) != jsonNull || !m.CheckedUpTo.IsZero() {
		f.t.Errorf("CheckMemory before a check = %+v; want empty lists, a null state and no time", m)
	}
}

// expectCleared clears nightly's state twice, checks the state is gone and its keys kept, and
// that a queued check refuses the clear.
func (f *fixture) expectCleared() {
	t := f.t

	for range 2 {
		if err := f.in.ClearCheckState(f.ctx(), f.factory, nightly); err != nil {
			t.Fatalf("ClearCheckState: %v", err)
		}
	}

	if m, err := f.in.CheckMemory(f.ctx(), f.factory, nightly, t0.Add(time.Hour)); err != nil || string(m.State) != jsonNull || !m.CheckedUpTo.IsZero() || m.Closed != 1 {
		t.Errorf("CheckMemory after Clear = %+v, %v; want a null state, no time and the closed key kept", m, err)
	}

	if in, err := f.in.CheckInput(f.ctx(), f.factory, nightly, t0.Add(time.Hour)); err != nil || string(in.State) != jsonNull {
		t.Errorf("CheckInput after Clear = %s, %v; want a null state", in.State, err)
	}

	again := f.runCheck(t0.Add(2*time.Hour), `{"file": [{"key": "INC-2", "title": "Disk full", "brief": "again"}]}`, domain.CheckLimits{}, false)
	if a := again.Applied; len(a.Filed) != 0 || !slices.Equal(skips(a), []string{"INC-2: closed on Oct 1: cancelled"}) {
		t.Errorf("check after Clear applied %+v; want the closed key refused", a)
	}

	f.checkFiring(t0.Add(3 * time.Hour))

	if err := f.in.ClearCheckState(f.ctx(), f.factory, nightly); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("ClearCheckState while a check is queued = %v, want ErrConflict", err)
	}
}

// Another organization's Clear deletes no state of the trigger, and its CheckMemory sees nothing.
func testIsolatedClear(f *fixture) {
	t := f.t

	f.runCheck(t0, `{"file": [{"key": "a", "title": "a", "brief": "b"}], "state": {"since": "a"}}`, domain.CheckLimits{}, false)

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))

	if err := f.in.ClearCheckState(labs, f.factory, nightly); err != nil {
		t.Errorf("labs ClearCheckState = %v, want nil", err)
	}

	if m, err := f.in.CheckMemory(labs, f.factory, nightly, t0.Add(time.Hour)); err != nil || m.Open != 0 || len(m.OpenItems) != 0 || string(m.State) != jsonNull {
		t.Errorf("labs CheckMemory = %+v, %v; want nothing", m, err)
	}

	if m, err := f.in.CheckMemory(f.ctx(), f.factory, nightly, t0.Add(time.Hour)); err != nil || m.Open != 1 || !sameJSON(m.State, `{"since": "a"}`) {
		t.Errorf("CheckMemory after labs' Clear = %+v, %v; want the item and the state kept", m, err)
	}
}

// ItemCheckKey is the key a check filed an item under while the item is the key's last; a
// schedule's item, an item whose key was filed again, an unknown item and another
// organization's read have none.
func testItemCheckKey(f *fixture) {
	t := f.t

	const reopened = "INC-2"

	inc1, inc2 := f.expectFiled(f.runCheck(t0, `{"file": [{"key": "INC-1", "title": "API down", "brief": "5xx"}, {"key": "INC-2", "title": "Disk full", "brief": "db-1"}]}`,
		domain.CheckLimits{}, false))
	f.close(inc2, domain.StateCancelled)

	again := f.runCheck(t0.Add(time.Hour), `{"file": [{"key": "INC-2", "title": "Disk full", "brief": "back", "reopen": "it came back"}]}`, domain.CheckLimits{}, false)
	if len(again.Applied.Filed) != 1 {
		t.Fatalf("reopen applied %+v; want INC-2 filed again", again.Applied)
	}

	scheduled := f.item(nightly, t0)

	for _, c := range []struct {
		what string
		org  domain.OrgID
		item domain.ItemID
		want string
	}{
		{"the check's item", f.org, inc1.ID, "INC-1"},
		{"the key's item filed again", f.org, f.itemByRef(again.Applied.Filed[0].Ref).ID, reopened},
		{"the key's earlier item", f.org, inc2.ID, ""},
		{"a schedule's item", f.org, scheduled.ID, ""},
		{"an unknown item", f.org, "not-an-item", ""},
		{"another organization's read", f.NewOrg(t), inc1.ID, ""},
	} {
		if got, err := f.in.ItemCheckKey(domain.WithOrg(t.Context(), c.org), c.item); err != nil || got != c.want {
			t.Errorf("ItemCheckKey of %s = %q, %v; want %q", c.what, got, err, c.want)
		}
	}
}
