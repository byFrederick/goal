// Package gitworkspace implements ports.Workspaces with the git command line on the host: every
// run gets a fresh clone of the factory's repository in <Root>/<run id>/repo, on the item's
// branch.
//
// The clone token never touches the disk or another process's command line. It reaches git
// once, for the clone, as an http.<origin>.extraHeader set through GIT_CONFIG_* environment
// variables, which git does not write to .git/config; the header is scoped to the repository's
// origin, credential helpers are switched off and git runs without the user's global and system
// configuration. Later git commands in the workspace (and the agent in its container) have no
// credentials at all.
package gitworkspace

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var (
	_ ports.Workspaces = (*Workspaces)(nil)
	_ ports.Reaper     = (*Workspaces)(nil)
)

// Config tunes the workspaces. The zero value of each field means its default.
type Config struct {
	// Root holds one folder per run; Docker must be able to bind-mount it. Default:
	// $TMPDIR/goal-workspaces.
	Root string
	// Git is the git executable. Default: "git" on PATH.
	Git string
	// Timeout bounds one git command. Default: 10m.
	Timeout time.Duration
	// AuthorName and AuthorEmail are the clone's local git identity, for commits the agent makes.
	// Defaults: "Goal agent" and "goal-agent@example.com".
	AuthorName  string
	AuthorEmail string
}

func (c Config) withDefaults() Config {
	if c.Root == "" {
		c.Root = filepath.Join(os.TempDir(), "goal-workspaces")
	}

	if c.Git == "" {
		c.Git = "git"
	}

	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Minute
	}

	if c.AuthorName == "" {
		c.AuthorName = "Goal agent"
	}

	if c.AuthorEmail == "" {
		c.AuthorEmail = "goal-agent@example.com"
	}

	return c
}

// Workspaces prepares run workspaces under one root folder. It is safe for concurrent use.
type Workspaces struct {
	secrets ports.Secrets
	cfg     Config
	log     *slog.Logger
}

// New returns workspaces that resolve clone tokens through secrets.
func New(secrets ports.Secrets, cfg Config, log *slog.Logger) *Workspaces {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Workspaces{secrets: secrets, cfg: cfg.withDefaults(), log: log}
}

// repoDir is the checkout inside a run's folder; the folder itself stays private to goald.
const repoDir = "repo"

// safeRun matches run ids usable as a folder name.
var safeRun = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Prepare implements ports.Workspaces.
func (w *Workspaces) Prepare(ctx context.Context, s ports.WorkspaceSpec) (ports.Workspace, error) {
	if !safeRun.MatchString(string(s.RunID)) {
		return ports.Workspace{}, fmt.Errorf("run id %q is not usable as a folder name", s.RunID)
	}

	// The root and run folders are private to goald's user; only the checkout inside is shared
	// with the container, whose user may differ.
	if err := os.MkdirAll(w.cfg.Root, 0o700); err != nil {
		return ports.Workspace{}, fmt.Errorf("create workspace root: %w", err)
	}

	runDir := w.runDir(s.RunID)
	if err := os.Mkdir(runDir, 0o700); err != nil {
		return ports.Workspace{}, fmt.Errorf("create run workspace: %w", err)
	}

	ws := ports.Workspace{RunID: s.RunID, Dir: filepath.Join(runDir, repoDir)}

	err := w.prepare(ctx, s, &ws)
	if err == nil {
		err = shareWithContainer(ws.Dir)
	}

	if err != nil {
		if rerr := os.RemoveAll(runDir); rerr != nil {
			w.log.WarnContext(ctx, "remove failed workspace", slog.String("dir", runDir), slog.Any("error", rerr))
		}

		return ports.Workspace{}, err
	}

	return ws, nil
}

func (w *Workspaces) prepare(ctx context.Context, s ports.WorkspaceSpec, ws *ports.Workspace) error {
	if s.Repository.URL == "" {
		return os.Mkdir(ws.Dir, 0o700)
	}

	origin, err := cloneOrigin(s.Repository.URL)
	if err != nil {
		return err
	}

	var token string
	if s.Repository.AuthRef != "" {
		if token, err = w.secrets.Resolve(ctx, s.Repository.AuthRef); err != nil {
			return fmt.Errorf("repository token: %w", err)
		}
	}

	if _, err := w.git(ctx, "", nil, "check-ref-format", "--branch", s.Branch); err != nil {
		return fmt.Errorf("branch %q: %w", s.Branch, err)
	}

	clone := []string{"clone", "--quiet", "--", s.Repository.URL, ws.Dir}
	if _, err := w.git(ctx, "", authEnv(origin, token), clone...); err != nil {
		return redacted(fmt.Errorf("clone %s: %w", s.Repository.URL, err), token)
	}

	_, err = w.git(ctx, ws.Dir, nil, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+s.Branch)
	ws.Existed = err == nil

	checkout := []string{"checkout", "--quiet", "-b", s.Branch}
	if ws.Existed {
		checkout = []string{"checkout", "--quiet", "-B", s.Branch, "origin/" + s.Branch}
	}

	steps := [][]string{
		checkout,
		{"config", "user.name", w.cfg.AuthorName},
		{"config", "user.email", w.cfg.AuthorEmail},
	}
	for _, args := range steps {
		if _, err := w.git(ctx, ws.Dir, nil, args...); err != nil {
			return err
		}
	}

	ws.Branch = s.Branch

	return nil
}

