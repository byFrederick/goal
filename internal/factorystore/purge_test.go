package factorystore

import (
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

func TestDeleteOrg_DeletesOnlyThatOrgsObjectsAndKeepsTheirVersions(t *testing.T) {
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

	gone := put(Prefix(org, "f1") + "factory.yaml")
	put(OrgPrefix(org) + "blobs/abc")

	kept := Prefix(other, "f2") + "factory.yaml"
	put(kept)

	if n, err := DeleteOrg(ctx, store, org); err != nil || n != 2 {
		t.Fatalf("DeleteOrg = %d, %v; want the org's 2 objects", n, err)
	}

	if left, err := store.List(ctx, OrgPrefix(org)); err != nil || len(left) != 0 {
		t.Errorf("objects left under the org = %v, %v; want none", left, err)
	}

	if _, err := store.Get(ctx, gone.Key, gone.Version); err != nil {
		t.Errorf("a deleted object's version is no longer readable: %v", err)
	}

	if _, err := store.Get(ctx, kept, ""); err != nil {
		t.Errorf("another org's object = %v; want it untouched", err)
	}

	if n, err := DeleteOrg(ctx, store, org); err != nil || n != 0 {
		t.Errorf("DeleteOrg again = %d, %v; want nothing to do", n, err)
	}

	if _, err := DeleteOrg(ctx, store, ""); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("DeleteOrg without an org = %v; want ErrNoOrg, never the whole orgs/ prefix", err)
	}
}
