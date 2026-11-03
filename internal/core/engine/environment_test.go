package engine_test

import (
	"context"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The test organization's secrets (ADR 0037) and their values.
const (
	secClaude  = "claude-key"
	secGitHub  = "github-token"
	secDB      = "db-pass"
	secTickets = "tickets-key"

	apiKeyValue      = "sk-ant-api03-key-for-tests-2223"   //nolint:gosec // a test value
	oauthTokenValue  = "sk-ant-oat01-token-for-tests-2425" //nolint:gosec // a test value
	githubTokenValue = "github-token-for-tests-2627"
	dbPassValue      = "db-pass-for-tests-2829"
	ticketsKeyValue  = "tickets-key-for-tests-3031"

	envBrowser     = "browser"
	envDatabaseURL = "DATABASE_URL"
	envGHToken     = "GH_TOKEN"
	envSentryOrg   = "SENTRY_ORG"
)

// envGatewayURL is the variable every exec gets the gateway's address in.
const envGatewayURL = "GOAL_GATEWAY_URL"

// secretsLine is a version on secrets: its model key is {{claude-key}}, its default environment
// gives GH_TOKEN, DATABASE_URL with {{db-pass}} inside it and a plain SENTRY_ORG, and its browser
// environment TICKETS_KEY. It keeps a repository connection and agent connections, leftovers a
// run on secrets never opens.
func secretsLine() domain.Line {
	line := lineOf(agentStation(stWork, agWorker, 1, 1))
	line.Runtime.ModelConnection, line.Runtime.ModelKey = "", "{{"+secClaude+"}}"
	line.Runtime.Repository = domain.Repository{URL: repoURL, Connection: connGit, Token: "{{" + secGitHub + "}}"}
	line.Connections = []string{connTracker}
	line.Agents = agentConnections(line.Agents, agWorker, connTracker)
	line.Environments = []domain.Environment{
		{Name: domain.DefaultEnvironment, Env: map[string]string{
			envGHToken: "{{" + secGitHub + "}}", envDatabaseURL: "postgres://app:{{ " + secDB + " }}@db/app", envSentryOrg: "acme",
		}},
		{Name: envBrowser, Env: map[string]string{"TICKETS_KEY": "{{" + secTickets + "}}"}},
	}

	return line
}

// withEnvironment gives agent name the environment env.
func withEnvironment(line domain.Line, name, env string) domain.Line {
	line.Agents = slices.Clone(line.Agents)

	for i := range line.Agents {
		if line.Agents[i].Name == name {
			line.Agents[i].Environment = env
		}
	}

	return line
}

// addSecret creates secret name valued value in the test organization, for every factory unless
// scope lists some.
func (h *harness) addSecret(t *testing.T, name, value string, scope ...domain.FactoryID) {
	t.Helper()

	n := domain.NewSecret{Name: name, Value: value, Scope: domain.SecretScope{All: len(scope) == 0, Factories: scope}}
	if _, err := h.orgSecrets.Create(inOrg(t), n, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

// addSecrets creates the default environment's secrets and the model key, valued apiKeyValue.
func (h *harness) addSecrets(t *testing.T) {
	t.Helper()

	h.addSecret(t, secClaude, apiKeyValue)
	h.addSecret(t, secGitHub, githubTokenValue)
	h.addSecret(t, secDB, dbPassValue)
}

// secretsUsed are the secret.used audit entries' secret names, sorted, failing the test unless
// each records the test factory, agent and a run.
func (h *harness) secretsUsed(t *testing.T, agent string) []string {
	t.Helper()

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditSecretUsed, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	var names []string

	for _, e := range entries {
		name, _ := e.Data["name"].(string)
		names = append(names, name)

		if f, _ := e.Data["factory_id"].(string); f != string(factoryID) {
			t.Errorf("secret.used of %s has factory %q; want %s", name, f, factoryID)
		}

		if a, _ := e.Data["agent"].(string); a != agent {
			t.Errorf("secret.used of %s has agent %q; want %q", name, a, agent)
		}

		if r, _ := e.Data["run_id"].(string); r == "" {
			t.Errorf("secret.used of %s has no run", name)
		}
	}

	slices.Sort(names)

	return names
}

// noConnectionUsed fails the test when the run opened a connection or a credential.
func (h *harness) noConnectionUsed(t *testing.T) {
	t.Helper()

	for _, action := range []string{domain.AuditConnectionUsed, domain.AuditCredentialUsed} {
		entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: action, Limit: 10})
		if err != nil || len(entries) != 0 {
			t.Errorf("%s entries %v (%v); want none on secrets", action, entries, err)
		}
	}
}

// oneAgentRun is the one agent exec of the test, failing it unless there is exactly one.
func (h *harness) oneAgentRun(t *testing.T, it domain.Item) porttest.ExecCall {
	t.Helper()

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs, want 1 (events %v)", len(runs), h.kinds(it.ID))
	}

	return runs[0]
}

