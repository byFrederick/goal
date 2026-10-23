package definitions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const acmeID domain.FactoryID = "8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b"

// acme is a small factory with a local skill, a skills.sh skill and a contract.
func acme(name, contract string) map[string]string {
	return map[string]string{
		"factory.yaml": `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(acmeID) + `, name: ` + name + ` }
spec:
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key }
  knowledgeBase: { collections: [notes] }
  skills:
    - { name: acme-notes, source: ./skills/acme-notes }
    - { name: golang-testing, source: "skills.sh:samber/cc-skills-golang@golang-testing" }
  contracts: { note: ./contracts/note.schema.json }
  agents:
    - { name: writer, prompt: ./agents/writer.md, skills: [acme-notes, golang-testing] }
  board:
    stations:
      - { id: write, kind: agent, agent: writer, output: note }
      - { id: done, kind: done }
`,
		"agents/writer.md":                    "# writer\n",
		"skills/acme-notes/SKILL.md":          "---\nname: acme-notes\n---\n# Notes\n",
		"skills/acme-notes/templates/note.md": "a template\n",
		"contracts/note.schema.json":          contract,
	}
}

const note = "note"

const noteContract = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["title"],
  "properties": { "title": { "type": "string", "minLength": 1 } }
}`

// bucket is a catalog over an in-memory bucket.
type bucket struct {
	objects *porttest.ObjectStore
	catalog *factorystore.Catalog
}

func newBucket() *bucket {
	objects := porttest.NewObjectStore()
	return &bucket{objects: objects, catalog: factorystore.New(objects)}
}

func (b *bucket) push(t *testing.T, files map[string]string) {
	t.Helper()

	dir := t.TempDir()

	for p, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, err := b.catalog.Push(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
}

func loadedCache(t *testing.T, files map[string]string) *Cache {
	t.Helper()

	b := newBucket()
	b.push(t, files)

	c := New(b.catalog, nil, nil)
	if _, err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	return c
}

func TestValidate_OutputAgainstTheFactorysContract(t *testing.T) {
	t.Parallel()

	c := loadedCache(t, acme("acme", noteContract))

	tests := []struct {
		name      string
		contract  string
		output    string
		violation bool
		fails     bool
	}{
		{name: "satisfies", contract: note, output: `{"title":"hello"}`},
		{name: "missing property", contract: note, output: `{}`, violation: true, fails: true},
		{name: "wrong type", contract: note, output: `{"title":3}`, violation: true, fails: true},
		{name: "not JSON", contract: note, output: `title`, violation: true, fails: true},
		{name: "unknown contract", contract: "spec", output: `{}`, fails: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := c.Validate(t.Context(), acmeID, tt.contract, json.RawMessage(tt.output))
			if (err != nil) != tt.fails || errors.Is(err, ports.ErrContractViolation) != tt.violation {
				t.Errorf("Validate = %v; want failure %v, contract violation %v", err, tt.fails, tt.violation)
			}
		})
	}

	if err := c.Validate(t.Context(), "00000000-0000-4000-8000-000000000000", note, json.RawMessage(`{}`)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Validate for an unloaded factory = %v, want ErrNotFound", err)
	}
}

// A contract is content from the bucket; compiling it must never read the host's files or
// the network.
func TestValidate_ContractsCannotReferenceOutsideThemselves(t *testing.T) {
	t.Parallel()

	secret := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(secret, []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c := loadedCache(t, acme("acme", `{"$ref": "file://`+filepath.ToSlash(secret)+`"}`))

	err := c.Validate(t.Context(), acmeID, note, json.RawMessage(`{}`))
	if err == nil || errors.Is(err, ports.ErrContractViolation) {
		t.Errorf("Validate with a file $ref = %v; want a contract error that is not a violation", err)
	}
}

func TestWriteAgentFiles_StagesPromptLocalSkillsAndSchema(t *testing.T) {
	t.Parallel()

	c := loadedCache(t, acme("acme", noteContract))
	dir := t.TempDir()

	err := c.WriteAgentFiles(t.Context(), ports.TaskSpec{
		FactoryID: acmeID, Agent: "writer", PromptPath: "./agents/writer.md", Output: note,
		Skills: []domain.SkillRef{
			{Name: "acme-notes", Source: "./skills/acme-notes"},
			{Name: "golang-testing", Source: "skills.sh:samber/cc-skills-golang@golang-testing"},
		},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"prompt.md":                           "# writer\n",
		"skills/acme-notes/SKILL.md":          "---\nname: acme-notes\n---\n# Notes\n",
		"skills/acme-notes/templates/note.md": "a template\n",
		"output.schema.json":                  noteContract,
	}

	var got []string

	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		rel, _ := filepath.Rel(dir, p)
		got = append(got, filepath.ToSlash(rel))

		body, err := os.ReadFile(p) //nolint:gosec // the test's own temp dir
		if err != nil {
			return err
		}

		if w, ok := want[filepath.ToSlash(rel)]; ok && string(body) != w {
			t.Errorf("%s = %q, want %q", rel, body, w)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(got)

	wantFiles := []string{"output.schema.json", "prompt.md", "skills/acme-notes/SKILL.md", "skills/acme-notes/templates/note.md"}
	if !slices.Equal(got, wantFiles) {
		t.Errorf("files = %v, want %v (skills.sh skills are not staged)", got, wantFiles)
	}
}

func TestRefresh_KeepsTheLastValidDefinitionAndReportsLoads(t *testing.T) {
	t.Parallel()

	b := newBucket()

	var loads [][]string

	c := New(b.catalog, func(_ context.Context, fs []*config.Factory) error {
		names := []string{}
		for _, f := range fs {
			names = append(names, f.Metadata.Name)
		}

		loads = append(loads, names)

		return nil
	}, nil)

	if _, err := c.Lookup(t.Context(), "acme"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Lookup in an empty bucket = %v, want ErrNotFound", err)
	}

	b.push(t, acme("acme", noteContract))

	f, err := c.Lookup(t.Context(), "acme") // pushed after the last refresh
	if err != nil || f.Metadata.ID != string(acmeID) {
		t.Fatalf("Lookup by name after a push = %v, %v; want the factory", f, err)
	}

	// A definition broken in the bucket (written past push's validation) leaves the last
	// valid one in use.
	if _, err := b.objects.Put(t.Context(), factorystore.Prefix(string(acmeID))+config.FileName, []byte("kind: nonsense\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	if f, err := c.Factory(t.Context(), acmeID); err != nil || f.Metadata.Name != "acme" {
		t.Errorf("Factory after a broken push = %v, %v; want the last valid definition", f, err)
	}

	want := [][]string{{}, {"acme"}, {"acme"}}
	if !slices.EqualFunc(loads, want, slices.Equal) {
		t.Errorf("loads = %v, want %v", loads, want)
	}

	if !strings.Contains(c.Factories()[0].Metadata.Name, "acme") {
		t.Errorf("Factories = %v, want acme", c.Factories())
	}
}
