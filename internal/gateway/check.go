package gateway

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Checks read what a trigger's check is given (ADR 0040 decision 19); ports.Intake satisfies it.
type Checks interface {
	CheckInput(ctx context.Context, factory domain.FactoryID, trigger string, now time.Time) (domain.CheckInput, error)
}

// errNoChecks answers a check token's bundle when the gateway has no Deps.Checks.
var errNoChecks = errors.New("this gateway serves no check")

// bundleCheck is a check's trigger and what it is given besides its brief: the open items its
// trigger filed, the keys whose items closed, the last check's decisions and the saved state.
type bundleCheck struct {
	Trigger string            `json:"trigger"`
	Input   domain.CheckInput `json:"input"`
}

// editTools are the built-in tools that write or run, which a check never has: it only reads.
var editTools = []string{"Write", "Edit", "MultiEdit", "NotebookEdit", "Bash", "Task"}

// checkBundle is a check's bundle (ADR 0040 decision 19; apps/worker/README.md, "Check mode"):
// the check's agent's prompt and skills, the fixed answer contract as its output schema, the
// trigger's check brief as the brief and its input as spec.check. Its tools are the agent's but
// the built-ins that edit or run, which are denied; its environment or connections are the
// agent's, with every ask tool off, as no person can answer one; check.maxUsd is its max_usd. It
// has no item, station, repository, rule, knowledge or granted call. A trigger the version does
// not have, one without a check or a check of another agent wraps errForbidden.
func (g *Gateway) checkBundle(ctx context.Context, c domain.TaskClaims) (runBundle, error) {
	if g.checks == nil {
		return runBundle{}, errNoChecks
	}

	f, err := g.defs.Version(ctx, c.VersionID)
	if err != nil {
		return runBundle{}, fmt.Errorf("load factory %s version %s: %w", c.FactoryID, c.VersionID, err)
	}

	if f.Metadata.ID != string(c.FactoryID) {
		return runBundle{}, fmt.Errorf("%w: version %s is not a version of factory %s", errForbidden, c.VersionID, c.FactoryID)
	}

	line := f.Line()

	t, ok := line.Trigger(c.Trigger)
	if !ok || t.Check == nil || t.Check.Agent != c.Agent {
		return runBundle{}, fmt.Errorf("%w: trigger %q of factory %s has no check by agent %q", errForbidden, c.Trigger, c.FactoryID, c.Agent)
	}

	agent, ok := line.Agent(c.Agent)
	if !ok {
		return runBundle{}, fmt.Errorf("%w: agent %q is not in factory %s", errForbidden, c.Agent, c.FactoryID)
	}

	agentFiles, err := g.defs.AgentFiles(ctx, c.VersionID, agent, "")
	if err != nil {
		return runBundle{}, fmt.Errorf("agent %s files: %w", agent.Name, err)
	}

	in, err := g.checks.CheckInput(ctx, c.FactoryID, c.Trigger, g.clock.Now())
	if err != nil {
		return runBundle{}, fmt.Errorf("input of trigger %s's check: %w", c.Trigger, err)
	}

	maxTurns, _ := agent.Limits(line.Runtime)
	b := runBundle{
		Spec: bundleSpec{
			FactoryID: c.FactoryID, RunID: c.RunID, Agent: agent.Name, Model: cmp.Or(c.Model, agent.Model, line.Runtime.Model),
			Skills: make([]string, 0, len(agent.Skills)), ToolsAllow: readOnly(agent.ToolsAllow),
			ToolsDeny: withDenied(append(slices.Clone(agent.ToolsDeny), editTools...), c.DeniedCommands), Purpose: line.Purpose,
			Brief: t.Check.Brief, MaxTurns: maxTurns, MaxUSD: t.Check.MaxUSD,
			Connections: []bundleConnection{}, Rules: []bundleRule{}, GrantedCalls: []grantedCall{},
			Check: &bundleCheck{Trigger: t.Name, Input: in},
		},
		Files: make(map[string][]byte, len(agentFiles)+1),
		Git:   []gitCredential{},
		Tools: append([]string{}, line.Runtime.Tools...),
	}

	for _, sk := range agent.Skills {
		b.Spec.Skills = append(b.Spec.Skills, sk.Name)
	}

	for p, body := range agentFiles {
		b.Files[path.Join("agent", p)] = body
	}

	b.Files[path.Join("agent", "output.schema.json")] = []byte(domain.CheckAnswerSchema)

	if line.OnSecrets() {
		err = g.addEnvironment(ctx, c, line, agent, &b)
	} else {
		err = g.addConnections(ctx, c, line, agent, &b)
	}

	if err != nil {
		return runBundle{}, err
	}

	askOff(&b)

	return b, nil
}

// readOnly is the allow rules without those of the built-in tools that edit or run.
func readOnly(allow []string) []string {
	out := []string{}

	for _, rule := range allow {
		name, _, _ := strings.Cut(rule, "(")
		if !slices.Contains(editTools, name) {
			out = append(out, rule)
		}
	}

	return out
}

// askOff serves every ask tool of b's connections and environment servers as off: a check has
// no item for a person to answer on (ADR 0040 decision 19).
func askOff(b *runBundle) {
	for i := range b.Spec.Connections {
		c := &b.Spec.Connections[i]
		for tool, mode := range c.ToolModes {
			if mode == domain.ToolModeAsk {
				c.ToolModes[tool] = domain.ToolModeOff
			}
		}

		c.AlwaysAllowOffered = []string{}
	}

	if b.Spec.Environment == nil {
		return
	}

	for i := range b.Spec.Environment.MCPServers {
		for j, m := range b.Spec.Environment.MCPServers[i].ToolModes {
			if m.Mode == domain.ToolModeAsk {
				b.Spec.Environment.MCPServers[i].ToolModes[j].Mode = domain.ToolModeOff
			}
		}
	}
}
