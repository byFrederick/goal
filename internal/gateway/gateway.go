// Package gateway is goald's MCP gateway: the one MCP endpoint (streamable HTTP) through which
// agent containers reach the tools of their factory's connections. Credentials stay on the
// host: the gateway resolves them through ports.Secrets and adds them to upstream requests,
// so an agent only ever holds its per-task token.
//
// # Auth
//
// Every request carries "Authorization: Bearer <task token>" signed by the engine
// (domain.SignTaskToken). A missing, forged or expired token gets 401 with no detail. The
// claims name the factory, agent and run; the factory's current definition must still declare
// that agent, otherwise 403.
//
// # Connections and tools
//
// A task is granted the connections that are both in its token and in its agent's
// connections in the current definition. Remote MCP servers (type mcp, transport http) and
// stdio servers with an image (sidecars, below) are served; every other kind (builtin, aws, a
// stdio command without an image) is reported unavailable, not an error. Tools are exposed as
// "<connection>__<tool>"; connection names are slugs without underscores, so the first "__"
// always splits a name unambiguously.
//
// # Sidecars
//
// A stdio connection's server runs in its own hardened container per run, through
// ports.Sidecars: never on the host and never in the agent's container. Its env values and
// command words that are secret references are resolved on the host and passed only to the
// sidecar. Its image must be pinned (config.Pinned); an unpinned one is reported unavailable.
// The sidecar starts on the run's first tools/list or tools/call that needs it, keeps one MCP
// session over its stdio for the rest of the run, and stops when goald reports the run ended
// (EndRun), after SidecarIdle without requests, or when its server exits. MaxSidecars caps the
// sidecars of one goald; past it a connection is unavailable until a run ends. A sidecar that
// fails to start makes its connection unavailable for that request only.
//
// An agent's tools.allow and tools.deny entries of the form "<connection>__<tool pattern>",
// where <connection> is one of the factory's connections, apply to gateway tools (patterns as
// in path.Match, so "github__get_*"). Other entries (Read, Bash(git push*)) are the worker's
// business and are ignored here. Deny wins; a connection with at least one allow entry serves
// only the tools its allow entries match; a connection with none serves all its tools.
//
// # State
//
// The gateway is stateless towards agents: every HTTP request is verified and served on its own
// (the SDK's stateless mode) and opens its own short sessions with remote servers. The registry
// of running sidecars, keyed by run, is its only state, and it is per instance: a run's MCP
// traffic must reach the goald that runs its sidecars. In phase 2 one goald serves a laptop, so
// it always does; several instances will need requests routed by run (the run id is in the
// token) or sidecars any instance can reach. A goald that dies leaves its sidecars to the
// runner's Reap, as it does its agent containers.
//
// # Audit
//
// Every tools/call, including denied ones, appends an event (EventToolCall) with the
// connection, tool, duration, outcome and request and response sizes; never arguments,
// results or credentials. Resolved credentials are also scrubbed from upstream results and
// error messages before they reach the agent, the logs or an event.
package gateway

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// EventToolCall is the kind of the audit event every gateway tool call appends.
const EventToolCall domain.EventKind = "gateway_tool_call"

// Definitions returns the current definition of a factory. The gateway calls it on every
// request, so it should be cheap (goald backs it with a cache over the bucket). A factory that
// does not exist is an error wrapping ports.ErrNotFound.
type Definitions interface {
	Factory(ctx context.Context, id domain.FactoryID) (*config.Factory, error)
}

// Events records audit events; ports.Store satisfies it.
type Events interface {
	AppendEvents(ctx context.Context, events ...domain.Event) error
}

// Deps are the gateway's collaborators. Transport may be nil (http.DefaultTransport); Logger
// may be nil (logs discarded); Sidecars may be nil (stdio connections are unavailable).
type Deps struct {
	Definitions Definitions
	Secrets     ports.Secrets
	Events      Events
	Clock       ports.Clock
	Logger      *slog.Logger
	// Transport carries upstream requests.
	Transport http.RoundTripper
	// Sidecars runs the servers of stdio connections.
	Sidecars ports.Sidecars
}

// Config bounds the gateway. Zero values take the defaults below.
type Config struct {
	// Key verifies task tokens: the resolved value of the engine's GatewayKeyRef.
	Key []byte
	// CallTimeout bounds one upstream tools/call, connection included (default 2m).
	CallTimeout time.Duration
	// ListTimeout bounds listing the tools of all granted connections (default 30s).
	ListTimeout time.Duration
	// MaxResponseBytes caps one upstream HTTP response and one tool result (default 8 MiB).
	MaxResponseBytes int64
	// MaxRequestBytes caps one request from an agent (default 4 MiB).
	MaxRequestBytes int64
	// SidecarIdle stops a sidecar no request has used for this long (default 10m).
	SidecarIdle time.Duration
	// SidecarStartTimeout bounds starting one sidecar, pulling its image included (default 5m).
	SidecarStartTimeout time.Duration
	// MaxSidecars caps the sidecars this goald runs at once (default 16).
	MaxSidecars int
}

const (
	defaultCallTimeout         = 2 * time.Minute
	defaultListTimeout         = 30 * time.Second
	defaultMaxResponseBytes    = 8 << 20
	defaultMaxRequestBytes     = 4 << 20
	defaultSidecarIdle         = 10 * time.Minute
	defaultSidecarStartTimeout = 5 * time.Minute
	defaultMaxSidecars         = 16
	auditTimeout               = 5 * time.Second
)

// implementation identifies the gateway to agents and to upstream servers.
var implementation = &mcp.Implementation{Name: "goal-gateway", Version: "1"}

