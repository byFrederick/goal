package jonah

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ReviewDefinitions reads the lines and files the review loop works on, for the organization in
// ctx; cmd/goald adapts definitions.Cache to it.
type ReviewDefinitions interface {
	// CurrentLines are the current versions' lines of the organization's factories.
	CurrentLines(ctx context.Context) ([]domain.Line, error)
	// Line is version id's line; ErrNotFound when the organization has no such version.
	Line(ctx context.Context, id domain.DefinitionVersionID) (domain.Line, error)
	// VersionFiles are every file of version id by its path in the definition.
	VersionFiles(ctx context.Context, id domain.DefinitionVersionID) (map[string][]byte, error)
}

// TaskRunner runs one agent run end to end; *engine.Engine satisfies it.
type TaskRunner interface {
	RunTask(ctx context.Context, req domain.RunRequest) (domain.RunResult, error)
}

// ReviewDeps are the ports the review loop works through.
type ReviewDeps struct {
	Orgs        Orgs
	Jonah       ports.JonahStore
	Definitions ReviewDefinitions
	Runner      TaskRunner
	Clock       ports.Clock
	// Log may be nil.
	Log *slog.Logger
}

// ReviewConfig sets the review loop's times.
type ReviewConfig struct {
	// Timeout bounds one review run.
	Timeout time.Duration
	// Margin is the lease's time beyond the run, for reading the version and recording.
	Margin time.Duration
}

// ReviewAgent is the agent a review run records: Goal's own reviewer, not an agent of a
// definition.
const ReviewAgent = "jonah"

// reviewEventLimit is how many sent-back events a review reads, as the gateway serves them.
const reviewEventLimit = 200

// reviewActor queues threshold reviews.
var reviewActor = domain.Actor{Kind: domain.ActorSystem, ID: ReviewAgent, Name: "Jonah"}

// Reviews queues threshold reviews and works the queued reviews of every factory (ADR 0034
// decisions 1 to 5): one jonah-kind run per review, within the factory's weekly budget, whose
// proposals goald checks and whose before values goald computes. Any goald instance may run it:
// at most one review per factory is in flight, and a review whose worker died is claimed again
// when its lease ends.
type Reviews struct {
	d   ReviewDeps
	c   ReviewConfig
	log *slog.Logger
}

// NewReviews returns a review loop over d with the times of c.
func NewReviews(d ReviewDeps, c ReviewConfig) *Reviews {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Reviews{d: d, c: c, log: log}
}

// Run sweeps and works the queue at once and then each time every passes, until ctx ends;
// failures are logged and retried on the next round.
func (r *Reviews) Run(ctx context.Context, every time.Duration) {
	rounds(ctx, r.d.Clock, every, r.log, "jonah review", r.Sweep, r.Work)
}

// Sweep queues a threshold review of each factory of every live organization whose
// learning.jonah is enabled, once its sent-back touches since the last review's window reach
// proposeAfter, unless its review runs of the last BudgetWindow cost weeklyUsd or more. A factory
// with a review in flight keeps it. A factory or organization that fails does not hold up the
// others; the error names each one.
func (r *Reviews) Sweep(ctx context.Context) error {
	orgs, err := r.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return fmt.Errorf("jonah review sweep: list organizations: %w", err)
	}

	var errs []error

	for _, org := range orgs {
		octx := domain.WithOrg(ctx, org)

		lines, err := r.d.Definitions.CurrentLines(octx)
		if err != nil {
			errs = append(errs, fmt.Errorf("jonah review sweep of org %s: list lines: %w", org, err))

			continue
		}

		for _, l := range lines {
			if !l.Jonah.Enabled {
				continue
			}

			if err := r.sweepFactory(octx, l); err != nil {
				errs = append(errs, fmt.Errorf("jonah review sweep of factory %s: %w", l.FactoryID, err))
			}
		}
	}

	return errors.Join(errs...)
}

