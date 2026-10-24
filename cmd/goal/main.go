// Command goal is the Goal CLI. Through goald's HTTP API, always within one organization
// (org.go resolves which), it changes factory definitions (factory.go: goald keeps them as
// drafts and published versions; a folder pulled with goal factory pull is edited and sent back
// with goal factory push, which is the one way a definition changes outside the builder),
// manages organizations, and submits, follows and decides on work items. It also starts the
// local platform (goal up), validates folders offline (goal validate), and keeps the install's
// skills and template catalogs in the bucket (catalog.go).
//
// goald's API is GOAL_API_URL, else api_url in the CLI's config file, else
// http://127.0.0.1:8080. The operator commands that use the bucket directly (goal storage init,
// goal catalog) read object storage from the environment: GOAL_S3_BUCKET, and for a local
// S3-compatible server (RustFS) GOAL_S3_ENDPOINT, GOAL_S3_ACCESS_KEY and GOAL_S3_SECRET_KEY. On
// AWS the standard credential chain applies.
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
)

const usage = `usage:
  goal up                                         start PostgreSQL and RustFS, prepare the database roles
                                                  and the bucket, run goald
  goal run <factory> "<brief>" [--type T] [--body B] [--priority N] [--detach]
                                                  submit a work item and follow it until a human or done
  goal follow <item>                              follow an item's events again
  goal item <item>                                show an item with its outputs
  goal approve <item>                             approve an item waiting at a human station
  goal reject <item> --reason "..."               send it back for rework
  goal retry <item>                               send a blocked item back to its station's queue
  goal merge <item>                               merge the item's pull request, then approve it
                                                  (at a station with action: merge)
  goal note <item> "<text>"                       leave a note for the agents: the item's next run
                                                  reads it
  goal move <item> <station> [--reason "..."]     put the item at another station (a reason is needed
                                                  upstream, where it counts as rework); a run in
                                                  progress is stopped
  goal pause <item> [--reason "..."]              stop the item where it is (goal retry resumes it);
                                                  a run in progress is stopped
  goal andon <factory>                            whether the line runs, its gates' rework tallies and
                                                  the recent andon pulls
  goal andon pull <factory> --reason "..."        pull the andon cord: the line releases and claims
                                                  nothing until it is cleared
  goal andon clear <factory> [--note "..."]       clear it: the line runs again
  goal factory pull <factory> <dir> [--version N|current|draft]
                                                  write a version's files (default: the current one)
                                                  into a new folder, with .goal/pulled.json
  goal factory validate <dir>... [--offline]      validate folders through goald (--offline: schema and
                                                  references only, without goald; goal validate is
                                                  goal factory validate --offline, same flags)
  goal factory push <dir> [--publish [--message M]] [--replace-draft] [--force] [--move S=T]...
                                                  make the folder the factory's draft (creating the
                                                  factory if its metadata.id is new); --publish then
                                                  publishes it. Refused when an open draft has changes
                                                  the folder was not pulled from (--replace-draft
                                                  archives it) or a version was published after the
                                                  pull (--force pushes over it)
  goal factory publish <factory> [--message M] [--move S=T]...
                                                  validate and publish the open draft
  goal factory list [--validate]                  the factories: current version, open draft, paused
  goal factory versions <factory>                 the published versions (* the one that runs)
  goal factory restore <factory> <N> [--move S=T]...
                                                  run version N again
  goal org list                                   the organizations (* the one commands act in)
  goal org use <slug>                             act in this organization from now on (config file)
  goal org create <slug> --name N [--mark M]      create an organization
  goal org update <slug> [--name N] [--slug S] [--mark M] [--max-slots N]
                                                  rename it, or cap the agents running at once
                                                  across its factories (--max-slots 0: no cap)
  goal org delete <slug> [--yes]                  delete it after typing its name (--yes: no prompt);
                                                  it is purged after GOAL_ORG_RETENTION
  goal storage init                               create the bucket if missing and enable versioning
  goal catalog skills sync --file <sources.yaml> [--search <q>]... [--top <n>]
                                                  fetch skills.sh skills (a YAML list of
                                                  owner/repo@skill[#commit], plus the top n of each
                                                  search) into the bucket's skills catalog
  goal catalog skills list [q]                    the catalog's skills: key, commit, installs
  goal catalog templates push <dir> --version N [--name NAME]
                                                  add a template version (<dir>/template.yaml and the
                                                  definition in <dir>/factory/; NAME defaults to the
                                                  folder's name) to the bucket's template catalog
  goal catalog templates list                     the catalog's templates, latest version of each
  goal db init                                    create the database roles goald runs as (goal_owner,
                                                  goal_app) and hand the schema to goal_owner
  goal version                                    print the module version and build info

<item> is goal#2 (factory and number), the item's id, or a bare number such as 2 when one
factory has that item (--factory F picks the factory of a bare number). <factory> is a name or an id.
--move S=T sends the open items of station S, which the new version drops, to station T.

Factory definitions live in goald (drafts and published versions), not in files: goal factory
push is how a folder changes one, and goald ignores factory files written to the bucket.

Every command that talks to goald (run, follow, item, note, move, pause, the decisions, andon, factory) acts in one
organization: --org <slug|id>, else GOAL_ORG, else org in the config file
($XDG_CONFIG_HOME/goal/config.json, default ~/.config/goal/config.json; goal org use writes it),
else the install's only organization (decisions are the local user's)`

// errUsage marks a command line that names no valid command; main exits 2 for it.
var errUsage = errors.New(usage)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	out := &printer{w: os.Stdout, in: os.Stdin}
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

// itemCommand is a command on one work item.
type itemCommand func(ctx context.Context, out *printer, getenv func(string) string, args []string) error

func run(ctx context.Context, args []string, out *printer, getenv func(string) string, readBuildInfo buildInfoFunc) error {
	if len(args) == 0 {
		return errUsage
	}

	cmd, rest := args[0], args[1:]

	// The commands on one item, by name.
	if onItem, ok := map[string]itemCommand{
		"run": runItem, "follow": followItem, "item": showItem, verbNote: noteItem, verbMove: moveItem, verbPause: pauseItem,
	}[cmd]; ok {
		return onItem(ctx, out, getenv, rest)
	}

	switch cmd {
	case "version":
		return version(out, readBuildInfo, rest)
	case "validate":
		return factoryValidate(ctx, out, getenv, rest, validateAlias)
	case "factory":
		return factory(ctx, out, getenv, rest)
	case "up":
		return up(ctx, out, getenv, rest)
	case verbApprove, verbReject, verbRetry, verbMerge:
		return decide(ctx, out, getenv, cmd, rest)
	case "org":
		return orgCommand(ctx, out, getenv, rest)
	case "andon":
		return andon(ctx, out, getenv, rest)
	case "db", "storage":
		return setUp(ctx, out, getenv, cmd, rest)
	case "catalog":
		return catalogCommand(ctx, out, getenv, rest)
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

// printer writes command output to stdout and keeps the first write error, so a closed pipe
// fails the command once instead of being ignored.
type printer struct {
	w io.Writer
	// in is the terminal's input, read for confirmations.
	in  io.Reader
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
