// Package jonah is Jonah's goald loop (ADR 0025 decision 2, ADR 0002's phase 5 amendment): it
// samples finished production runs for the judges of their definition version, queues one score
// per sampled run and judge, and works the queue under leases, each score a short judge run. Any
// goald instance may run it: sampling is a hash every instance computes alike, a second enqueue of
// the same judge and run changes nothing, and a score whose worker died returns to the queue when
// its lease ends, so the loop keeps no state of its own (AGENTS.md rule 4).
package jonah

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Orgs lists the organizations the loop serves; ports.Tenancy satisfies it.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// Runs reads one run of the organization in ctx; ports.Store satisfies it.
type Runs interface {
	Run(ctx context.Context, id domain.RunID) (domain.Run, error)
}

// Deps are the ports the loop works through.
type Deps struct {
	Orgs     Orgs
	Runs     Runs
	Finished ports.FinishedRuns
	Judges   ports.Judges
	Scores   ports.JudgeScores
	Runner   ports.JudgeRunner
	Clock    ports.Clock
	// Log may be nil.
	Log *slog.Logger
}

// Config sets the loops' times; the eval loop reads Timeout and Margin as the judge loop does,
// for one eval try instead of one judge run.
type Config struct {
	// Lookback is how far back a sweep reads finished runs; each sweep re-reads it, and a run that
	// ended while no goald swept for longer than it is never sampled.
	Lookback time.Duration
	// Timeout bounds one judge run; a score takes at most two (one retry).
	Timeout time.Duration
	// Margin is the lease's time beyond the judge runs, for reading and recording.
	Margin time.Duration
	// Poll is how often the eval loop reads a running try's eval run to see whether it was
	// cancelled; it must be positive.
	Poll time.Duration
}

// attempts is how many judge runs a score gets: the first and one retry after an
// infrastructure error.
const attempts = 2

// Loop samples finished runs and scores them with judges.
type Loop struct {
	d   Deps
	c   Config
	log *slog.Logger
}

// New returns a loop over d with the times of c.
func New(d Deps, c Config) *Loop {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Loop{d: d, c: c, log: log}
}

// Sampled reports whether the judge with slug judge scores run at sample share share: the first
// 8 bytes of SHA-256 over the run id and the slug, as a share of 2^64, fall below share. Every
// goald makes the same choice, 0 never samples and 1 always does.
func Sampled(run domain.RunID, judge string, share float64) bool {
	switch {
	case share <= 0:
		return false
	case share >= 1:
		return true
	}

	h := sha256.Sum256([]byte(string(run) + "\x00" + judge))

	return float64(binary.BigEndian.Uint64(h[:8]))/math.Exp2(64) < share
}

