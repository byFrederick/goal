package definitions

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

// acmeName is the metadata.name the tests give acme.
const acmeName = "acme"

const noteContract = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["title"],
  "properties": { "title": { "type": "string", "minLength": 1 } }
}`

// org and other are the organizations of the tests: newBucket creates them in this order.
const (
	org   domain.OrgID = "00000000-0000-7000-8000-000000000001"
	other domain.OrgID = "00000000-0000-7000-8000-000000000002"
)

func ctxOf(t *testing.T, o domain.OrgID) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), o)
}

// bucket is a catalog over an in-memory bucket, with the organizations org and other.
type bucket struct {
	objects *porttest.ObjectStore
	catalog *factorystore.Catalog
	orgs    *porttest.Tenancy
}

func newBucket(t *testing.T) *bucket {
	t.Helper()

	objects := porttest.NewObjectStore()
	b := &bucket{objects: objects, catalog: factorystore.New(objects), orgs: porttest.NewTenancy(time.Now)}

	for _, want := range []domain.OrgID{org, other} {
		o, err := b.orgs.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "org-" + string(want[len(want)-1:]), Name: "An org"})
		if err != nil || o.ID != want {
			t.Fatalf("create organization = %v (%v), want %s", o.ID, err, want)
		}
	}

	return b
}

// push pushes a factory to org.
func (b *bucket) push(t *testing.T, files map[string]string) {
	t.Helper()

	b.pushTo(t, org, files)
}

func (b *bucket) pushTo(t *testing.T, o domain.OrgID, files map[string]string) {
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

	if _, _, err := b.catalog.Push(t.Context(), o, dir); err != nil {
		t.Fatal(err)
	}
}

func loadedCache(t *testing.T, files map[string]string) *Cache {
	t.Helper()

	b := newBucket(t)
	b.push(t, files)

	c := New(b.catalog, b.orgs, nil, nil)
	if _, err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	return c
}

func TestValidate_OutputAgainstTheFactorysContract(t *testing.T) {
	t.Parallel()

	c := loadedCache(t, acme(acmeName, noteContract))

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

			err := c.Validate(ctxOf(t, org), acmeID, tt.contract, json.RawMessage(tt.output))
			if (err != nil) != tt.fails || errors.Is(err, ports.ErrContractViolation) != tt.violation {
				t.Errorf("Validate = %v; want failure %v, contract violation %v", err, tt.fails, tt.violation)
			}
		})
	}

	if err := c.Validate(ctxOf(t, org), "00000000-0000-4000-8000-000000000000", note, json.RawMessage(`{}`)); !errors.Is(err, ports.ErrNotFound) {
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

	c := loadedCache(t, acme(acmeName, `{"$ref": "file://`+filepath.ToSlash(secret)+`"}`))

	err := c.Validate(ctxOf(t, org), acmeID, note, json.RawMessage(`{}`))
	if err == nil || errors.Is(err, ports.ErrContractViolation) {
		t.Errorf("Validate with a file $ref = %v; want a contract error that is not a violation", err)
	}
}

func TestWriteAgentFiles_StagesPromptLocalSkillsAndSchema(t *testing.T) {
	t.Parallel()

	c := loadedCache(t, acme(acmeName, noteContract))
	dir := t.TempDir()

	err := c.WriteAgentFiles(ctxOf(t, org), ports.TaskSpec{
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
		// Without "$schema": Claude Code's structured output refuses a 2020-12 meta-schema.
		"output.schema.json": `{"properties":{"title":{"type":"string","minLength":1}},"required":["title"],"type":"object"}`,
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

	b := newBucket(t)

	var loads [][]string

	c := New(b.catalog, b.orgs, func(_ context.Context, fs []Loaded) error {
		names := []string{}
		for _, f := range fs {
			names = append(names, f.Factory.Metadata.Name)
		}

		loads = append(loads, names)

		return nil
	}, nil)

	if _, err := c.Lookup(ctxOf(t, org), acmeName); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Lookup in an empty bucket = %v, want ErrNotFound", err)
	}

	b.push(t, acme(acmeName, noteContract))

	f, err := c.Lookup(ctxOf(t, org), acmeName) // pushed after the last refresh
	if err != nil || f.Metadata.ID != string(acmeID) {
		t.Fatalf("Lookup by name after a push = %v, %v; want the factory", f, err)
	}

	// A definition broken in the bucket (written past push's validation) leaves the last
	// valid one in use.
	if _, err := b.objects.Put(t.Context(), factorystore.Prefix(org, string(acmeID))+config.FileName, []byte("kind: nonsense\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	if f, err := c.Factory(ctxOf(t, org), acmeID); err != nil || f.Metadata.Name != acmeName {
		t.Errorf("Factory after a broken push = %v, %v; want the last valid definition", f, err)
	}

	want := [][]string{{}, {acmeName}, {acmeName}}
	if !slices.EqualFunc(loads, want, slices.Equal) {
		t.Errorf("loads = %v, want %v", loads, want)
	}

	if !strings.Contains(c.Factories(ctxOf(t, org))[0].Metadata.Name, acmeName) {
		t.Errorf("Factories = %v, want acme", c.Factories(ctxOf(t, org)))
	}
}

const otherID domain.FactoryID = "5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01"

// withID is files with another metadata.id.
func withID(files map[string]string, id domain.FactoryID) map[string]string {
	out := maps.Clone(files)
	out[config.FileName] = strings.ReplaceAll(out[config.FileName], string(acmeID), string(id))

	return out
}

func TestRefresh_FactoriesOfTwoOrgsWithTheSameNameLoadSideBySide(t *testing.T) {
	t.Parallel()

	b := newBucket(t)
	b.push(t, acme(acmeName, noteContract))
	b.pushTo(t, other, withID(acme(acmeName, noteContract), otherID))

	var lines []domain.Line

	c := New(b.catalog, b.orgs, func(_ context.Context, fs []Loaded) error {
		lines = lines[:0]
		for _, f := range fs {
			lines = append(lines, f.Line())
		}

		return nil
	}, nil)

	if _, err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		org domain.OrgID
		id  domain.FactoryID
	}{{org, acmeID}, {other, otherID}} {
		f, err := c.Lookup(ctxOf(t, tt.org), acmeName)
		if err != nil || f.Metadata.ID != string(tt.id) {
			t.Errorf("Lookup(acme) in %s = %v (%v), want %s", tt.org, f, err, tt.id)
		}

		if fs := c.Factories(ctxOf(t, tt.org)); len(fs) != 1 || fs[0].Metadata.ID != string(tt.id) {
			t.Errorf("Factories of %s = %v, want only %s", tt.org, fs, tt.id)
		}

		if _, err := c.Factory(ctxOf(t, tt.org), tt.id); err != nil {
			t.Errorf("Factory(%s) in its own organization: %v", tt.id, err)
		}
	}

	want := map[domain.FactoryID]domain.OrgID{acmeID: org, otherID: other}
	if len(lines) != 2 || want[lines[0].FactoryID] != lines[0].OrgID || want[lines[1].FactoryID] != lines[1].OrgID {
		t.Errorf("lines = %+v, want each in the organization that stores it", lines)
	}
}

func TestReads_SeeOnlyTheOrgInTheirContext(t *testing.T) {
	t.Parallel()

	c := loadedCache(t, acme(acmeName, noteContract))

	if _, err := c.Factory(ctxOf(t, other), acmeID); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Factory from another organization = %v, want ErrNotFound", err)
	}

	if err := c.Validate(ctxOf(t, other), acmeID, note, json.RawMessage(`{"title":"x"}`)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Validate from another organization = %v, want ErrNotFound", err)
	}

	if _, err := c.Lookup(ctxOf(t, other), acmeName); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Lookup from another organization = %v, want ErrNotFound", err)
	}

	if _, err := c.Lookup(t.Context(), acmeName); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("Lookup without an organization = %v, want ErrNoOrg", err)
	}

	if _, err := c.Factory(t.Context(), acmeID); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("Factory without an organization = %v, want ErrNoOrg", err)
	}

	if fs := c.Factories(t.Context()); len(fs) != 0 {
		t.Errorf("Factories without an organization = %v, want none", fs)
	}
}

func TestRefresh_DropsTheFactoriesOfADeletedOrg(t *testing.T) {
	t.Parallel()

	b := newBucket(t)
	b.pushTo(t, other, acme(acmeName, noteContract))

	c := New(b.catalog, b.orgs, nil, nil)
	if _, err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := b.orgs.DeleteOrg(ctxOf(t, other), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	loaded, err := c.Refresh(t.Context())
	if err != nil || len(loaded) != 0 {
		t.Errorf("Refresh after deleting the organization = %v (%v), want nothing loaded", loaded, err)
	}
}

func TestRefresh_AFactoryIDInTwoOrgsStaysInTheFirst(t *testing.T) {
	t.Parallel()

	b := newBucket(t)
	b.pushTo(t, other, acme(acmeName, noteContract))

	c := New(b.catalog, b.orgs, nil, nil)
	if _, err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	b.push(t, acme("copied", noteContract)) // the same id in org, which sorts first

	loaded, err := c.Refresh(t.Context())
	if err != nil || len(loaded) != 1 || loaded[0].Org != other || loaded[0].Factory.Metadata.Name != acmeName {
		t.Fatalf("Refresh = %+v (%v), want acme kept in the organization it was loaded in", loaded, err)
	}

	if _, err := c.Factory(ctxOf(t, org), acmeID); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Factory in the copying organization = %v, want ErrNotFound", err)
	}
}
