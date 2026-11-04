package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// checkTrigger is a schedule trigger every 5 minutes whose firings queue a check by agWorker.
func checkTrigger(name string) domain.Trigger {
	return domain.Trigger{
		Name: name, Enabled: true, On: domain.OnSchedule, Cron: "*/5 * * * *",
		Create: domain.TriggerCreate{Type: typeFeature, Station: stWork},
		Check:  &domain.TriggerCheck{Agent: agWorker, Brief: "Look for new incidents.", MaxUSD: 0.1, Timeout: 5 * time.Minute},
	}
}

// checkHarness is a harness whose line has the triggers ts and whose agent runs run.
func checkHarness(t *testing.T, line domain.Line, run porttest.AgentFunc, ts ...domain.Trigger) *harness {
	t.Helper()

	line.Triggers = ts

	h := newHarness(t, line, run)
	h.store.AddFactory(testOrg, factoryID)

	return h
}

// fireAt fires the harness's schedules at now and returns the one firing it recorded.
func (h *harness) fireAt(t *testing.T, now time.Time) domain.TriggerFiring {
	t.Helper()

	tick, err := h.e.FireSchedules(t.Context(), now)
	if err != nil || len(tick.Firings) != 1 {
		t.Fatalf("FireSchedules at %s = %+v, %v; want one firing", now, tick, err)
	}

	return tick.Firings[0]
}

// firing reads the firing of trigger for the instant key.
func (h *harness) firing(t *testing.T, trigger, key string) domain.TriggerFiring {
	t.Helper()

	f, ok, err := h.intake.Firing(inOrg(t), factoryID, trigger, key)
	if err != nil || !ok {
		t.Fatalf("Firing %s %s = %v, %v", trigger, key, ok, err)
	}

	return f
}

const (
	incidents = "new-incidents"
	at0900    = "2026-10-23T09:00:00Z"
)

func TestFireSchedules_QueuesCheck(t *testing.T) {
	t.Parallel()

	h := checkHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`), checkTrigger(incidents))

	f := h.fireAt(t, t0.Add(2*time.Minute))
	if f.Outcome != domain.FiringChecked || f.CheckState != domain.CheckQueued || f.IdempotencyKey != at0900 || f.ItemID != "" {
		t.Errorf("firing = %+v; want a queued check for the 09:00 instant and no item", f)
	}

	if n := h.items(t); n != 0 {
		t.Errorf("%d items; a check trigger's firing files none", n)
	}
}

func TestFireSchedules_StillChecking(t *testing.T) {
	t.Parallel()

	h := checkHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`), checkTrigger(incidents))

	h.fireAt(t, t0.Add(2*time.Minute))

	if f := h.fireAt(t, t0.Add(7*time.Minute)); f.Outcome != domain.FiringSkipped || f.Reason != domain.StillChecking {
		t.Errorf("firing while the 09:00 check is queued = %+v; want skipped, %q", f, domain.StillChecking)
	}
}

func TestFireSchedules_NoCheckWhilePaused(t *testing.T) {
	t.Parallel()

	line := lineOf(agentStation(stWork, agWorker, 1, 1))
	line.Paused = true
	h := checkHarness(t, line, porttest.Succeed(`{}`), checkTrigger(incidents))

	if f := h.fireAt(t, t0.Add(2*time.Minute)); f.Outcome != domain.FiringSkipped || f.Reason != domain.CheckLinePaused || f.CheckState != "" {
		t.Errorf("firing on a paused line = %+v; want skipped, %q", f, domain.CheckLinePaused)
	}
}

// A queued check is leased on the next tick and run as a check run of its trigger; its answer's
// decisions are kept on the firing and its state saved for the next check.
func TestChecks_LeaseAndRun(t *testing.T) {
	t.Parallel()

	answer := `{"skip": [{"key": "INC-1", "reason": "a test page"}], "state": {"since": "2026-10-23T09:00:00Z"}}`
	h := checkHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(answer), CostUSD: 0.03}
	}, checkTrigger(incidents))

	h.fireAt(t, t0.Add(2*time.Minute))
	h.tick(t)
	h.wait(t, h.e)

	f := h.firing(t, incidents, at0900)
	if f.CheckState != domain.CheckDone || f.RunID == "" || f.CostUSD != 0.03 || !strings.Contains(f.Decisions, `"INC-1"`) || strings.Contains(f.Decisions, "since") {
		t.Fatalf("firing = %+v; want done with its run, cost and decisions, without the state", f)
	}

	h.expectCheckRun(t, f.RunID)
}

