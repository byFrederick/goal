package main

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/adapters/docker"
	"github.com/byFrederick/goal/internal/adapters/masterkey"
	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// settings is goald's configuration, read from the environment (12-factor). Secrets appear here
// only as references (env:NAME), resolved through the Secrets port.
type settings struct {
	// MigrateDatabaseURL connects as goal_owner, which owns the schema, to apply migrations at
	// startup (GOAL_MIGRATE_DATABASE_URL). DatabaseURL connects as goal_app for everything else
	// (GOAL_DATABASE_URL); goald refuses a role that bypasses row-level security. Both default to
	// the compose.yaml database with GOAL_PG_OWNER_PASSWORD and GOAL_PG_APP_PASSWORD, the
	// passwords `goal db init` gives the roles.
	MigrateDatabaseURL string
	DatabaseURL        string
	// Listen is the API's address, and the gateway's without GatewayListen (GOAL_LISTEN, default
	// 127.0.0.1:8080). Under GOAL_AUTH=none it must be a loopback address; under session it may
	// be any, with GOAL_PUBLIC_URL and a gateway listener of its own off loopback.
	Listen string
	// GatewayListen is the MCP gateway's own address (GOAL_GATEWAY_LISTEN, optional). When set,
	// that listener serves only /mcp and /run and the main one answers them with 404; it is required, and
	// different, when Listen is not loopback (phase 3, §5.5). It may be off loopback, such as the
	// Docker bridge's address on Linux: every request there needs a task token.
	GatewayListen string
	// PublicURL is the origin people open goald at (GOAL_PUBLIC_URL, default http://<listen> on
	// loopback, required otherwise; https unless its host is loopback), such as a reverse proxy's
	// name. Its host is the one name
	// besides the loopback names and the listen host that the Host check accepts (api.CheckHost),
	// and over https the UI is served with Strict-Transport-Security.
	PublicURL string
	// Auth is how requests are authenticated (GOAL_AUTH, default none): none, every request is
	// the local user (one developer's laptop, loopback only); session, people sign in and every
	// request's session or API key is checked in the database. Any other value refuses to start.
	Auth authMode
	// Google offers signing in with Google beside email codes (GOAL_AUTH_GOOGLE: on or off,
	// default off); the identity service holds the client and its secret.
	Google bool
	// AuthUpstream is the identity service that /api/auth/* is proxied to under session
	// (GOAL_AUTH_UPSTREAM, default http://127.0.0.1:8081; http on a loopback address only).
	AuthUpstream *url.URL
	// TrustedProxies are the proxies in front of goald whose X-Forwarded-For names the client the
	// identity service is told about (GOAL_TRUSTED_PROXIES: comma-separated CIDRs or addresses,
	// default none).
	TrustedProxies []netip.Prefix
	// GatewayURL is the MCP gateway's URL (GOAL_GATEWAY_URL, default http://<gateway listen or
	// listen>/mcp, a wildcard host becoming 127.0.0.1). Agent containers are given the egress
	// proxy's address at its path, and the proxy reaches goald as GOAL_EGRESS_GATEWAY says
	// (goal up derives it from this listener).
	GatewayURL string
	// GatewayKeyRef names the secret that signs task tokens (GOAL_GATEWAY_KEY_REF, default
	// env:GOAL_GATEWAY_KEY).
	GatewayKeyRef string
	// TaskEnv maps variables of every agent run to secret references (GOAL_TASK_ENV,
	// NAME=ref pairs separated by commas; default CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN).
	TaskEnv map[string]string
	// TickEvery is how often the engine pulls work (GOAL_TICK_EVERY, default 5s); RefreshEvery how
	// often the current definition versions are reconciled with PostgreSQL (GOAL_REFRESH_EVERY,
	// default 30s), a safety net behind the goal_definitions notices that reload them at once.
	TickEvery    time.Duration
	RefreshEvery time.Duration
	// DefinitionCacheBytes bounds the files of the definition versions kept for runs that started
	// on a version no longer current (GOAL_DEFINITION_CACHE_BYTES, default 64 MiB).
	DefinitionCacheBytes int64
	// MaxRuns caps the agent runs this goald has in flight at once (GOAL_MAX_RUNS, default 0:
	// no cap). With a cap, claims are shared out fairly across organizations.
	MaxRuns int
	// OrgRetention is how long a deleted organization is kept before it is purged with its rows
	// and bucket objects (GOAL_ORG_RETENTION, default 7d; a Go duration or a number of days).
	OrgRetention time.Duration
	// GitHubAPIURL is the GitHub REST API goald merges pull requests through
	// (GOAL_GITHUB_API_URL, default https://api.github.com; http only on a loopback address).
	GitHubAPIURL string
	// GitHubWebHost is the host (host[:port], lower case) of the GitHub that holds the factories'
	// repositories and pull requests (GOAL_GITHUB_WEB_URL, an origin: default https://github.com,
	// a GitHub Enterprise Server's own; http only on a loopback address). A definition chooses its
	// repository's URL but the token is the install's, so clones reach only this host and merges
	// only its pull requests.
	GitHubWebHost string
	// Models are the models the factory editor offers (GOAL_MODELS: comma-separated model ids,
	// each optionally id=Label; the first is the default; default defaultModels).
	Models []builder.Model
	// LogLevel (GOAL_LOG_LEVEL: debug, info, warn, error) and LogFormat (GOAL_LOG_FORMAT: json or
	// text) shape the structured log on stdout.
	LogLevel  slog.Level
	LogFormat string
	// AgentImage is the image every run's sandbox starts from (GOAL_AGENT_IMAGE, default the
	// agent image this repository builds, task worker:image); a definition's runtime.image, a
	// removed key (field-removed), is ignored.
	AgentImage string
	// SandboxNetwork is the Docker network sandboxes and sidecars run on (GOAL_SANDBOX_NETWORK,
	// default goal_agents): compose.yaml's agents network, whose goal-egress service is their
	// only way out (ADR 0009 and its phase 4 amendment).
	SandboxNetwork string
	// Ignored are the removedVariables that are set: goald warns about them at start.
	Ignored []string
	// AllowPrivateUpstreams lets goald's requests for connections (their tests) reach private,
	// loopback and link-local addresses too (GOAL_ALLOW_PRIVATE_UPSTREAMS: true or false, default
	// false): for a laptop's own test servers, and refused when goald listens off loopback.
	AllowPrivateUpstreams bool
}

