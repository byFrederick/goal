// Package retention removes what outlived its retention window: organizations deleted longer
// ago than the install's retention (GOAL_ORG_RETENTION), with every row they own and every
// object under their bucket prefix. Any goald instance may run it; the purge lock lets one at a
// time do the work, and every step is safe to repeat, so an instance that dies mid-purge leaves
// nothing the next run cannot finish.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
)

// Orgs purges deleted organizations; ports.Tenancy satisfies it.
type Orgs interface {
	PurgeDeletedOrgs(ctx context.Context, cutoff time.Time, limit int,
		also func(context.Context, domain.OrgID) error) ([]domain.OrgID, error)
}

// batch is how many organizations one purge transaction takes.
const batch = 10

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

// PurgeAfter is when an organization deleted at deletedAt may be purged.
func (p *Purger) PurgeAfter(deletedAt time.Time) time.Time { return deletedAt.Add(p.retention) }

// Purge purges every organization deleted more than the retention ago, in batches, and returns
// their ids: each one's rows (goal_purge_org) and then delete markers on its bucket prefix, in
// one transaction per batch, so a failure keeps the batch for the next run.
func (p *Purger) Purge(ctx context.Context) ([]domain.OrgID, error) {
	if p.retention <= 0 {
		return nil, fmt.Errorf("retention: %s is not a positive retention; nothing is purged", p.retention)
	}

	cutoff := p.clock.Now().Add(-p.retention)

	var purged []domain.OrgID

	for {
		ids, err := p.orgs.PurgeDeletedOrgs(ctx, cutoff, batch, p.deleteObjects)
		if err != nil {
			return purged, err
		}

		for _, id := range ids {
			p.log.InfoContext(ctx, "organization purged", slog.String("org_id", string(id)))
		}

		purged = append(purged, ids...)

		if len(ids) < batch {
			return purged, nil
		}
	}
}

// deleteObjects removes an organization's objects from the bucket.
func (p *Purger) deleteObjects(ctx context.Context, org domain.OrgID) error {
	n, err := factorystore.DeleteOrg(ctx, p.objects, org)
	if err != nil {
		return err
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
