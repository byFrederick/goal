package engine_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

func TestRetry_BlockedItemGoesBackToItsQueue(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)),
		porttest.Sequence(domain.RunResult{Status: domain.RunFailed, Error: "container exited 1"}))
	it := h.submit(t, domain.NewItem{})

	if _, err := h.e.Retry(inOrg(t), it.ID, human); !errors.Is(err, engine.ErrNotBlocked) {
		t.Errorf("Retry of a queued item = %v, want ErrNotBlocked", err)
	}

	h.step(t)

	got, err := h.e.Retry(inOrg(t), it.ID, human)
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stWork, State: domain.StateQueued}) || got.ReworkCount != 0 {
		t.Errorf("retried item at %+v rework %d, want queued at %s with no rework", got.Position(), got.ReworkCount, stWork)
	}

	if retried := h.eventsOf(it.ID, domain.EventRetried); len(retried) != 1 || retried[0].Actor != human.Name || retried[0].Station != stWork {
		t.Errorf("retried events %+v, want one by fcruz at %s", retried, stWork)
	}

	if _, err := h.e.Retry(inOrg(t), it.ID, human); !errors.Is(err, engine.ErrNotBlocked) {
		t.Errorf("second Retry = %v, want ErrNotBlocked", err)
	}
}

// human decides in the tests: the local user, as every request in phase 2.2.
var human = domain.LocalUser

const (
	stMerge  = "merge"
	prURL    = "https://github.com/acme/app/pull/7"
	docsURL  = "https://github.com/acme/docs/pull/2"
	mrURL    = "https://gitlab.com/acme/web/-/merge_requests/3"
	headSHA  = "4b825dc6"
	itBranch = "goal/1-an-item"

	codeHostGitHub = "github"
	codeHostGitLab = "gitlab"
	connGitHub     = "github"
	connGitLab     = "gitlab"
)

// mergeLine is work (an agent that reports pull requests) → merge (human, action merge) → done,
// with the organization's github and gitlab connections. It has no repository: the pull
// requests' URLs say where they are.
func mergeLine(action domain.StationAction) domain.Line {
	l := lineOf(agentStation(stWork, agWorker, 1, 1), domain.Station{ID: stMerge, Kind: domain.KindHuman, Action: action})
	l.Connections = []string{connGitHub, connGitLab}

	return l
}

// expectMergeUsed fails unless the merges of item opened exactly conn, for the person's merge:
// the connection.used row names the person and the item, and no run.
func (h *harness) expectMergeUsed(t *testing.T, item domain.ItemID, conn string) {
	t.Helper()

	if used := h.mergesUsed(t, item); len(used) != 1 || used[0].Data["name"] != conn || used[0].Actor.ID != human.ID ||
		used[0].Data["run_id"] != nil {
		t.Errorf("connection.used of the merge %+v; want %s only, by the person, naming no run", used, conn)
	}
}

// expectRefusedMerge fails unless err says how to fix a version with removed forms, and unless a
// merge refused for its hosts (ErrNoCodeHost) opened no connection.
func (h *harness) expectRefusedMerge(t *testing.T, item domain.ItemID, err error, removed bool) {
	t.Helper()

	if removed && !strings.Contains(err.Error(), "publish a version with connections") {
		t.Errorf("Merge = %v; want it to say how to fix the version", err)
	}

	if used := h.mergesUsed(t, item); errors.Is(err, engine.ErrNoCodeHost) && len(used) != 0 {
		t.Errorf("connection.used of a merge refused for its hosts %+v; want none opened", used)
	}
}

// setRemoved makes l the line of a version with a form connections replaced.
func (h *harness) setRemoved(t *testing.T, l domain.Line) {
	t.Helper()

	l.Removed = []string{"spec.runtime.repository.token (env-reference-removed)"}
	if err := h.e.SetLines([]domain.Line{l}); err != nil {
		t.Fatal(err)
	}
}

// mergesUsed are the connection.used rows of merges of item: the connections they opened.
func (h *harness) mergesUsed(t *testing.T, item domain.ItemID) []domain.AuditEntry {
	t.Helper()

	used, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Limit: 100, Action: domain.AuditConnectionUsed})
	if err != nil {
		t.Fatal(err)
	}

	return slices.DeleteFunc(used, func(e domain.AuditEntry) bool { return e.Data["item_id"] != string(item) })
}

