package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/byFrederick/goal/internal/adapters/postgres"
)

// goaldStopGrace is how long goald gets to release its runs after Ctrl-C before it is killed;
// authStopGrace how long the auth service gets.
const (
	goaldStopGrace = 30 * time.Second
	authStopGrace  = 5 * time.Second
)

// Sign-in under goal up (spec 3 §5.6, §9).
const (
	// defaultAuthMode is goald's GOAL_AUTH default (cmd/goald/settings.go defaultAuth); goal up
	// resolves the mode the same way and only passes the variable on.
	defaultAuthMode = authSession
	authSession     = "session"
	authNone        = "none"
	// keepLocalUser ends every refusal of sign-in: the explicit way back to the single local user.
	keepLocalUser = "or set GOAL_AUTH=none to keep the single local user (loopback only, one developer's laptop)"
	authModeEnv   = "GOAL_AUTH"
	authSecretEnv = "BETTER_AUTH_SECRET" //nolint:gosec // a variable name, not a credential
	gatewayKeyEnv = "GOAL_GATEWAY_KEY"
	// authServer is the auth service's build (task auth:build), from the repository root.
	authServer = "auth/dist/server.js"
	// mailpitInbox is compose.yaml's Mailpit, where sign-in codes arrive locally; its UI and API
	// take mailpitUser and the password in mailpitAuthName, beside the CLI's config.
	mailpitInbox    = "http://127.0.0.1:8025"
	mailpitUser     = "goal"
	mailpitAuthName = "mailpit-ui-auth"
)

// authVariables are the auth service's own variables, and the only ones it is started with: the
// Taskfile's AUTH_VARIABLES (task auth:dev), kept equal by TestUp_AuthVariablesAreTheTaskfiles.
var authVariables = []string{
	"PATH", "HOME", "GOAL_PUBLIC_URL", "GOAL_AUTH_GOOGLE", "GOAL_AUTH_LISTEN", "GOAL_AUTH_DATABASE_URL", "GOAL_PG_AUTH_PASSWORD",
	"BETTER_AUTH_SECRET", "BETTER_AUTH_SECRETS", "GOAL_BOOTSTRAP_OWNER_EMAIL", "GOAL_SMTP_URL", "GOAL_SMTP_FROM",
	"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "NODE_ENV", "NODE_OPTIONS", "NODE_EXTRA_CA_CERTS",
}

// authOnly are the auth service's variables goald never gets: its secrets, its database, its
// mail and its Google client (goald's reserved list holds them too, cmd/goald identityVariables).
var authOnly = []string{
	"GOAL_AUTH_LISTEN", "GOAL_AUTH_DATABASE_URL", "GOAL_PG_AUTH_PASSWORD", "BETTER_AUTH_SECRET", "BETTER_AUTH_SECRETS",
	"BETTER_AUTH_URL", "GOAL_BOOTSTRAP_OWNER_EMAIL", "GOAL_SMTP_URL", "GOAL_SMTP_FROM", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET",
}

// up starts the local platform: PostgreSQL and RustFS from compose.yaml (waiting until they are
// healthy), the database roles (goal db init, so an existing database is handed to goal_owner
// before goald migrates it), the bucket, then goald in the foreground until Ctrl-C. Under
// GOAL_AUTH=session, the default, it also starts Mailpit (compose) and the auth service (node
// auth/dist/server.js) beside goald, each process with only its own variables, and refuses to
// start (before anything when it can tell, before the roles otherwise) without a usable
// BETTER_AUTH_SECRET, the auth service's build, or GOAL_BOOTSTRAP_OWNER_EMAIL while nobody has
// signed in, or with a database password this repository publishes, each refusal naming
// GOAL_AUTH=none as the way to keep the single local user. It
// pushes no factory: the bucket's definitions are what goald runs. The compose services keep
// running afterwards; `docker compose down` stops them.
func up(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) != 0 {
		return errUsage
	}

	signIn, err := signInMode(getenv)
	if err != nil {
		return err
	}

	if err := compose(ctx, out, "PostgreSQL and RustFS", "postgres", "rustfs"); err != nil {
		return err
	}

	// Before the roles, the bucket, the auth service or goald: sign-in nobody could use.
	if signIn {
		if err := bootstrapReady(ctx, getenv); err != nil {
			return err
		}
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

	environ := os.Environ()
	publicURL := publicURLOf(getenv)

	if signIn {
		inbox, err := mailpitAuth(getenv)
		if err != nil {
			return err
		}

		stop, err := startSignIn(ctx, out, authEnv(environ, publicURL))
		if err != nil {
			return err
		}
		defer stop()

		out.printf("sign in at %s (codes arrive in Mailpit, %s: user %s, password in %s)\n", publicURL, mailpitInbox, mailpitUser, inbox)
	}

	return runGoald(ctx, out, bin, goaldEnv(environ, getenv, signIn, publicURL))
}

