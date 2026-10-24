package factorystore

import (
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// A purged organization's objects are gone for good, every version and delete marker of them, so
// the bucket keeps nothing of it; other organizations' objects are untouched.
func TestDeleteOrg_DeletesEveryVersionOfThatOrgsObjectsOnly(t *testing.T) {
	t.Parallel()

	const other domain.OrgID = "019a1c00-0000-7000-8000-0000000000ff"

	ctx := t.Context()
	store := porttest.NewObjectStore()

	put := func(key string) ports.ObjectInfo {
		t.Helper()

		info, err := store.Put(ctx, key, []byte(key))
		if err != nil {
			t.Fatal(err)
		}

		return info
	}

	old := put(Prefix(org, "f1") + "factory.yaml")
	put(old.Key) // a second version
	removed := put(OrgPrefix(org) + "blobs/abc")

	if err := store.Delete(ctx, removed.Key); err != nil { // a delete marker over an old version
		t.Fatal(err)
	}

	kept := Prefix(other, "f2") + "factory.yaml"
	put(kept)

	if n, err := DeleteOrg(ctx, store, org); err != nil || n != 4 {
		t.Fatalf("DeleteOrg = %d, %v; want the org's 4 versions and markers", n, err)
	}

	for _, v := range []ports.ObjectInfo{old, removed} {
		if _, err := store.Get(ctx, v.Key, v.Version); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("version %s of %s after the purge = %v; want it gone", v.Version, v.Key, err)
		}

		if n := store.Versions(v.Key); n != 0 {
			t.Errorf("%s keeps %d versions; want none", v.Key, n)
		}
	}

	if _, err := store.Get(ctx, kept, ""); err != nil {
		t.Errorf("another org's object = %v; want it untouched", err)
	}

	if n, err := DeleteOrg(ctx, store, org); err != nil || n != 0 {
		t.Errorf("DeleteOrg again = %d, %v; want nothing to do", n, err)
	}
}

// Only an organization's own id names the prefix a purge removes: no org, or anything else, is
// refused before the bucket is touched.
func TestDeleteOrg_OnlyAnOrgIDNamesThePrefix(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := porttest.NewObjectStore()

	kept := Prefix(org, "f1") + "factory.yaml"
	if _, err := store.Put(ctx, kept, []byte("x")); err != nil {
		t.Fatal(err)
	}

	if _, err := DeleteOrg(ctx, store, ""); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("DeleteOrg without an org = %v; want ErrNoOrg, never the whole orgs/ prefix", err)
	}

	for _, bad := range []domain.OrgID{"..", "a/b", domain.OrgID(string(org)[:35])} {
		if _, err := DeleteOrg(ctx, store, bad); err == nil {
			t.Errorf("DeleteOrg(%q) = nil; want it refused", bad)
		}
	}

	if store.Versions(kept) != 1 {
		t.Errorf("%s after the refusals has %d versions; want it untouched", kept, store.Versions(kept))
	}
}
