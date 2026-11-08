package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The CI poller's bounds (ADR 0044 decision 9). A poll is leased one at a time for ciPollLease,
// well above one Checks call's ciChecksTimeout, so a live owner records before its lease ends.
const (
	ciPollLease     = time.Minute
	ciChecksTimeout = 20 * time.Second
)

// PollCI polls CI for the open CI waits of every active organization (ADR 0044 decision 9). In
// each it leases the due commit polls one at a time under the engine's holder, up to SweepLimit,
// makes one CodeHost.Checks call per poll and decides every open wait on that commit from its
// answer (pollCommit). Instances share the polling through the leases; a poll whose owner died is
// due again once its lease ends. It returns how many polls it polled.
func (e *Engine) PollCI(ctx context.Context) (int, error) {
	orgs, err := e.orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("poll ci: list organizations: %w", err)
	}

	var (
		polled int
		errs   []error
	)

	for _, org := range orgs {
		octx := domain.WithOrg(ctx, org)

		for range e.cfg.SweepLimit {
			polls, err := e.store.LeaseDueCIPolls(octx, e.clock.Now(), e.holder, ciPollLease, 1)
			if err != nil {
				errs = append(errs, fmt.Errorf("poll ci in organization %s: lease: %w", org, err))

				break
			}

			if len(polls) == 0 {
				break
			}

			polled++

			if err := e.pollCommit(octx, polls[0]); err != nil {
				errs = append(errs, fmt.Errorf("poll ci in organization %s: %s@%s: %w", org, polls[0].Repo, polls[0].SHA, err))
			}
		}
	}

	return polled, errors.Join(errs...)
}

// ciWaitOn is one open wait a poll decides, with the limits of its station's waitFor.
type ciWaitOn struct {
	wait   domain.CIWaitRecord
	limits domain.CIWait
}

// pollCommit polls the commit of the leased poll p once and decides every open wait on it:
//
//   - every check finished: CIPassed when none failed, else CIFailed;
//   - Max passed since the wait started: CITimedOut;
//   - no check seen within Grace after the commit was first polled, or the wait started if
//     earlier: CINoCI, so a later wait on a commit that showed no CI ends at its first poll;
//   - the token cannot read CI (ports.ErrCINotReadable): CIUnreadable;
//   - the pull request's head moved: the wait moves to the new head (Store.ReplaceCIWaitSHA).
//
// A rate limit postpones the poll to its RetryAt, and any other error backs it off; both keep
// every wait. The interval doubles while the checks stay the same and resets to
// domain.CIPollMinInterval when they change. Each wait that ends goes to ciWaitEnded.
func (e *Engine) pollCommit(ctx context.Context, p domain.CIPoll) error {
	ws, err := e.store.OpenCIWaitsOnPoll(ctx, p.CIPollKey)
	if err != nil {
		return e.recordCIError(ctx, p, fmt.Errorf("open waits: %w", err))
	}

	if len(ws) == 0 {
		return e.recordCIPoll(ctx, p, domain.CIPollResult{Interval: ciBackoff(p.Interval)})
	}

	waits, target, url, err := e.ciTarget(ctx, p, ws)

	switch {
	case errors.Is(err, errCIUnreachable):
		e.log.WarnContext(ctx, "ci poll cannot read its repository", slog.String("repository", p.Repo), slog.String("sha", p.SHA), slog.Any("error", err))
		e.endCIWaits(ctx, waits, domain.CIUnreadable, ports.CommitChecks{})

		return e.recordCIPoll(ctx, p, domain.CIPollResult{Error: err.Error(), Interval: ciBackoff(p.Interval)})
	case err != nil:
		return e.recordCIError(ctx, p, err)
	}

	cctx, cancel := context.WithTimeout(ctx, ciChecksTimeout)
	checks, err := target.host.Checks(cctx, target.repo, target.token, p.SHA, url)

	cancel()

	var limited *ports.RateLimitedError

	switch {
	case errors.As(err, &limited):
		next := limited.RetryAt
		if !next.After(e.clock.Now()) {
			next = e.clock.Now().Add(ciInterval(p.Interval))
		}

		return e.recordCIPoll(ctx, p, domain.CIPollResult{Error: err.Error(), NextPollAt: next, Interval: ciInterval(p.Interval)})
	case errors.Is(err, ports.ErrCINotReadable):
		e.endCIWaits(ctx, waits, domain.CIUnreadable, ports.CommitChecks{})

		return e.recordCIPoll(ctx, p, domain.CIPollResult{Error: err.Error(), Interval: ciBackoff(p.Interval)})
	case err != nil:
		return e.recordCIError(ctx, p, fmt.Errorf("checks: %w", err))
	}

	if checks.Head != "" && checks.Head != p.SHA {
		e.followCIHead(ctx, waits, url, checks.Head)

		return e.recordCIPoll(ctx, p, domain.CIPollResult{Interval: domain.CIPollMinInterval})
	}

	return e.decideCIWaits(ctx, p, waits, checks)
}

