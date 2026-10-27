package engine_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The models of the policy tests' platform (GOAL_MODELS).
const (
	modelOpus   = "claude-opus-5-5"
	modelSonnet = "claude-sonnet-5-5"
)

// withPolicy saves p as the test organization's policy over a platform that offers opus and
// sonnet, and rebuilds the harness's engine to read it.
func (h *harness) withPolicy(t *testing.T, p domain.OrgPolicy) {
	t.Helper()

	policies := porttest.NewOrgPolicies([]string{modelOpus, modelSonnet}, h.tenancy, h.clock.Now)
	if _, err := policies.Put(inOrg(t), p, 0, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	h.policies = policies
	h.e = h.engine(t, h.store)
}

// policy is the default policy over opus and sonnet with change applied.
func policy(change func(*domain.OrgPolicy)) domain.OrgPolicy {
	p := domain.DefaultOrgPolicy([]string{modelOpus, modelSonnet})
	change(&p)

	return p
}

// agentRun is the claim's one agent exec, failing the test unless there is exactly one.
func (h *harness) agentRun(t *testing.T) porttest.ExecCall {
	t.Helper()

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs, want 1", len(runs))
	}

	return runs[0]
}

// A run whose agent and factory name no model gets the policy's default model, recorded on the
// run and signed into its task token for the gateway's bundle.
func TestEngine_RunGetsThePolicyDefaultModel(t *testing.T) {
	t.Parallel()

	h := newHarness(t, repoLine(), porttest.Succeed(`{}`))
	h.withPolicy(t, policy(func(p *domain.OrgPolicy) { p.DefaultModel = modelSonnet }))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if runs := h.runs(t, testOrg, it.ID); len(runs) != 1 || runs[0].Model != modelSonnet {
		t.Errorf("runs %+v, want one on %s", runs, modelSonnet)
	}

	if c := h.claims(t, h.agentRun(t).Env["GOAL_GATEWAY_TOKEN"]); c.Model != modelSonnet {
		t.Errorf("task token model %q, want %s", c.Model, modelSonnet)
	}
}

// Every run carries the policy's denied commands in its task token, whatever its version's tool
// rules say, so the gateway adds them to its agent's deny rules.
func TestEngine_RunDeniesOrgCommands(t *testing.T) {
	t.Parallel()

	denied := []string{"Bash(terraform apply:*)", "Bash(git push --force:*)"}
	h := newHarness(t, repoLine(), porttest.Succeed(`{}`))
	h.withPolicy(t, policy(func(p *domain.OrgPolicy) { p.DeniedCommands = denied }))
	h.submit(t, domain.NewItem{})

	h.step(t)

	if c := h.claims(t, h.agentRun(t).Env["GOAL_GATEWAY_TOKEN"]); !slices.Equal(c.DeniedCommands, denied) {
		t.Errorf("task token denies %q, want %q", c.DeniedCommands, denied)
	}
}

// A run whose model the policy no longer allows is not started: no run, no sandbox, and the item
// waits for a person with a reason that names the model.
func TestEngine_RunWithADisallowedModelWaitsForAPerson(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Runtime.Model = modelOpus
	h := newHarness(t, line, porttest.Succeed(`{}`))
	h.withPolicy(t, policy(func(p *domain.OrgPolicy) { p.AllowedModels, p.DefaultModel = []string{modelSonnet}, modelSonnet }))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	h.expectAt(t, it, stWork, domain.StateWaitingHuman)
	h.expectOutcomes(t, it.ID, domain.LeaseEscalated)

	if runs := h.runs(t, testOrg, it.ID); len(runs) != 0 {
		t.Errorf("%d runs started, want none", len(runs))
	}

	if n := len(h.worker.Prepares()) + len(h.worker.Runs()); n != 0 {
		t.Errorf("%d sandbox execs, want none", n)
	}

	h.noSandboxes(t)

	waits := h.eventsOf(it.ID, domain.EventWaitingHuman)
	if len(waits) != 1 {
		t.Fatalf("%d waiting_human events, want 1 (events %v)", len(waits), h.kinds(it.ID))
	}

	if reason, _ := waits[0].Data["reason"].(string); !strings.Contains(reason, modelOpus) || !strings.Contains(reason, "organization policy") {
		t.Errorf("reason %q; want it to name %s and the organization policy", reason, modelOpus)
	}
}

// The policy's box time limit caps the run and its sandbox: a 10-minute limit under the default
// 2-hour task timeout gives a 10-minute agent exec and sandbox deadline.
func TestEngine_BoxTimeLimitCapsTheRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t, repoLine(), porttest.Succeed(`{}`))
	h.withPolicy(t, policy(func(p *domain.OrgPolicy) { p.BoxTimeLimitMinutes = 10 }))
	h.submit(t, domain.NewItem{})

	h.step(t)

	run := h.agentRun(t)
	if run.Timeout != 10*time.Minute {
		t.Errorf("agent exec timeout %s, want 10m", run.Timeout)
	}

	if want := t0.Add(10 * time.Minute); !run.Sandbox.Deadline.Equal(want) {
		t.Errorf("sandbox deadline %s, want %s", run.Sandbox.Deadline, want)
	}
}