const (
	// modelsEnv lists the editor's models; defaultModels are the current Claude models, the most
	// capable first.
	modelsEnv      = "GOAL_MODELS"
	publicURLEnv   = "GOAL_PUBLIC_URL"
	listenEnv      = "GOAL_LISTEN"
	gatewayEnv     = "GOAL_GATEWAY_LISTEN"
	gatewayURLEnv  = "GOAL_GATEWAY_URL"
	authEnv        = "GOAL_AUTH"
	upstreamEnv    = "GOAL_AUTH_UPSTREAM"
	proxiesEnv     = "GOAL_TRUSTED_PROXIES"
	githubWebEnv   = "GOAL_GITHUB_WEB_URL"
	googleEnv      = "GOAL_AUTH_GOOGLE"
	defaultAuthURL = "http://127.0.0.1:8081"
	schemeHTTP     = "http"
	schemeHTTPS    = "https"
	taskEnvEnv     = "GOAL_TASK_ENV"
	// sandboxNetworkEnv names the agents' network (compose.yaml's agents).
	sandboxNetworkEnv = "GOAL_SANDBOX_NETWORK"
	// authProxySecretEnv is the secret goald's /api/auth proxy sends the identity service, which
	// refuses requests without it (GOAL_AUTH=session only; goal up generates one for both).
	authProxySecretEnv = "GOAL_AUTH_PROXY_SECRET" //nolint:gosec // a variable name, not a credential
	authProxySecretRef = "env:" + authProxySecretEnv
	defaultModels      = "claude-opus-5-5,claude-sonnet-5-5,claude-haiku-5-5"
	defaultListen      = "127.0.0.1:8080"
	defaultTaskEnv     = "CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN"
	defaultKeyRef      = "env:GOAL_GATEWAY_KEY"
	defaultImage       = "ghcr.io/byfrederick/goal-agent:0.2.0"
	agentImageEnv      = "GOAL_AGENT_IMAGE"
)

