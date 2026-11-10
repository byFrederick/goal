package engine_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The commits the hold tests' runs push: full shas, as a worker reports them (domain.GitHead).
var (
	pushSHA1 = strings.Repeat("a1", 20)
	pushSHA2 = strings.Repeat("b2", 20)
	pushSHA3 = strings.Repeat("c3", 20)
)

// ghCIPermission is the permission a GitHub token lacks when it cannot read CI.
const ghCIPermission = "read access to Checks and Commit statuses"

// pusher plays, for an item at a station, the results set for it in order, the last one
// repeating, and ok({}) for the rest.
type pusher struct {
	mu  sync.Mutex
	out map[string][]domain.RunResult
}

func newPusher() *pusher { return &pusher{out: map[string][]domain.RunResult{}} }

func (p *pusher) set(id domain.ItemID, station string, rs ...domain.RunResult) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.out[string(id)+"@"+station] = rs
}

func (p *pusher) agent(_ context.Context, c porttest.ExecCall, _ io.Writer) domain.RunResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := string(c.Sandbox.ItemID) + "@" + c.Sandbox.Station

	rs := p.out[key]
	if len(rs) == 0 {
		return ok(`{}`)
	}

	if len(rs) > 1 {
		p.out[key] = rs[1:]
	}

	return rs[0]
}

// pushedTo is a succeeded run with output that pushed sha to appRepo.
func pushedTo(output, sha string) domain.RunResult {
	r := ok(output)
	r.Repositories = []domain.RunRepository{{Repo: appRepo, PushedHead: sha, Wrote: true}}

	return r
}

// holdLine is build → review (a gate with action, waitFor: ci and rounds) → done.
func holdLine(action domain.StationAction, rounds int) domain.Line {
	l := turnLine("")

	for i := range l.Stations {
		if l.Stations[i].ID == stReview {
			l.Stations[i].Action = action
			l.Stations[i].WaitFor = domain.CIWait{Grace: ciGrace, Max: ciMax, Rounds: rounds}
		}
	}

	return l
}

// holdHarness is a harness on line whose agents play a pusher.
func holdHarness(t *testing.T, line domain.Line) (*harness, *pusher) {
	t.Helper()

	p := newPusher()

	return newHarness(t, line, p.agent), p
}

// expectHeld fails unless the item is back in review's queue with one open after wait on sha of
// appRepo in round, holding a run, after runs runs.
func (h *harness) expectHeld(t *testing.T, id domain.ItemID, sha string, round, runs int) domain.CIWaitRecord {
	t.Helper()

	h.expectPosition(t, id, queuedAtMerge)

	if n := len(h.worker.Runs()); n != runs {
		t.Fatalf("%d runs; want %d", n, runs)
	}

	open := h.openWaits(t, id)
	if len(open) != 1 || open[0].Phase != domain.CIPhaseAfter || open[0].SHA != sha || open[0].Repo != appKey ||
		open[0].Station != stReview || open[0].Round != round {
		t.Fatalf("open waits = %+v; want one after wait at review on %s in round %d", open, sha, round)
	}

	var held struct {
		Run string `json:"run"`
	}
	if err := json.Unmarshal(open[0].Held, &held); err != nil || held.Run == "" {
		t.Fatalf("wait holds %s (%v); want the run", open[0].Held, err)
	}

	return open[0]
}

// endCI makes CI on sha answer checks and polls it.
func (h *harness) endCI(t *testing.T, sha string, checks porttest.ChecksAnswer) {
	t.Helper()

	h.codehost.SetChecks(appRepo, sha, checks)
	h.clock.Advance(domain.CIPollMinInterval)
	pollCI(t, h.e)
}

// failing are checks of which one, e2e, failed with a failed job.
var failing = porttest.ChecksAnswer{Checks: ports.CommitChecks{Checks: []ports.Check{
	{Name: "lint", State: ports.CheckFinished, Result: ports.CheckPassed},
	{Name: "e2e", State: ports.CheckFinished, Result: ports.CheckFailed, URL: "https://github.com/acme/app/runs/9", FailedJobs: []string{"shard (3/12)"}},
}}}

