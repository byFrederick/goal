// Command goal is the Goal CLI: it validates factory definitions and moves them between a
// local working copy and the bucket where they live. `goal up` (daemon + UI) lands later.
//
// Object storage comes from the environment: GOAL_S3_BUCKET, and for a local S3-compatible
// server (RustFS) GOAL_S3_ENDPOINT, GOAL_S3_ACCESS_KEY and GOAL_S3_SECRET_KEY. On AWS the
// standard credential chain applies.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/byFrederick/goal/internal/adapters/s3store"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/factorystore"
)

const usage = `usage:
  goal factory validate <dir> ...                 validate local factory folders (alias: goal validate)
  goal factory push <dir>                         validate, then upload to factories/<metadata.id>/
  goal factory pull <id|name> <dir>               download a factory into a new folder
  goal factory list                               show and validate every factory in the bucket
  goal storage init                               create the bucket if missing and enable versioning
  goal version                                    print the module version and build info`

// errUsage marks a command line that names no valid command; main exits 2 for it.
var errUsage = errors.New(usage)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	out := &printer{w: os.Stdout}
	err := errors.Join(run(ctx, os.Args[1:], out, os.Getenv, debug.ReadBuildInfo), out.err)

	stop()

	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(os.Stderr, "goal:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out *printer, getenv func(string) string, readBuildInfo buildInfoFunc) error {
	if len(args) == 0 {
		return errUsage
	}

	switch cmd, rest := args[0], args[1:]; cmd {
	case "version":
		return version(out, readBuildInfo, rest)
	case "validate":
		return validate(out, rest)
	case "factory":
		return factory(ctx, out, getenv, rest)
	case "storage":
		if len(rest) != 1 || rest[0] != "init" {
			return errUsage
		}

		return storageInit(ctx, out, getenv)
	default:
		return errUsage
	}
}

func factory(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	if args[0] == "validate" {
		return validate(out, args[1:])
	}

	store, err := newStore(ctx, getenv)
	if err != nil {
		return err
	}

	catalog := factorystore.New(store)

	switch cmd, rest := args[0], args[1:]; cmd {
	case "push":
		if len(rest) != 1 {
			return errUsage
		}

		return push(ctx, out, catalog, store.Bucket(), rest[0])
	case "pull":
		return pull(ctx, out, catalog, rest)
	case "list":
		if len(rest) != 0 {
			return errUsage
		}

		return list(ctx, out, catalog, store.Bucket())
	default:
		return errUsage
	}
}

func newStore(ctx context.Context, getenv func(string) string) (*s3store.Store, error) {
	cfg, err := s3store.ConfigFromEnv(getenv)
	if err != nil {
		return nil, err
	}

	return s3store.New(ctx, cfg)
}

func validate(out *printer, dirs []string) error {
	if len(dirs) == 0 {
		return errUsage
	}

	failed := 0
	seen := map[string]string{} // factory id → folder

	for _, dir := range dirs {
		f, err := config.LoadDir(dir)
		if err != nil {
			failed++

			out.printf("✗ %s\n%v\n\n", dir, err)

			continue
		}

		if other, dup := seen[f.Metadata.ID]; dup {
			failed++

			out.printf("✗ %s\nid %s is already used by %s; copy a factory, then give it a new id (uuidgen)\n\n",
				dir, f.Metadata.ID, other)

			continue
		}

		seen[f.Metadata.ID] = dir
		out.printf("✓ %s (%s) — %d agents, %d stations, stored at %s\n",
			f.Metadata.Name, f.Metadata.ID[:8], len(f.Spec.Agents), len(f.Spec.Board.Stations),
			factorystore.Prefix(f.Metadata.ID))
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d factories are invalid", failed, len(dirs))
	}

	return nil
}

func push(ctx context.Context, out *printer, catalog *factorystore.Catalog, bucket, dir string) error {
	f, changes, err := catalog.Push(ctx, dir)
	for _, c := range changes {
		out.printf("  %-9s %-50s %s\n", c.Action, c.Path, c.Version)
	}

	if err != nil {
		return err
	}

	out.printf("✓ pushed %s to s3://%s/%s\n", f.Metadata.Name, bucket, factorystore.Prefix(f.Metadata.ID))

	return nil
}

func pull(ctx context.Context, out *printer, catalog *factorystore.Catalog, args []string) error {
	if len(args) != 2 {
		return errUsage
	}

	id, dir := args[0], args[1]

	s, err := catalog.Pull(ctx, id, dir)
	if err != nil {
		return err
	}

	for _, f := range s.Files {
		out.printf("  %-60s %s\n", f.Key, f.Version)
	}

	out.printf("✓ pulled %s (%d files) into %s\n", s.ID, len(s.Files), dir)

	return nil
}

func list(ctx context.Context, out *printer, catalog *factorystore.Catalog, bucket string) error {
	entries, err := catalog.List(ctx)
	if err != nil {
		return err
	}

	invalid := 0

	for _, e := range entries {
		if e.Err != nil {
			invalid++

			out.printf("✗ %s\n%v\n\n", e.ID, e.Err)

			continue
		}

		out.printf("✓ %-12s %s  %d agents, %d stations, %d files\n", e.Factory.Metadata.Name, e.ID,
			len(e.Factory.Spec.Agents), len(e.Factory.Spec.Board.Stations), len(e.Snapshot.Files))
	}

	out.printf("%d factories in s3://%s/%s\n", len(entries), bucket, factorystore.Root)

	if invalid > 0 {
		return fmt.Errorf("%d factories in the bucket are invalid", invalid)
	}

	return nil
}

// printer writes command output to stdout and keeps the first write error, so a closed pipe
// fails the command once instead of being ignored.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

func storageInit(ctx context.Context, out *printer, getenv func(string) string) error {
	store, err := newStore(ctx, getenv)
	if err != nil {
		return err
	}

	created, err := store.EnsureBucket(ctx)
	if err != nil {
		return err
	}

	state := "exists"
	if created {
		state = "created"
	}

	out.printf("✓ bucket %s %s, versioning enabled\n", store.Bucket(), state)

	return nil
}
