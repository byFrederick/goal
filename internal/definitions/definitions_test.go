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
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
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

// bodyContract is v2's contract: it requires a body instead of a title.
const bodyContract = `{"type": "object", "required": ["body"]}`

// org and other are the organizations of the tests: newHarness creates them in this order.
const (
	org   domain.OrgID = "00000000-0000-7000-8000-000000000001"
	other domain.OrgID = "00000000-0000-7000-8000-000000000002"
)

const otherID domain.FactoryID = "5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01"

func ctxOf(t *testing.T, o domain.OrgID) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), o)
}

// harness is a definition store over an in-memory bucket, with the organizations org and other.
type harness struct {
	objects *porttest.ObjectStore
	store   *porttest.Definitions
	orgs    *porttest.Tenancy
	notices *porttest.DefinitionNotices
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	objects := porttest.NewObjectStore()
	h := &harness{objects: objects, orgs: porttest.NewTenancy(time.Now), notices: porttest.NewDefinitionNotices()}
	h.store = porttest.NewDefinitions(porttest.DefinitionsDeps{Objects: objects})

	for _, want := range []domain.OrgID{org, other} {
		o, err := h.orgs.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "org-" + string(want[len(want)-1:]), Name: "An org"})
		if err != nil || o.ID != want {
			t.Fatalf("create organization = %v (%v), want %s", o.ID, err, want)
		}
	}

	return h
}

// publish publishes files, a valid factory, as the next version of its factory in o.
func (h *harness) publish(t *testing.T, o domain.OrgID, files map[string]string) domain.DefinitionVersion {
	t.Helper()

	f, err := config.Load(config.Files(bytesOf(files)))
	if err != nil {
		t.Fatal(err)
	}

	return h.publishAs(t, o, domain.FactoryID(f.Metadata.ID), f.Metadata.Name, files)
}

// publishAs publishes files, valid or not, as the next version of factory id.
func (h *harness) publishAs(t *testing.T, o domain.OrgID, id domain.FactoryID, name string, files map[string]string) domain.DefinitionVersion {
	t.Helper()

	v, err := porttest.Publish(ctxOf(t, o), h.store, id, name, bytesOf(files))
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func bytesOf(files map[string]string) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for p, s := range files {
		out[p] = []byte(s)
	}

	return out
}

// cache returns a cache over the harness's store, refreshed once.
func (h *harness) cache(t *testing.T, onLoad LoadFunc) *Cache {
	t.Helper()

	c := New(h.store, h.orgs, onLoad, Config{}, nil)
	if _, err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	return c
}

// loads records the lines of every reload.
type loads struct {
	mu    sync.Mutex
	lines [][]domain.Line
	added chan struct{}
}

func newLoads() *loads { return &loads{added: make(chan struct{}, 1)} }

func (l *loads) record(_ context.Context, lines []domain.Line) error {
	l.mu.Lock()
	l.lines = append(l.lines, lines)
	l.mu.Unlock()

	select {
	case l.added <- struct{}{}:
	default:
	}

	return nil
}

func (l *loads) last() []domain.Line {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.lines) == 0 {
		return nil
	}

	return l.lines[len(l.lines)-1]
}

// await waits until the last reload's lines satisfy ok.
func (l *loads) await(t *testing.T, what string, ok func([]domain.Line) bool) {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for !ok(l.last()) {
		select {
		case <-l.added:
		case <-deadline:
			t.Fatalf("no reload %s; last lines %+v", what, l.last())
		}
	}
}

func versionsOf(lines []domain.Line) map[domain.FactoryID]domain.DefinitionVersionID {
	out := map[domain.FactoryID]domain.DefinitionVersionID{}
	for _, l := range lines {
		out[l.FactoryID] = l.VersionID
	}

	return out
}

