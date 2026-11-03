package cli

import (
	"errors"
	"strings"
	"testing"
)

// The MCP servers the CLI's environment tests name.
const (
	linearMCP = "https://mcp.linear.example.com/mcp"
	docsMCP   = "https://mcp.docs.example.com/mcp"
	// envWithServers is acme's default environment with a remote server with a key, one without
	// and a stdio one.
	envWithServers = `apiVersion: goal/v1
kind: Environment
spec:
  env:
    ACME_KEY: "{{acme-token}}"
  mcp:
    linear:
      url: ` + linearMCP + `
      headers: {Authorization: "Bearer {{acme-token}}"}
    docs:
      url: ` + docsMCP + `
    playwright:
      command: [npx, "@playwright/mcp@0.0.41"]
`
)

// publishWithServers creates acme-token for every factory and publishes acme with envWithServers.
func (g *defGoald) publishWithServers(t *testing.T) {
	t.Helper()

	if out, err := g.goalIn(t, secretValue+"\n", cmdSecret, verbCreate, secretName, "--all-factories"); err != nil {
		t.Fatalf("create = %q, %v", out, err)
	}

	g.mustGoal(t, "factory", "push", newCopy(t, map[string]string{envPath: envWithServers}), "--publish")

	if _, err := g.cache.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// goal env test prints each server's tools, failure or that it is not tested, exits 1 when one
// failed, and --server tests only one; no output holds the secret.
func TestEnvTestCommand(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.publishWithServers(t)
	g.tools.Serve(linearMCP, "list_issues", "create_issue")
	g.tools.Fail(docsMCP, errors.New("connection refused"))

	out, err := g.goal(t, "env", "test", "acme")
	if !errors.Is(err, errEnvTestFailed) {
		t.Fatalf("env test = %v; want errEnvTestFailed\n%s", err, out)
	}

	for _, want := range []string{
		"acme default (v1)", "✓ linear", "2 tools: create_issue, list_issues", "✗ docs", "connection refused", "- playwright", "not tested",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("env test = %s; want %q", out, want)
		}
	}

	one := g.mustGoal(t, "env", "test", "acme", "default", "--server", "linear")
	if !strings.Contains(one, "✓ linear") || strings.Contains(one, "docs") {
		t.Errorf("env test --server linear = %s", one)
	}

	if strings.Contains(out+one, secretValue) {
		t.Error("an output holds the secret")
	}

	if _, err := g.goal(t, "env", "test"); !errors.Is(err, errUsage) {
		t.Errorf("env test without a factory = %v; want errUsage", err)
	}

	if _, err := g.goal(t, "env", "test", "acme", "review"); err == nil || !strings.Contains(err.Error(), "review") {
		t.Errorf("env test of an unknown environment = %v; want it named", err)
	}
}

// goal access prints the counts, then each group's rows with kind, status, secrets and uses.
func TestAccessCommand(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.publishWithServers(t)
	g.tools.Serve(linearMCP, "list_issues")
	g.tools.Fail(docsMCP, errors.New("connection refused"))

	if _, err := g.goal(t, "env", "test", "acme"); !errors.Is(err, errEnvTestFailed) {
		t.Fatal(err)
	}

	out := g.mustGoal(t, "access")
	for _, want := range []string{
		"4 reached, 1 need attention", "MCP servers", "mcp  docs", "failed", "(connection refused)", "mcp  linear", "tested",
		"acme-token", "acme/default", "Variables", "env  ACME_KEY",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("access = %s; want %q", out, want)
		}
	}

	bySecret := g.mustGoal(t, "access", "--factory", "acme", "--by", "secret")
	if !strings.Contains(bySecret, "\nacme-token\n") || strings.Contains(bySecret, "docs") {
		t.Errorf("access --by secret = %s", bySecret)
	}

	if _, err := g.goal(t, "access", "--by", "team"); !errors.Is(err, errUsage) {
		t.Errorf("access --by team = %v; want errUsage", err)
	}
}
