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
// declare the agent, otherwise 403, except a Jonah review's (below).
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
// Its rules are the factory rules of the run's version that apply to its agent, station and item's lane and work type (ADR 0014; Definitions.Rules), each with the hash of its
// rule file and the file the worker writes; the first bundle served to a run that any rule
// applies to appends domain.EventRulesApplied with their names and hashes, and a bundle fetched
// again appends none.
//
// # Knowledge
//
// The engine pins, at a claim's start, the newest snapshot of each knowledge source of the version
// that applies to the run (ADR 0038 decision 7), and the task token carries the pins. The bundle's
// spec.knowledge holds the run's Knowledge section (domain.KnowledgeIndex: the ground rules, one
// line per pinned source with its read-when line, file count and top folders read from its zip's
// central directory, the sources not synced yet and the factory memory) and, for each pin, the URL
// prepare downloads it from: GET /run/knowledge/<source> (ServeKnowledge), which streams the pinned
// zip and answers 404 for a source the token did not pin. An agent whose builtins.read is off gets
// no knowledge; a draft request's bundle gets the section and no snapshot. The first bundle served
// to a run with a pin appends domain.EventKnowledgePinned, once, as for rules.
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
// dots); a tool the run may not call, a connection's included, is refused like an unknown one.
//
// Every run is served question__ask (question.go, ADR 0018): the agent asks the person
// responsible for its item a question, 1 to 500 characters, with 0 to 4 quick-answer choices of
// 1 to 60 characters. The gateway checks the limits and passes the task's item, run and agent with
// the question to Questions (the engine's Ask, adapted by cmd/goald), which records it and ends
// the run; the agent is told to stop. Input outside the limits and the engine's refusals, such
// as a second open question, are tool errors with the reason.
//
// # Jonah reviews
//
// A token of kind domain.TaskKindJonah is a Jonah review's run (ADR 0034 decision 1, jonah.go).
// Its bundle's spec.jonah holds the version's files a proposal may edit, the factory's sent-back
// events in the token's window and its proposals rejected in the last 90 days, read through
// Deps.Jonah (503 when nil); it names no item, repository, connection, rule or knowledge source.
// Its MCP session needs no agent of the version, lists no tool and refuses every call.
//
// # Tries of past items
//
// A token of kind domain.TaskKindTry is a try of a past item (ADR 0039 decision 9, try.go). Its
// bundle is the station's on the version with the proposal's edits applied to it, from what
// the original run recorded: its brief, the item's earlier outputs and its commit. Its agent gets
// no connection, environment file or MCP server, and its environment's env is for setup alone.
// Its MCP session lists no tool, and it records no event on the past item.
//
// # Grades
//
// A token of kind domain.TaskKindGrade is a grade (ADR 0039 decision 10, grade.go): one model call
// that decides whether a result fixed the reason an item went back. It names the try and which
// result it grades, a try's or the item's original or final one. Its bundle's spec.grade holds the
// reason and that result's output and diff, read through Deps.Grades (503 when it or Deps.Jonah is
// nil), with the token's model and Jonah's budget left as max_usd; it has no item, repository,
// definition, file, connection, knowledge, rule or tool. Its MCP session lists no tool, refuses
// every call, and its notes are none.
//
// # Checks
//
// A token of kind domain.TaskKindCheck is a trigger's check (ADR 0040 decision 19, check.go). It
// names the trigger and no item or station. Its bundle is the check's agent with the built-ins
// that edit or run denied, its environment or connections with every ask tool off, the check's
// brief, the fixed answer contract as its output schema, check.maxUsd as max_usd, and spec.check:
// the input read through Deps.Checks (503 when nil). It has no repository, rule or knowledge. Its
// MCP session lists no tool, so question__ask and the knowledge tools are not offered.
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
const EventToolCall = domain.EventGatewayToolCall

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
	// VersionFiles returns every file of version id by its path in the definition, which a
	// Jonah review reads (definitions.Cache.VersionFiles).
	VersionFiles(ctx context.Context, id domain.DefinitionVersionID) (map[string][]byte, error)
}

