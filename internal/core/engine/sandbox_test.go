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

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const repoURL = "https://" + gitHost + "/acme/app.git"

// repoLine is a one-station line whose runs clone repoURL with the repository connection connGit.
func repoLine() domain.Line {
	line := lineOf(agentStation(stWork, agWorker, 1, 1))
	line.Runtime.Repository = domain.Repository{URL: repoURL, Connection: connGit}

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
// connection's git password, the agent the run's token and the model's access, never the
// repository's password.
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
	if !slices.Equal(prepare.Argv, []string{"goal-agent", "prepare"}) || prepare.Env[envCodeToken] != codeToken ||
		prepare.Env["GOAL_GATEWAY_URL"] != gatewayURL || h.claims(t, prepare.Env["GOAL_GATEWAY_TOKEN"]).RunID != run {
		t.Errorf("prepare = %q with env %v; want goal-agent prepare with the repository token and the run's task token", prepare.Argv, keys(prepare.Env))
	}

	if _, ok := prepare.Env[envModelKey]; ok {
		t.Error("prepare got the model's access")
	}

	agent := runs[0]
	if !slices.Equal(agent.Argv, []string{"goal-agent", "run"}) || agent.Env[envModelKey] != modelKey ||
		h.claims(t, agent.Env["GOAL_GATEWAY_TOKEN"]).RunID != run {
		t.Errorf("agent = %q with env %v; want goal-agent run with the model's access and the run's task token", agent.Argv, keys(agent.Env))
	}

	if _, ok := agent.Env[envCodeToken]; ok {
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

			if strings.Contains(feedback, codeToken) || strings.Contains(feedback, envCodeToken) {
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

	if _, ok := h.worker.Prepares()[0].Env[envCodeToken]; ok {
		t.Error("prepare got a repository token for a factory without a repository")
	}
}

// A sandbox that cannot be prepared fails its run and blocks the item; no agent runs, and the
// sandbox goes.
func TestRun_PrepareFailureFailsTheRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t, repoLine(), porttest.Succeed(`{}`))
	h.worker.FailPrepare(1, "goal-agent prepare: git clone exited 128 with "+codeToken)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateBlocked {
		t.Fatalf("item is %s, want blocked (events %v)", got.State, h.kinds(it.ID))
	}

	reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string)
	if !strings.Contains(reason, "prepare") || !strings.Contains(reason, "git clone exited 128") || strings.Contains(reason, codeToken) {
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

// Since the cutover a version runs on connections only: one that still has a form connections
// replaced, or names no model connection, blocks its items with what to do, before any run, box
// or connection is opened (spec 4 §14, step 5).
func TestRun_RefusesAVersionThatDoesNotRunOnConnections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edit func(*domain.Line)
		want string
	}{
		{
			name: "a removed form", want: "spec.runtime.repository.token (env-reference-removed)",
			edit: func(l *domain.Line) { l.Removed = []string{"spec.runtime.repository.token (env-reference-removed)"} },
		},
		{name: "no model connection", want: "runtime.modelConnection", edit: func(l *domain.Line) { l.Runtime.ModelConnection = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := repoLine()
			tt.edit(&line)
			h := newHarness(t, line, porttest.Succeed(`{}`))
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != domain.StateBlocked {
				t.Fatalf("item is %s, want blocked", got.State)
			}

			reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string)
			if !strings.Contains(reason, tt.want) || !strings.Contains(reason, "publish a version with connections (spec 4 §4.4)") {
				t.Errorf("blocked reason %q; want it to name %q and the fix", reason, tt.want)
			}

			used, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Limit: 10, Action: domain.AuditConnectionUsed})
			if runs := h.runs(t, testOrg, it.ID); len(runs) != 0 || len(h.worker.Prepares()) != 0 || err != nil || len(used) != 0 {
				t.Errorf("%d runs, %d prepares, connections used %+v (%v); want none", len(runs), len(h.worker.Prepares()), used, err)
			}

			h.noSandboxes(t)
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
			run.Env[envModelKey]+`"}`+"\n")
		_, _ = io.WriteString(stdout, `{"type":"finished","status":"failed","permission_denials":[{"tool":"Bash","id":"t2"}]}`+"\n")

		return domain.RunResult{Status: domain.RunFailed, Error: "gave up with " + run.Env["GOAL_GATEWAY_TOKEN"]}
	})
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	calls := h.eventsOf(it.ID, domain.EventToolCall)
	if len(calls) != 1 || calls[0].RunID != h.runs(t, testOrg, it.ID)[0].ID || calls[0].Actor != agWorker {
		t.Fatalf("tool_call events %+v, want one of the run, by its agent", calls)
	}

	if input, _ := calls[0].Data["input"].(string); strings.Contains(input, modelKey) || !strings.Contains(input, "[redacted]") {
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

// Commits as the worker reports them: the item branch's and the base's heads before prepare's
// rebase, and the branch's head on the remote when the agent ended.
var (
	branchHead = strings.Repeat("a", 40)
	baseHead   = strings.Repeat("b", 40)
	pushedHead = strings.Repeat("c", 40)
)

// pushed is a succeeded result with output that pushed head.
func pushed(output, head string) domain.RunResult {
	res := ok(output)
	res.PushedHead = head

	return res
}

// A claim's first run records what a try needs to rerun it (ADR 0039, decision 8): the brief the
// agent was given, the bundle's and goald's feedback as the worker joins them, the heads prepare
// found, its own output and the head it pushed.
func TestRun_RecordsTheBriefOutputAndHeads(t *testing.T) {
	t.Parallel()

	h := newHarness(t, repoLine(), porttest.Sequence(pushed(`{"lane":"backend"}`, pushedHead)))
	h.worker.Heads(branchHead, baseHead)
	it := h.submit(t, domain.NewItem{Title: "Add a version command", Body: "Print the build's version."})

	h.step(t)

	feedback := h.worker.Runs()[0].Env["GOAL_FEEDBACK"]
	if feedback == "" {
		t.Fatal("the agent heard no feedback; the brief would hold only the item's work")
	}

	runs := h.runs(t, testOrg, it.ID)
	if len(runs) != 1 {
		t.Fatalf("%d runs, want 1", len(runs))
	}

	want := domain.RunInput{
		Brief:      "Add a version command\n\nPrint the build's version.\n\n" + feedback,
		BranchHead: branchHead, BaseHead: baseHead,
	}

	run := runs[0]
	if run.Input != want {
		t.Errorf("run input =\n%+v\nwant\n%+v", run.Input, want)
	}

	if string(run.Result.Output) != `{"lane":"backend"}` || run.Result.PushedHead != pushedHead {
		t.Errorf("run output %s and pushed head %q; want the agent's output and %q", run.Result.Output, run.Result.PushedHead, pushedHead)
	}
}

// A retry runs in its claim's prepared box, so it records its own brief (with why its first
// output was rejected), output and pushed head, and no heads: only a claim's first run can be
// tried.
func TestRun_RetryInTheSameBoxRecordsNoHeads(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Stations[1].Checks = []string{makeTest} // the work station, after the inbox
	h := newHarness(t, line, porttest.Sequence(pushed(`{"try":1}`, branchHead), pushed(`{"try":2}`, pushedHead)))
	h.worker.Heads(branchHead, baseHead)
	h.worker.Exit(makeTest, 2, 0)
	it := h.submit(t, domain.NewItem{Title: "Add a version command"})

	h.step(t)

	if n := len(h.worker.Prepares()); n != 1 {
		t.Fatalf("%d prepares, want 1: the retry runs in the first run's box", n)
	}

	runs := h.runs(t, testOrg, it.ID)
	if len(runs) != 2 {
		t.Fatalf("%d runs, want 2", len(runs))
	}

	first, retry := runs[0], runs[1]
	if first.Input.BranchHead != branchHead || first.Input.BaseHead != baseHead {
		t.Errorf("first run heads %+v; want %q and %q", first.Input, branchHead, baseHead)
	}

	feedback := h.worker.Runs()[1].Env["GOAL_FEEDBACK"]
	if !strings.Contains(feedback, "`make test` exited 2") {
		t.Fatalf("retry feedback %q does not carry the failed check", feedback)
	}

	want := domain.RunInput{Brief: "Add a version command\n\n" + feedback}
	if retry.Input != want {
		t.Errorf("retry input =\n%+v\nwant\n%+v", retry.Input, want)
	}

	if string(retry.Result.Output) != `{"try":2}` || retry.Result.PushedHead != pushedHead {
		t.Errorf("retry output %s and pushed head %q; want its own", retry.Result.Output, retry.Result.PushedHead)
	}
}

// Without a repository a run records its brief and output, and no heads, even when the worker
// reports some; nothing fails.
func TestRun_WithoutARepositoryRecordsNoHeads(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{"lane":"backend"}`))
	h.worker.Heads(branchHead, baseHead)
	it := h.submit(t, domain.NewItem{Title: "Write the release notes"})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Fatalf("item is %s, want done (events %v)", got.State, h.kinds(it.ID))
	}

	run := h.runs(t, testOrg, it.ID)[0]
	if run.Input != (domain.RunInput{Brief: "Write the release notes"}) || run.Result.PushedHead != "" {
		t.Errorf("run input %+v and pushed head %q; want only the brief", run.Input, run.Result.PushedHead)
	}

	if string(run.Result.Output) != `{"lane":"backend"}` {
		t.Errorf("run output %s; want the agent's", run.Result.Output)
	}
}