// signInMode resolves GOAL_AUTH as goald does and reports whether it is session, refusing
// sign-in that could not start: no usable secret, or no build of the auth service.
func signInMode(getenv func(string) string) (bool, error) {
	mode := strings.TrimSpace(getenv(authModeEnv))
	if mode == "" {
		mode = defaultAuthMode
	}

	switch mode {
	case authNone:
		return false, nil
	case authSession:
	default:
		return false, fmt.Errorf("GOAL_AUTH %q is not none or session", mode)
	}

	if err := authSecrets(getenv); err != nil {
		return false, err
	}

	if _, err := os.Stat(authServer); err != nil {
		return false, fmt.Errorf("%s needs the auth service's build %s: run task auth:build (task up does); %s", sessionMode(getenv), authServer, keepLocalUser)
	}

	if err := publishedPasswords(getenv); err != nil {
		return false, err
	}

	return true, nil
}

// devPasswords are the database passwords this repository publishes (compose.yaml, .env.example,
// the postgres.Dev* defaults): good enough for the single local user, never once people sign in,
// since any process on the machine could use them.
var devPasswords = []string{"goal-dev-only", postgres.DevOwnerPassword, postgres.DevAppPassword, postgres.DevAuthPassword}

// publishedPasswords refuses sign-in while a database role goal up, goald or the auth service
// connect as has one of devPasswords, set or by default, naming the variables to change.
func publishedPasswords(getenv func(string) string) error {
	s := dbSettingsFrom(getenv)

	admin := superuserPasswordEnv
	if strings.TrimSpace(getenv(adminURLEnv)) != "" {
		admin = adminURLEnv
	}

	var published []string

	check := func(name, password string) {
		if slices.Contains(devPasswords, password) {
			published = append(published, name)
		}
	}

	check(admin, passwordOf(s.AdminURL))
	check(ownerPasswordEnv, s.Roles.OwnerPassword)
	check(appPasswordEnv, s.Roles.AppPassword)
	check(authPasswordEnv, s.Roles.AuthPassword)

	for _, name := range []string{"GOAL_MIGRATE_DATABASE_URL", "GOAL_DATABASE_URL", "GOAL_AUTH_DATABASE_URL"} {
		if raw := strings.TrimSpace(getenv(name)); raw != "" {
			check(name, passwordOf(raw))
		}
	}

	if len(published) == 0 {
		return nil
	}

	superuser := ""
	if slices.Contains(published, superuserPasswordEnv) {
		superuser = " Compose's superuser keeps the password its database was created with: change it there first " +
			`(docker compose exec postgres psql -U goal -d goal -c "ALTER ROLE goal PASSWORD '<new>'"), then GOAL_PG_PASSWORD.`
	}

	return fmt.Errorf("%s refuses the database passwords this repository publishes, which any process on this machine could use: %s. "+
		"Give each a value of its own in .env (openssl rand -hex 24); goal up gives goal_owner, goal_app and goal_auth their new passwords itself.%s %s",
		sessionMode(getenv), strings.Join(published, ", "), superuser, keepLocalUser)
}