// A run on secrets gets its default environment's env with each {{secret}} filled in, whole or
// part of a value, and plain values as written, and the model key. Each secret is opened once for
// the run's factory, agent and run, one secret.used each, and no connection or credential is.
func TestRunEnv_EnvironmentSecrets(t *testing.T) {
	t.Parallel()

	h := newHarness(t, secretsLine(), porttest.Succeed(`{}`))
	h.addSecrets(t)
	h.addSecret(t, secTickets, ticketsKeyValue) // the browser environment's, which the agent does not get
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	env := h.oneAgentRun(t, it).Env
	want := map[string]string{
		envGHToken: githubTokenValue, envDatabaseURL: "postgres://app:" + dbPassValue + "@db/app", envSentryOrg: "acme",
		domain.EnvAnthropicAPIKey: apiKeyValue,
	}

	for name, v := range want {
		if env[name] != v {
			t.Errorf("the agent's %s = %q; want %q", name, env[name], v)
		}
	}

	for _, name := range []string{"TICKETS_KEY", envTrackerToken, envModelKey, domain.EnvClaudeOAuthToken} {
		if _, ok := env[name]; ok {
			t.Errorf("the agent got %s; a run on secrets gets only its environment's env and the model key", name)
		}
	}

	if got, want := h.secretsUsed(t, agWorker), []string{secClaude, secDB, secGitHub}; !slices.Equal(got, want) {
		t.Errorf("secret.used of %v; want one each of %v", got, want)
	}

	h.noConnectionUsed(t)

	logs := h.logs.String()
	if !strings.Contains(logs, `"msg":"secret used"`) {
		t.Errorf("no secret used line in the logs:\n%s", logs)
	}

	for _, v := range []string{apiKeyValue, githubTokenValue, dbPassValue} {
		if strings.Contains(logs, v) {
			t.Errorf("a secret's value reached the logs:\n%s", logs)
		}
	}
}

// An agent that names an environment gets that one's env, not the default's, and only its
// secrets are opened.
func TestRunEnv_NamedEnvironment(t *testing.T) {
	t.Parallel()

	h := newHarness(t, withEnvironment(secretsLine(), agWorker, envBrowser), porttest.Succeed(`{}`))
	h.addSecrets(t)
	h.addSecret(t, secTickets, ticketsKeyValue)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	env := h.oneAgentRun(t, it).Env
	if env["TICKETS_KEY"] != ticketsKeyValue || env[domain.EnvAnthropicAPIKey] != apiKeyValue {
		t.Errorf("the agent's TICKETS_KEY and model key are not the secrets' values")
	}

	for _, name := range []string{envGHToken, envDatabaseURL, envSentryOrg} {
		if _, ok := env[name]; ok {
			t.Errorf("the agent got %s of the default environment", name)
		}
	}

	if got, want := h.secretsUsed(t, agWorker), []string{secClaude, secTickets}; !slices.Equal(got, want) {
		t.Errorf("secret.used of %v; want %v", got, want)
	}
}

