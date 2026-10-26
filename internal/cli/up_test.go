package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/adapters/postgres"
)

// Variables the tests set.
const (
	xdgConfigHome = "XDG_CONFIG_HOME"
	listenEnv     = "GOAL_LISTEN"
)

// goal up and task auth:dev start the auth service with the same variables.
func TestUp_AuthVariablesAreTheTaskfiles(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}

	var taskfile struct {
		Vars struct {
			AuthVariables string `yaml:"AUTH_VARIABLES"`
		} `yaml:"vars"`
	}

	if err := yaml.Unmarshal(raw, &taskfile); err != nil {
		t.Fatal(err)
	}

	if got := strings.Fields(taskfile.Vars.AuthVariables); !slices.Equal(got, authVariables) {
		t.Errorf("Taskfile AUTH_VARIABLES = %v, goal up's %v; keep them equal", got, authVariables)
	}
}

// Every task that starts goald first builds the agent image from the checkout, as it builds the
// egress proxy's. A box started from an image older than the checkout runs an older worker, which
// drops what the run bundle gained since: the image built before 4-11 gave agents no MCP server
// of their connections.
func TestUp_TasksThatStartGoaldBuildTheAgentImage(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../Taskfile.yml")
	if err != nil {
		t.Fatal(err)
	}

	var taskfile struct {
		Tasks map[string]struct {
			Deps []string `yaml:"deps"`
			Cmds []any    `yaml:"cmds"`
		} `yaml:"tasks"`
	}

	if err := yaml.Unmarshal(raw, &taskfile); err != nil {
		t.Fatal(err)
	}

	starters := 0

	for name, task := range taskfile.Tasks {
		if !slices.ContainsFunc(task.Cmds, func(c any) bool {
			s, _ := c.(string)
			return s == "bin/goald" || strings.HasPrefix(s, "bin/goal up")
		}) {
			continue
		}

		starters++

		if !slices.Contains(task.Deps, "worker:image:build") {
			t.Errorf("task %s starts goald with deps %v, want worker:image:build among them", name, task.Deps)
		}
	}

	if starters < 3 {
		t.Errorf("found %d tasks that start goald, want up, up:compose and goald at least", starters)
	}
}

// The postmaster treats any child it did not start that exits non-zero as a crashed backend and
// restarts the server. As a container's PID 1 it inherits every orphan in the container, such as
// the pg_isready a shell healthcheck leaves behind when Docker kills the shell on a timeout. So
// docker-init is PID 1 and reaps them, and the healthcheck runs pg_isready without a shell, within
// a timeout longer than pg_isready's own.
func TestUp_PostgresIsNotTheContainersInit(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../compose.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var compose struct {
		Services struct {
			Postgres struct {
				Init        bool `yaml:"init"`
				Healthcheck struct {
					Test    []string `yaml:"test"`
					Timeout string   `yaml:"timeout"`
				} `yaml:"healthcheck"`
			} `yaml:"postgres"`
		} `yaml:"services"`
	}

	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}

	postgres := compose.Services.Postgres
	if !postgres.Init {
		t.Error("compose.yaml postgres: init is not true, so the postmaster is PID 1 and reaps strays as crashed backends")
	}

	if test := postgres.Healthcheck.Test; len(test) < 2 || test[0] != "CMD" || test[1] != "pg_isready" {
		t.Errorf("compose.yaml postgres healthcheck = %q, want [CMD pg_isready ...] (no shell to orphan it)", test)
	}

	// pg_isready gives up after 3 seconds by default; Docker must not kill it first.
	if timeout, err := time.ParseDuration(postgres.Healthcheck.Timeout); err != nil || timeout <= 3*time.Second {
		t.Errorf("compose.yaml postgres healthcheck timeout = %q, want longer than pg_isready's 3s", postgres.Healthcheck.Timeout)
	}
}

