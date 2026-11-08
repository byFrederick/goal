package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// resumeOutcome is what became of a claim that tried to resume a run (resume).
type resumeOutcome int

const (
	// resumeSkipped: nothing was reused, and the claim goes on as if no run had succeeded.
	resumeSkipped resumeOutcome = iota
	// resumeSettled: the claim ended, the run's output held for CI, applied, or the item blocked,
	// released or no longer this instance's.
	resumeSettled
	// resumeRetry: the run's output failed its checks again, and the station runs its agent with
	// the rejection, as its second attempt.
	resumeRetry
)

// resumableRun is the claim's run to resume, when there is one: a succeeded run whose output
// was never applied because its claim ended under it, most often a shutdown while the station's
// checks ran (finishRun records the run before runOnce's then runs them). It is the item's latest
// run, at this station, succeeded, and after its end the item only saw its own run's events
// (check_failed aside), claims, releases at shutdown or unload, and lease expiries, with at least
// one of those ending its claim; no CI wait holds it. Its pushed heads are not compared here
// (headsKept). False on anything else, and when the store cannot say, which is logged.
func (e *Engine) resumableRun(ctx context.Context, log *slog.Logger, c claimed) (domain.Run, bool) {
	runs, err := e.store.Runs(ctx, c.item.ID, ciRunsBound)
	if err != nil {
		log.WarnContext(ctx, "run not resumed", slog.Any("error", err))
		return domain.Run{}, false
	}

	if len(runs) == 0 {
		return domain.Run{}, false
	}

	r := runs[len(runs)-1]
	if r.Station != c.station.ID || r.Result.Status != domain.RunSucceeded {
		return domain.Run{}, false
	}

	events, err := e.itemEvents(ctx, c.item.ID)
	if err != nil {
		log.WarnContext(ctx, "run not resumed", slog.String("run", string(r.ID)), slog.Any("error", err))
		return domain.Run{}, false
	}

	if !claimCutAfter(events, r.ID) {
		return domain.Run{}, false
	}

	waits, err := e.store.CIWaits(ctx, c.item.ID, c.station.ID)
	if err != nil {
		log.WarnContext(ctx, "run not resumed", slog.String("run", string(r.ID)), slog.Any("error", err))
		return domain.Run{}, false
	}

	if slices.ContainsFunc(waits, func(w domain.CIWaitRecord) bool { return holds(w, r.ID) }) {
		return domain.Run{}, false
	}

	return r, true
}

// claimCutAfter reports whether, after run's run_finished event, events hold only what a claim
// cut under the run leaves: the run's own events other than check_failed, claims, releases at
// shutdown or unload and lease expiries, with at least one of those two ending a claim. Anything
// else (a move, a retry, a decision, a block, an escalation, a rejected output, a person's note)
// means the run is not resumed.
func claimCutAfter(events []domain.Event, run domain.RunID) bool {
	i := slices.IndexFunc(events, func(ev domain.Event) bool { return ev.RunID == run && ev.Kind == domain.EventRunFinished })
	if i < 0 {
		return false
	}

	cut := false

	for _, ev := range events[i+1:] {
		switch {
		case ev.Kind == domain.EventCheckFailed:
			return false
		case ev.RunID == run, ev.Kind == domain.EventClaimed:
		case ev.Kind == domain.EventLeaseExpired:
			cut = true
		case ev.Kind == domain.EventReleased && (ev.Data[keyReason] == reasonShutdown || ev.Data[keyReason] == reasonUnloaded):
			cut = true
		default:
			return false
		}
	}

	return cut
}

// holds reports whether the CI wait w holds run (ciHeld).
func holds(w domain.CIWaitRecord, run domain.RunID) bool {
	var h ciHeld

	return len(w.Held) > 0 && json.Unmarshal(w.Held, &h) == nil && h.Run == run
}

// headsKept reports whether the head of the claim's branch on the code host is still what r
// pushed, in each repository it pushed to (pushedHeads). False when a head moved, cannot be
// read, or the line has no code host for a repository; each is logged.
func (e *Engine) headsKept(ctx context.Context, log *slog.Logger, c claimed, r domain.Run) bool {
	branch := cmp.Or(c.branch, c.item.Branch())

	for _, cm := range pushedHeads([]domain.Run{r}) {
		head, err := e.pushedBranchHead(ctx, c.line, cm.repo, branch)

		switch {
		case err != nil:
			log.WarnContext(ctx, "run not resumed", slog.String("run", string(r.ID)), slog.String("repository", cm.repo), slog.Any("error", err))
			return false
		case head != cm.sha:
			log.InfoContext(ctx, "run not resumed; the branch moved", slog.String("run", string(r.ID)),
				slog.String("repository", cm.repo), slog.String("pushed", cm.sha), slog.String("head", head))

			return false
		}
	}

	return true
}

