package engine_test

import (
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