func TestRefresh_LoadsEveryCurrentVersionAsALine(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v1 := h.publish(t, org, acme(acmeName, noteContract))
	rec := newLoads()
	c := h.cache(t, rec.record)

	if got := rec.last(); len(got) != 1 || !sameVersion(got[0], domain.Line{OrgID: org, FactoryID: acmeID, VersionID: v1.ID, Number: 1}) ||
		len(got[0].Stations) != 2 {
		t.Fatalf("lines = %+v, want acme's v1 line in its organization", got)
	}

	if _, err := h.store.SetPaused(ctxOf(t, org), acmeID, true, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	v2 := h.publish(t, org, acme(acmeName, bodyContract))

	want := domain.Line{OrgID: org, FactoryID: acmeID, VersionID: v2.ID, Number: 2, Paused: true}
	if lines, err := c.Refresh(t.Context()); err != nil || len(lines) != 1 || !sameVersion(lines[0], want) {
		t.Fatalf("lines after a pause and v2 = %+v (%v), want v2, paused", lines, err)
	}
}

// sameVersion reports whether a line is of want's factory, organization, version and pause.
func sameVersion(l, want domain.Line) bool {
	return l.OrgID == want.OrgID && l.FactoryID == want.FactoryID && l.VersionID == want.VersionID &&
		l.Number == want.Number && l.Paused == want.Paused
}

func TestRefresh_AVersionThatDoesNotLoadKeepsThePreviousOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		spoil func(t *testing.T, h *harness)
	}{
		{
			name: "it does not validate",
			spoil: func(t *testing.T, h *harness) {
				t.Helper()

				broken := acme(acmeName, noteContract)
				broken[config.FileName] = "kind: nonsense\n"
				h.publishAs(t, org, acmeID, acmeName, broken)
			},
		},
		{
			name: "a file no longer matches its hash",
			spoil: func(t *testing.T, h *harness) {
				t.Helper()

				files := acme(acmeName, noteContract)
				files["agents/writer.md"] = "# writer, v2\n"
				h.publish(t, org, files)

				key := domain.BlobKey(org, domain.HashBlob([]byte(files["agents/writer.md"])))
				if _, err := h.objects.Put(t.Context(), key, []byte("# not what was published\n")); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			v1 := h.publish(t, org, acme(acmeName, noteContract))
			c := h.cache(t, nil)

			tt.spoil(t, h)

			lines, err := c.Refresh(t.Context())
			if err != nil || len(lines) != 1 || lines[0].VersionID != v1.ID {
				t.Fatalf("lines = %+v (%v), want v1 kept", lines, err)
			}

			if f, err := c.Lookup(ctxOf(t, org), acmeName); err != nil || f.Spec.Contracts[note] == "" {
				t.Errorf("Lookup = %v, %v; want v1's definition", f, err)
			}

			// A goald that never loaded v1 has nothing to keep.
			if lines, err := h.cache(t, nil).Refresh(t.Context()); err != nil || len(lines) != 0 {
				t.Errorf("lines of a new cache = %+v (%v), want none", lines, err)
			}
		})
	}
}

func TestRefresh_FactoriesOfTwoOrgsWithTheSameNameLoadSideBySide(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publish(t, org, acme(acmeName, noteContract))
	h.publish(t, other, withID(acme(acmeName, noteContract), otherID))

	rec := newLoads()
	c := h.cache(t, rec.record)

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
	}

	want := map[domain.FactoryID]domain.OrgID{acmeID: org, otherID: other}
	if lines := rec.last(); len(lines) != 2 || want[lines[0].FactoryID] != lines[0].OrgID || want[lines[1].FactoryID] != lines[1].OrgID {
		t.Errorf("lines = %+v, want each in its organization", lines)
	}
}

func TestRefresh_DropsTheFactoriesOfADeletedOrg(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publish(t, other, acme(acmeName, noteContract))
	c := h.cache(t, nil)

	if err := h.orgs.DeleteOrg(ctxOf(t, other), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	lines, err := c.Refresh(t.Context())
	if err != nil || len(lines) != 0 {
		t.Errorf("Refresh after deleting the organization = %v (%v), want nothing loaded", lines, err)
	}
}

// withID is files with another metadata.id.
func withID(files map[string]string, id domain.FactoryID) map[string]string {
	out := maps.Clone(files)
	out[config.FileName] = strings.ReplaceAll(out[config.FileName], string(acmeID), string(id))

	return out
}

// A run keeps its version: a version that is no longer current is still served by id, from
// the store when this goald never loaded it.
func TestVersion_ServesPinnedVersionsOnDemand(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v1 := h.publish(t, org, acme(acmeName, noteContract))
	v2 := h.publish(t, org, acme(acmeName, bodyContract))
	c := h.cache(t, nil) // loads v2 only

	title := json.RawMessage(`{"title":"hello"}`)

	if err := c.Validate(ctxOf(t, org), v1.ID, note, title); err != nil {
		t.Errorf("Validate on v1 = %v, want v1's contract satisfied", err)
	}

	if err := c.Validate(ctxOf(t, org), v2.ID, note, title); !errors.Is(err, ports.ErrContractViolation) {
		t.Errorf("Validate on v2 = %v, want v2's contract violated", err)
	}

	if f, err := c.Version(ctxOf(t, org), v1.ID); err != nil || f.Metadata.ID != string(acmeID) {
		t.Errorf("Version(v1) = %v, %v; want acme", f, err)
	}

	for _, id := range []domain.DefinitionVersionID{v1.ID, v2.ID} {
		if _, err := c.Version(ctxOf(t, other), id); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("Version(%s) from another organization = %v, want ErrNotFound", id, err)
		}
	}

	if _, err := c.Version(ctxOf(t, org), "019a1c00-0000-7000-8000-0000000000ff"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Version of an unknown id = %v, want ErrNotFound", err)
	}
}

