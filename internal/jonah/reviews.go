package jonah

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ReviewDefinitions reads the lines and files the review loop works on, for the organization in
// ctx; *definitions.Cache satisfies it.
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
	// Check checks the files a proposal's edits give (ADR 0039 decision 6).
	Check DefinitionCheck
	// Policies are the organization's policy, whose allowed models an agent's model may change to.
	Policies Policies
	Clock    ports.Clock
	// Log may be nil.
	Log *slog.Logger
}

// Policies reads the organization's policy; ports.OrgPolicies satisfies it.
type Policies interface {
	Get(ctx context.Context) (domain.OrgPolicy, error)
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

// Limits of what a review's output is checked against: the sent-back events, as the gateway
// serves them, and the open observations and proposals.
const (
	reviewEventLimit       = 200
	reviewObservationLimit = 200
	reviewProposalLimit    = 500
	// reviewEvidenceLimit is the evidence items a review's facts may name, as the gateway serves them.
	reviewEvidenceLimit = 200
)

// reviewActor queues the daily reviews.
var reviewActor = domain.JonahActor

// Reviews queues each factory's daily review and works the queued reviews of every factory (ADR
// 0034 decisions 1 to 5, ADR 0039 decisions 3 and 4): one jonah-kind run per attempt, within the
// factory's weekly budget, retried twice, whose proposals goald checks and whose before values
// goald computes. Any goald instance may run it:
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

// Sweep queues the daily review of each factory of every live organization whose learning.jonah
// is enabled (ADR 0039 decision 3): once UTC now is past the factory's hour of the day
// (ReviewHour), when its latest review was queued before that hour today, it has an item finished
// since its window starts, and Jonah's runs of the last BudgetWindow cost less than its share of
// the line's spend (ADR 0039 decision 16).
// A factory with a review in flight keeps it. A factory or organization that fails does not hold
// up the others; the error names each one.
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

// ReviewHour is the UTC hour of the day factory f's daily review is queued from: a stable hash of
// its id, so the organizations' reviews spread over the day (ADR 0039 decision 3).
func ReviewHour(f domain.FactoryID) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(f))

	return int(h.Sum32() % 24)
}

// slot is the time today, in UTC, that f's daily review is queued from.
func slot(f domain.FactoryID, now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	return day.Add(time.Duration(ReviewHour(f)) * time.Hour)
}

func (r *Reviews) sweepFactory(ctx context.Context, l domain.Line) error {
	now := r.d.Clock.Now().UTC()

	due := slot(l.FactoryID, now)
	if now.Before(due) {
		return nil
	}

	last, err := r.d.Jonah.JonahReviews(ctx, l.FactoryID, 1)
	if err != nil {
		return fmt.Errorf("read the last review: %w", err)
	}

	if len(last) > 0 && !last[0].CreatedAt.Before(due) {
		return nil
	}

	spend, err := r.d.Jonah.JonahSpendSince(ctx, l.FactoryID, now.Add(-BudgetWindow))
	if err != nil {
		return fmt.Errorf("read spend: %w", err)
	}

	if l.Jonah.Budget(spend).Exhausted() {
		return nil
	}

	start, err := windowStart(ctx, r.d.Jonah, l.FactoryID, now)
	if err != nil {
		return err
	}

	items, err := r.d.Jonah.EvidenceItems(ctx, l.FactoryID, start, now, 1)
	if err != nil {
		return fmt.Errorf("look for evidence: %w", err)
	}

	if len(items) == 0 {
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
		slog.String("trigger", string(rv.Trigger)))

	return nil
}

// windowStart is where a review queued now starts reading (ADR 0039 decision 3): the end of the
// factory's last review that ended done, or FirstWindow back. A failed review never moves it.
func windowStart(ctx context.Context, reviews interface {
	LastDoneJonahReview(ctx context.Context, factory domain.FactoryID) (domain.JonahReview, error)
}, f domain.FactoryID, now time.Time,
) (time.Time, error) {
	last, err := reviews.LastDoneJonahReview(ctx, f)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return now.Add(-FirstWindow), nil
	case err != nil:
		return time.Time{}, fmt.Errorf("read the last done review: %w", err)
	case !last.WindowEnd.IsZero() && last.WindowEnd.Before(now):
		return last.WindowEnd, nil
	default:
		return now.Add(-FirstWindow), nil
	}
}

