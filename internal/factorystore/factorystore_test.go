package factorystore

import (
	"errors"
	"maps"
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

// putFactory writes files under a factory's prefix in org, as the layout before versions held it.
func putFactory(t *testing.T, store ports.ObjectStore, o domain.OrgID, id string, files map[string]string) {
	t.Helper()

	for p, body := range files {
		if _, err := store.Put(t.Context(), Prefix(o, id)+p, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestList_ReadsEachFactoryWithoutKnowledge(t *testing.T) {
	t.Parallel()

	store := porttest.NewObjectStore()
	putFactory(t, store, org, acmeID, acmeFactory("acme-notes"))
	putFactory(t, store, org, acmeID, map[string]string{"knowledge/notes/remote.md": "# remote\n", "agents/": ""})

	entries, err := New(store).List(t.Context(), org)
	if err != nil || len(entries) != 1 || entries[0].Err != nil {
		t.Fatalf("list = %+v, %v", entries, err)
	}

	got := map[string]string{}

	for _, o := range entries[0].Snapshot.Files {
		obj, err := store.Get(t.Context(), Prefix(org, acmeID)+o.Key, o.Version)
		if err != nil {
			t.Fatal(err)
		}

		got[o.Key] = string(obj.Body)
	}

	if want := acmeFactory("acme-notes"); !maps.Equal(got, want) {
		t.Errorf("snapshot =\n%v\nwant\n%v (no knowledge, no folder markers)", got, want)
	}
}

func TestList_ReportsEveryFactoryAndWhyOneIsBroken(t *testing.T) {
	t.Parallel()

	store := porttest.NewObjectStore()
	putFactory(t, store, org, acmeID, acmeFactory("acme-notes"))

	const brokenID = "00000000-0000-4000-8000-0000000000bb"
	putFactory(t, store, org, brokenID, map[string]string{"knowledge/notes/a.md": "# A\n"})

	entries, err := New(store).List(t.Context(), org)
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
}

func TestList_KeepsEachOrgsFactoriesApart(t *testing.T) {
	t.Parallel()

	store := porttest.NewObjectStore()
	putFactory(t, store, org, acmeID, acmeFactory("acme-notes"))

	if entries, err := New(store).List(t.Context(), otherOrg); err != nil || len(entries) != 0 {
		t.Errorf("another organization lists %v (%v), want nothing", entries, err)
	}
}
