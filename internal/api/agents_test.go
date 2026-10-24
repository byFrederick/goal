package api_test

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

const backendStats = "/v1/orgs/acme/factories/line/agents/backend/stats"

// The backend agent builds an item in 30 minutes and a person sends it back from merge: one
// task, sent back with the person's reason, its run's turns and spend, and the call it was denied.
func TestAPI_AgentStats(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, stBuild, domain.StateQueued, t0)
	it := h.claim(t, 0)

	run := h.startRun(t, it, "", t0)
	if err := h.store.FinishRun(inOrg(t), run, domain.RunResult{Status: domain.RunSucceeded, Turns: 12, CostUSD: 0.6}, t0); err != nil {
		t.Fatal(err)
	}

	if err := h.store.AppendEvents(inOrg(t), domain.Event{
		FactoryID: lineID, ItemID: it.ID, RunID: run, At: t0, Kind: domain.EventToolDenied, Station: stBuild, Actor: agBackend,
		Data: map[string]any{"tool": "Bash", "id": "toolu_1"},
	}); err != nil {
		t.Fatal(err)
	}

	h.clock.Advance(30 * time.Minute)

	lease, _ := h.store.Lease(it.ID)
	h.move(t, it, domain.Position{Station: stMerge, State: domain.StateWaitingHuman}, func(tr *ports.Transition) {
		tr.Lease, tr.Outcome = &lease, domain.LeaseAdvanced
	})

	h.clock.Advance(10 * time.Minute)

	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/"+string(it.ID)+"/reject", `{"reason":"Needs a test"}`, nil); code != http.StatusOK {
		t.Fatalf("reject = %d", code)
	}

	var got gen.AgentStats
	if code := h.call(t, http.MethodGet, backendStats, "", &got); code != http.StatusOK {
		t.Fatalf("stats = %d", code)
	}

	expectBackendStats(t, got)
}

func expectBackendStats(t *testing.T, got gen.AgentStats) {
	t.Helper()

	want := gen.AgentStats{
		Agent: agBackend, From: day(2026, 10, 17), To: day(2026, 10, 23), Model: got.Model,
		Stations:      []gen.AgentStation{{Id: stBuild, Slots: 2, Constraint: true}},
		TasksFinished: 1, SentBack: gen.SentBack{Count: 1, Ratio: new(1.0)},
		MedianTurns: new(12.0), SpendPerTaskUsd: new(0.6), TaskTimeP85Minutes: new(30.0), SpendUsd: 0.6,
		Reasons:         []gen.SendBackReason{{Label: "needs a test", Count: 1}},
		DeniedToolCalls: gen.DeniedToolCalls{Count: 1, Tools: []gen.DeniedTool{{Tool: "Bash", Count: 1}}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("stats =\n  %s\nwant 1 task of 30 min at build, sent back by a person, 12 turns, $0.60, Bash denied:\n  %s",
			asJSON(t, got), asJSON(t, want))
	}
}

func day(y int, m time.Month, d int) openapi_types.Date {
	return openapi_types.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// An idle agent has no ratios to show; reasons and denials are capped at 5 and 3.
func TestAPI_AgentStatsOfAnIdleAgent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var got gen.AgentStats
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/agents/frontend/stats", "", &got); code != http.StatusOK {
		t.Fatalf("stats = %d", code)
	}

	if got.TasksFinished != 0 || got.SentBack.Ratio != nil || got.MedianTurns != nil || got.SpendPerTaskUsd != nil ||
		got.TaskTimeP85Minutes != nil || got.Reasons == nil || len(got.Reasons) != 0 || got.DeniedToolCalls.Tools == nil {
		t.Errorf("stats of an idle agent = %+v; want zeros, nulls and empty lists", got)
	}
}

func TestAPI_AgentStatsRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var p gen.Problem
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/agents/nobody/stats", "", &p); code != http.StatusNotFound ||
		deref(p.Code) != "agent-not-found" {
		t.Errorf("stats of an unknown agent = %d %+v; want 404 agent-not-found", code, p)
	}

	if code := h.call(t, http.MethodGet, backendStats+"?to=2026-10-30", "", nil); code != http.StatusBadRequest {
		t.Errorf("stats to a future day = %d; want 400", code)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/nowhere/agents/backend/stats", "", nil); code != http.StatusNotFound {
		t.Errorf("stats of an unknown factory = %d; want 404", code)
	}
}
