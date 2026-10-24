package factorystore

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const acmeID = "8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b"

// org and otherOrg are the organizations the tests store factories in.
const (
	org      domain.OrgID = "019a1c00-0000-7000-8000-0000000000aa"
	otherOrg domain.OrgID = "019a1c00-0000-7000-8000-0000000000bb"
)

func acmeFactory(name string) map[string]string {
	return map[string]string{
		"factory.yaml": `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + acmeID + `, name: ` + name + ` }
spec:
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key }
  knowledgeBase: { collections: [notes] }
  skills:
    - { name: acme-notes, source: ./skills/acme-notes }
  agents:
    - { name: writer, prompt: ./agents/writer.md, skills: [acme-notes] }
  board:
    stations:
      - { id: write, kind: agent, agent: writer }
      - { id: done, kind: done }
`,
		"agents/writer.md":             "# writer\n",
		"skills/acme-notes/SKILL.md":   "---\nname: acme-notes\n---\n# Notes\n",
		"evals/cases/writes-note.yaml": "kind: EvalCase\n",
	}
}

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for p, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readLocal(t *testing.T, dir string) map[string]string {
	t.Helper()

	tree, err := readTree(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]string{}
	for p, b := range tree {
		out[p] = string(b)
	}

	return out
}

func actions(changes []Change) map[string]Action {
	out := map[string]Action{}
	for _, c := range changes {
		out[c.Path] = c.Action
	}

	return out
}

func TestPushThenPull_RoundTripsTheFactory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	catalog := New(porttest.NewObjectStore())

	src := t.TempDir()
	writeTree(t, src, acmeFactory("acme-notes"))
	writeTree(t, src, map[string]string{".DS_Store": "junk", ".git/HEAD": "ref: main"})

	f, changes, err := catalog.Push(ctx, org, src)
	if err != nil {
		t.Fatal(err)
	}

	if f.Metadata.ID != acmeID || len(changes) != 4 {
		t.Fatalf("pushed %s with %d changes, want %s with 4 (hidden files skipped)", f.Metadata.ID, len(changes), acmeID)
	}

	for _, ref := range []string{acmeID, "acme-notes"} {
		dst := filepath.Join(t.TempDir(), "pulled")
		if _, err := catalog.Pull(ctx, org, ref, dst); err != nil {
			t.Fatalf("pull %s: %v", ref, err)
		}

		if got, want := readLocal(t, dst), acmeFactory("acme-notes"); !maps.Equal(got, want) {
			t.Errorf("pull %s =\n%v\nwant\n%v", ref, got, want)
		}
	}
}

func TestPush_SyncsChangesButNeverDeletesKnowledge(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := porttest.NewObjectStore()
	catalog := New(store)
	prefix := Prefix(org, acmeID)

	src := t.TempDir()
	writeTree(t, src, acmeFactory("acme-notes"))

	if _, _, err := catalog.Push(ctx, org, src); err != nil {
		t.Fatal(err)
	}

	note, err := store.Put(ctx, prefix+"knowledge/notes/onboarding.md", []byte("# Onboarding\n"))
	if err != nil {
		t.Fatal(err)
	}

	removed, err := store.Get(ctx, prefix+"evals/cases/writes-note.yaml", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(src, "evals", "cases", "writes-note.yaml")); err != nil {
		t.Fatal(err)
	}

	writeTree(t, src, map[string]string{"agents/writer.md": "# writer\n\nWrite short notes.\n"})

	_, changes, err := catalog.Push(ctx, org, src)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]Action{
		"agents/writer.md":             Updated,
		"evals/cases/writes-note.yaml": Deleted,
		"factory.yaml":                 Unchanged,
		"skills/acme-notes/SKILL.md":   Unchanged,
	}
	if got := actions(changes); !maps.Equal(got, want) {
		t.Errorf("changes = %v, want %v", got, want)
	}

	if n := store.Versions(prefix + "factory.yaml"); n != 1 {
		t.Errorf("unchanged factory.yaml has %d versions, want 1", n)
	}

	if got, err := store.Get(ctx, note.Key, ""); err != nil || got.Version != note.Version {
		t.Errorf("knowledge document after push: %+v, %v; want it untouched", got.ObjectInfo, err)
	}

	if _, err := store.Get(ctx, removed.Key, ""); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("removed file still current: %v", err)
	}

	if old, err := store.Get(ctx, removed.Key, removed.Version); err != nil || string(old.Body) != "kind: EvalCase\n" {
		t.Errorf("previous version of a deleted file is not restorable: %v", err)
	}
}

func TestPush_RejectsAnInvalidFactoryWithoutWriting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := porttest.NewObjectStore()

	src := t.TempDir()
	files := acmeFactory("acme-notes")
	delete(files, "agents/writer.md")
	writeTree(t, src, files)

	if _, _, err := New(store).Push(ctx, org, src); err == nil || !strings.Contains(err.Error(), "agents/writer.md not found") {
		t.Fatalf("err = %v, want the missing prompt reported", err)
	}

	if objs, _ := store.List(ctx, ""); len(objs) != 0 {
		t.Errorf("push of an invalid factory wrote %d objects", len(objs))
	}
}

func TestPull_NeverWritesIntoANonEmptyFolder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	catalog := New(porttest.NewObjectStore())

	src := t.TempDir()
	writeTree(t, src, acmeFactory("acme-notes"))

	if _, _, err := catalog.Push(ctx, org, src); err != nil {
		t.Fatal(err)
	}

	if _, err := catalog.Pull(ctx, org, "acme-notes", src); err == nil {
		t.Fatal("pull into a folder with edits succeeded")
	}
}

// knowledge/ is reserved for the knowledge base: definitions never carry it in either direction.
func TestDefinitionsLeaveKnowledgeAlone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := porttest.NewObjectStore()
	catalog := New(store)

	src := t.TempDir()
	writeTree(t, src, acmeFactory("acme-notes"))
	writeTree(t, src, map[string]string{"knowledge/notes/local.md": "# local\n"})

	_, changes, err := catalog.Push(ctx, org, src)
	if err != nil {
		t.Fatal(err)
	}

	if _, pushed := actions(changes)["knowledge/notes/local.md"]; pushed {
		t.Error("push uploaded a local knowledge document")
	}

	if _, err := store.Put(ctx, Prefix(org, acmeID)+"knowledge/notes/remote.md", []byte("# remote\n")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "pulled")
	if _, err := catalog.Pull(ctx, org, acmeID, dst); err != nil {
		t.Fatal(err)
	}

	if _, has := readLocal(t, dst)["knowledge/notes/remote.md"]; has {
		t.Error("pull downloaded a knowledge document")
	}
}

func TestList_ReportsEveryFactoryAndWhyOneIsBroken(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := porttest.NewObjectStore()
	catalog := New(store)

	src := t.TempDir()
	writeTree(t, src, acmeFactory("acme-notes"))

	if _, _, err := catalog.Push(ctx, org, src); err != nil {
		t.Fatal(err)
	}

	const brokenID = "00000000-0000-4000-8000-0000000000bb"
	if _, err := store.Put(ctx, Prefix(org, brokenID)+"knowledge/notes/a.md", []byte("# A\n")); err != nil {
		t.Fatal(err)
	}

	entries, err := catalog.List(ctx, org)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	if e := entries[0]; e.ID != brokenID || !errors.Is(e.Err, ports.ErrNotFound) {
		t.Errorf("prefix without factory.yaml: %+v, want ErrNotFound", e)
	}

	if e := entries[1]; e.Err != nil || e.Factory.Metadata.Name != "acme-notes" {
		t.Errorf("valid factory: %+v", e)
	}

	if _, err := catalog.Resolve(ctx, org, "nobody"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("resolve unknown name: %v, want ErrNotFound", err)
	}
}

func TestPush_KeepsEachOrgsFactoriesUnderItsPrefix(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := porttest.NewObjectStore()
	catalog := New(store)

	src := t.TempDir()
	writeTree(t, src, acmeFactory("acme-notes"))

	if _, _, err := catalog.Push(ctx, org, src); err != nil {
		t.Fatal(err)
	}

	objs, err := store.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	for _, o := range objs {
		if !strings.HasPrefix(o.Key, "orgs/"+string(org)+"/factories/"+acmeID+"/") {
			t.Errorf("object %s is outside the organization's factory prefix", o.Key)
		}
	}

	if entries, err := catalog.List(ctx, otherOrg); err != nil || len(entries) != 0 {
		t.Errorf("another organization lists %v (%v), want nothing", entries, err)
	}

	if _, err := catalog.Pull(ctx, otherOrg, "acme-notes", filepath.Join(t.TempDir(), "pulled")); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("pull from another organization = %v, want ErrNotFound", err)
	}
}
