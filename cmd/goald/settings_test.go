package main

import (
	"maps"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/adapters/envsecrets"
	"github.com/byFrederick/goal/internal/adapters/s3store"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/config"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// A public origin and an off-loopback gateway listener the tests configure.
const (
	testPublicURL = "https://goal.example.com"
	testGateway   = "10.0.0.5:8090"
)

func TestSettings_Defaults(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(map[string]string{
		"GOAL_PG_PASSWORD": "superuser-pw", "GOAL_PG_OWNER_PASSWORD": "owner-pw", "GOAL_PG_APP_PASSWORD": "app-pw",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if s.Listen != "127.0.0.1:8080" || s.GatewayListen != "" || s.GatewayURL != "http://127.0.0.1:8080/mcp" || s.GatewayKeyRef != "env:GOAL_GATEWAY_KEY" {
		t.Errorf("listen %s, gateway listen %q, gateway %s, key %s; want the loopback defaults on one listener",
			s.Listen, s.GatewayListen, s.GatewayURL, s.GatewayKeyRef)
	}

	// goald never connects as compose's superuser: migrations run as goal_owner, the rest as goal_app.
	if s.MigrateDatabaseURL != "postgres://goal_owner:owner-pw@127.0.0.1:5432/goal?sslmode=disable" { //nolint:gosec // a test password
		t.Errorf("migrate database url = %s, want the compose database as goal_owner", s.MigrateDatabaseURL)
	}

	if s.DatabaseURL != "postgres://goal_app:app-pw@127.0.0.1:5432/goal?sslmode=disable" { //nolint:gosec // a test password
		t.Errorf("database url = %s, want the compose database as goal_app", s.DatabaseURL)
	}

	if s.OrgRetention != 7*24*time.Hour {
		t.Errorf("org retention = %s, want 7 days", s.OrgRetention)
	}

	if s.DefinitionCacheBytes != 64<<20 || !s.LegacyImport {
		t.Errorf("definition cache %d bytes, legacy import %v; want 64 MiB and on", s.DefinitionCacheBytes, s.LegacyImport)
	}

	want := map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "env:CLAUDE_CODE_OAUTH_TOKEN"} //nolint:gosec // a reference, not a secret
	if !maps.Equal(s.TaskEnv, want) {
		t.Errorf("task env = %v, want %v", s.TaskEnv, want)
	}
}

// Agents reach the model API and the gateway through the egress proxy, nothing else, unless
// GOAL_AGENT_EGRESS adds hosts.
func TestSettings_Egress(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}

	if e := s.Egress; e.Name != "goal-egress" || e.Image != "ghcr.io/byfrederick/goal-egress:0.1.0" || e.Gateway != s.GatewayURL ||
		!slices.Equal(e.AgentHosts, []string{"api.anthropic.com"}) || len(e.Trusted) != 0 {
		t.Errorf("egress = %+v; want the default proxy serving the gateway and the model API to agents", e)
	}

	s, err = settingsFrom(envOf(map[string]string{agentEgressEnv: " api.anthropic.com, proxy.golang.org:443 ,", egressNameEnv: "goal-egress-dev"}))
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(s.Egress.AgentHosts, []string{"api.anthropic.com", "proxy.golang.org:443"}) || s.Egress.Name != "goal-egress-dev" {
		t.Errorf("egress = %+v; want GOAL_AGENT_EGRESS's hosts and GOAL_EGRESS_NAME", s.Egress)
	}
}

// Sign-in by default (spec 3 §14, PR 3-16; GOAL_AUTH=none only when asked for), the identity
// service beside goald on loopback, no proxy believed, and the public origin the listen address,
// over http.
func TestSettings_AuthDefaults(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}

	if s.Auth != authSession || s.AuthUpstream.String() != "http://127.0.0.1:8081" || len(s.TrustedProxies) != 0 {
		t.Errorf("auth %s, upstream %s, trusted proxies %v; want session, http://127.0.0.1:8081 and none", s.Auth, s.AuthUpstream, s.TrustedProxies)
	}

	none, err := settingsFrom(envOf(map[string]string{"GOAL_AUTH": "none"}))
	if err != nil || none.Auth != authNone {
		t.Errorf("GOAL_AUTH=none = %s, %v; want the local user when asked for", none.Auth, err)
	}

	if s.PublicURL != "http://127.0.0.1:8080" || s.strictTransport() {
		t.Errorf("public URL %s (strict transport %v), want http://127.0.0.1:8080 without", s.PublicURL, s.strictTransport())
	}
}

