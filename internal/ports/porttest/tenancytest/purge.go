package tenancytest

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The purge's hook runs for every organization of the batch, and an error from it purges none.
func testPurgeHook(f *fixture) {
	first, second := f.create(acme, acmeName), f.create(labs, labsName)
	f.remove(first, domain.LocalUser)
	f.remove(second, domain.LocalUser)

	cutoff := t0.Add(time.Second)

	ids, err := f.Tenancy.PurgeDeletedOrgs(f.t.Context(), cutoff, 10, func(_ context.Context, org domain.OrgID) error {
		if org == second.ID {
			return errors.New("bucket unavailable")
		}

		return nil
	})
	if err == nil || len(ids) != 0 {
		f.t.Fatalf("PurgeDeletedOrgs with a failing hook = %v, %v; want an error and nothing purged", ids, err)
	}

	if got := f.log(first, ports.AuditQuery{}); len(got) == 0 {
		f.t.Error("a failed purge removed the batch's audit log; want the batch kept for the next run")
	}

	var seen []domain.OrgID

	ids, err = f.Tenancy.PurgeDeletedOrgs(f.t.Context(), cutoff, 10, func(_ context.Context, org domain.OrgID) error {
		seen = append(seen, org)

		return nil
	})

	slices.Sort(seen)

	want := []domain.OrgID{first.ID, second.ID}
	slices.Sort(want)

	slices.Sort(ids)

	if err != nil || !slices.Equal(ids, want) || !slices.Equal(seen, want) {
		f.t.Errorf("PurgeDeletedOrgs = %v, %v, hook saw %v; want both orgs purged and seen", ids, err, seen)
	}
}
