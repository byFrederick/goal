package engine_test

import (
	"context"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const repoURL = "https://" + gitHost + "/acme/app.git"

// repoLine is a one-station line whose runs clone repoURL with the repository token.
func repoLine() domain.Line {
	line := lineOf(agentStation(stWork, agWorker, 1, 1))
	line.Runtime.Repository = domain.Repository{URL: repoURL, AuthRef: "env:GIT_TOKEN"}

	return line
}

// expectSandboxes fails the test unless n sandboxes stand.
func (h *harness) expectSandboxes(t *testing.T, n int) {
	t.Helper()

	if boxes, err := h.sandboxes.List(t.Context()); err != nil || len(boxes) != n {
		t.Errorf("sandboxes %v (%v); want %d", boxes, err, n)
	}
}

// noSandboxes fails the test when a sandbox is left.
func (h *harness) noSandboxes(t *testing.T) {
	t.Helper()

	h.expectSandboxes(t, 0)
}

// claims verifies a task token with the tests' gateway key.
func (h *harness) claims(t *testing.T, token string) domain.TaskClaims {
	t.Helper()

	c, err := domain.VerifyTaskToken([]byte("k3y"), token, h.clock.Now())
	if err != nil {
		t.Fatalf("task token does not verify with the gateway key: %v", err)
	}

	return c
}

// A claim's run gets one sandbox of the agent image and the line's size, labelled with the run,
// that lives as long as its two attempts can, and holds no credential.
func TestRun_CreatesTheRunsSandbox(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Runtime.Size = domain.SandboxLarge
	h := newHarness(t, line, porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs, want 1", len(runs))
	}

	got := runs[0].Sandbox
	want := ports.SandboxSpec{
		OrgID: testOrg, FactoryID: factoryID, ItemID: it.ID, RunID: h.runs(t, testOrg, it.ID)[0].ID, Station: stWork, Agent: agWorker,
		Image: agentImage, Size: domain.SandboxLarge, Env: map[string]string{"GOAL_GATEWAY_URL": gatewayURL},
		Deadline: t0.Add(2*2*time.Hour + ttl), // two attempts of the default task timeout, and a lease TTL
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("sandbox =\n%+v\nwant\n%+v", got, want)
	}

	if kinds := h.kinds(it.ID); !isSubsequence(kinds, domain.EventRunStarted, domain.EventSandboxStarted, domain.EventPrepared,
		domain.EventRunFinished, domain.EventDone) {
		t.Errorf("events %v, want run_started, sandbox_started, prepared, run_finished, then done", kinds)
	}

	h.noSandboxes(t)
}

// isSubsequence reports whether want appear in s in order.
func isSubsequence(s []domain.EventKind, want ...domain.EventKind) bool {
	for _, k := range s {
		if len(want) > 0 && k == want[0] {
			want = want[1:]
		}
	}

	return len(want) == 0
}

// Each exec gets what it needs and nothing more: prepare the run's token and the repository
// token, the agent the run's token and the model's access, never the repository token.
func TestRun_EachExecGetsItsOwnEnvironment(t *testing.T) {
	t.Parallel()

	h := newHarness(t, repoLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	prepares, runs := h.worker.Prepares(), h.worker.Runs()
	if len(prepares) != 1 || len(runs) != 1 {
		t.Fatalf("%d prepares and %d agent runs, want one of each", len(prepares), len(runs))
	}

	run := h.runs(t, testOrg, it.ID)[0].ID

	prepare := prepares[0]
	if !slices.Equal(prepare.Argv, []string{"goal-agent", "prepare"}) || prepare.Env["GOAL_REPOSITORY_TOKEN"] != gitToken ||
		prepare.Env["GOAL_GATEWAY_URL"] != gatewayURL || h.claims(t, prepare.Env["GOAL_GATEWAY_TOKEN"]).RunID != run {
		t.Errorf("prepare = %q with env %v; want goal-agent prepare with the repository token and the run's task token", prepare.Argv, keys(prepare.Env))
	}

	if _, ok := prepare.Env["MODEL_ACCESS"]; ok {
		t.Error("prepare got the model's access")
	}

	agent := runs[0]
	if !slices.Equal(agent.Argv, []string{"goal-agent", "run"}) || agent.Env["MODEL_ACCESS"] != modelToken ||
		h.claims(t, agent.Env["GOAL_GATEWAY_TOKEN"]).RunID != run {
		t.Errorf("agent = %q with env %v; want goal-agent run with the model's access and the run's task token", agent.Argv, keys(agent.Env))
	}

	if _, ok := agent.Env["GOAL_REPOSITORY_TOKEN"]; ok {
		t.Error("the agent got the repository token")
	}
}

func keys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }

// The agent hears what its workspace holds: a clone of the repository on the item's branch, new
// or with the work of earlier runs, as prepare found it.
func TestRun_AgentHearsWhatItsWorkspaceHolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		existed bool
		note    string
	}{
		{name: "new branch", note: "a new branch from the default branch"},
		{name: "branch of an earlier run", existed: true, note: "holds the work of earlier runs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, repoLine(), porttest.Succeed(`{}`))
			if tt.existed {
				h.worker.BranchExisted()
			}

			h.submit(t, domain.NewItem{Title: "Add a version command"})

			h.step(t)

			feedback := h.worker.Runs()[0].Env["GOAL_FEEDBACK"]
			for _, s := range []string{repoURL, "`goal/1-add-a-version-command`", tt.note} {
				if !strings.Contains(feedback, s) {
					t.Errorf("feedback %q does not mention %q", feedback, s)
				}
			}

			if strings.Contains(feedback, gitToken) || strings.Contains(feedback, "GIT_TOKEN") {
				t.Errorf("feedback %q names the repository credential", feedback)
			}
		})
	}
}

