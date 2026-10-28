package engine_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// integrationBranch is the integration branch of the assembly that the assembly helper builds:
// item 1, assemblyTitle; mainBranch is a repository's default branch.
const (
	assemblyTitle     = "big request"
	integrationBranch = "goal/a1-big-request"
	mainBranch        = "main"
)

// integrationLine is work (an agent) → merge (human, action merge, the landing station) → done,
// with a repository and flow.assemblies.integration in mode, refresh eachPart, and minParts.
func integrationLine(mode domain.IntegrationMode, minParts int) domain.Line {
	l := mergeLine(domain.ActionMerge)
	l.Runtime.Repository = domain.Repository{URL: repoURL, Connection: connGit}
	l.Flow.Assemblies.Integration = domain.IntegrationPolicy{Mode: mode, Refresh: domain.RefreshEachPart, MinParts: minParts}

	return l
}

// integrationSets counts the store's SetIntegrationBranch calls.
type integrationSets struct {
	ports.Store

	mu    sync.Mutex
	calls []string // branch@landAt
}

func (s *integrationSets) SetIntegrationBranch(ctx context.Context, root domain.ItemID, branch, landAt string, at time.Time) (bool, error) {
	s.mu.Lock()
	s.calls = append(s.calls, branch+"@"+landAt)
	s.mu.Unlock()

	return s.Store.SetIntegrationBranch(ctx, root, branch, landAt, at)
}

func (s *integrationSets) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string{}, s.calls...)
}

// countSets makes the harness's engine run on a store that counts SetIntegrationBranch calls.
func (h *harness) countSets(t *testing.T) *integrationSets {
	t.Helper()

	s := &integrationSets{Store: h.store}
	h.e = h.engine(t, s)

	return s
}

// assembly makes item 1, "big request", an assembly at work of n parts, queued there.
func (h *harness) assembly(t *testing.T, n int) (domain.Item, []domain.Item) {
	t.Helper()

	it := h.at(t, h.submit(t, domain.NewItem{Title: assemblyTitle}), stWork, domain.StateReady)

	plan := make([]domain.NewPart, 0, n)
	for _, key := range []string{keyStore, "parts-api", "parts-web"}[:n] {
		plan = append(plan, domain.NewPart{Key: key, Title: key})
	}

	root, parts, err := h.store.Assemble(inOrg(t), ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stWork, State: domain.StateAssembly}, At: h.clock.Now(),
		},
		Parts: plan,
	})
	if err != nil {
		t.Fatal(err)
	}

	return root, parts.Items
}

