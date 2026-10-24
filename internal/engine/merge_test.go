package engine_test

import (
	"errors"
	"reflect"
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
	headSHA  = "4b825dc6"
	itBranch = "goal/1-an-item"
)

// mergeLine is work (an agent that reports a pull request) → merge (human, action merge) →
// done, on a repository.
func mergeLine(action domain.StationAction) domain.Line {
	l := lineOf(agentStation(stWork, agWorker, 1, 1), domain.Station{ID: stMerge, Kind: domain.KindHuman, Action: action})
	l.Runtime.Repository = domain.Repository{URL: "https://github.com/acme/app.git", AuthRef: "env:REPO_TOKEN"}

	return l
}

func openPR() ports.PullRequest {
	return ports.PullRequest{
		URL: prURL, Open: true, Mergeable: true, HeadSHA: headSHA, HeadBranch: itBranch, SameRepository: true, BaseBranch: "main",
	}
}

func TestMerge_MergesThePullRequestThenApproves(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pr          func(*ports.PullRequest)
		wantMerges  []ports.Merge
		wantDeleted []string
	}{
		{
			name: "open and mergeable", pr: func(*ports.PullRequest) {},
			wantMerges: []ports.Merge{{Method: engine.MergeSquash, HeadSHA: headSHA}}, wantDeleted: []string{itBranch},
		},
		{
			name: "from a fork: its branch is not ours to delete", pr: func(pr *ports.PullRequest) { pr.SameRepository = false },
			wantMerges: []ports.Merge{{Method: engine.MergeSquash, HeadSHA: headSHA}},
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
			pr := openPR()
			tt.pr(&pr)
			h.codehost.Add(pr)

			it := h.submit(t, domain.NewItem{})
			h.step(t)

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
		})
	}
}

func TestMerge_RefusesAndChangesNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		action  domain.StationAction
		output  string
		pr      func(*ports.PullRequest)
		early   bool // ask before the item reaches the merge station
		wantErr error
	}{
		{name: "not yet at a human station", action: domain.ActionMerge, early: true, wantErr: engine.ErrNotWaitingHuman},
		{name: "a human station that does not merge", action: "", wantErr: engine.ErrNotMergeStep},
		{name: "no pull request in the outputs", action: domain.ActionMerge, output: `{"summary":"done"}`, wantErr: ports.ErrNotMergeable},
		{name: "closed", action: domain.ActionMerge, pr: func(pr *ports.PullRequest) { pr.Open = false }, wantErr: ports.ErrNotMergeable},
		{name: "conflicts or failing checks", action: domain.ActionMerge, pr: func(pr *ports.PullRequest) { pr.Mergeable = false }, wantErr: ports.ErrNotMergeable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output := tt.output
			if output == "" {
				output = `{"pull_request":"` + prURL + `"}`
			}

			h := newHarness(t, mergeLine(tt.action), porttest.Succeed(output))
			pr := openPR()

			if tt.pr != nil {
				tt.pr(&pr)
			}

			h.codehost.Add(pr)

			it := h.submit(t, domain.NewItem{})
			if !tt.early {
				h.step(t)
			}

			before := h.item(t, it.ID)

			if _, err := h.e.Merge(inOrg(t), it.ID, human, ""); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Merge = %v, want %v", err, tt.wantErr)
			}

			if after := h.item(t, it.ID); after.Position() != before.Position() {
				t.Errorf("item moved from %+v to %+v", before.Position(), after.Position())
			}

			if len(h.codehost.Merges()) != 0 || len(h.codehost.DeletedBranches()) != 0 {
				t.Errorf("merges %v, deleted %v; want none", h.codehost.Merges(), h.codehost.DeletedBranches())
			}
		})
	}
}