// The identity service's address and the proxies goald believes, as configured: a loopback
// upstream by any loopback name, CIDRs masked, single addresses as one-address prefixes.
func TestSettings_AuthProxy(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(map[string]string{
		upstreamEnv: "http://[::1]:9081/", proxiesEnv: " 10.1.2.3/8, 192.0.2.7 ,2001:db8::/32,::ffff:198.51.100.1",
		publicURLEnv: testPublicURL,
	}))
	if err != nil {
		t.Fatal(err)
	}

	if s.AuthUpstream.String() != "http://[::1]:9081" {
		t.Errorf("upstream = %s, want http://[::1]:9081", s.AuthUpstream)
	}

	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.0.2.7/32"),
		netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("198.51.100.1/32"),
	}
	if !slices.Equal(s.TrustedProxies, want) {
		t.Errorf("trusted proxies = %v, want %v", s.TrustedProxies, want)
	}

	if !s.strictTransport() {
		t.Error("an https public URL is served without Strict-Transport-Security")
	}
}

// The editor's models: the current Claude models by default, the first being the default;
// GOAL_MODELS replaces them, a label after = or one made from the id.
func TestSettings_Models(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}

	want := []builder.Model{
		{ID: "claude-opus-5-5", Label: "Claude Opus 5.5"},
		{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5"},
		{ID: "claude-haiku-5-5", Label: "Claude Haiku 5.5"},
	}
	if !slices.Equal(s.Models, want) {
		t.Errorf("default models = %+v, want %+v", s.Models, want)
	}

	s, err = settingsFrom(envOf(map[string]string{modelsEnv: " claude-sonnet-5-5 , claude-fable-5-1=Fable (preview)"}))
	if err != nil {
		t.Fatal(err)
	}

	want = []builder.Model{{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5"}, {ID: "claude-fable-5-1", Label: "Fable (preview)"}}
	if !slices.Equal(s.Models, want) {
		t.Errorf("models = %+v, want %+v", s.Models, want)
	}
}

func TestSettings_RefusesUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "public listen address", env: map[string]string{listenEnv: "0.0.0.0:8080"}, want: "loopback"},
		{name: "API key for tasks", env: map[string]string{taskEnvEnv: "ANTHROPIC_API_KEY=env:ANTHROPIC_API_KEY"}, want: "never reach"},
		{name: "GitHub token for tasks", env: map[string]string{taskEnvEnv: "goal_github_token=env:X"}, want: "never reach"},
		{name: "literal secret for tasks", env: map[string]string{taskEnvEnv: "CLAUDE_CODE_OAUTH_TOKEN=sk-literal"}, want: "NAME=env:REF"},
		{name: "bad duration", env: map[string]string{"GOAL_TICK_EVERY": "-1s"}, want: "GOAL_TICK_EVERY"},
		{name: "negative run cap", env: map[string]string{"GOAL_MAX_RUNS": "-2"}, want: "GOAL_MAX_RUNS"},
		{name: "no retention", env: map[string]string{"GOAL_ORG_RETENTION": "0d"}, want: "GOAL_ORG_RETENTION"},
		{name: "empty definition cache", env: map[string]string{"GOAL_DEFINITION_CACHE_BYTES": "0"}, want: "GOAL_DEFINITION_CACHE_BYTES"},
		{name: "unclear legacy import", env: map[string]string{"GOAL_LEGACY_IMPORT": "maybe"}, want: "GOAL_LEGACY_IMPORT"},
		{name: "GitHub API in clear text", env: map[string]string{"GOAL_GITHUB_API_URL": "http://api.example.com"}, want: "must be https"},
		{name: "GitHub web in clear text", env: map[string]string{githubWebEnv: "http://ghe.example.com"}, want: githubWebEnv},
		{name: "GitHub web with a path", env: map[string]string{githubWebEnv: "https://ghe.example.com/acme"}, want: githubWebEnv},
		{name: "GitHub web with a user", env: map[string]string{githubWebEnv: "https://u@ghe.example.com"}, want: githubWebEnv},
		{name: "not a model id", env: map[string]string{modelsEnv: "claude-opus-5-5,gpt-5"}, want: "not a model id"},
		{name: "a model twice", env: map[string]string{modelsEnv: "claude-opus-5-5,claude-opus-5-5"}, want: "twice"},
		{name: "an empty model", env: map[string]string{modelsEnv: "claude-opus-5-5,"}, want: "not a model id"},
		{name: "public URL in clear text", env: map[string]string{publicURLEnv: "http://goal.example.com"}, want: publicURLEnv},
		{name: "public URL without a scheme", env: map[string]string{publicURLEnv: "goal.example.com"}, want: publicURLEnv},
		{name: "public URL with a path", env: map[string]string{publicURLEnv: "https://example.com/goal"}, want: publicURLEnv},
		{name: "gateway listener on the main one", env: map[string]string{gatewayEnv: defaultListen}, want: "another address"},
		{name: "gateway listener on the main one by name", env: map[string]string{gatewayEnv: "localhost:8080"}, want: "another address"},
		{name: "gateway listener on every interface's main port", env: map[string]string{gatewayEnv: ":8080"}, want: "another address"},
		{name: "gateway listener without a port", env: map[string]string{gatewayEnv: "gateway.example.com"}, want: "host:port"},
		{name: "an agent egress URL", env: map[string]string{agentEgressEnv: "https://api.anthropic.com/v1"}, want: agentEgressEnv},
		{name: "an egress name with a path", env: map[string]string{egressNameEnv: "../goal"}, want: egressNameEnv},
		{
			name: "gateway URL on the main listener beside a gateway listener",
			env:  map[string]string{gatewayEnv: "127.0.0.1:8090", gatewayURLEnv: "http://host.docker.internal:8080/mcp"},
			want: "names GOAL_LISTEN",
		},
		// Fail closed: no mode goald cannot enforce, and no unknown one.
		{
			name: "the local user off loopback", env: map[string]string{authEnv: "none", listenEnv: "10.0.0.5:8080", gatewayEnv: testGateway, publicURLEnv: testPublicURL},
			want: "GOAL_AUTH=none",
		},
		{
			name: "sign-in off loopback without a public URL", env: map[string]string{authEnv: string(authSession), listenEnv: "10.0.0.5:8080", gatewayEnv: testGateway},
			want: publicURLEnv + " is required",
		},
		{
			name: "sign-in off loopback without a gateway listener", env: map[string]string{authEnv: string(authSession), listenEnv: "0.0.0.0:8080", publicURLEnv: testPublicURL},
			want: gatewayEnv + " is required",
		},
		{
			name: "sign-in off loopback in clear text",
			env:  map[string]string{authEnv: string(authSession), listenEnv: "10.0.0.5:8080", gatewayEnv: testGateway, publicURLEnv: "http://goal.example.com"},
			want: "must be https",
		},
		{name: "Google neither on nor off", env: map[string]string{googleEnv: "yes"}, want: googleEnv},
		{name: "unknown auth mode", env: map[string]string{authEnv: "off"}, want: authEnv},
		{name: "auth service off this machine", env: map[string]string{upstreamEnv: "http://auth.example.com:8081"}, want: upstreamEnv},
		{name: "auth service over https", env: map[string]string{upstreamEnv: "https://127.0.0.1:8081"}, want: upstreamEnv},
		{name: "auth service with a path", env: map[string]string{upstreamEnv: "http://127.0.0.1:8081/api/auth"}, want: upstreamEnv},
		{name: "auth service with a user", env: map[string]string{upstreamEnv: "http://u:p@127.0.0.1:8081"}, want: upstreamEnv}, //nolint:gosec // a test URL
		{name: "trusted proxy not a CIDR", env: map[string]string{proxiesEnv: "10.0.0.0/8,proxy.example.com"}, want: proxiesEnv},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := settingsFrom(envOf(tt.env)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("settings error = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

// GOAL_GATEWAY_URL follows the gateway's own listener, so agents are given only its address; a
// wildcard host becomes loopback, which the Docker runner rewrites to the host's name, and an
// explicit URL elsewhere is kept.
func TestSettings_GatewayURLFollowsTheGatewayListener(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "one listener", want: "http://127.0.0.1:8080/mcp"},
		{name: "one listener elsewhere on loopback", env: map[string]string{listenEnv: "127.0.0.1:9090"}, want: "http://127.0.0.1:9090/mcp"},
		{name: "gateway listener on loopback", env: map[string]string{gatewayEnv: "127.0.0.1:8090"}, want: "http://127.0.0.1:8090/mcp"},
		{name: "gateway listener on the Docker bridge", env: map[string]string{gatewayEnv: "172.17.0.1:8090"}, want: "http://172.17.0.1:8090/mcp"},
		{name: "gateway listener on every interface", env: map[string]string{gatewayEnv: ":8090"}, want: "http://127.0.0.1:8090/mcp"},
		{name: "gateway listener on every IPv4 interface", env: map[string]string{gatewayEnv: "0.0.0.0:8090"}, want: "http://127.0.0.1:8090/mcp"},
		{
			name: "explicit gateway URL", env: map[string]string{gatewayEnv: "0.0.0.0:8090", gatewayURLEnv: "http://gateway.example.com:8090/mcp"},
			want: "http://gateway.example.com:8090/mcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := settingsFrom(envOf(tt.env))
			if err != nil {
				t.Fatal(err)
			}

			if s.GatewayURL != tt.want {
				t.Errorf("gateway URL = %s, want %s", s.GatewayURL, tt.want)
			}
		})
	}
}

