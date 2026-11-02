package api_test

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// A build whose check fails once, then a person sends the item back from merge: the teardown has
// its time per station, both loops of rework (the second still open) and the person's touch, set
// against the line's six-hour target and $4 budget. An unknown item, and an item read through
// another organization, are 404 item-not-found.
func TestAPI_GetItemTeardown(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := inOrg(t)
	minute := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }

	it := h.seed(t, stBuild, domain.StateQueued, t0)

	h.clock.Advance(2 * time.Minute)
	it = h.claim(t, 0)

	r1 := h.startRun(t, it, "", minute(2))
	if err := h.store.FinishRun(ctx, r1, domain.RunResult{Status: domain.RunSucceeded, Turns: 10, CostUSD: 1}, minute(10)); err != nil {
		t.Fatal(err)
	}

	if err := h.store.AppendEvents(ctx, domain.Event{
		FactoryID: lineID, ItemID: it.ID, RunID: r1, At: minute(10), Kind: domain.EventCheckFailed, Station: stBuild, Actor: domain.ActorEngine,
		Data: map[string]any{"command": "task check", "exit_code": 1},
	}); err != nil {
		t.Fatal(err)
	}

	r2 := h.startRun(t, it, "", minute(11))
	if err := h.store.FinishRun(ctx, r2, domain.RunResult{Status: domain.RunSucceeded, Turns: 5, CostUSD: 0.5}, minute(20)); err != nil {
		t.Fatal(err)
	}

	h.clock.Advance(18 * time.Minute)

	lease, _ := h.store.Lease(it.ID)
	h.move(t, it, domain.Position{Station: stMerge, State: domain.StateWaitingHuman}, func(tr *ports.Transition) {
		tr.Lease, tr.Outcome = &lease, domain.LeaseAdvanced
	})

	h.clock.Advance(10 * time.Minute)

	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/"+string(it.ID)+"/reject", `{"reason":"needs a test"}`, nil); code != http.StatusOK {
		t.Fatalf("reject = %d", code)
	}

	h.clock.Advance(5 * time.Minute)

	var got gen.Teardown
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(it.ID)+"/teardown", "", &got); code != http.StatusOK {
		t.Fatalf("teardown = %d", code)
	}

	wantStations := []gen.TeardownStation{
		{Station: stBuild, WorkingSeconds: 18 * 60, WaitingSeconds: 7 * 60, TotalSeconds: 25 * 60},
		{Station: stMerge, WaitingSeconds: 10 * 60, TotalSeconds: 10 * 60},
	}
	if !reflect.DeepEqual(got.Stations, wantStations) || got.Total.TotalSeconds != 35*60 || got.LeadTimeSeconds != 35*60 || !got.Open {
		t.Errorf("stations %+v total %+v lead %d open %v; want %+v, 35 minutes, open", got.Stations, got.Total, got.LeadTimeSeconds, got.Open, wantStations)
	}

	if got.TargetSeconds == nil || *got.TargetSeconds != 6*3600 || got.BudgetUsd == nil || *got.BudgetUsd != 4 || got.CostUsd != 1.5 || len(got.Runs) != 2 ||
		got.Runs[0].Outcome != gen.TeardownRunOutcomeCheckFailed || got.Runs[1].Outcome != gen.TeardownRunOutcomeFinished {
		t.Errorf("target %v budget %v cost %v runs %+v; want 6 h, $4, $1.50, a failed check then a finished run",
			got.TargetSeconds, got.BudgetUsd, got.CostUsd, got.Runs)
	}

	if len(got.Loops) != 2 {
		t.Fatalf("loops %+v, want the check failure and the send-back", got.Loops)
	}

	check, back := got.Loops[0], got.Loops[1]
	if check.Cause != gen.ReworkLoopCauseCheckFailed || check.Station != stBuild || check.Seconds != 10*60 || check.CostUsd != 0.5 || check.EventId == 0 {
		t.Errorf("check loop %+v, want check_failed at build, 10 minutes, $0.50, with its event", check)
	}

	if back.Cause != gen.ReworkLoopCauseRejected || back.Station != stMerge || deref(back.To) != stBuild ||
		deref(back.Reason) != "needs a test" || back.Seconds != 5*60 || back.CostUsd != 0 || back.EventId <= check.EventId {
		t.Errorf("send-back loop %+v, want rejected at merge to build, its reason, 5 minutes so far", back)
	}

	if got.Rework != (gen.TeardownRework{Loops: 2, Seconds: 15 * 60, CostUsd: 0.5}) {
		t.Errorf("rework %+v, want 2 loops, 15 minutes, $0.50", got.Rework)
	}

	if got.TouchCount != 1 || len(got.Touches) != 1 || got.Touches[0].Touch.EventId != back.EventId ||
		deref(got.Touches[0].Touch.Station) != stMerge || got.Touches[0].WaitSeconds != 10*60 || got.TouchWaitSeconds != 10*60 {
		t.Errorf("touches %d %+v, want the rejection at merge after 10 minutes' wait", got.TouchCount, got.Touches)
	}

	for _, path := range []string{
		"/v1/orgs/acme/items/acme%2399/teardown",
		"/v1/orgs/example-labs/items/" + string(it.ID) + "/teardown",
	} {
		var p gen.Problem
		if code := h.call(t, http.MethodGet, path, "", &p); code != http.StatusNotFound || deref(p.Code) != codeItemNotFound {
			t.Errorf("GET %s = %d %+v; want 404 item-not-found", path, code, p)
		}
	}
}
