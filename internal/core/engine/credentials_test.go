package engine_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The test organization's credentials (ADR 0017), their values and the connection that refers to
// one.
const (
	credTracker = "tracker-key"
	credDeploy  = "deploy-key"
	// connRef is a tracker connection whose token is {{tracker-key}}.
	connRef = "tracker-ref"
	// kindTracker is connRef's kind, the tracker connections'.
	kindTracker = "tracker"

	trackerKeyValue = "tracker-key-value-for-tests-1819"
	deployKeyValue  = "deploy-key-value-for-tests-2021"
	envDeployKey    = "DEPLOY_KEY"
)

// addCredential creates credential n in the test organization.
func (h *harness) addCredential(t *testing.T, n domain.NewCredential) {
	t.Helper()

	if n.Type == "" {
		n.Type = domain.CredentialAPIKey
	}

	if _, err := h.credentials.Create(inOrg(t), n, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

// addRefConnection creates connRef, whose token refers to credential credTracker.
func (h *harness) addRefConnection(t *testing.T) {
	t.Helper()

	conns, ok := h.connections.(*porttest.Connections)
	if !ok {
		t.Fatalf("the harness's connections are a %T", h.connections)
	}

	n := domain.NewConnection{Name: connRef, Kind: kindTracker, Secrets: map[string]string{fieldToken: "{{" + credTracker + "}}"}}
	if _, err := conns.Create(inOrg(t), n, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

// refLine is connLine whose worker is given connRef instead of connTracker.
func refLine() domain.Line {
	line := connLine()
	line.Connections = append(line.Connections, connRef)
	line.Agents = agentConnections(line.Agents, agWorker, connRef)

	return line
}

// trackerKey is credTracker allowing scope, valued trackerKeyValue.
func trackerKey(scope ...domain.FactoryID) domain.NewCredential {
	return domain.NewCredential{Name: credTracker, Scope: domain.CredentialScope{Factories: scope}, Value: trackerKeyValue}
}

// A connection whose secret field is {{name}} renders with the credential's value in the agent's
// env. The credential is opened for the run's factory, agent and run, audited once as
// credential.used with them, and logged by name, never by value.
func TestRunEnv_OpensCredentialReference(t *testing.T) {
	t.Parallel()

	h := newHarness(t, refLine(), porttest.Succeed(`{}`))
	h.addCredential(t, trackerKey(factoryID))
	h.addRefConnection(t)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs, want 1 (events %v)", len(runs), h.kinds(it.ID))
	}

	if got := runs[0].Env[envTrackerToken]; got != trackerKeyValue {
		t.Errorf("agent's %s is %q, want the credential's value", envTrackerToken, got)
	}

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditCredentialUsed, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	run := h.runs(t, testOrg, it.ID)[0]
	if len(entries) != 1 {
		t.Fatalf("%d credential.used entries, want 1", len(entries))
	}

	want := map[string]string{"name": credTracker, "factory_id": string(factoryID), "agent": agWorker, "run_id": string(run.ID)}
	for key, v := range want {
		if got, _ := entries[0].Data[key].(string); got != v {
			t.Errorf("credential.used %s is %q, want %q", key, got, v)
		}
	}

	logs := h.logs.String()
	if !strings.Contains(logs, `"msg":"credential used"`) || !strings.Contains(logs, credTracker) {
		t.Errorf("no credential used line naming %s in the logs:\n%s", credTracker, logs)
	}

	if strings.Contains(logs, trackerKeyValue) {
		t.Errorf("the credential's value reached the logs:\n%s", logs)
	}
}

// A connection that refers to a credential the run's factory may not open, or to none, blocks the
// run with a reason that names the credential and the connection and holds no value. No sandbox
// starts and nothing is audited as used.
func TestRunEnv_RefusesCredentialOutsideFactory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// cred, when set, is created.
		cred *domain.NewCredential
		want error
	}{
		{name: "a factory outside its scope", cred: new(trackerKey(otherFactory)), want: ports.ErrCredentialNotAllowed},
		{name: "a missing credential", want: ports.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, refLine(), porttest.Succeed(`{}`))
			if tt.cred != nil {
				h.addCredential(t, *tt.cred)
			}

			h.addRefConnection(t)
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != domain.StateBlocked {
				t.Fatalf("item is %s, want blocked (events %v)", got.State, h.kinds(it.ID))
			}

			reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string)
			for _, want := range []string{"connection " + connRef, "credential " + credTracker, tt.want.Error()} {
				if !strings.Contains(reason, want) {
					t.Errorf("blocked reason %q does not mention %q", reason, want)
				}
			}

			for _, secret := range []string{trackerKeyValue, modelKey, codeToken, trackerToken} {
				if strings.Contains(reason, secret) {
					t.Errorf("blocked reason %q holds a secret value", reason)
				}
			}

			if n := len(h.worker.Runs()); n != 0 {
				t.Errorf("%d agent runs, want none", n)
			}

			h.noSandboxes(t)

			used, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditCredentialUsed, Limit: 10})
			if err != nil || len(used) != 0 {
				t.Errorf("credential.used entries %v (%v), want none", used, err)
			}
		})
	}
}

