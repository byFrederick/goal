package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// envTaskToken is the env variable that carries a run's task token.
const envTaskToken = "GOAL_GATEWAY_TOKEN"

// agJonah is the agent name the Jonah runs of the tests record.
const agJonah = "jonah"

// jonahRequest is a Jonah review's run of the test factory: no item, station, lease, repository
// or connection but the model's (ADR 0034 decision 1), served the jonah bundle of the review's
// window, which ends within a second.
func jonahRequest() domain.RunRequest {
	return domain.RunRequest{
		OrgID: testOrg, FactoryID: factoryID, Version: v1, Kind: domain.RunKindJonah, Agent: agJonah,
		Model: "claude-opus-5-5", Brief: "Review the sent-back runs.", ModelConnection: connModel,
		Bundle: domain.TaskKindJonah, WindowStart: t0.Add(-24 * time.Hour), WindowEnd: t0.Add(-time.Hour + 500*time.Millisecond),
		Manual: true,
	}
}

// A Jonah run records a run of kind jonah without an item, prepares its sandbox without a
// repository's git login, gives its agent only the model connection's env (no connection or
// credential of the factory's agents), signs a jonah token
// with the review's window and no connection, writes no item event, returns the run's result and
// deletes its sandbox.
func TestRunTask_Jonah(t *testing.T) {
	t.Parallel()

	output := `{"observations":[],"proposals":[]}`
	h := newHarness(t, connLine(), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output), CostUSD: 0.25, Turns: 2}
	})
	h.store.AddFactory(testOrg, factoryID) // the factory exists before any item does
	h.addCredential(t, domain.NewCredential{
		Name: credDeploy, Scope: domain.CredentialScope{All: true}, Value: deployKeyValue,
	})

	res, err := h.e.RunTask(t.Context(), jonahRequest())
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	if res.Status != domain.RunSucceeded || string(res.Output) != output || res.CostUSD != 0.25 {
		t.Errorf("RunTask = %+v; want the agent's succeeded result with its output and cost", res)
	}

	prepares, runs := h.worker.Prepares(), h.worker.Runs()
	if len(prepares) != 1 || len(runs) != 1 {
		t.Fatalf("%d prepares and %d agent runs; want one of each", len(prepares), len(runs))
	}

	if res.RunID == "" || res.RunID != prepares[0].Sandbox.RunID {
		t.Errorf("RunTask's run = %q, want the recorded run %q", res.RunID, prepares[0].Sandbox.RunID)
	}

	expectNoClone(t, prepares[0])
	expectOnlyModelEnv(t, runs[0].Env)
	h.expectJonahRun(t, prepares[0].Sandbox.RunID)

	expectJonahToken(t, h.claims(t, runs[0].Env[envTaskToken]))

	if events := h.store.Events(""); len(events) != 0 {
		t.Errorf("events without an item = %+v; want none", events)
	}

	h.expectSandboxes(t, 0)
}

