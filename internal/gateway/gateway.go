// Package gateway is Goal's MCP server for runs (spec 4 §5.9): the run bundle (GET /run) and one
// MCP endpoint (streamable HTTP) for Goal's own tools, authorized by the run's task token. It
// proxies no connection and adds no credential: a run uses its organization connections directly
// from its box, MCP servers included, with the env the engine gave it.
//
// # Auth
//
// Every request carries "Authorization: Bearer <task token>" signed by the engine
// (domain.SignTaskToken) for the audience goal-gateway (domain.TaskAudience). A missing, forged
// or expired token, or one for no or another audience, gets 401 with no detail. goald's API
// refuses task tokens, so they work only here. The
// claims name the factory, its definition version, the agent and the run; that version must
// declare the agent, otherwise 403.
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
// mentions. Its rules are the factory rules of the run's version that apply to its agent,
// station and item's lane and work type (ADR 0014; Definitions.Rules), each with the hash of its
// rule file and the file the worker writes; the first bundle served to a run that any rule
// applies to appends domain.EventRulesApplied with their names and hashes, and a bundle fetched
// again appends none.
//
// # Notes
//
// ServeRun answers GET /run?notes_after=<event id> (0 or a positive integer, else 400) with the
// notes people left on the token's item while its run works, which the worker polls between the
// agent's turns: {"notes":[{id, at, by, text}]}, the item's note events whose id is greater than
// both the run's own run_started and notes_after, oldest first, at most 50. A note from before
// the run started is never listed. It authorizes like the bundle (401, 403) and records nothing;
// the worker reports each note it gives the agent as a note_delivered line (domain.EventNoteDelivered).
//
// # Ask-first tool calls
//
// The bundle gives each connection the tools that ask first or are off for the run's agent
// (tool_modes, from domain.EffectiveToolModes with the organization's standing rules) and the
// asks that offer Always allow, and the spec the calls a person approved for the run
// (granted_calls, domain.GrantedCalls). ServeToolApproval answers POST /run/tool-approval
// (toolapproval.go, ADR 0031 decision 4): the run reports a call that asks first, and
// ToolApprovals (the engine's RequestToolApproval, adapted by cmd/goald) records it and ends the
// run. The input is never logged.
//
// # Goal's tools
//
// The endpoint serves Goal's own tools, named "<server>__<tool>" (Claude's tool names allow no
// dots); a tool the run may not call, a connection's included, is refused like an unknown one. A
// run whose definition version lists knowledge sources (spec.knowledge.sources) is served
// knowledge__search, knowledge__read and knowledge__verdict (knowledge.go). They search and read
// those sources only, in the token's organization only (row-level security), whatever the agent
// asks: a document of another source answers as one that does not exist. A search and a read
// record the run's use of each document they return, best effort, and knowledge__verdict records
// the agent's verdict on a document the run used (ADR 0027).
//
// Every run is served question__ask (question.go, ADR 0018): the agent asks the person
// responsible for its item a question, 1 to 500 characters, with 0 to 4 quick-answer choices of
// 1 to 60 characters. The gateway checks the limits and passes the task's item, run and agent with
// the question to Questions (the engine's Ask, adapted by cmd/goald), which records it and ends
// the run; the agent is told to stop. Input outside the limits and the engine's refusals, such
// as a second open question, are tool errors with the reason.
//
// # State
//
// None: every HTTP request is verified and served on its own (the SDK's stateless mode), so any
// goald serves any run.
//
// # Audit
//
// Every tools/call, including refused ones, appends an event (EventToolCall) with the tool, its
// duration, outcome and request and response sizes; never arguments or results.
package gateway

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
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
	// Rules returns the factory rules of version id that apply to run, in declaration order,
	// with their hash and content (definitions.Cache.Rules).
	Rules(ctx context.Context, id domain.DefinitionVersionID, run domain.RuleTarget) ([]domain.RunRule, error)
}

