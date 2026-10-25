package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
)

// ReapOrphans deletes the sandboxes of runs that are over. A sandbox outlives its run when the
// goald running it dies, and several goald instances may share one sandbox backend, so a
// sandbox goes only once the store says its run is over (Store.RunsOver): it neither runs nor
// holds its lease. Another live instance's runs are never over; a dead instance's are once
// SweepLeases requeued their expired leases, so any instance reaps them then.
//
// A sandbox is looked up in the organization it is labelled with, when that one is active. One
// whose run that organization does not know, or that names no active organization, is left
// alone: it may be another deployment's on a shared backend. So are the sandboxes of an
// organization the store cannot answer for, until the next reap. It returns how many runs'
// sandboxes it deleted.
func (e *Engine) ReapOrphans(ctx context.Context) (int, error) {
	left, err := e.sandboxes.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("reap: list sandboxes: %w", err)
	}

	if len(left) == 0 {
		return 0, nil
	}

	orgs, err := e.orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reap: list organizations: %w", err)
	}

	byOrg := map[domain.OrgID][]domain.RunID{}

	for _, l := range left {
		if slices.Contains(orgs, l.Org) {
			byOrg[l.Org] = append(byOrg[l.Org], l.Run)
		}
	}

	var (
		reaped int
		errs   []error
	)

	for _, org := range slices.Sorted(maps.Keys(byOrg)) {
		over, err := e.store.RunsOver(domain.WithOrg(ctx, org), slices.Compact(slices.Sorted(slices.Values(byOrg[org]))))
		if err != nil {
			errs = append(errs, fmt.Errorf("reap: runs over in organization %s: %w", org, err))
			continue
		}

		for _, run := range over {
			if err := e.sandboxes.Delete(ctx, run); err != nil {
				errs = append(errs, fmt.Errorf("reap: delete the sandbox of run %s: %w", run, err))
				continue
			}

			reaped++
		}
	}

	return reaped, errors.Join(errs...)
}
