package engine_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const (
	stBuild  = "build"
	stReview = "review"
)

// gateMergeLine is build (an agent that reports pull requests) → review (a gate whose approval
// merges) → done, with the organization's github and gitlab connections.
func gateMergeLine() domain.Line {
	review := agentStation(stReview, agReviewer, 1, 1)
	review.Gate = &domain.Gate{OnReject: stBuild, MaxRework: 2}
	review.Action = domain.ActionMerge

	l := lineOf(agentStation(stBuild, agWorker, 1, 1), review)
	l.Connections = []string{connGitHub, connGitLab}

	return l
}

// The gate's approval merges every pull request the outputs name, its own output's included, as
// the engine: no person touched the item.
func TestGateMerge_MergesOnApprovalAsTheEngine(t *testing.T) {
	t.Parallel()

	h := newHarness(t, gateMergeLine(), byAgent(map[string]string{
		agWorker:   `{"pull_request":"` + prURL + `"}`,
		agReviewer: `{"decision":"approve","pull_requests":["` + docsURL + `"]}`,
	}))
	h.codehost.Add(openPR(prURL))
	h.codehost.Add(openPR(docsURL))

	it := h.submit(t, domain.NewItem{})
	h.step(t) // built
	h.step(t) // reviewed and merged

	if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stDone, State: domain.StateDone}) {
		t.Fatalf("item at %+v, want done (events %v)", got.Position(), h.kinds(it.ID))
	}

	if merges := h.codehost.Merges(); !reflect.DeepEqual(merges, []porttest.MergeCall{squashOf(prURL, connGitHub), squashOf(docsURL, connGitHub)}) {
		t.Errorf("merges %+v, want app#7 then docs#2 with the github connection", merges)
	}

	if deleted := h.codehost.DeletedBranches(); len(deleted) != 2 {
		t.Errorf("deleted branches %v, want both head branches", deleted)
	}

	if approved := h.eventsOf(it.ID, domain.EventGateApproved); len(approved) != 1 || approved[0].Actor != agReviewer {
		t.Errorf("gate_approved events %+v, want one by the reviewer", approved)
	}

	merged := h.eventsOf(it.ID, domain.EventMerged)
	if len(merged) != 2 {
		t.Fatalf("merged events %+v, want one per pull request", merged)
	}

	for _, e := range merged {
		if e.Actor != domain.ActorEngine || e.Data["actor"] != domain.EngineActor.Ref() || e.Station != stReview {
			t.Errorf("merged event %+v, want by the engine at the gate", e)
		}
	}

	if touches := domain.TouchesOf(h.store.Events(it.ID)); len(touches) != 0 {
		t.Errorf("touches %+v, want none: the engine merged", touches)
	}

	if used := h.mergesUsed(t, it.ID); len(used) != 1 || used[0].Data["name"] != connGitHub ||
		used[0].Actor != domain.EngineActor || used[0].Data["run_id"] != nil {
		t.Errorf("connection.used of the merge %+v; want github only, by the engine, naming no run", used)
	}

	h.expectOutcomes(t, it.ID, domain.LeaseAdvanced, domain.LeaseAdvanced)
}

