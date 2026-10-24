package storetest

import (
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	agReviewer = "reviewer"
	// keyReason is the data key of a rejection's or a move's reason.
	keyReason = "reason"
)

// review is the gate after work: its reviewer sends items back to work.
var review = domain.Station{ID: stNext, Kind: domain.KindAgent, Agent: agReviewer, Agents: 1}

// An agent's statistics over a window: its finished tasks and their p85 from the lease spans that
// ended in it, the advanced ones later sent back by another station with what the rejection
// said (category, else the reason; a person's move back by its reason), its runs started in it (spend of all, median turns of the
// succeeded) and the tool calls those runs were denied. Another agent's work, a task advanced
// before the window and another organization count for nothing.
func testAgentStats(f *fixture) {
	t := f.t
	h := func(n float64) time.Time { return t1.Add(time.Duration(n * float64(time.Hour))) }

	d := f.create(t, domain.NewItem{CreatedAt: h(-6)})
	a := f.create(t, domain.NewItem{CreatedAt: h(-5)})
	b := f.create(t, domain.NewItem{CreatedAt: h(-4)})
	e := f.create(t, domain.NewItem{CreatedAt: h(-3.5)})

	// d: advanced before the window, sent back inside it; then parked.
	dRun := f.run(t, d, agWorker, h(-3), domain.RunResult{Status: domain.RunSucceeded, Turns: 50, CostUSD: 9})
	f.deny(t, d, dRun, h(-2), "Bash")
	_, l := f.claim(t, work, 0, h(-3))
	f.move(t, next(d, l, h(-2)))
	_, l = f.claim(t, review, 0, h(-2))
	f.sendBack(t, d, l, h(-1), map[string]any{keyReason: "old news"})
	f.move(t, ports.Transition{
		ItemID: d.ID, From: domain.Position{Station: stWork, State: domain.StateQueued},
		To: domain.Position{Station: stWork, State: domain.StateBlocked}, At: h(-1),
	})

	// a and b advance; review sends both back, a with a category, b with a reason only.
	aRun := f.run(t, a, agWorker, h(0), domain.RunResult{Status: domain.RunSucceeded, Turns: 10, CostUSD: 0.5})
	f.deny(t, a, aRun, h(0.5), "Bash")
	f.deny(t, a, aRun, h(0.6), "Bash")

	_, l = f.claim(t, work, 0, h(0))
	f.move(t, next(a, l, h(1)))

	bRun := f.run(t, b, agWorker, h(1), domain.RunResult{Status: domain.RunSucceeded, Turns: 20, CostUSD: 0.7})
	f.gatewayCall(t, b, bRun, h(1.5), "denied")
	f.gatewayCall(t, b, bRun, h(1.6), "ok")

	_, l = f.claim(t, work, 1, h(1))
	f.move(t, next(b, l, h(2)))

	revRun := f.run(t, a, agReviewer, h(2), domain.RunResult{Status: domain.RunSucceeded, Turns: 99, CostUSD: 5})
	f.deny(t, a, revRun, h(2.5), "Write")

	_, l = f.claim(t, review, 0, h(2))
	f.sendBack(t, a, l, h(3), map[string]any{keyReason: "No links to dashboards", "category": "Claims without evidence"})
	_, l = f.claim(t, review, 0, h(3))
	f.sendBack(t, b, l, h(5), map[string]any{keyReason: "  Tests Missing  "})

	// a's second round advances and stays; b's second lease is blocked (not a finished task).
	f.run(t, a, agWorker, h(4), domain.RunResult{Status: domain.RunFailed, Turns: 4, CostUSD: 0.2})
	_, l = f.claim(t, work, 0, h(4))
	f.move(t, next(a, l, h(6)))
	_, l = f.claim(t, work, 1, h(5))
	f.move(t, ports.Transition{
		ItemID: b.ID, From: domain.Position{Station: stWork, State: domain.StateInProgress},
		To: domain.Position{Station: stWork, State: domain.StateBlocked}, Lease: &l, Outcome: domain.LeaseBlocked, At: h(5.5),
	})

	// e advances; a person moves it back from next to work with a reason.
	_, l = f.claim(t, work, 1, h(6))
	f.move(t, next(e, l, h(7)))
	f.move(t, ports.Transition{
		ItemID: e.ID, From: domain.Position{Station: stNext, State: domain.StateQueued},
		To: domain.Position{Station: stWork, State: domain.StateQueued}, Rework: true, At: h(8),
		Events: []domain.Event{{
			FactoryID: e.FactoryID, ItemID: e.ID, At: h(8), Kind: domain.EventMoved, Station: stNext, Actor: "Local user",
			Data: map[string]any{"from": stNext, "to": stWork, "manual": true, keyReason: "Wrong repo"},
		}},
	})

	got, err := f.Flow.AgentStats(f.ctx(t), f.factory, agWorker, h(0), h(10))
	if err != nil {
		t.Fatal(err)
	}

	got.MedianTurns = roundTo(got.MedianTurns, 6)
	got.SpendUSD = roundTo(got.SpendUSD, 6)
	want := domain.AgentStats{
		TasksFinished: 4, Advanced: 4, SentBack: 3,
		Reasons: []domain.Tally{
			{Label: "Claims without evidence", Count: 1}, {Label: "tests missing", Count: 1}, {Label: "wrong repo", Count: 1},
		},
		TaskTimeP85:   93 * time.Minute, // 1 h, 1 h, 1 h and 2 h
		SucceededRuns: 2, MedianTurns: 15, SpendUSD: 1.4,
		Denied: []domain.Tally{{Label: "Bash", Count: 2}, {Label: "github__merge_pr", Count: 1}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("AgentStats =\n  %+v\nwant\n  %+v", got, want)
	}

	other, err := f.Flow.AgentStats(domain.WithOrg(t.Context(), f.NewOrg(t)), f.factory, agWorker, h(0), h(10))
	if err != nil || !reflect.DeepEqual(other, domain.AgentStats{}) {
		t.Errorf("another organization measures %+v (%v); want nothing", other, err)
	}
}

// run records a finished run of agent on it.
func (f *fixture) run(t *testing.T, it domain.Item, agent string, at time.Time, res domain.RunResult) domain.RunID {
	t.Helper()

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: stWork, Agent: agent, Model: model, StartedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Store.FinishRun(f.ctx(t), id, res, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	return id
}

// deny records a tool call the run's policy refused.
func (f *fixture) deny(t *testing.T, it domain.Item, run domain.RunID, at time.Time, tool string) {
	t.Helper()

	f.event(t, domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, RunID: run, At: at, Kind: domain.EventToolDenied, Station: stWork,
		Actor: agWorker, Data: map[string]any{"tool": tool, "id": "toolu_1"},
	})
}

// gatewayCall records a gateway tool call of the run with outcome.
func (f *fixture) gatewayCall(t *testing.T, it domain.Item, run domain.RunID, at time.Time, outcome string) {
	t.Helper()

	f.event(t, domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, RunID: run, At: at, Kind: "gateway_tool_call", Station: stWork,
		Actor: agWorker, Data: map[string]any{"connection": "github", "tool": "merge_pr", "outcome": outcome},
	})
}

func (f *fixture) event(t *testing.T, e domain.Event) {
	t.Helper()

	if err := f.Store.AppendEvents(f.ctx(t), e); err != nil {
		t.Fatal(err)
	}
}

// sendBack has the review gate, holding lease l, send it back to work with a gate_rejected event
// carrying data.
func (f *fixture) sendBack(t *testing.T, it domain.Item, l domain.Lease, at time.Time, data map[string]any) {
	t.Helper()

	f.move(t, ports.Transition{
		ItemID: it.ID, From: domain.Position{Station: stNext, State: domain.StateInProgress},
		To: domain.Position{Station: stWork, State: domain.StateQueued}, Lease: &l, Outcome: domain.LeaseRejected, Rework: true, At: at,
		Events: []domain.Event{{
			FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: domain.EventGateRejected, Station: stNext, Actor: agReviewer, Data: data,
		}},
	})
}

func roundTo(x float64, places int) float64 {
	p := 1.0
	for range places {
		p *= 10
	}

	return float64(int64(x*p+0.5)) / p
}