// A secret that is missing or whose scope leaves out the factory blocks the run before any
// sandbox opens, with a reason that names the secret and the environment and holds no value;
// nothing is audited as used. So does an environment the version has no file for.
func TestRunEnv_RefusesSecretOutsideScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line domain.Line
		// db, when set, creates secDB with this scope.
		db   []domain.FactoryID
		want []string
	}{
		{
			name: "a factory outside its scope", line: secretsLine(), db: []domain.FactoryID{otherFactory},
			want: []string{"environment default", secDB, ports.ErrSecretNotAllowed.Error()},
		},
		{name: "a missing secret", line: secretsLine(), want: []string{"environment default", secDB, ports.ErrNotFound.Error()}},
		{
			name: "a missing environment", line: withEnvironment(secretsLine(), agWorker, "gpu"), db: []domain.FactoryID{factoryID},
			want: []string{"agent " + agWorker, "environment gpu"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, tt.line, porttest.Succeed(`{}`))
			h.addSecret(t, secClaude, apiKeyValue)
			h.addSecret(t, secGitHub, githubTokenValue)

			if tt.db != nil {
				h.addSecret(t, secDB, dbPassValue, tt.db...)
			}

			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != domain.StateBlocked {
				t.Fatalf("item is %s, want blocked (events %v)", got.State, h.kinds(it.ID))
			}

			reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string)
			for _, want := range tt.want {
				if !strings.Contains(reason, want) {
					t.Errorf("blocked reason %q does not mention %q", reason, want)
				}
			}

			for _, v := range []string{apiKeyValue, githubTokenValue, dbPassValue} {
				if strings.Contains(reason, v) {
					t.Errorf("blocked reason %q holds a secret value", reason)
				}
			}

			if n := len(h.worker.Runs()); n != 0 {
				t.Errorf("%d agent runs, want none", n)
			}

			h.noSandboxes(t)

			if used := h.secretsUsed(t, agWorker); len(used) != 0 {
				t.Errorf("secret.used of %v; want none", used)
			}
		})
	}
}

// The model key goes in CLAUDE_CODE_OAUTH_TOKEN when it is an OAuth token and in
// ANTHROPIC_API_KEY otherwise, never both.
func TestRunEnv_ModelKey(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ value, env, not string }{
		"an API key":     {apiKeyValue, domain.EnvAnthropicAPIKey, domain.EnvClaudeOAuthToken},
		"an OAuth token": {oauthTokenValue, domain.EnvClaudeOAuthToken, domain.EnvAnthropicAPIKey},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, secretsLine(), porttest.Succeed(`{}`))
			h.addSecret(t, secClaude, tt.value)
			h.addSecret(t, secGitHub, githubTokenValue)
			h.addSecret(t, secDB, dbPassValue)
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			env := h.oneAgentRun(t, it).Env
			if env[tt.env] != tt.value {
				t.Errorf("the agent's %s is not the model key", tt.env)
			}

			if _, ok := env[tt.not]; ok {
				t.Errorf("the agent got %s too", tt.not)
			}
		})
	}
}

// With the organization policy's Use credentials off, the agent gets no environment env and its
// secrets are not opened, but the model key still reaches it.
func TestRunEnv_PolicyOffKeepsModelKey(t *testing.T) {
	t.Parallel()

	h := newHarness(t, secretsLine(), porttest.Succeed(`{}`))
	h.withPolicy(t, policy(func(p *domain.OrgPolicy) { p.UseCredentials = domain.ToolOff }))
	h.addSecrets(t)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	env := h.oneAgentRun(t, it).Env
	if env[domain.EnvAnthropicAPIKey] != apiKeyValue {
		t.Error("the agent did not get the model key")
	}

	for _, name := range []string{envGHToken, envDatabaseURL, envSentryOrg} {
		if _, ok := env[name]; ok {
			t.Errorf("the agent got %s with Use credentials off", name)
		}
	}

	if got, want := h.secretsUsed(t, agWorker), []string{secClaude}; !slices.Equal(got, want) {
		t.Errorf("secret.used of %v; want %v", got, want)
	}
}