// Spec 3 §5.6, threat model row 33: each process gets only its own variables. The auth service
// sees its own and nothing of goald's (the gateway key, the bucket's or goald's database
// credentials); goald sees nothing of the auth service's secrets, mail or Google client.
func TestUp_EachProcessGetsItsOwnVariables(t *testing.T) {
	t.Parallel()

	environ := []string{
		"PATH=/usr/bin", "HOME=/home/acme", "GOAL_AUTH=session", "BETTER_AUTH_SECRET=auth-secret", "GOAL_GATEWAY_KEY=gateway-key",
		"GOAL_S3_SECRET_KEY=s3", "GOAL_DATABASE_URL=postgres://goal_app@db/goal", "GOOGLE_CLIENT_SECRET=google", "GOAL_SMTP_URL=smtp://mail",
		"GOAL_PG_AUTH_PASSWORD=pg-auth", "GOAL_BOOTSTRAP_OWNER_EMAIL=owner@example.com", "NODE_OPTIONS=--max-old-space-size=256",
		"CLAUDE_CODE_OAUTH_TOKEN=token",
	}

	auth := authEnv(environ, "http://127.0.0.1:9090")
	want := []string{
		"PATH=/usr/bin", "HOME=/home/acme", "BETTER_AUTH_SECRET=auth-secret", "GOOGLE_CLIENT_SECRET=google", "GOAL_SMTP_URL=smtp://mail",
		"GOAL_PG_AUTH_PASSWORD=pg-auth", "GOAL_BOOTSTRAP_OWNER_EMAIL=owner@example.com", "NODE_OPTIONS=--max-old-space-size=256",
		"GOAL_PUBLIC_URL=http://127.0.0.1:9090",
	}

	if !slices.Equal(auth, want) {
		t.Errorf("auth service env = %v, want %v", auth, want)
	}

	getenv := func(string) string { return "" }
	goald := goaldEnv(environ, getenv, true, "http://127.0.0.1:9090")

	for _, kv := range goald {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(authOnly, name) {
			t.Errorf("goald gets %s, the auth service's", name)
		}
	}

	for _, kv := range []string{"GOAL_GATEWAY_KEY=gateway-key", "GOAL_AUTH=session", "GOAL_PUBLIC_URL=http://127.0.0.1:9090", "GOAL_LOG_FORMAT=text"} {
		if !slices.Contains(goald, kv) {
			t.Errorf("goald env lacks %s: %v", kv, goald)
		}
	}
}

// Review 3, L4: goald and the auth service get one shared secret, the one the auth service
// requires of every request: .env's, else a new one per goal up, never two different ones.
func TestUp_GoaldAndTheAuthServiceShareAProxySecret(t *testing.T) {
	t.Parallel()

	environ := []string{"PATH=/usr/bin", "BETTER_AUTH_SECRET=auth-secret"}
	none := func(string) string { return "" }

	shared := withProxySecret(environ, none)
	secretOf := func(env []string) []string {
		var out []string

		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, proxySecretEnv+"="); ok {
				out = append(out, v)
			}
		}

		return out
	}

	auth, goald := secretOf(authEnv(shared, "http://127.0.0.1:8080")), secretOf(goaldEnv(shared, none, true, "http://127.0.0.1:8080"))
	if len(auth) != 1 || len(auth[0]) != 64 || !slices.Equal(auth, goald) {
		t.Errorf("auth service gets %v, goald %v; want one 32-byte secret for both", auth, goald)
	}

	if again := secretOf(withProxySecret(environ, none)); slices.Equal(again, auth) {
		t.Error("two goal ups generated the same secret")
	}

	set := append(slices.Clone(environ), proxySecretEnv+"=from-dotenv")
	if got := withProxySecret(set, func(n string) string { return map[string]string{proxySecretEnv: "from-dotenv"}[n] }); !slices.Equal(got, set) {
		t.Errorf("with .env's secret: %v; want it unchanged", got)
	}
}

// goald and the auth service agree on the origin people open: GOAL_PUBLIC_URL, else goald's
// listen address over http.
func TestUp_PublicURLIsGoaldsDefault(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{want: "http://127.0.0.1:8080"},
		{env: map[string]string{"GOAL_LISTEN": "127.0.0.1:18089"}, want: "http://127.0.0.1:18089"},
		{env: map[string]string{"GOAL_PUBLIC_URL": "https://goal.example.com", "GOAL_LISTEN": "0.0.0.0:8080"}, want: "https://goal.example.com"},
	} {
		if got := publicURLOf(func(n string) string { return c.env[n] }); got != c.want {
			t.Errorf("public URL of %v = %s, want %s", c.env, got, c.want)
		}
	}
}

// wayBack is what every refusal of sign-in names: the explicit way to keep the single local user.
const wayBack = "GOAL_AUTH=none"

