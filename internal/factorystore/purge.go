package factorystore

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// DeleteOrg makes every object under the organization's prefix absent (a delete marker on each
// current key; the bucket's versioning keeps the old versions until lifecycle rules expire them)
// and returns how many it deleted. Deleting an organization with no objects left does nothing,
// so it is safe to repeat.
func DeleteOrg(ctx context.Context, store ports.ObjectStore, org domain.OrgID) (int, error) {
	if org == "" {
		return 0, fmt.Errorf("delete org objects: %w", domain.ErrNoOrg)
	}

	objects, err := store.List(ctx, OrgPrefix(org))
	if err != nil {
		return 0, fmt.Errorf("list objects of org %s: %w", org, err)
	}

	for i, o := range objects {
		if err := store.Delete(ctx, o.Key); err != nil {
			return i, fmt.Errorf("delete %s: %w", o.Key, err)
		}
	}

	return len(objects), nil
}