// namePR records an output of part's at work that names prURL.
func (h *harness) namePR(t *testing.T, part domain.Item) domain.Item {
	t.Helper()

	got, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: part.ID, From: part.Position(), To: part.Position(), At: h.clock.Now(),
		OutputStation: stWork, Output: []byte(`{"pull_request":"` + prURL + `"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// The first part to run sets its assembly's integration branch, landing at the line's first merge
// station after the planning one; the second part finds it set and sets nothing.
func TestRun_PartSetsTheIntegrationBranchOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t, integrationLine(domain.IntegrationModeBranch, 2), porttest.Succeed(`{}`))
	sets := h.countSets(t)
	root, _ := h.assembly(t, 2)

	h.step(t)
	h.step(t)

	if got, want := sets.Calls(), []string{integrationBranch + "@" + stMerge}; !slices.Equal(got, want) {
		t.Errorf("SetIntegrationBranch calls %v, want %v", got, want)
	}

	if got := h.item(t, root.ID); got.IntegrationBranch != integrationBranch || got.IntegrationLandAt != stMerge {
		t.Errorf("assembly's integration branch %q landing at %q; want %q at %q", got.IntegrationBranch, got.IntegrationLandAt, integrationBranch, stMerge)
	}

	runs := h.worker.Runs()
	if len(runs) != 2 {
		t.Fatalf("%d runs, want 2", len(runs))
	}

	for i, run := range runs {
		if c := h.claims(t, run.Env["GOAL_GATEWAY_TOKEN"]); c.Base != integrationBranch || !strings.HasPrefix(c.Branch, "goal/") {
			t.Errorf("run %d token branch %q base %q; want the part's branch on %q", i, c.Branch, c.Base, integrationBranch)
		}
	}
}

// No integration branch in mode trunk, for an assembly with fewer parts than minParts, or for a
// part that already names a pull request: such parts keep the default branch.
func TestRun_NoIntegrationBranchInTrunkModeOrBelowMinParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     domain.IntegrationMode
		minParts int
		parts    int
		namesPR  bool
	}{
		{name: "trunk mode", mode: domain.IntegrationModeTrunk, minParts: 1, parts: 2},
		{name: "below minParts", mode: domain.IntegrationModeBranch, minParts: 3, parts: 2},
		{name: "the part names a pull request", mode: domain.IntegrationModeBranch, minParts: 1, parts: 1, namesPR: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, integrationLine(tt.mode, tt.minParts), porttest.Succeed(`{}`))
			sets := h.countSets(t)
			root, parts := h.assembly(t, tt.parts)

			if tt.namesPR {
				h.namePR(t, parts[0])
			}

			h.step(t)

			if calls := sets.Calls(); len(calls) != 0 {
				t.Errorf("SetIntegrationBranch calls %v, want none", calls)
			}

			if got := h.item(t, root.ID); got.IntegrationBranch != "" {
				t.Errorf("assembly's integration branch %q, want none", got.IntegrationBranch)
			}

			run := h.worker.Runs()[0]
			if c := h.claims(t, run.Env["GOAL_GATEWAY_TOKEN"]); c.Branch != "" || c.Base != "" {
				t.Errorf("token branch %q base %q; want neither", c.Branch, c.Base)
			}

			if note := run.Env["GOAL_FEEDBACK"]; !strings.Contains(note, "a new branch from the default branch") || strings.Contains(note, "open your pull request into") {
				t.Errorf("feedback %q; want the default branch's note", note)
			}
		})
	}
}

// A part on its assembly's integration branch hears it as its base, where its pull request goes
// and when the branch takes in the default branch; one whose pull request went to main before the
// switch stays on the default branch.
func TestRun_PartHearsItsIntegrationBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prBase   string // "" when the part names no pull request
		wantBase string
	}{
		{name: "no pull request yet", wantBase: integrationBranch},
		{name: "a pull request into the integration branch", prBase: integrationBranch, wantBase: integrationBranch},
		{name: "a pull request opened against main", prBase: mainBranch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, integrationLine(domain.IntegrationModeBranch, 1), porttest.Succeed(`{}`))
			sets := h.countSets(t)
			root, parts := h.assembly(t, 1)

			if _, err := h.store.SetIntegrationBranch(inOrg(t), root.ID, integrationBranch, stMerge, h.clock.Now()); err != nil {
				t.Fatal(err)
			}

			if tt.prBase != "" {
				pr := openPR(prURL)
				pr.BaseBranch = tt.prBase
				h.codehost.Add(pr)
				h.namePR(t, parts[0])
			}

			h.step(t)

			if calls := sets.Calls(); len(calls) != 0 {
				t.Errorf("SetIntegrationBranch calls %v, want none: the branch was set", calls)
			}

			run := h.worker.Runs()[0]

			c := h.claims(t, run.Env["GOAL_GATEWAY_TOKEN"])
			if c.Base != tt.wantBase {
				t.Errorf("token base %q, want %q", c.Base, tt.wantBase)
			}

			note := run.Env["GOAL_FEEDBACK"]

			want := []string{"`" + parts[0].Branch() + "`", "a new branch from the default branch"}
			if tt.wantBase != "" {
				want = []string{
					"`" + parts[0].Branch() + "`", "a new branch from `" + integrationBranch + "`",
					"open your pull request into `" + integrationBranch + "`", string(domain.RefreshEachPart),
				}
			}

			for _, s := range want {
				if !strings.Contains(note, s) {
					t.Errorf("feedback %q does not mention %q", note, s)
				}
			}
		})
	}
}

// expectLandingRun fails unless run worked on the integration branch with the default branch as
// its base and heard that it lands the whole assembly.
func (h *harness) expectLandingRun(t *testing.T, run porttest.ExecCall) {
	t.Helper()

	if c := h.claims(t, run.Env["GOAL_GATEWAY_TOKEN"]); c.Branch != integrationBranch || c.Base != "" {
		t.Errorf("token branch %q base %q; want %q on the default branch", c.Branch, c.Base, integrationBranch)
	}

	note := run.Env["GOAL_FEEDBACK"]
	for _, s := range []string{
		"`" + integrationBranch + "` checked out", "landing the whole assembly", "merge the default branch into `" + integrationBranch + "`",
		"gh pr create --base <default branch> --head " + integrationBranch, "pull_request",
	} {
		if !strings.Contains(note, s) {
			t.Errorf("feedback %q does not mention %q", note, s)
		}
	}

	if strings.Contains(note, "This part builds on") {
		t.Errorf("feedback %q tells the landing it is a part", note)
	}
}

// A finished assembly's landing run works on its integration branch, based on the default branch,
// and hears how to land it.
func TestRun_LandingRunsOnTheIntegrationBranch(t *testing.T) {
	t.Parallel()

	h := newHarness(t, landingLine(), byAgent(map[string]string{agReviewer: approveEmpty}))
	h.landing(t)
	h.step(t)

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d runs, want 1", len(runs))
	}

	h.expectLandingRun(t, runs[0])
}

// A landing the gate rejects goes back to build, which reworks it on the integration branch with
// the default branch as its base and hears to fix what the rejection asks for, not to land it.
func TestRun_RejectedLandingReworksOnTheIntegrationBranch(t *testing.T) {
	t.Parallel()

	h := newHarness(t, landingLine(), byAgent(map[string]string{
		agReviewer: `{"decision":"reject","reason":"conflicts with main"}`,
		agWorker:   `{"summary":"reconciled"}`,
	}))
	root := h.landing(t)
	h.step(t) // rejected

	if got := h.item(t, root.ID); got.Position() != (domain.Position{Station: stBuild, State: domain.StateQueued}) {
		t.Fatalf("assembly at %+v, want back at build (events %v)", got.Position(), h.kinds(root.ID))
	}

	h.step(t) // reworked

	runs := h.worker.Runs()
	if len(runs) != 2 || runs[1].Sandbox.Agent != agWorker {
		t.Fatalf("runs %d, want the gate's then the worker's", len(runs))
	}

	h.expectLandingReworkRun(t, runs[1])

	// The rejection reason reaches the rework's brief as the gate's output among the earlier
	// stations' outputs, which the run bundle serves.
	if out := string(h.item(t, root.ID).Outputs[stReview]); !strings.Contains(out, "conflicts with main") {
		t.Errorf("gate output %q does not keep the rejection reason", out)
	}
}

// expectLandingReworkRun fails unless run reworked the landing assembly away from its landing
// station: on the integration branch with the default branch as its base, told to fix what the
// rejection asks for and push, and not told to land the assembly.
func (h *harness) expectLandingReworkRun(t *testing.T, run porttest.ExecCall) {
	t.Helper()

	if c := h.claims(t, run.Env["GOAL_GATEWAY_TOKEN"]); c.Branch != integrationBranch || c.Base != "" {
		t.Errorf("token branch %q base %q; want %q on the default branch", c.Branch, c.Base, integrationBranch)
	}

	note := run.Env["GOAL_FEEDBACK"]
	for _, s := range []string{
		"`" + integrationBranch + "` checked out", "This is the assembly's integration branch `" + integrationBranch + "`",
		"it lands on the default branch", "Fix what the last rejection asks for here",
		"push to `" + integrationBranch + "` as a plain push", "keep its pull request into the default branch open",
	} {
		if !strings.Contains(note, s) {
			t.Errorf("feedback %q does not mention %q", note, s)
		}
	}

	for _, s := range []string{"landing the whole assembly", "gh pr create", "This part builds on"} {
		if strings.Contains(note, s) {
			t.Errorf("feedback %q mentions %q at a station that does not land", note, s)
		}
	}
}