// goal up refuses, before starting anything, sign-in it could not run: no secret, the gateway
// key reused as one, an unknown mode or no build of the auth service; each names the way out.
func TestUp_RefusesSignInItCannotStart(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		env  map[string]string
		want []string
	}{
		{name: "no secret", env: map[string]string{authModeEnv: authSession}, want: []string{authSecretEnv, wayBack}},
		// A laptop upgraded from 2.2 with nothing new in .env: sign-in is the default, and goal up
		// names both variables it needs before it starts anything.
		{
			name: "nothing new set", env: map[string]string{},
			want: []string{"GOAL_AUTH=session (the default)", authSecretEnv, "GOAL_BOOTSTRAP_OWNER_EMAIL", wayBack},
		},
		{
			name: "the gateway key as the secret",
			env:  map[string]string{authModeEnv: authSession, authSecretEnv: "same-0123456789abcdef0123456789ab", gatewayKeyEnv: "same-0123456789abcdef0123456789ab"},
			want: []string{"GOAL_GATEWAY_KEY"},
		},
		{
			name: "the gateway key among versioned secrets",
			env:  map[string]string{authModeEnv: authSession, "BETTER_AUTH_SECRETS": "2:new-secret,1:gateway-key", gatewayKeyEnv: "gateway-key"}, //nolint:gosec // fabricated test secrets
			want: []string{"GOAL_GATEWAY_KEY"},
		},
		{name: "an unknown mode", env: map[string]string{authModeEnv: "sso"}, want: []string{"GOAL_AUTH"}},
		{
			name: "no build of the auth service",
			env:  map[string]string{authModeEnv: authSession, authSecretEnv: "auth-0123456789abcdef0123456789abc", gatewayKeyEnv: "gateway"},
			want: []string{"task auth:build", wayBack},
		},
	} {
		err := up(t.Context(), &printer{w: io.Discard}, func(n string) string { return c.env[n] }, nil)
		for _, w := range c.want {
			if err == nil || !strings.Contains(err.Error(), w) {
				t.Errorf("%s: goal up = %v, want a refusal naming %q", c.name, err, w)
			}
		}

		if err != nil && strings.Contains(err.Error(), "auth-0123") {
			t.Errorf("%s: the refusal shows the secret", c.name)
		}
	}
}

// Review 3, H1: once people sign in, no database role keeps a password this repository
// publishes, set or by default; the refusal names each variable to change and how.
func TestUp_SignInRefusesPublishedDatabasePasswords(t *testing.T) {
	t.Parallel()

	own := map[string]string{ //nolint:gosec // fabricated passwords of the install's own
		superuserPasswordEnv: "superuser-own-pw", ownerPasswordEnv: "owner-own-pw", appPasswordEnv: "app-own-pw", authPasswordEnv: "auth-own-pw",
	}

	with := func(changes map[string]string) func(string) string {
		env := maps.Clone(own)
		maps.Copy(env, changes)

		return func(n string) string { return env[n] }
	}

	if err := publishedPasswords(with(nil)); err != nil {
		t.Errorf("own passwords: %v", err)
	}

	unset := map[string]string{superuserPasswordEnv: "", ownerPasswordEnv: "", appPasswordEnv: "", authPasswordEnv: ""}

	for _, c := range []struct {
		name    string
		changes map[string]string
		want    []string
	}{
		{name: "nothing set", changes: unset, want: []string{
			"GOAL_PG_PASSWORD, GOAL_PG_OWNER_PASSWORD, GOAL_PG_APP_PASSWORD, GOAL_PG_AUTH_PASSWORD", "ALTER ROLE goal", wayBack,
		}},
		{name: ".env.example's old app password", changes: map[string]string{appPasswordEnv: postgres.DevAppPassword}, want: []string{appPasswordEnv, wayBack}},
		{name: "a dev password under another role", changes: map[string]string{authPasswordEnv: "goal-dev-only"}, want: []string{authPasswordEnv}}, //nolint:gosec // the published dev password
		{
			name:    "an explicit URL with a dev password",
			changes: map[string]string{"GOAL_DATABASE_URL": "postgres://goal_app:" + postgres.DevAppPassword + "@127.0.0.1:5432/goal"},
			want:    []string{"GOAL_DATABASE_URL"},
		},
		{
			name:    "the admin URL",
			changes: map[string]string{adminURLEnv: "postgres://goal:goal-dev-only@127.0.0.1:5432/goal"}, //nolint:gosec // the published dev password
			want:    []string{adminURLEnv},
		},
	} {
		err := publishedPasswords(with(c.changes))
		for _, w := range c.want {
			if err == nil || !strings.Contains(err.Error(), w) {
				t.Errorf("%s: %v, want a refusal naming %q", c.name, err, w)
			}
		}

		if err != nil && strings.Contains(err.Error(), "own-pw") {
			t.Errorf("%s: the refusal shows a password", c.name)
		}

		if c.name != "nothing set" && err != nil && strings.Contains(err.Error(), "ALTER ROLE") {
			t.Errorf("%s: asks to change the superuser, whose password is its own", c.name)
		}
	}
}