// Events records audit events and reads an item's, to record a run's rules once; ports.Store
// satisfies it.
type Events interface {
	AppendEvents(ctx context.Context, events ...domain.Event) error
	ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error)
}

// Connections reads an organization's connections and kinds without their secret values, and its
// standing rules, for the run bundle; ports.Connections satisfies it.
type Connections interface {
	Get(ctx context.Context, name string) (domain.Connection, error)
	Kind(ctx context.Context, name string) (domain.ConnectionKind, error)
	// StandingRules are the organization's standing rules, the revoked ones with includeRevoked.
	StandingRules(ctx context.Context, includeRevoked bool) ([]domain.StandingRule, error)
}

// Deps are the gateway's collaborators. Logger may be nil (logs discarded).
type Deps struct {
	Definitions Definitions
	// Items gives the run bundle the item's brief, earlier outputs and branch.
	Items Items
	// Connections describe the run's organization connections in its bundle.
	Connections Connections
	Events      Events
	Clock       ports.Clock
	Logger      *slog.Logger
	// Knowledge serves Goal's knowledge tools to runs whose factory lists knowledge sources; nil
	// serves none.
	Knowledge Knowledge
	// Questions takes the questions agents ask with question__ask, which every run is served.
	Questions Questions
	// ToolApprovals take the Ask-first tool calls runs report at POST /run/tool-approval.
	ToolApprovals ToolApprovals
}

// Config bounds the gateway. Zero values take the defaults below.
type Config struct {
	// Key verifies task tokens: the resolved value of the engine's GatewayKeyRef.
	Key []byte
	// MaxRequestBytes caps one request from an agent (default 4 MiB).
	MaxRequestBytes int64
}

const (
	defaultMaxRequestBytes = 4 << 20
	auditTimeout           = 5 * time.Second
)

// implementation identifies the gateway to agents.
var implementation = &mcp.Implementation{Name: "goal-gateway", Version: "1"}

// Gateway serves the MCP endpoint. It is an http.Handler, safe for concurrent use.
type Gateway struct {
	defs        Definitions
	items       Items
	connections Connections
	events      Events
	clock       ports.Clock
	log         *slog.Logger
	knowledge   Knowledge // nil without Deps.Knowledge
	questions   Questions
	approvals   ToolApprovals
	cfg         Config
	mcp         http.Handler
}

// New returns a gateway; goald mounts it at its MCP path.
func New(d Deps, cfg Config) (*Gateway, error) {
	if d.Definitions == nil || d.Items == nil || d.Connections == nil || d.Events == nil || d.Clock == nil || d.Questions == nil ||
		d.ToolApprovals == nil {
		return nil, errors.New("gateway: Definitions, Items, Connections, Events, Clock, Questions and ToolApprovals are required")
	}

	if len(cfg.Key) == 0 {
		return nil, errors.New("gateway: a token key is required")
	}

	cfg.MaxRequestBytes = cmp.Or(cfg.MaxRequestBytes, defaultMaxRequestBytes)

	g := &Gateway{
		defs: d.Definitions, items: d.Items, connections: d.Connections, events: d.Events, clock: d.Clock, log: d.Logger,
		knowledge: d.Knowledge, questions: d.Questions, approvals: d.ToolApprovals, cfg: cfg,
	}
	if g.log == nil {
		g.log = slog.New(slog.DiscardHandler)
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

	if !slices.ContainsFunc(f.Spec.Agents, func(a config.Agent) bool { return a.Name == c.Agent }) {
		return nil, fmt.Errorf("%w: agent %q is not in factory %s", errForbidden, c.Agent, c.FactoryID)
	}

	return &session{g: g, claims: c, sources: g.searchable(f.Line())}, nil
}

func claimAttrs(c domain.TaskClaims, more ...any) []any {
	return append([]any{"factory", c.FactoryID, "agent", c.Agent, "run", c.RunID}, more...)
}