// decideCIWaits ends each of waits that checks, read now, settle, and records them on p.
func (e *Engine) decideCIWaits(ctx context.Context, p domain.CIPoll, waits []ciWaitOn, checks ports.CommitChecks) error {
	now := e.clock.Now()

	raw, err := json.Marshal(checks)
	if err != nil {
		return e.recordCIError(ctx, p, fmt.Errorf("encode the checks: %w", err))
	}

	r := domain.CIPollResult{Checks: raw, Interval: domain.CIPollMinInterval}
	if p.FirstPolledAt.IsZero() {
		p.FirstPolledAt, r.FirstPolledAt = now, now
	}

	if sameChecks(p.Checks, raw) {
		r.Interval = ciBackoff(p.Interval)
	}

	seen := p.FirstCheckSeenAt
	if len(checks.Checks) > 0 && seen.IsZero() {
		seen, r.FirstCheckSeenAt = now, now
	}

	for _, w := range waits {
		if outcome, ok := ciOutcome(checks, w, p.FirstPolledAt, seen, now); ok {
			e.endCIWaits(ctx, []ciWaitOn{w}, outcome, checks)
		}
	}

	return e.recordCIPoll(ctx, p, r)
}

// ciOutcome is how w ends with checks read at now, the commit first polled at polled and its
// first check seen at seen; false while it stays open. Max counts from the wait's start, and the
// grace from the earlier of the commit's first poll and the wait's start (ADR 0044 decision 7).
func ciOutcome(checks ports.CommitChecks, w ciWaitOn, polled, seen, now time.Time) (domain.CIOutcome, bool) {
	since := now.Sub(w.wait.StartedAt)

	graceFrom := w.wait.StartedAt
	if !polled.IsZero() && polled.Before(graceFrom) {
		graceFrom = polled
	}

	switch {
	case checks.Finished() && len(checks.Failed()) > 0:
		return domain.CIFailed, true
	case checks.Finished():
		return domain.CIPassed, true
	case since >= w.limits.Max:
		return domain.CITimedOut, true
	case len(checks.Checks) == 0 && seen.IsZero() && now.Sub(graceFrom) >= w.limits.Grace:
		return domain.CINoCI, true
	}

	return "", false
}

// errCIUnreachable means no open wait on a poll names a repository goald can read CI of: none
// is on a loaded line, or the line has no code host or token for the repository. Its waits end
// as CIUnreadable rather than being polled forever.
var errCIUnreachable = errors.New("no line reads the repository")

// ciTarget is what a poll reads: each open wait with its station's limits, and the repository
// with its adapter and token, looked up as repositoryHost does on the line of the first wait
// whose line is loaded, with the first wait's pull request URL, if any, so a moved head shows.
// A wait whose line is not loaded, or whose station has no waitFor, keeps the default limits.
func (e *Engine) ciTarget(ctx context.Context, p domain.CIPoll, ws []domain.CIWaitRecord) ([]ciWaitOn, mergeTarget, string, error) {
	waits := make([]ciWaitOn, 0, len(ws))

	var (
		line   domain.Line
		loaded bool
		url    string
	)

	for _, w := range ws {
		on := ciWaitOn{wait: w, limits: domain.CIWait{Grace: domain.DefaultCIGrace, Max: domain.DefaultCIMax}}

		if url == "" {
			url = w.PRURL
		}

		it, err := e.store.Item(ctx, w.Item)

		switch {
		case errors.Is(err, ports.ErrNotFound):
			waits = append(waits, on)

			continue
		case err != nil:
			return nil, mergeTarget{}, "", fmt.Errorf("item %s: %w", w.Item, err)
		}

		if l, err := e.lineIn(ctx, it.FactoryID); err == nil {
			if st, ok := l.Station(w.Station); ok && st.WaitsForCI() {
				on.limits = ciLimits(st.WaitFor)
			}

			if !loaded {
				line, loaded = l, true
			}
		}

		waits = append(waits, on)
	}

	if !loaded {
		return waits, mergeTarget{}, "", fmt.Errorf("the waits' lines are not loaded: %w", errCIUnreachable)
	}

	target, err := e.repositoryHost(ctx, line, repositoryURL(p.Repo))

	switch {
	case errors.Is(err, ErrNoCodeHost), errors.Is(err, ports.ErrNotMergeable):
		return waits, mergeTarget{}, "", fmt.Errorf("%w: %w", errCIUnreachable, err)
	case err != nil:
		return waits, mergeTarget{}, "", fmt.Errorf("the repository's code host: %w", err)
	}

	return waits, target, url, nil
}