// An approval at a waitFor gate whose run pushed is held: the item goes back to the gate's queue
// with an after wait holding the run, the claim skips it while CI runs, and once CI passes the
// next claim applies the approval without running the agent again.
func TestCIHold_PassedMovesOn(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine("", domain.DefaultCIRounds))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	p.set(it.ID, stReview, pushedTo(approve, pushSHA1))

	h.step(t)
	h.expectHeld(t, it.ID, pushSHA1, 1, 1)

	if n := len(h.eventsOf(it.ID, domain.EventGateApproved)); n != 0 {
		t.Fatalf("%d gate_approved events while CI runs; want none", n)
	}

	h.step(t) // the wait is open: the claim skips the item
	h.expectHeld(t, it.ID, pushSHA1, 1, 1)

	h.endCI(t, pushSHA1, checksOf(ports.CheckPassed, ports.CheckFinished))
	h.step(t)

	h.expectPosition(t, it.ID, isDone)

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d runs; want the held run only", n)
	}

	if n := len(h.eventsOf(it.ID, domain.EventGateApproved)); n != 1 {
		t.Errorf("%d gate_approved events; want 1", n)
	}

	if got := h.item(t, it.ID).Outputs[stReview]; !strings.Contains(string(got), "approve") {
		t.Errorf("review output %s; want the held run's", got)
	}
}

// At an action: merge gate, the held approval merges the pull request through the gate's merge
// once CI passes, and no merge happens while it runs.
func TestCIHold_PassedMerges(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine(domain.ActionMerge, domain.DefaultCIRounds))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	h.codehost.Add(prFrom(prOf(it), it))
	p.set(it.ID, stReview, pushedTo(`{"decision":"approve","pull_request":"`+prOf(it)+`"}`, pushSHA1))

	h.step(t)
	h.expectHeld(t, it.ID, pushSHA1, 1, 1)

	if m := h.codehost.Merges(); len(m) != 0 {
		t.Fatalf("merges while CI runs = %+v; want none", m)
	}

	h.endCI(t, pushSHA1, checksOf(ports.CheckPassed, ports.CheckFinished))
	h.step(t)

	h.expectPosition(t, it.ID, isDone)

	if m := h.codehost.Merges(); len(m) != 1 || m[0].URL != prOf(it) {
		t.Errorf("merges = %+v; want %s merged once", m, prOf(it))
	}

	if n := len(h.eventsOf(it.ID, domain.EventMerged)); n != 1 {
		t.Errorf("%d merged events; want 1", n)
	}
}

// A waitFor station without a gate holds its move on the same way.
func TestCIHold_NoGateStationHeld(t *testing.T) {
	t.Parallel()

	p := newPusher()
	h := newHarness(t, ciLine(), p.agent)
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	p.set(it.ID, stReview, pushedTo(`{}`, pushSHA1))

	h.step(t)
	h.expectHeld(t, it.ID, pushSHA1, 1, 1)

	h.endCI(t, pushSHA1, checksOf(ports.CheckPassed)) // no check: no_ci after grace
	h.clock.Advance(ciGrace)
	pollCI(t, h.e)
	h.step(t)

	h.expectPosition(t, it.ID, isDone)

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d runs; want the held run only", n)
	}
}

// When CI on what the run pushed fails, the station runs again, its brief's CI section naming
// the failing checks, and the next push is held in round 2.
func TestCIHold_FailedRunsStationAgain(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine("", domain.DefaultCIRounds))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	p.set(it.ID, stReview, pushedTo(approve, pushSHA1), pushedTo(approve, pushSHA2))

	h.step(t)
	h.expectHeld(t, it.ID, pushSHA1, 1, 1)

	h.endCI(t, pushSHA1, failing)
	h.step(t)

	section := h.ciSectionOf(t, 1)
	for _, want := range []string{pushSHA1, string(domain.CIFailed), "e2e: failed", "https://github.com/acme/app/runs/9", "shard (3/12)"} {
		if !strings.Contains(section, want) {
			t.Errorf("CI section lacks %q:\n%s", want, section)
		}
	}

	h.expectHeld(t, it.ID, pushSHA2, 2, 2)

	if n := len(h.eventsOf(it.ID, domain.EventGateApproved)); n != 0 {
		t.Errorf("%d gate_approved events; want none before CI passes", n)
	}
}

