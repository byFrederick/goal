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
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/definitions"
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
	// 127.0.0.1:8080). The API has no authentication yet, so it must be a loopback address.
	Listen string
	// GatewayListen is the MCP gateway's own address (GOAL_GATEWAY_LISTEN, optional). When set,
	// that listener serves only /mcp and the main one answers /mcp with 404; it is required, and
	// different, when Listen is not loopback (phase 3, §5.5). It may be off loopback, such as the
	// Docker bridge's address on Linux: every request there needs a task token.
	GatewayListen string
	// PublicURL is the origin people open goald at (GOAL_PUBLIC_URL, default http://<listen>;
	// https unless its host is loopback), such as a reverse proxy's name. Its host is the one name
	// besides the loopback names and the listen host that the Host check accepts (api.CheckHost),
	// and over https the UI is served with Strict-Transport-Security.
	PublicURL string
	// Auth is how requests are authenticated (GOAL_AUTH): none, everyone is the local user. Phase
	// 3 adds session (people sign in); until goald can check sessions it refuses that value, so
	// no request is let through under a mode that is not there.
	Auth authMode
	// AuthUpstream is the identity service that /api/auth/* is proxied to under session
	// (GOAL_AUTH_UPSTREAM, default http://127.0.0.1:8081; http on a loopback address only).
	AuthUpstream *url.URL
	// TrustedProxies are the proxies in front of goald whose X-Forwarded-For names the client the
	// identity service is told about (GOAL_TRUSTED_PROXIES: comma-separated CIDRs or addresses,
	// default none).
	TrustedProxies []netip.Prefix
	// GatewayURL is the MCP gateway as agent containers reach it, the only goald address they are
	// given (GOAL_GATEWAY_URL, default http://<gateway listen or listen>/mcp, a wildcard host
	// becoming 127.0.0.1; the Docker runner rewrites loopback to host.docker.internal).
	GatewayURL string
	// GatewayKeyRef names the secret that signs task tokens (GOAL_GATEWAY_KEY_REF, default
	// env:GOAL_GATEWAY_KEY).
	GatewayKeyRef string
	// TaskEnv maps variables of every agent task to secret references (GOAL_TASK_ENV,
	// NAME=ref pairs separated by commas; default CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN).
	TaskEnv map[string]string
	// WorkspaceRoot holds the runs' git checkouts (GOAL_WORKSPACE_ROOT, default
	// $TMPDIR/goal-workspaces); RunDir the runs' agent files (GOAL_RUN_DIR, default
	// $TMPDIR/goal-runs). Docker must be able to bind-mount both.
	WorkspaceRoot string
	RunDir        string
	// TickEvery is how often the engine pulls work (GOAL_TICK_EVERY, default 5s); RefreshEvery how
	// often the current definition versions are reconciled with PostgreSQL (GOAL_REFRESH_EVERY,
	// default 30s), a safety net behind the goal_definitions notices that reload them at once.
	TickEvery    time.Duration
	RefreshEvery time.Duration
	// DefinitionCacheBytes bounds the files of the definition versions kept for runs that started
	// on a version no longer current (GOAL_DEFINITION_CACHE_BYTES, default 64 MiB).
	DefinitionCacheBytes int64
	// LegacyImport imports, at startup, the factories of the bucket layout before versions that
	// have no version yet (GOAL_LEGACY_IMPORT, default true for this release). It is a no-op once
	// every organization's import ran: each organization from before versions imports once.
	LegacyImport bool
	// MaxRuns caps the agent runs this goald has in flight at once (GOAL_MAX_RUNS, default 0:
	// no cap). With a cap, claims are shared out fairly across organizations.
	MaxRuns int
	// OrgRetention is how long a deleted organization is kept before it is purged with its rows
	// and bucket objects (GOAL_ORG_RETENTION, default 7d; a Go duration or a number of days).
	OrgRetention time.Duration
	// GitHubAPIURL is the GitHub REST API goald merges pull requests through
	// (GOAL_GITHUB_API_URL, default https://api.github.com; http only on a loopback address).
	GitHubAPIURL string
	// Models are the models the factory editor offers (GOAL_MODELS: comma-separated model ids,
	// each optionally id=Label; the first is the default; default defaultModels).
	Models []builder.Model
	// LogLevel (GOAL_LOG_LEVEL: debug, info, warn, error) and LogFormat (GOAL_LOG_FORMAT: json or
	// text) shape the structured log on stdout.
	LogLevel  slog.Level
	LogFormat string
	// DefaultAgentImage is the agent image a blank factory's skeleton names
	// (GOAL_DEFAULT_AGENT_IMAGE, default the agent image this repository builds).
	DefaultAgentImage string
}