// Under GOAL_AUTH=session goald may listen off loopback, behind its public URL, with the gateway
// on a listener of its own; it then answers only the names it listens and is opened by.
func TestSettings_SignInMayListenOffLoopback(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(map[string]string{
		authEnv: string(authSession), listenEnv: "0.0.0.0:8080", gatewayEnv: "172.17.0.1:8090", publicURLEnv: testPublicURL,
		googleEnv: "on",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if s.Auth != authSession || !s.Google || s.GatewayURL != "http://172.17.0.1:8090/mcp" || !s.strictTransport() {
		t.Errorf("settings = %+v; want session with Google, the gateway's own URL and strict transport", s)
	}

	if got, want := s.hosts(), []string{"0.0.0.0", "goal.example.com"}; !slices.Equal(got, want) {
		t.Errorf("hosts = %v, want %v (no loopback names off loopback)", got, want)
	}
}

// Phase 3's listener rule: off loopback the gateway needs a listener of its own.
func TestGatewayListen_IsRequiredOffLoopback(t *testing.T) {
	t.Parallel()

	const offLoopback = "10.0.0.5:8080"

	tests := []struct {
		name, listen, gateway string
		ok                    bool
	}{
		{name: "loopback, one listener", listen: defaultListen, ok: true},
		{name: "loopback, gateway listener", listen: defaultListen, gateway: "[::1]:8090", ok: true},
		{name: "off loopback, one listener", listen: offLoopback},
		{name: "off loopback, gateway listener", listen: offLoopback, gateway: testGateway, ok: true},
		{name: "off loopback, gateway on the main listener", listen: offLoopback, gateway: offLoopback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := gatewayListen(tt.listen, tt.gateway); (err == nil) != tt.ok {
				t.Errorf("gatewayListen(%q, %q) = %v, want ok %v", tt.listen, tt.gateway, err, tt.ok)
			}
		})
	}
}

// goald answers requests naming the loopback names, its listen host and its public URL's host;
// the Host check refuses every other (DNS rebinding).
func TestSettings_Hosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{name: "defaults", want: []string{"localhost", "127.0.0.1", "::1"}},
		{
			name: "listen host and public URL",
			env:  map[string]string{listenEnv: "127.0.0.2:9090", publicURLEnv: testPublicURL},
			want: []string{"localhost", "127.0.0.1", "::1", "127.0.0.2", "goal.example.com"},
		},
		{
			name: "loopback public URL in clear text", env: map[string]string{publicURLEnv: "http://localhost:8080/"},
			want: []string{"localhost", "127.0.0.1", "::1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := settingsFrom(envOf(tt.env))
			if err != nil {
				t.Fatal(err)
			}

			if got := s.hosts(); !slices.Equal(got, tt.want) {
				t.Errorf("hosts = %v, want %v", got, tt.want)
			}
		})
	}
}