// Run sweeps and works the queue at once and then each time every passes, until ctx ends;
// failures are logged and retried on the next round.
func (l *Loop) Run(ctx context.Context, every time.Duration) {
	t := l.d.Clock.NewTicker(every)
	defer t.Stop()

	for {
		if err := l.Sweep(ctx); err != nil && ctx.Err() == nil {
			l.log.WarnContext(ctx, "judge sweep", slog.Any("error", err))
		}

		if _, err := l.Work(ctx); err != nil && ctx.Err() == nil {
			l.log.WarnContext(ctx, "judge work", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

// Sweep queues a score for each finished production run of every live organization that ended
// within the lookback, for each judge of the run's version that is enabled, scores the run's
// agent and samples the run. Runs of no known version are left alone. An organization that fails
// does not hold up the others; the error names each one.
func (l *Loop) Sweep(ctx context.Context) error {
	orgs, err := l.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return fmt.Errorf("judge sweep: list organizations: %w", err)
	}

	var errs []error

	for _, org := range orgs {
		if err := l.sweepOrg(domain.WithOrg(ctx, org)); err != nil {
			errs = append(errs, fmt.Errorf("judge sweep of org %s: %w", org, err))
		}
	}

	return errors.Join(errs...)
}

type versionKey struct {
	factory domain.FactoryID
	version domain.DefinitionVersionID
}

func (l *Loop) sweepOrg(ctx context.Context) error {
	now := l.d.Clock.Now()

	runs, err := l.d.Finished.EndedBetween(ctx, now.Add(-l.c.Lookback), now)
	if err != nil {
		return fmt.Errorf("list finished runs: %w", err)
	}

	judges := map[versionKey][]domain.Judge{}

	for _, r := range runs {
		if r.DefinitionVersionID == "" {
			continue
		}

		key := versionKey{r.FactoryID, r.DefinitionVersionID}

		js, ok := judges[key]
		if !ok {
			if js, err = l.d.Judges.Judges(ctx, r.FactoryID, r.DefinitionVersionID); err != nil {
				return fmt.Errorf("judges of version %s: %w", r.DefinitionVersionID, err)
			}

			judges[key] = js
		}

		for _, j := range js {
			if !j.Enabled || !j.Scores(r.Agent) || !Sampled(r.ID, j.Slug, j.Sample) {
				continue
			}

			if _, err := l.d.Scores.Enqueue(ctx, domain.NewJudgeScore{
				FactoryID: r.FactoryID, VersionID: r.DefinitionVersionID, Judge: j.Slug, RunID: r.ID, ItemID: r.ItemID, At: now,
			}); err != nil {
				return fmt.Errorf("enqueue judge %s of run %s: %w", j.Slug, r.ID, err)
			}
		}
	}

	return nil
}

// Try queues judge slug of version as a try on the n newest finished runs of its agents in
// factory, for the organization in ctx, and returns the tries. A try names the run's item but no
// run, so it never counts in the judge's production figures; Work judges it against that item's
// newest finished run of the judge's agents that ended by the try's queueing. ErrNotFound when
// the version has no such judge.
func (l *Loop) Try(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID, slug string, n int) ([]domain.JudgeScore, error) {
	j, err := l.judge(ctx, factory, version, slug)
	if err != nil {
		return nil, fmt.Errorf("try judge %s: %w", slug, err)
	}

	now := l.d.Clock.Now()

	runs, err := l.d.Finished.Latest(ctx, ports.FinishedRunQuery{Factory: factory, Agents: j.Agents, EndedBy: now, Limit: n})
	if err != nil {
		return nil, fmt.Errorf("try judge %s: list finished runs: %w", slug, err)
	}

	tries := make([]domain.JudgeScore, 0, len(runs))

	for _, r := range runs {
		s, err := l.d.Scores.Enqueue(ctx, domain.NewJudgeScore{FactoryID: factory, VersionID: version, Judge: slug, ItemID: r.ItemID, At: now})
		if err != nil {
			return nil, fmt.Errorf("try judge %s on run %s: %w", slug, r.ID, err)
		}

		tries = append(tries, s)
	}

	return tries, nil
}

// judge is the judge slug of version, ErrNotFound when it has none.
func (l *Loop) judge(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID, slug string) (domain.Judge, error) {
	js, err := l.d.Judges.Judges(ctx, factory, version)
	if err != nil {
		return domain.Judge{}, err
	}

	for _, j := range js {
		if j.Slug == slug {
			return j, nil
		}
	}

	return domain.Judge{}, fmt.Errorf("version %s has no judge %s: %w", version, slug, ports.ErrNotFound)
}

// Work claims and scores every due score of every live organization, one at a time, and returns
// how many it recorded. A score whose lease was lost meanwhile is logged and left to the claim
// that took it. An organization that fails does not hold up the others.
func (l *Loop) Work(ctx context.Context) (int, error) {
	orgs, err := l.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("judge work: list organizations: %w", err)
	}

	var (
		worked int
		errs   []error
	)

	for _, org := range orgs {
		octx := domain.WithOrg(ctx, org)

		for ctx.Err() == nil {
			claimed, recorded, err := l.workOne(octx)
			if err != nil {
				errs = append(errs, fmt.Errorf("judge work of org %s: %w", org, err))

				break
			}

			if !claimed {
				break
			}

			if recorded {
				worked++
			}
		}
	}

	return worked, errors.Join(errs...)
}

// workOne claims the next due score and scores it: whether it claimed one and whether its result
// was recorded.
func (l *Loop) workOne(ctx context.Context) (claimed, recorded bool, err error) {
	now := l.d.Clock.Now()

	c, err := l.d.Scores.Claim(ctx, now, now.Add(attempts*l.c.Timeout+l.c.Margin))
	if err != nil {
		return false, false, fmt.Errorf("claim judge score: %w", err)
	}

	if c == nil {
		return false, false, nil
	}

	res, err := l.score(ctx, c.Score)
	if err != nil {
		// Left running: the lease ends and another claim takes the score again.
		return true, false, err
	}

	res.At = l.d.Clock.Now()

	_, err = l.d.Scores.Record(ctx, c.Lease, res)
	if errors.Is(err, ports.ErrLeaseLost) {
		l.log.WarnContext(ctx, "judge score lease lost", slog.String("score_id", string(c.Score.ID)), slog.String("judge", c.Score.Judge))

		return true, false, nil
	}

	if err != nil {
		return true, false, fmt.Errorf("record judge score %s: %w", c.Score.ID, err)
	}

	l.log.InfoContext(ctx, "judge score recorded", slog.String("score_id", string(c.Score.ID)), slog.String("judge", c.Score.Judge),
		slog.String("state", string(res.State)), slog.Bool("passed", res.Passed))

	return true, true, nil
}

// failure is an error result with reason.
func failure(format string, a ...any) domain.JudgeResult {
	return domain.JudgeResult{State: domain.JudgeScoreError, Reason: truncate(fmt.Sprintf(format, a...), domain.MaxJudgeReason)}
}

// score runs s's judge and returns the result to record (At unset). Something s names that is
// gone is an error result; a read that failed is an error, and the score is left to its lease.
func (l *Loop) score(ctx context.Context, s domain.JudgeScore) (domain.JudgeResult, error) {
	j, err := l.judge(ctx, s.FactoryID, s.VersionID, s.Judge)
	if errors.Is(err, ports.ErrNotFound) {
		return failure("judge %s is not in definition version %s", s.Judge, s.VersionID), nil
	}

	if err != nil {
		return domain.JudgeResult{}, fmt.Errorf("judge score %s: %w", s.ID, err)
	}

	run, ok, err := l.subject(ctx, s, j)
	if err != nil {
		return domain.JudgeResult{}, fmt.Errorf("judge score %s: %w", s.ID, err)
	}

	if !ok {
		return failure("no run to judge"), nil
	}

	return l.runJudge(ctx, ports.JudgeTask{Score: s, Judge: j, Run: run}), nil
}

// subject is the run s judges: its own, or for a try its item's newest finished run of j's agents
// that ended by the try's queueing. False when there is none.
func (l *Loop) subject(ctx context.Context, s domain.JudgeScore, j domain.Judge) (domain.Run, bool, error) {
	if s.RunID != "" {
		r, err := l.d.Runs.Run(ctx, s.RunID)
		if errors.Is(err, ports.ErrNotFound) {
			return domain.Run{}, false, nil
		}

		return r, err == nil, err
	}

	if s.ItemID == "" {
		return domain.Run{}, false, nil
	}

	runs, err := l.d.Finished.Latest(ctx, ports.FinishedRunQuery{Factory: s.FactoryID, Agents: j.Agents, Item: s.ItemID, EndedBy: s.CreatedAt, Limit: 1})
	if err != nil || len(runs) == 0 {
		return domain.Run{}, false, err
	}

	return runs[0], true, nil
}

// runJudge runs t's judge, once more after an infrastructure error, and maps its answer to the
// judge's outcome. The cost of every attempt counts.
func (l *Loop) runJudge(ctx context.Context, t ports.JudgeTask) domain.JudgeResult {
	var (
		cost float64
		last error
	)

	for range attempts {
		out, err := l.attempt(ctx, t)
		cost += out.CostUSD

		if err != nil {
			last = err

			l.log.WarnContext(ctx, "judge run failed", slog.String("score_id", string(t.Score.ID)), slog.String("judge", t.Judge.Slug), slog.Any("error", err))

			continue
		}

		o, ok := t.Judge.Outcome(out.Outcome)
		if !ok {
			r := failure("the judge answered %q, which is not one of its outcomes", truncate(out.Outcome, domain.MaxJudgeOutcome))
			r.CostUSD, r.JudgeRunID = cost, out.RunID

			return r
		}

		return domain.JudgeResult{
			State: domain.JudgeScoreScored, Outcome: o.Label, Score: o.Score, Passed: t.Judge.Passes(o.Score),
			Reason: truncate(out.Reason, domain.MaxJudgeReason), CostUSD: cost, JudgeRunID: out.RunID,
		}
	}

	r := failure("the judge run failed twice: %v", last)
	r.CostUSD = cost

	return r
}

// attempt is one judge run, bounded by the timeout.
func (l *Loop) attempt(ctx context.Context, t ports.JudgeTask) (ports.JudgeRun, error) {
	ctx, cancel := context.WithTimeout(ctx, l.c.Timeout)
	defer cancel()

	return l.d.Runner.RunJudge(ctx, t)
}

// truncate cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}

	return s[:n]
}
