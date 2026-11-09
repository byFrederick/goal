package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ciAfterVisitPrefix starts the visit key of every after wait (ciAfterVisit).
const ciAfterVisitPrefix = "after@"

// ciAfterVisit is the visit key of the after waits of the item's visit to its station that began
// at reset, its latest move or retry (latestMoveOrRetry): a move or a person's retry starts a new
// count of CI rounds.
func ciAfterVisit(reset time.Time) string {
	return ciAfterVisitPrefix + reset.UTC().Format(time.RFC3339Nano)
}

// ciHeld is what an after wait holds: the run whose output is applied once CI passes.
type ciHeld struct {
	Run domain.RunID `json:"run"`
}

// holdForCI is the hold after a push at a station with waitFor: ci (ADR 0044 decision 3). When
// the run pushed (its own repositories with Wrote and a pushed head), or its approval would merge
// pull requests (heldCommits), and its checked output would move the item forward
// (movesForward), it opens an after wait per commit, holding the run, and returns the item to its station's queue with the lease released, as deferMerge does;
// the output is not accepted yet. Once the visit has had the station's rounds of CI, the item
// blocks for a person instead, naming the checks that failed last. It reports whether it held or
// blocked the item; false leaves the outcome to finish.
func (e *Engine) holdForCI(ctx context.Context, log *slog.Logger, c claimed, run domain.RunID, chk checked) bool {
	if !c.station.WaitsForCI() || !movesForward(c, chk) {
		return false
	}

	r, err := e.store.Run(ctx, run)
	if err != nil {
		e.block(ctx, log, c, fmt.Sprintf("read run %s for its pushes: %v", run, err))
		return true
	}

	commits := e.heldCommits(ctx, log, c, r)
	if len(commits) == 0 {
		return false
	}

	reset, err := e.latestMoveOrRetry(ctx, c.item.ID)
	if err != nil {
		e.block(ctx, log, c, err.Error())
		return true
	}

	visit := ciAfterVisit(reset)

	rounds, err := e.store.CIRounds(ctx, c.item.ID, c.station.ID, visit)
	if err != nil {
		e.block(ctx, log, c, fmt.Sprintf("count the item's CI rounds: %v", err))
		return true
	}

	if limit := ciLimits(c.station.WaitFor).Rounds; rounds >= limit {
		e.block(ctx, log, c, e.ciRoundsReason(ctx, c, visit, limit))
		return true
	}

	held, err := json.Marshal(ciHeld{Run: run})
	if err != nil {
		e.block(ctx, log, c, fmt.Sprintf("hold run %s: %v", run, err))
		return true
	}

	now := e.clock.Now()
	shas := make([]string, 0, len(commits))

	for _, cm := range commits {
		shas = append(shas, cm.repo+"@"+cm.sha)

		_, err := e.store.OpenCIWait(ctx, domain.NewCIWait{
			Item: c.item.ID, Station: c.station.ID, Visit: visit, Repo: cm.repo, SHA: cm.sha, PRURL: cm.prURL,
			Phase: domain.CIPhaseAfter, Round: rounds + 1, Held: held, StartedAt: now,
		})
		if err != nil && !errors.Is(err, ports.ErrConflict) {
			e.block(ctx, log, c, fmt.Sprintf("open a CI wait on %s at %s: %v", cm.repo, cm.sha, err))
			return true
		}
	}

	log.InfoContext(ctx, "ci hold waiting", slog.Any("commits", shas), slog.Int("round", rounds+1), slog.String("run", string(run)))

	e.transition(ctx, log, ports.Transition{
		ItemID: c.item.ID, From: domain.Position{Station: c.station.ID, State: domain.StateInProgress},
		To: domain.Position{Station: c.station.ID, State: c.station.Arrival()}, Lease: &c.lease, Outcome: domain.LeaseReleased,
		At: now,
		Events: []domain.Event{event(c.item, domain.EventReleased, now, domain.ActorEngine,
			map[string]any{keyReason: "waiting for CI on " + strings.Join(shas, ", ") + " before moving on"})},
	})

	return true
}

// heldCommits are the commits holdForCI waits on for run r: the heads r pushed, each on its pull
// request's head when one of the item's pull requests is in its repository (withPullRequests).
// At an action: merge station, the head of each open pull request the merge takes, in a
// repository r did not push to, is added (ADR 0044 decision 4, amended 2026-11-09): those are
// the item's outputs with the station's replaced by r's, as mergeOnApproval reads them.
func (e *Engine) heldCommits(ctx context.Context, log *slog.Logger, c claimed, r domain.Run) []ciCommit {
	pushed := pushedHeads([]domain.Run{r})
	if c.station.Action != domain.ActionMerge {
		return e.withPullRequests(ctx, log, c.line, c.item.PullRequests(c.line), pushed, false)
	}

	it := c.item
	it.Outputs = maps.Clone(it.Outputs)

	if it.Outputs == nil {
		it.Outputs = map[string]json.RawMessage{}
	}

	it.Outputs[c.station.ID] = r.Result.Output

	return e.withPullRequests(ctx, log, c.line, it.PullRequests(c.line), pushed, true)
}

