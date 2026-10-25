package engine_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const week = 7 * 24 * time.Hour

// logBuffer collects JSON log lines from goroutines.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.Write(p)
}

// String is everything logged so far.
func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.String()
}

// budgetLogs returns the budget transitions logged so far, in order, as "<msg> <gauge>".
func (l *logBuffer) budgetLogs(t *testing.T) []string {
	t.Helper()

	l.mu.Lock()
	defer l.mu.Unlock()

	var out []string

	sc := bufio.NewScanner(bytes.NewReader(l.b.Bytes()))
	for sc.Scan() {
		var rec struct {
			Msg   string  `json:"msg"`
			Gauge float64 `json:"goal_factory_budget_exhausted"`
		}

		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}

		if strings.HasPrefix(rec.Msg, "budget_") {
			out = append(out, rec.Msg+" "+map[bool]string{true: "1", false: "0"}[rec.Gauge == 1])
		}
	}

	return out
}

// budgeted is a line inbox → work (slots agent slots) → stations... → done whose weekly
// budget is weeklyUSD with policy p.
func budgeted(weeklyUSD float64, p domain.BudgetPolicy, slots int, stations ...domain.Station) domain.Line {
	l := lineOf(append([]domain.Station{agentStation(stWork, agWorker, slots, slots)}, stations...)...)
	l.Runtime.WeeklyUSD, l.Runtime.OnExhausted = weeklyUSD, p

	return l
}

// spent records earlier work of the factory: a done item whose run, started at at, cost usd.
func (h *harness) spent(t *testing.T, at time.Time, usd float64) {
	t.Helper()

	ctx := inOrg(t)

	it, err := h.store.CreateItem(ctx, domain.NewItem{
		FactoryID: factoryID, Type: typeFeature, Title: "earlier work", Station: stDone, State: domain.StateDone,
		Class: domain.ClassStandard, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	run, err := h.store.StartRun(ctx, ports.NewRun{FactoryID: factoryID, ItemID: it.ID, Station: stWork, Agent: agWorker, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at); err != nil {
		t.Fatal(err)
	}
}

// waitingHuman puts an item at the human station, waiting for a decision.
func (h *harness) waitingHuman(t *testing.T, station string) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: factoryID, Type: typeFeature, Title: "needs an OK", Station: station, State: domain.StateWaitingHuman,
		Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

func (h *harness) tick(t *testing.T) {
	t.Helper()

	if err := h.e.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}
}

func TestWeeklyBudget_DrainSkipsAgentClaimsOnly(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h := newHarness(t, budgeted(10, domain.BudgetDrain, 2, domain.Station{ID: stApprove, Kind: domain.KindHuman}),
		porttest.Block(release, `{}`))

	running := h.submit(t, domain.NewItem{Title: "running"})
	h.tick(t) // under budget: claimed

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	h.spent(t, t0, 10) // the budget is used up
	queued := h.submit(t, domain.NewItem{Title: "queued"})
	decision := h.waitingHuman(t, stApprove)

	h.tick(t)

	// The queue station still releases; the agent station claims nothing though a slot is free.
	if got := h.item(t, queued.ID); got.Station != stWork || got.State != domain.StateQueued {
		t.Errorf("queued item = %s/%s; want released to %s and left queued", got.Station, got.State, stWork)
	}

	// Human decisions still move.
	if _, err := h.e.Approve(inOrg(t), decision.ID, domain.LocalUser); err != nil {
		t.Fatalf("approve while the budget is used up: %v", err)
	}

	// The run in progress finishes and its item moves on.
	close(release)
	h.wait(t, h.e)

	if got := h.item(t, running.ID); got.Station != stApprove || len(h.eventsOf(running.ID, domain.EventAgentCancelled)) != 0 {
		t.Errorf("running item = %s/%s; want it finished and moved to %s, not cancelled", got.Station, got.State, stApprove)
	}

	if got := h.item(t, decision.ID); got.State != domain.StateDone {
		t.Errorf("approved item = %s; want done", got.State)
	}

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d runs started; want only the one claimed under budget", n)
	}

	if got, want := h.logs.budgetLogs(t), []string{"budget_exhausted 1"}; !slices.Equal(got, want) {
		t.Errorf("budget logs = %v; want %v", got, want)
	}
}

func TestWeeklyBudget_StopCancelsRunningAndRequeues(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	t.Cleanup(func() { close(release) })

	h := newHarness(t, budgeted(10, domain.BudgetStop, 1), porttest.Block(release, `{}`))

	running := h.submit(t, domain.NewItem{Title: "running"})
	waiting := h.submit(t, domain.NewItem{Title: "waiting"})
	h.tick(t) // under budget: the one slot is claimed

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	h.spent(t, t0, 12)
	h.tick(t) // over budget: the run is cancelled
	h.wait(t, h.e)

	got := h.item(t, running.ID)
	if got.Station != stWork || got.State != domain.StateQueued {
		t.Errorf("cancelled item = %s/%s; want back in the queue at %s", got.Station, got.State, stWork)
	}

	if _, held := h.store.Lease(running.ID); held {
		t.Error("the cancelled item still holds its lease")
	}

	cancelled := h.eventsOf(running.ID, domain.EventAgentCancelled)
	if len(cancelled) != 1 || cancelled[0].Data["reason"] != "budget" || cancelled[0].Station != stWork {
		t.Errorf("agent_cancelled events = %+v; want one at %s with reason budget", cancelled, stWork)
	}

	if runs := h.runs(t, testOrg, running.ID); len(runs) != 1 || runs[0].Result.Status != domain.RunCancelled {
		t.Errorf("runs = %+v; want the one run cancelled", runs)
	}

	h.expectOutcomes(t, running.ID, domain.LeaseReleased)

	h.tick(t) // still over budget: nothing is claimed again
	h.wait(t, h.e)

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d runs started; want no claim while the budget is used up", n)
	}

	if w := h.item(t, waiting.ID); w.State != domain.StateQueued {
		t.Errorf("waiting item = %s; want still queued", w.State)
	}
}