// A Jonah run pins the newest snapshot of each of the line's knowledge sources that applies to
// every run, and none scoped to an agent, a station, a lane or a work type (ADR 0039 decision 4).
func TestRunTask_JonahPinsTheFactoryWideKnowledge(t *testing.T) {
	t.Parallel()

	line := connLine()
	line.Knowledge = []domain.KnowledgeEntry{
		{Name: "runbooks", AppliesTo: domain.RuleScope{Agents: []string{agWorker}}},
		{Name: kbHandbook},
	}

	h := newHarness(t, line, func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{}`)}
	})
	h.store.AddFactory(testOrg, factoryID)

	kb := h.knowledgeBase(t)
	kb.putSnapshot(t, kbHandbook, "h1")
	kb.putSnapshot(t, "runbooks", "r1")
	h.e = h.engine(t, h.store)

	if _, err := h.e.RunTask(t.Context(), jonahRequest()); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	want := []domain.KnowledgePin{{Source: kbHandbook, Snapshot: "h1"}}
	if got := h.pinsOf(t, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("jonah run's pins = %+v, want %+v", got, want)
	}
}

// A run that fails still names its record.
func TestRunTask_FailedRunNamesItsRecord(t *testing.T) {
	t.Parallel()

	h := newHarness(t, connLine(), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunFailed, Error: "out of turns"}
	})
	h.store.AddFactory(testOrg, factoryID)

	res, err := h.e.RunTask(t.Context(), jonahRequest())
	if err != nil || res.Status != domain.RunFailed || res.RunID == "" {
		t.Errorf("RunTask = %+v, %v; want a failed result naming its run", res, err)
	}
}

// expectJonahToken checks a Jonah run's token: of kind jonah, manual, with no item, station or
// connection, and jonahRequest's window with its end rounded up to the second, so the token's
// window holds the review's.
func expectJonahToken(t *testing.T, c domain.TaskClaims) {
	t.Helper()

	if c.Kind != domain.TaskKindJonah || len(c.Connections) != 0 || c.ItemID != "" || c.Station != "" || !c.Manual ||
		c.WindowStart != t0.Add(-24*time.Hour).Unix() || c.WindowEnd != t0.Add(-time.Hour).Unix()+1 {
		t.Errorf("token claims = %+v; want a jonah token of the review's window, manual, with no item, station or connection", c)
	}
}

// RunTask refuses a Jonah request that names an item, a station, a lease, a repository or a
// connection besides the model's, or whose jonah bundle has no window that ends after it starts,
// and a window on another bundle, before it records a run or opens a sandbox.
func TestRunTask_RefusesWhatAJonahRunMayNotHave(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*domain.RunRequest){
		"an item":                         func(r *domain.RunRequest) { r.Item = "00000000-0000-7000-8000-000000000001" },
		"a station":                       func(r *domain.RunRequest) { r.Station = stWork },
		"a lease":                         func(r *domain.RunRequest) { r.Lease = &domain.Lease{} },
		"a repository":                    func(r *domain.RunRequest) { r.Repository = domain.Repository{URL: repoURL, Connection: connGit} },
		"a connection":                    func(r *domain.RunRequest) { r.Connections = []string{connTracker} },
		"no model":                        func(r *domain.RunRequest) { r.ModelConnection = "" },
		"a jonah bundle without a window": func(r *domain.RunRequest) { r.WindowStart, r.WindowEnd = time.Time{}, time.Time{} },
		"a window ending at its start":    func(r *domain.RunRequest) { r.WindowEnd = r.WindowStart },
		"a window on another bundle":      func(r *domain.RunRequest) { r.Bundle = domain.TaskKindEval },
	}

	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, connLine(), porttest.Succeed(`{}`))
			req := jonahRequest()
			change(&req)

			if _, err := h.e.RunTask(t.Context(), req); !errors.Is(err, domain.ErrBadRunRequest) {
				t.Errorf("RunTask = %v; want ErrBadRunRequest", err)
			}

			if n := len(h.worker.Prepares()); n != 0 {
				t.Errorf("%d prepares; want none", n)
			}
		})
	}
}

// expectNoClone fails the test unless prepare ran in the factory's sandbox for agJonah, without an
// item or a station, and without the repository connection's git password: the only variable
// prepare ever gets from a connection. No repository, no clone.
func expectNoClone(t *testing.T, prepare porttest.ExecCall) {
	t.Helper()

	box := prepare.Sandbox
	if box.ItemID != "" || box.Station != "" || box.Agent != agJonah || box.FactoryID != factoryID || box.OrgID != testOrg {
		t.Errorf("sandbox = %+v; want the factory's, for agent %s, without an item or a station", box, agJonah)
	}

	for name, v := range prepare.Env {
		if name == envCodeToken || strings.Contains(v, codeToken) {
			t.Errorf("prepare got %s, the repository's git login; a Jonah run clones no repository", name)
		}
	}
}

// expectOnlyModelEnv fails the test unless the agent's env has the model connection's key, no
// secret of another connection or credential, and the request's brief alone as its feedback.
func expectOnlyModelEnv(t *testing.T, env map[string]string) {
	t.Helper()

	if env[envModelKey] != modelKey {
		t.Errorf("the agent's %s = %q; want the model connection's key", envModelKey, env[envModelKey])
	}

	others := []string{codeToken, trackerToken, otherToken, cloudKey, deployKeyValue}
	for name, v := range env {
		if slices.ContainsFunc(others, func(secret string) bool { return strings.Contains(v, secret) }) {
			t.Errorf("the agent's %s holds a secret of a connection or credential other than the model's", name)
		}
	}

	if got := env["GOAL_FEEDBACK"]; got != jonahRequest().Brief {
		t.Errorf("the agent's GOAL_FEEDBACK = %q; want the request's brief and no workspace note", got)
	}
}

// expectJonahRun fails the test unless run is a finished run of kind jonah of agJonah on v1,
// without an item or a station, that cost 0.25.
func (h *harness) expectJonahRun(t *testing.T, id domain.RunID) {
	t.Helper()

	run, err := h.store.Run(inOrg(t), id)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if run.ItemID != "" || run.Station != "" || run.Agent != agJonah || run.DefinitionVersionID != v1 ||
		run.Result.Status != domain.RunSucceeded || run.Result.CostUSD != 0.25 {
		t.Errorf("run = %+v; want a finished run of agent %s on %s without an item, with its cost", run, agJonah, v1)
	}

	if s, err := h.store.JonahSpendSince(inOrg(t), factoryID, t0); err != nil || s.JonahUSD != 0.25 {
		t.Errorf("JonahSpendSince = %+v, %v; want 0.25 of Jonah's: the run is of kind jonah", s, err)
	}
}

// tryRequest is a try of a past item at stWork on the test factory: no item, lease or connection
// but the model's and the repository's (ADR 0039 decision 9), from a recorded commit.
func tryRequest() domain.RunRequest {
	return domain.RunRequest{
		OrgID: testOrg, FactoryID: factoryID, Version: v1, Kind: domain.RunKindTry, Station: stWork, Agent: agWorker,
		Model: modelOpus, ModelConnection: connModel, Bundle: domain.TaskKindTry, TryOf: "try-1",
		Commit: strings.Repeat("c", 40), Repository: domain.Repository{URL: repoURL, Connection: connGit},
	}
}

// A try records a run of kind try without an item, so the past item's cost and events stay as
// they were; its agent gets only the model connection's env; its token is of kind try, names the
// try and its station and no connection; and the result carries the box's diff.
func TestRunTask_TryRecordsNoItemCost(t *testing.T) {
	t.Parallel()

	h := newHarness(t, connLine(), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{}`), CostUSD: 0.5, Turns: 3}
	})
	h.store.AddFactory(testOrg, factoryID)
	past := h.submit(t, domain.NewItem{Title: "a past item"})
	before := len(h.store.Events(past.ID))

	const diff = "diff --git a/main.go b/main.go\n"
	h.worker.Diff(diff, 0)

	res, err := h.e.RunTask(t.Context(), tryRequest())
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	if res.Status != domain.RunSucceeded || res.Diff != diff || res.CostUSD != 0.5 || res.RunID == "" {
		t.Errorf("RunTask = %+v; want the succeeded result with the box's diff, its cost and its run", res)
	}

	if n := len(h.worker.Diffs()); n != 1 {
		t.Errorf("%d diffs read; want one, after the agent", n)
	}

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs; want one", len(runs))
	}

	expectNoSecretButModel(t, runs[0].Env)

	c := h.claims(t, runs[0].Env[envTaskToken])
	if c.Kind != domain.TaskKindTry || c.TryOf != "try-1" || c.Station != stWork || c.ItemID != "" || len(c.Connections) != 0 {
		t.Errorf("token claims = %+v; want a try token of try-1 at %s with no item or connection", c, stWork)
	}

	h.expectTryRun(t, res.RunID, past.ID, before)
}