// Every variable goald reads is one a factory definition may not reference, so no definition can
// hand goald's own settings or secrets to an agent or a connection.
func TestSettings_EveryVariableGoaldReadsIsReserved(t *testing.T) {
	t.Parallel()

	var read []string

	getenv := func(name string) string {
		read = append(read, name)

		if name == "GOAL_S3_BUCKET" {
			return "goal-test"
		}

		return ""
	}

	s, err := settingsFrom(getenv)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s3store.ConfigFromEnv(getenv); err != nil {
		t.Fatal(err)
	}

	reserved := s.reserved()
	for _, name := range append(read, "GOAL_GATEWAY_KEY") {
		if !slices.Contains(reserved, name) {
			t.Errorf("goald reads %s, but definitions may reference it", name)
		}
	}

	custom, err := settingsFrom(envOf(map[string]string{"GOAL_GATEWAY_KEY_REF": "env:ACME_SIGNING_KEY"}))
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(custom.reserved(), "ACME_SIGNING_KEY") {
		t.Errorf("reserved %v, want the variable the gateway key reference names", custom.reserved())
	}
}

// The install's repository token goes only to its GitHub: github.com, or the GitHub Enterprise
// host GOAL_GITHUB_WEB_URL names. Clones, merges and the credentials check read that one host, and
// a definition cannot read the setting.
func TestSettings_GitHubWebHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "default", want: "github.com"},
		{name: "GitHub Enterprise", env: map[string]string{githubWebEnv: "https://GHE.example.com/"}, want: "ghe.example.com"},
		{name: "a port", env: map[string]string{githubWebEnv: "https://ghe.example.com:8443"}, want: "ghe.example.com:8443"},
		{name: "a stand-in on this machine", env: map[string]string{githubWebEnv: "http://127.0.0.1:3000"}, want: "127.0.0.1:3000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := settingsFrom(envOf(tt.env))
			if err != nil {
				t.Fatal(err)
			}

			if s.GitHubWebHost != tt.want {
				t.Errorf("GitHub web host = %q, want %q", s.GitHubWebHost, tt.want)
			}

			if !slices.Contains(s.reserved(), githubWebEnv) {
				t.Errorf("definitions may reference %s", githubWebEnv)
			}

			if got := s.credentials(envsecrets.New(nil)).RepositoryHosts; !slices.Equal(got, []string{tt.want}) {
				t.Errorf("credentials check allows repositories on %v, want only %s", got, tt.want)
			}
		})
	}
}

