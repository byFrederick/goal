package engine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// resumeHarness is a hold harness on build → review (a gate with waitFor: ci and the check make
// test) → done, with an item queued at review whose agent approves and pushes pushSHA1 first and
// pushSHA2 on any run after.
func resumeHarness(t *testing.T) (*harness, domain.Item) {
	t.Helper()

	line := holdLine("", domain.DefaultCIRounds)
	for i := range line.Stations {
		if line.Stations[i].ID == stReview {
			line.Stations[i].Checks = []string{makeTest}
		}
	}

	h, p := holdHarness(t, line)
	it := h.at(t, h.submit(t, domain.NewItem{Title: "a"}), stReview, domain.StateQueued)
	p.set(it.ID, stReview, pushedTo(approve, pushSHA1), pushedTo(approve, pushSHA2))

	return h, it
}

// cutDuringChecks claims the item at review, lets its run succeed and push, and shuts the
// engine down while the station's check runs, as a deploy does: the item is released with reason
// shutdown. It returns the run.
func (h *harness) cutDuringChecks(t *testing.T, it domain.Item) domain.RunID {
	t.Helper()

	h.worker.HoldCheck(makeTest)

	ctx, cancel := context.WithCancel(inOrg(t))
	defer cancel()

	if err := h.e.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	if err := h.worker.WaitChecks(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	cancel()
	h.wait(t, h.e)

	h.expectPosition(t, it.ID, queuedAtMerge)

	if evs := h.eventsOf(it.ID, domain.EventReleased); len(evs) != 1 || evs[0].Data["reason"] != "shutdown" {
		t.Fatalf("released events %+v; want one with reason shutdown", evs)
	}

	runs := h.worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d runs before the cut; want 1", len(runs))
	}

	return runs[0].Sandbox.RunID
}

// headAt makes sha the head of the item's branch in appRepo on the code host.
func (h *harness) headAt(t *testing.T, id domain.ItemID, sha string) {
	t.Helper()

	h.codehost.SetBranchHead(appRepo, h.item(t, id).Branch(), sha)
}

// A deploy that shuts goald down while a succeeded run's checks run does not throw the run away:
// the next claim starts no agent, runs the checks again in a new box on the same head and holds
// the run for CI with an after wait, and once CI passes the run's output moves the item on.
func TestResume_CutDuringChecksResumesAndHoldsForCI(t *testing.T) {
	t.Parallel()

	h, it := resumeHarness(t)
	run := h.cutDuringChecks(t, it)
	h.headAt(t, it.ID, pushSHA1)

	h.step(t)

	w := h.expectHeld(t, it.ID, pushSHA1, 1, 1)

	var held struct {
		Run domain.RunID `json:"run"`
	}
	if err := json.Unmarshal(w.Held, &held); err != nil || held.Run != run {
		t.Errorf("the after wait holds %s; want the first run %s", w.Held, run)
	}

	checks := h.worker.Checks()
	if len(checks) != 2 || checks[1].Sandbox.RunID != run {
		t.Fatalf("checks = %+v; want the cut one and one more in a box of run %s", checks, run)
	}

	if runs, err := h.store.Runs(inOrg(t), it.ID, 10); err != nil || len(runs) != 1 {
		t.Fatalf("the store holds %d runs (%v); want the first only", len(runs), err)
	}

	if evs := h.eventsOf(it.ID, domain.EventCheckPassed); len(evs) != 1 || evs[0].RunID != run {
		t.Errorf("check_passed events %+v; want one on the first run", evs)
	}

	h.noSandboxes(t)

	h.endCI(t, pushSHA1, checksOf(ports.CheckPassed, ports.CheckFinished))
	h.step(t)

	h.expectPosition(t, it.ID, isDone)

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d agent runs; want the first only", n)
	}

	if got := h.item(t, it.ID).Outputs[stReview]; !strings.Contains(string(got), "approve") {
		t.Errorf("review output %s; want the first run's", got)
	}
}

// When the branch's head moved since the run pushed, the run is not reused: the station waits for
// CI and runs its agent again, as it does today.
func TestResume_HeadMovedRunsAgain(t *testing.T) {
	t.Parallel()

	h, it := resumeHarness(t)
	h.cutDuringChecks(t, it)
	h.headAt(t, it.ID, pushSHA3)

	h.step(t) // a before wait on what the run pushed, as today

	if n := len(h.worker.Checks()); n != 1 {
		t.Errorf("%d checks; want none run again", n)
	}

	h.endCI(t, pushSHA1, checksOf(ports.CheckPassed, ports.CheckFinished))
	h.step(t)

	h.expectHeld(t, it.ID, pushSHA2, 1, 2)

	if runs, err := h.store.Runs(inOrg(t), it.ID, 10); err != nil || len(runs) != 2 {
		t.Errorf("the store holds %d runs (%v); want a second run", len(runs), err)
	}
}

// When the checks fail on the resumed run, the station runs its agent again with the failed
// check's output in its brief, as after any failed check, and the failure is the first run's.
func TestResume_ChecksFailRunsAgain(t *testing.T) {
	t.Parallel()

	h, it := resumeHarness(t)
	run := h.cutDuringChecks(t, it)
	h.headAt(t, it.ID, pushSHA1)
	h.worker.Exit(makeTest, 2, 0)

	h.step(t)

	h.expectHeld(t, it.ID, pushSHA2, 1, 2)

	if evs := h.eventsOf(it.ID, domain.EventCheckFailed); len(evs) != 1 || evs[0].RunID != run {
		t.Errorf("check_failed events %+v; want one on the first run %s", evs, run)
	}

	if fb := h.feedback(1); !strings.Contains(fb, "`make test` exited 2") {
		t.Errorf("the second run's brief does not carry the failed check:\n%s", fb)
	}

	h.noSandboxes(t)
}