// expectCheckRun fails the test unless id is agWorker's run without an item or a station, whose
// token is a check token of incidents, and the next check gets its saved state and decisions.
func (h *harness) expectCheckRun(t *testing.T, id domain.RunID) {
	t.Helper()

	run, err := h.store.Run(inOrg(t), id)
	if err != nil || run.ItemID != "" || run.Station != "" || run.Agent != agWorker {
		t.Errorf("check run = %+v, %v; want %s's run without an item or a station", run, err, agWorker)
	}

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs; want one", len(runs))
	}

	if c := h.claims(t, runs[0].Env[envTaskToken]); c.Kind != domain.TaskKindCheck || c.Trigger != incidents || c.ItemID != "" {
		t.Errorf("token claims = %+v; want a check token of %s without an item", c, incidents)
	}

	in, err := h.intake.CheckInput(inOrg(t), factoryID, incidents, t0)
	if err != nil || string(in.State) != `{"since":"2026-10-23T09:00:00Z"}` || !strings.Contains(string(in.LastDecisions), "INC-1") {
		t.Errorf("next check's input = %+v, %v; want the saved state and these decisions", in, err)
	}
}

// A check counts against its factory's run slots: with one slot, the second trigger's check
// stays queued while the first runs, and runs on a later tick.
func TestChecks_FactoryFull(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h := checkHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Block(release, `{}`), checkTrigger(incidents), checkTrigger("status-page"))
	h.store.SetSlotCaps(testOrg, 0, map[domain.FactoryID]int{factoryID: 1})

	if tick, err := h.e.FireSchedules(t.Context(), t0.Add(2*time.Minute)); err != nil || len(tick.Firings) != 2 {
		t.Fatalf("FireSchedules = %+v, %v; want two checks queued", tick, err)
	}

	h.tick(t)

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	first, second := h.firing(t, incidents, at0900), h.firing(t, "status-page", at0900)
	if first.CheckState != domain.CheckRunning || second.CheckState != domain.CheckQueued {
		t.Errorf("checks = %s and %s; want the first running and the second queued at the factory's cap", first.CheckState, second.CheckState)
	}

	close(release)
	h.wait(t, h.e)
	h.tick(t)
	h.wait(t, h.e)

	if second := h.firing(t, "status-page", at0900); second.CheckState != domain.CheckDone {
		t.Errorf("second check = %+v; want done once the slot was free", second)
	}
}