// proposeAfter is p's proposeAfter, at least 1.
func proposeAfter(p domain.JonahPolicy) int { return max(p.ProposeAfter, 1) }

// Work claims and runs every claimable review of every live organization, one at a time, and
// returns how many it finished. A review put back for a retry is claimed again in the same round,
// up to MaxReviewAttempts. An organization that fails does not hold up the others.
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
	if errors.Is(err, errRetry) {
		res = retry(rv, res)
		err = nil
	}

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

	if res.State == domain.JonahPending {
		r.log.WarnContext(ctx, "jonah review retried", slog.String("review_id", string(rv.ID)), slog.String("factory", string(rv.FactoryID)),
			slog.Int("attempt", rv.Attempts), slog.String("error", res.Error))

		return true, false, nil
	}

	r.log.InfoContext(ctx, "jonah review finished", slog.String("review_id", string(rv.ID)), slog.String("factory", string(rv.FactoryID)),
		slog.String("state", string(res.State)), slog.Int("proposals", len(stored)), slog.Int("observations", len(res.Observations)))

	return true, true, nil
}

// MaxReviewAttempts is how many times a review runs before its error ends it: the first try and
// two retries (ADR 0039 decision 3).
const MaxReviewAttempts = 3

// errRetry marks a failed result that another attempt may fix: a run that failed or was not
// carried out, or an output that does not parse.
var errRetry = errors.New("retry the review")

// retry is res, a failed result of rv's attempt, as rv's next state: pending again with the error
// kept while rv has attempts left, failed after its last.
func retry(rv domain.JonahReview, res domain.JonahReviewResult) domain.JonahReviewResult {
	if rv.Attempts < MaxReviewAttempts {
		res.State = domain.JonahPending
	}

	return res
}

// failed is a failed result with reason.
func failed(format string, a ...any) domain.JonahReviewResult {
	return domain.JonahReviewResult{State: domain.JonahFailed, Error: truncate(fmt.Sprintf(format, a...), maxReviewError)}
}

// maxReviewError keeps a failed review's error within what the store takes (2000 characters).
const maxReviewError = 2000

// review runs rv and returns the result to record (At unset). A spent budget and a version that
// is gone are failed results; a failed run and an output that does not parse are failed results
// with errRetry, which the caller turns into a retry while rv has attempts left; a read that
// failed is an error, and the review is left to its lease. A review claimed past its last attempt
// (its runs died with their goald) fails at once.
func (r *Reviews) review(ctx context.Context, org domain.OrgID, rv domain.JonahReview) (domain.JonahReviewResult, error) {
	if rv.Attempts > MaxReviewAttempts {
		return failed("the review did not finish in %d attempts: %s", MaxReviewAttempts, rv.Error), nil
	}

	l, err := r.d.Definitions.Line(ctx, rv.VersionID)
	if errors.Is(err, ports.ErrNotFound) {
		return failed("definition version %s is gone", rv.VersionID), nil
	}

	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read version %s: %w", rv.VersionID, err)
	}

	spend, err := r.d.Jonah.JonahSpendSince(ctx, rv.FactoryID, r.d.Clock.Now().UTC().Add(-BudgetWindow))
	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read spend: %w", err)
	}

	if l.Jonah.Budget(spend).Exhausted() {
		return failed("%s", domain.ErrLearningBudgetExhausted.Error()), nil
	}

	req := domain.RunRequest{
		OrgID: org, FactoryID: rv.FactoryID, Version: rv.VersionID, Kind: domain.RunKindJonah, Agent: ReviewAgent, Model: l.Jonah.Model,
		Bundle: domain.TaskKindJonah, WindowStart: rv.WindowStart, WindowEnd: rv.WindowEnd, Manual: rv.Trigger == domain.JonahManual,
		ModelConnection: l.Runtime.ModelConnection, Size: domain.SandboxSmall, Timeout: r.c.Timeout,
	}
	if l.OnSecrets() { // the model key alone (ADR 0037 decision 7)
		req.ModelConnection, req.ModelKey = "", l.Runtime.ModelKey
	}

	run, err := r.d.Runner.RunTask(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return domain.JonahReviewResult{}, fmt.Errorf("run: %w", err)
		}

		return ran(failed("the review run failed: %v", err), run), errRetry
	}

	if run.Status != domain.RunSucceeded {
		return ran(failed("the review run ended %s: %s", run.Status, run.Error), run), errRetry
	}

	out, err := parseReviewOutput(run.Output)
	if err != nil {
		return ran(failed("%v", err), run), errRetry
	}

	res, err := r.result(ctx, rv, l, out)

	return ran(res, run), err
}