func openPR(url string) ports.PullRequest {
	return ports.PullRequest{
		URL: url, Open: true, Mergeable: true, HeadSHA: headSHA, HeadBranch: itBranch, SameRepository: true, BaseBranch: "main",
	}
}

func squashOf(url, conn string) porttest.MergeCall {
	return porttest.MergeCall{URL: url, Connection: conn, Merge: ports.Merge{Method: engine.MergeSquash, HeadSHA: headSHA}}
}

// atMerge submits an item whose work station outputs output and steps it to the merge station.
func (h *harness) atMerge(t *testing.T) domain.Item {
	t.Helper()

	it := h.submit(t, domain.NewItem{})
	h.step(t)

	if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stMerge, State: domain.StateWaitingHuman}) {
		t.Fatalf("item at %+v, want waiting at the merge station", got.Position())
	}

	return it
}

func TestMerge_MergesThePullRequestThenApproves(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pr          func(*ports.PullRequest)
		wantMerges  []porttest.MergeCall
		wantDeleted []string
	}{
		{
			name: "open and mergeable", pr: func(*ports.PullRequest) {},
			wantMerges: []porttest.MergeCall{squashOf(prURL, connGitHub)}, wantDeleted: []string{itBranch},
		},
		{
			name: "from a fork: its branch is not ours to delete", pr: func(pr *ports.PullRequest) { pr.SameRepository = false },
			wantMerges: []porttest.MergeCall{squashOf(prURL, connGitHub)},
		},
		{
			// A merge whose approval failed is approved when merged again.
			name: "already merged", pr: func(pr *ports.PullRequest) { pr.Open, pr.Merged, pr.MergeSHA = false, true, "abc123" },
			wantDeleted: []string{itBranch},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, mergeLine(domain.ActionMerge), porttest.Succeed(`{"pull_request":"`+prURL+`"}`))
			pr := openPR(prURL)
			tt.pr(&pr)
			h.codehost.Add(pr)

			it := h.atMerge(t)

			got, err := h.e.Merge(inOrg(t), it.ID, human, "")
			if err != nil {
				t.Fatal(err)
			}

			if got.Position() != (domain.Position{Station: stDone, State: domain.StateDone}) {
				t.Errorf("item at %+v, want done", got.Position())
			}

			if merges := h.codehost.Merges(); !reflect.DeepEqual(merges, tt.wantMerges) {
				t.Errorf("merges %+v, want %+v", merges, tt.wantMerges)
			}

			if deleted := h.codehost.DeletedBranches(); !reflect.DeepEqual(deleted, tt.wantDeleted) {
				t.Errorf("deleted branches %v, want %v", deleted, tt.wantDeleted)
			}

			merged := h.eventsOf(it.ID, domain.EventMerged)
			if len(merged) != 1 || merged[0].Actor != human.Name || merged[0].Data["pull_request"] != prURL ||
				merged[0].Data["branch_deleted"] != (len(tt.wantDeleted) == 1) {
				t.Errorf("merged events %+v, want one by fcruz naming the pull request", merged)
			}

			if approved := h.eventsOf(it.ID, domain.EventApproved); len(approved) != 1 || approved[0].Actor != human.Name {
				t.Errorf("approved events %+v, want one by fcruz", approved)
			}

			// A merge at a merge station is a decision the line requires.
			plannedAll(t, append(merged, h.eventsOf(it.ID, domain.EventApproved)...))

			h.expectMergeUsed(t, it.ID, connGitHub)
		})
	}
}

