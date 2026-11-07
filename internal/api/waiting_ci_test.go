package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The repository and commits of TestAPI_WaitingForCI.
const (
	ciRepo      = "github.com/acme/app"
	ciFailedSHA = "aaa111"
	ciSHA       = "bbb222"
	ciTestsURL  = "https://github.com/acme/app/actions/runs/2"
)

// A part queued at build, a station with waitFor: ci, whose first CI round failed and whose open
// wait's poll read 2 of 3 checks finished, waits for kind ci: getItem, its Board card and its
// card in the list carry the commit, every check with its state, result, link and failed jobs,
// the counts, and the failed round's failed checks.
func TestAPI_WaitingForCI(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, parts := h.assembledOf(t, domain.NewPart{Key: "server", Title: "the server", Paths: []string{"cmd/goald/main.go"}})
	part := parts[0]

	if part.Station != stBuild || part.State != domain.StateQueued {
		part = h.move(t, part, domain.Position{Station: stBuild, State: domain.StateQueued}, nil)
	}

	first := h.openCIWait(t, part, ciFailedSHA, 1)
	h.recordChecks(t, ciFailedSHA, ports.Check{
		Name: "test", State: ports.CheckFinished, Result: ports.CheckFailed, URL: ciTestsURL, FailedJobs: []string{"unit"},
	})

	if _, err := h.store.EndCIWaits(inOrg(t), []domain.CIWaitID{first.ID}, domain.CIFailed, h.clock.Now()); err != nil {
		t.Fatal(err)
	}

	h.openCIWait(t, part, ciSHA, 2)
	h.recordChecks(t, ciSHA,
		ports.Check{Name: "lint", State: ports.CheckFinished, Result: ports.CheckPassed},
		ports.Check{Name: "test", State: ports.CheckFinished, Result: ports.CheckFailed, URL: ciTestsURL, FailedJobs: []string{"unit", "race"}},
		ports.Check{Name: "e2e", State: ports.CheckRunning},
	)

	passed, failed := gen.CICheckResultPassed, gen.CICheckResultFailed
	url, pr := ciTestsURL, prURL
	failedTest := gen.CICheck{
		Repo: ciRepo, Name: "test", State: gen.CICheckStateFinished, Result: &failed, Url: &url, FailedJobs: []string{"unit"},
	}
	want := &gen.ItemWaiting{
		Kind: gen.WaitingCI, Items: []string{}, Files: []string{}, Needs: []gen.ItemWaitingNeed{},
		Ci: &gen.ItemWaitingCI{
			Commits: []gen.CICommit{{Repo: ciRepo, Sha: ciSHA, Url: &pr}},
			Checks: []gen.CICheck{
				{Repo: ciRepo, Name: "lint", State: gen.CICheckStateFinished, Result: &passed, FailedJobs: []string{}},
				{Repo: ciRepo, Name: "test", State: gen.CICheckStateFinished, Result: &failed, Url: &url, FailedJobs: []string{"unit", "race"}},
				{Repo: ciRepo, Name: "e2e", State: gen.CICheckStateRunning, FailedJobs: []string{}},
			},
			Finished: 2, Total: 3,
			Failure: &gen.CIFailure{Repo: ciRepo, Sha: ciFailedSHA, Url: &pr, Checks: []gen.CICheck{failedTest}},
		},
	}

	var it gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(part.ID), "", &it); code != http.StatusOK {
		t.Fatalf("GET %s = %d; want 200", part.ID, code)
	}

	if !reflect.DeepEqual(it.Waiting, want) {
		t.Errorf("getItem waiting = %s; want %s", jsonOf(t, it.Waiting), jsonOf(t, want))
	}

	var (
		list  gen.ItemPage
		board gen.Board
	)

	h.getReadingOnce(t, "OpenNeeds", "/v1/orgs/acme/factories/line/items", &list)
	h.getReadingOnce(t, "OpenNeeds", "/v1/orgs/acme/factories/line/board", &board)

	for from, cards := range map[string]map[int64]gen.ItemCard{"the list": byNumber(list.Items), "the board": cardsOf(board)} {
		if c, ok := cards[part.Number]; !ok || !reflect.DeepEqual(c.Waiting, want) {
			t.Errorf("%s: part card waiting = %s (on it: %t); want %s", from, jsonOf(t, c.Waiting), ok, jsonOf(t, want))
		}
	}
}

// openCIWait opens a before wait of it at its station on sha of ciRepo, in round, as the engine
// would have.
func (h *harness) openCIWait(t *testing.T, it domain.Item, sha string, round int) domain.CIWaitRecord {
	t.Helper()

	w, err := h.store.OpenCIWait(inOrg(t), domain.NewCIWait{
		Item: it.ID, Station: it.Station, Visit: "visit-1", Repo: ciRepo, SHA: sha, PRURL: prURL,
		Phase: domain.CIPhaseBefore, Round: round, StartedAt: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return w
}

// recordChecks records checks as a poll of sha on ciRepo read them, as goald's poller would have.
func (h *harness) recordChecks(t *testing.T, sha string, checks ...ports.Check) {
	t.Helper()

	ctx, now := inOrg(t), h.clock.Now()
	if _, err := h.store.LeaseDueCIPolls(ctx, now, "poller", time.Minute, 10); err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(ports.CommitChecks{Head: sha, Checks: checks})
	if err != nil {
		t.Fatal(err)
	}

	key := domain.CIPollKey{Host: "github.com", Repo: ciRepo, SHA: sha}
	if err := h.store.RecordCIPoll(ctx, key, "poller", domain.CIPollResult{
		Checks: b, FirstCheckSeenAt: now, NextPollAt: now.Add(domain.CIPollMinInterval), Interval: domain.CIPollMinInterval,
	}); err != nil {
		t.Fatal(err)
	}
}

// jsonOf is v as JSON, for a failure message that shows what pointers hold.
func jsonOf(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}
