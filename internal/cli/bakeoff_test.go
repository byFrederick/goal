package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

const (
	webBakeOffs    = "/v1/orgs/acme/factories/web/evals/bake-offs"
	caseReviewStep = "review-step"
)

// goalBakeOff runs goal bake-off args against api, polling a --follow every millisecond, and
// returns what it printed and its exit code as the golden files hold them.
func goalBakeOff(t *testing.T, api string, args ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer

	out := &printer{w: &stdout, e: &stderr}
	getenv := testEnv(t, api, map[string]string{envOrg: orgAcme})
	code := 0

	if err := errors.Join(bakeOffGroup(time.Millisecond)(t.Context(), out, getenv, args), out.err); err != nil {
		code = 1
		_, _ = fmt.Fprintln(&stderr, "goal:", err)
	}

	return fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", code, stdout.String(), stderr.String())
}

// bakeOffOf is a bake-off of frontend-engineer on v3 with three arms, the first the current one.
// Done, it has summaries and the tries passed per case; arm 3 is recommended.
func bakeOffOf(state gen.BakeOffState) gen.BakeOff {
	b := gen.BakeOff{
		Id: "bo1", State: state, Agent: "frontend-engineer", Station: "ship", Version: 3, Trials: 3, Cases: 2,
		EstimateUsd: 4.5, CostUsd: 1.1, CreatedAt: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC),
		Arms: []gen.BakeOffArm{
			{Arm: 1, Model: "claude-opus-5-5", IsCurrent: true},
			{Arm: 2, Model: "claude-sonnet-5-5"},
			{Arm: 3, Model: cliHaiku},
		},
		PerCase: []gen.BakeOffCase{
			{Case: "lease-fix", Arms: []gen.BakeOffCaseArm{{Arm: 1, Passed: 2}, {Arm: 2, Passed: 1}, {Arm: 3, Passed: 0}}},
			{Case: caseReviewStep, Arms: []gen.BakeOffCaseArm{{Arm: 1, Passed: 1}, {Arm: 2, Passed: 0}, {Arm: 3, Passed: 1}}},
		},
	}

	if state != gen.BakeOffStateDone {
		return b
	}

	title := "plain language"
	b.CostUsd = 3.9
	b.PerCase[0].Arms = []gen.BakeOffCaseArm{{Arm: 1, Passed: 3}, {Arm: 2, Passed: 2}, {Arm: 3, Passed: 3}}
	b.PerCase[1].Arms = []gen.BakeOffCaseArm{{Arm: 1, Passed: 2}, {Arm: 2, Passed: 1}, {Arm: 3, Passed: 2}}
	b.Arms[0].Summary = &gen.BakeOffArmSummary{
		Tries: 6, Passed: 5, PassRate: 5.0 / 6, CostPerItemUsd: 1.2, TimePerItemSeconds: 250.4, Turns: 31.25,
		Judged: &gen.BakeOffJudged{Judge: "plain-language", Title: &title, Share: 0.8},
	}
	b.Arms[1].Summary = &gen.BakeOffArmSummary{
		Tries: 6, Passed: 3, Errors: 1, PassRate: 0.5, CostPerItemUsd: 0.4, TimePerItemSeconds: 140, Turns: 25,
	}
	b.Arms[2].Summary = &gen.BakeOffArmSummary{
		Tries: 6, Passed: 5, PassRate: 5.0 / 6, CostPerItemUsd: 0.12, TimePerItemSeconds: 95.6, Turns: 28,
		Judged: &gen.BakeOffJudged{Judge: "plain-language", Title: &title, Share: 0.75},
	}
	three := 3
	b.RecommendedArm = &three

	return b
}

func bakeOffStarted(state gen.BakeOffState) gen.BakeOffStarted {
	return gen.BakeOffStarted{BakeOff: bakeOffOf(state), Estimate: gen.EvalEstimate{Usd: 4.5, Unpriced: []string{caseReviewStep}}}
}

