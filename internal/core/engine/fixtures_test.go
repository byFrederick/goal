package engine_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Names a line or a run request gives in the forms connections had, which no run reads (ADR 0037
// decision 19), and secret values no run may show; modelKey is the value of the model key every
// organization of the harness holds (lineOf), codeToken the one of runtime.repository.token.
const (
	connModel   = "claude"
	connGit     = "code"
	connTracker = "tracker"
	connDocs    = "docs"
	connOther   = "tracker-two"
	connGHE     = "ghe"

	modelKey     = "model-key-for-tests-0123"
	codeToken    = "code-token-for-tests-4567"
	trackerToken = "tracker-token-for-tests-89"
	otherToken   = "other-token-for-tests-1011"
	// mintAWS is the minter the harness's engines have (porttest.Minter).
	mintAWS = "aws-sts"

	// envModelKey and envTrackerToken are variables no run on secrets gets unless its
	// environment file sets them.
	envModelKey     = "MODEL_KEY"
	envTrackerToken = "TRACKER_TOKEN"
)

// connLine is repoLine with the connections a version once listed: the worker is given
// connTracker and legacy, a name the factory does not list.
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