// A refused merge moves nothing past the gate: the item is blocked there with the reason.
func TestGateMerge_RefusedBlocksAndRetrySkipsMerged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string   // the build's output; empty: names app#7
		conns  []string // nil: github and gitlab
		pr     func(*ports.PullRequest)
		moved  bool // a push moves the head between the read and the merge
		reason string
	}{
		{name: "closed", pr: func(pr *ports.PullRequest) { pr.Open = false }, reason: "is closed"},
		{name: "not mergeable", pr: func(pr *ports.PullRequest) { pr.Mergeable = false }, reason: "has conflicts"},
		{name: "head moved", moved: true, reason: "head moved"},
		{name: "no pull request in the outputs", output: `{"summary":"done"}`, reason: "no output names a pull_request"},
		{name: "no connection serves the host", conns: []string{}, reason: "no connection of the factory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output := tt.output
			if output == "" {
				output = `{"pull_request":"` + prURL + `"}`
			}

			line := gateMergeLine()
			if tt.conns != nil {
				line.Connections = tt.conns
			}

			h := newHarness(t, line, byAgent(map[string]string{agWorker: output, agReviewer: `{"decision":"approve"}`}))

			pr := openPR(prURL)
			if tt.pr != nil {
				tt.pr(&pr)
			}

			h.codehost.Add(pr)

			if tt.moved {
				h.codehost.MoveHeadAfterRead(prURL, "f00d")
			}

			it := h.submit(t, domain.NewItem{})
			h.step(t)
			h.step(t)

			h.expectBlockedAtGate(t, it.ID, tt.reason)
		})
	}

	t.Run("partial merge, then retry", func(t *testing.T) {
		t.Parallel()

		output := `{"pull_requests":["` + prURL + `","` + mrURL + `","` + docsURL + `"]}`
		h := newHarness(t, gateMergeLine(), byAgent(map[string]string{agWorker: output, agReviewer: `{"decision":"approve"}`}))
		h.codehost.Add(openPR(prURL))
		h.codehost.Add(openPR(docsURL))

		conflicted := openPR(mrURL)
		conflicted.Mergeable = false
		h.gitlab.Add(conflicted)

		it := h.submit(t, domain.NewItem{})
		h.step(t)
		h.step(t)

		h.expectBlockedAtGate(t, it.ID, mrURL)

		if merges := h.codehost.Merges(); !reflect.DeepEqual(merges, []porttest.MergeCall{squashOf(prURL, connGitHub)}) {
			t.Errorf("github merges %+v, want only app#7, before the refusal", merges)
		}

		// The conflict is resolved; the gate runs again and its merge skips app#7.
		h.gitlab.Add(openPR(mrURL))

		if _, err := h.e.Retry(inOrg(t), it.ID, human); err != nil {
			t.Fatal(err)
		}

		h.step(t)

		if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stDone, State: domain.StateDone}) {
			t.Fatalf("item at %+v, want done (events %v)", got.Position(), h.kinds(it.ID))
		}

		if merges := h.codehost.Merges(); !reflect.DeepEqual(merges, []porttest.MergeCall{squashOf(prURL, connGitHub), squashOf(docsURL, connGitHub)}) {
			t.Errorf("github merges %+v, want app#7 once, then docs#2", merges)
		}

		if merges := h.gitlab.Merges(); len(merges) != 1 || merges[0].URL != mrURL {
			t.Errorf("gitlab merges %+v, want web!3 once", merges)
		}

		if merged := h.eventsOf(it.ID, domain.EventMerged); len(merged) != 3 {
			t.Errorf("merged events %+v, want one per pull request", merged)
		}
	})
}

func TestGateMerge_RejectMergesNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t, gateMergeLine(), byAgent(map[string]string{
		agWorker:   `{"pull_request":"` + prURL + `"}`,
		agReviewer: `{"decision":"reject","reason":"no test for the sweep"}`,
	}))
	h.codehost.Add(openPR(prURL))

	it := h.submit(t, domain.NewItem{})
	h.step(t)
	h.step(t)

	if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stBuild, State: domain.StateQueued}) || got.ReworkCount != 1 {
		t.Fatalf("item at %+v rework %d, want back at build", got.Position(), got.ReworkCount)
	}

	if len(h.codehost.Merges()) != 0 || len(h.eventsOf(it.ID, domain.EventMerged)) != 0 {
		t.Errorf("merges %+v, merged events %v; want none", h.codehost.Merges(), h.kinds(it.ID))
	}
}

// expectBlockedAtGate fails unless item is blocked at the review gate, with a blocked event whose
// reason says reason, and no merged event.
func (h *harness) expectBlockedAtGate(t *testing.T, item domain.ItemID, reason string) {
	t.Helper()

	if got := h.item(t, item); got.Position() != (domain.Position{Station: stReview, State: domain.StateBlocked}) {
		t.Fatalf("item at %+v, want blocked at the gate (events %v)", got.Position(), h.kinds(item))
	}

	blocked := h.eventsOf(item, domain.EventBlocked)
	if r, _ := blocked[len(blocked)-1].Data["reason"].(string); !strings.Contains(r, reason) {
		t.Errorf("blocked reason %q, want it to say %q", r, reason)
	}

	if merged := h.eventsOf(item, domain.EventMerged); len(merged) != 0 {
		t.Errorf("merged events %+v, want none on a blocked item", merged)
	}
}