// Read the web and docs off or ask denies the web builtins (WebFetch, WebSearch) after the
// policy's denied commands, each once, whatever the agent's builtins.web says; allowed leaves
// the agent's own setting.
func TestEngine_ReadWebPolicyDeniesWebTools(t *testing.T) {
	t.Parallel()

	denied := []string{"Bash(terraform apply:*)", "WebSearch"}
	tests := []struct {
		setting domain.ToolSetting
		want    []string
	}{
		{domain.ToolOff, []string{"Bash(terraform apply:*)", "WebSearch", "WebFetch"}},
		{domain.ToolAsk, []string{"Bash(terraform apply:*)", "WebSearch", "WebFetch"}},
		{domain.ToolAllowed, denied},
	}

	for _, tt := range tests {
		t.Run(string(tt.setting), func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, repoLine(), porttest.Succeed(`{}`))
			h.withPolicy(t, policy(func(p *domain.OrgPolicy) { p.DeniedCommands, p.ReadWeb = denied, tt.setting }))
			h.submit(t, domain.NewItem{})

			h.step(t)

			if c := h.claims(t, h.agentRun(t).Env["GOAL_GATEWAY_TOKEN"]); !slices.Equal(c.DeniedCommands, tt.want) {
				t.Errorf("task token denies %q, want %q", c.DeniedCommands, tt.want)
			}
		})
	}
}

// Merge a pull request off stops an approving agent gate whose action is merge from merging:
// the item waits for a person at the gate, with the approval recorded and a reason that names
// the organization policy, and a person's merge then merges it.
func TestEngine_MergePolicyOffLeavesTheMergeToAPerson(t *testing.T) {
	t.Parallel()

	h := newHarness(t, gateMergeLine(), byAgent(map[string]string{
		agWorker:   `{"pull_request":"` + prURL + `"}`,
		agReviewer: `{"decision":"approve"}`,
	}))
	h.withPolicy(t, policy(func(p *domain.OrgPolicy) { p.MergePullRequest = domain.ToolOff }))
	h.codehost.Add(openPR(prURL))

	it := h.submit(t, domain.NewItem{})
	h.step(t) // built
	h.step(t) // reviewed, not merged

	h.expectAt(t, it, stReview, domain.StateWaitingHuman)

	if merges := h.codehost.Merges(); len(merges) != 0 {
		t.Fatalf("merges %+v, want none before a person merges", merges)
	}

	if approved := h.eventsOf(it.ID, domain.EventGateApproved); len(approved) != 1 || approved[0].Actor != agReviewer {
		t.Errorf("gate_approved events %+v, want one by the reviewer", approved)
	}

	waits := h.eventsOf(it.ID, domain.EventWaitingHuman)
	if len(waits) != 1 {
		t.Fatalf("%d waiting_human events, want 1 (events %v)", len(waits), h.kinds(it.ID))
	}

	if reason, _ := waits[0].Data["reason"].(string); !strings.Contains(reason, "organization policy") {
		t.Errorf("reason %q; want it to name the organization policy", reason)
	}

	got, err := h.e.Merge(inOrg(t), it.ID, human, "")
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stDone, State: domain.StateDone}) {
		t.Errorf("item at %+v, want done", got.Position())
	}

	if merges := h.codehost.Merges(); !reflect.DeepEqual(merges, []porttest.MergeCall{squashOf(prURL, connGitHub)}) {
		t.Errorf("merges %+v, want app#7 by the person's merge", merges)
	}
}

// Use credentials off signs no connections into the run's task token, though its agent lists
// some; attached_only signs the agent's list.
func TestEngine_UseCredentialsPolicyOffGrantsNoConnections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setting domain.ToolSetting
		want    []string
	}{
		{domain.ToolOff, nil},
		{domain.ToolAttachedOnly, []string{connGitHub}},
	}

	for _, tt := range tests {
		t.Run(string(tt.setting), func(t *testing.T) {
			t.Parallel()

			line := repoLine()
			line.Connections = []string{connGitHub}
			i := slices.IndexFunc(line.Agents, func(a domain.AgentSpec) bool { return a.Name == agWorker })
			line.Agents[i].Connections = []string{connGitHub}

			h := newHarness(t, line, porttest.Succeed(`{}`))
			h.withPolicy(t, policy(func(p *domain.OrgPolicy) { p.UseCredentials = tt.setting }))
			h.submit(t, domain.NewItem{})

			h.step(t)

			if c := h.claims(t, h.agentRun(t).Env["GOAL_GATEWAY_TOKEN"]); !slices.Equal(c.Connections, tt.want) {
				t.Errorf("task token connections %q, want %q", c.Connections, tt.want)
			}
		})
	}
}