// forbiddenTaskEnv never reaches a task: on a laptop the model is reached with the subscription
// token, and an API key in a container would silently take over billing.
var forbiddenTaskEnv = []string{"ANTHROPIC_API_KEY", "GOAL_GATEWAY_KEY", authProxySecretEnv, repositoryToken}

// repositoryToken is the install's GitHub token, for the factory's runtime.repository (clones,
// merges). Until phase 4's connectors give each connection its own credential, a definition may
// also send it to GitHub's remote MCP server (githubMCPHost) in an https connection's headers,
// and nowhere else (domain.SecretPolicy.RepositoryOnly, which authoring and the gateway apply).
const (
	repositoryToken = "GOAL_GITHUB_TOKEN" //nolint:gosec // a variable name, not a credential
	githubMCPHost   = "api.githubcopilot.com"
)

// ownVariables are the environment variables of goald itself: every setting it reads
// (settingsFrom, s3store.ConfigFromEnv) or reads later in phase 3 (GOAL_AUTH_GOOGLE), the
// database passwords, the default gateway key and the AWS credentials the bucket client reads. A
// factory definition may not reference them (authoring.EnvReferences, env-reserved), so no
// definition can hand them to an agent or a connection.
var ownVariables = []string{
	"GOAL_MIGRATE_DATABASE_URL", "GOAL_DATABASE_URL", "GOAL_PG_PASSWORD", "GOAL_PG_OWNER_PASSWORD", "GOAL_PG_APP_PASSWORD",
	listenEnv, publicURLEnv, gatewayEnv, gatewayURLEnv, "GOAL_GATEWAY_KEY_REF", "GOAL_GATEWAY_KEY", taskEnvEnv,
	authEnv, upstreamEnv, proxiesEnv, googleEnv, authProxySecretEnv,
	"GOAL_TICK_EVERY", "GOAL_REFRESH_EVERY", "GOAL_MAX_RUNS",
	"GOAL_GITHUB_API_URL", githubWebEnv, "GOAL_LOG_LEVEL", "GOAL_LOG_FORMAT", agentImageEnv, "GOAL_ORG_RETENTION", "GOAL_MODELS",
	"GOAL_DEFINITION_CACHE_BYTES", sandboxNetworkEnv,
	"GOAL_S3_BUCKET", "GOAL_S3_ENDPOINT", "GOAL_S3_ACCESS_KEY", "GOAL_S3_SECRET_KEY",
	"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", masterkey.KeysEnv, masterkey.FileEnv, allowPrivateEnv,
}

// identityVariables are the identity service's (auth/: its listen address, database, secrets,
// base URL, bootstrap owner, mail and Google client), a person's API key for the CLI and the
// database administrator `goal db init` connects as. goald never reads them, and a definition may
// not reference them either, even when goald was started with them in its environment.
var identityVariables = []string{
	"GOAL_AUTH_LISTEN", "GOAL_AUTH_DATABASE_URL", "GOAL_PG_AUTH_PASSWORD", "BETTER_AUTH_SECRETS", "BETTER_AUTH_SECRET",
	"BETTER_AUTH_URL", "GOAL_BOOTSTRAP_OWNER_EMAIL", "GOAL_SMTP_URL", "GOAL_SMTP_FROM", "GOAL_SMTP_PLAINTEXT",
	"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET",
	"GOAL_API_KEY", "GOAL_DATABASE_ADMIN_URL",
}

// removedVariables are settings of earlier releases that goald ignores, warning at start while
// one is still set (spec 4 §5.13): the egress proxy is compose.yaml's goal-egress service now,
// with one policy for every box (ADR 0009's phase 4 amendment); runs need no host folders, their
// sandbox has everything; GOAL_AGENT_IMAGE replaces GOAL_DEFAULT_AGENT_IMAGE.
var removedVariables = []string{
	"GOAL_AGENT_EGRESS", "GOAL_EGRESS_NAME", "GOAL_EGRESS_IMAGE", "GOAL_WORKSPACE_ROOT", "GOAL_RUN_DIR", "GOAL_DEFAULT_AGENT_IMAGE",
}

