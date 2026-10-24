package factorystore

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// DeleteOrg removes, for good, every version and delete marker of every object under the
// organization's prefix (ports.ObjectStore.DeletePrefix), so the bucket keeps nothing of a
// purged organization, and returns how many it removed. Only an organization id (a UUID) names a
// prefix, so the call never reaches above or beside that organization's folder. Deleting an
// organization with nothing left does nothing, so it is safe to repeat.
func DeleteOrg(ctx context.Context, store ports.ObjectStore, org domain.OrgID) (int, error) {
	if org == "" {
		return 0, fmt.Errorf("delete org objects: %w", domain.ErrNoOrg)
	}

	if err := uuid.Validate(string(org)); err != nil {
		return 0, fmt.Errorf("delete org objects: org %q is not an organization id: %w", org, err)
	}

	n, err := store.DeletePrefix(ctx, OrgPrefix(org))
	if err != nil {
		return n, fmt.Errorf("delete objects of org %s: %w", org, err)
	}

	return n, nil
}