func TestBakeOff_RunPrintsEstimateAndArms(t *testing.T) {
	t.Parallel()

	srv, sent := evalGoald(t, map[string]evalAnswer{
		listFactoriesRoute:    {http.StatusOK, gen.FactoryList{Factories: []gen.Factory{{Id: "f1", Name: searchFactory}}}},
		"POST " + webBakeOffs: {http.StatusAccepted, bakeOffStarted(gen.BakeOffStateQueued)},
	})

	checkGolden(t, "bake-off-run", goalBakeOff(t, srv.URL, subRun, "frontend-engineer"))

	goalBakeOff(t, srv.URL, subRun, "frontend-engineer", "--factory", searchFactory,
		"--model", "claude-opus-5-5", "--model", "claude-haiku-5-5,claude-sonnet-5-5", "--trials", "2")

	want := []string{
		listFactoriesRoute,
		"POST " + webBakeOffs + ` {"agent":"frontend-engineer"}`,
		"POST " + webBakeOffs + ` {"agent":"frontend-engineer","models":["claude-opus-5-5","claude-haiku-5-5","claude-sonnet-5-5"],"trials":2}`,
	}
	if s := sent(); strings.Join(s, "\n") != strings.Join(want, "\n") {
		t.Errorf("goald was sent\n%s\nwant\n%s", strings.Join(s, "\n"), strings.Join(want, "\n"))
	}
}

