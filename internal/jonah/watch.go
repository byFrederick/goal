package jonah

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// WatchDefinitions are the factories a watch sweep reads and their version histories;
// *authoring.Service satisfies it.
type WatchDefinitions interface {
	Factories(ctx context.Context) ([]ports.FactoryRecord, error)
	// Reach is the factory's newest limit published versions, oldest first, each with the
	// stations it reaches against the version before it.
	Reach(ctx context.Context, f domain.FactoryID, limit int) ([]domain.VersionReach, error)
}

// WatchProposals reads the proposals whose publishes a sweep watches; ports.JonahStore satisfies it.
type WatchProposals interface {
	Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error)
}

// WatchDeps are the ports a watch sweep works through. Log may be nil.
type WatchDeps struct {
	Orgs        Orgs
	Definitions WatchDefinitions
	Proposals   WatchProposals
	Watches     ports.Watches
	Clock       ports.Clock
	Log         *slog.Logger
}

// Bounds of a sweep.
const (
	// WatchHistory is how many of a factory's newest versions a sweep reads: the changes it
	// watches and the versions before them.
	WatchHistory = 50
	// watchGrace is how long past WatchMaxAge a sweep still visits a change, so a watch closes
	// even when no goald swept at its fourteenth day.
	watchGrace = 2 * 24 * time.Hour
	// watchProposals bounds the published proposals a sweep reads per factory.
	watchProposals = 1000
)

// Watcher watches every published change, station by station (ADR 0039 decisions 14 and 15).
type Watcher struct {
	d   WatchDeps
	log *slog.Logger
}

// NewWatcher returns a Watcher over d.
func NewWatcher(d WatchDeps) *Watcher {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Watcher{d: d, log: log}
}

// watchKey is one watch of a change: a metric at a station, for the proposal that published it
// when the watch is on its own number.
type watchKey struct {
	station  string
	metric   domain.ProposalMetric
	proposal domain.ProposalID
}

// Sweep writes the watches of each change of every live organization published within
// WatchMaxAge (and a grace): for a Jonah proposal's publish its own number at its station, for
// any other publish the send-back rate and the cost per item at each station it reaches. Each
// side counts the outcomes credited to its versions: before, those since the last change that
// reached the station; after, the change's version and those after it until the next change
// that reaches the station. A watch is recomputed until it closes, at WatchMaxAge or
// WatchMaxItems after, so the sweep is idempotent and any goald may run it. A factory or an
// organization that fails does not hold up the others.
func (w *Watcher) Sweep(ctx context.Context) error {
	orgs, err := w.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return fmt.Errorf("watch sweep: list organizations: %w", err)
	}

	var errs []error

	for _, org := range orgs {
		octx := domain.WithOrg(ctx, org)

		recs, err := w.d.Definitions.Factories(octx)
		if err != nil {
			errs = append(errs, fmt.Errorf("watch sweep of org %s: list factories: %w", org, err))

			continue
		}

		for _, rec := range recs {
			if rec.Current == nil {
				continue
			}

			if err := w.sweepFactory(octx, rec.ID); err != nil {
				errs = append(errs, fmt.Errorf("watch sweep of factory %s: %w", rec.ID, err))
			}
		}
	}

	return errors.Join(errs...)
}

