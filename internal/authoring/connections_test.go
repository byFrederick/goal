package authoring_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// awsSecret is a secret value the connections check must never repeat.
const awsSecret = "aws-secret-value"

// sources is the organization's knowledge sources.
type sources []string

func (s sources) SourceNames(context.Context) ([]string, error) { return s, nil }

// withAWS gives the harness's organization two AWS connections, which set the same variables.
func withAWS(t *testing.T, h *harness) {
	t.Helper()

	for _, name := range []string{"aws-dev", "aws-prod"} {
		h.connection(t, domain.NewConnection{
			Name: name, Kind: "aws", Secrets: map[string]string{"access_key_id": "AKIA" + name, "secret_access_key": awsSecret},
		})
	}
}

// Each connections problem names what the definition asks for, is an error located for both
// editors, and comes with the fixes the organization's connections allow.
func TestConnectionsCheck_ReportsEachProblemWithItsFixes(t *testing.T) {
	t.Parallel()

	const agentsKey = "  agents:\n    # The one engineer.\n"

	tests := []struct {
		name     string
		edits    []string // old, new pairs on acmeYAML
		aws      bool
		code     string
		pointer  string
		fixes    []string
		inPlaces []string // the message names these
	}{
		{
			name: "an unknown connection", edits: []string{agentsKey, "  connections: [docs, ghost]\n" + agentsKey},
			code: "connection-unknown", pointer: "/connections/1", fixes: []string{"Remove ghost"}, inPlaces: []string{"ghost"},
		},
		{
			name: "an unknown model connection", edits: []string{"modelConnection: claude", "modelConnection: ghost"},
			code: "connection-unknown", pointer: "/basics/model_connection", fixes: []string{"Use claude"}, inPlaces: []string{"ghost"},
		},
		{
			name: "a model connection without model access", edits: []string{"modelConnection: claude", "modelConnection: code"},
			code: "connection-not-model", pointer: "/basics/model_connection", fixes: []string{"Use claude"},
			inPlaces: []string{"code is a GitHub"},
		},
		{
			name: "an unknown repository connection", edits: []string{"connection: code }", "connection: ghost }"},
			code: "connection-unknown", pointer: "/basics/repository/connection", fixes: []string{"Use code", "Clone without a connection"},
		},
		{
			name: "a repository on another host", edits: []string{"connection: code }", "connection: github }"},
			code: "repository-host-mismatch", pointer: "/basics/repository/connection", fixes: []string{"Use code", "Clone without a connection"},
			inPlaces: []string{"github.com", gitHost},
		},
		{
			name: "a connection without a git login", edits: []string{"connection: code }", "connection: docs }"},
			code: "repository-host-mismatch", pointer: "/basics/repository/connection", fixes: []string{"Use code", "Clone without a connection"},
		},
		{
			name: "two connections set the same variable", aws: true,
			edits: []string{
				agentsKey, "  connections: [aws-dev, aws-prod]\n" + agentsKey,
				"      prompt: ./agents/engineer.md\n", "      prompt: ./agents/engineer.md\n      connections: [aws-dev, aws-prod]\n",
			},
			code: "connection-env-conflict", pointer: "/agents/agents/0/connections/1", fixes: []string{"Keep aws-dev", "Keep aws-prod"},
			inPlaces: []string{"engineer", "aws-dev", "aws-prod", "AWS_ACCESS_KEY_ID"},
		},
		{
			name: "an unknown knowledge source", edits: []string{agentsKey, "  knowledge: { sources: [handbook, ghost] }\n" + agentsKey},
			code: "knowledge-source-unknown", pointer: "/basics/knowledge_sources/1", fixes: []string{"Remove ghost"}, inPlaces: []string{"ghost"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, withKnowledge(sources{"handbook"}))
			if tt.aws {
				withAWS(t, h)
			}

			doc := acmeYAML
			for i := 0; i+1 < len(tt.edits); i += 2 {
				doc = strings.Replace(doc, tt.edits[i], tt.edits[i+1], 1)
			}

			v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(doc))
			if err != nil {
				t.Fatal(err)
			}

			p, ok := problem(v.Problems, tt.code)
			if !ok || v.OK {
				t.Fatalf("validation = ok %v, problems %v; want %s", v.OK, codes(v.Problems), tt.code)
			}

			if p.Severity != domain.SeverityError || p.Check != authoring.CheckConnections || p.Pointer != tt.pointer || p.Line == 0 {
				t.Errorf("problem %+v, want an error of the connections check at %s", p, tt.pointer)
			}

			if labels := fixLabels(p); !reflect.DeepEqual(labels, tt.fixes) {
				t.Errorf("fixes %q, want %q", labels, tt.fixes)
			}

			assertNamesNoValue(t, p.Message, tt.inPlaces)
		})
	}
}

