package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// The test organization's connections and the secret values they hold.
const (
	connModel   = "claude"
	connGit     = "code"
	connTracker = "tracker"
	connCloud   = "cloud"
	connDocs    = "docs"
	connOther   = "tracker-two"

	modelKey     = "model-key-for-tests-0123"
	codeToken    = "code-token-for-tests-4567"
	trackerToken = "tracker-token-for-tests-89"
	otherToken   = "other-token-for-tests-1011"
	cloudKey     = "cloud-key-for-tests-1213"
	cloudSecret  = "cloud-secret-for-tests-1415"
	leasedKey    = "leased-key-for-tests-1617"
	connLeased   = "leased"
	cloudRegion  = "eu-west-1"
	// mintAWS is the minter the harness's engines have (porttest.Minter); a kind's other mint names
	// one they lack.
	mintAWS = "aws-sts"

	// envModelKey is the variable the model connection sets; fieldToken and fieldHost are fields of
	// the test kinds.
	envModelKey = "MODEL_KEY"
	fieldToken  = "token"
	fieldHost   = "host"

	envCodeToken    = "CODE_TOKEN"
	envTrackerToken = "TRACKER_TOKEN"
	envCloudKeyID   = "AWS_ACCESS_KEY_ID"
	envCloudSession = "AWS_SESSION_TOKEN"

	// Connections whose kind names a code host goald lacks (connBitbucket), a code host on
	// another host than the pull requests' (connGHE) and none at all (connPlain), all but connGHE
	// for github.com.
	connPlain     = "plain"
	connBitbucket = "bitbucket"
	connGHE       = "ghe"
)

// secretField is a required secret field of a test kind.
func secretField(name string) domain.KindField {
	return domain.KindField{Name: name, Title: name, Secret: true, Required: true}
}

// codeHostKind is a kind of git over https to host (by default) whose pull requests the code host
// codeHost merges.
func codeHostKind(name, codeHost, host string) domain.ConnectionKind {
	return domain.ConnectionKind{Name: name, Title: name, Platform: true, Spec: domain.KindSpec{
		Fields:   []domain.KindField{secretField(fieldToken), {Name: fieldHost, Title: "Host", Default: host}},
		Env:      map[string]string{"HOST_TOKEN": "{{token}}"},
		Git:      &domain.KindGit{Host: "{{host}}", Username: "x-access-token", PasswordEnv: "HOST_TOKEN"},
		CodeHost: codeHost,
	}}
}

// testKinds are the platform kinds of the tests' connections: a model's access, git over https
// with an API token, a tracker's API and remote MCP server, a minted cloud login, a docs server
// without credentials started in the box, and the code hosts merges reach.
func testKinds() []domain.ConnectionKind {
	secret := secretField

	return []domain.ConnectionKind{
		codeHostKind("github-host", codeHostGitHub, "github.com"),
		codeHostKind("gitlab-host", codeHostGitLab, "gitlab.com"),
		codeHostKind("bitbucket-host", "bitbucket", "github.com"),
		{Name: "model-key", Title: "Model key", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{secret("key")}, Env: map[string]string{envModelKey: "{{key}}"}, Model: true,
		}},
		{Name: "code-host", Title: "Code host", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{secret(fieldToken), {Name: fieldHost, Title: "Host", Default: gitHost}},
			Env:    map[string]string{envCodeToken: "{{token}}", "CODE_HOST": "{{host}}"},
			Git:    &domain.KindGit{Host: "{{host}}", Username: "x-access-token", PasswordEnv: envCodeToken},
		}},
		{Name: "tracker", Title: "Tracker", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{secret(fieldToken)},
			Env:    map[string]string{envTrackerToken: "{{token}}"},
			MCP:    &domain.KindMCP{URL: "https://mcp.tracker.example.com/", Headers: map[string]string{"Authorization": "Bearer ${TRACKER_TOKEN}"}},
			Docs:   "File issues with the tracker's tools.",
		}},
		{Name: "cloud", Title: "Cloud", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{secret("access_key_id"), secret("secret_access_key"), {Name: "region", Title: "Region", Default: cloudRegion}},
			Mint:   mintAWS,
			Env: map[string]string{
				envCloudKeyID: "{{minted.access_key_id}}", "AWS_SECRET_ACCESS_KEY": "{{minted.secret_access_key}}",
				envCloudSession: "{{minted.session_token}}", "AWS_REGION": "{{region}}",
			},
		}},
		{Name: "leased", Title: "Leased", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{secret("key")}, Mint: "lease", Env: map[string]string{"LEASE_TOKEN": "{{minted.token}}"}, //nolint:gosec // a template, not a credential
		}},
		{Name: "docs-server", Title: "Docs server", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{{Name: "command", Title: "Command"}}, MCP: &domain.KindMCP{Command: []string{"sh", "-c", "{{command}}"}},
		}},
	}
}