// Review 3, H1: Mailpit's UI and API, where sign-in codes arrive, need a password; goal up
// generates it once, readable only by the user, beside the CLI's config, and keeps it after.
func TestUp_MailpitAuthIsGeneratedOnceAndPrivate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	getenv := func(n string) string { return map[string]string{xdgConfigHome: dir}[n] }

	path, err := mailpitAuth(getenv)
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(dir, "goal", "mailpit-ui-auth"); path != want {
		t.Errorf("path = %s, want %s (compose.yaml mounts it from there)", path, want)
	}

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("password file %v, %v; want mode 0600", info, err)
	}

	first, _ := os.ReadFile(path) //nolint:gosec // the test's file
	if !regexp.MustCompile(`^goal:[0-9a-f]{48}\n$`).Match(first) {
		t.Errorf("password file holds %q; want goal:<48 hex digits>", first)
	}

	if _, err := mailpitAuth(getenv); err != nil {
		t.Fatal(err)
	}

	if again, _ := os.ReadFile(path); string(again) != string(first) { //nolint:gosec // the test's file
		t.Error("a second goal up replaced Mailpit's password")
	}
}

// errSchemaMismatch is how the auth service exits on a database without its tables: Better
// Auth's schema check refuses to serve.
var errSchemaMismatch = errors.New("exit status 1 (Database schema mismatch: missing tables user, session)")

// fakeInstall is goal up's world in a test: a database whose sign-in migrations are pending
// until something applies them, and fakes of the auth service and goald.
type fakeInstall struct {
	mu       sync.Mutex
	migrated bool
	steps    []string
	// composed is what each compose step was given: its env and its args, by what it starts.
	composed map[string][]string
	// authStarts counts the auth services started; serving is closed once one serves.
	authStarts int
	serving    chan struct{}
	// failFirst is how many auth services stop by themselves after starting, whatever the schema;
	// with runFor, every one stops by itself after serving that long.
	failFirst int
	runFor    time.Duration
	// goaldRuns is how long goald runs once an auth service serves (default: it stops then).
	goaldRuns time.Duration
}

func newFakeInstall() *fakeInstall {
	return &fakeInstall{serving: make(chan struct{}), composed: map[string][]string{}}
}

func (f *fakeInstall) step(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.steps = append(f.steps, name)
}

// migrate applies the pending migrations, as goald does at its start.
func (f *fakeInstall) migrate() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.migrated = true
}

// startAuth is the auth service: it exits at once on a database without its schema (and the
// first failFirst times), else serves until ctx ends.
func (f *fakeInstall) startAuth(ctx context.Context, _ []string) (func() error, error) {
	f.step("auth service")

	f.mu.Lock()
	defer f.mu.Unlock()

	f.authStarts++

	if !f.migrated {
		return func() error { return errSchemaMismatch }, nil
	}

	if f.authStarts <= f.failFirst {
		return func() error { return errors.New("exit status 1") }, nil
	}

	select {
	case <-f.serving:
	default:
		close(f.serving)
	}

	return func() error {
		if f.runFor > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(f.runFor):
				return errors.New("exit status 1")
			}
		}

		<-ctx.Done()

		return errors.New("signal: terminated")
	}, nil
}