const (
	// modelsEnv lists the editor's models; defaultModels are the current Claude models, the most
	// capable first.
	modelsEnv      = "GOAL_MODELS"
	publicURLEnv   = "GOAL_PUBLIC_URL"
	gatewayEnv     = "GOAL_GATEWAY_LISTEN"
	gatewayURLEnv  = "GOAL_GATEWAY_URL"
	authEnv        = "GOAL_AUTH"
	upstreamEnv    = "GOAL_AUTH_UPSTREAM"
	proxiesEnv     = "GOAL_TRUSTED_PROXIES"
	defaultAuthURL = "http://127.0.0.1:8081"
	schemeHTTP     = "http"
	schemeHTTPS    = "https"
	taskEnvEnv     = "GOAL_TASK_ENV"
	defaultModels  = "claude-opus-5-5,claude-sonnet-5-5,claude-haiku-5-5"
	defaultListen  = "127.0.0.1:8080"
	defaultTaskEnv = "CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN"
	defaultKeyRef  = "env:GOAL_GATEWAY_KEY"
	defaultImage   = "ghcr.io/byfrederick/goal-agent:0.1.0"
)

// forbiddenTaskEnv never reaches a task: on a laptop the model is reached with the subscription
// token, and an API key in a container would silently take over billing.
var forbiddenTaskEnv = []string{"ANTHROPIC_API_KEY", "GOAL_GATEWAY_KEY", repositoryToken}

// repositoryToken is the install's GitHub token, for the factory's runtime.repository (clones,
// merges). Until phase 4's connectors give each connection its own credential, a definition may
// also send it to GitHub's remote MCP server (githubMCPHost) in an https connection's headers,
// and nowhere else (authoring.EnvReferences.RepositoryOnly).
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
	"GOAL_LISTEN", publicURLEnv, gatewayEnv, gatewayURLEnv, "GOAL_GATEWAY_KEY_REF", "GOAL_GATEWAY_KEY", taskEnvEnv,
	authEnv, upstreamEnv, proxiesEnv, "GOAL_AUTH_GOOGLE",
	"GOAL_WORKSPACE_ROOT", "GOAL_RUN_DIR", "GOAL_TICK_EVERY", "GOAL_REFRESH_EVERY", "GOAL_MAX_RUNS",
	"GOAL_GITHUB_API_URL", "GOAL_LOG_LEVEL", "GOAL_LOG_FORMAT", "GOAL_DEFAULT_AGENT_IMAGE", "GOAL_ORG_RETENTION", "GOAL_MODELS",
	"GOAL_DEFINITION_CACHE_BYTES", "GOAL_LEGACY_IMPORT",
	"GOAL_S3_BUCKET", "GOAL_S3_ENDPOINT", "GOAL_S3_ACCESS_KEY", "GOAL_S3_SECRET_KEY",
	"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
}

// identityVariables are the identity service's (auth/: its listen address, database, secrets,
// base URL, bootstrap owner, mail and Google client), a person's API key for the CLI and the
// database administrator `goal db init` connects as. goald never reads them, and a definition may
// not reference them either, even when goald was started with them in its environment.
var identityVariables = []string{
	"GOAL_AUTH_LISTEN", "GOAL_AUTH_DATABASE_URL", "GOAL_PG_AUTH_PASSWORD", "BETTER_AUTH_SECRETS", "BETTER_AUTH_SECRET",
	"BETTER_AUTH_URL", "GOAL_BOOTSTRAP_OWNER_EMAIL", "GOAL_SMTP_URL", "GOAL_SMTP_FROM", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET",
	"GOAL_API_KEY", "GOAL_DATABASE_ADMIN_URL",
}

// reserved is the variables definitions may not reference: goald's own, the identity service's,
// the one its gateway key reference names, the platform secrets that never reach a task (forbiddenTaskEnv, but the
// repository token, which serves the repository) and the variables GOAL_TASK_ENV hands every
// task (the model's access, such as CLAUDE_CODE_OAUTH_TOKEN).
func (s settings) reserved() []string {
	out := slices.Concat(ownVariables, identityVariables)

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

// credentials is the check of a definition's secret references until phase 4: env:NAME
// references resolve through secrets, the reserved variables are refused (env-reserved), and
// the repository token serves only the repository and GitHub's MCP server.
func (s settings) credentials(secrets ports.Secrets) authoring.EnvReferences {
	return authoring.EnvReferences{
		Secrets: secrets, Reserved: s.reserved(),
		RepositoryOnly: []string{repositoryToken}, RepositoryHosts: []string{githubMCPHost},
	}
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
		Listen:        or("GOAL_LISTEN", defaultListen),
		GatewayKeyRef: or("GOAL_GATEWAY_KEY_REF", defaultKeyRef),
		WorkspaceRoot: or("GOAL_WORKSPACE_ROOT", filepath.Join(os.TempDir(), "goal-workspaces")),
		RunDir:        or("GOAL_RUN_DIR", filepath.Join(os.TempDir(), "goal-runs")),
		LogFormat:     or("GOAL_LOG_FORMAT", "json"),
		GitHubAPIURL:  or("GOAL_GITHUB_API_URL", "https://api.github.com"),

		DefaultAgentImage: or("GOAL_DEFAULT_AGENT_IMAGE", defaultImage),
	}

	s.PublicURL = or(publicURLEnv, defaultPublicURL(s.Listen))
	s.GatewayListen = strings.TrimSpace(getenv(gatewayEnv))
	s.GatewayURL = or(gatewayURLEnv, defaultGatewayURL(s))
	errs := append(listenErrors(s), authSettings(&s, or)...)

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

	if s.MaxRuns, err = strconv.Atoi(or("GOAL_MAX_RUNS", "0")); err != nil || s.MaxRuns < 0 {
		errs = append(errs, fmt.Errorf("GOAL_MAX_RUNS %q is not a count of runs (0 for no cap)", getenv("GOAL_MAX_RUNS")))
	}

	if err := s.LogLevel.UnmarshalText([]byte(or("GOAL_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_LOG_LEVEL: %w", err))
	}

	if err := apiURL(s.GitHubAPIURL); err != nil {
		errs = append(errs, err)
	}

	if s.LogFormat != "json" && s.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("GOAL_LOG_FORMAT %q is not json or text", s.LogFormat))
	}

	// errors.Join drops a nil error.
	s.Models, err = models(or(modelsEnv, defaultModels))

	return s, errors.Join(append(errs, err)...)
}