// newConnections holds the test organization's connections of testKinds, audited through audit.
func newConnections(t *testing.T, audit ports.Audit) *porttest.Connections {
	t.Helper()

	c := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), audit)
	if err := c.SyncPlatformKinds(t.Context(), testKinds()); err != nil {
		t.Fatal(err)
	}

	for _, n := range []domain.NewConnection{
		{Name: connModel, Kind: "model-key", Secrets: map[string]string{"key": modelKey}},
		{Name: connGit, Kind: "code-host", Secrets: map[string]string{fieldToken: codeToken}},
		{Name: connTracker, Kind: "tracker", Secrets: map[string]string{fieldToken: trackerToken}},
		{Name: connOther, Kind: "tracker", Secrets: map[string]string{fieldToken: otherToken}},
		{Name: connCloud, Kind: "cloud", Secrets: map[string]string{"access_key_id": cloudKey, "secret_access_key": cloudSecret}},
		{Name: connLeased, Kind: "leased", Secrets: map[string]string{"key": leasedKey}},
		{Name: connDocs, Kind: "docs-server", Fields: map[string]string{"command": "npx -y @acme/docs-mcp@1.0.0"}},
		// The host as people write it: matched without case.
		{Name: connGitHub, Kind: "github-host", Fields: map[string]string{fieldHost: "GitHub.com"}, Secrets: map[string]string{fieldToken: "github-token"}},
		{Name: connGitLab, Kind: "gitlab-host", Secrets: map[string]string{fieldToken: "gitlab-token"}},
		{Name: connPlain, Kind: "code-host", Fields: map[string]string{fieldHost: "github.com"}, Secrets: map[string]string{fieldToken: "plain-token"}},
		{Name: connBitbucket, Kind: "bitbucket-host", Secrets: map[string]string{fieldToken: "bitbucket-token"}},
		{Name: connGHE, Kind: "github-host", Fields: map[string]string{fieldHost: "ghe.example.com"}, Secrets: map[string]string{fieldToken: "ghe-token"}},
	} {
		if _, err := c.Create(inOrg(t), n, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	return c
}

// connLine is repoLine with more connections: the worker is given connTracker and legacy, a
// name the factory does not list, which it does not get.
func connLine() domain.Line {
	line := repoLine()
	line.Connections = []string{connTracker, connOther, connCloud, connLeased, connDocs}
	line.Agents = agentConnections(line.Agents, agWorker, connTracker, "legacy")

	return line
}

// agentConnections gives agent name the connections names.
func agentConnections(agents []domain.AgentSpec, name string, names ...string) []domain.AgentSpec {
	agents = slices.Clone(agents)

	for i := range agents {
		if agents[i].Name == name {
			agents[i].Connections = names
		}
	}

	return agents
}

// Each exec gets the connections it needs and no other: prepare the repository connection's git
// password, the agent the model's access and its own connections' env, a check nothing.
func TestRun_ConnectionsReachOnlyTheExecsThatNeedThem(t *testing.T) {
	t.Parallel()

	line := connLine()
	line.Stations[1].Checks = []string{makeTest}
	h := newHarness(t, line, porttest.Succeed(`{}`))
	h.submit(t, domain.NewItem{})

	h.step(t)

	prepares, runs, checks := h.worker.Prepares(), h.worker.Runs(), h.worker.Checks()
	if len(prepares) != 1 || len(runs) != 1 || len(checks) != 1 {
		t.Fatalf("%d prepares, %d agent runs and %d checks; want one of each", len(prepares), len(runs), len(checks))
	}

	tests := []struct {
		name string
		env  map[string]string
		want map[string]string
	}{
		{name: "prepare", env: prepares[0].Env, want: map[string]string{envCodeToken: codeToken}},
		{name: "agent", env: runs[0].Env, want: map[string]string{envModelKey: modelKey, envTrackerToken: trackerToken}},
		{name: "check", env: checks[0].Env, want: map[string]string{}},
	}

	for _, tt := range tests {
		got := map[string]string{}

		for _, name := range []string{envCodeToken, "CODE_HOST", envModelKey, envTrackerToken, envCloudKeyID} {
			if v, ok := tt.env[name]; ok {
				got[name] = v
			}
		}

		if !maps.Equal(got, tt.want) {
			t.Errorf("%s got the connections' %v; want exactly %v", tt.name, keys(got), keys(tt.want))
		}
	}
}

// Every run that gets a connection audits its use with the run's id, a retry's too: the model,
// the repository and the agent's own connections, never one the agent does not name.
func TestRun_ConnectionsAreAuditedForEveryRun(t *testing.T) {
	t.Parallel()

	work := agentStation(stWork, agWorker, 1, 1)
	work.Output = contractResult
	line := connLine()
	line.Stations[1] = work
	h := newHarness(t, line, porttest.Sequence(ok(`{}`), ok(`{"answer":1}`)))
	h.contracts.Require(v1, contractResult, "answer")
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditConnectionUsed, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	got := map[domain.RunID][]string{}

	for _, e := range entries {
		run, _ := e.Data["run_id"].(string)
		name, _ := e.Data["name"].(string)
		got[domain.RunID(run)] = append(got[domain.RunID(run)], name)
	}

	runs := h.runs(t, testOrg, it.ID)
	if len(runs) != 2 || len(got) != 2 {
		t.Fatalf("connection.used for runs %v; want both of the item's %d runs", got, len(runs))
	}

	for _, r := range runs {
		if names := slices.Sorted(slices.Values(got[r.ID])); !slices.Equal(names, []string{connModel, connGit, connTracker}) {
			t.Errorf("run %s used %v; want %v", r.ID, names, []string{connModel, connGit, connTracker})
		}
	}

	if logs := h.logs.String(); !strings.Contains(logs, `"msg":"connection used"`) {
		t.Errorf("no connection used line in the logs:\n%s", logs)
	}
}

// A version that names connections the run cannot be given blocks the item with the reason and
// starts no sandbox.
func TestRun_ConnectionsThatCannotBeGivenBlockTheRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*domain.Line)
		want   string
		// mintErr, when set, fails every mint.
		mintErr error
	}{
		{
			name: "an unknown connection", want: "gone",
			change: func(l *domain.Line) {
				l.Connections = append(l.Connections, "gone")
				l.Agents = agentConnections(l.Agents, agWorker, connTracker, "gone")
			},
		},
		{
			name: "a model connection without model access", want: "model",
			change: func(l *domain.Line) { l.Runtime.ModelConnection = connTracker },
		},
		{
			name: "a repository connection without a git login", want: "git",
			change: func(l *domain.Line) { l.Runtime.Repository.Connection = connTracker },
		},
		{
			name: "two connections setting one variable", want: envTrackerToken,
			change: func(l *domain.Line) { l.Agents = agentConnections(l.Agents, agWorker, connTracker, connOther) },
		},
		{
			name: "a kind whose minter this goald lacks", want: `connection leased: its kind leased mints with "lease"`,
			change: func(l *domain.Line) { l.Agents = agentConnections(l.Agents, agWorker, connLeased) },
		},
		{
			name: "a mint that fails", want: "connection cloud: mint: AccessDenied for [redacted]",
			change:  func(l *domain.Line) { l.Agents = agentConnections(l.Agents, agWorker, connCloud) },
			mintErr: errors.New("AccessDenied for " + cloudKey),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := connLine()
			tt.change(&line)
			h := newHarness(t, line, porttest.Succeed(`{}`))
			h.minter.Fail(tt.mintErr)
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != domain.StateBlocked {
				t.Fatalf("item is %s, want blocked (events %v)", got.State, h.kinds(it.ID))
			}

			reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string)
			if !strings.Contains(reason, tt.want) {
				t.Errorf("blocked reason %q does not mention %q", reason, tt.want)
			}

			for _, secret := range []string{modelKey, codeToken, trackerToken, otherToken, cloudKey, cloudSecret, leasedKey} {
				if strings.Contains(reason, secret) {
					t.Errorf("blocked reason %q holds a connection's secret", reason)
				}
			}

			if n := len(h.worker.Prepares()); n != 0 {
				t.Errorf("%d prepares, want none", n)
			}

			h.noSandboxes(t)
		})
	}
}

