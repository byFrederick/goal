package engine_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The test organization's connections, which merges of a version on connections still open
// (software#789), and the secret values they hold; modelKey is the value of the model key every
// organization of the harness holds too (lineOf).
const (
	connModel   = "claude"
	connGit     = "code"
	connTracker = "tracker"
	connDocs    = "docs"
	connOther   = "tracker-two"

	modelKey     = "model-key-for-tests-0123"
	codeToken    = "code-token-for-tests-4567"
	trackerToken = "tracker-token-for-tests-89"
	otherToken   = "other-token-for-tests-1011"
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
	line.Connections = []string{connTracker, connOther, connDocs}
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

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}
