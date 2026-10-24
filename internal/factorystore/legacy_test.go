package factorystore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// legacyBucket is a bucket in the layout before organizations: acme's definition, a knowledge
// document, and a folder marker. It returns the version of every key.
func legacyBucket(t *testing.T) (*porttest.ObjectStore, map[string]string) {
	t.Helper()

	store := porttest.NewObjectStore()
	versions := map[string]string{}

	files := acmeFactory("acme-notes")
	files["knowledge/notes/a.md"] = "# A\n"
	files["agents/"] = ""

	for p, body := range files {
		o, err := store.Put(t.Context(), LegacyRoot+acmeID+"/"+p, []byte(body))
		if err != nil {
			t.Fatal(err)
		}

		versions[o.Key] = o.Version
	}

	return store, versions
}

func TestMigrateLegacy_MovesFactoriesIntoTheOrgAndKeepsOldVersions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, versions := legacyBucket(t)

	m, err := MigrateLegacy(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}

	if m != (Migration{Copied: len(versions), Deleted: len(versions)}) {
		t.Errorf("migration = %+v, want %d copied and deleted", m, len(versions))
	}

	for key, version := range versions {
		assertMoved(t, store, key, version)
	}

	entries, err := New(store).List(ctx, org)
	if err != nil || len(entries) != 1 || entries[0].Err != nil || entries[0].Factory.Metadata.Name != "acme-notes" {
		t.Fatalf("the organization lists %+v (%v), want acme-notes", entries, err)
	}

	again, err := MigrateLegacy(ctx, store, org)
	if err != nil || again != (Migration{}) {
		t.Errorf("second migration = %+v (%v), want nothing to do", again, err)
	}
}

// assertMoved checks that a legacy key is no longer current, that its old version is still
// readable and that the organization holds its content.
func assertMoved(t *testing.T, store *porttest.ObjectStore, key, version string) {
	t.Helper()

	if _, err := store.Get(t.Context(), key, ""); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("legacy %s is still current (%v)", key, err)
	}

	old, err := store.Get(t.Context(), key, version)
	if err != nil {
		t.Errorf("legacy %s version %s is no longer readable: %v", key, version, err)
		return
	}

	moved, err := store.Get(t.Context(), OrgPrefix(org)+key, "")
	if err != nil || string(moved.Body) != string(old.Body) {
		t.Errorf("moved %s = %q (%v), want %q", key, moved.Body, err, old.Body)
	}
}

func TestMigrateLegacy_FinishesAPartialCopyAndFixesADifferentOne(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, versions := legacyBucket(t)

	// An earlier run copied one file and died; another object differs.
	if _, err := store.Put(ctx, OrgPrefix(org)+LegacyRoot+acmeID+"/agents/writer.md", []byte("# writer\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Put(ctx, OrgPrefix(org)+LegacyRoot+acmeID+"/factory.yaml", []byte("half written")); err != nil {
		t.Fatal(err)
	}

	m, err := MigrateLegacy(ctx, store, org)
	if err != nil {
		t.Fatal(err)
	}

	if m != (Migration{Copied: len(versions) - 1, Present: 1, Deleted: len(versions)}) {
		t.Errorf("migration = %+v, want one present and the rest copied", m)
	}

	moved, err := store.Get(ctx, OrgPrefix(org)+LegacyRoot+acmeID+"/factory.yaml", "")
	if err != nil || !strings.Contains(string(moved.Body), "acme-notes") {
		t.Errorf("factory.yaml = %q (%v), want the legacy definition", moved.Body, err)
	}
}

// failingPut fails every write to one key, as a bucket that refuses a request would.
type failingPut struct {
	*porttest.ObjectStore

	key string
}

func (f failingPut) Put(ctx context.Context, key string, body []byte) (ports.ObjectInfo, error) {
	if key == f.key {
		return ports.ObjectInfo{}, errors.New("bucket refused the write")
	}

	return f.ObjectStore.Put(ctx, key, body)
}

func TestMigrateLegacy_DeletesNothingUntilEveryCopyMatches(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, versions := legacyBucket(t)
	broken := failingPut{ObjectStore: store, key: OrgPrefix(org) + LegacyRoot + acmeID + "/agents/writer.md"}

	if _, err := MigrateLegacy(ctx, broken, org); err == nil {
		t.Fatal("migration with a failing copy succeeded")
	}

	for key := range versions {
		if _, err := store.Get(ctx, key, ""); err != nil {
			t.Errorf("legacy %s was deleted before every copy matched: %v", key, err)
		}
	}

	// factory.yaml is copied last, so the organization never sees a definition without its files.
	if _, err := store.Get(ctx, OrgPrefix(org)+LegacyRoot+acmeID+"/factory.yaml", ""); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("factory.yaml was copied before the files it names (%v)", err)
	}

	m, err := MigrateLegacy(ctx, store, org)
	if err != nil || m.Deleted != len(versions) {
		t.Errorf("rerun = %+v (%v), want every legacy key deleted", m, err)
	}
}