// When CI on what the run pushed cannot be read, the item blocks, naming the token and the
// permission it needs.
func TestCIHold_UnreadableBlocks(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine("", domain.DefaultCIRounds))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	p.set(it.ID, stReview, pushedTo(approve, pushSHA1))

	h.step(t)
	h.expectHeld(t, it.ID, pushSHA1, 1, 1)

	h.endCI(t, pushSHA1, porttest.ChecksAnswer{Err: &ports.CINotReadableError{Permission: ghCIPermission}})
	h.step(t)

	h.expectPosition(t, it.ID, domain.Position{Station: stReview, State: domain.StateBlocked})

	blocked := h.eventsOf(it.ID, domain.EventBlocked)
	if len(blocked) != 1 {
		t.Fatalf("%d blocked events; want 1", len(blocked))
	}

	reason, _ := blocked[0].Data["reason"].(string)
	for _, want := range []string{appKey, pushSHA1, "runtime.repositories entry https://github.com/acme/*", ghCIPermission} {
		if !strings.Contains(reason, want) {
			t.Errorf("block reason %q lacks %q", reason, want)
		}
	}
}

// Once a visit has had the station's rounds of CI, the next push blocks the item for a person,
// naming the checks that failed last.
func TestCIHold_RoundsCapBlocks(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine("", 2))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	p.set(it.ID, stReview, pushedTo(approve, pushSHA1), pushedTo(approve, pushSHA2), pushedTo(approve, pushSHA3))

	h.step(t)
	h.expectHeld(t, it.ID, pushSHA1, 1, 1)
	h.endCI(t, pushSHA1, failing)

	h.step(t)
	h.expectHeld(t, it.ID, pushSHA2, 2, 2)
	h.endCI(t, pushSHA2, failing)

	h.step(t)

	h.expectPosition(t, it.ID, domain.Position{Station: stReview, State: domain.StateBlocked})

	if open := h.openWaits(t, it.ID); len(open) != 0 {
		t.Errorf("open waits = %+v; want none past the rounds", open)
	}

	blocked := h.eventsOf(it.ID, domain.EventBlocked)
	if len(blocked) != 1 {
		t.Fatalf("%d blocked events; want 1", len(blocked))
	}

	reason, _ := blocked[0].Data["reason"].(string)
	for _, want := range []string{"CI on the item's commits did not pass in 2 rounds", "e2e", appKey, pushSHA2} {
		if !strings.Contains(reason, want) {
			t.Errorf("block reason %q lacks %q", reason, want)
		}
	}
}

// A rejection goes back to onReject at once, though its run pushed, and opens no CI wait.
func TestCIHold_RejectionDoesNotWait(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine("", domain.DefaultCIRounds))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	p.set(it.ID, stReview, pushedTo(reject, pushSHA1))

	h.step(t)

	h.expectPosition(t, it.ID, domain.Position{Station: stBuild, State: domain.StateQueued})

	if ws, err := h.store.CIWaits(inOrg(t), it.ID, stReview); err != nil || len(ws) != 0 {
		t.Errorf("CI waits = %+v, %v; want none", ws, err)
	}
}

// An approval whose run pushed nothing moves on at once, without a CI wait.
func TestCIHold_NothingPushedMovesOn(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine("", domain.DefaultCIRounds))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	p.set(it.ID, stReview, ok(approve))

	h.step(t)

	h.expectPosition(t, it.ID, isDone)

	if ws, err := h.store.CIWaits(inOrg(t), it.ID, stReview); err != nil || len(ws) != 0 {
		t.Errorf("CI waits = %+v, %v; want none", ws, err)
	}
}

// A held approval whose merge is not mergeable yet goes back to the queue, and the next claim
// merges again from the same passed wait without running the agent again.
func TestCIHold_DeferredMergeRetriesWithoutRun(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine(domain.ActionMerge, domain.DefaultCIRounds))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	h.codehost.Add(prFrom(prOf(it), it))
	h.codehost.RefuseMerges(prOf(it), 1)
	p.set(it.ID, stReview, pushedTo(`{"decision":"approve","pull_request":"`+prOf(it)+`"}`, pushSHA1))

	h.step(t)
	h.expectHeld(t, it.ID, pushSHA1, 1, 1)

	h.endCI(t, pushSHA1, checksOf(ports.CheckPassed, ports.CheckFinished))
	h.step(t)

	h.expectDeferredFor(t, it.ID, prOf(it), 1, "required checks")

	h.step(t)

	h.expectPosition(t, it.ID, isDone)

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d runs; want the held run only", n)
	}
}