// ciLimits are a station's waitFor with the defaults for what it leaves zero.
func ciLimits(c domain.CIWait) domain.CIWait {
	if c.Grace <= 0 {
		c.Grace = domain.DefaultCIGrace
	}

	if c.Max <= 0 {
		c.Max = domain.DefaultCIMax
	}

	if c.Rounds <= 0 {
		c.Rounds = domain.DefaultCIRounds
	}

	return c
}

// followCIHead moves the waits on the pull request at url to head, its new head; the other
// waits stay for the poll's next round.
func (e *Engine) followCIHead(ctx context.Context, waits []ciWaitOn, url, head string) {
	for _, w := range waits {
		if w.wait.PRURL != url {
			continue
		}

		moved, err := e.store.ReplaceCIWaitSHA(ctx, w.wait.ID, head, "", e.clock.Now())
		if err != nil {
			e.log.WarnContext(ctx, "ci wait not moved to the new head", slog.String("wait", string(w.wait.ID)), slog.Any("error", err))

			continue
		}

		e.log.InfoContext(ctx, "ci wait moved to the new head", slog.String("item", string(w.wait.Item)), slog.String("wait", string(w.wait.ID)),
			slog.String("new_wait", string(moved.ID)), slog.String("sha", w.wait.SHA), slog.String("head", head))
	}
}

// endCIWaits ends waits with outcome and hands each one this call ended to ciWaitEnded; a wait
// another instance ended meanwhile is left alone.
func (e *Engine) endCIWaits(ctx context.Context, waits []ciWaitOn, outcome domain.CIOutcome, checks ports.CommitChecks) {
	now := e.clock.Now()

	for _, w := range waits {
		n, err := e.store.EndCIWaits(ctx, []domain.CIWaitID{w.wait.ID}, outcome, now)
		if err != nil {
			e.log.WarnContext(ctx, "ci wait not ended", slog.String("wait", string(w.wait.ID)), slog.Any("error", err))

			continue
		}

		if n == 1 {
			w.wait.EndedAt, w.wait.Outcome = now, outcome
			e.ciWaitEnded(ctx, w.wait, outcome, checks)
		}
	}
}

// ciWaitEnded is called once for each CI wait the poller ended, with its outcome and the checks
// that decided it (none for CIUnreadable), and logs it. The poller holds no item lease, so it
// acts on no outcome: the item's next claim does, under its lease (ciGate), starting the run a
// before wait gated, or applying (applyHeld) or running the station again on what an after wait
// held.
func (e *Engine) ciWaitEnded(ctx context.Context, w domain.CIWaitRecord, outcome domain.CIOutcome, checks ports.CommitChecks) {
	failed := make([]string, 0, len(checks.Failed()))
	for _, c := range checks.Failed() {
		failed = append(failed, c.Name)
	}

	e.log.InfoContext(ctx, "ci wait ended", slog.String("item", string(w.Item)), slog.String("station", w.Station),
		slog.String("wait", string(w.ID)), slog.String("repository", w.Repo), slog.String("sha", w.SHA),
		slog.String("outcome", string(outcome)), slog.Any("failed", failed))
}

// recordCIError records err on p, backing its interval off; the waits stay open.
func (e *Engine) recordCIError(ctx context.Context, p domain.CIPoll, err error) error {
	if rerr := e.recordCIPoll(ctx, p, domain.CIPollResult{Error: err.Error(), Interval: ciBackoff(p.Interval)}); rerr != nil {
		return errors.Join(err, rerr)
	}

	return err
}

// recordCIPoll records r on p and releases its lease, due r.Interval from now unless r says when.
func (e *Engine) recordCIPoll(ctx context.Context, p domain.CIPoll, r domain.CIPollResult) error {
	if r.NextPollAt.IsZero() {
		r.NextPollAt = e.clock.Now().Add(r.Interval)
	}

	if err := e.store.RecordCIPoll(ctx, p.CIPollKey, e.holder, r); err != nil {
		return fmt.Errorf("record: %w", err)
	}

	return nil
}

// ciInterval is d within domain.CIPollMinInterval and domain.CIPollMaxInterval.
func ciInterval(d time.Duration) time.Duration {
	return min(max(d, domain.CIPollMinInterval), domain.CIPollMaxInterval)
}

// ciBackoff is the interval after d when nothing changed: twice d, within the bounds.
func ciBackoff(d time.Duration) time.Duration { return ciInterval(2 * ciInterval(d)) }

// sameChecks reports whether the recorded checks old (as the store returns them, normalized or
// not) are raw, the checks just read.
func sameChecks(old json.RawMessage, raw []byte) bool {
	if len(old) == 0 {
		return false
	}

	var c ports.CommitChecks
	if json.Unmarshal(old, &c) != nil {
		return false
	}

	again, err := json.Marshal(c)

	return err == nil && bytes.Equal(again, raw)
}
