package engine_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// makeTest is the station check the tests script.
const makeTest = "make test"

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
			work.Checks = []string{"make lint", makeTest}
			h := newHarness(t, lineOf(work), porttest.Succeed(`{}`))
			h.worker.Exit(makeTest, tt.exits...)
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != tt.want {
				t.Fatalf("item is %s, want %s (events %v)", got.State, tt.want, h.kinds(it.ID))
			}

			if got := len(h.eventsOf(it.ID, domain.EventCheckFailed)); got != tt.failed {
				t.Errorf("%d check_failed events, want %d", got, tt.failed)
			}

			runs := h.worker.Runs()
			if len(runs) != tt.runs {
				t.Fatalf("%d agent runs, want %d", len(runs), tt.runs)
			}

			if tt.runs == 2 && !strings.Contains(runs[1].Env["GOAL_FEEDBACK"], "`make test` exited 2") {
				t.Errorf("retry feedback %q does not carry the failed check", runs[1].Env["GOAL_FEEDBACK"])
			}

			h.noSandboxes(t)
		})
	}
}

// A check is a shell command in the run's sandbox, in its workspace, with no credential: not
// the model's access, not the task token, not the repository token.
func TestChecks_RunInTheSandboxWithoutCredentials(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Stations[1].Checks = []string{makeTest}
	h := newHarness(t, line, porttest.Succeed(`{}`))
	h.submit(t, domain.NewItem{})

	h.step(t)

	checks := h.worker.Checks()
	if len(checks) != 1 {
		t.Fatalf("%d checks, want 1", len(checks))
	}

	c := checks[0]
	if !slices.Equal(c.Argv, []string{"sh", "-c", makeTest}) || c.Dir != "/workspace" || c.Env["CI"] != "true" ||
		c.Sandbox.RunID != h.worker.Runs()[0].Sandbox.RunID {
		t.Errorf("check = %q in %q with env %v; want sh -c in /workspace of the run's sandbox, CI=true", c.Argv, c.Dir, keys(c.Env))
	}

	for _, name := range []string{"MODEL_ACCESS", "GOAL_GATEWAY_TOKEN", "GOAL_REPOSITORY_TOKEN"} {
		if _, ok := c.Env[name]; ok {
			t.Errorf("the check got %s", name)
		}
	}

	if c.Timeout <= 0 {
		t.Error("the check has no timeout")
	}
}

func TestChecks_RunOnlyOnOutputThatSatisfiesTheContract(t *testing.T) {
	t.Parallel()

	work := agentStation(stWork, agWorker, 1, 1)
	work.Output, work.Checks = contractResult, []string{makeTest}
	h := newHarness(t, lineOf(work), porttest.Sequence(ok(`{}`), ok(`{}`)))
	h.contracts.Require(v1, contractResult, "answer")
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateBlocked {
		t.Fatalf("item is %s, want blocked by its contract", got.State)
	}

	if n := len(h.worker.Checks()); n != 0 {
		t.Errorf("%d checks ran on output that broke its contract, want 0", n)
	}
}
