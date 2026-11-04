package intaketest

import (
	"encoding/json"
	"errors"
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
}

// checkFiring is a schedule firing of nightly at at that queues a check.
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

	open := f.item(domain.ItemFromSchedule, nightly, t0)
	f.fire(nightly, "k-open", "INC-1", open.ID, t0)

	if err := f.Store.AppendEvents(f.ctx(), domain.Event{
		FactoryID: f.factory, ItemID: open.ID, At: t0.Add(time.Minute), Kind: domain.EventNote, Actor: "user:jo", Data: map[string]any{"text": "still down"},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	closed := f.item(domain.ItemFromSchedule, nightly, t0.Add(-time.Hour))
	f.fire(nightly, "k-closed", "INC-2", closed.ID, t0.Add(-time.Hour))
	f.close(closed, domain.StateCancelled)

	f.item(domain.ItemFromSchedule, flaky, t0) // another trigger's

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

	if string(in.LastDecisions) != "null" || string(in.State) != "null" {
		t.Errorf("decisions, state = %s, %s; want null and null", in.LastDecisions, in.State)
	}

	later, err := f.in.CheckInput(f.ctx(), f.factory, nightly, t0.Add(8*24*time.Hour))
	if err != nil || len(later.Closed) != 0 || len(later.Open) != 1 {
		t.Errorf("CheckInput 8 days on = %+v, %v; want no closed key and the open item", later, err)
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
