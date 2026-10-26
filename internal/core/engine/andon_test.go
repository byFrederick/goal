package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// inProgress names the item whose run is in progress when the cord is pulled.
const inProgress = "in progress"

// andonLine is inbox → work (one slot) → approve (human) → done.
func andonLine() domain.Line {
	return lineOf(agentStation(stWork, agWorker, 1, 1), domain.Station{ID: stApprove, Kind: domain.KindHuman})
}

// pull pulls the andon cord on the test factory as the local user.
func (h *harness) pull(t *testing.T) domain.AndonPull {
	t.Helper()

	p, err := h.e.PullAndon(inOrg(t), factoryID, domain.LocalUser, "Bad answers reach customers")
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func (h *harness) clear(t *testing.T) {
	t.Helper()

	if _, err := h.e.ClearAndon(inOrg(t), factoryID, domain.LocalUser, "Fixed the prompt"); err != nil {
		t.Fatal(err)
	}
}

// queuedAt puts an item at station, waiting to be pulled; it was created before anything the
// test submits, so it is pulled first.
func (h *harness) queuedAt(t *testing.T, station, title string) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: factoryID, Type: typeFeature, Title: title, Station: station, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: t0.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

func (h *harness) expectAt(t *testing.T, it domain.Item, station string, state domain.ItemState) {
	t.Helper()

	if got := h.item(t, it.ID); got.Station != station || got.State != state {
		t.Errorf("%s = %s/%s; want %s/%s", it.Title, got.Station, got.State, station, state)
	}
}

// While the cord is pulled the line releases and claims nothing; people still submit and decide.
// Clearing it restarts the line.
func TestAndon_StopsReleasesAndClaimsUntilCleared(t *testing.T) {
	t.Parallel()

	h := newHarness(t, andonLine(), porttest.Succeed(`{}`))
	h.store.AddFactory(testOrg, factoryID)

	pulled := h.pull(t)
	if pulled.Kind != domain.AndonManual || pulled.PulledBy != domain.LocalUser.Ref() || pulled.PulledAt != t0 {
		t.Errorf("pull = %+v; want a manual pull by the local user now", pulled)
	}

	inbox := h.submit(t, domain.NewItem{Title: "submitted while stopped"})
	ready := h.queuedAt(t, stWork, "ready at work")
	decision := h.waitingHuman(t, stApprove)

	h.step(t)

	h.expectAt(t, inbox, "inbox", domain.StateQueued)
	h.expectAt(t, ready, stWork, domain.StateQueued)

	if n := len(h.worker.Runs()); n != 0 {
		t.Errorf("%d runs started while the cord is pulled; want none", n)
	}

	if _, err := h.e.Approve(inOrg(t), decision.ID, domain.LocalUser); err != nil {
		t.Fatalf("approve while the line is stopped: %v", err)
	}

	h.clear(t)
	h.step(t)

	h.expectAt(t, ready, stApprove, domain.StateWaitingHuman)
	h.expectAt(t, inbox, stWork, domain.StateQueued) // released; the one slot was taken first
	h.expectAt(t, decision, stDone, domain.StateDone)
}

// A run in progress when the cord is pulled finishes, and its item moves on.
func TestAndon_RunsInProgressFinish(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h := newHarness(t, andonLine(), porttest.Block(release, `{}`))
	running := h.submit(t, domain.NewItem{Title: inProgress})

	h.tick(t) // inbox releases it and work claims it

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	h.pull(t)
	h.tick(t)
	close(release)
	h.wait(t, h.e)

	h.expectAt(t, running, stApprove, domain.StateWaitingHuman)

	if cancelled := h.eventsOf(running.ID, domain.EventAgentCancelled); len(cancelled) != 0 {
		t.Errorf("agent_cancelled events = %+v; want the run to finish", cancelled)
	}
}

// The three stops combine; none lifts another. A pause and an open pull each stop releases and
// claims, so ending one leaves the line stopped by the other; a used-up budget with the stop
// policy cancels runs whether or not the line is paused or stopped.
func TestAndon_CombinesWithPauseAndBudget(t *testing.T) {
	t.Parallel()

	t.Run("resuming a paused line with an open pull keeps it stopped", func(t *testing.T) {
		t.Parallel()

		line := andonLine()
		h := newHarness(t, line, porttest.Succeed(`{}`))
		ready := h.queuedAt(t, stWork, "ready")
		h.pull(t)

		line.Paused = true
		h.setLines(t, line)
		h.step(t)

		line.Paused = false
		h.setLines(t, line)
		h.step(t)
		h.expectAt(t, ready, stWork, domain.StateQueued)

		h.clear(t)
		h.step(t)
		h.expectAt(t, ready, stApprove, domain.StateWaitingHuman)
	})

	t.Run("clearing the pull of a paused line keeps it paused", func(t *testing.T) {
		t.Parallel()

		line := andonLine()
		line.Paused = true
		h := newHarness(t, line, porttest.Succeed(`{}`))
		ready := h.queuedAt(t, stWork, "ready")
		h.pull(t)
		h.clear(t)
		h.step(t)
		h.expectAt(t, ready, stWork, domain.StateQueued)
	})

	t.Run("the budget's stop still cancels the runs of a stopped line", func(t *testing.T) {
		t.Parallel()

		release := make(chan struct{})

		t.Cleanup(func() { close(release) })

		h := newHarness(t, budgeted(10, domain.BudgetStop, 1), porttest.Block(release, `{}`))
		running := h.queuedAt(t, stWork, inProgress)
		h.tick(t)

		if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
			t.Fatal(err)
		}

		h.pull(t)
		h.spent(t, t0, 12)
		h.tick(t)
		h.wait(t, h.e)

		h.expectAt(t, running, stWork, domain.StateQueued)

		if cancelled := h.eventsOf(running.ID, domain.EventAgentCancelled); len(cancelled) != 1 {
			t.Errorf("agent_cancelled events = %+v; want the budget to cancel the run", cancelled)
		}
	})

	t.Run("clearing the pull of a line over its budget releases but claims nothing", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, budgeted(10, domain.BudgetDrain, 1), porttest.Succeed(`{}`))
		inbox := h.submit(t, domain.NewItem{Title: "in the inbox"})
		h.spent(t, t0, 10)
		h.pull(t)
		h.clear(t)
		h.step(t)

		h.expectAt(t, inbox, stWork, domain.StateQueued)

		if n := len(h.worker.Runs()); n != 0 {
			t.Errorf("%d runs started over budget; want none", n)
		}
	})
}