// definitionSettings reads the settings of the definitions cache and the legacy import.
func definitionSettings(s *settings, or func(name, def string) string) []error {
	var (
		errs []error
		err  error
	)

	cacheBytes := or("GOAL_DEFINITION_CACHE_BYTES", strconv.Itoa(definitions.DefaultCacheBytes))
	if s.DefinitionCacheBytes, err = strconv.ParseInt(cacheBytes, 10, 64); err != nil || s.DefinitionCacheBytes <= 0 {
		errs = append(errs, fmt.Errorf("GOAL_DEFINITION_CACHE_BYTES %q is not a positive number of bytes", cacheBytes))
	}

	legacy := or("GOAL_LEGACY_IMPORT", "true")
	if s.LegacyImport, err = strconv.ParseBool(legacy); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_LEGACY_IMPORT %q is not true or false", legacy))
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

// authSettings reads GOAL_AUTH, GOAL_AUTH_UPSTREAM and GOAL_TRUSTED_PROXIES.
func authSettings(s *settings, or func(name, def string) string) []error {
	var errs []error

	switch mode := authMode(or(authEnv, string(authNone))); mode {
	case authNone:
		s.Auth = mode
	case authSession:
		errs = append(errs, errors.New("GOAL_AUTH=session: this goald cannot check sign-ins yet; use none"))
	default:
		errs = append(errs, fmt.Errorf("GOAL_AUTH %q is not none or session", string(mode)))
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

// loopback refuses listen addresses other machines could reach: the phase 1 API has no
// authentication.
func loopback(listen string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("GOAL_LISTEN %q: %w", listen, err)
	}

	if !isLoopback(host) {
		return fmt.Errorf("GOAL_LISTEN %q: the API has no authentication yet, so goald listens on a loopback address only", listen)
	}

	return nil
}

// listenErrors refuses a listen address off loopback, a gateway listener that breaks §5.5's rule,
// a gateway URL that names the main listener when the gateway has its own, and a public URL that
// is not a safe origin.
func listenErrors(s settings) []error {
	var errs []error

	for _, err := range []error{loopback(s.Listen), gatewayListen(s.Listen, s.GatewayListen), gatewayURL(s), publicURL(s.PublicURL)} {
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// gatewayListen applies phase 3's listener rule: the gateway's own address is a host and port,
// not the main listener's, and it is required when the main listener is off loopback (so /mcp is
// never served beside the API off this machine). GOAL_LISTEN stays loopback-only until 3-9, so
// for now only the first two parts can refuse a start.
func gatewayListen(listen, gateway string) error {
	host, _, _ := net.SplitHostPort(listen) // loopback reports a malformed GOAL_LISTEN

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
// in clear text off this machine. Empty is no public URL.
func publicURL(raw string) error {
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != schemeHTTPS && u.Scheme != schemeHTTP) || strings.TrimSuffix(u.Path, "/") != "" ||
		u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("GOAL_PUBLIC_URL %q is not an origin such as https://goal.example.com", raw)
	}

	if u.Scheme == schemeHTTP && !isLoopback(u.Hostname()) {
		return fmt.Errorf("GOAL_PUBLIC_URL %q must be https off this machine", raw)
	}

	return nil
}

// hosts are the names goald answers to (api.CheckHost), each once: the loopback names when it
// listens on loopback (always, until phase 3), the listen host and the public URL's host.
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

// apiURL refuses a GitHub API that a repository token would reach in clear text, except on
// this machine (a stand-in API for tests).
func apiURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("GOAL_GITHUB_API_URL %q is not a URL", raw)
	}

	if u.Scheme == schemeHTTPS {
		return nil
	}

	if u.Scheme != schemeHTTP || !isLoopback(u.Hostname()) {
		return fmt.Errorf("GOAL_GITHUB_API_URL %q must be https (http only on a loopback address)", raw)
	}

	return nil
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
