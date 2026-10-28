package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// agJonah is the agent name the Jonah runs of the tests record.
const agJonah = "jonah"

// jonahRequest is a Jonah review's run of the test factory: no item, station, lease, repository
// or connection but the model's (ADR 0034 decision 1).
func jonahRequest() domain.RunRequest {
	return domain.RunRequest{
		OrgID: testOrg, FactoryID: factoryID, Version: v1, Kind: domain.RunKindJonah, Agent: agJonah,
		Model: "claude-opus-5-5", Brief: "Review the sent-back runs.", ModelConnection: connModel,
	}
}

// A Jonah run records a run of kind jonah without an item, prepares its sandbox without a
// repository's git login, gives its agent only the model connection's env (no connection or
// credential of the factory's agents, even one listed for its agent name), writes no item event,
// returns the run's result and deletes its sandbox.
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

	box := prepares[0].Sandbox
	if box.ItemID != "" || box.Station != "" || box.Agent != agJonah || box.FactoryID != factoryID || box.OrgID != testOrg {
		t.Errorf("sandbox = %+v; want the factory's, for agent %s, without an item or a station", box, agJonah)
	}

	// The repository connection's git password is the only variable prepare ever gets from a
	// connection; no repository, no clone.
	for name, v := range prepares[0].Env {
		if name == envCodeToken || strings.Contains(v, codeToken) {
			t.Errorf("prepare got %s, the repository's git login; a Jonah run clones no repository", name)
		}
	}

	agent := runs[0].Env
	if agent[envModelKey] != modelKey {
		t.Errorf("the agent's %s = %q; want the model connection's key", envModelKey, agent[envModelKey])
	}

	for name, v := range agent {
		for _, secret := range []string{codeToken, trackerToken, otherToken, cloudKey, deployKeyValue} {
			if strings.Contains(v, secret) {
				t.Errorf("the agent's %s holds a secret of a connection or credential other than the model's", name)
			}
		}
	}

	if got := agent["GOAL_FEEDBACK"]; got != jonahRequest().Brief {
		t.Errorf("the agent's GOAL_FEEDBACK = %q; want the request's brief and no workspace note", got)
	}

	run, err := h.store.Run(inOrg(t), box.RunID)
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

	if events := h.store.Events(""); len(events) != 0 {
		t.Errorf("events without an item = %+v; want none", events)
	}

	h.expectSandboxes(t, 0)
}

// RunTask refuses a Jonah request that names an item, a station, a lease, a repository or a
// connection besides the model's, before it records a run or opens a sandbox.
func TestRunTask_RefusesWhatAJonahRunMayNotHave(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*domain.RunRequest){
		"an item":      func(r *domain.RunRequest) { r.Item = "00000000-0000-7000-8000-000000000001" },
		"a station":    func(r *domain.RunRequest) { r.Station = stWork },
		"a lease":      func(r *domain.RunRequest) { r.Lease = &domain.Lease{} },
		"a repository": func(r *domain.RunRequest) { r.Repository = domain.Repository{URL: repoURL, Connection: connGit} },
		"a connection": func(r *domain.RunRequest) { r.Connections = []string{connTracker} },
		"no model":     func(r *domain.RunRequest) { r.ModelConnection = "" },
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