func (w *Watcher) sweepFactory(ctx context.Context, f domain.FactoryID) error {
	now := w.d.Clock.Now().UTC()

	history, err := w.d.Definitions.Reach(ctx, f, WatchHistory)
	if err != nil {
		return fmt.Errorf("read versions: %w", err)
	}

	recent := slices.ContainsFunc(history, func(v domain.VersionReach) bool { return !v.First && w.watched(v, now) })
	if !recent {
		return nil
	}

	published, err := w.d.Proposals.Proposals(ctx, ports.ProposalQuery{Factory: f, State: domain.ProposalPublished, Limit: watchProposals})
	if err != nil {
		return fmt.Errorf("read published proposals: %w", err)
	}

	byVersion := map[domain.DefinitionVersionID]domain.Proposal{}
	for _, p := range published {
		byVersion[p.PublishedVersionID] = p
	}

	outcomes := map[string][]domain.StationOutcome{}

	for i, v := range history {
		if v.First || !w.watched(v, now) {
			continue
		}

		for _, k := range watchKeys(v, byVersion[v.ID]) {
			got, ok := outcomes[k.station]
			if !ok {
				if got, err = w.d.Watches.StationOutcomes(ctx, f, k.station, versionIDs(history)); err != nil {
					return fmt.Errorf("outcomes at %s: %w", k.station, err)
				}

				outcomes[k.station] = got
			}

			watch := watchOf(f, history, i, k, got, now)
			if _, err := w.d.Watches.SaveWatch(ctx, watch); err != nil {
				return fmt.Errorf("save watch of v%d at %s: %w", v.Number, k.station, err)
			}
		}
	}

	return nil
}

// watched reports whether a sweep at now still visits v's watches.
func (w *Watcher) watched(v domain.VersionReach, now time.Time) bool {
	return now.Sub(v.PublishedAt) <= domain.WatchMaxAge+watchGrace
}

// watchKeys are the watches of v: p's own number when a Jonah proposal with an estimate
// published v, else the send-back rate and the cost per item at each station v reaches.
func watchKeys(v domain.VersionReach, p domain.Proposal) []watchKey {
	if p.ID != "" && p.Estimate.Metric != "" && p.Estimate.Station != "" {
		return []watchKey{{station: p.Estimate.Station, metric: p.Estimate.Metric, proposal: p.ID}}
	}

	out := make([]watchKey, 0, 2*len(v.Stations))
	for _, s := range v.Stations {
		out = append(out, watchKey{station: s, metric: domain.MetricSentBackRate}, watchKey{station: s, metric: domain.MetricCostPerItem})
	}

	return out
}

// watchOf is the watch k of history[i] from the station's outcomes at now.
func watchOf(f domain.FactoryID, history []domain.VersionReach, i int, k watchKey, outcomes []domain.StationOutcome, now time.Time) domain.Watch {
	v := history[i]
	before, after := sides(history, i, k.station)

	var bs, as []float64

	for _, o := range outcomes {
		switch {
		case before[o.VersionID]:
			bs = append(bs, o.Sample(k.metric))
		case after[o.VersionID]:
			as = append(as, o.Sample(k.metric))
		}
	}

	watch := domain.Watch{
		FactoryID: f, VersionID: v.ID, ProposalID: k.proposal, Station: k.station, Metric: k.metric,
		Before: domain.WatchSideOf(k.metric, bs), After: domain.WatchSideOf(k.metric, as), OpenedAt: v.PublishedAt, UpdatedAt: now,
	}

	if iv, ok := domain.WatchIntervalOf(k.metric, bs, as); ok {
		watch.Interval = &iv
	}

	age := now.Sub(v.PublishedAt)
	watch.Verdict = domain.WatchVerdictOf(k.metric, watch.Before, watch.After, watch.Interval, age)

	if age >= domain.WatchMaxAge || watch.After.N >= domain.WatchMaxItems {
		watch.ClosedAt = now
	}

	return watch
}

// sides are the versions on each side of history[i] at station: before, those from the last
// earlier version that reached the station up to history[i]; after, history[i] and those after
// it until the next version that reaches the station.
func sides(history []domain.VersionReach, i int, station string) (before, after map[domain.DefinitionVersionID]bool) {
	before, after = map[domain.DefinitionVersionID]bool{}, map[domain.DefinitionVersionID]bool{}

	for j := i - 1; j >= 0; j-- {
		before[history[j].ID] = true

		if history[j].Reaches(station) {
			break
		}
	}

	after[history[i].ID] = true

	for j := i + 1; j < len(history) && !history[j].Reaches(station); j++ {
		after[history[j].ID] = true
	}

	return before, after
}

func versionIDs(history []domain.VersionReach) []domain.DefinitionVersionID {
	out := make([]domain.DefinitionVersionID, 0, len(history))
	for _, v := range history {
		out = append(out, v.ID)
	}

	return out
}