// expectTryRun fails the test unless run is a finished try at stWork without an item, whose cost
// is Jonah's and not the line's, and past's cost and events are as they were (before events).
func (h *harness) expectTryRun(t *testing.T, id domain.RunID, past domain.ItemID, before int) {
	t.Helper()

	run, err := h.store.Run(inOrg(t), id)
	if err != nil || run.ItemID != "" || run.Station != stWork || run.Result.CostUSD != 0.5 {
		t.Errorf("run = %+v, %v; want a finished run at %s without an item, with its cost", run, err, stWork)
	}

	if s, err := h.store.JonahSpendSince(inOrg(t), factoryID, t0); err != nil || s.JonahUSD != 0.5 || s.LineUSD != 0 {
		t.Errorf("JonahSpendSince = %+v, %v; want the try's 0.5 as Jonah's and none as the line's", s, err)
	}

	if got := h.item(t, past); got.CostUSD != 0 || len(h.store.Events(past)) != before {
		t.Errorf("past item cost %v with %d events; want it unchanged", got.CostUSD, len(h.store.Events(past)))
	}

	if events := h.store.Events(""); len(events) != 0 {
		t.Errorf("events without an item = %+v; want none", events)
	}

	h.expectSandboxes(t, 0)
}

// A try whose diff cannot be read failed on its infrastructure: RunTask returns an error with the
// result, which names the run.
func TestRunTask_TryDiffFailureIsAnError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, connLine(), porttest.Succeed(`{}`))
	h.store.AddFactory(testOrg, factoryID)
	h.worker.Diff("", 128)

	res, err := h.e.RunTask(t.Context(), tryRequest())
	if err == nil || res.RunID == "" {
		t.Errorf("RunTask = %+v, %v; want an error naming the run", res, err)
	}
}

