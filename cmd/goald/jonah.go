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
func jonahReviewLoop(s settings, store *postgres.Store, defs *definitions.Cache, eng *engine.Engine, clock ports.Clock,
	log *slog.Logger,
) *jonah.Reviews {
	return jonah.NewReviews(jonah.ReviewDeps{
		Orgs: store, Jonah: store, Definitions: defs, Runner: eng, Clock: clock, Log: log,
	}, s.JonahReview)
}

// scheduleLock runs fn on the one goald that takes the install's schedule lock; ports.Intake
// satisfies it.
type scheduleLock interface {
	WithScheduleLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error)
}

// jonahReviews is the Jonah review loop's two steps; *jonah.Reviews satisfies it.
type jonahReviews interface {
	Sweep(ctx context.Context) error
	Work(ctx context.Context) (int, error)
}

// runJonahReviews runs a round of the Jonah review loop at once and then on each tick of every
// until ctx ends (ADR 0034).
func runJonahReviews(ctx context.Context, log *slog.Logger, lock scheduleLock, reviews jonahReviews, clock ports.Clock, every time.Duration) {
	t := clock.NewTicker(every)
	defer t.Stop()

	for {
		jonahRound(ctx, log, lock, reviews)

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

// jonahRound queues the daily reviews that are due, on the goald holding the schedule lock
// only, and then works the queued reviews on every goald: each claim is leased (ClaimJonahReview),
// so a review runs once wherever it is claimed. Failures are logged and retried next round.
func jonahRound(ctx context.Context, log *slog.Logger, lock scheduleLock, reviews jonahReviews) {
	ran, err := lock.WithScheduleLock(ctx, reviews.Sweep)

	switch {
	case err != nil:
		if ctx.Err() == nil {
			log.WarnContext(ctx, "jonah review sweep", slog.Any("error", err))
		}
	case !ran:
		log.DebugContext(ctx, "jonah review sweep skipped: another goald holds the schedule lock")
	}

	n, err := reviews.Work(ctx)
	if err != nil && ctx.Err() == nil {
		log.WarnContext(ctx, "jonah review work", slog.Int("finished", n), slog.Any("error", err))
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

// runJonahTries runs a round of the try loop at once and then on each tick of every until ctx
// ends: the goald holding the schedule lock queues the tries, and every goald works them, each
// claim leased (ClaimPastTry). Failures are logged and retried next round.
func runJonahTries(ctx context.Context, log *slog.Logger, lock scheduleLock, tries jonahReviews, clock ports.Clock, every time.Duration) {
	t := clock.NewTicker(every)
	defer t.Stop()

	for {
		jonahTryRound(ctx, log, lock, tries)

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

// jonahTryRound is one round of the try loop.
func jonahTryRound(ctx context.Context, log *slog.Logger, lock scheduleLock, tries jonahReviews) {
	ran, err := lock.WithScheduleLock(ctx, tries.Sweep)

	switch {
	case err != nil:
		if ctx.Err() == nil {
			log.WarnContext(ctx, "jonah try sweep", slog.Any("error", err))
		}
	case !ran:
		log.DebugContext(ctx, "jonah try sweep skipped: another goald holds the schedule lock")
	}

	n, err := tries.Work(ctx)
	if err != nil && ctx.Err() == nil {
		log.WarnContext(ctx, "jonah try work", slog.Int("finished", n), slog.Any("error", err))
	}
}

// tryDefinitions is jonah.TryDefinitions over the definitions cache: a proposal's line is its
// version with the proposal's files written over it, and the stations it reaches are
// factorymodel.StationsReached's between the two.
type tryDefinitions struct{ defs *definitions.Cache }

func (d tryDefinitions) CurrentLines(ctx context.Context) ([]domain.Line, error) { return d.defs.CurrentLines(ctx) }

func (d tryDefinitions) ProposedLine(ctx context.Context, v domain.DefinitionVersionID, files []domain.ProposalFile) (domain.Line, []string, error) {
	set := make(map[string][]byte, len(files))
	for _, f := range files {
		set[f.Path] = []byte(f.Content)
	}

	before, err := d.defs.VersionFiles(ctx, v)
	if err != nil {
		return domain.Line{}, nil, err
	}

	p, err := d.defs.Proposed(ctx, v, set)
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

func (g gatewayProposed) Proposed(ctx context.Context, id domain.DefinitionVersionID, files map[string][]byte) (gateway.Definitions, error) {
	return g.defs.Proposed(ctx, id, files)
}