func (r *Reviews) sweepFactory(ctx context.Context, l domain.Line) error {
	now := r.d.Clock.Now().UTC()

	spent, err := r.d.Jonah.JonahSpendSince(ctx, l.FactoryID, now.Add(-BudgetWindow))
	if err != nil {
		return fmt.Errorf("read spend: %w", err)
	}

	if spent >= l.Jonah.WeeklyUSD {
		return nil
	}

	start, err := windowStart(ctx, r.d.Jonah, l.FactoryID, now)
	if err != nil {
		return err
	}

	n, err := r.d.Jonah.SentBackSince(ctx, l.FactoryID, start)
	if err != nil {
		return fmt.Errorf("count sent-back touches: %w", err)
	}

	if n < proposeAfter(l.Jonah) {
		return nil
	}

	rv, err := r.d.Jonah.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: l.FactoryID, VersionID: l.VersionID, Trigger: domain.JonahThreshold, WindowStart: start, WindowEnd: now,
		By: reviewActor, At: now,
	})
	if errors.Is(err, ports.ErrConflict) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("queue review: %w", err)
	}

	r.log.InfoContext(ctx, "jonah review queued", slog.String("factory", string(l.FactoryID)), slog.String("review_id", string(rv.ID)),
		slog.String("trigger", string(rv.Trigger)), slog.Int("sent_back", n))

	return nil
}

// windowStart is where a review queued now starts reading: the end of the factory's last
// review's window, or FirstWindow back.
func windowStart(ctx context.Context, reviews interface {
	JonahReviews(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.JonahReview, error)
}, f domain.FactoryID, now time.Time,
) (time.Time, error) {
	last, err := reviews.JonahReviews(ctx, f, 1)
	if err != nil {
		return time.Time{}, fmt.Errorf("read the last review: %w", err)
	}

	if len(last) > 0 && !last[0].WindowEnd.IsZero() && last[0].WindowEnd.Before(now) {
		return last[0].WindowEnd, nil
	}

	return now.Add(-FirstWindow), nil
}

// proposeAfter is p's proposeAfter, at least 1.
func proposeAfter(p domain.JonahPolicy) int { return max(p.ProposeAfter, 1) }

// Work claims and runs every claimable review of every live organization, one at a time, and
// returns how many it finished. An organization that fails does not hold up the others.
func (r *Reviews) Work(ctx context.Context) (int, error) {
	orgs, err := r.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("jonah review work: list organizations: %w", err)
	}

	var (
		worked int
		errs   []error
	)

	for _, org := range orgs {
		octx := domain.WithOrg(ctx, org)

		for ctx.Err() == nil {
			claimed, finished, err := r.workOne(octx, org)
			if err != nil {
				errs = append(errs, fmt.Errorf("jonah review work of org %s: %w", org, err))

				break
			}

			if !claimed {
				break
			}

			if finished {
				worked++
			}
		}
	}

	return worked, errors.Join(errs...)
}

// workOne claims the next review and runs it: whether it claimed one and whether it finished it.
func (r *Reviews) workOne(ctx context.Context, org domain.OrgID) (claimed, finished bool, err error) {
	rv, err := r.d.Jonah.ClaimJonahReview(ctx, r.d.Clock.Now().UTC(), r.c.Timeout+r.c.Margin)
	if errors.Is(err, ports.ErrNotFound) {
		return false, false, nil
	}

	if err != nil {
		return false, false, fmt.Errorf("claim review: %w", err)
	}

	res, err := r.review(ctx, org, rv)
	if err != nil {
		// Left running: the lease ends and another claim takes the review again.
		return true, false, fmt.Errorf("review %s: %w", rv.ID, err)
	}

	res.At = r.d.Clock.Now().UTC()

	_, stored, err := r.d.Jonah.FinishJonahReview(ctx, rv, res)
	if errors.Is(err, ports.ErrLeaseLost) {
		r.log.WarnContext(ctx, "jonah review lease lost", slog.String("review_id", string(rv.ID)), slog.String("factory", string(rv.FactoryID)))

		return true, false, nil
	}

	if err != nil {
		return true, false, fmt.Errorf("finish review %s: %w", rv.ID, err)
	}

	r.log.InfoContext(ctx, "jonah review finished", slog.String("review_id", string(rv.ID)), slog.String("factory", string(rv.FactoryID)),
		slog.String("state", string(res.State)), slog.Int("proposals", len(stored)), slog.Int("observations", len(res.Observations)))

	return true, true, nil
}

// failed is a failed result with reason.
func failed(format string, a ...any) domain.JonahReviewResult {
	return domain.JonahReviewResult{State: domain.JonahFailed, Error: truncate(fmt.Sprintf(format, a...), maxReviewError)}
}

