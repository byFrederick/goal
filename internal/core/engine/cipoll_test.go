package engine_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The commits, the second repository and the waitFor limits the CI poller tests use.
const (
	ciSHA, ciNewSHA = "c1c1c1c1", "c2c2c2c2"
	gitlabRepo      = "https://gitlab.com/acme/web"
	ciGrace, ciMax  = 5 * time.Minute, 60 * time.Minute
)

// ciLine is review (an agent station with waitFor: ci) on the organization's github and gitlab
// connections, which sign the poller in to each host.
func ciLine() domain.Line {
	review := agentStation(stReview, agReviewer, 1, 1)
	review.WaitFor = domain.CIWait{Grace: ciGrace, Max: ciMax, Rounds: domain.DefaultCIRounds}
	l := lineOf(review)
	l.Connections = []string{connGitHub, connGitLab}

	return l
}

// newCIHarness is a harness on ciLine whose agents never run.
func newCIHarness(t *testing.T) *harness {
	t.Helper()

	return newHarness(t, ciLine(), porttest.Sequence(domain.RunResult{Status: domain.RunSucceeded}))
}

// openWait opens a before-run CI wait of a new item at review on sha of the repository at repo,
// started now, on the pull request prURL when it is set.
func (h *harness) openWait(t *testing.T, repo, sha, prURL string) domain.CIWaitRecord {
	t.Helper()

	it := h.submit(t, domain.NewItem{})

	key, ok := domain.RepositoryKey(repo)
	if !ok {
		t.Fatalf("%s is not a repository", repo)
	}

	w, err := h.store.OpenCIWait(inOrg(t), domain.NewCIWait{
		Item: it.ID, Station: stReview, Visit: "visit-1", Repo: key, SHA: sha, PRURL: prURL,
		Phase: domain.CIPhaseBefore, Round: 1, StartedAt: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return w
}

// pollCI runs one PollCI of e, failing the test on an error.
func pollCI(t *testing.T, e *engine.Engine) {
	t.Helper()

	if _, err := e.PollCI(t.Context()); err != nil {
		t.Fatalf("PollCI = %v", err)
	}
}

// openWaits are the item's open CI waits.
func (h *harness) openWaits(t *testing.T, item domain.ItemID) []domain.CIWaitRecord {
	t.Helper()

	ws, err := h.store.OpenCIWaits(inOrg(t), item)
	if err != nil {
		t.Fatal(err)
	}

	return ws
}

// ciEnd is one "ci wait ended" log line: the outcome ciWaitEnded was given and the failed checks.
type ciEnd struct {
	Outcome domain.CIOutcome `json:"outcome"`
	Failed  []string         `json:"failed"`
}

// ciEnds are the waits ciWaitEnded was called for, by wait, failing the test when one was called
// for twice.
func (h *harness) ciEnds(t *testing.T) map[domain.CIWaitID]ciEnd {
	t.Helper()

	out := map[domain.CIWaitID]ciEnd{}

	sc := bufio.NewScanner(bytes.NewReader([]byte(h.logs.String())))
	for sc.Scan() {
		var rec struct {
			ciEnd

			Msg  string          `json:"msg"`
			Wait domain.CIWaitID `json:"wait"`
		}

		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}

		if rec.Msg != "ci wait ended" {
			continue
		}

		if _, twice := out[rec.Wait]; twice {
			t.Errorf("ci wait %s ended twice", rec.Wait)
		}

		out[rec.Wait] = rec.ciEnd
	}

	return out
}

// expectEnded fails unless wait ended with outcome, and its item has no open wait left.
func (h *harness) expectEnded(t *testing.T, w domain.CIWaitRecord, outcome domain.CIOutcome) {
	t.Helper()

	if got := h.ciEnds(t)[w.ID]; got.Outcome != outcome {
		t.Errorf("wait %s ended %q; want %q", w.ID, got.Outcome, outcome)
	}

	if open := h.openWaits(t, w.Item); len(open) != 0 {
		t.Errorf("open waits of %s = %+v; want none", w.Item, open)
	}
}

// expectOpen fails unless wait has not ended.
func (h *harness) expectOpen(t *testing.T, w domain.CIWaitRecord) domain.CIWaitRecord {
	t.Helper()

	if got, ok := h.ciEnds(t)[w.ID]; ok {
		t.Fatalf("wait %s ended %q; want it open", w.ID, got.Outcome)
	}

	open := h.openWaits(t, w.Item)
	if len(open) != 1 || open[0].ID != w.ID {
		t.Fatalf("open waits of %s = %+v; want %s", w.Item, open, w.ID)
	}

	return open[0]
}

// checksOf are CommitChecks with one check per state, each finished one with result.
func checksOf(result ports.CheckResult, states ...ports.CheckState) porttest.ChecksAnswer {
	var c ports.CommitChecks

	for i, s := range states {
		ch := ports.Check{Name: "check-" + string(rune('a'+i)), State: s}
		if s == ports.CheckFinished {
			ch.Result = result
		}

		c.Checks = append(c.Checks, ch)
	}

	return porttest.ChecksAnswer{Checks: c}
}

// Every check finished ends a wait: passed when none failed, else failed, naming the failed checks.
func TestCIPoller_PassedAndFailed(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	passed := h.openWait(t, appRepo, ciSHA, "")
	failed := h.openWait(t, appRepo, ciNewSHA, "")

	h.codehost.SetChecks(appRepo, ciSHA, checksOf(ports.CheckPassed, ports.CheckFinished, ports.CheckFinished))
	h.codehost.SetChecks(appRepo, ciNewSHA, porttest.ChecksAnswer{Checks: ports.CommitChecks{Checks: []ports.Check{
		{Name: "lint", State: ports.CheckFinished, Result: ports.CheckPassed},
		{Name: "test", State: ports.CheckFinished, Result: ports.CheckFailed},
	}}})

	pollCI(t, h.e)

	h.expectEnded(t, passed, domain.CIPassed)
	h.expectEnded(t, failed, domain.CIFailed)

	if got := h.ciEnds(t)[failed.ID].Failed; !slices.Equal(got, []string{"test"}) {
		t.Errorf("failed checks = %v; want [test]", got)
	}
}

// No check within the station's grace after the wait started ends it as no_ci; checks that
// appeared and are still running keep their wait open past grace.
func TestCIPoller_NoChecksWithinGrace(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	none := h.openWait(t, appRepo, ciSHA, "")
	running := h.openWait(t, appRepo, ciNewSHA, "")
	h.codehost.SetChecks(appRepo, ciNewSHA, checksOf("", ports.CheckRunning))

	pollCI(t, h.e)
	h.expectOpen(t, none)

	if p := h.expectOpen(t, running).Poll; !p.FirstCheckSeenAt.Equal(t0) {
		t.Errorf("first check seen at %s; want %s", p.FirstCheckSeenAt, t0)
	}

	h.clock.Advance(ciGrace - time.Second)
	pollCI(t, h.e)
	h.expectOpen(t, none)

	h.clock.Advance(domain.CIPollMaxInterval)
	pollCI(t, h.e)
	h.expectEnded(t, none, domain.CINoCI)
	h.expectOpen(t, running)
}

// A wait whose checks have not finished when the station's max has passed since it started ends
// as timed_out.
func TestCIPoller_MaxTimesOut(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	w := h.openWait(t, appRepo, ciSHA, "")
	h.codehost.SetChecks(appRepo, ciSHA, checksOf(ports.CheckPassed, ports.CheckFinished, ports.CheckRunning))

	for h.clock.Now().Before(t0.Add(ciMax - domain.CIPollMaxInterval)) {
		pollCI(t, h.e)
		h.expectOpen(t, w)
		h.clock.Advance(domain.CIPollMaxInterval)
	}

	h.clock.Advance(domain.CIPollMaxInterval)
	pollCI(t, h.e)
	h.expectEnded(t, w, domain.CITimedOut)
}

// A pull request whose head moved moves its wait to the new head, which the poller then polls;
// the superseded wait is not handed on as ended.
func TestCIPoller_HeadMovedFollowsNewHead(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	w := h.openWait(t, appRepo, ciSHA, prURL)
	h.codehost.SetChecks(appRepo, ciSHA, porttest.ChecksAnswer{Checks: ports.CommitChecks{Head: ciNewSHA}})
	h.codehost.SetChecks(appRepo, ciNewSHA, checksOf("", ports.CheckRunning), checksOf(ports.CheckPassed, ports.CheckFinished))

	pollCI(t, h.e) // the new head's poll is due at once, so the same tick polls it too

	open := h.openWaits(t, w.Item)
	if len(open) != 1 || open[0].SHA != ciNewSHA || open[0].PRURL != prURL || open[0].ID == w.ID {
		t.Fatalf("open waits after the head moved = %+v; want one on %s", open, ciNewSHA)
	}

	moved := open[0]

	h.clock.Advance(domain.CIPollMinInterval)
	pollCI(t, h.e)

	calls := h.codehost.ChecksCalls()
	if len(calls) != 3 || calls[0].SHA != ciSHA || calls[0].URL != prURL || calls[1].SHA != ciNewSHA || calls[2].SHA != ciNewSHA {
		t.Errorf("Checks calls = %+v; want %s then %s twice, on the pull request", calls, ciSHA, ciNewSHA)
	}

	h.expectEnded(t, moved, domain.CIPassed)

	if _, ok := h.ciEnds(t)[w.ID]; ok {
		t.Errorf("the superseded wait %s was handed on as ended", w.ID)
	}
}

// A token that cannot read CI ends the wait as unreadable: hidden CI never counts as passed.
func TestCIPoller_UnreadableEndsWait(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	w := h.openWait(t, appRepo, ciSHA, "")
	h.codehost.SetChecks(appRepo, ciSHA, porttest.ChecksAnswer{Err: &ports.CINotReadableError{Permission: "Checks: read"}})

	pollCI(t, h.e)
	h.expectEnded(t, w, domain.CIUnreadable)
}

// Two items waiting on one commit share its poll: one Checks call per tick decides both.
func TestCIPoller_TwoItemsShareOneCommitPoll(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	a := h.openWait(t, appRepo, ciSHA, "")
	b := h.openWait(t, appRepo, ciSHA, "")
	h.codehost.SetChecks(appRepo, ciSHA, checksOf("", ports.CheckRunning), checksOf(ports.CheckPassed, ports.CheckFinished))

	pollCI(t, h.e)

	if calls := h.codehost.ChecksCalls(); len(calls) != 1 {
		t.Fatalf("Checks calls after one tick = %d; want 1", len(calls))
	}

	h.expectOpen(t, a)
	h.expectOpen(t, b)

	pollCI(t, h.e) // not due yet

	h.clock.Advance(domain.CIPollMinInterval)
	pollCI(t, h.e)

	if calls := h.codehost.ChecksCalls(); len(calls) != 2 {
		t.Errorf("Checks calls after two ticks = %d; want 2", len(calls))
	}

	h.expectEnded(t, a, domain.CIPassed)
	h.expectEnded(t, b, domain.CIPassed)
}

// Each repository is polled with the adapter of its host's kind and the token of the factory's
// connection for that host.
func TestCIPoller_TwoRepositories(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	app := h.openWait(t, appRepo, ciSHA, "")
	web := h.openWait(t, gitlabRepo, ciNewSHA, "")
	h.codehost.SetChecks(appRepo, ciSHA, checksOf(ports.CheckPassed, ports.CheckFinished))
	h.gitlab.SetChecks(gitlabRepo, ciNewSHA, checksOf(ports.CheckFailed, ports.CheckFinished))

	pollCI(t, h.e)

	gh, gl := h.codehost.ChecksCalls(), h.gitlab.ChecksCalls()
	if len(gh) != 1 || gh[0].Repo.Key() != app.Repo || gh[0].Token != "github-token" || gh[0].SHA != ciSHA {
		t.Errorf("github Checks calls = %+v; want one on %s with the github connection's token", gh, app.Repo)
	}

	if len(gl) != 1 || gl[0].Repo.Key() != web.Repo || gl[0].Token != "gitlab-token" || gl[0].SHA != ciNewSHA {
		t.Errorf("gitlab Checks calls = %+v; want one on %s with the gitlab connection's token", gl, web.Repo)
	}

	h.expectEnded(t, app, domain.CIPassed)
	h.expectEnded(t, web, domain.CIFailed)
}

// A rate limit postpones the poll to its RetryAt and keeps the wait: no Checks call before then.
func TestCIPoller_RateLimitHonoured(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	w := h.openWait(t, appRepo, ciSHA, "")
	retry := t0.Add(10 * time.Minute)
	h.codehost.SetChecks(appRepo, ciSHA,
		porttest.ChecksAnswer{Err: &ports.RateLimitedError{RetryAt: retry}}, checksOf(ports.CheckPassed, ports.CheckFinished))

	pollCI(t, h.e)

	if p := h.expectOpen(t, w).Poll; !p.NextPollAt.Equal(retry) || p.LastError == "" ||
		p.Interval < domain.CIPollMinInterval || p.Interval > domain.CIPollMaxInterval {
		t.Errorf("rate limited poll = %+v; want due at %s with the error and the interval within bounds", p, retry)
	}

	for h.clock.Now().Before(retry.Add(-time.Minute)) {
		h.clock.Advance(time.Minute)
		pollCI(t, h.e)
	}

	if calls := h.codehost.ChecksCalls(); len(calls) != 1 {
		t.Fatalf("Checks calls before RetryAt = %d; want 1", len(calls))
	}

	h.expectOpen(t, w)

	h.clock.Advance(time.Minute)
	pollCI(t, h.e)

	if calls := h.codehost.ChecksCalls(); len(calls) != 2 {
		t.Errorf("Checks calls at RetryAt = %d; want 2", len(calls))
	}

	h.expectEnded(t, w, domain.CIPassed)
}

// Any other error records it and backs the poll off, doubling up to the longest interval, and
// keeps the wait.
func TestCIPoller_OtherErrorBacksOff(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	w := h.openWait(t, appRepo, ciSHA, "")
	h.codehost.SetChecks(appRepo, ciSHA, porttest.ChecksAnswer{Err: errors.New("connection reset")})

	for _, want := range []time.Duration{30 * time.Second, time.Minute, domain.CIPollMaxInterval, domain.CIPollMaxInterval} {
		if _, err := h.e.PollCI(t.Context()); err == nil {
			t.Fatal("PollCI of a failing host = nil; want its error")
		}

		p := h.expectOpen(t, w).Poll
		if p.Interval != want || p.LastError == "" || !p.NextPollAt.Equal(h.clock.Now().Add(want)) {
			t.Fatalf("poll after an error = %+v; want every %s from now, with the error", p, want)
		}

		h.clock.Advance(want)
	}
}

// A poll another holder leased (an instance that died mid-poll) is not polled until its lease
// ends; then a new engine on the same store leases it, polls it and ends the wait.
func TestCIPoller_RestartResumesAfterLeaseExpiry(t *testing.T) {
	t.Parallel()

	h := newCIHarness(t)
	w := h.openWait(t, appRepo, ciSHA, "")
	h.codehost.SetChecks(appRepo, ciSHA, checksOf(ports.CheckPassed, ports.CheckFinished))

	leased, err := h.store.LeaseDueCIPolls(inOrg(t), h.clock.Now(), "goald-gone", time.Minute, 10)
	if err != nil || len(leased) != 1 {
		t.Fatalf("LeaseDueCIPolls = %+v, %v; want the one poll", leased, err)
	}

	restarted := h.engine(t, h.store)

	pollCI(t, restarted)
	h.clock.Advance(time.Minute - time.Second)
	pollCI(t, restarted)

	if calls := h.codehost.ChecksCalls(); len(calls) != 0 {
		t.Fatalf("Checks calls under another holder's lease = %d; want 0", len(calls))
	}

	h.expectOpen(t, w)

	h.clock.Advance(time.Second)
	pollCI(t, restarted)

	if calls := h.codehost.ChecksCalls(); len(calls) != 1 {
		t.Errorf("Checks calls after the lease ended = %d; want 1", len(calls))
	}

	h.expectEnded(t, w, domain.CIPassed)
}
