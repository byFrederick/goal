package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
)

// ReapOrphans removes what runs that are over left on this instance's host: containers, run
// folders and workspaces (Deps.Reapers). Several goald instances may share one host, so the
// leftovers of a run go only once the store says the run is over (Store.RunsOver): it neither
// runs nor holds its lease. Another live instance's runs are never over; a dead instance's are
// once SweepLeases requeued their expired leases, so any instance reaps them then.
//
// A leftover labelled with its organization is looked up there, an unlabelled one (a folder) in
// every active organization. A run the store does not know is left alone: it may be another
// deployment's on a shared host. So are the leftovers of an organization the store cannot
// answer for, until the next reap. It returns how many runs it reaped.
func (e *Engine) ReapOrphans(ctx context.Context) (int, error) {
	orgOf := map[domain.RunID]domain.OrgID{} // "" when no leftover names the run's organization
	leftBy := make([][]domain.RunID, len(e.reapers))

	var errs []error

	for i, r := range e.reapers {
		left, err := r.Leftovers(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("reap: list leftovers: %w", err))
			continue
		}

		for _, l := range left {
			leftBy[i] = append(leftBy[i], l.Run)

			if org, seen := orgOf[l.Run]; !seen || org == "" {
				orgOf[l.Run] = l.Org
			} else if l.Org != "" && l.Org != org {
				orgOf[l.Run] = "" // the labels disagree: look the run up everywhere
			}
		}
	}

	if len(orgOf) == 0 {
		return 0, errors.Join(errs...)
	}

	over, err := e.runsOver(ctx, orgOf)
	errs = append(errs, err)

	reaped := map[domain.RunID]bool{}

	for i, r := range e.reapers {
		runs := slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(leftBy[i]))), func(run domain.RunID) bool { return !over[run] })
		if len(runs) == 0 {
			continue
		}

		if err := r.Reap(ctx, runs); err != nil {
			errs = append(errs, fmt.Errorf("reap: %w", err))
		}

		for _, run := range runs {
			reaped[run] = true
		}
	}

	return len(reaped), errors.Join(errs...)
}

// runsOver reports which runs of orgOf the store knows are over: a labelled run is asked of its
// organization when that one is active, an unlabelled run of every active organization.
func (e *Engine) runsOver(ctx context.Context, orgOf map[domain.RunID]domain.OrgID) (map[domain.RunID]bool, error) {
	orgs, err := e.orgs.ActiveOrgs(ctx)
	if err != nil {
		return nil, fmt.Errorf("reap: list organizations: %w", err)
	}

	active := map[domain.OrgID]bool{}
	for _, o := range orgs {
		active[o] = true
	}

	ask := map[domain.OrgID][]domain.RunID{}

	for _, run := range slices.Sorted(maps.Keys(orgOf)) {
		switch org := orgOf[run]; {
		case org == "":
			for _, o := range orgs {
				ask[o] = append(ask[o], run)
			}
		case active[org]:
			ask[org] = append(ask[org], run)
		}
	}

	over := map[domain.RunID]bool{}

	var errs []error

	for _, org := range slices.Sorted(maps.Keys(ask)) {
		runs, err := e.store.RunsOver(domain.WithOrg(ctx, org), ask[org])
		if err != nil {
			errs = append(errs, fmt.Errorf("reap: runs over in organization %s: %w", org, err))
			continue
		}

		for _, run := range runs {
			over[run] = true
		}
	}

	return over, errors.Join(errs...)
}