// passwordOf is the password in a postgres:// URL, or "" when it has none or does not parse.
func passwordOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return ""
	}

	p, _ := u.User.Password()

	return p
}

// sessionMode names the sign-in mode in a refusal: "GOAL_AUTH=session (the default)" when .env
// does not set it, so people upgrading learn why goal up asks for something new.
func sessionMode(getenv func(string) string) string {
	if strings.TrimSpace(getenv(authModeEnv)) == "" {
		return "GOAL_AUTH=session (the default)"
	}

	return "GOAL_AUTH=session"
}

// mailpitAuth makes sure Mailpit's password file exists (compose.yaml mounts it; Mailpit does
// not start without it): mailpitUser and a random password, mode 0600, beside the CLI's config
// ($XDG_CONFIG_HOME/goal, default ~/.config/goal). An existing file is kept. It returns its path.
func mailpitAuth(getenv func(string) string) (string, error) {
	config, err := configPath(getenv)
	if err != nil {
		return "", err
	}

	path := filepath.Join(filepath.Dir(config), mailpitAuthName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("mailpit password file: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the user's own config dir
	if errors.Is(err, fs.ErrExist) {
		return path, nil
	}

	if err != nil {
		return "", fmt.Errorf("mailpit password file: %w", err)
	}

	secret := make([]byte, 24)
	_, _ = rand.Read(secret)

	_, err = fmt.Fprintf(f, "%s:%s\n", mailpitUser, hex.EncodeToString(secret))
	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("mailpit password file: %w", err)
	}

	return path, nil
}

// startSignIn starts what sign-in needs beside goald: Mailpit and the auth service with env. It
// returns what stops the auth service.
func startSignIn(ctx context.Context, out *printer, env []string) (func(), error) {
	if err := compose(ctx, out, "Mailpit", "mailpit"); err != nil {
		return nil, err
	}

	return startAuth(ctx, out, env)
}

// compose starts compose.yaml's services and waits until they are healthy.
func compose(ctx context.Context, out *printer, what string, services ...string) error {
	out.printf("starting %s (docker compose)\n", what)

	cmd := exec.CommandContext(ctx, "docker", slices.Concat([]string{"compose", "up", "--detach", "--wait"}, services)...) //nolint:gosec // fixed service names
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w (run goal up from the repository root)", err)
	}

	return nil
}

// runGoald runs goald in the foreground with env until it exits or ctx ends.
func runGoald(ctx context.Context, out *printer, bin string, env []string) error {
	out.printf("starting %s (Ctrl-C stops it; queued work stays in PostgreSQL)\n", bin)

	goald := exec.CommandContext(ctx, bin)
	goald.Stdout, goald.Stderr = os.Stdout, os.Stderr
	goald.Env = env
	goald.Cancel = func() error { return goald.Process.Signal(syscall.SIGTERM) }
	goald.WaitDelay = goaldStopGrace

	if err := goald.Run(); err != nil && ctx.Err() == nil { // after Ctrl-C goald has shut down
		return fmt.Errorf("goald: %w", err)
	}

	return nil
}

// startAuth starts the auth service in the background with env and returns what stops it
// (SIGTERM, then a kill after authStopGrace). It logs to this terminal; should it stop, goald goes
// on (signed-in people keep working) and goal up says so.
func startAuth(ctx context.Context, out *printer, env []string) (func(), error) {
	out.printf("starting the auth service (node %s)\n", authServer)

	ctx, cancel := context.WithCancel(ctx)

	node := exec.CommandContext(ctx, "node", authServer)
	node.Stdout, node.Stderr = os.Stderr, os.Stderr
	node.Env = env
	node.Cancel = func() error { return node.Process.Signal(syscall.SIGTERM) }
	node.WaitDelay = authStopGrace

	if err := node.Start(); err != nil {
		cancel()

		return nil, fmt.Errorf("auth service: %w (is node on PATH? mise installs it)", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		if err := node.Wait(); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "goal up: the auth service stopped (%v); signing in waits for it, goald goes on\n", err)
		}
	}()

	return func() {
		cancel()
		<-done
	}, nil
}