// Threat row 9 (spec 4 §11.2): what a run reports, its events, its error and goald's logs, holds
// none of its connections' secret values, however the agent or prepare echoes them.
func TestEngine_RelayScrubsConnectionValues(t *testing.T) {
	t.Parallel()

	h := newHarness(t, connLine(), func(_ context.Context, run porttest.ExecCall, stdout io.Writer) domain.RunResult {
		_, _ = io.WriteString(stdout, `{"type":"tool_use","at":"2026-10-23T09:00:01Z","id":"t1","tool":"Bash","input":"curl -H 'Authorization: Bearer `+
			run.Env[envTrackerToken]+`'"}`+"\n")
		_, _ = io.WriteString(stdout, `{"type":"message","at":"2026-10-23T09:00:02Z","text":"the model key is `+run.Env[envModelKey]+`"}`+"\n")

		return domain.RunResult{Status: domain.RunFailed, Error: "gave up: " + run.Env[envTrackerToken] + " was refused"}
	})
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	var reported []string

	for _, e := range h.store.Events(it.ID) {
		reported = append(reported, mustJSON(t, e.Data))
	}

	runs := h.runs(t, testOrg, it.ID)
	if len(runs) != 1 {
		t.Fatalf("%d runs, want 1", len(runs))
	}

	reported = append(reported, runs[0].Result.Error, h.logs.String())

	for _, r := range reported {
		for _, secret := range []string{modelKey, codeToken, trackerToken} {
			if strings.Contains(r, secret) {
				t.Errorf("a connection's secret value reached what the run reports:\n%s", r)
			}
		}
	}

	if !strings.Contains(runs[0].Result.Error, "[redacted]") {
		t.Errorf("run error %q, want the tracker token redacted", runs[0].Result.Error)
	}
}

