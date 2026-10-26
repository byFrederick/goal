package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
