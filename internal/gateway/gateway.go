// Package gateway is Goal's MCP server for runs (spec 4 §5.9): the run bundle (GET /run) and one
// MCP endpoint (streamable HTTP) for Goal's own tools, authorized by the run's task token. It
// proxies no organization connection: a run uses those directly from its box, MCP servers
// included, with the env the engine gave it. Until the cutover (spec 4, 4-14) the endpoint also
// serves the remote MCP servers of inline connections in versions published before phase 4,
// whose credentials stay on the host: the gateway resolves them through ports.Secrets and adds
// them to upstream requests.
//
// # Auth
//
// Every request carries "Authorization: Bearer <task token>" signed by the engine
// (domain.SignTaskToken) for the audience goal-gateway (domain.TaskAudience). A missing, forged
// or expired token, or one for no or another audience, gets 401 with no detail. goald's API
// refuses task tokens, so they work only here. The
// claims name the factory, agent and run; the factory's current definition must still declare
// that agent, otherwise 403.
//
// # Run bundle
//
// ServeRun answers GET /run with the bundle of the run in the token, which goal-agent prepare
// writes to /goal in the run's sandbox: the task (spec.json: brief, earlier outputs, model, tool
// rules, limits, the agent's connections), the agent's prompt, skills and output schema from the
// definition version the run started on, the repository and the item's branch to check out, the
// git credentials to configure, each naming the environment variable that holds its password,
// and the tools to install. It describes the run's organization connections from what anyone may
// see of them (domain.ConnectionKind.Outline): env names, MCP servers whose headers name env
// variables (${NAME}, expanded by Claude Code in the box), git logins, CLI tools and docs. The
// bundle holds no secret, and it proxies nothing: everything it describes is used from the box.
// Its knowledge_sources name what Goal's knowledge tools search, which the worker's prompt
// mentions.
//
// # Goal's tools
//
// A run whose definition version lists knowledge sources (spec.knowledge.sources) is served
// knowledge__search and knowledge__read (knowledge.go) next to its connections' tools. They search
// and read those sources only, in the token's organization only (row-level security), whatever
// the agent asks: a document of another source answers as one that does not exist.
//
// # Inline connections and tools
//
// A task is granted the inline connections that are both in its token and in its agent's
// connections in the definition version. Their remote MCP servers (type mcp, transport http)
// are served; every other kind (builtin, aws, stdio) is reported unavailable, not an error.
// Organization connections are not listed: the worker configures their MCP servers in the box.
// Tools are exposed as "<connection>__<tool>"; connection names are slugs without underscores,
// so the first "__" always splits a name unambiguously.
//
// An agent's tools.allow and tools.deny entries of the form "<connection>__<tool pattern>",
// where <connection> is one of the factory's connections, apply to gateway tools (patterns as
// in path.Match, so "github__get_*"); the worker applies the same entries to the MCP servers of
// organization connections. Other entries (Read, Bash(git push*)) are the worker's business and
// are ignored here. Deny wins; a connection with at least one allow entry serves only the tools
// its allow entries match; a connection with none serves all its tools.
//
// # Secret policy
//
// Before it resolves a header value, the gateway checks each of its env:NAME references against
// Config.SecretPolicy, the same domain.SecretPolicy goald's credentials check validates
// definitions with: a reserved variable goes to no connection, and a host-bound one (the
// install's repository token) only in the headers of an https connection to one of the policy's
// hosts (GitHub's MCP server). A refused connection is unavailable (its tools are not listed and
// calling one is an error): the refused reference is never resolved, nothing is sent to its
// server, and the refusal is logged with the reference, never a value. This protects versions
// published before the credentials check refused them.
//
// # State
//
// None: every HTTP request is verified and served on its own (the SDK's stateless mode) and opens
// its own short sessions with remote servers, so any goald serves any run.
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

// Definitions returns a published definition version. The gateway reads the version a task's
// run started on (its token's v claim), so the run sees that version's agents and connections
// to the end. It is called on every request, so it should be cheap (goald backs it with a
// cache). A version that does not exist in the organization of ctx is an error wrapping
// ports.ErrNotFound.
type Definitions interface {
	Version(ctx context.Context, id domain.DefinitionVersionID) (*config.Factory, error)
	// AgentFiles returns an agent's prompt, skills and the output schema of contract output
	// from version id, by path under the agent's folder (definitions.Cache.AgentFiles).
	AgentFiles(ctx context.Context, id domain.DefinitionVersionID, agent domain.AgentSpec, output string) (map[string][]byte, error)
}

// Events records audit events; ports.Store satisfies it.
type Events interface {
	AppendEvents(ctx context.Context, events ...domain.Event) error
}

// Connections reads an organization's connections and kinds without their secret values, for the
// run bundle; ports.Connections satisfies it.
type Connections interface {
	Get(ctx context.Context, name string) (domain.Connection, error)
	Kind(ctx context.Context, name string) (domain.ConnectionKind, error)
}