// platform is goal up with every step a fake. goald migrates at its start, as the real one does,
// then runs until an auth service serves (and goaldRuns more) or ten minutes have passed.
func (f *fakeInstall) platform() platform {
	return platform{
		compose: func(_ context.Context, _ *printer, what string, env []string, args ...string) error {
			f.step("compose " + what)

			f.mu.Lock()
			f.composed[what] = slices.Concat(env, args)
			f.mu.Unlock()

			return nil
		},
		bootstrap: func(context.Context, func(string) string) error {
			f.step("bootstrap check")

			return nil
		},
		dbInit: func(context.Context, *printer, func(string) string) error {
			f.step("db init")

			return nil
		},
		dbMigrate: func(context.Context, *printer, func(string) string) error {
			f.step("db migrate")
			f.migrate()

			return nil
		},
		storageInit: func(context.Context, *printer, func(string) string) error {
			f.step("storage init")

			return nil
		},
		goaldPath: func() (string, error) { return "goald", nil },
		startAuth: f.startAuth,
		runGoald: func(ctx context.Context, _ *printer, _ string, _ []string) error {
			f.step("goald")
			f.migrate()

			select {
			case <-f.serving:
				time.Sleep(f.goaldRuns)
			case <-time.After(10 * time.Minute):
			case <-ctx.Done():
			}

			return nil
		},
	}
}

// startUp runs goal up with sign-in on f's platform and returns what it told the terminal.
func startUp(t *testing.T, f *fakeInstall) string {
	t.Helper()

	var notices bytes.Buffer

	dir := t.TempDir()
	out := &printer{w: io.Discard, e: &notices}

	if err := f.platform().start(t.Context(), out, func(n string) string { return map[string]string{xdgConfigHome: dir}[n] }, true); err != nil {
		t.Fatal(err)
	}

	return notices.String()
}

// The upgrade from migration 39 to 49 under GOAL_AUTH=session: the auth service's schema
// (00040 on) does not exist until the migrations are applied. goal up applies them before it
// starts the auth service, so its first start serves and goal up never reports it stopped.
func TestUp_MigratesBeforeTheAuthServiceStarts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		f := newFakeInstall()
		notices := startUp(t, f)

		want := []string{
			"compose PostgreSQL and RustFS", "bootstrap check", "db init", "db migrate", "storage init", "compose the egress proxy", "compose Mailpit",
			"auth service", "goald",
		}
		if !slices.Equal(f.steps, want) {
			t.Errorf("goal up took %v; want %v", f.steps, want)
		}

		if notices != "" {
			t.Errorf("goal up said %q; want the auth service never to stop", notices)
		}
	})
}

// An auth service that stops by itself while goald runs is started again after a backoff that
// doubles, and goal up says so each time; signing in comes back without a second goal up.
func TestUp_RestartsAnAuthServiceThatStops(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		f := newFakeInstall()
		f.failFirst = 2

		began := time.Now()
		notices := startUp(t, f)

		if f.authStarts != 3 {
			t.Errorf("auth service started %d times; want 3 (two stops, then it serves)", f.authStarts)
		}

		for _, w := range []string{"restarting it in 1s (1 of 5)", "restarting it in 2s (2 of 5)"} {
			if !strings.Contains(notices, w) {
				t.Errorf("goal up said %q; want %q", notices, w)
			}
		}

		if waited := time.Since(began); waited != 3*time.Second {
			t.Errorf("signing in came back after %s; want 3s of backoff", waited)
		}
	})
}

// The restarts are bounded: after authRestarts in a row goal up stops trying and says what is
// left (goald goes on).
func TestUp_StopsRestartingAnAuthServiceThatKeepsStopping(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		f := newFakeInstall()
		f.failFirst = 1000

		notices := startUp(t, f)

		if f.authStarts != authRestarts+1 {
			t.Errorf("auth service started %d times; want %d", f.authStarts, authRestarts+1)
		}

		if !strings.Contains(notices, "restarting it in 16s (5 of 5)") || !strings.Contains(notices, "6 times in a row") ||
			!strings.Contains(notices, "goald goes on") {
			t.Errorf("goal up said %q; want five restarts, then that it gave up and goald goes on", notices)
		}
	})
}

// A run that lasted authSteady starts the count again: an auth service that stops now and then
// over a long day is always restarted, after the first backoff.
func TestUp_SteadyRunsResetTheAuthServicesRestarts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		f := newFakeInstall()
		f.runFor, f.goaldRuns = 2*time.Minute, 30*time.Minute

		notices := startUp(t, f)

		if f.authStarts <= authRestarts+1 {
			t.Errorf("auth service started %d times in 30 minutes; want it restarted after every stop", f.authStarts)
		}

		if strings.Contains(notices, "in a row") || strings.Contains(notices, "in 2s") {
			t.Errorf("goal up said %q; want every restart after the first backoff", notices)
		}
	})
}