func TestBakeOff_RunFollowPrintsTheResult(t *testing.T) {
	t.Parallel()

	// The bake-off runs for two reads, then is done.
	var reads atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST "+webBakeOffs, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusAccepted, bakeOffStarted(gen.BakeOffStateQueued))
	})
	mux.HandleFunc("GET "+webBakeOffs+"/bo1", func(w http.ResponseWriter, _ *http.Request) {
		state := gen.BakeOffStateRunning
		if reads.Add(1) > 2 {
			state = gen.BakeOffStateDone
		}

		writeJSON(t, w, http.StatusOK, bakeOffOf(state))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	checkGolden(t, "bake-off-run-follow", goalBakeOff(t, srv.URL, subRun, "frontend-engineer", "--factory", searchFactory, "--follow"))

	if n := reads.Load(); n != 3 {
		t.Errorf("--follow read the bake-off %d times, want 3: until it was done", n)
	}
}

func TestBakeOff_FollowStopsOnCtrlC(t *testing.T) {
	t.Parallel()

	srv, _ := evalGoald(t, map[string]evalAnswer{"GET " + webBakeOffs + "/bo1": {http.StatusOK, bakeOffOf(gen.BakeOffStateRunning)}})

	c, err := connect(t.Context(), testEnv(t, srv.URL, map[string]string{envOrg: orgAcme}), "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	if err := followBakeOff(ctx, &printer{w: &buf}, c, searchFactory, "bo1", time.Millisecond); err == nil || !strings.Contains(err.Error(), "keeps running") {
		t.Errorf("follow cut short = %v, want that the bake-off keeps running", err)
	}
}

func TestBakeOff_ShowPrintsHeadlineTableAndCases(t *testing.T) {
	t.Parallel()

	current := bakeOffOf(gen.BakeOffStateDone)
	current.RecommendedArm = new(int)
	*current.RecommendedArm = 1

	noJudge := bakeOffOf(gen.BakeOffStateDone)
	for i := range noJudge.Arms {
		noJudge.Arms[i].Summary.Judged = nil
	}

	srv, _ := evalGoald(t, map[string]evalAnswer{
		"GET " + webBakeOffs + "/bo1": {http.StatusOK, bakeOffOf(gen.BakeOffStateDone)},
		"GET " + webBakeOffs + "/bo2": {http.StatusOK, bakeOffOf(gen.BakeOffStateRunning)},
		"GET " + webBakeOffs + "/bo3": {http.StatusOK, current},
		"GET " + webBakeOffs + "/bo4": {http.StatusOK, noJudge},
	})

	checkGolden(t, "bake-off-show", goalBakeOff(t, srv.URL, subShow, "bo1", "--factory", searchFactory))
	checkGolden(t, "bake-off-show-running", goalBakeOff(t, srv.URL, subShow, "bo2", "--factory", searchFactory))

	if got := goalBakeOff(t, srv.URL, subShow, "bo3", "--factory", searchFactory); !strings.Contains(got, "claude-opus-5-5 (Now) stays recommended") {
		t.Errorf("show of a bake-off the current model won printed\n%s\nwant that it stays recommended", got)
	}

	if got := goalBakeOff(t, srv.URL, subShow, "bo4", "--factory", searchFactory); strings.Contains(got, "JUDGED") {
		t.Errorf("show of a bake-off no judge scores printed\n%s\nwant no JUDGED column", got)
	}
}

func TestBakeOff_ApplyWritesTheArmToTheDraft(t *testing.T) {
	t.Parallel()

	running := bakeOffOf(gen.BakeOffStateRunning)
	running.Id = "bo2"

	srv, sent := evalGoald(t, map[string]evalAnswer{
		"GET " + webBakeOffs + "/bo1":        {http.StatusOK, bakeOffOf(gen.BakeOffStateDone)},
		"GET " + webBakeOffs + "/bo2":        {http.StatusOK, running},
		"POST " + webBakeOffs + "/bo1/apply": {http.StatusOK, gen.Draft{Id: "d7", NextNumber: 4, Revision: 12}},
	})

	checkGolden(t, "bake-off-apply", goalBakeOff(t, srv.URL, "apply", "bo1", "--factory", searchFactory))

	if got := goalBakeOff(t, srv.URL, "apply", "bo1", "--factory", searchFactory, "--model", "claude-sonnet-5-5"); !strings.Contains(got, "exit 0") {
		t.Errorf("apply --model printed\n%s", got)
	}

	if got := goalBakeOff(t, srv.URL, "apply", "bo1", "--factory", searchFactory, "--model", "claude-mythical-1"); !strings.Contains(got, "has no arm with claude-mythical-1: its models are claude-opus-5-5, claude-sonnet-5-5, claude-haiku-5-5") {
		t.Errorf("apply of a model the bake-off lacks printed\n%s", got)
	}

	if got := goalBakeOff(t, srv.URL, "apply", "bo2", "--factory", searchFactory); !strings.Contains(got, "bake-off bo2 is running and recommends no model yet: pass --model") {
		t.Errorf("apply of a bake-off without a recommendation printed\n%s", got)
	}

	want := []string{
		"GET " + webBakeOffs + "/bo1",
		"POST " + webBakeOffs + `/bo1/apply {"arm":3}`,
		"GET " + webBakeOffs + "/bo1",
		"POST " + webBakeOffs + `/bo1/apply {"arm":2}`,
		"GET " + webBakeOffs + "/bo1",
		"GET " + webBakeOffs + "/bo2",
	}
	if s := sent(); strings.Join(s, "\n") != strings.Join(want, "\n") {
		t.Errorf("goald was sent\n%s\nwant\n%s", strings.Join(s, "\n"), strings.Join(want, "\n"))
	}
}

func TestBakeOff_Refusals(t *testing.T) {
	t.Parallel()

	budget, conflict, invalid := "learning-budget-exhausted", "draft-conflict", "invalid-bake-off"
	srv, _ := evalGoald(t, map[string]evalAnswer{
		"POST " + webBakeOffs: {http.StatusConflict, gen.Problem{
			Title: http.StatusText(http.StatusConflict), Status: http.StatusConflict, Code: &budget, Detail: optional("the bake-off's estimate exceeds what is left of the learning budget"),
			Estimate: &gen.EvalEstimate{Usd: 4.5, Unpriced: []string{caseReviewStep}},
		}},
		"GET " + webBakeOffs + "/bo1": {http.StatusOK, bakeOffOf(gen.BakeOffStateDone)},
		"POST " + webBakeOffs + "/bo1/apply": {http.StatusPreconditionFailed, gen.Problem{
			Title: "Precondition Failed", Status: http.StatusPreconditionFailed, Code: &conflict, Detail: optional("agents/frontend-engineer.yaml changed since v3"),
		}},
		"GET " + webBakeOffs + "/bo9": {http.StatusUnprocessableEntity, gen.Problem{
			Title: "Unprocessable Entity", Status: http.StatusUnprocessableEntity, Code: &invalid, Detail: optional("not a bake-off"),
		}},
	})

	checkGolden(t, "bake-off-budget-exhausted", goalBakeOff(t, srv.URL, subRun, "frontend-engineer", "--factory", searchFactory))
	checkGolden(t, "bake-off-draft-conflict", goalBakeOff(t, srv.URL, "apply", "bo1", "--factory", searchFactory))

	if got := goalBakeOff(t, srv.URL, subShow, "bo9", "--factory", searchFactory); !strings.Contains(got, "goal: Unprocessable Entity: not a bake-off") {
		t.Errorf("another refusal printed\n%s\nwant goald's problem", got)
	}

	for _, args := range [][]string{{}, {cmdUnknownName}, {subRun}, {subRun, "a", "b"}, {subRun, "a", "--trials", "x"}, {subShow}, {subShow, "a", "b"}, {"apply"}} {
		var buf bytes.Buffer
		if err := run(t.Context(), append([]string{"bake-off"}, args...), &printer{w: &buf}, testEnv(t, srv.URL, nil), nil); !errors.Is(err, errUsage) {
			t.Errorf("goal bake-off %q = %v, want the usage", args, err)
		}
	}
}

// writeJSON writes v as JSON with status.
func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}
