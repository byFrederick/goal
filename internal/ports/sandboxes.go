package ports

import (
	"context"
	"io"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// Sandboxes runs one container per agent run: Docker locally, Cloudflare Containers in the
// cloud. A sandbox has public internet and no route to private, loopback or metadata addresses.
// porttest/sandboxtest is the conformance suite every implementation passes.
type Sandboxes interface {
	// Create starts the sandbox of s.RunID; a second call for the same run returns the same one.
	Create(ctx context.Context, s SandboxSpec) (Sandbox, error)
	// List returns every sandbox this backend holds, live ones included, with run and org.
	// Several goalds may share a backend, so the engine asks the store which runs are over
	// (Store.RunsOver) and deletes only those.
	List(ctx context.Context) ([]Leftover, error)
	// Delete removes a run's sandbox, ending its running commands; one that is already gone is
	// not an error.
	Delete(ctx context.Context, run domain.RunID) error
}

// Leftover is a sandbox as Sandboxes.List reports it: the run it was created for and the
// organization it is labelled with, whose store knows the run.
type Leftover struct {
	Run domain.RunID
	Org domain.OrgID
}

// SandboxSpec is the sandbox of one run.
type SandboxSpec struct {
	OrgID     domain.OrgID
	FactoryID domain.FactoryID
	ItemID    domain.ItemID
	RunID     domain.RunID
	Station   string
	Agent     string
	Image     string
	Size      domain.SandboxSize
	// Env holds settings, never credentials: those go to the Exec that needs them.
	Env map[string]string
	// Deadline is a backstop: a backend that can (Cloudflare) destroys the sandbox then even if
	// no goald is left to delete it.
	Deadline time.Time
}

// Sandbox is one run's container.
type Sandbox interface {
	// Exec runs one command; cancelling ctx kills it. Env is added to the sandbox's.
	Exec(ctx context.Context, e Exec) (Process, error)
	// Delete removes the sandbox, as Sandboxes.Delete does for its run.
	Delete(ctx context.Context) error
}

// Exec is one command in a sandbox.
type Exec struct {
	Argv []string
	Env  map[string]string // adapters never log it
	// Dir is the working directory; empty is the image's.
	Dir string
	// Timeout kills the command after this long (ExitStatus.TimedOut); 0 is none.
	Timeout time.Duration
}

// Process is a running command.
type Process interface {
	// Stdout is the command's standard output as it is written. The caller drains it to its end,
	// then calls Wait.
	Stdout() io.Reader
	// Wait returns how the command ended; an error means the outcome is unknown. A command
	// killed because its ctx ended returns that ctx's error.
	Wait() (ExitStatus, error)
}

// ExitStatus is how a command ended, as the runtime saw it: never from code the agent can reach,
// so an agent cannot report a check as passed.
type ExitStatus struct {
	// Code is the exit code; it means nothing when TimedOut.
	Code       int
	TimedOut   bool
	StderrTail string // last MaxStderrTail bytes
}

// MaxStderrTail bounds ExitStatus.StderrTail: the last 8 KiB of the command's stderr.
const MaxStderrTail = 8 << 10
