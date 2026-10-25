// Package retention removes what outlived its retention window: organizations deleted longer
// ago than the install's retention (GOAL_ORG_RETENTION), with every row they own and every
// object under their bucket prefix. Any goald instance may run it: each organization is purged in
// a transaction of its own, which another instance purging at the same time skips, and every
// step is safe to repeat, so an instance that dies mid-purge leaves nothing the next run cannot
// finish.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Orgs purges deleted organizations; ports.Tenancy satisfies it.
type Orgs interface {
	DeletedOrgs(ctx context.Context, cutoff time.Time, limit int) ([]domain.OrgID, error)
	PurgeOrg(ctx context.Context, org domain.OrgID, cutoff time.Time,
		also func(context.Context, domain.OrgID) error) (bool, error)
}

// perRun is how many organizations one run takes, oldest deletion first; the next run takes the
// rest.
const perRun = 1000

// Purger purges deleted organizations once their retention has passed.
type Purger struct {
	orgs      Orgs
	objects   ports.ObjectStore
	clock     ports.Clock
	retention time.Duration
	log       *slog.Logger
}

// New returns a purger keeping deleted organizations for retention, which must be positive (a
// purger without one purges nothing). log may be nil.
func New(orgs Orgs, objects ports.ObjectStore, clock ports.Clock, retention time.Duration, log *slog.Logger) *Purger {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Purger{orgs: orgs, objects: objects, clock: clock, retention: retention, log: log}
}

// Purge purges every organization deleted more than the retention ago, each on its own, and
// returns those it purged: an organization's rows (goal_purge_org) and then its bucket objects,
// committed only when both are gone. One that fails is kept, rows and objects, for the next run
// and does not hold up the others; the error names every one that failed.
func (p *Purger) Purge(ctx context.Context) ([]domain.OrgID, error) {
	if p.retention <= 0 {
		return nil, fmt.Errorf("retention: %s is not a positive retention; nothing is purged", p.retention)
	}

	cutoff := p.clock.Now().Add(-p.retention)

	due, err := p.orgs.DeletedOrgs(ctx, cutoff, perRun)
	if err != nil {
		return nil, err
	}

	var (
		purged []domain.OrgID
		errs   []error
	)

	for _, id := range due {
		ok, err := p.orgs.PurgeOrg(ctx, id, cutoff, p.deleteObjects)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if ok {
			p.log.InfoContext(ctx, "organization purged", slog.String("org_id", string(id)))

			purged = append(purged, id)
		}
	}

	return purged, errors.Join(errs...)
}

// deleteObjects removes every object under the organization's prefix (domain.OrgPrefix) from the
// bucket. Only an organization id (a UUID) names a prefix, so it never reaches above or beside
// that organization's folder. An organization with nothing left deletes nothing, so it is safe
// to repeat.
func (p *Purger) deleteObjects(ctx context.Context, org domain.OrgID) error {
	if err := uuid.Validate(string(org)); err != nil {
		return fmt.Errorf("delete objects of org %q: not an organization id: %w", org, err)
	}

	n, err := p.objects.DeletePrefix(ctx, domain.OrgPrefix(org))
	if err != nil {
		return fmt.Errorf("delete objects of org %s: %w", org, err)
	}

	p.log.DebugContext(ctx, "organization objects deleted", slog.String("org_id", string(org)), slog.Int("objects", n))

	return nil
}

// Run purges at once and then each time the interval passes, until ctx ends; failures are
// logged and retried on the next round.
func (p *Purger) Run(ctx context.Context, every time.Duration) {
	t := p.clock.NewTicker(every)
	defer t.Stop()

	for {
		if _, err := p.Purge(ctx); err != nil && ctx.Err() == nil {
			p.log.WarnContext(ctx, "purge deleted organizations", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}
