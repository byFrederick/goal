package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// appKey is appRepo's domain.RepositoryKey, which CI waits are on.
var appKey, _ = domain.RepositoryKey(appRepo)

// newCIGateHarness is a harness on line whose agents succeed with an empty output.
func newCIGateHarness(t *testing.T, line domain.Line) *harness {
	t.Helper()

	return newHarness(t, line, porttest.Succeed(`{}`))
}

// atReview submits an item whose earlier runs pushed heads (repository key → sha, oldest run
// first) and places it in review's queue, with output as the inbox's output when it is set.
func (h *harness) atReview(t *testing.T, output string, pushes ...map[string]string) domain.Item {
	t.Helper()

	ctx := inOrg(t)
	it := h.submit(t, domain.NewItem{Title: "Add a health endpoint"})

	for _, heads := range pushes {
		id, err := h.store.StartRun(ctx, ports.NewRun{FactoryID: factoryID, ItemID: it.ID, Station: stInbox, Agent: agWorker, StartedAt: h.clock.Now()})
		if err != nil {
			t.Fatal(err)
		}

		res := ok(`{}`)
		for repo, sha := range heads {
			res.Repositories = append(res.Repositories, domain.RunRepository{Repo: repo, PushedHead: sha, Wrote: sha != ""})
		}

		if err := h.store.FinishRun(ctx, id, res, h.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}

	tr := ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stReview, State: domain.StateQueued}, At: h.clock.Now(),
	}
	if output != "" {
		tr.OutputStation, tr.Output = stInbox, json.RawMessage(output)
	}

	moved, err := h.store.Transition(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}

	return moved
}

// ciSectionOf is the `## CI` section of the i-th run's feedback, failing the test unless the
// feedback starts with it.
func (h *harness) ciSectionOf(t *testing.T, i int) string {
	t.Helper()

	feedback := h.feedback(i)
	if !strings.HasPrefix(feedback, "## CI\n") {
		t.Fatalf("feedback of run %d does not start with ## CI:\n%s", i, feedback)
	}

	section, _, _ := strings.Cut(feedback, "\n\n## ")

	return section
}

// expectWaitingBeforeRun fails unless the item has no run, one open before wait at review on
// sha of appRepo, and is back in review's queue after one released lease.
func (h *harness) expectWaitingBeforeRun(t *testing.T, id domain.ItemID, sha string) {
	t.Helper()

	if n := len(h.worker.Runs()); n != 0 {
		t.Fatalf("%d runs before CI finished; want none", n)
	}

	open := h.openWaits(t, id)
	if len(open) != 1 || open[0].SHA != sha || open[0].Repo != appKey || open[0].Phase != domain.CIPhaseBefore || open[0].Station != stReview {
		t.Fatalf("open waits = %+v; want one before wait at review on %s, the latest pushed head", open, sha)
	}

	if got := h.item(t, id).Position(); got != (domain.Position{Station: stReview, State: domain.StateQueued}) {
		t.Fatalf("item at %+v; want back in review's queue", got)
	}

	h.expectOutcomes(t, id, domain.LeaseReleased)
}

// A run at a waitFor station waits for CI on the commit the item's runs pushed: the first claim
// opens a before wait and returns the item to its queue without a run, the claim skips it while
// the wait is open, and once the poller ends it the next claim starts the run, whose brief
// starts with the CI section naming the commit and each check's name, result and link.
func TestCIGate_WaitsBeforeRun(t *testing.T) {
	t.Parallel()

	h := newCIGateHarness(t, ciLine())
	it := h.atReview(t, "", map[string]string{appKey: ciNewSHA}, map[string]string{appKey: ciSHA})

	h.step(t)
	h.expectWaitingBeforeRun(t, it.ID, ciSHA)

	h.step(t) // the wait is open: the claim skips the item

	if n := len(h.worker.Runs()); n != 0 || len(h.outcomes(it.ID)) != 1 {
		t.Fatalf("%d runs and leases %v while the wait is open; want no new claim", n, h.outcomes(it.ID))
	}

	h.codehost.SetChecks(appRepo, ciSHA, porttest.ChecksAnswer{Checks: ports.CommitChecks{Checks: []ports.Check{
		{Name: "lint", State: ports.CheckFinished, Result: ports.CheckPassed, URL: "https://github.com/acme/app/runs/1"},
		{Name: "test", State: ports.CheckFinished, Result: ports.CheckFailed, URL: "https://github.com/acme/app/runs/2", FailedJobs: []string{"unit (2/4)"}},
	}}})
	pollCI(t, h.e)
	h.step(t)

	if n := len(h.worker.Runs()); n != 1 {
		t.Fatalf("%d runs once CI finished; want 1", n)
	}

	section := h.ciSectionOf(t, 0)
	for _, want := range []string{
		appKey, ciSHA, string(domain.CIFailed),
		"lint: passed", "https://github.com/acme/app/runs/1",
		"test: failed", "https://github.com/acme/app/runs/2", "unit (2/4)",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("CI section lacks %q:\n%s", want, section)
		}
	}

	if open := h.openWaits(t, it.ID); len(open) != 0 {
		t.Errorf("open waits after the run = %+v; want none", open)
	}
}

