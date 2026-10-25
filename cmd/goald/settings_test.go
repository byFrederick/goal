package main

import (
	"maps"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/builder"
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

	if s.DefinitionCacheBytes != 64<<20 {
		t.Errorf("definition cache %d bytes; want 64 MiB", s.DefinitionCacheBytes)
	}
}

// Without GOAL_EMBED_URL the knowledge base searches by keywords; with it the model defaults to
// Workers AI's bge-m3, and two workers sync.
func TestSettings_Knowledge(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(nil))
	if err != nil || s.Embed.URL != "" || s.KBWorkers != 2 || s.UploadMaxBytes != 25<<20 {
		t.Fatalf("defaults = %+v, %d, %d, %v", s.Embed, s.KBWorkers, s.UploadMaxBytes, err)
	}

	s, err = settingsFrom(envOf(map[string]string{
		embedURLEnv: "https://api.cloudflare.com/client/v4/accounts/acme/ai/v1", embedKeyEnv: "k", kbWorkersEnv: "0", uploadMaxEnv: "1048576",
	}))
	if err != nil || s.Embed.Model != "@cf/baai/bge-m3" || s.Embed.Key != "k" || s.KBWorkers != 0 || s.UploadMaxBytes != 1<<20 {
		t.Errorf("with an embedding API = %+v, %d, %d, %v", s.Embed, s.KBWorkers, s.UploadMaxBytes, err)
	}

	for _, bad := range []string{"0", "-1", "25MiB"} {
		if _, err := settingsFrom(envOf(map[string]string{uploadMaxEnv: bad})); err == nil || !strings.Contains(err.Error(), uploadMaxEnv) {
			t.Errorf("%s=%s: %v; want an error naming it", uploadMaxEnv, bad, err)
		}
	}
}

// Containers run on compose.yaml's agents network unless GOAL_SANDBOX_NETWORK names another;
// the egress and run folder settings of earlier releases are ignored, and listed for goald's
// warning.
func TestSettings_SandboxNetwork(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}

	if s.SandboxNetwork != "goal_agents" || len(s.Ignored) != 0 || s.AgentImage != "ghcr.io/byfrederick/goal-agent:0.2.0" {
		t.Errorf("network %q, ignored %v, image %q; want goal_agents, nothing ignored and goal-agent 0.2.0", s.SandboxNetwork, s.Ignored, s.AgentImage)
	}

	s, err = settingsFrom(envOf(map[string]string{
		sandboxNetworkEnv: "goal-dev_agents", "GOAL_AGENT_EGRESS": "api.anthropic.com, proxy.golang.org", "GOAL_EGRESS_NAME": "../goal",
		"GOAL_EGRESS_IMAGE": " ", "GOAL_RUN_DIR": "/tmp/goal-stack/runs", "GOAL_DEFAULT_AGENT_IMAGE": "ghcr.io/acme/agent:1",
		agentImageEnv: "ghcr.io/acme/goal-agent:2",
	}))
	if err != nil {
		t.Fatalf("removed settings refused goald's start: %v", err)
	}

	wantIgnored := []string{"GOAL_AGENT_EGRESS", "GOAL_EGRESS_NAME", "GOAL_RUN_DIR", "GOAL_DEFAULT_AGENT_IMAGE"}
	if s.SandboxNetwork != "goal-dev_agents" || !slices.Equal(ignored(s), wantIgnored) || s.AgentImage != "ghcr.io/acme/goal-agent:2" {
		t.Errorf("network %q, ignored %v, image %q; want GOAL_SANDBOX_NETWORK's, %v and GOAL_AGENT_IMAGE's", s.SandboxNetwork, ignored(s), s.AgentImage, wantIgnored)
	}
}

// ignored names the removed settings s ignores.
func ignored(s settings) []string {
	out := make([]string, 0, len(s.Ignored))
	for _, r := range s.Ignored {
		out = append(out, r.name)
	}

	return out
}