// Events records audit events and reads an item's, to record a run's rules once; ports.Store
// satisfies it.
type Events interface {
	AppendEvents(ctx context.Context, events ...domain.Event) error
	ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error)
}

// Connections reads an organization's connections and kinds without their secret values, for the
// run bundle; ports.Connections satisfies it.
type Connections interface {
	Get(ctx context.Context, name string) (domain.Connection, error)
	Kind(ctx context.Context, name string) (domain.ConnectionKind, error)
}

// StandingRules reads an organization's standing rules, which the run bundle offers its tools
// that ask first; ports.StandingRules satisfies it.
type StandingRules interface {
	// StandingRules are the organization's standing rules, the revoked ones with includeRevoked.
	StandingRules(ctx context.Context, includeRevoked bool) ([]domain.StandingRule, error)
}

// Policies reads the organization policy of the organization in ctx, whose MCP tool mode cap the
// run bundle applies; ports.OrgPolicies satisfies it.
type Policies interface {
	Get(ctx context.Context) (domain.OrgPolicy, error)
}

// Deps are the gateway's collaborators. Logger may be nil (logs discarded).
type Deps struct {
	Definitions Definitions
	// Items gives the run bundle the item's brief, earlier outputs and branch.
	Items Items
	// Connections describe the run's organization connections in its bundle.
	Connections Connections
	// StandingRules mark the bundle's tools that ask first and have a standing rule.
	StandingRules StandingRules
	Events        Events
	Clock         ports.Clock
	Logger        *slog.Logger
	// Knowledge and Objects give runs the knowledge snapshots their tokens pin, and their prompt's
	// Knowledge section (knowledge.go); without Knowledge every pinned source reads as not synced,
	// and without Objects no snapshot is served and no top folder listed.
	Knowledge Knowledge
	Objects   Objects
	// Policies cap the tool modes of the run bundle's environment MCP servers.
	Policies Policies
	// Questions takes the questions agents ask with question__ask, which every run is served.
	Questions Questions
	// ToolApprovals take the Ask-first tool calls runs report at POST /run/tool-approval.
	ToolApprovals ToolApprovals
	// DraftRequests and Drafts give a draft request's run its request and the factory's draft
	// files (definitionBundle); without both, draft request tokens are refused.
	DraftRequests DraftRequests
	Drafts        Drafts
	// Jonah reads a Jonah review's sent-back events and rejected proposals for its bundle; nil
	// answers a jonah token's bundle with 503 and changes nothing for other tokens.
	Jonah Jonah
	// Watches reads the watches of the published proposals a review's bundle holds (ADR 0039
	// decisions 4 and 14); nil leaves the watch out of their files.
	Watches ProposalWatches
	// Tries and Proposed give a try of a past item its bundle (try.go): the try's record and its
	// version with the proposal's edits applied to it. Without both, or without Jonah, a try
	// token's bundle is 503.
	Tries    Tries
	Proposed ProposedDefinitions
	// Grades give a grade its reason and the result it grades (grade.go). Without it, or without
	// Jonah, a grade token's bundle is 503.
	Grades Grades
	// Checks give a check its input (check.go); without it a check token's bundle is 503.
	Checks Checks
}

// DraftRequests reads a draft request of the organization in ctx; ports.DraftRequests satisfies
// it.
type DraftRequests interface {
	Get(ctx context.Context, id domain.DraftRequestID) (domain.DraftRequest, error)
}

// Drafts reads a factory's open draft and the blobs of its files, and its published versions
// (a Jonah review's versions.md), for the organization in ctx; ports.DefinitionStore satisfies it.
// The gateway only reads them: writing a draft is authoring's.
type Drafts interface {
	Draft(ctx context.Context, f domain.FactoryID) (domain.DefinitionVersion, error)
	Blob(ctx context.Context, h domain.BlobHash) ([]byte, error)
	Versions(ctx context.Context, f domain.FactoryID, before, limit int) ([]domain.DefinitionVersion, error)
}