// pushedBranchHead is the head of branch in the repository key names, read with the host and
// token repositoryHost picks.
func (e *Engine) pushedBranchHead(ctx context.Context, l domain.Line, key, branch string) (string, error) {
	t, err := e.repositoryHost(ctx, l, repositoryURL(key))
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, ciChecksTimeout)
	defer cancel()

	sha, err := t.host.BranchHead(ctx, t.repo, t.token, branch)
	if err != nil {
		return "", fmt.Errorf("read the head of branch %s: %w", branch, err)
	}

	return sha, nil
}

// resume goes on with r, the claim's resumable run (resumableRun) whose pushed heads are still
// its branch's (headsKept), as runOnce would have after its agent: no agent starts. Its output is
// checked against the current line, a leftover box of the cut claim is deleted, and in a new box
// opened and prepared under r's ID (so the reaper keeps it: Store.RunsOver) the station's checks
// run again, recording their events on r, while the lease is heartbeated. On pass r is settled:
// held for CI with an after wait (holdForCI), or finished. An output that fails its checks is
// recorded as the claim's first attempt and resumeRetry returned with the rejection for the
// second. When the box cannot be opened or a check cannot be run, nothing is reused
// (resumeSkipped).
func (e *Engine) resume(ctx context.Context, log *slog.Logger, c claimed, r domain.Run) (string, resumeOutcome) {
	log = log.With(slog.String("run", string(r.ID)))
	log.InfoContext(ctx, "run resumed")

	var b box
	defer e.deleteBox(ctx, log, &b)

	var chk checked

	err := e.underLease(ctx, c.lease, func(ctx context.Context) error {
		got, err := e.check(ctx, c, r.Result.Output)
		if err != nil {
			return &outputError{err: err}
		}

		if err := e.sandboxes.Delete(ctx, r.ID); err != nil {
			log.WarnContext(ctx, "delete the cut claim's sandbox", slog.Any("error", err))
		}

		t := runTask{RunRequest: c.request(""), item: c.item, box: &b, work: c.item.Work()}

		o, err := e.openRun(ctx, t, r.ID)
		if err != nil {
			return err
		}

		if err := e.runChecks(ctx, c, r.ID, &b, o.scrub); err != nil {
			return err
		}

		if err := e.follow(ctx, c, got); err != nil {
			return err
		}

		chk = got

		return nil
	})

	if e.cut(ctx, log, c, err) {
		return "", resumeSettled
	}

	var rejected *outputError

	switch {
	case errors.As(err, &rejected):
		if !e.outputFailed(ctx, log, c, 1, rejected.err) {
			return "", resumeSettled
		}

		return rejectionOf(rejected.err), resumeRetry
	case err != nil:
		log.WarnContext(ctx, "run not resumed; the station runs again", slog.Any("error", err))
		return "", resumeSkipped
	}

	e.settle(ctx, log, c, domain.RunResult{RunID: r.ID, Status: domain.RunSucceeded, Output: r.Result.Output}, chk)

	return "", resumeSettled
}

// underLease runs fn while heartbeating lease, as runTask does for a run. It returns ErrLeaseLost
// when the lease was lost, which also ends fn's ctx.
func (e *Engine) underLease(ctx context.Context, lease domain.Lease, fn func(context.Context) error) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	var (
		beats sync.WaitGroup
		lost  bool
	)

	beatCtx, stopBeat := context.WithCancel(ctx)

	beats.Go(func() { lost = e.heartbeat(beatCtx, lease, stop) })

	err := fn(ctx)

	stopBeat()
	beats.Wait()

	if lost {
		return ports.ErrLeaseLost
	}

	return err
}

// rejectionOf is what the next attempt's brief says about an output that failed its checks.
func rejectionOf(err error) string {
	return "Your previous attempt was rejected: " + err.Error() +
		"\nFix that and return output that satisfies the station contract."
}