// Every pull request the outputs name is merged, each through the code host of the connection
// for its URL's host, and the approval carries one merged event per pull request.
func TestMerge_EveryPullRequestThroughItsHostsConnection(t *testing.T) {
	t.Parallel()

	output := `{"pull_request":"` + prURL + `","pull_requests":["` + mrURL + `","` + docsURL + `","` + prURL + `"]}`
	h := newHarness(t, mergeLine(domain.ActionMerge), porttest.Succeed(output))
	h.codehost.Add(openPR(prURL))
	h.codehost.Add(openPR(docsURL))
	h.gitlab.Add(openPR(mrURL))

	it := h.atMerge(t)

	got, err := h.e.Merge(inOrg(t), it.ID, human, "")
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stDone, State: domain.StateDone}) {
		t.Errorf("item at %+v, want done", got.Position())
	}

	if merges := h.codehost.Merges(); !reflect.DeepEqual(merges, []porttest.MergeCall{squashOf(prURL, connGitHub), squashOf(docsURL, connGitHub)}) {
		t.Errorf("github merges %+v, want app#7 then docs#2 with the github connection", merges)
	}

	if merges := h.gitlab.Merges(); !reflect.DeepEqual(merges, []porttest.MergeCall{squashOf(mrURL, connGitLab)}) {
		t.Errorf("gitlab merges %+v, want web!3 with the gitlab connection", merges)
	}

	var named []string

	for _, e := range h.eventsOf(it.ID, domain.EventMerged) {
		url, _ := e.Data["pull_request"].(string)
		named = append(named, url)
	}

	if want := []string{prURL, mrURL, docsURL}; !slices.Equal(named, want) {
		t.Errorf("merged events name %v, want %v", named, want)
	}

	if approved := h.eventsOf(it.ID, domain.EventApproved); len(approved) != 1 {
		t.Errorf("approved events %+v, want one", approved)
	}
}

// A merge that stops at a refusal leaves the item where it is and what it merged merged; asked
// again, it skips those and merges the rest.
func TestMerge_PartialMergeThenRetry(t *testing.T) {
	t.Parallel()

	output := `{"pull_requests":["` + prURL + `","` + mrURL + `","` + docsURL + `"]}`
	h := newHarness(t, mergeLine(domain.ActionMerge), porttest.Succeed(output))
	h.codehost.Add(openPR(prURL))
	h.codehost.Add(openPR(docsURL))

	conflicted := openPR(mrURL)
	conflicted.Mergeable = false
	h.gitlab.Add(conflicted)

	it := h.atMerge(t)
	before := h.item(t, it.ID)

	_, err := h.e.Merge(inOrg(t), it.ID, human, "")
	if !errors.Is(err, ports.ErrNotMergeable) || !strings.Contains(err.Error(), mrURL) {
		t.Fatalf("Merge = %v, want ErrNotMergeable naming %s", err, mrURL)
	}

	if after := h.item(t, it.ID); after.Position() != before.Position() || len(h.eventsOf(it.ID, domain.EventMerged)) != 0 {
		t.Errorf("item moved to %+v with merged events; want it waiting, nothing recorded", after.Position())
	}

	if merges := h.codehost.Merges(); !reflect.DeepEqual(merges, []porttest.MergeCall{squashOf(prURL, connGitHub)}) {
		t.Errorf("github merges %+v, want only app#7, before the refusal", merges)
	}

	// The conflict is resolved; the retry skips app#7, which is merged already.
	h.gitlab.Add(openPR(mrURL))

	got, err := h.e.Merge(inOrg(t), it.ID, human, "")
	if err != nil {
		t.Fatal(err)
	}

	if got.Position() != (domain.Position{Station: stDone, State: domain.StateDone}) {
		t.Errorf("item at %+v, want done", got.Position())
	}

	if merges := h.codehost.Merges(); !reflect.DeepEqual(merges, []porttest.MergeCall{squashOf(prURL, connGitHub), squashOf(docsURL, connGitHub)}) {
		t.Errorf("github merges %+v, want app#7 once, then docs#2", merges)
	}

	if merges := h.gitlab.Merges(); len(merges) != 1 || merges[0].URL != mrURL {
		t.Errorf("gitlab merges %+v, want web!3 once (the refused one was not merged)", merges)
	}

	if merged := h.eventsOf(it.ID, domain.EventMerged); len(merged) != 3 {
		t.Errorf("merged events %+v, want one per pull request", merged)
	}
}

