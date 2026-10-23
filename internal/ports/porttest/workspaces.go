package porttest

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/ports"
)

var (
	_ ports.Workspaces = (*Workspaces)(nil)
	_ ports.Checker    = (*Checker)(nil)
)

// Workspaces hands out workspace paths without touching the filesystem and records which are
// live, so tests can see every prepared workspace removed again.
type Workspaces struct {
	mu sync.Mutex
	// Existing are the branches the fake repository already has.
	existing map[string]bool
	prepared []ports.WorkspaceSpec
	live     map[string]bool
}

// NewWorkspaces returns workspaces whose repository already has the given branches.
func NewWorkspaces(existing ...string) *Workspaces {
	w := &Workspaces{existing: map[string]bool{}, live: map[string]bool{}}
	for _, b := range existing {
		w.existing[b] = true
	}

	return w
}

// Prepare implements ports.Workspaces.
func (w *Workspaces) Prepare(_ context.Context, s ports.WorkspaceSpec) (ports.Workspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.prepared = append(w.prepared, s)
	ws := ports.Workspace{RunID: s.RunID, Dir: fmt.Sprintf("/workspaces/%s/repo", s.RunID)}

	if s.Repository.URL != "" {
		ws.Branch, ws.Existed = s.Branch, w.existing[s.Branch]
	}

	w.live[ws.Dir] = true

	return ws, nil
}

// Remove implements ports.Workspaces.
func (w *Workspaces) Remove(_ context.Context, ws ports.Workspace) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.live, ws.Dir)

	return nil
}

// Prepared returns the spec of every workspace prepared so far, in order.
func (w *Workspaces) Prepared() []ports.WorkspaceSpec {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.prepared)
}

// Live returns how many prepared workspaces have not been removed.
func (w *Workspaces) Live() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return len(w.live)
}

// Checker plays command checks from a script: each command exits with the codes queued for it,
// in order, repeating the last; an unscripted command passes.
type Checker struct {
	mu      sync.Mutex
	exits   map[string][]int
	checked []ports.CheckSpec
}

// NewChecker returns a checker on which every command passes.
func NewChecker() *Checker { return &Checker{exits: map[string][]int{}} }

// Exit scripts the exit codes of successive runs of command.
func (c *Checker) Exit(command string, codes ...int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.exits[command] = codes
}

// Check implements ports.Checker.
func (c *Checker) Check(_ context.Context, s ports.CheckSpec) (ports.CheckResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.checked = append(c.checked, s)

	codes := c.exits[s.Command]
	if len(codes) == 0 {
		return ports.CheckResult{}, nil
	}

	code := codes[0]
	if len(codes) > 1 {
		c.exits[s.Command] = codes[1:]
	}

	return ports.CheckResult{ExitCode: code, Output: fmt.Sprintf("%s exited %d", s.Command, code)}, nil
}

// Checked returns every check run so far, in order.
func (c *Checker) Checked() []ports.CheckSpec {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.checked)
}
