package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// judgeGoald serves acme's judges routes and its factories with routes, recording the paths it
// was sent.
func judgeGoald(t *testing.T, routes map[string]func(w http.ResponseWriter)) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu   sync.Mutex
		sent []string
	)

	mux := http.NewServeMux()

	for pattern, answer := range routes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()

			sent = append(sent, r.Method+" "+r.URL.Path)
			mu.Unlock()

			answer(w)
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

// answerJSON answers status with v as JSON.
func answerJSON(t *testing.T, status int, contentType string, v any) func(w http.ResponseWriter) {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write(b)
	}
}

func goalJudge(t *testing.T, api string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), append([]string{"judge"}, args...), &printer{w: &buf}, testEnv(t, api, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

// acmeFactories is an organization with the one factory web.
var acmeFactories = gen.FactoryList{Factories: []gen.Factory{{Id: "f1", Name: searchFactory}}}

const webJudges = "GET /v1/orgs/acme/factories/web/judges"

// A row per judge with its last 30 days, then the factory's judge cost; without --factory, the
// organization's only factory.
func TestJudgeCommand_List(t *testing.T) {
	t.Parallel()

	version := 3
	srv, sent := judgeGoald(t, map[string]func(http.ResponseWriter){
		"GET /v1/orgs/acme/factories": answerJSON(t, http.StatusOK, "application/json", acmeFactories),
		webJudges: answerJSON(t, http.StatusOK, "application/json", gen.JudgeList{
			Version: &version, JudgeCostUsd: 1.234,
			Judges: []gen.JudgeListEntry{
				{
					Slug: "spec-followed", Title: "Spec followed", Agents: []string{"backend"}, Enabled: true, Sample: 0.2, PassMark: 0.5,
					Summary: gen.JudgeSummary{Scored: 40, Passed: 34, PassRate: 0.85, CostUsd: 1.2, Days: []gen.JudgeDay{}},
				},
				{
					Slug: "tests-added", Title: "Tests added", Agents: []string{}, Enabled: false, Sample: 0, PassMark: 0.7,
					Summary: gen.JudgeSummary{Days: []gen.JudgeDay{}},
				},
			},
		}),
	})

	out, err := goalJudge(t, srv.URL, "list")
	if err != nil {
		t.Fatalf("goal judge list = %v", err)
	}

	checkGolden(t, "judge-list", out)

	if got := sent(); len(got) != 2 || got[1] != webJudges {
		t.Errorf("goald was sent %v, want the factories, then web's judges", got)
	}
}

func TestJudgeCommand_ListEmptyAndJSON(t *testing.T) {
	t.Parallel()

	body := gen.JudgeList{Judges: []gen.JudgeListEntry{}}
	srv, sent := judgeGoald(t, map[string]func(http.ResponseWriter){
		webJudges: answerJSON(t, http.StatusOK, "application/json", body),
	})

	out, err := goalJudge(t, srv.URL, "list", "--factory", searchFactory)
	if err != nil || !strings.HasPrefix(out, "web has no judges") {
		t.Errorf("goal judge list --factory web = %q, %v; want no judges", out, err)
	}

	if got := sent(); len(got) != 1 {
		t.Errorf("goald was sent %v, want only web's judges with --factory", got)
	}

	want, _ := json.Marshal(body)
	if out, err := goalJudge(t, srv.URL, "list", "--factory", searchFactory, "--json"); err != nil || out != string(want) {
		t.Errorf("goal judge list --json = %q, %v; want goald's body %s", out, err, want)
	}
}

// Without --factory, an organization of several factories is an error naming them.
func TestJudgeCommand_ListNeedsAFactory(t *testing.T) {
	t.Parallel()

	srv, _ := judgeGoald(t, map[string]func(http.ResponseWriter){
		"GET /v1/orgs/acme/factories": answerJSON(t, http.StatusOK, "application/json", gen.FactoryList{
			Factories: []gen.Factory{{Id: "f1", Name: "web"}, {Id: "f2", Name: "api"}},
		}),
	})

	if _, err := goalJudge(t, srv.URL, "list"); err == nil || !strings.Contains(err.Error(), "web, api: pass --factory") {
		t.Errorf("goal judge list = %v, want an error naming web and api", err)
	}
}

func TestJudgeCommand_Try(t *testing.T) {
	t.Parallel()

	srv, sent := judgeGoald(t, map[string]func(http.ResponseWriter){
		"POST /v1/orgs/acme/factories/web/judges/spec-followed/try": answerJSON(t, http.StatusAccepted, "application/json", gen.JudgeTries{
			Tries: []gen.JudgeTry{
				{Id: "s1", State: gen.JudgeScoreStateQueued, ItemRef: "web#14", ItemTitle: "Log in with a key"},
				{Id: "s2", State: gen.JudgeScoreStateQueued, ItemRef: "web#2", ItemTitle: "Add a sign-up form"},
			},
		}),
	})

	out, err := goalJudge(t, srv.URL, "try", "spec-followed", "--factory", searchFactory)
	if err != nil {
		t.Fatalf("goal judge try = %v", err)
	}

	checkGolden(t, "judge-try", out)

	if got := sent(); len(got) != 1 {
		t.Errorf("goald was sent %v, want one try", got)
	}
}

// goald's problem is the command's error; a refused permission exits 77.
func TestJudgeCommand_Problems(t *testing.T) {
	t.Parallel()

	tryPath := "POST /v1/orgs/acme/factories/web/judges/nobody/try"
	srv, _ := judgeGoald(t, map[string]func(http.ResponseWriter){
		tryPath: answerJSON(t, http.StatusNotFound, "application/problem+json", gen.Problem{
			Title: "Not Found", Status: http.StatusNotFound, Detail: new("judge \"nobody\": judge not found"), Code: new("judge-not-found"),
		}),
		webJudges: answerJSON(t, http.StatusForbidden, "application/problem+json", gen.Problem{
			Title: "Forbidden", Status: http.StatusForbidden, Code: new("forbidden"),
		}),
	})

	if _, err := goalJudge(t, srv.URL, "try", "nobody", "--factory", searchFactory); err == nil || !strings.Contains(err.Error(), "judge not found") {
		t.Errorf("goal judge try nobody = %v, want goald's not found", err)
	}

	_, err := goalJudge(t, srv.URL, "list", "--factory", searchFactory)

	if _, ok := errors.AsType[*accessError](err); !ok {
		t.Errorf("goal judge list refused = %v, want an access error (exit 77)", err)
	}
}

func TestJudgeCommand_Usage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{}, {"show"}, {"try"}, {"try", "a", "b"}, {"list", "more"}} {
		if _, err := goalJudge(t, "http://127.0.0.1:1", args...); !errors.Is(err, errUsage) {
			t.Errorf("goal judge %v = %v, want the usage", args, err)
		}
	}
}
