package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// statusAPI answers goal status's reads for two factories: lab finished items with touches,
// support finished none. gotFrom is the first day the last touch metrics read asked for.
func statusAPI(t *testing.T, gotFrom *string) *httptest.Server {
	t.Helper()

	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}

	lab := gen.TouchStats{
		Finished: 12, Touches: 19, PerItemMean: 1.58, TouchlessRate: 0.31,
		ByStation: []gen.TouchCount{
			{Key: "sign-off", Touches: 12, Planned: 12},
			{Key: "review", Touches: 6, Unplanned: 6},
			{Key: "", Touches: 1, Unplanned: 7},
		},
	}
	total := gen.TouchStats{Finished: 12, Touches: 19, PerItemMean: 1.58, TouchlessRate: 0.31}
	lead := struct {
		P50 float64 `json:"p50"`
		P85 float64 `json:"p85"`
	}{P50: 3, P85: 9.24}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/metrics/touches", func(w http.ResponseWriter, r *http.Request) {
		*gotFrom = r.URL.Query().Get("from")

		reply(w, gen.OrgTouchMetrics{Total: total, Factories: []gen.FactoryTouchMetrics{
			{FactoryId: "f-lab", Name: "lab", Stats: lab},
			{FactoryId: "f-support", Name: "support", Stats: gen.TouchStats{}},
		}})
	})
	mux.HandleFunc("GET /v1/orgs/acme/factories/lab/metrics/touches", func(w http.ResponseWriter, r *http.Request) {
		*gotFrom = r.URL.Query().Get("from")

		reply(w, gen.TouchMetrics{Stats: lab})
	})

	metrics := map[string]gen.Metrics{
		"f-lab":     {Wip: 5, ItemsDone: 12, LeadTimeHours: &lead},
		"lab":       {Wip: 5, ItemsDone: 12, LeadTimeHours: &lead},
		"f-support": {Wip: 2},
	}

	mux.HandleFunc("GET /v1/orgs/acme/factories/{f}/metrics", func(w http.ResponseWriter, r *http.Request) {
		m, ok := metrics[r.PathValue("f")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		reply(w, m)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestStatus_EachFactoryAndTheTotal(t *testing.T) {
	t.Parallel()

	var from string

	srv := statusAPI(t, &from)
	env := testEnv(t, srv.URL, map[string]string{envOrg: orgAcme})

	goal := func(args ...string) (string, error) {
		var buf bytes.Buffer

		err := run(t.Context(), append([]string{"status"}, args...), &printer{w: &buf}, env, nil)

		return buf.String(), err
	}

	out, err := goal("--window", "14d")
	want := "acme, the last 14 days (from " + from + ", UTC)\n" +
		"FACTORY  WIP  DONE  LEAD TIME P85  TOUCHES/ITEM  TOUCHLESS  MOST UNPLANNED\n" +
		"lab      5    12    9.2 h          1.6           31%        review (6)\n" +
		"support  2    0     -              -             -          -\n" +
		"all      7    12    -              1.6           31%        -\n"

	if err != nil || out != want {
		t.Errorf("goal status --window 14d =\n%s(%v); want\n%s", out, err, want)
	}

	// The window ends today: it starts 13 days before, in UTC.
	if day := time.Now().UTC().AddDate(0, 0, -13).Format(time.DateOnly); from != day {
		t.Errorf("from = %q; want %q", from, day)
	}

	out, err = goal("--factory", "lab")
	want = "acme, the last 7 days (from " + from + ", UTC)\n" +
		"FACTORY  WIP  DONE  LEAD TIME P85  TOUCHES/ITEM  TOUCHLESS  MOST UNPLANNED\n" +
		"lab      5    12    9.2 h          1.6           31%        review (6)\n"

	if err != nil || out != want {
		t.Errorf("goal status --factory lab =\n%s(%v); want\n%s", out, err, want)
	}

	for _, args := range [][]string{{"--window", "0d"}, {"--window", "91d"}, {"--window", "7"}, {"lab"}} {
		if _, err := goal(args...); !errors.Is(err, errUsage) {
			t.Errorf("goal status %v = %v; want a usage error", args, err)
		}
	}
}

func TestShowItem_TouchesOnStderr(t *testing.T) {
	t.Parallel()

	station, reason := "review", gen.TouchReason("missing_knowledge")
	at := time.Date(2026, 10, 24, 14, 5, 0, 0, time.UTC)
	item := gen.Item{Id: "it-1", Ref: closedRef, Touches: &gen.ItemTouches{
		Count: 2, Planned: 1, Unplanned: 1,
		List: []gen.Touch{
			{EventId: 7, Kind: "note", Station: &station, At: at, Actor: gen.Actor{Name: "A. Lee"}, Reason: &reason},
			{EventId: 9, Kind: "approved", At: at.Add(time.Hour), Actor: gen.Actor{Name: "M. Okafor"}, Planned: true},
		},
	}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/items/{item}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(item)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer

	env := testEnv(t, srv.URL, map[string]string{envOrg: orgAcme})
	if err := run(t.Context(), []string{"item", closedRef}, &printer{w: &stdout, e: &stderr}, env, nil); err != nil {
		t.Fatal(err)
	}

	var got gen.Item
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || got.Touches == nil || got.Touches.Count != 2 {
		t.Errorf("stdout is not the item's JSON with its touches: %v\n%s", err, stdout.String())
	}

	want := "2 human touches (1 planned, 1 unplanned):\n" +
		"  2026-10-24 14:05 UTC  A. Lee  note at review, unplanned, reason: missing knowledge\n" +
		"  2026-10-24 15:05 UTC  M. Okafor  approved no station, planned, no reason\n"
	if stderr.String() != want {
		t.Errorf("stderr =\n%s; want\n%s", stderr.String(), want)
	}
}

// teardownAPI serves one item's teardown at GET /v1/orgs/acme/items/goal%232/teardown and fails
// the test if goal item reads the item itself.
func teardownAPI(t *testing.T, status int, v any) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/items/{item}/teardown", func(w http.ResponseWriter, r *http.Request) {
		if got := r.PathValue("item"); got != closedRef {
			t.Errorf("teardown of %q; want %q", got, closedRef)
		}

		w.Header().Set("Content-Type", "application/json")
		if status != http.StatusOK {
			w.Header().Set("Content-Type", "application/problem+json")
		}

		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	})
	mux.HandleFunc("GET /v1/orgs/acme/items/{item}", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("goal item --teardown read the item; it reads only its teardown")
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestShowItem_Teardown(t *testing.T) {
	t.Parallel()

	target, review, build, why := int64(3*3600), "review", "build", "the tests do not cover the empty list"
	at := time.Date(2026, 10, 24, 14, 5, 0, 0, time.UTC)
	td := gen.Teardown{
		LeadTimeSeconds: 2*3600 + 5*60, TargetSeconds: &target, CostUsd: 1.2, Open: false,
		Rework:     gen.TeardownRework{Loops: 1, Seconds: 40 * 60, CostUsd: 0.3},
		TouchCount: 1, TouchWaitSeconds: 12 * 60,
		Stations: []gen.TeardownStation{
			{Station: build, WorkingSeconds: 3600, WaitingSeconds: 5 * 60, TotalSeconds: 3600 + 5*60},
			{Station: review, WorkingSeconds: 45, WaitingSeconds: 59*60 + 15, TotalSeconds: 3600},
		},
		Total: gen.TeardownTime{WorkingSeconds: 3600 + 45, WaitingSeconds: 64*60 + 15, TotalSeconds: 2*3600 + 5*60},
		Runs: []gen.TeardownRun{
			{Id: "r1", Station: build, Agent: "engineer", Model: "claude-opus-5-5", Turns: 31, Seconds: 50 * 60, CostUsd: 0.9, Outcome: gen.TeardownRunOutcomeFinished},
			{Id: "r2", Station: build, Agent: "engineer", Model: "claude-opus-5-5", Turns: 9, Seconds: 10 * 60, CostUsd: 0.3, Outcome: gen.TeardownRunOutcomeCheckFailed},
		},
		RunTotal: gen.TeardownRunTotal{Runs: 2, Turns: 40, Seconds: 3600, CostUsd: 1.2},
		Loops: []gen.ReworkLoop{
			{EventId: 7, At: at, Cause: gen.ReworkLoopCauseRejected, Station: review, To: &build, Reason: &why, Seconds: 40 * 60, CostUsd: 0.3},
		},
		Touches: []gen.TeardownTouch{
			{Touch: gen.Touch{EventId: 9, Kind: "approved", Station: &review, At: at.Add(time.Hour), Actor: gen.Actor{Name: "M. Okafor"}, Planned: true}, WaitSeconds: 12 * 60},
		},
	}

	srv := teardownAPI(t, http.StatusOK, td)

	var stdout, stderr bytes.Buffer

	env := testEnv(t, srv.URL, map[string]string{envOrg: orgAcme})
	if err := run(t.Context(), []string{"item", closedRef, "--teardown"}, &printer{w: &stdout, e: &stderr}, env, nil); err != nil {
		t.Fatal(err)
	}

	want := "lead time 2h 5m, target 3h; cost $1.20, no budget\n" +
		"rework: 1 loop, 40m, $0.30\n" +
		"human touches: 1, waited 12m\n" +
		"\n" +
		"Time by station\n" +
		"STATION  WORKING  WAITING  TOTAL\n" +
		"build    1h       5m       1h 5m\n" +
		"review   45s      59m 15s  1h\n" +
		"total    1h       1h 4m    2h 5m\n" +
		"\n" +
		"Runs\n" +
		"STATION  AGENT     MODEL            TURNS  TOOK  COST   OUTCOME\n" +
		"build    engineer  claude-opus-5-5  31     50m   $0.90  finished\n" +
		"build    engineer  claude-opus-5-5  9      10m   $0.30  check failed\n" +
		"total    2 runs                     40     1h    $1.20\n" +
		"\n" +
		"Why it went back\n" +
		"  rejected at review, to build: the tests do not cover the empty list (40m, $0.30)\n" +
		"\n" +
		"Human touches\n" +
		"  review  2026-10-24 15:05 UTC  waited 12m\n"
	if stdout.String() != want {
		t.Errorf("stdout =\n%s; want\n%s", stdout.String(), want)
	}
}

func TestShowItem_TeardownNotFound(t *testing.T) {
	t.Parallel()

	code := "item-not-found"
	srv := teardownAPI(t, http.StatusNotFound, gen.Problem{Title: "Item not found", Status: 404, Code: &code})

	var stdout bytes.Buffer

	env := testEnv(t, srv.URL, map[string]string{envOrg: orgAcme})

	err := run(t.Context(), []string{"item", closedRef, "--teardown"}, &printer{w: &stdout, e: &bytes.Buffer{}}, env, nil)
	if err == nil || !strings.Contains(err.Error(), "Item not found") {
		t.Errorf("err = %v; want the API's item-not-found message", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q; want nothing", stdout.String())
	}
}

// TestPrintTeardown_OpenItem: an open item's lead time and cost read "so far", and a factory with
// no target and an item with no budget say so.
func TestPrintTeardown_OpenItem(t *testing.T) {
	t.Parallel()

	budget := 5.0

	var stdout bytes.Buffer

	printTeardown(&printer{w: &stdout, e: &bytes.Buffer{}}, &gen.Teardown{Open: true, LeadTimeSeconds: 45, CostUsd: 0.5, BudgetUsd: &budget})

	first, _, _ := strings.Cut(stdout.String(), "\n")
	if want := "lead time 45s so far, no target; cost $0.50 so far, budget $5.00"; first != want {
		t.Errorf("headline = %q; want %q", first, want)
	}
}