// Deps are the gateway's collaborators. Transport may be nil (http.DefaultTransport); Logger
// may be nil (logs discarded).
type Deps struct {
	Definitions Definitions
	// Items gives the run bundle the item's brief, earlier outputs and branch.
	Items Items
	// Connections describe the run's organization connections in its bundle.
	Connections Connections
	Secrets     ports.Secrets
	Events      Events
	Clock       ports.Clock
	Logger      *slog.Logger
	// Transport carries upstream requests.
	Transport http.RoundTripper
	// Knowledge serves Goal's knowledge tools to runs whose factory lists knowledge sources; nil
	// serves none.
	Knowledge Knowledge
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
	// SecretPolicy says which env:NAME references a connection may send where; goald passes
	// the policy authoring validates with. The zero value restricts nothing.
	SecretPolicy domain.SecretPolicy
}

const (
	defaultCallTimeout      = 2 * time.Minute
	defaultListTimeout      = 30 * time.Second
	defaultMaxResponseBytes = 8 << 20
	defaultMaxRequestBytes  = 4 << 20
	auditTimeout            = 5 * time.Second
)

// implementation identifies the gateway to agents and to upstream servers.
var implementation = &mcp.Implementation{Name: "goal-gateway", Version: "1"}

// Gateway serves the MCP endpoint. It is an http.Handler, safe for concurrent use.
type Gateway struct {
	defs        Definitions
	items       Items
	connections Connections
	secrets     ports.Secrets
	events      Events
	clock       ports.Clock
	log         *slog.Logger
	transport   http.RoundTripper
	knowledge   Knowledge // nil without Deps.Knowledge
	cfg         Config
	mcp         http.Handler
}

// New returns a gateway; goald mounts it at its MCP path.
func New(d Deps, cfg Config) (*Gateway, error) {
	if d.Definitions == nil || d.Items == nil || d.Connections == nil || d.Secrets == nil || d.Events == nil || d.Clock == nil {
		return nil, errors.New("gateway: Definitions, Items, Connections, Secrets, Events and Clock are required")
	}

	if len(cfg.Key) == 0 {
		return nil, errors.New("gateway: a token key is required")
	}

	cfg.CallTimeout = cmp.Or(cfg.CallTimeout, defaultCallTimeout)
	cfg.ListTimeout = cmp.Or(cfg.ListTimeout, defaultListTimeout)
	cfg.MaxResponseBytes = cmp.Or(cfg.MaxResponseBytes, defaultMaxResponseBytes)
	cfg.MaxRequestBytes = cmp.Or(cfg.MaxRequestBytes, defaultMaxRequestBytes)

	g := &Gateway{
		defs: d.Definitions, items: d.Items, connections: d.Connections, secrets: d.Secrets, events: d.Events, clock: d.Clock,
		log: d.Logger, transport: d.Transport, knowledge: d.Knowledge, cfg: cfg,
	}
	if g.log == nil {
		g.log = slog.New(slog.DiscardHandler)
	}

	if g.transport == nil {
		g.transport = http.DefaultTransport
	}

	g.mcp = mcp.NewStreamableHTTPHandler(serverOf, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		MaxRequestBodyBytes: cfg.MaxRequestBytes,
		// The egress proxy reaches goald as host.docker.internal while goald may listen on
		// loopback; the SDK's DNS-rebinding guard would refuse that Host header. A rebinding
		// page cannot present a task token, which every request here must carry.
		DisableLocalhostProtection: true,
	})

	return g, nil
}

// errForbidden means a valid token asks for something the current definition does not grant.
var errForbidden = errors.New("forbidden")

// ServeHTTP authenticates the request, builds the task's view of its tools and hands the
// request to the SDK's streamable handler.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, err := g.authenticate(r)
	if err != nil {
		g.unauthorized(w, r, err)
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

// unauthorized answers a request without a valid task token: 401 with no detail.
func (g *Gateway) unauthorized(w http.ResponseWriter, r *http.Request, reason error) {
	g.log.WarnContext(r.Context(), "gateway request rejected", "path", r.URL.Path, "remote", r.RemoteAddr, "reason", reason)
	w.Header().Set("WWW-Authenticate", `Bearer realm="goal"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

func (g *Gateway) authenticate(r *http.Request) (domain.TaskClaims, error) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return domain.TaskClaims{}, fmt.Errorf("%w: no bearer token", domain.ErrBadTaskToken)
	}

	return domain.VerifyTaskToken(g.cfg.Key, strings.TrimSpace(token), g.clock.Now())
}

// session resolves what the task may reach under the definition version its run started on.
func (g *Gateway) session(ctx context.Context, c domain.TaskClaims) (*session, error) {
	f, err := g.defs.Version(ctx, c.VersionID)
	if err != nil {
		return nil, fmt.Errorf("load factory %s version %s: %w", c.FactoryID, c.VersionID, err)
	}

	if f.Metadata.ID != string(c.FactoryID) {
		return nil, fmt.Errorf("%w: version %s is not a version of factory %s", errForbidden, c.VersionID, c.FactoryID)
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

	s := &session{g: g, claims: c, policy: newPolicy(f.Spec.Connections, agent), sources: g.searchable(f.Line())}
	s.grant(ctx, f.Spec.Connections, agent.Connections)

	return s, nil
}

func claimAttrs(c domain.TaskClaims, more ...any) []any {
	return append([]any{"factory", c.FactoryID, "agent", c.Agent, "run", c.RunID}, more...)
}