func TestWeeklyBudget_ResumesWhenWindowMoves(t *testing.T) {
	t.Parallel()

	h := newHarness(t, budgeted(10, domain.BudgetDrain, 1), porttest.Succeed(`{}`))
	h.spent(t, t0, 10)
	it := h.submit(t, domain.NewItem{})

	h.step(t)
	h.clock.Advance(week) // the run started exactly 7×24h ago still counts
	h.step(t)

	if n := len(h.worker.Runs()); n != 0 {
		t.Fatalf("%d runs started; want none while the spend is in the window", n)
	}

	h.clock.Advance(time.Second) // now it has left the window
	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Errorf("item = %s/%s; want claimed and done once the spend left the window", got.Station, got.State)
	}

	if got, want := h.logs.budgetLogs(t), []string{"budget_exhausted 1", "budget_resumed 0"}; !slices.Equal(got, want) {
		t.Errorf("budget logs = %v; want %v", got, want)
	}
}

func TestWeeklyBudget_ResumesWhenTheCapRises(t *testing.T) {
	t.Parallel()

	h := newHarness(t, budgeted(10, domain.BudgetDrain, 1), porttest.Succeed(`{}`))
	h.spent(t, t0, 10)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	raised := h.line
	raised.Runtime.WeeklyUSD = 20 // a publish raised the cap

	if err := h.e.SetLines([]domain.Line{raised}); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Errorf("item = %s/%s; want claimed and done under the raised cap", got.Station, got.State)
	}

	if got, want := h.logs.budgetLogs(t), []string{"budget_exhausted 1", "budget_resumed 0"}; !slices.Equal(got, want) {
		t.Errorf("budget logs = %v; want %v", got, want)
	}
}

// spendReads counts the SpendSince calls made through a store.
type spendReads struct {
	ports.Store

	calls atomic.Int64
}

func (s *spendReads) SpendSince(ctx context.Context, f domain.FactoryID, since time.Time) (float64, error) {
	s.calls.Add(1)
	return s.Store.SpendSince(ctx, f, since)
}

func TestWeeklyBudget_UnsetMeansUnlimited(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
	h.spent(t, t0, 1000)

	reads := &spendReads{Store: h.store}
	h.e = h.engine(t, reads)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Errorf("item = %s/%s; want claimed and done without a weekly budget", got.Station, got.State)
	}

	if n := reads.calls.Load(); n != 0 {
		t.Errorf("SpendSince read %d times; want no reads without a weekly budget", n)
	}

	if logs := h.logs.budgetLogs(t); len(logs) != 0 {
		t.Errorf("budget logs = %v; want none", logs)
	}
}

// Pausing takes a line out of releases and claims, not out of its budget: a used-up budget
// with the stop policy still cancels the paused line's runs.
func TestWeeklyBudget_StopStillCancelsTheRunsOfAPausedLine(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	t.Cleanup(func() { close(release) })

	line := budgeted(10, domain.BudgetStop, 1)
	h := newHarness(t, line, porttest.Block(release, `{}`))
	running := h.submit(t, domain.NewItem{Title: "running"})
	h.tick(t)

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	line.Paused = true
	if err := h.e.SetLines([]domain.Line{line}); err != nil {
		t.Fatal(err)
	}

	h.spent(t, t0, 12)
	h.tick(t)
	h.wait(t, h.e)

	if got := h.item(t, running.ID); got.Station != stWork || got.State != domain.StateQueued {
		t.Errorf("item of the paused line = %s/%s; want its run cancelled and the item back in the queue", got.Station, got.State)
	}

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d runs started; want nothing claimed while paused", n)
	}
}