// A try that succeeded runs its station's checks in its own box, in order, in the workspace, with
// CI=true, no credential and the check timeout, after its diff was read; it stops at the first
// that fails, which comes back on the result with its exit code and the end of its output. A try
// whose agent failed runs none.
func TestRunTask_TryRunsChecksInItsBox(t *testing.T) {
	t.Parallel()

	const lint, test, build = "make lint", "make test", "make build"

	h := newHarness(t, connLine(), porttest.Succeed(`{}`))
	h.store.AddFactory(testOrg, factoryID)
	h.worker.Exit(test, 2)

	req := tryRequest()
	req.Checks = []string{lint, test, build}

	res, err := h.e.RunTask(t.Context(), req)
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	want := domain.FailedCheck{Command: test, ExitCode: 2, Output: test + " exited 2"}
	if res.Status != domain.RunSucceeded || res.FailedCheck == nil || *res.FailedCheck != want {
		t.Errorf("RunTask = %+v with failed check %+v; want succeeded with %+v", res, res.FailedCheck, want)
	}

	h.expectTryChecks(t, lint, test)

	if n := len(h.worker.Diffs()); n != 1 {
		t.Errorf("%d diffs read; want one", n)
	}

	failed := newHarness(t, connLine(), porttest.Sequence(domain.RunResult{Status: domain.RunFailed, Error: "the agent gave up"}))
	failed.store.AddFactory(testOrg, factoryID)

	if res, err := failed.e.RunTask(t.Context(), req); err != nil || res.FailedCheck != nil || len(failed.worker.Checks()) != 0 {
		t.Errorf("RunTask of a failed agent = %+v, %v with %d checks; want no check run", res, err, len(failed.worker.Checks()))
	}
}

