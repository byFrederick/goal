package main

import (
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/adapters/postgres"
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