// Without a repository, notes or a rejection goald adds nothing to the brief.
func TestRun_NoFeedbackWhenThereIsNothingToAdd(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
	h.submit(t, domain.NewItem{})

	h.step(t)

	run := h.worker.Runs()[0]
	if feedback, ok := run.Env["GOAL_FEEDBACK"]; ok {
		t.Errorf("GOAL_FEEDBACK = %q, want it unset", feedback)
	}

	if _, ok := h.worker.Prepares()[0].Env["GOAL_REPOSITORY_TOKEN"]; ok {
		t.Error("prepare got a repository token for a factory without a repository")
	}
}

// A sandbox that cannot be prepared fails its run and blocks the item; no agent runs, and the
// sandbox goes.
func TestRun_PrepareFailureFailsTheRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t, repoLine(), porttest.Succeed(`{}`))
	h.worker.FailPrepare(1, "goal-agent prepare: git clone exited 128 with "+gitToken)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateBlocked {
		t.Fatalf("item is %s, want blocked (events %v)", got.State, h.kinds(it.ID))
	}

	reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string)
	if !strings.Contains(reason, "prepare") || !strings.Contains(reason, "git clone exited 128") || strings.Contains(reason, gitToken) {
		t.Errorf("blocked reason %q, want prepare's failure without the token", reason)
	}

	if runs := h.runs(t, testOrg, it.ID); len(runs) != 1 || runs[0].Result.Status != domain.RunFailed {
		t.Errorf("runs %+v, want one failed", runs)
	}

	if n := len(h.worker.Runs()); n != 0 {
		t.Errorf("%d agent runs in a sandbox that was not prepared, want 0", n)
	}

	h.noSandboxes(t)
}

// The repository token reaches only the install's repository hosts and never names a reserved
// variable, even for a version published before the rule: the run fails before any sandbox.
func TestRun_RepositoryTokenOnlyForTheInstallsHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo domain.Repository
		want string
	}{
		{name: "another host", repo: domain.Repository{URL: "https://code.example.com/acme/app.git", AuthRef: "env:GIT_TOKEN"}, want: "code.example.com"},
		{name: "reserved variable", repo: domain.Repository{URL: repoURL, AuthRef: "env:GOAL_GATEWAY_KEY"}, want: "env-reserved"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := repoLine()
			line.Runtime.Repository = tt.repo
			h := newHarness(t, line, porttest.Succeed(`{}`))
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != domain.StateBlocked {
				t.Fatalf("item is %s, want blocked", got.State)
			}

			if reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string); !strings.Contains(reason, tt.want) {
				t.Errorf("blocked reason %q does not mention %q", reason, tt.want)
			}

			if n := len(h.worker.Prepares()); n != 0 {
				t.Errorf("%d prepares, want none", n)
			}
		})
	}
}