// Remove implements ports.Workspaces.
func (w *Workspaces) Remove(_ context.Context, ws ports.Workspace) error {
	if !safeRun.MatchString(string(ws.RunID)) {
		return fmt.Errorf("run id %q is not usable as a folder name", ws.RunID)
	}

	return removeAll(w.runDir(ws.RunID))
}

// Leftovers implements ports.Reaper: the runs with a workspace here. A folder records no
// organization.
func (w *Workspaces) Leftovers(context.Context) ([]ports.Leftover, error) {
	entries, err := os.ReadDir(w.cfg.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}

	var left []ports.Leftover

	for _, e := range entries {
		if e.IsDir() && safeRun.MatchString(e.Name()) {
			left = append(left, ports.Leftover{Run: domain.RunID(e.Name())})
		}
	}

	return left, nil
}

// Reap implements ports.Reaper: it removes the given runs' workspaces, for example after the
// goald running them died.
func (w *Workspaces) Reap(_ context.Context, runs []domain.RunID) error {
	var errs []error

	for _, run := range runs {
		if !safeRun.MatchString(string(run)) {
			continue
		}

		if err := removeAll(w.runDir(run)); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (w *Workspaces) runDir(run domain.RunID) string { return filepath.Join(w.cfg.Root, string(run)) }

// git runs one git command in dir with only the environment git needs, never the user's
// configuration, and returns its stdout.
func (w *Workspaces) git(ctx context.Context, dir string, extra []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, w.cfg.Git, args...) //nolint:gosec // fixed git subcommands; values are separated by --
	cmd.Dir = dir

	cmd.Env = append(baseEnv(), extra...)

	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

// baseEnv is git's whole environment: no user or system configuration, no prompts, no
// credential helpers, and nothing inherited from goald's own environment but PATH and HOME.
func baseEnv() []string {
	env := []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"LC_ALL=C",
	}

	for _, name := range []string{"PATH", "HOME", "TMPDIR"} {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}

	return env
}

// authEnv passes the token to one git command as configuration in the environment: an
// Authorization header sent only to origin, with credential helpers off.
func authEnv(origin, token string) []string {
	env := []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0="}
	if token == "" {
		return env
	}

	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))

	return []string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=http." + origin + ".extraHeader", "GIT_CONFIG_VALUE_1=Authorization: Basic " + basic,
	}
}

// cloneOrigin checks a clone URL and returns its origin (scheme://host/) for scoping the
// header. URLs are https without credentials; plain http is accepted for loopback only.
func cloneOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("repository url: %w", err)
	}

	switch {
	case u.User != nil:
		return "", errors.New("repository url must not carry credentials; use the repository token")
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return "", fmt.Errorf("repository url %q must be https", raw)
	}

	if u.Host == "" {
		return "", fmt.Errorf("repository url %q has no host", raw)
	}

	return u.Scheme + "://" + u.Host + "/", nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// redacted removes the token, raw and encoded, from err's message.
func redacted(err error, token string) error {
	if token == "" {
		return err
	}

	msg := err.Error()
	for _, s := range []string{base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token)), token} {
		msg = strings.ReplaceAll(msg, s, "[redacted]")
	}

	return errors.New(msg)
}

// shareWithContainer makes the checkout writable by the container's user, who may not be
// goald's user on Linux. The run folder around it stays private (0700), so no other host user
// can reach it.
func shareWithContainer(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		mode := os.FileMode(0o666)
		if d.IsDir() {
			mode = 0o777
		} else if info, err := d.Info(); err == nil && info.Mode()&0o111 != 0 {
			mode = 0o777
		}

		return os.Chmod(p, mode) //nolint:gosec // see above: shared with the container's user only
	})
}

// removeAll deletes a run folder, first making read-only folders git or tools left writable.
func removeAll(dir string) error {
	err := os.RemoveAll(dir)
	if err == nil {
		return nil
	}

	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700) //nolint:gosec // only to delete it
		}

		return nil
	})

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove workspace %s: %w", dir, err)
	}

	return nil
}