// expectTryChecks fails the test unless the checks run are commands, in order, each in the
// workspace of the agent's box with CI=true, no credential and a timeout.
func (h *harness) expectTryChecks(t *testing.T, commands ...string) {
	t.Helper()

	checks := h.worker.Checks()
	if len(checks) != len(commands) {
		t.Fatalf("checks = %+v; want %q, and none after the failure", checks, commands)
	}

	runBox := h.worker.Runs()[0].Sandbox.RunID
	for i, c := range checks {
		if c.Argv[2] != commands[i] || c.Dir != "/workspace" || c.Env["CI"] != "true" || c.Timeout <= 0 || c.Sandbox.RunID != runBox {
			t.Errorf("check %q in %q with env %v and timeout %s; want %q in the try's box's workspace, CI=true and a timeout",
				c.Argv, c.Dir, keys(c.Env), c.Timeout, commands[i])
		}

		for _, name := range []string{envModelKey, envTaskToken, envCodeToken} {
			if _, ok := c.Env[name]; ok {
				t.Errorf("check %q got %s", c.Argv, name)
			}
		}
	}
}

// A try whose check cannot be run failed on its infrastructure, not its check: RunTask returns an
// error with the result, which names the run.
func TestRunTask_TryCheckThatCannotRunIsAnError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, connLine(), porttest.Succeed(`{}`))
	h.store.AddFactory(testOrg, factoryID)
	h.sandboxes.Lose("sh -c "+makeTest, 1, lostBox)

	req := tryRequest()
	req.Checks = []string{makeTest}

	res, err := h.e.RunTask(t.Context(), req)
	if err == nil || !errors.Is(err, ports.ErrSandboxLost) || res.RunID == "" || res.FailedCheck != nil {
		t.Errorf("RunTask = %+v, %v; want an error wrapping ports.ErrSandboxLost naming the run, and no failed check", res, err)
	}
}

// RunTask refuses a try with an item, a connection or no recorded commit before it opens a box.
func TestRunTask_TryRefused(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(*domain.RunRequest){
		"an item":      func(r *domain.RunRequest) { r.Item = "00000000-0000-7000-8000-000000000001" },
		"a connection": func(r *domain.RunRequest) { r.Connections = []string{connTracker} },
		"no commit":    func(r *domain.RunRequest) { r.Commit = "" },
	} {
		h := newHarness(t, connLine(), porttest.Succeed(`{}`))
		req := tryRequest()
		change(&req)

		if _, err := h.e.RunTask(t.Context(), req); !errors.Is(err, domain.ErrBadRunRequest) || len(h.worker.Prepares()) != 0 {
			t.Errorf("%s: RunTask = %v with %d prepares; want ErrBadRunRequest and none", name, err, len(h.worker.Prepares()))
		}
	}
}

// expectNoSecretButModel fails the test unless the agent's env has the model connection's key and
// no secret of another connection or credential.
func expectNoSecretButModel(t *testing.T, env map[string]string) {
	t.Helper()

	if env[envModelKey] != modelKey {
		t.Errorf("the agent's %s = %q; want the model connection's key", envModelKey, env[envModelKey])
	}

	for name, v := range env {
		for _, secret := range []string{codeToken, trackerToken, otherToken, cloudKey, deployKeyValue} {
			if strings.Contains(v, secret) {
				t.Errorf("the agent's %s holds a secret of a connection or credential other than the model's", name)
			}
		}
	}
}

// gradeRequest is a grade of try-1's original result at stWork on the test factory: a run of
// kind try on a grade bundle, with no item, lease, repository, commit, check or connection but the
// model's (ADR 0039 decision 10).
func gradeRequest() domain.RunRequest {
	return domain.RunRequest{
		OrgID: testOrg, FactoryID: factoryID, Version: v1, Kind: domain.RunKindTry, Station: stWork, Agent: agWorker,
		Model: modelOpus, ModelConnection: connModel, Bundle: domain.TaskKindGrade, TryOf: "try-1", GradeOf: domain.GradeOriginal,
	}
}