// publicURLOf is GOAL_PUBLIC_URL as goald resolves it: http://<GOAL_LISTEN> by default.
func publicURLOf(getenv func(string) string) string {
	if u := strings.TrimSpace(getenv("GOAL_PUBLIC_URL")); u != "" {
		return u
	}

	listen := strings.TrimSpace(getenv("GOAL_LISTEN"))
	if listen == "" {
		listen = "127.0.0.1:8080"
	}

	return "http://" + listen
}

// authEnv is the auth service's environment: exactly authVariables of environ, and the public
// URL people open goald at (the same default goald takes).
func authEnv(environ []string, publicURL string) []string {
	out := []string{}

	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(authVariables, name) && name != "GOAL_PUBLIC_URL" {
			out = append(out, kv)
		}
	}

	return append(out, "GOAL_PUBLIC_URL="+publicURL)
}

// goaldEnv is goald's environment: environ without the auth service's own variables, with the
// text log on a terminal, and under sign-in the public URL the auth service was given.
func goaldEnv(environ []string, getenv func(string) string, signIn bool, publicURL string) []string {
	out := []string{}

	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(authOnly, name) || (signIn && name == "GOAL_PUBLIC_URL") {
			continue
		}

		out = append(out, kv)
	}

	if getenv("GOAL_LOG_FORMAT") == "" {
		out = append(out, "GOAL_LOG_FORMAT=text")
	}

	if signIn {
		out = append(out, "GOAL_PUBLIC_URL="+publicURL)
	}

	return out
}

// authSecrets refuses to sign people in without BETTER_AUTH_SECRET or BETTER_AUTH_SECRETS, or
// with one that is goald's gateway key: the two must never be the same secret.
func authSecrets(getenv func(string) string) error {
	secret, versioned := strings.TrimSpace(getenv(authSecretEnv)), strings.TrimSpace(getenv("BETTER_AUTH_SECRETS"))
	if secret == "" && versioned == "" {
		need := "BETTER_AUTH_SECRET in .env (openssl rand -hex 32, not GOAL_GATEWAY_KEY)"
		if strings.TrimSpace(getenv("GOAL_BOOTSTRAP_OWNER_EMAIL")) == "" {
			need += " and, until the first owner signs in, GOAL_BOOTSTRAP_OWNER_EMAIL (that owner's address)"
		}

		return fmt.Errorf("%s needs %s; %s", sessionMode(getenv), need, keepLocalUser)
	}

	gateway := strings.TrimSpace(getenv(gatewayKeyEnv))
	if gateway == "" {
		return nil
	}

	candidates := []string{secret}

	for entry := range strings.SplitSeq(versioned, ",") {
		_, value, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok {
			value = entry
		}

		candidates = append(candidates, strings.TrimSpace(value))
	}

	for _, c := range candidates {
		if c != "" && subtle.ConstantTimeCompare([]byte(c), []byte(gateway)) == 1 {
			return errors.New("BETTER_AUTH_SECRET is GOAL_GATEWAY_KEY: give the auth service a secret of its own (openssl rand -hex 32)")
		}
	}

	return nil
}

// bootstrapReady refuses to start sign-in that nobody could use: no user exists yet and
// GOAL_BOOTSTRAP_OWNER_EMAIL, the only address that may then sign in, is unset.
func bootstrapReady(ctx context.Context, getenv func(string) string) error {
	if strings.TrimSpace(getenv("GOAL_BOOTSTRAP_OWNER_EMAIL")) != "" {
		return nil
	}

	pool, err := postgres.Open(ctx, dbSettingsFrom(getenv).AdminURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	exist, err := postgres.UsersExist(ctx, pool)
	if err != nil {
		return err
	}

	if !exist {
		return fmt.Errorf("nobody can sign in yet: %s needs GOAL_BOOTSTRAP_OWNER_EMAIL in .env, the first owner's address; %s",
			sessionMode(getenv), keepLocalUser)
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