// expectNoMergeTried fails if the code host merged anything or the item counted a deferral.
func (h *harness) expectNoMergeTried(t *testing.T, id domain.ItemID) {
	t.Helper()

	if m := h.codehost.Merges(); len(m) != 0 {
		t.Errorf("merges while CI runs = %+v; want none", m)
	}

	if d := h.eventsOf(id, domain.EventMergeDeferred); len(d) != 0 {
		t.Errorf("merge_deferred events %+v; want none while CI runs", d)
	}
}

// At an action: merge gate, an approval whose run pushed nothing, and whose own output is the
// only one to name the pull request, is held on the pull request's head while its CI runs: no
// merge is tried and no deferral counted. Once CI passes, the next claim merges once without a
// new run.
func TestCIHold_MergeWithoutPushWaitsOnPullRequest(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine(domain.ActionMerge, domain.DefaultCIRounds))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	h.codehost.Add(prFrom(prOf(it), it))
	p.set(it.ID, stReview, ok(`{"decision":"approve","pull_request":"`+prOf(it)+`"}`))

	h.step(t)

	if w := h.expectHeld(t, it.ID, headSHA, 1, 1); w.PRURL != prOf(it) {
		t.Errorf("wait on %q; want the pull request %s", w.PRURL, prOf(it))
	}

	released := h.eventsOf(it.ID, domain.EventReleased)
	if r, _ := released[len(released)-1].Data["reason"].(string); !strings.Contains(r, "waiting for CI on "+appKey+"@"+headSHA) {
		t.Errorf("released for %q; want waiting for CI on the pull request's head", r)
	}

	h.expectNoMergeTried(t, it.ID)

	h.step(t) // the wait is open: the claim skips the item
	h.expectHeld(t, it.ID, headSHA, 1, 1)
	h.expectNoMergeTried(t, it.ID)

	h.endCI(t, headSHA, checksOf(ports.CheckPassed, ports.CheckFinished))
	h.step(t)

	h.expectPosition(t, it.ID, isDone)

	if m := h.codehost.Merges(); len(m) != 1 || m[0].URL != prOf(it) {
		t.Errorf("merges = %+v; want %s merged once", m, prOf(it))
	}

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d runs; want the held run only", n)
	}
}

// When CI on the pull request's head fails, the merging gate whose run pushed nothing runs
// again with the failing checks in its CI section, its next approval is held in round 2, and
// past the station's rounds the item blocks for a person without a merge.
func TestCIHold_MergeWithoutPushFailedRunsAgain(t *testing.T) {
	t.Parallel()

	h, p := holdHarness(t, holdLine(domain.ActionMerge, 2))
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	h.codehost.Add(prFrom(prOf(it), it))
	p.set(it.ID, stReview, ok(`{"decision":"approve","pull_request":"`+prOf(it)+`"}`))

	h.step(t)
	h.expectHeld(t, it.ID, headSHA, 1, 1)
	h.endCI(t, headSHA, failing)

	h.step(t)

	section := h.ciSectionOf(t, 1)
	for _, want := range []string{headSHA, prOf(it), string(domain.CIFailed), "e2e: failed", "shard (3/12)"} {
		if !strings.Contains(section, want) {
			t.Errorf("CI section lacks %q:\n%s", want, section)
		}
	}

	h.expectHeld(t, it.ID, headSHA, 2, 2)
	h.expectNoMergeTried(t, it.ID)
	h.endCI(t, headSHA, failing)

	h.step(t)

	h.expectPosition(t, it.ID, domain.Position{Station: stReview, State: domain.StateBlocked})
	h.expectNoMergeTried(t, it.ID)

	blocked := h.eventsOf(it.ID, domain.EventBlocked)
	if len(blocked) != 1 {
		t.Fatalf("%d blocked events; want 1", len(blocked))
	}

	if reason, _ := blocked[0].Data["reason"].(string); !strings.Contains(reason, "2 rounds") || !strings.Contains(reason, "e2e") {
		t.Errorf("block reason %q; want the rounds and the failing check", reason)
	}
}
