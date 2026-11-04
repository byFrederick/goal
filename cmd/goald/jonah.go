package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/adapters/store/postgres"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/factorymodel"
	"github.com/byFrederick/goal/internal/gateway"
	"github.com/byFrederick/goal/internal/jonah"
)

// jonahReviewLoop is the Jonah review loop (ADR 0034, ADR 0039 decision 3): daily reviews queued
// from store, each queued review run by eng as a jonah-kind run on its version in defs, within s.JonahReview's times.
// Its proposals are checked by config's definition check against the organization policy's models.
func jonahReviewLoop(s settings, store *postgres.Store, defs *definitions.Cache, eng *engine.Engine, clock ports.Clock,
	log *slog.Logger,
) *jonah.Reviews {
	return jonah.NewReviews(jonah.ReviewDeps{
		Orgs: store, Jonah: store, Definitions: defs, Runner: eng, Check: config.ProposalCheck{},
		Policies: postgres.NewOrgPolicies(store, s.modelIDs()), Clock: clock, Log: log,
	}, s.JonahReview)
}

// scheduleLock runs fn on the one goald that takes the install's schedule lock; ports.Intake
// satisfies it.
type scheduleLock interface {
	WithScheduleLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error)
}

// jonahReviews is a Jonah loop's two steps; *jonah.Reviews and *jonah.Tries satisfy it.
type jonahReviews interface {
	Sweep(ctx context.Context) error
	Work(ctx context.Context) (int, error)
}

// loopEvents are the log events of one of Jonah's loops, stable per loop.
type loopEvents struct{ sweep, skipped, work string }

// The log events of the review loop and of the try loop.
var (
	reviewEvents = loopEvents{
		sweep: "jonah review sweep", skipped: "jonah review sweep skipped: another goald holds the schedule lock", work: "jonah review work",
	}
	tryEvents = loopEvents{
		sweep: "jonah try sweep", skipped: "jonah try sweep skipped: another goald holds the schedule lock", work: "jonah try work",
	}
)

// runJonahLoop runs a round of one of Jonah's loops, its reviews (ADR 0034) or its tries of past
// items (ADR 0039 decisions 9 and 11), at once and then on each tick of every until ctx ends.
func runJonahLoop(ctx context.Context, log *slog.Logger, lock scheduleLock, loop jonahReviews, clock ports.Clock, every time.Duration,
	ev loopEvents,
) {
	t := clock.NewTicker(every)
	defer t.Stop()

	for {
		jonahRound(ctx, log, lock, loop, ev)

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

// jonahRound sweeps (queues the daily reviews or the tries that are due) on the goald holding
// the schedule lock only, and then works the queue on every goald: each claim is leased
// (ClaimJonahReview, ClaimPastTry), so each runs once wherever it is claimed. Failures are logged
// and retried next round.
func jonahRound(ctx context.Context, log *slog.Logger, lock scheduleLock, loop jonahReviews, ev loopEvents) {
	ran, err := lock.WithScheduleLock(ctx, loop.Sweep)

	switch {
	case err != nil:
		if ctx.Err() == nil {
			log.WarnContext(ctx, ev.sweep, slog.Any("error", err))
		}
	case !ran:
		log.DebugContext(ctx, ev.skipped)
	}

	n, err := loop.Work(ctx)
	if err != nil && ctx.Err() == nil {
		log.WarnContext(ctx, ev.work, slog.Int("finished", n), slog.Any("error", err))
	}
}

// watchEvery is how often the goald holding the schedule lock sweeps the watches (ADR 0039
// decision 14): a watch's verdict moves with the items counted, which take hours, not minutes.
const watchEvery = 15 * time.Minute

// watchSweep is the watch sweep; *jonah.Watcher satisfies it.
type watchSweep interface {
	Sweep(ctx context.Context) error
}

// runWatches sweeps the watches at once and then on each tick of every until ctx ends, on the
// goald holding the schedule lock only. The sweep is idempotent, so a lock that moves between
// instances does no harm. Failures are logged and retried next tick.
func runWatches(ctx context.Context, log *slog.Logger, lock scheduleLock, watches watchSweep, clock ports.Clock, every time.Duration) {
	t := clock.NewTicker(every)
	defer t.Stop()

	for {
		if _, err := lock.WithScheduleLock(ctx, watches.Sweep); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "watch sweep", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

// jonahTryLoop is the loop of tries of past items (ADR 0039 decisions 9 and 11): proposals'
// tries queued from store, each run by eng as a try-kind run on the proposal's version in defs,
// within s.JonahTry's times and cap.
func jonahTryLoop(s settings, store *postgres.Store, defs *definitions.Cache, eng *engine.Engine, clock ports.Clock,
	log *slog.Logger,
) *jonah.Tries {
	return jonah.NewTries(jonah.TryDeps{
		Orgs: store, Jonah: store, Tries: store, Definitions: tryDefinitions{defs: defs}, Waiting: store, Runner: eng, Clock: clock, Log: log,
	}, s.JonahTry)
}

// tryDefinitions is jonah.TryDefinitions over the definitions cache: a proposal's line is its
// version with the proposal's edits applied to it, and the stations it reaches are
// factorymodel.StationsReached's between the two.
type tryDefinitions struct{ defs *definitions.Cache }

func (d tryDefinitions) CurrentLines(ctx context.Context) ([]domain.Line, error) {
	return d.defs.CurrentLines(ctx)
}

func (d tryDefinitions) ProposedLine(ctx context.Context, v domain.DefinitionVersionID, edits []domain.ProposalEdit) (domain.Line, []string, error) {
	before, err := d.defs.VersionFiles(ctx, v)
	if err != nil {
		return domain.Line{}, nil, err
	}

	p, err := d.defs.Proposed(ctx, v, edits)
	if err != nil {
		return domain.Line{}, nil, err
	}

	after, err := p.VersionFiles(ctx, v)
	if err != nil {
		return domain.Line{}, nil, err
	}

	return p.Line(), factorymodel.StationsReached(snapshot(before), snapshot(after)), nil
}

// snapshot is files as a factorymodel.Snapshot.
func snapshot(files map[string][]byte) factorymodel.Snapshot {
	m := make(domain.Manifest, len(files))
	for p, b := range files {
		m[p] = domain.HashBlob(b)
	}

	return factorymodel.Snapshot{Manifest: m, Files: config.Files(files)}
}

// gatewayProposed is gateway.ProposedDefinitions over the definitions cache.
type gatewayProposed struct{ defs *definitions.Cache }

func (g gatewayProposed) Proposed(ctx context.Context, id domain.DefinitionVersionID, edits []domain.ProposalEdit) (gateway.Definitions, error) {
	return g.defs.Proposed(ctx, id, edits)
}