// No factory definition can hand an agent the identity settings and secrets, whether goald reads
// them or only the identity service, the CLI or `goal db init` does, even when goald was started
// with them in its environment (spec 3, §5.6).
func TestSettings_ReservedIncludesAuthVariables(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"GOAL_AUTH", "GOAL_PUBLIC_URL", "GOAL_AUTH_UPSTREAM", "GOAL_TRUSTED_PROXIES", "GOAL_GATEWAY_LISTEN", "GOAL_AUTH_GOOGLE",
		"GOAL_AUTH_LISTEN", "GOAL_AUTH_DATABASE_URL", "BETTER_AUTH_SECRETS", "BETTER_AUTH_SECRET", "BETTER_AUTH_URL",
		"GOAL_BOOTSTRAP_OWNER_EMAIL", "GOAL_SMTP_URL", "GOAL_SMTP_FROM", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET",
		"GOAL_PG_AUTH_PASSWORD", "GOAL_API_KEY", "GOAL_DATABASE_ADMIN_URL",
	} {
		if !slices.Contains(s.reserved(), name) {
			t.Errorf("definitions may reference %s", name)
		}
	}
}

// The platform's secrets are reserved too: those that never reach a task and the variables the
// tasks' model access is read from (GOAL_TASK_ENV's references). The repository token is not
// reserved but kept to the repository and GitHub's MCP server.
func TestSettings_ReservesPlatformSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		env      map[string]string
		reserved []string
	}{
		{name: "defaults", reserved: []string{"ANTHROPIC_API_KEY", "GOAL_GATEWAY_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}},
		{
			name:     "custom task env",
			env:      map[string]string{taskEnvEnv: "CLAUDE_CODE_OAUTH_TOKEN=env:ACME_MODEL_TOKEN,ACME_FLAG=env:ACME_FLAG_SOURCE"},
			reserved: []string{"ANTHROPIC_API_KEY", "ACME_MODEL_TOKEN", "ACME_FLAG_SOURCE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := settingsFrom(envOf(tt.env))
			if err != nil {
				t.Fatal(err)
			}

			// Authoring and the gateway apply one policy.
			got := s.secretPolicy()
			if check := s.credentials(envsecrets.New(func(string) (string, bool) { return "", false })); !reflect.DeepEqual(check.Policy, got) {
				t.Errorf("the credentials check's policy %+v is not the gateway's %+v", check.Policy, got)
			}

			for _, name := range tt.reserved {
				if !slices.Contains(got.Reserved, name) {
					t.Errorf("reserved %v, want %s among them", got.Reserved, name)
				}
			}

			if slices.Contains(got.Reserved, repositoryToken) || !slices.Equal(got.RepositoryOnly, []string{repositoryToken}) ||
				!slices.Equal(got.ConnectionHosts, []string{githubMCPHost}) {
				t.Errorf("reserved %v, repository only %v to %v; want %s kept to the repository and %s",
					got.Reserved, got.RepositoryOnly, got.ConnectionHosts, repositoryToken, githubMCPHost)
			}
		})
	}
}

// The credentials check goald authors definitions with refuses a reference to one of its own
// settings with env-reserved, even though the variable is set, and passes another set variable.
func TestSettings_CredentialsRefuseGoaldsOwnVariables(t *testing.T) {
	t.Parallel()

	env := map[string]string{"GOAL_DATABASE_URL": "postgres://app@db.example.com/goal", "ACME_GIT_TOKEN": "t0ken"}

	s, err := settingsFrom(envOf(env))
	if err != nil {
		t.Fatal(err)
	}

	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	findings, err := s.credentials(envsecrets.New(lookup)).Check(t.Context(), "", []config.SecretUse{
		{Path: "runtime.repository.token", Ref: "env:GOAL_DATABASE_URL"},
		{Path: "spec.connections[0].headers.Authorization", Ref: "env:ACME_GIT_TOKEN"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(findings) != 1 || findings[0].Code != authoring.CodeEnvReserved || findings[0].Use.Ref != "env:GOAL_DATABASE_URL" {
		t.Errorf("findings = %+v; want only env:GOAL_DATABASE_URL refused with %s", findings, authoring.CodeEnvReserved)
	}
}
