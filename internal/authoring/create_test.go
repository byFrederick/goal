package authoring_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/domain"
)

// putTemplate stores a template version in the catalog.
func (h *harness) putTemplate(t *testing.T, name, version, meta string, files map[string][]byte) {
	t.Helper()

	base := authoring.TemplatesPrefix + name + "/" + version + "/"
	if _, err := h.objects.Put(h.ctx, base+"template.yaml", []byte(meta)); err != nil {
		t.Fatal(err)
	}

	for p, b := range files {
		if _, err := h.objects.Put(h.ctx, base+"factory/"+p, b); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCreateFactory_TemplateRewritesIdentityAndKeepsComments(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putTemplate(t, "acme-review", "1", "title: Acme review\n", acmeFiles(acmeYAML))
	h.putTemplate(t, "acme-review", "2", "title: Acme review\nsummary: Build and review\nchanged: \"2026-10-09\"\n",
		acmeFiles(strings.Replace(acmeYAML, "wip: 1", "wip: 2", 1)))

	c, err := h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{
		Name: "acme-costs", Title: "Acme costs",
		Start: authoring.Start{Kind: authoring.StartTemplate, Template: "acme-review"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	doc := h.draftFile(t, c.Factory.ID, yamlFile)
	for _, want := range []string{
		"# Acme review line: a fixture for authoring tests.", "# The one engineer.",
		"id: " + string(c.Factory.ID), "name: acme-costs", "title: Acme costs", "wip: 2",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("draft factory.yaml lacks %q:\n%s", want, doc)
		}
	}

	if strings.Contains(doc, "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10") {
		t.Errorf("draft keeps the template's id:\n%s", doc)
	}

	if got := h.draftFile(t, c.Factory.ID, promptFile); got != enginePrompt {
		t.Errorf("prompt = %q", got)
	}

	if c.Factory.Name != "acme-costs" || c.Draft.Source.Kind != domain.SourceTemplate || c.Draft.Source.Template != "acme-review@2" {
		t.Errorf("created %+v from %+v", c.Factory, c.Draft.Source)
	}

	cmp, err := h.svc.Compare(h.ctx, c.Factory.ID, authoring.VersionSelector{Kind: authoring.SelectSource}, authoring.VersionSelector{})
	if err != nil {
		t.Fatalf("compare with the template: %v", err)
	}

	if cmp.Counts.Changes == 0 || cmp.From != nil {
		t.Errorf("changes from the template = %+v (from %v), want the identity changes", cmp.Counts, cmp.From)
	}
}

func TestCreateFactory_TemplateErrors(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{
		Name: "acme-costs", Start: authoring.Start{Kind: authoring.StartTemplate, Template: "nope"},
	})
	if !errors.Is(err, authoring.ErrTemplateNotFound) {
		t.Errorf("unknown template: %v", err)
	}

	_, err = h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{Start: authoring.Start{Kind: authoring.StartBlank}})
	if !errors.Is(err, authoring.ErrInvalidRequest) {
		t.Errorf("blank without a name: %v", err)
	}
}

func TestCreateFactory_BlankSkeletonNamesNoStationOrAgent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	c, err := h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{Name: "acme-new", Start: authoring.Start{Kind: authoring.StartBlank}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	m, err := h.svc.DraftModel(h.ctx, c.Factory.ID)
	if err != nil {
		t.Fatalf("model: %v", err)
	}

	if m.Model.Basics["name"] != "acme-new" || len(m.Model.Line) != 0 || len(m.Model.Agents) != 0 {
		t.Errorf("blank model = %+v", m.Model)
	}

	if !strings.Contains(h.draftFile(t, c.Factory.ID, yamlFile), `image: "ghcr.io/acme/agent:1.0.0"`) {
		t.Errorf("skeleton lacks the default image")
	}

	if c.Draft.Source.Kind != domain.SourceBlank || c.Draft.Source.Template != "" {
		t.Errorf("source = %+v", c.Draft.Source)
	}
}

func TestCreateFactory_BlankUsesTheBlankTemplate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putTemplate(t, authoring.BlankTemplate, "1", "title: Blank\n", acmeFiles(acmeYAML))

	c, err := h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{Name: "acme-new", Start: authoring.Start{Kind: authoring.StartBlank}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if c.Draft.Source.Template != "blank@1" || !strings.Contains(h.draftFile(t, c.Factory.ID, yamlFile), "id: inbox") {
		t.Errorf("blank did not start from the blank template: %+v", c.Draft.Source)
	}
}

const credentialYAML = `apiVersion: goal/v1
kind: Factory
metadata:
  id: 0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a20
  name: acme-source
spec:
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key }
  knowledgeBase: { collections: [notes] }
  connections:
    # Code host.
    - connector: github
      name: code
      credential: acme-github
      agents: [engineer]
  agents:
    - name: engineer
      prompt: ./agents/engineer.md
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: done, kind: done }
`

func TestCreateFactory_CloneDropsCredentialsAndSharesBlobs(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	src := h.imported(t, acmeFiles(credentialYAML))

	c, err := h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{
		Name: "acme-copy", Start: authoring.Start{Kind: authoring.StartClone, Factory: "acme-source"},
	})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}

	doc := h.draftFile(t, c.Factory.ID, yamlFile)
	if strings.Contains(doc, "credential") || !strings.Contains(doc, "# Code host.") || !strings.Contains(doc, "connector: github") {
		t.Errorf("clone factory.yaml:\n%s", doc)
	}

	srcDraft, err := h.svc.Draft(h.ctx, src)
	if err != nil {
		t.Fatal(err)
	}

	if c.Draft.Manifest[promptFile] != srcDraft.Draft.Manifest[promptFile] {
		t.Errorf("clone does not share the prompt blob")
	}

	if c.Draft.Source.Kind != domain.SourceClone || c.Draft.Source.Factory != src || c.Draft.Source.Version != srcDraft.Draft.ID {
		t.Errorf("source = %+v", c.Draft.Source)
	}
}

