package authoring_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// acmeYAML is a small valid definition: a queue, an agent station, a human station and done,
// on the organization's connections claude and code.
const acmeYAML = `# Acme review line: a fixture for authoring tests.
apiVersion: goal/v1
kind: Factory
metadata:
  id: 0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10
  name: acme-review
  title: Acme review
spec:
  runtime:
    modelConnection: claude
    repository: { url: "https://git.example.com/acme/review.git", connection: code }
  agents:
    # The one engineer.
    - name: engineer
      prompt: ./agents/engineer.md
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: build, kind: agent, agent: engineer, wip: 1 }
      - { id: review, kind: human }
      - { id: done, kind: done }
`

const enginePrompt = "You build what the item asks for.\n"

// The platform's GitHub kind, and the host of the acme repository the connection code signs in to.
const (
	kindGitHub = "github"
	gitHost    = "git.example.com"
)

// Paths of the fixture's files.
const (
	yamlFile   = "factory.yaml"
	promptFile = "agents/engineer.md"
)

func acmeFiles(yaml string) map[string][]byte {
	return map[string][]byte{yamlFile: []byte(yaml), promptFile: []byte(enginePrompt)}
}

type harness struct {
	svc     *authoring.Service
	store   *porttest.Definitions
	items   *porttest.Store
	objects *porttest.ObjectStore
	clock   *porttest.Clock
	conns   *porttest.Connections
	ctx     context.Context //nolint:containedctx // the test's request context, built once per harness
}

type option func(*authoring.Deps)

func withChecks(c ...authoring.Check) option { return func(d *authoring.Deps) { d.Checks = c } }

func withStore(wrap func(ports.DefinitionStore) ports.DefinitionStore) option {
	return func(d *authoring.Deps) { d.Store = wrap(d.Store) }
}

// withConnectionsCheck changes the harness's connections check.
func withConnectionsCheck(change func(*authoring.ConnectionsCheck)) option {
	return func(d *authoring.Deps) {
		if c, ok := d.Credentials.(authoring.ConnectionsCheck); ok {
			change(&c)
			d.Credentials = c
		}
	}
}

// withKnowledge gives the organization knowledge sources.
func withKnowledge(k authoring.KnowledgeSources) option {
	return withConnectionsCheck(func(c *authoring.ConnectionsCheck) { c.Knowledge = k })
}

// newHarness is authoring in one organization with the platform's connection kinds and the
// connections claude (a model), code (git on git.example.com), github (git on github.com) and
// docs (an MCP server), whose definitions may still hold the env: references of the interim
// rule.
func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()

	tenancy := porttest.NewTenancy(time.Now)

	o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	h := &harness{
		items:   porttest.NewStore(),
		objects: porttest.NewObjectStore(),
		clock:   porttest.NewClock(time.Date(2026, 10, 9, 14, 32, 0, 0, time.UTC)),
		conns:   porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy),
		ctx:     domain.WithActor(domain.WithOrg(t.Context(), o.ID), domain.LocalUser),
	}
	h.store = porttest.NewDefinitions(porttest.DefinitionsDeps{Objects: h.objects, Items: h.items, Now: h.clock.Now})

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	if err := h.conns.SyncPlatformKinds(h.ctx, kinds); err != nil {
		t.Fatal(err)
	}

	for _, c := range []domain.NewConnection{
		{Name: "claude", Kind: "anthropic", Secrets: map[string]string{"key": "sk-ant-secret"}},
		{Name: "code", Kind: kindGitHub, Fields: map[string]string{"host": gitHost, "api_url": "https://" + gitHost + "/api/v3"}, Secrets: map[string]string{"token": "a-token"}},
		{Name: kindGitHub, Kind: kindGitHub, Secrets: map[string]string{"token": "a-token"}},
		{Name: "docs", Kind: "mcp-server", Fields: map[string]string{"url": "https://docs.example.com/mcp"}},
	} {
		h.connection(t, c)
	}

	var ids atomic.Int64

	d := authoring.Deps{
		Store: h.store, Loads: h.items, Templates: h.objects, Clock: h.clock,
		Credentials: authoring.ConnectionsCheck{Connections: h.conns},
		NewID:       func() string { return fmt.Sprintf("0b6f1c9e-0000-7000-8000-%012d", ids.Add(1)) },
	}
	for _, o := range opts {
		o(&d)
	}

	h.svc = authoring.New(d)

	return h
}