// Every value a run opens is scrubbed from what it reports, its events, its error and goald's
// logs: a secret filled into part of a value too, when the agent prints it alone.
func TestRunEnv_ScrubsSecretValues(t *testing.T) {
	t.Parallel()

	h := newHarness(t, secretsLine(), func(_ context.Context, run porttest.ExecCall, stdout io.Writer) domain.RunResult {
		pass := strings.TrimSuffix(strings.TrimPrefix(run.Env[envDatabaseURL], "postgres://app:"), "@db/app")
		_, _ = io.WriteString(stdout, `{"type":"message","at":"2026-10-23T09:00:02Z","text":"pass `+pass+
			` token `+run.Env[envGHToken]+`"}`+"\n")

		return domain.RunResult{Status: domain.RunFailed, Error: "gave up: " + pass + " " + run.Env[domain.EnvAnthropicAPIKey]}
	})
	h.addSecrets(t)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	runs := h.runs(t, testOrg, it.ID)
	if len(runs) != 1 {
		t.Fatalf("%d runs, want 1", len(runs))
	}

	if got := runs[0].Result.Error; got != "gave up: [redacted] [redacted]" {
		t.Errorf("run error %q, want the secrets' values redacted", got)
	}

	reported := []string{h.logs.String()}
	for _, e := range h.store.Events(it.ID) {
		reported = append(reported, mustJSON(t, e.Data))
	}

	for _, r := range reported {
		for _, v := range []string{apiKeyValue, githubTokenValue, dbPassValue} {
			if strings.Contains(r, v) {
				t.Errorf("a secret's value reached what the run reports:\n%s", r)
			}
		}
	}
}

// A Jonah run of a version on secrets gets the model key alone: no environment, no connection.
func TestRunTask_JonahOnSecrets(t *testing.T) {
	t.Parallel()

	h := newHarness(t, secretsLine(), porttest.Succeed(`{"observations":[],"proposals":[]}`))
	h.store.AddFactory(testOrg, factoryID)
	h.addSecrets(t)

	req := jonahRequest()
	req.ModelConnection, req.ModelKey = "", "{{"+secClaude+"}}"

	if _, err := h.e.RunTask(t.Context(), req); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs; want 1", len(runs))
	}

	keys := slices.Sorted(maps.Keys(runs[0].Env))
	if want := []string{domain.EnvAnthropicAPIKey, "GOAL_FEEDBACK", "GOAL_GATEWAY_TOKEN", envGatewayURL}; !slices.Equal(keys, want) {
		t.Errorf("the agent's env = %v; want %v", keys, want)
	}

	if got, want := h.secretsUsed(t, agJonah), []string{secClaude}; !slices.Equal(got, want) {
		t.Errorf("secret.used of %v; want %v", got, want)
	}

	h.noConnectionUsed(t)
}

// A draft request's box on a version on secrets gets the model key alone.
func TestRunDefinition_ModelKeyOnly(t *testing.T) {
	t.Parallel()

	h := newHarness(t, secretsLine(), porttest.Succeed(definitionOutput))
	h.addSecrets(t)

	if _, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: draftRequest(), Revision: 1}); err != nil {
		t.Fatal(err)
	}

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs; want 1", len(runs))
	}

	keys := slices.Sorted(maps.Keys(runs[0].Env))
	if want := []string{domain.EnvAnthropicAPIKey, envGatewayURL}; !slices.Equal(keys, want) {
		t.Errorf("the box's env = %v; want %v", keys, want)
	}

	if got, want := h.secretsUsed(t, ""), []string{secClaude}; !slices.Equal(got, want) {
		t.Errorf("secret.used of %v; want %v", got, want)
	}

	h.noConnectionUsed(t)
}
