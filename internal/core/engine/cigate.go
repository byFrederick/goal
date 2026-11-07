package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ciRunsBound bounds the runs ciCommits reads of an item, oldest first: far above any item's
// run count, so the latest pushed head is never left out.
const ciRunsBound = 10000

// ciBeforeVisit is the visit key of every before wait: a wait is settled by its commit, so one
// key serves every visit to the station.
const ciBeforeVisit = "before"

// ciCommit is one commit the gate before a run waits on: the repository's key, the commit and
// the pull request in that repository whose head it is, if any.
type ciCommit struct {
	repo, sha, prURL string
}

// ciGate is the wait before a run at a station with waitFor: ci (ADR 0044 decisions 2, 4 and
// 8), run once per claim before anything else, so no box or run exists while it waits. It
// reports whether the run may start and, when it may, the `## CI` section its brief starts
// with:
//
//   - a station without waitFor, or an item with no pushed commit (ciCommits): the run starts
//     with no section;
//   - a commit whose before wait ended unreadable since the item's latest move or retry: the
//     item blocks for a person, naming the token and the permission it needs (ciUnreadable);
//   - a commit with no ended wait: a before wait opens on it and the item goes back to its
//     station's queue, which the claim skips until the poller ends the wait;
//   - every commit's wait ended, and the visit's latest round of after waits holds a run whose
//     CI passed or got none (heldRun, ADR 0044 decision 3): no run starts, and the held run's
//     output is applied (applyHeld);
//   - every commit's wait ended otherwise: the run starts, its brief listing each commit's checks.
//
// It returns the section, the held run to apply ("" for none) and whether to go on.
func (e *Engine) ciGate(ctx context.Context, log *slog.Logger, c claimed) (string, domain.RunID, bool) {
	if !c.station.WaitsForCI() {
		return "", "", true
	}

	commits, err := e.ciCommits(ctx, log, c.line, c.item)
	if err != nil {
		e.block(ctx, log, c, err.Error())
		return "", "", false
	}

	if len(commits) == 0 {
		return "", "", true
	}

	waits, err := e.store.CIWaits(ctx, c.item.ID, c.station.ID)
	if err != nil {
		e.block(ctx, log, c, fmt.Sprintf("read the item's CI waits: %v", err))
		return "", "", false
	}

	var (
		settled    []domain.CIWaitRecord
		pending    []ciCommit
		unreadable []string
	)

	reset, err := e.latestMoveOrRetry(ctx, c.item.ID)
	if err != nil {
		e.block(ctx, log, c, err.Error())
		return "", "", false
	}

	for _, cm := range commits {
		w, ok := endedWaitOn(waits, cm)

		switch {
		case !ok, w.Outcome == domain.CIUnreadable && w.EndedAt.Before(reset):
			pending = append(pending, cm)
		case w.Outcome == domain.CIUnreadable:
			unreadable = append(unreadable, e.ciUnreadable(ctx, c.line, w))
		default:
			settled = append(settled, w)
		}
	}

	switch {
	case len(unreadable) > 0:
		e.block(ctx, log, c, strings.Join(unreadable, " "))
		return "", "", false
	case len(pending) > 0:
		e.waitForCI(ctx, log, c, pending)
		return "", "", false
	}

	if run, ok := heldRun(waits, ciAfterVisit(reset), commits); ok {
		return "", run, true
	}

	log.InfoContext(ctx, "ci gate passed", slog.Int("commits", len(settled)))

	return ciSection(settled), "", true
}

// ciCommits are the commits the item's CI is waited on (ADR 0044 decision 4): for each
// repository its runs pushed to (a row with Wrote), the latest pushed head, in the order the
// runs first pushed to each; for a repository one of the item's pull requests is in, that pull
// request's current head as Checks returns it instead. A head that cannot be read is logged and
// the pushed head kept with the pull request's URL, so the poller follows the head. None when
// no run pushed.
func (e *Engine) ciCommits(ctx context.Context, log *slog.Logger, l domain.Line, it domain.Item) ([]ciCommit, error) {
	runs, err := e.store.Runs(ctx, it.ID, ciRunsBound)
	if err != nil {
		return nil, fmt.Errorf("read the item's runs: %w", err)
	}

	return e.withPullRequests(ctx, log, l, it, pushedHeads(runs)), nil
}