// Config bounds the gateway. Zero values take the defaults below.
type Config struct {
	// Key verifies task tokens: the resolved value of the engine's GatewayKeyRef.
	Key []byte
	// MaxRequestBytes caps one request from an agent (default 4 MiB).
	MaxRequestBytes int64
	// KnowledgeURLTTL, when above zero, gives each pinned snapshot in a run's bundle a signed
	// bucket URL valid that long (Objects.PresignGet), for boxes that reach the bucket: cloud ones
	// (ADR 0038 decision 7). Zero keeps KnowledgePath<source>, served by ServeKnowledge, for local
	// boxes that cannot reach the bucket.
	KnowledgeURLTTL time.Duration
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
	rules       StandingRules
	policies    Policies
	events      Events
	clock       ports.Clock
	log         *slog.Logger
	knowledge   Knowledge // nil without Deps.Knowledge
	objects     Objects   // nil without Deps.Objects
	questions   Questions
	approvals   ToolApprovals
	// draftRequests and drafts are nil without Deps.DraftRequests and Deps.Drafts.
	draftRequests DraftRequests
	drafts        Drafts
	jonah         Jonah // nil without Deps.Jonah
	watches       ProposalWatches
	// tries and proposed are nil without Deps.Tries and Deps.Proposed.
	tries    Tries
	proposed ProposedDefinitions
	grades   Grades // nil without Deps.Grades
	checks   Checks // nil without Deps.Checks
	cfg      Config
	mcp      http.Handler
}

// New returns a gateway; goald mounts it at its MCP path.
func New(d Deps, cfg Config) (*Gateway, error) {
	if d.Definitions == nil || d.Items == nil || d.Connections == nil || d.StandingRules == nil || d.Policies == nil || d.Events == nil || d.Clock == nil ||
		d.Questions == nil || d.ToolApprovals == nil {
		return nil, errors.New("gateway: Definitions, Items, Connections, StandingRules, Policies, Events, Clock, Questions and ToolApprovals are required")
	}

	if len(cfg.Key) == 0 {
		return nil, errors.New("gateway: a token key is required")
	}

	cfg.MaxRequestBytes = cmp.Or(cfg.MaxRequestBytes, defaultMaxRequestBytes)

	g := &Gateway{
		defs: d.Definitions, items: d.Items, connections: d.Connections, rules: d.StandingRules, policies: d.Policies, events: d.Events, clock: d.Clock, log: d.Logger,
		knowledge: d.Knowledge, objects: d.Objects, questions: d.Questions, approvals: d.ToolApprovals, draftRequests: d.DraftRequests, drafts: d.Drafts, jonah: d.Jonah,
		watches: d.Watches, tries: d.Tries, proposed: d.Proposed, grades: d.Grades, checks: d.Checks, cfg: cfg,
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
	// A draft request's run reaches no MCP server: definition mode has none.
	if c.Kind == domain.TaskKindDraftRequest {
		return nil, fmt.Errorf("%w: a draft request's task is served no tools", errForbidden)
	}

	f, err := g.defs.Version(ctx, c.VersionID)
	if err != nil {
		return nil, fmt.Errorf("load factory %s version %s: %w", c.FactoryID, c.VersionID, err)
	}

	if f.Metadata.ID != string(c.FactoryID) {
		return nil, fmt.Errorf("%w: version %s is not a version of factory %s", errForbidden, c.VersionID, c.FactoryID)
	}

	// A Jonah review's agent is not one of the factory's, and it is served no tool; nor is a try
	// of a past item, which has no person to ask and no connection, a grade, which calls none, or
	// a check, which has no person to ask and no knowledge (ADR 0040 decision 19).
	if c.Kind == domain.TaskKindJonah || c.Kind == domain.TaskKindTry || c.Kind == domain.TaskKindGrade || c.Kind == domain.TaskKindCheck {
		return &session{g: g, claims: c, noTools: true}, nil
	}

	if !slices.ContainsFunc(f.Spec.Agents, func(a config.Agent) bool { return a.Name == c.Agent }) {
		return nil, fmt.Errorf("%w: agent %q is not in factory %s", errForbidden, c.Agent, c.FactoryID)
	}

	return &session{g: g, claims: c}, nil
}

func claimAttrs(c domain.TaskClaims, more ...any) []any {
	return append([]any{"factory", c.FactoryID, "agent", c.Agent, "run", c.RunID}, more...)
}