// openAndonFails is a store whose andon cannot be read.
type openAndonFails struct {
	ports.Store
}

var errAndonRead = errors.New("andon unreadable")

func (openAndonFails) OpenAndon(context.Context, domain.FactoryID) (*domain.AndonPull, error) {
	return nil, errAndonRead
}

// A line whose andon cannot be read is treated as stopped for the tick: work never starts
// against a pull nobody could see.
func TestAndon_UnreadableStopsTheLineForTheTick(t *testing.T) {
	t.Parallel()

	h := newHarness(t, andonLine(), porttest.Succeed(`{}`))
	inbox := h.submit(t, domain.NewItem{Title: "in the inbox"})
	ready := h.queuedAt(t, stWork, "ready")
	h.e = h.engine(t, openAndonFails{Store: h.store})

	if err := h.e.Tick(inOrg(t)); !errors.Is(err, errAndonRead) {
		t.Errorf("Tick = %v; want the andon read's error", err)
	}

	h.wait(t, h.e)
	h.expectAt(t, inbox, "inbox", domain.StateQueued)
	h.expectAt(t, ready, stWork, domain.StateQueued)
}

func TestPullAndon_ReasonRequiredOnePullAtATimeInTheOrg(t *testing.T) {
	t.Parallel()

	h := newHarness(t, andonLine(), porttest.Succeed(`{}`))
	h.store.AddFactory(testOrg, factoryID)

	for _, reason := range []string{"", "   ", strings.Repeat("x", domain.MaxAndonReason+1)} {
		if _, err := h.e.PullAndon(inOrg(t), factoryID, domain.LocalUser, reason); !errors.Is(err, engine.ErrBadAndon) {
			t.Errorf("pull with reason of %d characters = %v; want ErrBadAndon", len(reason), err)
		}
	}

	if _, err := h.e.ClearAndon(inOrg(t), factoryID, domain.LocalUser, ""); !errors.Is(err, engine.ErrAndonNotPulled) {
		t.Errorf("clear of a running line = %v; want ErrAndonNotPulled", err)
	}

	p, err := h.e.PullAndon(inOrg(t), factoryID, domain.LocalUser, "  Stop  ")
	if err != nil || p.Reason != "Stop" {
		t.Fatalf("pull = %+v, %v; want it with the trimmed reason", p, err)
	}

	if _, err := h.e.PullAndon(inOrg(t), factoryID, domain.LocalUser, "again"); !errors.Is(err, engine.ErrAndonPulled) {
		t.Errorf("a second pull = %v; want ErrAndonPulled", err)
	}

	long := strings.Repeat("y", domain.MaxAndonNote+1)
	if _, err := h.e.ClearAndon(inOrg(t), factoryID, domain.LocalUser, long); !errors.Is(err, engine.ErrBadAndon) {
		t.Errorf("clear with a note too long = %v; want ErrBadAndon", err)
	}

	other := domain.WithOrg(t.Context(), h.newOrg(t, "labs"))
	if _, err := h.e.PullAndon(other, factoryID, domain.LocalUser, "theirs"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("pull from another organization = %v; want ErrNotFound", err)
	}
}

func (h *harness) setLines(t *testing.T, lines ...domain.Line) {
	t.Helper()

	if err := h.e.SetLines(lines); err != nil {
		t.Fatal(err)
	}
}