// Since the cutover to connections (spec 4 §14, step 5) factories sign in with their
// organization's connections: goald's model token, task env and GitHub settings are ignored, not
// refused, so an install upgrades on its old environment, and goald's warning says what replaced
// each.
func TestSettings_TheCutoverIgnoresTheEnvironmentCredentials(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(map[string]string{ //nolint:gosec // made-up values
		"GOAL_TASK_ENV": "ANTHROPIC_API_KEY=env:ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN": "sk-ant-oat-never-read",
		"GOAL_GITHUB_TOKEN": "ghp_never-read", "GOAL_GITHUB_API_URL": "http://api.example.com", "GOAL_GITHUB_WEB_URL": "https://u@ghe.example.com/x",
	}))
	if err != nil {
		t.Fatalf("an install's old environment refused goald's start: %v", err)
	}

	want := map[string]string{
		"GOAL_TASK_ENV": "runtime.modelConnection", "CLAUDE_CODE_OAUTH_TOKEN": "goal connection import-env",
		"GOAL_GITHUB_TOKEN": "github connection", "GOAL_GITHUB_API_URL": "host gives its API", "GOAL_GITHUB_WEB_URL": "host",
	}

	if got := ignored(s); len(got) != len(want) {
		t.Fatalf("ignored %v; want %v", got, slices.Sorted(maps.Keys(want)))
	}

	for _, r := range s.Ignored {
		if !strings.Contains(r.instead, want[r.name]) || strings.Contains(r.instead, "never-read") {
			t.Errorf("%s is replaced by %q; want it to name %q", r.name, r.instead, want[r.name])
		}
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
		{name: "bad duration", env: map[string]string{"GOAL_TICK_EVERY": "-1s"}, want: "GOAL_TICK_EVERY"},
		{name: "negative run cap", env: map[string]string{"GOAL_MAX_RUNS": "-2"}, want: "GOAL_MAX_RUNS"},
		{name: "no retention", env: map[string]string{"GOAL_ORG_RETENTION": "0d"}, want: "GOAL_ORG_RETENTION"},
		{name: "empty definition cache", env: map[string]string{"GOAL_DEFINITION_CACHE_BYTES": "0"}, want: "GOAL_DEFINITION_CACHE_BYTES"},
		{name: "not a model id", env: map[string]string{modelsEnv: "claude-opus-5-5,gpt-5"}, want: "not a model id"},
		{name: "a model twice", env: map[string]string{modelsEnv: "claude-opus-5-5,claude-opus-5-5"}, want: "twice"},
		{name: "an empty model", env: map[string]string{modelsEnv: "claude-opus-5-5,"}, want: "not a model id"},
		{name: "public URL in clear text", env: map[string]string{publicURLEnv: "http://goal.example.com"}, want: publicURLEnv},
		{name: "public URL in clear text under a localhost lookalike", env: map[string]string{publicURLEnv: "http://goal.localhost.example.com"}, want: publicURLEnv},
		{name: "public URL in clear text ending like localhost", env: map[string]string{publicURLEnv: "http://evillocalhost"}, want: publicURLEnv},
		{name: "public URL without a scheme", env: map[string]string{publicURLEnv: "goal.example.com"}, want: publicURLEnv},
		{name: "public URL with a path", env: map[string]string{publicURLEnv: "https://example.com/goal"}, want: publicURLEnv},
		{name: "embeddings in clear text", env: map[string]string{embedURLEnv: "http://embeddings.example.com/v1"}, want: embedURLEnv},
		{name: "negative knowledge workers", env: map[string]string{kbWorkersEnv: "-1"}, want: kbWorkersEnv},
		{name: "gateway listener on the main one", env: map[string]string{gatewayEnv: defaultListen}, want: "another address"},
		{name: "gateway listener on the main one by name", env: map[string]string{gatewayEnv: "localhost:8080"}, want: "another address"},
		{name: "gateway listener on every interface's main port", env: map[string]string{gatewayEnv: ":8080"}, want: "another address"},
		{name: "gateway listener without a port", env: map[string]string{gatewayEnv: "gateway.example.com"}, want: "host:port"},
		{name: "a gateway URL without a path", env: map[string]string{gatewayURLEnv: "http://127.0.0.1:9090"}, want: gatewayURLEnv},
		{name: "a gateway URL with a query", env: map[string]string{gatewayURLEnv: "http://127.0.0.1:8080/mcp?x=1"}, want: gatewayURLEnv},
		{name: "a gateway URL not over http", env: map[string]string{gatewayURLEnv: "unix:///mcp"}, want: gatewayURLEnv},
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
		{name: "private upstreams neither true nor false", env: map[string]string{allowPrivateEnv: "yes"}, want: allowPrivateEnv},
		{
			name: "private upstreams off loopback",
			env: map[string]string{
				authEnv: string(authSession), listenEnv: "10.0.0.6:8080", gatewayEnv: testGateway, publicURLEnv: testPublicURL, allowPrivateEnv: "true",
			},
			want: allowPrivateEnv + "=true",
		},
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

// Connection tests reach public addresses only, unless a loopback goald allows its laptop's own
// servers (spec 4 §5.9).
func TestSettings_AllowPrivateUpstreams(t *testing.T) {
	t.Parallel()

	for env, want := range map[string]bool{"": false, "false": false, "true": true} {
		s, err := settingsFrom(envOf(map[string]string{allowPrivateEnv: env}))
		if err != nil || s.AllowPrivateUpstreams != want {
			t.Errorf("%s=%q: AllowPrivateUpstreams %v, %v; want %v", allowPrivateEnv, env, s.AllowPrivateUpstreams, err, want)
		}
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

	loopback := []string{"localhost", "127.0.0.1", "::1"}

	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{name: "defaults", want: loopback},
		{
			name: "listen host and public URL",
			env:  map[string]string{listenEnv: "127.0.0.2:9090", publicURLEnv: testPublicURL},
			want: []string{"localhost", "127.0.0.1", "::1", "127.0.0.2", "goal.example.com"},
		},
		{
			name: "loopback public URL in clear text", env: map[string]string{publicURLEnv: "http://localhost:8080/"},
			want: []string{"localhost", "127.0.0.1", "::1"},
		},
		// A name of its own under .localhost keeps the session cookie from other local web servers
		// (cookies are not port-scoped; review L5); browsers resolve it to this machine.
		{
			name: "a .localhost public URL in clear text", env: map[string]string{publicURLEnv: "http://Goal.localhost:8080"},
			want: append(slices.Clone(loopback), "goal.localhost"),
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
