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

// runsGoald answers GET /v1/orgs/acme/runs with status and body, and records the queries it was
// sent.
func runsGoald(t *testing.T, status int, contentType string, body []byte) (*httptest.Server, func() []url.Values) {
	t.Helper()

	var (
		mu      sync.Mutex
		queries []url.Values
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/runs", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()

		queries = append(queries, r.URL.Query())
		mu.Unlock()

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, func() []url.Values {
		mu.Lock()
		defer mu.Unlock()

		return queries
	}
}

func runsJSON(t *testing.T, v gen.OrgRunList) []byte {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func goalRuns(t *testing.T, api string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), append([]string{"runs"}, args...), &printer{w: &buf}, testEnv(t, api, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

func emptyRuns() gen.OrgRunList {
	return gen.OrgRunList{Runs: []gen.OrgRun{}, Summary: gen.RunSummary{SpendByFactory: []gen.FactorySpend{}}}
}

// Each flag becomes its query parameter, --search as q; a flag left unset sends nothing.
func TestRuns_FlagsBecomeQuery(t *testing.T) {
	t.Parallel()

	srv, sent := runsGoald(t, http.StatusOK, "application/json", runsJSON(t, emptyRuns()))

	if _, err := goalRuns(t, srv.URL, "--factory", "web", "--status", "failed", "--since", "2026-10-01T00:00:00Z",
		"--until", "2026-10-02T00:00:00+02:00", "--search", "login", "--limit", "20", "--cursor", "c1"); err != nil {
		t.Fatalf("goal runs with every flag = %v", err)
	}

	if _, err := goalRuns(t, srv.URL); err != nil {
		t.Fatalf("goal runs = %v", err)
	}

	qs := sent()
	if len(qs) != 2 {
		t.Fatalf("goald was sent %d requests; want 2", len(qs))
	}

	want := "cursor=c1&factory=web&limit=20&q=login&since=2026-10-01T00%3A00%3A00Z&status=failed&until=2026-10-02T00%3A00%3A00%2B02%3A00"
	if got := qs[0].Encode(); got != want {
		t.Errorf("goald was sent %s; want %s", got, want)
	}

	if len(qs[1]) != 0 {
		t.Errorf("goal runs without flags sent %v; want no parameters", qs[1])
	}
}

// A status or time goald would refuse is a usage error, and no request is made.
func TestRuns_BadFlagsAreUsage(t *testing.T) {
	t.Parallel()

	srv, sent := runsGoald(t, http.StatusOK, "application/json", runsJSON(t, emptyRuns()))

	for _, args := range [][]string{
		{"--status", "finished"}, {"--since", "yesterday"}, {"--until", "2026-10-01"}, {"--limit", "-1"}, {"extra"},
	} {
		if _, err := goalRuns(t, srv.URL, args...); !errors.Is(err, errUsage) {
			t.Errorf("goal runs %q = %v; want the usage", args, err)
		}
	}

	if n := len(sent()); n != 0 {
		t.Errorf("goald was sent %d requests; want none", n)
	}
}

// The table's rows come in the order sent, a running run's duration reads running, then the
// summary and the next page's command.
func TestRuns_Table(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	ended := started.Add(4*time.Minute + 12*time.Second)
	failedEnd := started.Add(-2*time.Hour + 65*time.Minute)
	cursor := "eyJ0IjoxfQ"

	list := gen.OrgRunList{
		Runs: []gen.OrgRun{
			{
				Id: "r3", Agent: "coder", Model: "claude-opus-5-5", Station: "code", Status: gen.RunStatusRunning,
				StartedAt: started.Add(10 * time.Minute), Turns: 3, CostUsd: 0.0007,
				Factory: gen.FactoryHit{Id: "f1", Name: searchFactory, Title: "Web"},
				Item:    gen.RunItem{Id: "i2", Ref: "web#2", Title: "Fix the login page"},
			},
			{
				Id: "r2", Agent: "reviewer", Model: "claude-sonnet-5-5", Station: "check", Status: gen.RunStatusSucceeded,
				StartedAt: started, EndedAt: &ended, Turns: 14, CostUsd: 0.42,
				Factory: gen.FactoryHit{Id: "f2", Name: "operations", Title: "Operations"},
				Item:    gen.RunItem{Id: "i14", Ref: "operations#14", Title: "Rotate the\nbackup keys of every region before the end of the quarter"},
			},
			{
				Id: "r1", Agent: "coder", Model: "claude-opus-5-5", Station: "code", Status: gen.RunStatusFailed,
				StartedAt: started.Add(-2 * time.Hour), EndedAt: &failedEnd, Turns: 40, CostUsd: 2.5,
				Factory: gen.FactoryHit{Id: "f1", Name: searchFactory, Title: "Web"},
				Item:    gen.RunItem{Id: "i2", Ref: "web#2", Title: "Fix the login page"},
			},
		},
		Summary: gen.RunSummary{
			Total: 7, Running: 1, Finished: 5, Failed: 1, SpendUsd: 3.2,
			SpendByFactory: []gen.FactorySpend{},
		},
		NextCursor: &cursor,
	}

	srv, _ := runsGoald(t, http.StatusOK, "application/json", runsJSON(t, list))

	out, err := goalRuns(t, srv.URL)
	if err != nil {
		t.Fatalf("goal runs = %v", err)
	}

	checkGolden(t, "runs-results", out)

	// None: a line saying so, then the summary, and no next command on the last page.
	srv, _ = runsGoald(t, http.StatusOK, "application/json", runsJSON(t, emptyRuns()))

	want := "No runs\n0 runs: 0 running, 0 finished, 0 failed, $0.00 spent\n"
	if out, err := goalRuns(t, srv.URL); err != nil || out != want {
		t.Errorf("goal runs on a quiet day = %q, %v; want %q", out, err, want)
	}
}

// --json writes goald's body as it came, spacing and key order included.
func TestRuns_JSONAsSent(t *testing.T) {
	t.Parallel()

	body := []byte(`{"runs": [], "next_cursor":null,
 "summary":{"total":0,"running":0,"finished":0,"failed":0,"spend_usd":0,"spend_by_factory":[]}}`)
	srv, _ := runsGoald(t, http.StatusOK, "application/json", body)

	if out, err := goalRuns(t, srv.URL, "--json"); err != nil || out != string(body) {
		t.Errorf("goal runs --json = %q, %v; want %q", out, err, body)
	}
}

// An unknown --factory is goald's 404, reported with its text and exit 1.
func TestRuns_Problem(t *testing.T) {
	t.Parallel()

	b, err := json.Marshal(gen.Problem{Title: "Not found", Status: http.StatusNotFound, Code: new("not-found")})
	if err != nil {
		t.Fatal(err)
	}

	srv, _ := runsGoald(t, http.StatusNotFound, "application/problem+json", b)

	if _, err := goalRuns(t, srv.URL, "--factory", "nope"); err == nil || err.Error() != "Not found" {
		t.Errorf("goal runs --factory nope = %v; want the problem's title", err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(t.Context(), []string{"runs", "--factory", "nope"}, bytes.NewReader(nil), &stdout, &stderr,
		testEnv(t, srv.URL, map[string]string{envOrg: orgAcme}), fixedBuildInfo); code != 1 {
		t.Errorf("Run exit = %d; want 1", code)
	}
}