// An item no run pushed to has nothing to wait for: its first claim at a waitFor station starts
// the run, with no wait, no Checks call and no CI section.
func TestCIGate_NoPushedCommitStartsAtOnce(t *testing.T) {
	t.Parallel()

	h := newCIGateHarness(t, ciLine())
	it := h.atReview(t, "", map[string]string{appKey: ""}) // a run that read the repository only

	h.step(t)

	if n := len(h.worker.Runs()); n != 1 {
		t.Fatalf("%d runs; want 1 on the first claim", n)
	}

	if strings.Contains(h.feedback(0), "## CI") {
		t.Errorf("feedback has a CI section:\n%s", h.feedback(0))
	}

	if ws, err := h.store.CIWaits(inOrg(t), it.ID, stReview); err != nil || len(ws) != 0 {
		t.Errorf("CI waits = %+v, %v; want none", ws, err)
	}

	if calls := h.codehost.ChecksCalls(); len(calls) != 0 {
		t.Errorf("Checks calls = %+v; want none", calls)
	}
}

// A wait that ended unreadable blocks the item for a person at its next claim, naming the
// repository, the token's source and the permission it needs; no run starts. A person's retry
// waits again.
func TestCIGate_UnreadableBlocks(t *testing.T) {
	t.Parallel()

	h := newCIGateHarness(t, ciLine())
	it := h.atReview(t, "", map[string]string{appKey: ciSHA})

	h.step(t) // opens the wait
	h.codehost.SetChecks(appRepo, ciSHA, porttest.ChecksAnswer{Err: &ports.CINotReadableError{Permission: "read access to Checks and Commit statuses"}})
	pollCI(t, h.e)
	h.step(t)

	if n := len(h.worker.Runs()); n != 0 {
		t.Fatalf("%d runs on unreadable CI; want none", n)
	}

	if got := h.item(t, it.ID); got.State != domain.StateBlocked || got.Station != stReview {
		t.Fatalf("item %s at %s; want blocked at review", got.State, got.Station)
	}

	blocked := h.eventsOf(it.ID, domain.EventBlocked)
	if len(blocked) != 1 {
		t.Fatalf("%d blocked events; want 1", len(blocked))
	}

	reason, _ := blocked[0].Data["reason"].(string)
	for _, want := range []string{appKey, ciSHA, "connection " + connGitHub, "read access to Checks and Commit statuses"} {
		if !strings.Contains(reason, want) {
			t.Errorf("block reason %q lacks %q", reason, want)
		}
	}

	h.clock.Advance(domain.CIPollMinInterval)

	if _, err := h.e.Retry(inOrg(t), it.ID, human); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if open := h.openWaits(t, it.ID); len(open) != 1 || open[0].SHA != ciSHA {
		t.Errorf("open waits after a retry = %+v; want a new wait on %s", open, ciSHA)
	}
}

// When one of the item's pull requests is in a repository it pushed to, the wait is on the
// pull request's head that Checks returns, not on the pushed head, and it follows the pull
// request.
func TestCIGate_PullRequestHeadOverPushedHead(t *testing.T) {
	t.Parallel()

	h := newCIGateHarness(t, ciLine())
	it := h.atReview(t, `{"pull_request": "`+prURL+`"}`, map[string]string{appKey: ciSHA})
	h.codehost.SetChecks(appRepo, ciSHA, porttest.ChecksAnswer{Checks: ports.CommitChecks{Head: ciNewSHA}})

	h.step(t)

	open := h.openWaits(t, it.ID)
	if len(open) != 1 || open[0].SHA != ciNewSHA || open[0].PRURL != prURL {
		t.Fatalf("open waits = %+v; want one on the pull request's head %s with its URL", open, ciNewSHA)
	}

	calls := h.codehost.ChecksCalls()
	if len(calls) != 1 || calls[0].SHA != ciSHA || calls[0].URL != prURL {
		t.Errorf("Checks calls = %+v; want one for the pull request on the pushed head", calls)
	}

	h.codehost.SetChecks(appRepo, ciNewSHA, checksOf(ports.CheckPassed, ports.CheckFinished))
	pollCI(t, h.e)
	h.step(t)

	if n := len(h.worker.Runs()); n != 1 {
		t.Fatalf("%d runs once CI on the head finished; want 1", n)
	}

	if section := h.ciSectionOf(t, 0); !strings.Contains(section, ciNewSHA) || !strings.Contains(section, prURL) {
		t.Errorf("CI section does not name the head %s and the pull request:\n%s", ciNewSHA, section)
	}
}

// A station without waitFor is unchanged: an item whose runs pushed starts its run on its first
// claim with no wait, no Checks call and no CI section.
func TestCIGate_StationWithoutWaitForUnchanged(t *testing.T) {
	t.Parallel()

	line := ciLine()
	for i := range line.Stations {
		line.Stations[i].WaitFor = domain.CIWait{}
	}

	h := newCIGateHarness(t, line)
	it := h.atReview(t, `{"pull_request": "`+prURL+`"}`, map[string]string{appKey: ciSHA})

	h.step(t)

	if n := len(h.worker.Runs()); n != 1 {
		t.Fatalf("%d runs; want 1 on the first claim", n)
	}

	if strings.Contains(h.feedback(0), "## CI") {
		t.Errorf("feedback has a CI section:\n%s", h.feedback(0))
	}

	if ws, err := h.store.CIWaits(inOrg(t), it.ID, stReview); err != nil || len(ws) != 0 {
		t.Errorf("CI waits = %+v, %v; want none", ws, err)
	}

	if calls := h.codehost.ChecksCalls(); len(calls) != 0 {
		t.Errorf("Checks calls = %+v; want none", calls)
	}
}