// A credential whose agent list names the run's agent for the run's factory reaches the agent's
// env as its CredentialEnvName, and no other exec's. Another agent, or another factory, gets
// nothing; a variable a connection of the run sets refuses the run.
func TestRunEnv_GivesListedAgentItsCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cred domain.NewCredential
		// want is the agent's value of the credential's variable, "" for none.
		want string
		// blocked, when set, is what the reason the run is refused with holds.
		blocked []string
	}{
		{
			name: "a listed agent", want: deployKeyValue,
			cred: domain.NewCredential{
				Name: credDeploy, Scope: domain.CredentialScope{All: true}, Value: deployKeyValue,
				Agents: map[domain.FactoryID][]string{factoryID: {agWorker}},
			},
		},
		{
			name: "an agent that is not listed",
			cred: domain.NewCredential{
				Name: credDeploy, Scope: domain.CredentialScope{All: true}, Value: deployKeyValue,
				Agents: map[domain.FactoryID][]string{factoryID: {agReviewer}},
			},
		},
		{
			name: "a factory outside its scope",
			cred: domain.NewCredential{
				Name: credDeploy, Scope: domain.CredentialScope{Factories: []domain.FactoryID{otherFactory}}, Value: deployKeyValue,
				Agents: map[domain.FactoryID][]string{otherFactory: {agWorker}},
			},
		},
		{
			name:    "a variable a connection sets",
			blocked: []string{"credential tracker-token", "connection " + connTracker, envTrackerToken},
			cred: domain.NewCredential{
				Name: "tracker-token", Scope: domain.CredentialScope{All: true}, Value: deployKeyValue,
				Agents: map[domain.FactoryID][]string{factoryID: {agWorker}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := connLine()
			line.Stations[1].Checks = []string{makeTest}
			h := newHarness(t, line, porttest.Succeed(`{}`))
			h.addCredential(t, tt.cred)
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if tt.blocked != nil {
				h.expectRefused(t, it.ID, tt.blocked, deployKeyValue, trackerToken)
				return
			}

			prepares, runs, checks := h.worker.Prepares(), h.worker.Runs(), h.worker.Checks()
			if len(prepares) != 1 || len(runs) != 1 || len(checks) != 1 {
				t.Fatalf("%d prepares, %d agent runs and %d checks; want one of each", len(prepares), len(runs), len(checks))
			}

			if got := runs[0].Env[envDeployKey]; got != tt.want {
				t.Errorf("agent's %s is %q, want %q", envDeployKey, got, tt.want)
			}

			for name, env := range map[string]map[string]string{"prepare": prepares[0].Env, "check": checks[0].Env} {
				if _, ok := env[envDeployKey]; ok {
					t.Errorf("%s got %s", name, envDeployKey)
				}
			}
		})
	}
}

// Every value a run opens, through a connection's reference or for its agent directly, is
// scrubbed from what the run reports: its events, its error and goald's logs.
func TestRunEnv_ScrubsCredentialValues(t *testing.T) {
	t.Parallel()

	h := newHarness(t, refLine(), func(_ context.Context, run porttest.ExecCall, stdout io.Writer) domain.RunResult {
		_, _ = io.WriteString(stdout, `{"type":"message","at":"2026-10-23T09:00:02Z","text":"keys `+
			run.Env[envTrackerToken]+` and `+run.Env[envDeployKey]+`"}`+"\n")

		return domain.RunResult{Status: domain.RunFailed, Error: "gave up: " + run.Env[envTrackerToken] + " and " + run.Env[envDeployKey]}
	})
	h.addCredential(t, trackerKey(factoryID))
	h.addCredential(t, domain.NewCredential{
		Name: credDeploy, Scope: domain.CredentialScope{All: true}, Value: deployKeyValue,
		Agents: map[domain.FactoryID][]string{factoryID: {agWorker}},
	})
	h.addRefConnection(t)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	runs := h.runs(t, testOrg, it.ID)
	if len(runs) != 1 {
		t.Fatalf("%d runs, want 1", len(runs))
	}

	if got := runs[0].Result.Error; got != "gave up: [redacted] and [redacted]" {
		t.Errorf("run error %q, want both credential values redacted", got)
	}

	reported := []string{h.logs.String()}
	for _, e := range h.store.Events(it.ID) {
		reported = append(reported, mustJSON(t, e.Data))
	}

	for _, r := range reported {
		for _, secret := range []string{trackerKeyValue, deployKeyValue} {
			if strings.Contains(r, secret) {
				t.Errorf("a credential's value reached what the run reports:\n%s", r)
			}
		}
	}
}

// expectRefused fails the test unless item id is blocked before its agent ran, with a reason that
// mentions each of want and holds none of secrets.
func (h *harness) expectRefused(t *testing.T, id domain.ItemID, want []string, secrets ...string) {
	t.Helper()

	blocked := h.eventsOf(id, domain.EventBlocked)
	if len(blocked) == 0 {
		t.Fatalf("item not blocked (events %v)", h.kinds(id))
	}

	reason, _ := blocked[0].Data["reason"].(string)
	for _, w := range want {
		if !strings.Contains(reason, w) {
			t.Errorf("blocked reason %q does not mention %q", reason, w)
		}
	}

	for _, secret := range secrets {
		if strings.Contains(reason, secret) {
			t.Errorf("blocked reason %q holds a secret value", reason)
		}
	}

	if n := len(h.worker.Runs()); n != 0 {
		t.Errorf("%d agent runs, want none", n)
	}
}