// reserved is the variables definitions may not reference: goald's own (removed ones too), the identity service's,
// the one its gateway key reference names, the platform secrets that never reach a task (forbiddenTaskEnv, but the
// repository token, which serves the repository) and the variables GOAL_TASK_ENV hands every
// task (the model's access, such as CLAUDE_CODE_OAUTH_TOKEN).
func (s settings) reserved() []string {
	out := slices.Concat(ownVariables, identityVariables, removedVariables)

	add := func(ref string) {
		if name, ok := strings.CutPrefix(ref, "env:"); ok && name != repositoryToken && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}

	add(s.GatewayKeyRef)

	for _, name := range forbiddenTaskEnv {
		add("env:" + name)
	}

	for _, name := range slices.Sorted(maps.Keys(s.TaskEnv)) {
		add(s.TaskEnv[name])
	}

	return out
}

// secretPolicy is the install's rule for definitions' env:NAME references until phase 4: the
// reserved variables go nowhere, and the repository token, besides the repository, only to
// GitHub's MCP server. Authoring validates with it (credentials) and the MCP gateway enforces
// it again before resolving a reference for a version already published.
func (s settings) secretPolicy() domain.SecretPolicy {
	return domain.SecretPolicy{
		Reserved: s.reserved(), RepositoryOnly: []string{repositoryToken}, ConnectionHosts: []string{githubMCPHost},
	}
}

// credentials is the check of the env:NAME references versions published before phase 4 still
// hold, until the cutover: they resolve through secrets, they follow secretPolicy (env-reserved),
// and a repository must be on the install's GitHub host (GOAL_GITHUB_WEB_URL), the only one
// clones reach. The connections check runs it beside its own.
func (s settings) credentials(secrets ports.Secrets) authoring.EnvReferences {
	return authoring.EnvReferences{Secrets: secrets, Policy: s.secretPolicy(), RepositoryHosts: []string{s.GitHubWebHost}}
}

func settingsFrom(getenv func(string) string) (settings, error) {
	or := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}

		return def
	}

	s := settings{
		MigrateDatabaseURL: or("GOAL_MIGRATE_DATABASE_URL",
			localDatabase(postgres.OwnerRole, or("GOAL_PG_OWNER_PASSWORD", postgres.DevOwnerPassword))),
		DatabaseURL: or("GOAL_DATABASE_URL",
			localDatabase(postgres.AppRole, or("GOAL_PG_APP_PASSWORD", postgres.DevAppPassword))),
		Listen:        or(listenEnv, defaultListen),
		GatewayKeyRef: or("GOAL_GATEWAY_KEY_REF", defaultKeyRef),
		LogFormat:     or("GOAL_LOG_FORMAT", "json"),
		GitHubAPIURL:  or("GOAL_GITHUB_API_URL", "https://api.github.com"),
		AgentImage:    or(agentImageEnv, defaultImage),
	}

	s.PublicURL = or(publicURLEnv, defaultPublicURL(s.Listen))
	s.GatewayListen = strings.TrimSpace(getenv(gatewayEnv))
	s.GatewayURL = or(gatewayURLEnv, defaultGatewayURL(s))
	errs := authSettings(&s, or)
	errs = append(errs, listenErrors(s)...)
	errs = append(errs, sandboxSettings(&s, getenv, or)...)

	var err error
	if s.TaskEnv, err = taskEnv(or(taskEnvEnv, defaultTaskEnv)); err != nil {
		errs = append(errs, err)
	}

	if s.TickEvery, err = duration(or("GOAL_TICK_EVERY", "5s")); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_TICK_EVERY: %w", err))
	}

	if s.RefreshEvery, err = duration(or("GOAL_REFRESH_EVERY", "30s")); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_REFRESH_EVERY: %w", err))
	}

	if s.OrgRetention, err = duration(or("GOAL_ORG_RETENTION", "7d")); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_ORG_RETENTION: %w", err))
	}

	errs = append(errs, definitionSettings(&s, or)...)
	errs = append(errs, upstreamSettings(&s, or)...)

	if s.MaxRuns, err = strconv.Atoi(or("GOAL_MAX_RUNS", "0")); err != nil || s.MaxRuns < 0 {
		errs = append(errs, fmt.Errorf("GOAL_MAX_RUNS %q is not a count of runs (0 for no cap)", getenv("GOAL_MAX_RUNS")))
	}

	if err := s.LogLevel.UnmarshalText([]byte(or("GOAL_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_LOG_LEVEL: %w", err))
	}

	if _, err := githubURL("GOAL_GITHUB_API_URL", s.GitHubAPIURL); err != nil {
		errs = append(errs, err)
	}

	if s.GitHubWebHost, err = webHost(or(githubWebEnv, "https://github.com")); err != nil {
		errs = append(errs, err)
	}

	if s.LogFormat != "json" && s.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("GOAL_LOG_FORMAT %q is not json or text", s.LogFormat))
	}

	// errors.Join drops a nil error.
	s.Models, err = models(or(modelsEnv, defaultModels))

	return s, errors.Join(append(errs, err)...)
}