// The egress proxy forwards boxes' gateway calls to goald on this machine at the listener goald
// serves its gateway on, which a container names host.docker.internal.
func TestUp_EgressGatewayIsGoaldsGatewayListener(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		env  map[string]string
		want string
	}{
		"the default":                      {nil, "http://host.docker.internal:8080"},
		"the main listener":                {map[string]string{listenEnv: "127.0.0.1:9080"}, "http://host.docker.internal:9080"},
		"a gateway listener":               {map[string]string{listenEnv: "127.0.0.1:9080", "GOAL_GATEWAY_LISTEN": ":8090"}, "http://host.docker.internal:8090"},
		"a gateway listener on the bridge": {map[string]string{"GOAL_GATEWAY_LISTEN": "172.17.0.1:8090"}, "http://172.17.0.1:8090"},
		"GOAL_GATEWAY_URL":                 {map[string]string{"GOAL_GATEWAY_URL": "https://localhost:8443/mcp"}, "https://host.docker.internal:8443"},
		"GOAL_EGRESS_GATEWAY":              {map[string]string{"GOAL_EGRESS_GATEWAY": "http://gw.example.com", listenEnv: "127.0.0.1:9080"}, "http://gw.example.com"},
	} {
		if got := egressGateway(func(n string) string { return tt.env[n] }); got != tt.want {
			t.Errorf("%s: egressGateway = %q; want %q", name, got, tt.want)
		}
	}
}

// goal up starts the egress proxy toward this machine's goald before goald, so its first run
// has a way out.
func TestUp_StartsTheEgressProxyBeforeGoald(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		f := newFakeInstall()
		_ = startUp(t, f)

		if got := f.composed["the egress proxy"]; !slices.Equal(got, []string{"GOAL_EGRESS_GATEWAY=http://host.docker.internal:8080", "goal-egress"}) {
			t.Errorf("egress proxy started with %v; want goal-egress reaching goald on this machine", got)
		}

		if i, j := slices.Index(f.steps, "compose the egress proxy"), slices.Index(f.steps, "goald"); i < 0 || i > j {
			t.Errorf("goal up took %v; want the egress proxy before goald", f.steps)
		}
	})
}

// goal up --compose prepares the database and the bucket as goal up does, then runs goald, the
// auth service, Mailpit and the egress proxy in compose, built from this checkout, the proxy
// reaching goald's service and goald sharing a proxy secret with the auth service; it starts no
// process of its own.
func TestUp_ComposeRunsTheStackInCompose(t *testing.T) {
	t.Parallel()

	f := newFakeInstall()
	dir := t.TempDir()
	out := &printer{w: io.Discard, e: io.Discard}

	if err := f.platform().startStack(t.Context(), out, func(n string) string { return map[string]string{xdgConfigHome: dir}[n] }); err != nil {
		t.Fatal(err)
	}

	stack := "Mailpit, the egress proxy, goald and the auth service"
	if want := []string{"compose PostgreSQL and RustFS", "bootstrap check", "db init", "db migrate", "storage init", "compose " + stack}; !slices.Equal(f.steps, want) {
		t.Errorf("goal up --compose took %v; want %v", f.steps, want)
	}

	got := f.composed[stack]
	if len(got) != 7 || got[0] != "GOAL_EGRESS_GATEWAY=http://goald:8090" || !regexp.MustCompile(`^GOAL_AUTH_PROXY_SECRET=[0-9a-f]{64}$`).MatchString(got[1]) ||
		!slices.Equal(got[2:], []string{"--build", "mailpit", "goal-egress", "goald", "auth"}) {
		t.Errorf("the stack started with %v; want the proxy toward goald's service, a proxy secret and the four services built", got)
	}
}

// goald listens off loopback in its container, so goal up --compose signs people in.
func TestUp_ComposeRefusesTheSingleLocalUser(t *testing.T) {
	t.Parallel()

	getenv := func(n string) string { return map[string]string{"GOAL_AUTH": "none"}[n] }

	err := up(t.Context(), &printer{w: io.Discard, e: io.Discard}, getenv, []string{"--compose"})
	if err == nil || !strings.Contains(err.Error(), "GOAL_AUTH=session") {
		t.Errorf("goal up --compose under GOAL_AUTH=none = %v; want a refusal naming GOAL_AUTH=session", err)
	}
}