// maxReviewError keeps a failed review's error within what the store takes (2000 characters).
const maxReviewError = 2000

// review runs rv and returns the result to record (At unset). A spent budget, a version that is
// gone, a failed run and an output that does not parse are failed results; a read that failed
// is an error, and the review is left to its lease.
func (r *Reviews) review(ctx context.Context, org domain.OrgID, rv domain.JonahReview) (domain.JonahReviewResult, error) {
	l, err := r.d.Definitions.Line(ctx, rv.VersionID)
	if errors.Is(err, ports.ErrNotFound) {
		return failed("definition version %s is gone", rv.VersionID), nil
	}

	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read version %s: %w", rv.VersionID, err)
	}

	spent, err := r.d.Jonah.JonahSpendSince(ctx, rv.FactoryID, r.d.Clock.Now().UTC().Add(-BudgetWindow))
	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read spend: %w", err)
	}

	if spent >= l.Jonah.WeeklyUSD {
		return failed("%s", domain.ErrLearningBudgetExhausted.Error()), nil
	}

	run, err := r.d.Runner.RunTask(ctx, domain.RunRequest{
		OrgID: org, FactoryID: rv.FactoryID, Version: rv.VersionID, Kind: domain.RunKindJonah, Agent: ReviewAgent, Model: l.Jonah.Model,
		Bundle: domain.TaskKindJonah, WindowStart: rv.WindowStart, WindowEnd: rv.WindowEnd, Manual: rv.Trigger == domain.JonahManual,
		ModelConnection: l.Runtime.ModelConnection, Size: domain.SandboxSmall, Timeout: r.c.Timeout,
	})
	if err != nil {
		if ctx.Err() != nil {
			return domain.JonahReviewResult{}, fmt.Errorf("run: %w", err)
		}

		return failed("the review run failed: %v", err), nil
	}

	if run.Status != domain.RunSucceeded {
		return failed("the review run ended %s: %s", run.Status, run.Error), nil
	}

	out, err := parseReviewOutput(run.Output)
	if err != nil {
		return failed("%v", err), nil
	}

	return r.result(ctx, rv, l.Jonah, out)
}

// result checks out against rv's version and window and computes each kept proposal's before
// value: a done result.
func (r *Reviews) result(ctx context.Context, rv domain.JonahReview, p domain.JonahPolicy, out reviewOutput) (domain.JonahReviewResult, error) {
	files, err := r.d.Definitions.VersionFiles(ctx, rv.VersionID)
	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read files of version %s: %w", rv.VersionID, err)
	}

	events, err := r.d.Jonah.SentBackBetween(ctx, rv.FactoryID, rv.WindowStart, rv.WindowEnd, reviewEventLimit)
	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read sent-back events: %w", err)
	}

	proposals, observations := checkOutput(reviewInput{review: rv, proposeAfter: proposeAfter(p), files: files, events: events}, out,
		func(i int, reason string) {
			r.log.WarnContext(ctx, "jonah proposal dropped", slog.String("review_id", string(rv.ID)), slog.Int("proposal", i),
				slog.String("reason", reason))
		})

	flows := map[string]domain.StationFlow{}

	for i := range proposals {
		e := &proposals[i].Estimate

		f, ok := flows[e.Station]
		if !ok {
			if f, err = r.d.Jonah.StationFlow(ctx, rv.FactoryID, e.Station, rv.WindowStart, rv.WindowEnd); err != nil {
				return domain.JonahReviewResult{}, fmt.Errorf("measure station %s: %w", e.Station, err)
			}

			flows[e.Station] = f
		}

		e.Before = before(e.Metric, f)
	}

	return domain.JonahReviewResult{State: domain.JonahDone, Proposals: proposals, Observations: observations}, nil
}

// before is metric's value in f: the sent-back rate (0 when no item passed), or the lead time
// p85 in hours.
func before(metric domain.ProposalMetric, f domain.StationFlow) float64 {
	switch metric {
	case domain.MetricSentBackRate:
		rate, _ := f.SentBackRate()

		return rate
	case domain.MetricLeadTimeP85:
		return f.LeadTimeP85.Hours()
	default:
		return 0
	}
}
