package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// spendGoald answers GET /v1/orgs/acme/spend with body, and records the query it was sent.
func spendGoald(t *testing.T, body []byte) (*httptest.Server, func() url.Values) {
	t.Helper()

	var (
		mu    sync.Mutex
		query url.Values
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/spend", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		query = r.URL.Query()
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, func() url.Values {
		mu.Lock()
		defer mu.Unlock()

		return query
	}
}

func spendJSON(t *testing.T, v gen.OrgSpend) []byte {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func goalSpend(t *testing.T, api string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), append([]string{"spend"}, args...), &printer{w: &buf}, testEnv(t, api, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

// The totals, the cost per merged item, rework and one line per budget print; --window reaches
// goald.
func TestSpend_Text(t *testing.T) {
	t.Parallel()

	v := func(f float64) *float64 { return &f }
	merged, andon, over := 4, "rework rate over 40%", "spent $71.00 of the $70.00 weekly budget in the last 7 days"

	srv, sent := spendGoald(t, spendJSON(t, gen.OrgSpend{
		Window: gen.Month, Since: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Until: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC),
		Complete: true, TotalUsd: 84, ThisMonthUsd: v(84), LastMonthUsd: v(120.5), Merged: &merged, CostPerMergedUsd: v(21),
		ReworkUsd: v(21), ReworkShare: v(0.25),
		Budgets: []gen.SpendBudget{
			{
				Factory: gen.FactoryHit{Id: "f1", Name: "docs", Title: "Docs"}, TodayUsd: 1, WeekUsd: 3,
				State: gen.SpendBudgetStateStopped, Reason: &andon, ModelConnections: []gen.ConnectionHit{{Name: "claude", Kind: "anthropic"}},
			},
			{
				Factory: gen.FactoryHit{Id: "f2", Name: "web", Title: "Web"}, DailyUsd: v(15), WeeklyUsd: v(70), TodayUsd: 0, WeekUsd: 71,
				State: gen.SpendBudgetStateOverBudget, Reason: &over, ModelConnections: []gen.ConnectionHit{},
			},
		},
	}))

	out, err := goalSpend(t, srv.URL, "--window", "month")
	if err != nil {
		t.Fatalf("goal spend --window month = %v", err)
	}

	if w := sent().Get("window"); w != "month" {
		t.Errorf("goald was sent window=%q; want month", w)
	}

	checkGolden(t, "spend-results", out)

	// Without --window goald is asked for this week; an incomplete answer says so.
	srv, sent = spendGoald(t, spendJSON(t, gen.OrgSpend{
		Window: gen.Week, Since: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), Until: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC), TotalUsd: 1,
	}))

	out, err = goalSpend(t, srv.URL)
	if want := "This week (2026-10-19 to 2026-10-23): $1.00\n  Only the factories you see are counted\n"; err != nil || out != want {
		t.Errorf("goal spend = %q, %v; want %q", out, err, want)
	}

	if w := sent().Get("window"); w != "week" {
		t.Errorf("goald was sent window=%q; want week", w)
	}
}

// --json writes goald's body as it came.
func TestSpend_JSONAsSent(t *testing.T) {
	t.Parallel()

	body := []byte(`{"window": "week", "total_usd": 3,
 "budgets": []}`)
	srv, _ := spendGoald(t, body)

	if out, err := goalSpend(t, srv.URL, "--json"); err != nil || out != string(body) {
		t.Errorf("goal spend --json = %q, %v; want %q", out, err, body)
	}
}

// A window other than week, month or 90d, or an argument, is the usage (exit 2) before goald is
// asked.
func TestSpend_BadWindowIsUsage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"--window", "year"}, {"--window", "7d"}, {"now"}} {
		if _, err := goalSpend(t, "http://127.0.0.1:1", args...); !errors.Is(err, errUsage) {
			t.Errorf("goal spend %q = %v; want the usage", args, err)
		}
	}
}