// movesForward reports whether finish would move the item forward on chk: an approval (the
// gate's merge included) or a station without a gate finishing. A rejection, a close, a plan and
// an approved merge left to a person are applied at once.
func movesForward(c claimed, chk checked) bool {
	v := chk.verdict

	switch {
	case v != nil && v.Decision == domain.Reject, chk.close != "", len(chk.plan) > 0:
		return false
	case v != nil && c.waitsForPerson() && c.station.Action == domain.ActionMerge:
		return false
	}

	return true
}

// heldRun is the run whose output the item's latest round of after waits in visit holds, when
// each of them passed or got no CI and is still the latest ended wait on its repository's current
// commit (commits): the outcome to apply now, without a run. False when the visit holds nothing,
// or when CI failed or timed out, so the station runs again with the CI section.
func heldRun(waits []domain.CIWaitRecord, visit string, commits []ciCommit) (domain.RunID, bool) {
	latest := latestRound(waits, visit)
	if len(latest) == 0 {
		return "", false
	}

	var run domain.RunID

	for _, w := range latest {
		if w.Open() || (w.Outcome != domain.CIPassed && w.Outcome != domain.CINoCI) {
			return "", false
		}

		i := slices.IndexFunc(commits, func(cm ciCommit) bool { return cm.repo == w.Repo })
		if i < 0 {
			return "", false
		}

		if ended, ok := endedWaitOn(waits, commits[i]); !ok || ended.ID != w.ID {
			return "", false
		}

		var h ciHeld
		if json.Unmarshal(w.Held, &h) != nil || h.Run == "" || (run != "" && h.Run != run) {
			return "", false
		}

		run = h.Run
	}

	return run, true
}

// latestRound are the after waits of visit that hold an outcome in its highest round, the
// superseded ones left out.
func latestRound(waits []domain.CIWaitRecord, visit string) []domain.CIWaitRecord {
	var out []domain.CIWaitRecord

	for _, w := range waits {
		if w.Phase != domain.CIPhaseAfter || w.Visit != visit || len(w.Held) == 0 || w.Outcome == domain.CISuperseded {
			continue
		}

		if len(out) > 0 && w.Round > out[0].Round {
			out = out[:0]
		}

		if len(out) == 0 || w.Round == out[0].Round {
			out = append(out, w)
		}
	}

	return out
}

// applyHeld applies the output of run, held on the item's after waits whose CI passed: with the
// claim's agent and the organization policy (which decides whether its merge is left to a
// person), its output is checked against the current line again and finished without a new hold
// (finish), so the item moves on, and at an action: merge gate merges through mergeOnApproval.
func (e *Engine) applyHeld(ctx context.Context, log *slog.Logger, c claimed, run domain.RunID) {
	var err error
	if c.agent, err = agentFor(c.line, c.station, c.item.Lane); err == nil {
		_, err = e.applyPolicy(ctx, &c)
	}

	if err != nil {
		e.block(ctx, log, c, err.Error())
		return
	}

	r, err := e.store.Run(ctx, run)
	if err != nil {
		e.block(ctx, log, c, fmt.Sprintf("read the held run %s: %v", run, err))
		return
	}

	chk, err := e.check(ctx, c, r.Result.Output)
	if err != nil {
		e.block(ctx, log, c, fmt.Sprintf("check the held run %s's output: %v", run, err))
		return
	}

	log.InfoContext(ctx, "ci hold applied", slog.String("run", string(run)))

	e.finish(ctx, log, c, run, r.Result.Output, chk)
}

// ciRoundsReason is why an item blocks after limit CI rounds in visit: the checks that failed in
// the latest round, on each repository and commit.
func (e *Engine) ciRoundsReason(ctx context.Context, c claimed, visit string, limit int) string {
	reason := fmt.Sprintf("CI on what the item pushed did not pass in %d rounds at %s", limit, c.station.ID)

	waits, err := e.store.CIWaits(ctx, c.item.ID, c.station.ID)
	if err != nil {
		return reason + fmt.Sprintf(" (its checks could not be read: %v). Fix CI, then retry the item.", err)
	}

	var failing []string

	for _, w := range latestRound(waits, visit) {
		var checks ports.CommitChecks
		if len(w.Poll.Checks) > 0 {
			_ = json.Unmarshal(w.Poll.Checks, &checks)
		}

		names := make([]string, 0, len(checks.Failed()))
		for _, ch := range checks.Failed() {
			names = append(names, ch.Name)
		}

		failing = append(failing, fmt.Sprintf("%s at %s %s: %s", w.Repo, w.SHA, w.Outcome, cmp.Or(strings.Join(names, ", "), "no failed check read")))
	}

	if len(failing) > 0 {
		reason += "; " + strings.Join(failing, "; ")
	}

	return reason + ". Fix CI, then retry the item."
}
