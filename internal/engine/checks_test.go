package engine_test

import (
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const repoURL = "https://git.example.com/acme/app.git"

func TestRun_WorksInAFreshCloneOfTheItemsBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing []string
		note     string
	}{
		{name: "new branch", note: "a new branch from the default branch"},
		{name: "branch of an earlier run", existing: []string{"goal/1-add-a-version-command"}, note: "holds the work of earlier runs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := lineOf(agentStation(stWork, agWorker, 1, 1))
			line.Runtime.Repository = domain.Repository{URL: repoURL, AuthRef: "env:GIT_TOKEN"}
			h := newHarness(t, line, porttest.Succeed(`{}`))
			h.workspaces = porttest.NewWorkspaces(tt.existing...)
			h.e = h.engine(t, h.store)
			it := h.submit(t, domain.NewItem{Title: "Add a version command"})

			h.step(t)

			prepared := h.workspaces.Prepared()
			if len(prepared) != 1 {
				t.Fatalf("%d workspaces prepared, want 1", len(prepared))
			}

			spec := h.runner.Started()[0]

			want := ports.WorkspaceSpec{
				FactoryID: factoryID, ItemID: it.ID, RunID: spec.RunID, Repository: line.Runtime.Repository,
				Branch: "goal/1-add-a-version-command",
			}
			if prepared[0] != want {
				t.Errorf("workspace = %+v, want %+v", prepared[0], want)
			}

			if spec.WorkspaceDir != "/workspaces/"+string(spec.RunID)+"/repo" {
				t.Errorf("task workspace = %q, want the prepared one", spec.WorkspaceDir)
			}

			for _, s := range []string{repoURL, "`goal/1-add-a-version-command`", tt.note, "no git credentials"} {
				if !strings.Contains(spec.Brief, s) {
					t.Errorf("brief %q does not mention %q", spec.Brief, s)
				}
			}

			if strings.Contains(spec.Brief, "GIT_TOKEN") {
				t.Errorf("brief %q names the clone credential", spec.Brief)
			}

			if n := h.workspaces.Live(); n != 0 {
				t.Errorf("%d workspaces left after the run, want every one removed", n)
			}
		})
	}
}

func TestChecks_FailureRetriesWithItsOutputThenBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		exits  []int
		want   domain.ItemState
		failed int
		runs   int
	}{
		{name: "passes", exits: []int{0}, want: domain.StateDone, runs: 1},
		{name: "passes on retry", exits: []int{2, 0}, want: domain.StateDone, failed: 1, runs: 2},
		{name: "fails twice blocks", exits: []int{2, 2}, want: domain.StateBlocked, failed: 2, runs: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			work := agentStation(stWork, agWorker, 1, 1)
			work.Checks = []string{"make lint", "make test"}
			h := newHarness(t, lineOf(work), porttest.Succeed(`{}`))
			h.checker.Exit("make test", tt.exits...)
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != tt.want {
				t.Fatalf("item is %s, want %s (events %v)", got.State, tt.want, h.kinds(it.ID))
			}

			if got := len(h.eventsOf(it.ID, domain.EventCheckFailed)); got != tt.failed {
				t.Errorf("%d check_failed events, want %d", got, tt.failed)
			}

			started := h.runner.Started()
			if len(started) != tt.runs {
				t.Fatalf("%d tasks started, want %d", len(started), tt.runs)
			}

			if tt.runs == 2 && !strings.Contains(started[1].Brief, "`make test` exited 2") {
				t.Errorf("retry brief %q does not carry the failed check", started[1].Brief)
			}

			checked := h.checker.Checked()
			if checked[0].Command != "make lint" || checked[0].WorkspaceDir != started[0].WorkspaceDir ||
				checked[0].Image != "ghcr.io/acme/agent:1" || checked[0].RunID != started[0].RunID {
				t.Errorf("first check = %+v, want make lint in the first run's workspace and image", checked[0])
			}

			if n := h.workspaces.Live(); n != 0 {
				t.Errorf("%d workspaces left, want every one removed", n)
			}
		})
	}
}

func TestChecks_RunOnlyOnOutputThatSatisfiesTheContract(t *testing.T) {
	t.Parallel()

	work := agentStation(stWork, agWorker, 1, 1)
	work.Output, work.Checks = "result", []string{"make test"}
	h := newHarness(t, lineOf(work), porttest.Sequence(ok(`{}`), ok(`{}`)))
	h.contracts.Require(factoryID, "result", "answer")
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateBlocked {
		t.Fatalf("item is %s, want blocked by its contract", got.State)
	}

	if n := len(h.checker.Checked()); n != 0 {
		t.Errorf("%d checks ran on output that broke its contract, want 0", n)
	}
}