// withPullRequests puts on each of commits that is in the repository of one of the item's pull
// requests that pull request's URL and current head, as ciCommits does.
func (e *Engine) withPullRequests(ctx context.Context, log *slog.Logger, l domain.Line, it domain.Item, commits []ciCommit) []ciCommit {
	if len(commits) == 0 {
		return nil
	}

	for _, url := range it.PullRequests(l) {
		targets, err := e.codeRepositories(ctx, l, []string{url})
		if err != nil {
			log.WarnContext(ctx, "ci gate cannot place a pull request", slog.String("pull_request", url), slog.Any("error", err))
			continue
		}

		i := slices.IndexFunc(commits, func(cm ciCommit) bool { return cm.repo == targets[0].repo.Key() })
		if i < 0 || commits[i].prURL != "" {
			continue
		}

		commits[i].prURL = url
		if head, err := e.pullRequestHead(ctx, l, it, url, commits[i].sha); err != nil {
			log.WarnContext(ctx, "ci gate kept the pushed head", slog.String("pull_request", url), slog.Any("error", err))
		} else {
			commits[i].sha = head
		}
	}

	return commits
}

// pushedHeads are the latest pushed head of each repository runs (oldest first) wrote to, in the
// order they first pushed to each.
func pushedHeads(runs []domain.Run) []ciCommit {
	var commits []ciCommit

	for _, r := range runs {
		for _, rr := range r.Result.Repositories {
			key, ok := domain.RepositoryKey(rr.Repo)
			if !ok || !rr.Wrote || rr.PushedHead == "" {
				continue
			}

			if i := slices.IndexFunc(commits, func(cm ciCommit) bool { return cm.repo == key }); i >= 0 {
				commits[i].sha = rr.PushedHead
			} else {
				commits = append(commits, ciCommit{repo: key, sha: rr.PushedHead})
			}
		}
	}

	return commits
}

// pullRequestHead is the current head of the pull request at url, which Checks returns with the
// checks of sha, read with the host and token a merge uses (repositoryHost).
func (e *Engine) pullRequestHead(ctx context.Context, l domain.Line, it domain.Item, url, sha string) (string, error) {
	t, err := e.repositoryHost(ctx, l, it, url)
	if err != nil {
		return "", err
	}

	cctx, cancel := context.WithTimeout(ctx, ciChecksTimeout)
	defer cancel()

	checks, err := t.host.Checks(cctx, t.repo, t.token, sha, url)
	if err != nil {
		return "", fmt.Errorf("read the pull request's head: %w", err)
	}

	if checks.Head == "" {
		return sha, nil
	}

	return checks.Head, nil
}

// endedWaitOn is the latest wait among waits on cm's repository and commit that ended with an
// outcome other than superseded; false when there is none.
func endedWaitOn(waits []domain.CIWaitRecord, cm ciCommit) (domain.CIWaitRecord, bool) {
	for _, w := range slices.Backward(waits) {
		if w.Repo == cm.repo && w.SHA == cm.sha && !w.Open() && w.Outcome != domain.CISuperseded {
			return w, true
		}
	}

	return domain.CIWaitRecord{}, false
}

// latestMoveOrRetry is when the item last moved or a person retried it, zero when never: an
// unreadable wait that ended before it was seen already, so CI is waited for again.
func (e *Engine) latestMoveOrRetry(ctx context.Context, id domain.ItemID) (time.Time, error) {
	events, err := e.itemEvents(ctx, id)
	if err != nil {
		return time.Time{}, fmt.Errorf("read the item's events: %w", err)
	}

	for _, ev := range slices.Backward(events) {
		if ev.Kind == domain.EventMoved || ev.Kind == domain.EventRetried {
			return ev.At, nil
		}
	}

	return time.Time{}, nil
}

// waitForCI opens a before wait on each of commits (one already open is kept) and returns the
// item to its station's queue, releasing the lease, before any box or run exists. The claim
// skips the item while a wait is open.
func (e *Engine) waitForCI(ctx context.Context, log *slog.Logger, c claimed, commits []ciCommit) {
	now := e.clock.Now()
	shas := make([]string, 0, len(commits))

	for _, cm := range commits {
		shas = append(shas, cm.repo+"@"+cm.sha)

		_, err := e.store.OpenCIWait(ctx, domain.NewCIWait{
			Item: c.item.ID, Station: c.station.ID, Visit: ciBeforeVisit, Repo: cm.repo, SHA: cm.sha, PRURL: cm.prURL,
			Phase: domain.CIPhaseBefore, Round: 1, StartedAt: now,
		})
		if err != nil && !errors.Is(err, ports.ErrConflict) {
			e.block(ctx, log, c, fmt.Sprintf("open a CI wait on %s at %s: %v", cm.repo, cm.sha, err))
			return
		}
	}

	log.InfoContext(ctx, "ci gate waiting", slog.Any("commits", shas))

	e.transition(ctx, log, ports.Transition{
		ItemID: c.item.ID, From: domain.Position{Station: c.station.ID, State: domain.StateInProgress},
		To: domain.Position{Station: c.station.ID, State: c.station.Arrival()}, Lease: &c.lease, Outcome: domain.LeaseReleased,
		At: now,
		Events: []domain.Event{event(c.item, domain.EventReleased, now, domain.ActorEngine,
			map[string]any{keyReason: "waiting for CI on " + strings.Join(shas, ", ")})},
	})
}

