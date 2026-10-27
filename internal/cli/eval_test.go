package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// evalGoald serves routes, answering each with status and v as JSON, and records the requests it
// was sent with their bodies.
func evalGoald(t *testing.T, routes map[string]evalAnswer) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu   sync.Mutex
		sent []string
	)

	mux := http.NewServeMux()

	for pattern, a := range routes {
		b, err := json.Marshal(a.body)
		if err != nil {
			t.Fatal(err)
		}

		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)

			mu.Lock()

			sent = append(sent, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+string(body)))
			mu.Unlock()

			ct := "application/json"
			if a.status >= 400 {
				ct = "application/problem+json"
			}

			w.Header().Set("Content-Type", ct)
			w.WriteHeader(a.status)
			_, _ = w.Write(b)
		})
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), sent...)
	}
}

// evalAnswer is what one route of evalGoald answers.
type evalAnswer struct {
	status int
	body   any
}

func goalEval(t *testing.T, api string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), append([]string{"eval"}, args...), &printer{w: &buf}, testEnv(t, api, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

const webEvalRuns = "/v1/orgs/acme/factories/web/evals/runs"

// evalRunOf is a run of v3 with results, each "case/state".
func evalRunOf(state gen.EvalRunState, results ...string) gen.EvalRun {
	three := 3
	r := gen.EvalRun{
		Id: "er1", State: state, Trigger: "manual", Version: &three, ContentSha256: strings.Repeat("c", 64), EstimateUsd: 1.2, CostUsd: 0.85,
		CreatedAt: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), Results: []gen.EvalResult{},
	}

	tries := map[string]int{}

	for _, res := range results {
		name, st, _ := strings.Cut(res, "/")
		tries[name]++
		r.Results = append(r.Results, gen.EvalResult{Case: name, Try: tries[name], State: gen.EvalResultState(st)})
	}

	return r
}

func TestEval_RunPrintsTheRunAndItsEstimate(t *testing.T) {
	t.Parallel()

	srv, sent := evalGoald(t, map[string]evalAnswer{
		"GET /v1/orgs/acme/factories": {http.StatusOK, gen.FactoryList{Factories: []gen.Factory{{Id: "f1", Name: searchFactory}}}},
		"POST " + webEvalRuns: {http.StatusAccepted, gen.EvalRunStarted{
			Run:      evalRunOf("queued", "lease-fix/pending", "lease-fix/pending", "review-step/pending"),
			Estimate: gen.EvalEstimate{Usd: 2.4, Unpriced: []string{"review-step"}},
		}},
	})

	got, err := goalEval(t, srv.URL, "run")
	if err != nil {
		t.Fatal(err)
	}

	checkGolden(t, "eval-run", got)

	if _, err := goalEval(t, srv.URL, "run", "--case", "lease-fix", "--draft", "--factory", searchFactory); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"GET /v1/orgs/acme/factories",
		"POST " + webEvalRuns + ` {"on":"current"}`,
		"POST " + webEvalRuns + ` {"case":"lease-fix","on":"draft"}`,
	}
	if s := sent(); strings.Join(s, "\n") != strings.Join(want, "\n") {
		t.Errorf("goald was sent\n%s\nwant\n%s", strings.Join(s, "\n"), strings.Join(want, "\n"))
	}
}

func TestEval_ShowPrintsEachCasesTries(t *testing.T) {
	t.Parallel()

	srv, _ := evalGoald(t, map[string]evalAnswer{
		"GET " + webEvalRuns + "/er1": {http.StatusOK, evalRunOf("running",
			"lease-fix/pass", "lease-fix/pass", "merge-step/pass", "merge-step/fail", "review-step/error", "slow-case/running")},
	})

	got, err := goalEval(t, srv.URL, "show", "er1", "--factory", searchFactory)
	if err != nil {
		t.Fatal(err)
	}

	checkGolden(t, "eval-show", got)
}

func TestEval_RefusalsAndUsage(t *testing.T) {
	t.Parallel()

	code := "eval-run-not-found"
	srv, _ := evalGoald(t, map[string]evalAnswer{
		"GET " + webEvalRuns + "/nope": {http.StatusNotFound, gen.Problem{Title: "Not Found", Status: http.StatusNotFound, Code: &code, Detail: optional(`eval run "nope": eval run not found`)}},
	})

	if _, err := goalEval(t, srv.URL, "show", "nope", "--factory", searchFactory); err == nil || !strings.Contains(err.Error(), "eval run not found") {
		t.Errorf("show of an unknown run = %v, want goald's problem", err)
	}

	for _, args := range [][]string{{}, {"frobnicate"}, {"show"}, {"show", "a", "b"}, {"run", "extra"}} {
		if _, err := goalEval(t, srv.URL, args...); !errors.Is(err, errUsage) {
			t.Errorf("goal eval %q = %v, want the usage", args, err)
		}
	}
}
