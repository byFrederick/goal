// Command goal is the Goal CLI. Through goald's HTTP API, always within one organization
// (org.go resolves which), it changes factory definitions (factory.go: goald keeps them as
// drafts and published versions; a folder pulled with goal factory pull is edited and sent back
// with goal factory push, which is the one way a definition changes outside the builder),
// manages organizations, and submits, follows and decides on work items. It also starts the
// local platform (goal up), validates folders offline (goal validate), and keeps the install's
// skills and template catalogs in the bucket (catalog.go).
//
// goald's API is GOAL_API_URL, else api_url in the CLI's config file, else
// http://127.0.0.1:8080; requests carry the credential of its origin (credentials.go), which goal
// login stores (login.go). The operator commands that use the bucket directly (goal storage init,
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
                                                  (--type: one of the factory's work types; default its
                                                  first, none when it declares none)
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
  goal org create <slug> --name N [--mark M]      create an organization (platform admins only)
  goal org update <slug> [--name N] [--slug S] [--mark M] [--max-slots N]
                                                  rename it, or cap the agents running at once
                                                  across its factories (--max-slots 0: no cap)
  goal org delete <slug> [--yes]                  delete it after typing its name (--yes: no prompt);
                                                  it is purged after GOAL_ORG_RETENTION
  goal key list                                   the organization's API keys: hint, scopes, factories,
                                                  ceiling, expiry, last use
  goal key create <name> --scope S [--scope S]... [--factory F]... [--role viewer|approver|editor] [--expires 90d]
                                                  create an API key for scripts and CI (scopes: permissions
                                                  such as org:read, factory:read, item:create; --role is
                                                  the most it acts as on a factory, default viewer;
                                                  --expires 1d to 90d, default 90d). The secret is printed
                                                  once, alone on stdout: store it as GOAL_API_KEY
  goal key revoke <name|id>                       revoke a key: anything using it stops working at once
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
  goal db recover-admin <email>                   make the person a platform admin with database access
                                                  alone (GOAL_DATABASE_ADMIN_URL), creating them if no
                                                  one has the email; goald may be stopped
  goal db recover-owner <org> <email>             make the person an owner of the organization, the same way
  goal admin list                                 the platform admins (platform admins only)
  goal admin add <email>                          make a person who has signed in a platform admin
  goal admin remove <email>                       stop them being one (the last one stays)
  goal admin events [--kind K] [--before N]       the security log, newest first
  goal login [--store keychain|file]              sign the CLI in: approve the code it shows at
                                                  <goald>/device in a browser; the session goes to the
                                                  OS keychain, or with --store file (remembered) to
                                                  credentials.json beside the config file (mode 0600)
  goal login --token [--store keychain|file]      store a personal token (or API key) read from stdin
                                                  instead: goal login --token < token-file
  goal logout [--all]                             end the CLI's session (--all: every session of yours)
                                                  and forget it here
  goal whoami                                     who the CLI acts as, and in which organizations
  goal version                                    print the module version and build info

<item> is goal#2 (factory and number), the item's id, or a bare number such as 2 when one
factory has that item (--factory F picks the factory of a bare number). <factory> is a name or an id.
--move S=T sends the open items of station S, which the new version drops, to station T.

Factory definitions live in goald (drafts and published versions), not in files: goal factory
push is how a folder changes one, and goald ignores factory files written to the bucket.

Every command that talks to goald (run, follow, item, note, move, pause, the decisions, andon, factory) acts in one
organization: --org <slug|id>, else GOAL_ORG, else org in the config file
($XDG_CONFIG_HOME/goal/config.json, default ~/.config/goal/config.json; goal org use writes it),
else the install's only organization (decisions are the local user's); with GOAL_API_KEY set, commands
act in the key's own organization

With sign-in on (GOAL_AUTH=session), commands send GOAL_API_KEY (an API key goal_sk_… or a
personal token goal_pat_…) when it is set, else what goal login stored for goald's origin, and
only over https or to this machine; a refused credential or permission exits 77`

// errUsage marks a command line that names no valid command; main exits 2 for it.
var errUsage = errors.New(usage)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	out := &printer{w: os.Stdout, e: os.Stderr, in: os.Stdin}
	err := errors.Join(run(ctx, os.Args[1:], out, os.Getenv, debug.ReadBuildInfo), out.err)

	stop()

	var refused *accessError

	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	case errors.As(err, &refused):
		fmt.Fprintln(os.Stderr, "goal:", err)
		os.Exit(exitNoPerm)
	case err != nil:
		fmt.Fprintln(os.Stderr, "goal:", err)
		os.Exit(1)
	}
}

// command is a command, or a group of them, given its arguments.
type command func(ctx context.Context, out *printer, getenv func(string) string, args []string) error

func run(ctx context.Context, args []string, out *printer, getenv func(string) string, readBuildInfo buildInfoFunc) error {
	err := dispatch(ctx, args, out, getenv, readBuildInfo)

	// goal login cannot help when the refused credential is GOAL_API_KEY's.
	var refused *accessError
	if errors.As(err, &refused) && refused.signedOut && getenv(envAPIKey) != "" {
		return &accessError{
			msg:       fmt.Sprintf("%s refused the key in %s: it may have expired or been revoked", refused.origin, envAPIKey),
			signedOut: true, origin: refused.origin,
		}
	}

	return err
}

func dispatch(ctx context.Context, args []string, out *printer, getenv func(string) string, readBuildInfo buildInfoFunc) error {
	if len(args) == 0 {
		return errUsage
	}

	cmd, rest := args[0], args[1:]

	// The commands on one item, and the command groups, by name.
	if handler, ok := map[string]command{
		"run": runItem, "follow": followItem, "item": showItem, verbNote: noteItem, verbMove: moveItem, verbPause: pauseItem,
		"factory": factory, "up": up, "org": orgCommand, "key": keyCommand, "andon": andon, "admin": adminCommand, "catalog": catalogCommand,
	}[cmd]; ok {
		return handler(ctx, out, getenv, rest)
	}

	// The commands on the CLI's own sign-in, with the user's OS keychain.
	if account, ok := map[string]accountCommand{"login": login, "logout": logout, "whoami": whoami}[cmd]; ok {
		return account(ctx, out, getenv, rest, osKeychain{})
	}

	switch cmd {
	case "version":
		return version(out, readBuildInfo, rest)
	case "validate":
		return factoryValidate(ctx, out, getenv, rest, validateAlias)
	case verbApprove, verbReject, verbRetry, verbMerge:
		return decide(ctx, out, getenv, cmd, rest)
	case "db", "storage":
		return setUp(ctx, out, getenv, cmd, rest)
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
	// e is stderr, for what must stay out of stdout when stdout is piped (nil: discarded).
	e io.Writer
	// in is the terminal's input, read for confirmations.
	in  io.Reader
	err error
}

func (p *printer) printf(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

// noticef writes to stderr: what a person reads beside output a script consumes.
func (p *printer) noticef(format string, a ...any) {
	if p.e != nil {
		_, _ = fmt.Fprintf(p.e, format, a...)
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
