package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// fakeChecks gives every check checkInput.
type fakeChecks struct{}

// checkInput is what the check tests' check is given.
var checkInput = domain.CheckInput{
	Open:          []domain.CheckOpenItem{{Key: "INC-1", Ref: "#4", Title: "Checkout is down", State: "queued", LastNote: "still down"}},
	Closed:        []domain.CheckClosed{{Key: "INC-0", Ref: "#3", How: "done", Why: "fixed", At: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}},
	LastDecisions: json.RawMessage(`{"file":[],"note":[],"skip":[]}`),
	State:         json.RawMessage(`{"since":"2026-10-23T11:55:00Z"}`),
}

func (fakeChecks) CheckInput(context.Context, domain.FactoryID, string, time.Time) (domain.CheckInput, error) {
	return checkInput, nil
}

// checksOf is the harness's Deps.Checks: none with d.noChecks.
func checksOf(d harnessDeps) Checks {
	if d.noChecks {
		return nil
	}

	return fakeChecks{}
}

// checkYAML is a factory on secrets whose new-incidents trigger runs a check by the engineer,
// who may edit and allows an MCP tool that asks first.
const checkYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-bundle }
spec:
  runtime:
    model: claude-sonnet-5-5
    modelKey: "{{claude-key}}"
    repository: { url: "https://github.com/acme/api.git", token: "{{github-token}}" }
  agents:
    - { name: engineer, prompt: ./agents/engineer.md, builtins: { read: allowed, edit: allowed, run: allowed } }
  board:
    workTypes: [fix]
    triggers:
      - name: new-incidents
        on: schedule
        cron: "*/5 * * * *"
        check: { agent: engineer, brief: Look for new incidents., maxUsd: 0.2 }
        create: { type: fix, station: build }
    stations:
      - { id: spec, kind: human }
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`

// checkEnvironment is the engineer's environment: a pager server whose reads ask first.
const checkEnvironment = "apiVersion: goal/v1\nkind: Environment\nspec:\n  size: medium\n" +
	"  mcp:\n    pager: { url: \"https://pager.example.com/mcp\", tools: { \"get_*\": ask, \"*\": off } }\n"

// checkClaims are a check token's claims: new-incidents by the engineer, with no item or station.
func (h *harness) checkClaims() domain.TaskClaims {
	c := h.claims()
	c.Kind, c.Trigger, c.ItemID, c.Station, c.Connections, c.RunID = domain.TaskKindCheck, "new-incidents", "", "", nil, "run-check"

	return c
}

// A check's bundle (ADR 0040 decision 19) is the check's agent with its brief and input, the
// answer contract as its output schema and check.maxUsd as max_usd; the built-ins that edit or
// run are denied, the environment's ask tools are off, and it has no item, repository, git login,
// rule or knowledge. Its MCP session lists no tool, so question__ask is not offered.
func TestBundle_Check(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: checkYAML, files: map[string]string{"environments/default.yaml": checkEnvironment}})
	c := h.checkClaims()

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var b runBundle
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}

	s := b.Spec
	if s.Check == nil || s.Check.Trigger != "new-incidents" || s.Brief != "Look for new incidents." || s.MaxUSD != 0.2 || s.ItemID != "" || s.Station != "" {
		t.Fatalf("spec = %+v; want new-incidents' check with its brief and max_usd 0.2, no item or station", s)
	}

	if got, _ := json.Marshal(s.Check.Input); !strings.Contains(string(got), `"INC-1"`) || !strings.Contains(string(got), `"since"`) {
		t.Errorf("check input = %s; want the open items and the saved state", got)
	}

	if string(b.Files["agent/output.schema.json"]) != domain.CheckAnswerSchema || len(b.Files["agent/prompt.md"]) == 0 {
		t.Errorf("files = %v; want the engineer's prompt and the answer contract", keys(b.Files))
	}

	for _, tool := range []string{"Write", "Edit", "Bash"} {
		if slices.Contains(s.ToolsAllow, tool) || !slices.Contains(s.ToolsDeny, tool) {
			t.Errorf("tools allow %v, deny %v; want %s denied", s.ToolsAllow, s.ToolsDeny, tool)
		}
	}

	if !slices.Contains(s.ToolsAllow, "Read") {
		t.Errorf("tools allow %v; want Read kept", s.ToolsAllow)
	}

	if s.Environment == nil || len(s.Environment.MCPServers) != 1 {
		t.Fatalf("environment = %+v; want the pager server", s.Environment)
	}

	for _, m := range s.Environment.MCPServers[0].ToolModes {
		if m.Mode == domain.ToolModeAsk {
			t.Errorf("tool modes = %+v; want ask served as off", s.Environment.MCPServers[0].ToolModes)
		}
	}

	if b.Repository != nil || len(b.Git) != 0 || s.Knowledge != nil || len(s.Rules) != 0 || len(s.GrantedCalls) != 0 {
		t.Errorf("bundle = %+v; want no repository, git login, knowledge, rule or granted call", b)
	}

	expectNoKeys(t, body)

	sess, err := h.gw.session(domain.WithOrg(t.Context(), h.org), c)
	if err != nil || !sess.noTools {
		t.Errorf("session = %+v, %v; want one that lists no tool", sess, err)
	}
}

// A check's bundle is 503 without Deps.Checks, and 403 for a trigger without a check or a check
// of another agent.
func TestBundle_CheckRefused(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		deps   harnessDeps
		change func(*domain.TaskClaims)
		want   int
	}{
		"no checks":     {harnessDeps{noChecks: true}, func(*domain.TaskClaims) {}, http.StatusServiceUnavailable},
		"other trigger": {harnessDeps{}, func(c *domain.TaskClaims) { c.Trigger = "nightly" }, http.StatusForbidden},
		"other agent":   {harnessDeps{}, func(c *domain.TaskClaims) { c.Agent = "reviewer" }, http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tt.deps.yaml, tt.deps.files = checkYAML, map[string]string{"environments/default.yaml": checkEnvironment}
			h := newHarnessWith(t, Config{}, tt.deps)
			c := h.checkClaims()
			tt.change(&c)

			if status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c)); status != tt.want {
				t.Errorf("status = %d (%s), want %d", status, body, tt.want)
			}
		})
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return out
}