// ran is res with run's id: the review's agent run, when RunTask recorded one.
func ran(res domain.JonahReviewResult, run domain.RunResult) domain.JonahReviewResult {
	res.RunID = run.RunID

	return res
}

// result checks out against rv's version and window, the factory's open observations and
// proposals and the organization's allowed models, makes its facts one memory proposal checked
// against the window's evidence items, and computes each kept proposal's before value: a done
// result.
func (r *Reviews) result(ctx context.Context, rv domain.JonahReview, l domain.Line, out reviewOutput) (domain.JonahReviewResult, error) {
	files, err := r.d.Definitions.VersionFiles(ctx, rv.VersionID)
	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read files of version %s: %w", rv.VersionID, err)
	}

	events, err := r.d.Jonah.SentBackBetween(ctx, rv.FactoryID, rv.WindowStart, rv.WindowEnd, reviewEventLimit)
	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read sent-back events: %w", err)
	}

	in := reviewInput{
		review: rv, proposeAfter: proposeAfter(l.Jonah), files: files, events: events, check: r.d.Check,
		factoryName: l.Name, today: r.d.Clock.Now().UTC(),
	}

	if len(out.Facts) > 0 {
		if in.evidence, err = r.d.Jonah.EvidenceItems(ctx, rv.FactoryID, rv.WindowStart, rv.WindowEnd, reviewEvidenceLimit); err != nil {
			return domain.JonahReviewResult{}, fmt.Errorf("read evidence items: %w", err)
		}
	}

	if in.observations, err = r.d.Jonah.OpenObservations(ctx, rv.FactoryID, reviewObservationLimit); err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read open observations: %w", err)
	}

	in.proposals, err = r.d.Jonah.Proposals(ctx, ports.ProposalQuery{Factory: rv.FactoryID, State: domain.ProposalProposed, Limit: reviewProposalLimit})
	if err != nil {
		return domain.JonahReviewResult{}, fmt.Errorf("read open proposals: %w", err)
	}

	if r.d.Policies != nil {
		policy, err := r.d.Policies.Get(ctx)
		if err != nil {
			return domain.JonahReviewResult{}, fmt.Errorf("read the organization's policy: %w", err)
		}

		in.models = policy.AllowedModels
	}

	proposals, observations := checkOutput(in, out, func(i int, reason, detail string) {
		r.log.WarnContext(ctx, "jonah proposal dropped", slog.String("review_id", string(rv.ID)), slog.Int("proposal", i),
			slog.String("reason", reason), slog.String("detail", truncate(detail, maxReviewError)))
	})

	checker := definitionChecker{in: in}
	if memory, ok := memoryProposal(&checker, out.Facts, func(i int, reason, detail string) {
		r.log.WarnContext(ctx, "jonah fact dropped", slog.String("review_id", string(rv.ID)), slog.Int("fact", i),
			slog.String("reason", reason), slog.String("detail", truncate(detail, maxReviewError)))
	}); ok {
		proposals = append(proposals, memory)
	}

	if err := r.estimate(ctx, rv, proposals); err != nil {
		return domain.JonahReviewResult{}, err
	}

	return domain.JonahReviewResult{State: domain.JonahDone, Proposals: proposals, Observations: observations}, nil
}

// estimate computes each of proposals' before value over rv's window; a memory proposal has no
// metric and none.
func (r *Reviews) estimate(ctx context.Context, rv domain.JonahReview, proposals []domain.NewProposal) error {
	flows := map[string]domain.StationFlow{}

	for i := range proposals {
		e := &proposals[i].Estimate
		if e.Metric == "" {
			continue
		}

		f, ok := flows[e.Station]
		if !ok {
			var err error
			if f, err = r.d.Jonah.StationFlow(ctx, rv.FactoryID, e.Station, rv.WindowStart, rv.WindowEnd); err != nil {
				return fmt.Errorf("measure station %s: %w", e.Station, err)
			}

			flows[e.Station] = f
		}

		e.Before = before(e.Metric, f)
	}

	return nil
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
	case domain.MetricCostPerItem: // a watch's metric, never a proposal's estimate
		return 0
	default:
		return 0
	}
}
