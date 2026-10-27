package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// JudgeScores are Jonah's judge scores (ADR 0025 decision 2, ADR 0002), for the organization in
// ctx: each a judge's score of one finished production run, or of an eval case's try, queued,
// then claimed under a lease by any goald while its judge run works, then recorded as scored or
// error. A run is scored at most once per judge. Methods without an organization in ctx fail with
// domain.ErrNoOrg.
type JudgeScores interface {
	// Enqueue queues n (domain.ErrInvalid unless it passes Check; ErrNotFound for an item or run
	// the organization does not have). A second Enqueue of the same judge and run changes nothing
	// and returns the score already there, in whatever state it is; a try is never a duplicate.
	Enqueue(ctx context.Context, n domain.NewJudgeScore) (domain.JudgeScore, error)
	// Claim leases until until, as running, the oldest queued score, or the oldest running one
	// whose lease ended by now, skipping those another transaction holds. Nil when none is due.
	Claim(ctx context.Context, now, until time.Time) (*domain.JudgeScoreClaim, error)
	// Record ends l's score with r (domain.ErrInvalid unless r passes Check) and releases the
	// lease. ErrLeaseLost, changing nothing, when the lease ended by r.At, another claim took the
	// score, or it is no longer running.
	Record(ctx context.Context, l domain.JudgeScoreLease, r domain.JudgeResult) (domain.JudgeScore, error)
	// Recent is judge's newest production scores in factory, at most limit, newest first, in every
	// state, each with its item's number and title.
	Recent(ctx context.Context, factory domain.FactoryID, judge string, limit int) ([]domain.JudgeScore, error)
	// Summary is each judge's production scores in factory queued in the JudgeSummaryWindow
	// before now, by judge slug: the runs scored and passed, the pass rate, the cost of every
	// judge run that ended (errors included) and one point per UTC day with a scored run. A judge
	// with nothing ended in the window is left out.
	Summary(ctx context.Context, factory domain.FactoryID, now time.Time) ([]domain.JudgeSummary, error)
}