// imported creates a factory from files, keeping their metadata.id, and returns its id.
func (h *harness) imported(t *testing.T, files map[string][]byte) domain.FactoryID {
	t.Helper()

	c, err := h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{
		Start: authoring.Start{Kind: authoring.StartImport, Files: files, KeepID: true},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	return c.Factory.ID
}

// connection creates one of the organization's connections.
func (h *harness) connection(t *testing.T, c domain.NewConnection) {
	t.Helper()

	if _, err := h.conns.Create(h.ctx, c, domain.LocalUser); err != nil {
		t.Fatalf("create connection %s: %v", c.Name, err)
	}
}

// published creates the acme factory and publishes it as v1.
func (h *harness) published(t *testing.T) domain.FactoryID {
	t.Helper()

	f := h.imported(t, acmeFiles(acmeYAML))
	h.publish(t, f)

	return f
}

// publish publishes the open draft at its revision against the current version.
func (h *harness) publish(t *testing.T, f domain.FactoryID) domain.DefinitionVersion {
	t.Helper()

	v, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	return v
}

func (h *harness) publishRequest(t *testing.T, f domain.FactoryID) authoring.PublishRequest {
	t.Helper()

	d, err := h.svc.Draft(h.ctx, f)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}

	r := authoring.PublishRequest{Revision: d.Draft.Revision}
	if d.Current != nil {
		r.ExpectedCurrent = d.Current.ID
	}

	return r
}

// edit opens a draft on the current version and replaces its factory.yaml.
func (h *harness) edit(t *testing.T, f domain.FactoryID, yaml string) {
	t.Helper()

	if _, _, err := h.svc.OpenDraft(h.ctx, f, authoring.OpenDraftRequest{}); err != nil {
		t.Fatalf("open draft: %v", err)
	}

	if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: yamlFile, Content: []byte(yaml)}); err != nil {
		t.Fatalf("put factory.yaml: %v", err)
	}
}

// draftFile reads a file of the open draft.
func (h *harness) draftFile(t *testing.T, f domain.FactoryID, path string) string {
	t.Helper()

	file, err := h.svc.VersionFile(h.ctx, f, authoring.VersionSelector{Kind: authoring.SelectDraft}, path)
	if err != nil {
		t.Fatalf("read draft %s: %v", path, err)
	}

	return string(file.Content)
}

func codes(problems []domain.DefinitionProblem) []string {
	out := make([]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.Code)
	}

	return out
}

func problem(problems []domain.DefinitionProblem, code string) (domain.DefinitionProblem, bool) {
	for _, p := range problems {
		if p.Code == code {
			return p, true
		}
	}

	return domain.DefinitionProblem{}, false
}

func TestProblemCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("x: %w", authoring.ErrNoDraft), authoring.CodeNoDraft},
		{&authoring.SectionConflictError{Section: authoring.Line}, authoring.CodeDraftConflict},
		{&authoring.InvalidError{}, authoring.CodeDefinitionInvalid},
		{&ports.ItemsAtRemovedStationError{}, authoring.CodeItemsAtRemoved},
		{fmt.Errorf("x: %w", ports.ErrBaseMoved), authoring.CodeBaseMoved},
		{fmt.Errorf("x: %w", &authoring.ParseError{Line: 3}), authoring.CodeYAMLInvalid},
		{authoring.ErrModelInvalid, authoring.CodeModelInvalid},
		{errors.New("disk on fire"), ""},
	}

	for _, c := range cases {
		if got := authoring.ProblemCode(c.err); got != c.want {
			t.Errorf("ProblemCode(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestFactory_ByIDOrName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))

	for _, ref := range []string{string(f), "ACME-review"} {
		rec, err := h.svc.Factory(h.ctx, ref)
		if err != nil || rec.ID != f {
			t.Errorf("Factory(%q) = %v, %v; want %s", ref, rec.ID, err, f)
		}
	}

	if _, err := h.svc.Factory(h.ctx, "nope"); !errors.Is(err, authoring.ErrFactoryNotFound) {
		t.Errorf("Factory(nope) = %v, want ErrFactoryNotFound", err)
	}

	other := domain.WithOrg(h.ctx, "0b6f1c9e-0000-4000-8000-00000000b0b0")
	if _, err := h.svc.Factory(other, string(f)); !errors.Is(err, authoring.ErrFactoryNotFound) {
		t.Errorf("another organization found the factory: %v", err)
	}
}

func TestCheckPath(t *testing.T) {
	t.Parallel()

	for _, p := range []string{yamlFile, promptFile, "skills/acme-conventions/SKILL.md"} {
		if err := authoring.CheckPath(p); err != nil {
			t.Errorf("CheckPath(%q) = %v", p, err)
		}
	}

	for _, p := range []string{"", "/abs", "a//b", "../up", "agents/.hidden", "knowledge/notes.md", "a b", strings.Repeat("a", 256)} {
		if err := authoring.CheckPath(p); !errors.Is(err, authoring.ErrBadPath) {
			t.Errorf("CheckPath(%q) = %v, want ErrBadPath", p, err)
		}
	}
}
