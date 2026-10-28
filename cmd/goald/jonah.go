package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/adapters/store/postgres"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/jonah"
)

// jonahReviewLoop is the Jonah review loop (ADR 0034): threshold reviews queued from store, each
// queued review run by eng as a jonah-kind run on its version in defs, within s.JonahReview's times.
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

// jonahRound queues the threshold reviews that are due, on the goald holding the schedule lock
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
