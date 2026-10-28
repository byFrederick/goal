package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

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
// credential of the factory's agents, even one listed for its agent name), signs a jonah token
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
		Agents: map[domain.FactoryID][]string{factoryID: {agJonah, agWorker}},
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

	expectNoClone(t, prepares[0])
	expectOnlyModelEnv(t, runs[0].Env)
	h.expectJonahRun(t, prepares[0].Sandbox.RunID)

	expectJonahToken(t, h.claims(t, runs[0].Env["GOAL_GATEWAY_TOKEN"]))

	if events := h.store.Events(""); len(events) != 0 {
		t.Errorf("events without an item = %+v; want none", events)
	}

	h.expectSandboxes(t, 0)
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

	if usd, err := h.store.JonahSpendSince(inOrg(t), factoryID, t0); err != nil || usd != 0.25 {
		t.Errorf("JonahSpendSince = %v, %v; want 0.25: the run is of kind jonah", usd, err)
	}
}
