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
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// acmeYAML is a small valid definition: a queue, an agent station, a human station and done,
// on the organization's secrets claude-key and code-token.
const acmeYAML = `# Acme review line: a fixture for authoring tests.
apiVersion: goal/v1
kind: Factory
metadata:
  id: 0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10
  name: acme-review
  title: Acme review
spec:
  runtime:
    modelKey: "{{claude-key}}"
    repository: { url: "https://git.example.com/acme/review.git", token: "{{code-token}}", forge: github }
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

// Paths of the fixture's files.
const (
	yamlFile   = "factory.yaml"
	promptFile = "agents/engineer.md"
	envFile    = "environments/default.yaml"
)

// acmeEnvironment is the fixture's default environment.
const acmeEnvironment = "apiVersion: goal/v1\nkind: Environment\nspec:\n  size: medium\n"

func acmeFiles(yaml string) map[string][]byte {
	return map[string][]byte{yamlFile: []byte(yaml), promptFile: []byte(enginePrompt), envFile: []byte(acmeEnvironment)}
}

type harness struct {
	svc     *authoring.Service
	store   *porttest.Definitions
	items   *porttest.Store
	objects *porttest.ObjectStore
	clock   *porttest.Clock
	secrets *porttest.OrgSecrets
	tenancy *porttest.Tenancy
	ctx     context.Context //nolint:containedctx // the test's request context, built once per harness
}

type option func(*authoring.Deps)

func withStore(wrap func(ports.DefinitionStore) ports.DefinitionStore) option {
	return func(d *authoring.Deps) { d.Store = wrap(d.Store) }
}

// withSecretsCheck changes the harness's secrets check.
func withSecretsCheck(change func(*authoring.SecretsCheck)) option {
	return func(d *authoring.Deps) {
		if c, ok := d.Credentials.(authoring.SecretsCheck); ok {
			change(&c)
			d.Credentials = c
		}
	}
}

// withKnowledge gives the organization knowledge sources.
func withKnowledge(k authoring.KnowledgeSources) option {
	return withSecretsCheck(func(c *authoring.SecretsCheck) { c.Knowledge = k })
}

// newHarness is authoring in one organization with the secrets claude-key and code-token, open
// to every factory, and the code host git.example.com, the fixture's.
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
		secrets: porttest.NewOrgSecrets(porttest.NewMasterKeys().Key("k1"), tenancy, nil),
		tenancy: tenancy,
		ctx:     domain.WithActor(domain.WithOrg(t.Context(), o.ID), domain.LocalUser),
	}
	h.store = porttest.NewDefinitions(porttest.DefinitionsDeps{Objects: h.objects, Items: h.items, Now: h.clock.Now})

	for _, name := range []string{"claude-key", "code-token"} {
		h.secret(t, name, domain.SecretScope{All: true})
	}

	if _, err := tenancy.SetCodeHosts(h.ctx, domain.LocalUser, []domain.CodeHost{{Host: "git.example.com", Kind: domain.CodeHostGitHub}}); err != nil {
		t.Fatal(err)
	}

	var ids atomic.Int64

	d := authoring.Deps{
		Store: h.store, Loads: h.items, Templates: h.objects, Clock: h.clock,
		Credentials: authoring.SecretsCheck{Secrets: h.secrets, Hosts: h.tenancy},
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

// secret creates one of the organization's secrets, open to scope.
func (h *harness) secret(t *testing.T, name string, scope domain.SecretScope) {
	t.Helper()

	n := domain.NewSecret{Name: name, Source: domain.SecretSourceValue, Scope: scope, Value: "test-value-" + name}
	if _, err := h.secrets.Create(h.ctx, n, domain.LocalUser); err != nil {
		t.Fatalf("create secret %s: %v", name, err)
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