// sandboxSettings reads GOAL_SANDBOX_NETWORK, checks GOAL_GATEWAY_URL as containers are given
// its path, and lists the removed settings still set.
func sandboxSettings(s *settings, getenv func(string) string, or func(name, def string) string) []error {
	s.SandboxNetwork = or(sandboxNetworkEnv, docker.DefaultNetwork)

	for _, name := range removedVariables {
		if strings.TrimSpace(getenv(name)) != "" {
			s.Ignored = append(s.Ignored, name)
		}
	}

	if u, err := url.Parse(s.GatewayURL); err != nil || (u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS) || u.Host == "" || u.Path == "" || u.RawQuery != "" {
		return []error{fmt.Errorf("%s %q is not an http(s) URL with a path and no query, such as http://127.0.0.1:8080/mcp", gatewayURLEnv, s.GatewayURL)}
	}

	return nil
}

// allowPrivateEnv lets connection requests reach private addresses (spec 4 §5.9).
const allowPrivateEnv = "GOAL_ALLOW_PRIVATE_UPSTREAMS"

// upstreamSettings reads GOAL_ALLOW_PRIVATE_UPSTREAMS, refused off loopback: another machine's
// people could then aim a connection's test at goald's own network.
func upstreamSettings(s *settings, or func(name, def string) string) []error {
	allow, err := strconv.ParseBool(or(allowPrivateEnv, "false"))
	if err != nil {
		return []error{fmt.Errorf("%s %q is not true or false", allowPrivateEnv, or(allowPrivateEnv, ""))}
	}

	if host, _, err := net.SplitHostPort(s.Listen); allow && (err != nil || !isLoopback(host)) {
		return []error{fmt.Errorf("%s=true lets connection tests reach private addresses, so goald must listen on a loopback address, "+
			"not GOAL_LISTEN %q", allowPrivateEnv, s.Listen)}
	}

	s.AllowPrivateUpstreams = allow

	return nil
}

// definitionSettings reads the settings of the definitions cache.
func definitionSettings(s *settings, or func(name, def string) string) []error {
	var (
		errs []error
		err  error
	)

	cacheBytes := or("GOAL_DEFINITION_CACHE_BYTES", strconv.Itoa(definitions.DefaultCacheBytes))
	if s.DefinitionCacheBytes, err = strconv.ParseInt(cacheBytes, 10, 64); err != nil || s.DefinitionCacheBytes <= 0 {
		errs = append(errs, fmt.Errorf("GOAL_DEFINITION_CACHE_BYTES %q is not a positive number of bytes", cacheBytes))
	}

	return errs
}

// authMode is how goald authenticates requests (GOAL_AUTH).
type authMode string

const (
	// authNone makes every request the local user's: one developer's laptop, loopback only.
	authNone authMode = "none"
	// authSession resolves each request's session or API key, and proxies /api/auth/* to the
	// identity service.
	authSession authMode = "session"
)

// defaultAuth is GOAL_AUTH's default: people sign in (spec 3 §14, from PR 3-16). The single local
// user (none) is only ever asked for, and only on a loopback listen.
const defaultAuth = authSession