// A running check whose lease ran out (its goald died) is failed on the next tick, not run again.
func TestChecks_ExpiredLeaseFails(t *testing.T) {
	t.Parallel()

	h := checkHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`), checkTrigger(incidents))

	h.fireAt(t, t0.Add(2*time.Minute))

	if _, _, err := h.intake.ClaimCheck(inOrg(t), factoryID, "goald-gone", t0, ttl); err != nil {
		t.Fatal(err)
	}

	h.clock.Advance(ttl + time.Second)
	h.tick(t)
	h.wait(t, h.e)

	if f := h.firing(t, incidents, at0900); f.CheckState != domain.CheckFailed || f.Reason != domain.CheckLeaseExpired {
		t.Errorf("check = %+v; want failed, %q", f, domain.CheckLeaseExpired)
	}

	if n := len(h.worker.Runs()); n != 0 {
		t.Errorf("%d agent runs; an expired check is not run again", n)
	}
}

// A budget stop cancels a running check, which is failed with the cause.
func TestChecks_BudgetStopCancels(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	t.Cleanup(func() { close(release) })

	h := checkHarness(t, budgeted(10, domain.BudgetStop, 1), porttest.Block(release, `{}`), checkTrigger(incidents))

	h.fireAt(t, t0.Add(2*time.Minute))
	h.tick(t)

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	h.spent(t, t0, 12)
	h.tick(t) // over budget: the check is cancelled
	h.wait(t, h.e)

	f := h.firing(t, incidents, at0900)
	if f.CheckState != domain.CheckFailed || !strings.Contains(f.Reason, "cancelled") || !strings.Contains(f.Reason, "budget") {
		t.Errorf("check = %+v; want failed, cancelled by the budget", f)
	}
}

// An answer that breaks the contract, or a run that failed, fails the check with the reason and
// keeps the saved state as it was.
func TestChecks_FailedAnswerRecorded(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		res  domain.RunResult
		want string
	}{
		{"contract", ok(`{"file": [{"key": "INC-1"}], "state": {"since": "later"}}`), "file 0 needs a key, a title and a brief"},
		{"timeout", domain.RunResult{Status: domain.RunFailed, Error: "timed out after 5m0s"}, "timed out after 5m0s"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := checkHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
				return tt.res
			}, checkTrigger(incidents))

			h.fireAt(t, t0.Add(2*time.Minute))
			h.tick(t)
			h.wait(t, h.e)

			if f := h.firing(t, incidents, at0900); f.CheckState != domain.CheckFailed || !strings.Contains(f.Reason, tt.want) || f.Decisions != "" {
				t.Errorf("check = %+v; want failed naming %q, without decisions", f, tt.want)
			}

			if in, err := h.intake.CheckInput(inOrg(t), factoryID, incidents, t0); err != nil || string(in.State) != "null" {
				t.Errorf("saved state = %s, %v; want none", in.State, err)
			}
		})
	}
}

// checkRequest is a check run of incidents by agWorker.
func checkRequest() domain.RunRequest {
	return domain.RunRequest{
		OrgID: testOrg, FactoryID: factoryID, Version: v1, Kind: domain.RunKindCheck, Bundle: domain.TaskKindCheck, Trigger: incidents,
		Agent: agWorker, Model: modelOpus, ModelConnection: connModel, Timeout: 5 * time.Minute,
	}
}

// A check run records a run of kind check without an item, clones nothing, signs a check token
// naming its trigger and no item or station, and writes no item event.
func TestRunTask_Check(t *testing.T) {
	t.Parallel()

	h := checkHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`), checkTrigger(incidents))

	res, err := h.e.RunTask(t.Context(), checkRequest())
	if err != nil || res.Status != domain.RunSucceeded || res.RunID == "" {
		t.Fatalf("RunTask = %+v, %v; want a succeeded run with its record", res, err)
	}

	prepares, runs := h.worker.Prepares(), h.worker.Runs()
	if len(prepares) != 1 || len(runs) != 1 {
		t.Fatalf("%d prepares and %d agent runs; want one of each", len(prepares), len(runs))
	}

	if box := prepares[0].Sandbox; box.ItemID != "" || box.Station != "" || box.Agent != agWorker {
		t.Errorf("sandbox = %+v; want %s's, without an item or a station", box, agWorker)
	}

	h.expectCheckToken(t, runs[0].Env[envTaskToken])

	if events := h.store.Events(""); len(events) != 0 {
		t.Errorf("events without an item = %+v; want none", events)
	}

	h.expectSandboxes(t, 0)
}

// expectCheckToken fails the test unless token is a check token of incidents with no item,
// station or knowledge.
func (h *harness) expectCheckToken(t *testing.T, token string) {
	t.Helper()

	c := h.claims(t, token)
	if c.Kind != domain.TaskKindCheck || c.Trigger != incidents || c.ItemID != "" || c.Station != "" || len(c.Knowledge) != 0 {
		t.Errorf("token claims = %+v; want a check token of %s with no item, station or knowledge", c, incidents)
	}
}

// A check run with an item, a repository or knowledge, or without its trigger, is refused.
func TestRunTask_CheckRefused(t *testing.T) {
	t.Parallel()

	h := checkHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`), checkTrigger(incidents))

	for name, edit := range map[string]func(*domain.RunRequest){
		"an item":      func(r *domain.RunRequest) { r.Item = "item-1" },
		"a repository": func(r *domain.RunRequest) { r.Repository = domain.Repository{URL: repoURL} },
		"knowledge":    func(r *domain.RunRequest) { r.Knowledge = []domain.KnowledgePin{{Source: "handbook"}} },
		"no trigger":   func(r *domain.RunRequest) { r.Trigger = "" },
	} {
		req := checkRequest()
		edit(&req)

		if _, err := h.e.RunTask(t.Context(), req); !errors.Is(err, domain.ErrBadRunRequest) {
			t.Errorf("a check with %s = %v; want ErrBadRunRequest", name, err)
		}
	}

	h.expectSandboxes(t, 0)
}