// The run's sandbox is deleted however the claim ends, and a retry runs in the same sandbox,
// prepared once.
func TestRun_SandboxIsDeletedOnEveryPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		agent  porttest.AgentFunc
		state  domain.ItemState
		agents int
	}{
		{name: "done", agent: porttest.Succeed(`{"answer":1}`), state: domain.StateDone, agents: 1},
		{name: "run failed", agent: porttest.Sequence(domain.RunResult{Status: domain.RunFailed, Error: "boom"}), state: domain.StateBlocked, agents: 1},
		{name: "worker crashed", agent: porttest.Sequence(domain.RunResult{}), state: domain.StateBlocked, agents: 1},
		{name: "retried in the same sandbox", agent: porttest.Sequence(ok(`{}`), ok(`{"answer":1}`)), state: domain.StateDone, agents: 2},
		{name: "contract broken twice", agent: porttest.Succeed(`{}`), state: domain.StateBlocked, agents: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			work := agentStation(stWork, agWorker, 1, 1)
			work.Output = contractResult
			h := newHarness(t, lineOf(work), tt.agent)
			h.contracts.Require(v1, contractResult, "answer")
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != tt.state {
				t.Fatalf("item is %s, want %s (events %v)", got.State, tt.state, h.kinds(it.ID))
			}

			runs := h.worker.Runs()
			if len(runs) != tt.agents || len(h.worker.Prepares()) != 1 {
				t.Fatalf("%d agent runs, %d prepares; want %d and one", len(runs), len(h.worker.Prepares()), tt.agents)
			}

			if box := runs[0].Sandbox.RunID; runs[len(runs)-1].Sandbox.RunID != box || box != h.runs(t, testOrg, it.ID)[0].ID {
				t.Errorf("agent runs in sandboxes %s and %s; want both in the first run's", box, runs[len(runs)-1].Sandbox.RunID)
			}

			// Each attempt is a run of its own, with its own task token.
			stored := h.runs(t, testOrg, it.ID)
			for i, r := range runs {
				if got := h.claims(t, r.Env["GOAL_GATEWAY_TOKEN"]).RunID; got != stored[i].ID {
					t.Errorf("attempt %d's token names run %s, want %s", i+1, got, stored[i].ID)
				}
			}

			h.noSandboxes(t)
		})
	}
}

// Shutdown and a lost lease end the claim too: the sandbox goes with it.
func TestRun_SandboxIsDeletedWhenTheClaimIsCut(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Block(nil, `{}`))
	h.submit(t, domain.NewItem{})

	ctx, cancel := context.WithCancel(inOrg(t))
	if err := h.e.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	cancel()
	h.wait(t, h.e)

	h.noSandboxes(t)
}

// What the agent reports is relayed as item events of its run, with the run's secrets scrubbed
// from events and errors; lines outside the protocol are skipped.
func TestRun_RelaysAgentEventsScrubbed(t *testing.T) {
	t.Parallel()

	h := newHarness(t, repoLine(), func(_ context.Context, run porttest.ExecCall, stdout io.Writer) domain.RunResult {
		_, _ = io.WriteString(stdout, "go: downloading example.com/mod v1.0.0\n")
		_, _ = io.WriteString(stdout, `{"type":"message","text":"`+strings.Repeat("x", 17<<20)+`"}`+"\n") // over the line limit: dropped
		_, _ = io.WriteString(stdout, `{"type":"tool_use","at":"2026-10-23T09:00:01Z","id":"t1","tool":"Bash","input":"echo `+
			run.Env["MODEL_ACCESS"]+`"}`+"\n")
		_, _ = io.WriteString(stdout, `{"type":"finished","status":"failed","permission_denials":[{"tool":"Bash","id":"t2"}]}`+"\n")

		return domain.RunResult{Status: domain.RunFailed, Error: "gave up with " + run.Env["GOAL_GATEWAY_TOKEN"]}
	})
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	calls := h.eventsOf(it.ID, domain.EventToolCall)
	if len(calls) != 1 || calls[0].RunID != h.runs(t, testOrg, it.ID)[0].ID || calls[0].Actor != agWorker {
		t.Fatalf("tool_call events %+v, want one of the run, by its agent", calls)
	}

	if input, _ := calls[0].Data["input"].(string); strings.Contains(input, modelToken) || !strings.Contains(input, "[redacted]") {
		t.Errorf("tool_call input %q, want the model's access redacted", input)
	}

	if n := len(h.eventsOf(it.ID, domain.EventAgentMessage)); n != 0 {
		t.Errorf("%d agent_message events, want the overlong line dropped", n)
	}

	if denied := h.eventsOf(it.ID, domain.EventToolDenied); len(denied) != 1 || denied[0].Data["id"] != "t2" {
		t.Errorf("tool_denied events %+v, want the refused call t2", denied)
	}

	token := h.worker.Runs()[0].Env["GOAL_GATEWAY_TOKEN"]
	if run := h.runs(t, testOrg, it.ID)[0]; strings.Contains(run.Result.Error, token) || !strings.Contains(run.Result.Error, "[redacted]") {
		t.Errorf("run error %q, want the task token redacted", run.Result.Error)
	}
}