// Threat row 7 (spec 4 §11.2): a connection whose kind mints gets credentials minted for the run,
// from its stored keys with its kind's defaults, living the task timeout and 15 minutes more, in
// the agent's env only. The stored keys reach no sandbox and no exec, and the minted values are
// scrubbed from what the run reports.
func TestEngine_AWSStoredKeysNeverInEnv(t *testing.T) {
	t.Parallel()

	line := connLine()
	line.Stations[1].Checks = []string{makeTest}
	line.Agents = agentConnections(line.Agents, agWorker, connTracker, connCloud)
	h := newHarness(t, line, func(_ context.Context, run porttest.ExecCall, stdout io.Writer) domain.RunResult {
		_, _ = io.WriteString(stdout, `{"type":"message","at":"2026-10-23T09:00:02Z","text":"the session is `+run.Env[envCloudSession]+`"}`+"\n")

		return ok(`{}`)
	})
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	runs := h.runs(t, testOrg, it.ID)
	mints := h.minter.Mints()

	if len(runs) != 1 || len(mints) != 1 {
		t.Fatalf("%d runs and %d mints, want one of each", len(runs), len(mints))
	}

	run := runs[0].ID
	want := porttest.MintCall{
		Values: map[string]string{"access_key_id": cloudKey, "secret_access_key": cloudSecret, "region": cloudRegion},
		Run:    run, TTL: 2*time.Hour + 15*time.Minute,
	}

	if m := mints[0]; !maps.Equal(m.Values, want.Values) || m.Run != want.Run || m.TTL != want.TTL {
		t.Errorf("minted for run %s for %s from %v; want run %s for %s from the stored keys and the region's default",
			m.Run, m.TTL, keys(m.Values), want.Run, want.TTL)
	}

	agent := h.worker.Runs()
	if len(agent) != 1 {
		t.Fatalf("%d agent runs, want 1", len(agent))
	}

	for name, value := range map[string]string{
		envCloudKeyID: porttest.MintedValue("access_key_id", run), "AWS_SECRET_ACCESS_KEY": porttest.MintedValue("secret_access_key", run),
		envCloudSession: porttest.MintedValue("session_token", run), "AWS_REGION": cloudRegion,
	} {
		if got := agent[0].Env[name]; got != value {
			t.Errorf("agent env %s = %q, want %q", name, got, value)
		}
	}

	h.noStoredKeys(t)

	for _, e := range h.store.Events(it.ID) {
		if s := mustJSON(t, e.Data); strings.Contains(s, porttest.MintedValue("session_token", run)) {
			t.Errorf("a minted value reached the item's events: %s", s)
		}
	}
}