// authSettings reads GOAL_AUTH, GOAL_AUTH_GOOGLE, GOAL_AUTH_UPSTREAM and GOAL_TRUSTED_PROXIES.
func authSettings(s *settings, or func(name, def string) string) []error {
	var errs []error

	switch mode := authMode(or(authEnv, string(defaultAuth))); mode {
	case authNone, authSession:
		s.Auth = mode
	default:
		errs = append(errs, fmt.Errorf("GOAL_AUTH %q is not none or session", string(mode)))
	}

	switch google := or(googleEnv, "off"); google {
	case "on", "off":
		s.Google = google == "on"
	default:
		errs = append(errs, fmt.Errorf("%s %q is not on or off", googleEnv, google))
	}

	var err error
	if s.AuthUpstream, err = authUpstream(or(upstreamEnv, defaultAuthURL)); err != nil {
		errs = append(errs, err)
	}

	if s.TrustedProxies, err = trustedProxies(or(proxiesEnv, "")); err != nil {
		errs = append(errs, err)
	}

	return errs
}

// authUpstream parses GOAL_AUTH_UPSTREAM: an http origin on a loopback address, since the
// identity service listens on loopback only and the proxy hands it people's cookies.
func authUpstream(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != schemeHTTP || u.Host == "" || !isLoopback(u.Hostname()) || strings.TrimSuffix(u.Path, "/") != "" ||
		u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("%s %q is not an http origin on a loopback address, such as %s", upstreamEnv, raw, defaultAuthURL)
	}

	u.Path = ""

	return u, nil
}

// trustedProxies parses GOAL_TRUSTED_PROXIES: comma-separated CIDRs or single addresses.
func trustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix

	for item := range strings.SplitSeq(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}

		a, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a CIDR such as 10.0.0.0/8 or an address", proxiesEnv, item)
		}

		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}

	return out, nil
}

// strictTransport reports whether people open goald over https, so browsers must keep to it.
func (s settings) strictTransport() bool {
	u, err := url.Parse(s.PublicURL)

	return err == nil && u.Scheme == schemeHTTPS
}

// localDatabase is compose.yaml's database as role.
func localDatabase(role, password string) string {
	return (&url.URL{
		Scheme: "postgres", User: url.UserPassword(role, password),
		Host: "127.0.0.1:5432", Path: "goal", RawQuery: "sslmode=disable",
	}).String()
}

// listenAddress refuses a GOAL_LISTEN that is not host:port, and one other machines could reach
// unless people sign in (GOAL_AUTH=session): under none every request is the local user. Off
// loopback, people need GOAL_PUBLIC_URL to reach goald by.
func listenAddress(s settings) error {
	host, _, err := net.SplitHostPort(s.Listen)
	if err != nil {
		return fmt.Errorf("GOAL_LISTEN %q: %w", s.Listen, err)
	}

	switch {
	case isLoopback(host):
		return nil
	case s.Auth != authSession:
		return fmt.Errorf("GOAL_LISTEN %q: under GOAL_AUTH=%s every request is the local user, so goald listens on a loopback "+
			"address only; set GOAL_AUTH=session for sign-in", s.Listen, s.Auth)
	case s.PublicURL == "":
		return fmt.Errorf("%s is required when GOAL_LISTEN %q is not a loopback address: the origin people open goald at", publicURLEnv, s.Listen)
	default:
		return nil
	}
}

