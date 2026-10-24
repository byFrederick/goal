package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// goaldStopGrace is how long goald gets to release its runs after Ctrl-C before it is killed.
const goaldStopGrace = 30 * time.Second

// up starts the local platform: PostgreSQL and RustFS from compose.yaml (waiting until they are
// healthy), the database roles (goal db init, so an existing database is handed to goal_owner
// before goald migrates it), the bucket, then goald in the foreground until Ctrl-C. It pushes no
// factory: the bucket's definitions are what goald runs. The compose services keep running afterwards;
// `docker compose down` stops them.
func up(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) != 0 {
		return errUsage
	}

	out.printf("starting PostgreSQL and RustFS (docker compose)\n")

	compose := exec.CommandContext(ctx, "docker", "compose", "up", "--detach", "--wait", "postgres", "rustfs")
	compose.Stdout, compose.Stderr = os.Stderr, os.Stderr

	if err := compose.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w (run goal up from the repository root)", err)
	}

	if err := dbInit(ctx, out, getenv); err != nil {
		return err
	}

	if err := storageInit(ctx, out, getenv); err != nil {
		return err
	}

	bin, err := goaldPath()
	if err != nil {
		return err
	}

	out.printf("starting %s (Ctrl-C stops it; queued work stays in PostgreSQL)\n", bin)

	goald := exec.CommandContext(ctx, bin) //nolint:gosec // goald next to this binary or on PATH
	goald.Stdout, goald.Stderr = os.Stdout, os.Stderr
	goald.Env = os.Environ()

	if getenv("GOAL_LOG_FORMAT") == "" {
		goald.Env = append(goald.Env, "GOAL_LOG_FORMAT=text")
	}

	goald.Cancel = func() error { return goald.Process.Signal(syscall.SIGTERM) }
	goald.WaitDelay = goaldStopGrace

	if err := goald.Run(); err != nil && ctx.Err() == nil { // after Ctrl-C goald has shut down
		return fmt.Errorf("goald: %w", err)
	}

	return nil
}

// goaldPath finds goald next to this executable (task build puts both in bin/), else on PATH.
func goaldPath() (string, error) {
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), "goald")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}

	p, err := exec.LookPath("goald")
	if errors.Is(err, exec.ErrNotFound) {
		return "", errors.New("goald not found next to goal or on PATH; run `task build`")
	}

	return p, err
}