func TestMerge_RefusesAndChangesNothing(t *testing.T) {
	t.Parallel()

	const elsewhere = "https://code.example.com/acme/app/pull/1"

	tests := []struct {
		name    string
		action  domain.StationAction
		output  string
		conns   []string // the line's connections; nil: mergeLine's github and gitlab
		removed bool     // the line is of a version with a form connections replaced
		pr      func(*ports.PullRequest)
		early   bool // ask before the item reaches the merge station
		wantErr error
		says    string // the refusal's message contains it
		notSays string // and not this
	}{
		{name: "not yet at a human station", action: domain.ActionMerge, early: true, wantErr: engine.ErrNotWaitingHuman},
		{name: "a human station that does not merge", action: "", wantErr: engine.ErrNotMergeStep},
		{name: "no pull request in the outputs", action: domain.ActionMerge, output: `{"summary":"done"}`, wantErr: ports.ErrNotMergeable},
		{name: "closed", action: domain.ActionMerge, pr: func(pr *ports.PullRequest) { pr.Open = false }, wantErr: ports.ErrNotMergeable},
		{
			name: "conflicts or failing checks", action: domain.ActionMerge, pr: func(pr *ports.PullRequest) { pr.Mergeable = false },
			wantErr: ports.ErrNotMergeable, says: "conflicts", notSays: "computing",
		},
		{
			name: "mergeability still being computed", action: domain.ActionMerge,
			pr:      func(pr *ports.PullRequest) { pr.Mergeable, pr.MergeabilityPending = false, true },
			wantErr: ports.ErrNotMergeable, says: "still computing", notSays: "conflicts",
		},
		{
			// Refused before anything merges, the github pull request included.
			name: "a host no connection serves", action: domain.ActionMerge,
			output: `{"pull_request":"` + prURL + `","pull_requests":["` + elsewhere + `"]}`, wantErr: engine.ErrNoCodeHost,
		},
		{name: "no connection at all", action: domain.ActionMerge, conns: []string{}, wantErr: engine.ErrNoCodeHost},
		{
			// A version published before the cutover merged with the install's token, which is gone.
			name: "a version with a removed form and no connection", action: domain.ActionMerge, conns: []string{}, removed: true,
			wantErr: engine.ErrNoCodeHost,
		},
		{
			name: "a connection for the host whose kind names no code host", action: domain.ActionMerge,
			conns: []string{connPlain}, wantErr: engine.ErrNoCodeHost,
		},
		{
			name: "a connection for the host whose code host goald lacks", action: domain.ActionMerge,
			conns: []string{connBitbucket}, wantErr: engine.ErrNoCodeHost,
		},
		{
			name: "a connection for another host", action: domain.ActionMerge,
			conns: []string{connGHE}, wantErr: engine.ErrNoCodeHost,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output := tt.output
			if output == "" {
				output = `{"pull_request":"` + prURL + `"}`
			}

			line := mergeLine(tt.action)
			if tt.conns != nil {
				line.Connections = tt.conns
			}

			h := newHarness(t, line, porttest.Succeed(output))

			pr := openPR(prURL)

			if tt.pr != nil {
				tt.pr(&pr)
			}

			h.codehost.Add(pr)

			it := h.submit(t, domain.NewItem{})
			if !tt.early {
				h.step(t)
			}

			if tt.removed { // the item reached the merge station on a version that ran
				h.setRemoved(t, line)
			}

			before := h.item(t, it.ID)

			_, err := h.e.Merge(inOrg(t), it.ID, human, "")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Merge = %v, want %v", err, tt.wantErr)
			}

			refusalSays(t, err, tt.says, tt.notSays)

			h.expectRefusedMerge(t, it.ID, err, tt.removed)

			if after := h.item(t, it.ID); after.Position() != before.Position() {
				t.Errorf("item moved from %+v to %+v", before.Position(), after.Position())
			}

			if len(h.codehost.Merges()) != 0 || len(h.codehost.DeletedBranches()) != 0 {
				t.Errorf("merges %v, deleted %v; want none", h.codehost.Merges(), h.codehost.DeletedBranches())
			}
		})
	}
}

// refusalSays fails unless err's message contains says and, when notSays is set, not notSays.
func refusalSays(t *testing.T, err error, says, notSays string) {
	t.Helper()

	if !strings.Contains(err.Error(), says) || (notSays != "" && strings.Contains(err.Error(), notSays)) {
		t.Errorf("Merge = %v, want a message saying %q and not %q", err, says, notSays)
	}
}

// plannedAll fails unless every one of events carries data.planned true.
func plannedAll(t *testing.T, events []domain.Event) {
	t.Helper()

	for _, e := range events {
		if e.Data[domain.KeyPlanned] != true {
			t.Errorf("%s event data %v, want planned true", e.Kind, e.Data)
		}
	}
}
