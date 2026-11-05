package authoring

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// Warning codes of the GitHub tools check (ADR 0041 decision 14): an agent that only reads code,
// or a check's agent (ADR 0040 decision 19), may call GitHub MCP tools that write, or tools whose
// access nobody tested.
const (
	// CodeGitHubToolsNotReadOnly: the server's latest passing test lists tools without
	// readOnlyHint that the environment file allows.
	CodeGitHubToolsNotReadOnly = "github-tools-not-read-only"
	// CodeGitHubToolsUnknown: the server has no kept test, or its latest one failed.
	CodeGitHubToolsUnknown = "github-tools-unknown"
)

// maxNamedTools is how many tools a github-tools-not-read-only warning names before "and N more".
const maxNamedTools = 5

// EnvironmentTestList lists the organization of ctx's kept environment tests of factory
// (ports.EnvironmentTests' List).
type EnvironmentTestList interface {
	List(ctx context.Context, factory domain.FactoryID) ([]domain.EnvironmentTest, error)
}

// readsOnly is an agent that must not write to code, where a warning about it goes and how the
// warning names it.
type readsOnly struct {
	agent config.Agent
	at    string
	who   string
}

// readersAndChecks are the agents whose code is read, located at their code, then each trigger
// check's agent, located at its check.agent whatever its code.
func readersAndChecks(f *config.Factory) []readsOnly {
	var out []readsOnly

	for i, a := range f.Spec.Agents {
		if f.AgentCode(a) == config.CodeRead {
			out = append(out, readsOnly{agent: a, at: "/spec/agents/" + strconv.Itoa(i) + "/code", who: "agent " + a.Name + ", which only reads code,"})
		}
	}

	for i, t := range f.Spec.Board.Triggers {
		if t.Check == nil {
			continue
		}

		j := slices.IndexFunc(f.Spec.Agents, func(a config.Agent) bool { return a.Name == t.Check.Agent })
		if j < 0 {
			continue // the definition check reports it
		}

		out = append(out, readsOnly{
			agent: f.Spec.Agents[j], at: "/spec/board/triggers/" + strconv.Itoa(i) + "/check/agent",
			who: "trigger " + t.Name + "'s check agent " + t.Check.Agent + ",",
		})
	}

	return out
}

// gitHubTools warns, for each agent that only reads code and each check, about each GitHub MCP
// server of its environment whose latest passing test lists tools that aren't read-only and that
// the environment file allows (ask and off don't count), and about each one whose access is
// unknown because it has no passing test. It judges by the kept readOnlyHint, never by tool names.
func (r *secretsRun) gitHubTools(tests []domain.EnvironmentTest) {
	envs := r.f.Environments()

	for _, ro := range readersAndChecks(r.f) {
		name := ro.agent.EnvironmentName()

		k := slices.IndexFunc(envs, func(e config.Environment) bool { return e.Name == name })
		if k < 0 {
			continue // the definition check reports it
		}

		mcp := envs[k].Spec.MCP
		for _, server := range slices.Sorted(maps.Keys(mcp)) {
			if mcp[server].GitHub() {
				r.gitHubServer(ro, name, server, mcp[server], tests)
			}
		}
	}
}

// gitHubServer warns about one GitHub server of ro's environment env, as gitHubTools says.
func (r *secretsRun) gitHubServer(ro readsOnly, env, server string, s config.MCPServer, tests []domain.EnvironmentTest) {
	at := r.f.Locate(ro.at)
	where := fmt.Sprintf("%s environment %s, MCP server %s", ro.who, env, server)

	i := slices.IndexFunc(tests, func(t domain.EnvironmentTest) bool { return t.Environment == env && t.Server == server })
	if i < 0 || !tests[i].OK {
		r.add(at, domain.SeverityWarning, CodeGitHubToolsUnknown,
			where+": whether its tools write is unknown until the server is tested; test the environment, or use the github-read template")

		return
	}

	t, modes := tests[i], s.ToolModes()

	var writing []string

	for _, tool := range t.Tools {
		if !slices.Contains(t.ReadOnly, tool) && domain.ResolveToolMode(modes, tool) == domain.ToolModeAllow {
			writing = append(writing, tool)
		}
	}

	if len(writing) == 0 {
		return
	}

	slices.Sort(writing)

	named := strings.Join(writing[:min(len(writing), maxNamedTools)], ", ")
	if more := len(writing) - maxNamedTools; more > 0 {
		named += fmt.Sprintf(" and %d more", more)
	}

	r.add(at, domain.SeverityWarning, CodeGitHubToolsNotReadOnly, fmt.Sprintf(
		"%s: may call GitHub tools that aren't read-only, as the test of %s found them: %s; "+
			"set them to ask or off, or use the github-read template",
		where, t.At.UTC().Format("2006-01-02"), named))
}
