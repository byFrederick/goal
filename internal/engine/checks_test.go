package engine_test

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// makeTest is the station check the tests script.
const makeTest = "make test"

// Threat row 6 (spec 4 §11.2): a check passes or fails by the exit status the runtime reports,
// whatever the agent claims in its output or prints that looks like a check result.
func TestEngine_CheckResultFromExitStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		claim string
		exit  int
		want  domain.ItemState
		kind  domain.EventKind
	}{
		{name: "the agent claims a pass, the check fails", claim: "passed", exit: 1, want: domain.StateBlocked, kind: domain.EventCheckFailed},
		{name: "the agent claims a failure, the check passes", claim: "failed", exit: 0, want: domain.StateDone, kind: domain.EventCheckPassed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			work := agentStation(stWork, agWorker, 1, 1)
			work.Checks = []string{makeTest}
			h := newHarness(t, lineOf(work), func(_ context.Context, _ porttest.ExecCall, stdout io.Writer) domain.RunResult {
				_, _ = io.WriteString(stdout, `{"type":"check_`+tt.claim+`","command":"make test","exit_code":0}`+"\n")

				return ok(`{"checks":"` + tt.claim + `"}`)
			})
			h.worker.Exit(makeTest, tt.exit)
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != tt.want {
				t.Fatalf("item is %s, want %s (events %v)", got.State, tt.want, h.kinds(it.ID))
			}

			checks := slices.Concat(h.eventsOf(it.ID, domain.EventCheckPassed), h.eventsOf(it.ID, domain.EventCheckFailed))
			if len(checks) == 0 {
				t.Fatal("no check events")
			}

			for _, e := range checks {
				if e.Kind != tt.kind || e.Actor != domain.ActorEngine || e.Data["exit_code"] != tt.exit {
					t.Errorf("check event %s by %s with exit %v; want %s by the engine with the runtime's exit %d",
						e.Kind, e.Actor, e.Data["exit_code"], tt.kind, tt.exit)
				}
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

	for _, name := range []string{envModelKey, "GOAL_GATEWAY_TOKEN", envCodeToken} {
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