// noStoredKeys fails when the cloud connection's stored keys reached an exec or its sandbox, or
// minted credentials an exec but the agent's.
func (h *harness) noStoredKeys(t *testing.T) {
	t.Helper()

	others := slices.Concat(h.worker.Prepares(), h.worker.Checks())

	for _, c := range slices.Concat(others, h.worker.Runs()) {
		printed := fmt.Sprintf("%+v", c) // the sandbox's spec and env, the command and its env
		if strings.Contains(printed, cloudKey) || strings.Contains(printed, cloudSecret) {
			t.Errorf("a stored key of the connection reached exec %v", c.Argv)
		}
	}

	for _, c := range others {
		if _, ok := c.Env[envCloudSession]; ok {
			t.Errorf("exec %v got the minted credentials; only the agent does", c.Argv)
		}
	}
}

// Every run mints its own credentials, a retry's too, named after it.
func TestRun_EachRunMintsItsOwnCredentials(t *testing.T) {
	t.Parallel()

	work := agentStation(stWork, agWorker, 1, 1)
	work.Output = contractResult
	line := connLine()
	line.Stations[1] = work
	line.Agents = agentConnections(line.Agents, agWorker, connCloud)
	h := newHarness(t, line, porttest.Sequence(ok(`{}`), ok(`{"answer":1}`)))
	h.contracts.Require(v1, contractResult, "answer")
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	runs, mints, agent := h.runs(t, testOrg, it.ID), h.minter.Mints(), h.worker.Runs()
	if len(runs) != 2 || len(mints) != 2 || len(agent) != 2 {
		t.Fatalf("%d runs, %d mints and %d agent runs; want two of each", len(runs), len(mints), len(agent))
	}

	for i, r := range runs {
		if mints[i].Run != r.ID {
			t.Errorf("mint %d is for run %s, want %s", i, mints[i].Run, r.ID)
		}

		if got, want := agent[i].Env[envCloudSession], porttest.MintedValue("session_token", r.ID); got != want {
			t.Errorf("agent run %d has session %q, want its run's %q", i, got, want)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// The agent hears whether git can push its work: only with a connection of its own that signs git
// in to the repository's host, since the repository connection clones and goes no further.
func TestRun_AgentHearsWhetherItCanPush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		agents []string
		note   string
	}{
		{name: "a connection to the repository's host", agents: []string{connTracker, connGit}, note: "with your connection `" + connGit + "`"},
		{name: "none", agents: []string{connTracker}, note: "no git credentials"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := connLine()
			line.Connections = append(line.Connections, connGit)
			line.Agents = agentConnections(line.Agents, agWorker, tt.agents...)
			h := newHarness(t, line, porttest.Succeed(`{}`))
			h.submit(t, domain.NewItem{})

			h.step(t)

			if feedback := h.worker.Runs()[0].Env["GOAL_FEEDBACK"]; !strings.Contains(feedback, tt.note) {
				t.Errorf("feedback %q does not say %q", feedback, tt.note)
			}
		})
	}
}

// The repository connection's password that prepare echoes back on a failure is scrubbed from
// the reason the item is blocked with.
func TestRun_PrepareFailureScrubsTheRepositoryConnection(t *testing.T) {
	t.Parallel()

	h := newHarness(t, connLine(), porttest.Succeed(`{}`))
	h.worker.FailPrepare(1, "goal-agent prepare: git clone exited 128 for "+codeToken)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string)
	if !strings.Contains(reason, "git clone exited 128") || strings.Contains(reason, codeToken) {
		t.Errorf("blocked reason %q, want prepare's failure without the token", reason)
	}
}