func TestListen_ReloadsAnOrganizationOnItsNoticesAndEverythingOnAResync(t *testing.T) {
	t.Parallel()

	h := newHarness(t) // the store announces nothing: the test sends the notices
	rec := newLoads()
	c := New(h.store, h.orgs, rec.record, Config{}, nil)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- c.Listen(ctx, h.notices) }()

	t.Cleanup(func() {
		cancel()

		if err := <-done; err != nil {
			t.Error(err)
		}
	})

	rec.await(t, "on the first resync", func(l []domain.Line) bool { return l != nil })

	v1 := h.publish(t, org, acme(acmeName, noteContract))
	theirs := h.publish(t, other, withID(acme(acmeName, noteContract), otherID))

	h.notices.Publish(ports.DefinitionNotice{OrgID: org, FactoryID: acmeID, Kind: ports.NoticeCurrent, VersionID: v1.ID, Number: 1})
	rec.await(t, "on org's current notice", func(l []domain.Line) bool {
		return versionsOf(l)[acmeID] == v1.ID
	})

	if got := versionsOf(rec.last()); got[otherID] != "" {
		t.Errorf("lines %v: a notice of org reloaded other too", got)
	}

	h.notices.Publish(ports.DefinitionNotice{Resync: true})
	rec.await(t, "on a resync", func(l []domain.Line) bool { return versionsOf(l)[otherID] == theirs.ID })

	if _, err := h.store.SetPaused(ctxOf(t, org), acmeID, true, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	h.notices.Publish(ports.DefinitionNotice{OrgID: org, FactoryID: acmeID, Kind: ports.NoticeCurrent, VersionID: v1.ID, Number: 1})
	rec.await(t, "on a pause", func(l []domain.Line) bool {
		i := slices.IndexFunc(l, func(l domain.Line) bool { return l.FactoryID == acmeID })
		return i >= 0 && l[i].Paused
	})
}

func TestValidate_OutputAgainstTheVersionsContract(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v := h.publish(t, org, acme(acmeName, noteContract))
	c := h.cache(t, nil)

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

			err := c.Validate(ctxOf(t, org), v.ID, tt.contract, json.RawMessage(tt.output))
			if (err != nil) != tt.fails || errors.Is(err, ports.ErrContractViolation) != tt.violation {
				t.Errorf("Validate = %v; want failure %v, contract violation %v", err, tt.fails, tt.violation)
			}
		})
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

	h := newHarness(t)
	v := h.publish(t, org, acme(acmeName, `{"$ref": "file://`+filepath.ToSlash(secret)+`"}`))

	err := h.cache(t, nil).Validate(ctxOf(t, org), v.ID, note, json.RawMessage(`{}`))
	if err == nil || errors.Is(err, ports.ErrContractViolation) {
		t.Errorf("Validate with a file $ref = %v; want a contract error that is not a violation", err)
	}
}

func TestWriteAgentFiles_StagesPromptLocalSkillsAndSchema(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v := h.publish(t, org, acme(acmeName, noteContract))
	h.publish(t, org, acme(acmeName, bodyContract)) // the task's run started on v1
	c := h.cache(t, nil)
	dir := t.TempDir()

	err := c.WriteAgentFiles(ctxOf(t, org), ports.TaskSpec{
		FactoryID: acmeID, DefinitionVersionID: v.ID, Agent: "writer", PromptPath: "./agents/writer.md", Output: note,
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
		// v1's contract, without "$schema": Claude Code's structured output refuses a 2020-12
		// meta-schema.
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

	err = c.WriteAgentFiles(ctxOf(t, org), ports.TaskSpec{FactoryID: otherID, DefinitionVersionID: v.ID, PromptPath: "./agents/writer.md"}, t.TempDir())
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("WriteAgentFiles with another factory's version = %v, want ErrNotFound", err)
	}
}

func TestLookup_ReloadsOnAMiss(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	c := h.cache(t, nil)

	if _, err := c.Lookup(ctxOf(t, org), acmeName); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Lookup with nothing published = %v, want ErrNotFound", err)
	}

	h.publish(t, org, acme(acmeName, noteContract))

	if f, err := c.Lookup(ctxOf(t, org), acmeName); err != nil || f.Metadata.ID != string(acmeID) {
		t.Fatalf("Lookup by name after a publish = %v, %v; want the factory", f, err)
	}

	if f, err := c.Lookup(ctxOf(t, org), string(acmeID)); err != nil || f.Metadata.Name != acmeName {
		t.Errorf("Lookup by id = %v, %v; want the factory", f, err)
	}
}

func TestReads_SeeOnlyTheOrgInTheirContext(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v := h.publish(t, org, acme(acmeName, noteContract))
	c := h.cache(t, nil)

	if err := c.Validate(ctxOf(t, other), v.ID, note, json.RawMessage(`{"title":"x"}`)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Validate from another organization = %v, want ErrNotFound", err)
	}

	if _, err := c.Lookup(ctxOf(t, other), acmeName); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Lookup from another organization = %v, want ErrNotFound", err)
	}

	if _, err := c.Lookup(t.Context(), acmeName); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("Lookup without an organization = %v, want ErrNoOrg", err)
	}

	if _, err := c.Version(t.Context(), v.ID); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("Version without an organization = %v, want ErrNoOrg", err)
	}

	if fs := c.Factories(t.Context()); len(fs) != 0 {
		t.Errorf("Factories without an organization = %v, want none", fs)
	}
}