func TestCreateFactory_Import(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{Start: authoring.Start{
		Kind: authoring.StartImport, Files: map[string][]byte{yamlFile: []byte("a: [b\n")},
	}})
	if authoring.ProblemCode(err) != authoring.CodeImportInvalid {
		t.Errorf("unparsable import: %v", err)
	}

	_, err = h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{Start: authoring.Start{
		Kind: authoring.StartImport, Files: map[string][]byte{promptFile: []byte(enginePrompt)},
	}})
	if !errors.Is(err, authoring.ErrImportInvalid) {
		t.Errorf("import without factory.yaml: %v", err)
	}

	c, err := h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{Start: authoring.Start{
		Kind: authoring.StartImport, Files: acmeFiles(acmeYAML), KeepID: true,
	}})
	if err != nil || c.Factory.ID != "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10" || c.Factory.Name != "acme-review" {
		t.Fatalf("import keeping the id = %+v, %v", c.Factory, err)
	}

	if c.Draft.Source.Kind != domain.SourceCLI {
		t.Errorf("source = %s", c.Draft.Source.Kind)
	}

	_, err = h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{Start: authoring.Start{
		Kind: authoring.StartImport, Files: acmeFiles(acmeYAML), KeepID: true,
	}})
	if !errors.Is(err, authoring.ErrNameTaken) && !errors.Is(err, authoring.ErrFactoryExists) {
		t.Errorf("second import of the same id: %v", err)
	}

	_, err = h.svc.CreateFactory(h.ctx, authoring.NewFactoryRequest{Start: authoring.Start{Kind: authoring.StartImport, Files: acmeFiles(acmeYAML)}})
	if !errors.Is(err, authoring.ErrNameTaken) {
		t.Errorf("import of a taken name: %v", err)
	}
}