// ciUnreadable is the block reason for w, a wait that ended because the token cannot read CI
// (ADR 0044 decision 8): the repository and commit, the token's source and the permission it
// needs, from the poll's recorded error.
func (e *Engine) ciUnreadable(ctx context.Context, l domain.Line, w domain.CIWaitRecord) string {
	why := w.Poll.LastError
	if why == "" {
		why = "the token cannot read CI"
	}

	return fmt.Sprintf("CI on %s at %s cannot be read with %s: %s. Give the token that permission, then retry the item.",
		w.Repo, w.SHA, e.ciTokenSource(ctx, l, w.Repo), why)
}

// ciTokenSource names, without its value, the token goald reads CI of the repository key with,
// as repositoryHost picks it: the runtime.repositories entry that covers it, else on a version on
// secrets runtime.repository.token, else the factory's connection for its host.
func (e *Engine) ciTokenSource(ctx context.Context, l domain.Line, key string) string {
	if entry, ok := domain.TokenEntry(l.Runtime.Repositories, key, true); ok && entry.Token != "" {
		return "the token of the runtime.repositories entry " + entry.URL
	}

	if l.OnSecrets() {
		return repositoryLogin
	}

	host, _, _ := strings.Cut(key, "/")

	if conns, err := e.codeHostConnections(ctx, l); err == nil {
		if i := slices.IndexFunc(conns, func(c codeHostConnection) bool { return c.host == host }); i >= 0 {
			return "the token of the connection " + conns[i].name
		}
	}

	return "the token of the factory's connection for " + host
}

// ciSection is the `## CI` section a run's brief starts with (ADR 0044 decision 2), built like
// the changed-since section: for each wait, its repository, commit (and pull request), how CI
// ended and each check the poll last read, with its result or state, link and failed jobs.
func ciSection(waits []domain.CIWaitRecord) string {
	var b strings.Builder

	b.WriteString("## CI\n\ngoald waited for CI on the item's commits before this run started. Do not poll the code host for CI.")

	for _, w := range waits {
		fmt.Fprintf(&b, "\n\n### %s at %s", w.Repo, w.SHA)

		if w.PRURL != "" {
			fmt.Fprintf(&b, " (%s)", w.PRURL)
		}

		fmt.Fprintf(&b, ": %s\n", ciOutcomeText(w.Outcome))

		var checks ports.CommitChecks
		if len(w.Poll.Checks) > 0 && json.Unmarshal(w.Poll.Checks, &checks) == nil && len(checks.Checks) > 0 {
			for _, ch := range checks.Checks {
				b.WriteString("\n" + ciCheckLine(ch))
			}
		} else {
			b.WriteString("\nNo check was read.")
		}
	}

	return b.String()
}

// ciOutcomeText says how a before wait ended, for the CI section.
func ciOutcomeText(o domain.CIOutcome) string {
	switch o {
	case domain.CIPassed:
		return "passed, every check finished and none failed"
	case domain.CIFailed:
		return "failed, a check failed"
	case domain.CITimedOut:
		return "timed_out, CI had not finished within the station's max; the checks are as last read"
	case domain.CINoCI:
		return "no_ci, no check appeared within the station's grace"
	case domain.CIUnreadable, domain.CISuperseded: // neither settles a before wait
	}

	return string(o)
}

// ciCheckLine is one check as the CI section lists it.
func ciCheckLine(ch ports.Check) string {
	status := string(ch.State)
	if ch.State == ports.CheckFinished {
		status = string(ch.Result)
	}

	line := "- " + ch.Name + ": " + status
	if ch.URL != "" {
		line += " (" + ch.URL + ")"
	}

	if len(ch.FailedJobs) > 0 {
		line += "; failed jobs: " + strings.Join(ch.FailedJobs, ", ")
	}

	return line
}