// A grade records a run of kind try without an item, so its cost is Jonah's; signs a grade token
// naming the try and the result it grades; gives its agent the model connection's env alone, and
// prepare no repository's git login; reads no diff and runs no check; and returns the
// worker's answer and cost.
func TestRunTask_Grade(t *testing.T) {
	t.Parallel()

	const answer = `{"reason":"It does not touch the failing handler.","verdict":"not_fixed"}`

	h := newHarness(t, connLine(), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(answer), CostUSD: 0.5, Turns: 1}
	})
	h.store.AddFactory(testOrg, factoryID)
	h.addCredential(t, domain.NewCredential{Name: credDeploy, Scope: domain.CredentialScope{All: true}, Value: deployKeyValue})

	res, err := h.e.RunTask(t.Context(), gradeRequest())
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	if res.Status != domain.RunSucceeded || string(res.Output) != answer || res.CostUSD != 0.5 || res.RunID == "" || res.Diff != "" || res.FailedCheck != nil {
		t.Errorf("RunTask = %+v; want the worker's answer and cost, naming its run, with no diff or failed check", res)
	}

	if n := len(h.worker.Diffs()); n != 0 {
		t.Errorf("%d diffs read; want none for a grade", n)
	}

	prepares, runs := h.worker.Prepares(), h.worker.Runs()
	if len(prepares) != 1 || len(runs) != 1 {
		t.Fatalf("%d prepares and %d agent runs; want one of each", len(prepares), len(runs))
	}

	for name, v := range prepares[0].Env {
		if name == envCodeToken || strings.Contains(v, codeToken) {
			t.Errorf("prepare got %s, the repository's git login; a grade clones no repository", name)
		}
	}

	expectNoSecretButModel(t, runs[0].Env)

	c := h.claims(t, runs[0].Env[envTaskToken])
	if c.Kind != domain.TaskKindGrade || c.TryOf != "try-1" || c.GradeOf != domain.GradeOriginal || c.ItemID != "" || len(c.Connections) != 0 || c.Model != modelOpus {
		t.Errorf("token claims = %+v; want a grade token of try-1's original result with the model and no item or connection", c)
	}

	run, err := h.store.Run(inOrg(t), res.RunID)
	if err != nil || run.ItemID != "" || run.Result.CostUSD != 0.5 {
		t.Errorf("run = %+v, %v; want a finished run without an item, with its cost", run, err)
	}

	if s, err := h.store.JonahSpendSince(inOrg(t), factoryID, t0); err != nil || s.JonahUSD != 0.5 || s.LineUSD != 0 {
		t.Errorf("JonahSpendSince = %+v, %v; want the grade's 0.5 as Jonah's and none as the line's", s, err)
	}

	if events := h.store.Events(""); len(events) != 0 {
		t.Errorf("events without an item = %+v; want none", events)
	}

	h.expectSandboxes(t, 0)
}

// A grade of a version on secrets gets the model key alone: no repository token, environment or
// connection.
func TestRunTask_GradeOnSecrets(t *testing.T) {
	t.Parallel()

	h := newHarness(t, secretsLine(), porttest.Succeed(`{"reason":"r","verdict":"fixed"}`))
	h.store.AddFactory(testOrg, factoryID)
	h.addSecrets(t)

	req := gradeRequest()
	req.ModelConnection, req.ModelKey = "", "{{"+secClaude+"}}"

	if _, err := h.e.RunTask(t.Context(), req); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs; want 1", len(runs))
	}

	keys := slices.Sorted(maps.Keys(runs[0].Env))
	if want := []string{domain.EnvAnthropicAPIKey, "GOAL_GATEWAY_TOKEN", envGatewayURL}; !slices.Equal(keys, want) {
		t.Errorf("the agent's env = %v; want %v", keys, want)
	}

	if got, want := h.secretsUsed(t, agWorker), []string{secClaude}; !slices.Equal(got, want) {
		t.Errorf("secret.used of %v; want %v", got, want)
	}

	h.noConnectionUsed(t)
}