func fixLabels(p domain.DefinitionProblem) []string {
	labels := make([]string, 0, len(p.Fixes))
	for _, f := range p.Fixes {
		labels = append(labels, f.Label)
	}

	return labels
}

// assertNamesNoValue requires a message to name each of names and no secret value.
func assertNamesNoValue(t *testing.T, msg string, names []string) {
	t.Helper()

	for _, s := range names {
		if !strings.Contains(msg, s) {
			t.Errorf("message %q does not name %s", msg, s)
		}
	}

	if strings.Contains(msg, awsSecret) || strings.Contains(msg, "AKIA") {
		t.Errorf("message leaks a value: %s", msg)
	}
}

// A fix is a patch on the section the problem is in: keeping one of two connections takes the
// other out of the agent's list; a model connection is replaced by one with model access.
func TestConnectionsCheck_FixesPatchTheirSection(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	withAWS(t, h)

	doc := strings.Replace(acmeYAML, "modelConnection: claude", "modelConnection: code", 1)
	doc = strings.Replace(doc, "  agents:\n", "  connections: [aws-dev, aws-prod]\n  agents:\n", 1)
	doc = strings.Replace(doc, "      prompt: ./agents/engineer.md\n", "      prompt: ./agents/engineer.md\n      connections: [aws-dev, aws-prod]\n", 1)

	v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(doc))
	if err != nil {
		t.Fatal(err)
	}

	model, _ := problem(v.Problems, "connection-not-model")
	conflict, _ := problem(v.Problems, "connection-env-conflict")

	want := []domain.ProblemFix{
		{Label: "Use claude", Section: domain.SectionBasics, Patch: []domain.PatchOp{{Op: "replace", Path: "/model_connection", Value: json.RawMessage(`"claude"`)}}},
		{Label: "Keep aws-dev", Section: domain.SectionAgents, Patch: []domain.PatchOp{{Op: "remove", Path: "/agents/0/connections/1"}}},
		{Label: "Keep aws-prod", Section: domain.SectionAgents, Patch: []domain.PatchOp{{Op: "remove", Path: "/agents/0/connections/0"}}},
	}

	if got := slices.Concat(model.Fixes, conflict.Fixes); !reflect.DeepEqual(got, want) {
		t.Errorf("fixes =\n%+v\nwant\n%+v", got, want)
	}
}

// Before the knowledge base ships an organization has no knowledge source, so a definition that
// names one cannot be published.
func TestConnectionsCheck_NoKnowledgeSourceBeforeTheKnowledgeBase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(strings.Replace(acmeYAML, "  agents:\n", "  knowledge: { sources: [handbook] }\n  agents:\n", 1)))
	if err != nil {
		t.Fatal(err)
	}

	if p, ok := problem(v.Problems, "knowledge-source-unknown"); !ok || p.Pointer != "/basics/knowledge_sources/0" || v.OK {
		t.Errorf("problems %v, want handbook unknown", codes(v.Problems))
	}
}

// When the connections port fails, so does the validation; it is not taken as no connection.
func TestConnectionsCheck_FailsWhenConnectionsFail(t *testing.T) {
	t.Parallel()

	h := newHarness(t, withConnectionsCheck(func(c *authoring.ConnectionsCheck) {
		c.Connections = failingConnections{c.Connections}
	}))

	if _, err := h.svc.ValidateFiles(h.ctx, acmeFiles(acmeYAML)); err == nil {
		t.Error("a failing Connections port was taken as an organization without connections")
	}
}

type failingConnections struct{ ports.Connections }

func (failingConnections) List(context.Context) ([]domain.Connection, error) {
	return nil, errors.New("database unreachable")
}