// listenErrors refuses a listen address off loopback without sign-in, a gateway listener that
// breaks §5.5's rule, a gateway URL that names the main listener when the gateway has its own, and
// a public URL that is not a safe origin.
func listenErrors(s settings) []error {
	var errs []error

	for _, err := range []error{listenAddress(s), gatewayListen(s.Listen, s.GatewayListen), gatewayURL(s), publicURL(s.PublicURL)} {
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// gatewayListen applies phase 3's listener rule: the gateway's own address is a host and port,
// not the main listener's, and it is required when the main listener is off loopback (so /mcp is
// never served beside the API off this machine).
func gatewayListen(listen, gateway string) error {
	host, _, _ := net.SplitHostPort(listen) // listenAddress reports a malformed GOAL_LISTEN

	if gateway == "" {
		if host != "" && !isLoopback(host) {
			return fmt.Errorf("%s is required when GOAL_LISTEN %q is not a loopback address: the gateway gets a listener of its own", gatewayEnv, listen)
		}

		return nil
	}

	if _, port, err := net.SplitHostPort(gateway); err != nil || port == "" {
		return fmt.Errorf("%s %q is not a host:port address", gatewayEnv, gateway)
	}

	if sameAddress(listen, gateway) {
		return fmt.Errorf("%s %q is GOAL_LISTEN: the gateway's own listener needs another address", gatewayEnv, gateway)
	}

	return nil
}

// gatewayURL refuses a GOAL_GATEWAY_URL that names the main listener while the gateway has a
// listener of its own: the main listener answers /mcp with 404, so agents would reach nothing
// but the API.
func gatewayURL(s settings) error {
	if s.GatewayListen == "" {
		return nil
	}

	u, err := url.Parse(s.GatewayURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s %q is not a URL", gatewayURLEnv, s.GatewayURL)
	}

	port := u.Port()
	if port == "" && u.Scheme == schemeHTTPS {
		port = "443"
	} else if port == "" {
		port = "80"
	}

	host := u.Hostname()
	if host == dockerHost { // how agent containers name this machine (the Docker runner's alias)
		host = "127.0.0.1"
	}

	if sameAddress(s.Listen, net.JoinHostPort(host, port)) {
		return fmt.Errorf("%s %q names GOAL_LISTEN, which answers /mcp with 404 when %s is set; leave it unset to follow the gateway listener",
			gatewayURLEnv, s.GatewayURL, gatewayEnv)
	}

	return nil
}

// dockerHost is the name agent containers reach this machine by.
const dockerHost = "host.docker.internal"

// sameAddress reports whether host:port addresses a and b would be one listener: the same port,
// and the same host, two loopback names or a wildcard host (every interface).
func sameAddress(a, b string) bool {
	ah, ap, errA := net.SplitHostPort(a)
	bh, bp, errB := net.SplitHostPort(b)

	if errA != nil || errB != nil || ap != bp {
		return false
	}

	return strings.EqualFold(ah, bh) || (isLoopback(ah) && isLoopback(bh)) || wildcard(ah) || wildcard(bh)
}

// wildcard reports whether a listen host (no port) means every interface.
func wildcard(host string) bool {
	ip := net.ParseIP(host)

	return host == "" || (ip != nil && ip.IsUnspecified())
}

// defaultGatewayURL is the gateway as agent containers reach it: on its own listener when it has
// one, else on the main listener. A wildcard host (every interface) becomes 127.0.0.1, which the
// Docker runner rewrites to the host's name in a container.
func defaultGatewayURL(s settings) string {
	addr := cmp.Or(s.GatewayListen, s.Listen)

	if host, port, err := net.SplitHostPort(addr); err == nil && wildcard(host) {
		addr = net.JoinHostPort("127.0.0.1", port)
	}

	return "http://" + addr + "/mcp"
}

// defaultPublicURL is the listen address as an http origin when it is on loopback; off loopback
// there is no default (phase 3 requires GOAL_PUBLIC_URL there).
func defaultPublicURL(listen string) string {
	if host, _, err := net.SplitHostPort(listen); err == nil && isLoopback(host) {
		return schemeHTTP + "://" + listen
	}

	return ""
}

// isLoopback reports whether host (no port) names this machine.
func isLoopback(host string) bool {
	ip := net.ParseIP(host)

	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// publicURL refuses a GOAL_PUBLIC_URL that is not an origin (scheme and host, no path), or one
// in clear text off this machine: http only on a loopback host or a .localhost name. Empty is no
// public URL.
func publicURL(raw string) error {
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != schemeHTTPS && u.Scheme != schemeHTTP) || strings.TrimSuffix(u.Path, "/") != "" ||
		u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("GOAL_PUBLIC_URL %q is not an origin such as https://goal.example.com", raw)
	}

	if u.Scheme == schemeHTTP && !isLoopback(u.Hostname()) && !localhostName(u.Hostname()) {
		return fmt.Errorf("GOAL_PUBLIC_URL %q must be https off this machine", raw)
	}

	return nil
}

// localhostNameForm is a name under .localhost (RFC 6761 §6.3), such as goal.localhost.
var localhostNameForm = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+localhost$`)

// localhostName is whether host is a name under .localhost, which browsers resolve to this
// machine themselves, so a public URL on it may be http like one on localhost. Browsers do not
// separate cookies by port, so a name of its own keeps goald's session cookie from every other
// local web server the person opens (review L5, SECURITY.md).
func localhostName(host string) bool {
	return localhostNameForm.MatchString(strings.ToLower(host))
}

// hosts are the names goald answers to (api.CheckHost), each once: the loopback names when it
// listens on loopback, the listen host and the public URL's host.
func (s settings) hosts() []string {
	var out []string

	add := func(h string) {
		if h = strings.ToLower(h); h != "" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}

	if h, _, err := net.SplitHostPort(s.Listen); err == nil {
		if isLoopback(h) {
			add("localhost")
			add("127.0.0.1")
			add("::1")
		}

		add(h)
	}

	if u, err := url.Parse(s.PublicURL); err == nil {
		add(u.Hostname())
	}

	return out
}

// githubURL parses the GitHub URL setting name and refuses one that a repository token would
// reach in clear text, except on this machine (a stand-in GitHub for tests).
func githubURL(name, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%s %q is not a URL", name, raw)
	}

	if u.Scheme != schemeHTTPS && (u.Scheme != schemeHTTP || !isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("%s %q must be https (http only on a loopback address)", name, raw)
	}

	return u, nil
}

// webHost reads GOAL_GITHUB_WEB_URL, an origin, as its host[:port] in lower case: the one host
// the install's repository token may be sent to by clones and merges.
func webHost(raw string) (string, error) {
	u, err := githubURL(githubWebEnv, raw)
	if err != nil {
		return "", err
	}

	if u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s %q must be an origin such as https://github.com, without a user or a path", githubWebEnv, raw)
	}

	return strings.ToLower(u.Host), nil
}

// taskEnv parses NAME=ref pairs. Values must be references (env:NAME), never secret literals,
// and some names may never reach a task.
func taskEnv(raw string) (map[string]string, error) {
	out := map[string]string{}

	for pair := range strings.SplitSeq(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		name, ref, ok := strings.Cut(pair, "=")
		if !ok || name == "" || !strings.HasPrefix(ref, "env:") {
			return nil, fmt.Errorf("GOAL_TASK_ENV: %q is not NAME=env:REF", name)
		}

		for _, f := range forbiddenTaskEnv {
			if strings.EqualFold(name, f) {
				return nil, fmt.Errorf("GOAL_TASK_ENV: %s must never reach an agent task", name)
			}
		}

		out[name] = ref
	}

	return out, nil
}

// duration parses a positive Go duration, or a whole number of days such as 7d.
func duration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil {
			s = strconv.Itoa(n*24) + "h"
		}
	}

	d, err := time.ParseDuration(s)
	if err == nil && d <= 0 {
		err = fmt.Errorf("%s is not positive", s)
	}

	return d, err
}

// modelID is the shape of runtime.model and agents[].model in schema/factory.schema.json.
var modelID = regexp.MustCompile(`^claude-[a-z0-9-]+$`)

// models parses GOAL_MODELS: comma-separated model ids, each optionally followed by =Label. A
// model without a label gets one from its id (claude-opus-5-5 is Claude Opus 5.5).
func models(raw string) ([]builder.Model, error) {
	var out []builder.Model

	for item := range strings.SplitSeq(raw, ",") {
		id, label, _ := strings.Cut(strings.TrimSpace(item), "=")
		id, label = strings.TrimSpace(id), strings.TrimSpace(label)

		if !modelID.MatchString(id) {
			return nil, fmt.Errorf("GOAL_MODELS: %q is not a model id (claude-...)", id)
		}

		if slices.ContainsFunc(out, func(m builder.Model) bool { return m.ID == id }) {
			return nil, fmt.Errorf("GOAL_MODELS: %s is listed twice", id)
		}

		out = append(out, builder.Model{ID: id, Label: cmp.Or(label, modelLabel(id))})
	}

	return out, nil
}

// modelLabel names a model from its id: words capitalized, version digits joined by dots.
func modelLabel(id string) string {
	var words, version []string

	for part := range strings.SplitSeq(id, "-") {
		if part == "" {
			continue
		}

		if strings.Trim(part, "0123456789") == "" {
			version = append(version, part)
			continue
		}

		words = append(words, strings.ToUpper(part[:1])+part[1:])
	}

	if len(version) > 0 {
		words = append(words, strings.Join(version, "."))
	}

	return strings.Join(words, " ")
}
