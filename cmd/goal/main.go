// Command goal is the Goal CLI: it validates factory definitions and moves them between a
// local working copy and the bucket where they live, starts the local platform (goal up), and
// submits, follows and decides on work items through goald's HTTP API.
//
// Object storage comes from the environment: GOAL_S3_BUCKET, and for a local S3-compatible
// server (RustFS) GOAL_S3_ENDPOINT, GOAL_S3_ACCESS_KEY and GOAL_S3_SECRET_KEY. On AWS the
// standard credential chain applies. goald's API is GOAL_API_URL (default
// http://127.0.0.1:8080).
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

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/adapters/s3store"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
)

const usage = `usage:
  goal up                                         start PostgreSQL and RustFS, prepare the database roles
                                                  and the bucket, run goald
  goal run <factory> "<brief>" [--type T] [--body B] [--priority N] [--detach]
                                                  submit a work item and follow it until a human or done
  goal follow <item>                              follow an item's events again
  goal item <item>                                show an item with its outputs
  goal approve <item> [--by NAME]                 approve an item waiting at a human station
  goal reject <item> --reason "..." [--by NAME]   send it back for rework
  goal retry <item> [--by NAME]                   send a blocked item back to its station's queue
  goal merge <item> [--by NAME]                   merge the item's pull request, then approve it
                                                  (at a station with action: merge)
  goal factory validate <dir> ...                 validate local factory folders (alias: goal validate)
  goal factory push <dir> [--org ID]              validate, then upload to
                                                  orgs/<org>/factories/<metadata.id>/
  goal factory pull <id|name> <dir> [--org ID]    download a factory into a new folder
  goal factory list [--org ID]                    show and validate every factory of the organization
  goal storage init                               create the bucket if missing and enable versioning
  goal db init                                    create the database roles goald runs as (goal_owner,
                                                  goal_app) and hand the schema to goal_owner
  goal version                                    print the module version and build info

<item> is goal#2 (factory and number), the item's id, or a bare number such as 2 when one
factory has that item (--factory F picks the factory of a bare number)

--org is an organization's id; without it the factory commands use the default organization,
which holds every factory from before organizations`

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
	case "up":
		return up(ctx, out, getenv, rest)
	case "run":
		return runItem(ctx, out, getenv, rest)
	case "follow":
		return followItem(ctx, out, getenv, rest)
	case "item":
		return showItem(ctx, out, getenv, rest)
	case verbApprove, verbReject, verbRetry, verbMerge:
		return decide(ctx, out, getenv, cmd, rest)
	case "db", "storage":
		return setUp(ctx, out, getenv, cmd, rest)
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

	cmd := args[0]

	org, rest, err := factoryArgs(cmd, args[1:])
	if err != nil {
		return err
	}

	store, err := newStore(ctx, getenv)
	if err != nil {
		return err
	}

	if err := migrateLegacy(ctx, out, store); err != nil {
		return err
	}

	catalog := factorystore.New(store)

	switch cmd {
	case "push":
		return push(ctx, out, catalog, store.Bucket(), org, rest[0])
	case "pull":
		return pull(ctx, out, catalog, org, rest)
	default:
		return list(ctx, out, catalog, store.Bucket(), org)
	}
}

// factoryArgs parses the flags and arguments of goal factory push|pull|list.
func factoryArgs(cmd string, args []string) (domain.OrgID, []string, error) {
	want, known := map[string]int{"push": 1, "pull": 2, "list": 0}[cmd]
	if !known {
		return "", nil, errUsage
	}

	fs := newFlags("factory " + cmd)
	orgFlag := fs.String("org", string(postgres.DefaultOrgID), "the id of the organization that owns the factories")

	rest, err := parseInterspersed(fs, args)
	if err != nil || len(rest) != want {
		return "", nil, errUsage
	}

	org, err := orgID(*orgFlag)

	return org, rest, err
}

// migrateLegacy moves the bucket layout before organizations into the default organization, as
// goald does at startup. The CLI runs it before it writes or reads the new layout, so a push
// never lands before the migration and is then overwritten by it.
func migrateLegacy(ctx context.Context, out *printer, store ports.ObjectStore) error {
	m, err := factorystore.MigrateLegacy(ctx, store, postgres.DefaultOrgID)
	if err != nil {
		return fmt.Errorf("move legacy factory definitions into the default organization: %w", err)
	}

	if m.Copied+m.Deleted > 0 {
		out.printf("moved %d legacy objects from %s into %s (%d copied)\n", m.Deleted, factorystore.LegacyRoot,
			factorystore.OrgPrefix(postgres.DefaultOrgID), m.Copied)
	}

	return nil
}

// orgID checks an organization id given on the command line: it becomes part of bucket keys.
func orgID(s string) (domain.OrgID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return "", fmt.Errorf("--org %q is not an organization id", s)
	}

	return domain.OrgID(id.String()), nil
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
		out.printf("✓ %s (%s) — %d agents, %d stations\n",
			f.Metadata.Name, f.Metadata.ID[:8], len(f.Spec.Agents), len(f.Spec.Board.Stations))

		for _, w := range f.Warnings() {
			out.printf("  ⚠ %s\n", w)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d factories are invalid", failed, len(dirs))
	}

	return nil
}

func push(ctx context.Context, out *printer, catalog *factorystore.Catalog, bucket string, org domain.OrgID, dir string) error {
	f, changes, err := catalog.Push(ctx, org, dir)
	for _, c := range changes {
		out.printf("  %-9s %-50s %s\n", c.Action, c.Path, c.Version)
	}

	if err != nil {
		return err
	}

	out.printf("✓ pushed %s to s3://%s/%s\n", f.Metadata.Name, bucket, factorystore.Prefix(org, f.Metadata.ID))

	for _, w := range f.Warnings() {
		out.printf("  ⚠ %s\n", w)
	}

	return nil
}

func pull(ctx context.Context, out *printer, catalog *factorystore.Catalog, org domain.OrgID, args []string) error {
	id, dir := args[0], args[1]

	s, err := catalog.Pull(ctx, org, id, dir)
	if err != nil {
		return err
	}

	for _, f := range s.Files {
		out.printf("  %-60s %s\n", f.Key, f.Version)
	}

	out.printf("✓ pulled %s (%d files) into %s\n", s.ID, len(s.Files), dir)

	return nil
}

func list(ctx context.Context, out *printer, catalog *factorystore.Catalog, bucket string, org domain.OrgID) error {
	entries, err := catalog.List(ctx, org)
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

	out.printf("%d factories in s3://%s/%s\n", len(entries), bucket, factorystore.OrgPrefix(org))

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
