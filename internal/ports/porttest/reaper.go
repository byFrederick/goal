package porttest

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Reaper = (*Host)(nil)

// Host is a fake host of what runs leave behind (containers, folders), shared by the goald
// instances of a test as one Docker host is shared by the goalds on it.
type Host struct {
	mu      sync.Mutex
	left    map[ports.Leftover]bool
	changes changes
}

// NewHost returns an empty host.
func NewHost() *Host { return &Host{left: map[ports.Leftover]bool{}} }

// Leave records that run has something here labelled with org ("" for an unlabelled folder).
func (h *Host) Leave(run domain.RunID, org domain.OrgID) {
	h.mu.Lock()
	h.left[ports.Leftover{Run: run, Org: org}] = true
	h.mu.Unlock()

	h.changes.signal()
}

// Clear removes what run left, as its instance does when the run ends.
func (h *Host) Clear(run domain.RunID) {
	h.mu.Lock()
	h.clear(run)
	h.mu.Unlock()

	h.changes.signal()
}

// clear removes run's leftovers. Callers hold h.mu.
func (h *Host) clear(run domain.RunID) {
	for l := range h.left {
		if l.Run == run {
			delete(h.left, l)
		}
	}
}

// Has reports whether run has something here.
func (h *Host) Has(run domain.RunID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	for l := range h.left {
		if l.Run == run {
			return true
		}
	}

	return false
}

// Await blocks until run has something here (want true) or nothing (want false), or ctx ends.
func (h *Host) Await(ctx context.Context, run domain.RunID, want bool) error {
	return h.changes.await(ctx, func() bool { return h.Has(run) == want })
}

// Leftovers implements ports.Reaper.
func (h *Host) Leftovers(context.Context) ([]ports.Leftover, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]ports.Leftover, 0, len(h.left))
	for l := range h.left {
		out = append(out, l)
	}

	slices.SortFunc(out, func(a, b ports.Leftover) int { return cmp.Or(cmp.Compare(a.Run, b.Run), cmp.Compare(a.Org, b.Org)) })

	return out, nil
}

// Reap implements ports.Reaper.
func (h *Host) Reap(_ context.Context, runs []domain.RunID) error {
	h.mu.Lock()
	for _, run := range runs {
		h.clear(run)
	}
	h.mu.Unlock()

	h.changes.signal()

	return nil
}

// LeavingOn returns a RunFunc that plays fn while the task's container stands on host, labelled
// with the task's organization, and removes it when fn returns, as the Docker runner does.
func LeavingOn(host *Host, fn RunFunc) RunFunc {
	return func(ctx context.Context, t ports.TaskSpec, events chan<- ports.RunEvent) domain.RunResult {
		host.Leave(t.RunID, t.OrgID)
		defer host.Clear(t.RunID)

		return fn(ctx, t, events)
	}
}