// Gateway serves the MCP endpoint. It is an http.Handler, safe for concurrent use.
type Gateway struct {
	defs      Definitions
	secrets   ports.Secrets
	events    Events
	clock     ports.Clock
	log       *slog.Logger
	transport http.RoundTripper
	sidecars  *sidecarPool // nil without Deps.Sidecars
	cfg       Config
	mcp       http.Handler
}

// New returns a gateway; goald mounts it at its MCP path.
func New(d Deps, cfg Config) (*Gateway, error) {
	if d.Definitions == nil || d.Secrets == nil || d.Events == nil || d.Clock == nil {
		return nil, errors.New("gateway: Definitions, Secrets, Events and Clock are required")
	}

	if len(cfg.Key) == 0 {
		return nil, errors.New("gateway: a token key is required")
	}

	cfg.CallTimeout = cmp.Or(cfg.CallTimeout, defaultCallTimeout)
	cfg.ListTimeout = cmp.Or(cfg.ListTimeout, defaultListTimeout)
	cfg.MaxResponseBytes = cmp.Or(cfg.MaxResponseBytes, defaultMaxResponseBytes)
	cfg.MaxRequestBytes = cmp.Or(cfg.MaxRequestBytes, defaultMaxRequestBytes)
	cfg.SidecarIdle = cmp.Or(cfg.SidecarIdle, defaultSidecarIdle)
	cfg.SidecarStartTimeout = cmp.Or(cfg.SidecarStartTimeout, defaultSidecarStartTimeout)
	cfg.MaxSidecars = cmp.Or(cfg.MaxSidecars, defaultMaxSidecars)

	g := &Gateway{
		defs: d.Definitions, secrets: d.Secrets, events: d.Events, clock: d.Clock,
		log: d.Logger, transport: d.Transport, cfg: cfg,
	}
	if g.log == nil {
		g.log = slog.New(slog.DiscardHandler)
	}

	if g.transport == nil {
		g.transport = http.DefaultTransport
	}

	if d.Sidecars != nil {
		g.sidecars = newSidecarPool(d.Sidecars, cfg, g.log)
	}

	g.mcp = mcp.NewStreamableHTTPHandler(serverOf, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		MaxRequestBodyBytes: cfg.MaxRequestBytes,
		// Agent containers reach goald as host.docker.internal while goald may listen on
		// loopback; the SDK's DNS-rebinding guard would refuse that Host header. A rebinding
		// page cannot present a task token, which every request here must carry.
		DisableLocalhostProtection: true,
	})

	return g, nil
}

// EndRun stops the sidecars of a run that has finished and waits until they are gone. goald
// calls it when the run's agent task ends; ending a run without sidecars is a no-op.
func (g *Gateway) EndRun(_ context.Context, run domain.RunID) {
	if g.sidecars != nil {
		g.sidecars.end(run, false)
	}
}

// Close stops every sidecar and starts no more; goald calls it at shutdown, after the HTTP
// server stopped.
func (g *Gateway) Close() {
	if g.sidecars != nil {
		g.sidecars.end("", true)
	}
}

// errForbidden means a valid token asks for something the current definition does not grant.
var errForbidden = errors.New("forbidden")

// ServeHTTP authenticates the request, builds the task's view of its tools and hands the
// request to the SDK's streamable handler.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, err := g.authenticate(r)
	if err != nil {
		g.log.WarnContext(r.Context(), "gateway request rejected", "remote", r.RemoteAddr, "reason", err)
		w.Header().Set("WWW-Authenticate", `Bearer realm="goal"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)

		return
	}

	// The token's organization binds every store call of the request (its audit events).
	r = r.WithContext(domain.WithOrg(r.Context(), claims.OrgID))

	s, err := g.session(r.Context(), claims)
	switch {
	case errors.Is(err, errForbidden) || errors.Is(err, ports.ErrNotFound):
		g.log.WarnContext(r.Context(), "gateway request forbidden", claimAttrs(claims, "reason", err)...)
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	case err != nil:
		g.log.ErrorContext(r.Context(), "gateway session failed", claimAttrs(claims, "error", err)...)
		http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)

		return
	}

	g.mcp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serverKey{}, s.server())))
}

type serverKey struct{}

// serverOf returns the MCP server ServeHTTP built for this request.
func serverOf(r *http.Request) *mcp.Server {
	s, _ := r.Context().Value(serverKey{}).(*mcp.Server)

	return s
}

func (g *Gateway) authenticate(r *http.Request) (domain.TaskClaims, error) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return domain.TaskClaims{}, fmt.Errorf("%w: no bearer token", domain.ErrBadTaskToken)
	}

	return domain.VerifyTaskToken(g.cfg.Key, strings.TrimSpace(token), g.clock.Now())
}

// session resolves what the task may reach under the factory's current definition.
func (g *Gateway) session(ctx context.Context, c domain.TaskClaims) (*session, error) {
	f, err := g.defs.Factory(ctx, c.FactoryID)
	if err != nil {
		return nil, fmt.Errorf("load factory %s: %w", c.FactoryID, err)
	}

	var agent *config.Agent

	for i := range f.Spec.Agents {
		if f.Spec.Agents[i].Name == c.Agent {
			agent = &f.Spec.Agents[i]
		}
	}

	if agent == nil {
		return nil, fmt.Errorf("%w: agent %q is not in factory %s", errForbidden, c.Agent, c.FactoryID)
	}

	s := &session{g: g, claims: c, policy: newPolicy(f.Spec.Connections, agent)}
	s.grant(ctx, f.Spec.Connections, agent.Connections)

	return s, nil
}

func claimAttrs(c domain.TaskClaims, more ...any) []any {
	return append([]any{"factory", c.FactoryID, "agent", c.Agent, "run", c.RunID}, more...)
}
