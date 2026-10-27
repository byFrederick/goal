// Package api serves goald's HTTP API, the contract in contracts/api/openapi.yaml: health, the install's
// organizations and, under /v1/orgs/{org}, each organization's factories, their boards and flow
// metrics, submitting and listing work items, reading items, their history and runs, human decisions
// (approve, reject, retry, merge) and controls (move, pause; control.go), factory definition authoring (definitions.go: drafts,
// validation, publishing, versions, templates), a Server-Sent Events stream of the
// organization's events, the factory editor's metadata and skills catalog (builder.go), and the
// organization's connections (connections.go) and knowledge base (knowledge.go).
//
// The handlers implement the strict server interface generated into internal/api/gen
// (`task api:gen`). They reach the engine, the store, the organizations and the factory
// definitions only through the small interfaces below and Definitions; cmd/goald wires the real
// ones.
//
// Every request goes through the same layers: the access log (accesslog.go: one line per
// request naming its principal and credential by id, never a secret), cross-origin protection (a browser may change
// things only from goald's own origin), a body limit, a request id (X-Request-Id), the request's
// credential (credential.go: the local user under GOAL_AUTH=none), then, per operation, its
// principal and the organization of the path (identity.go: under GOAL_AUTH=session an unknown,
// expired or revoked credential is 401 whatever the path, then an unknown or deleted organization
// is 404, and the request is bound to it with domain.WithOrg so row-level security scopes every
// store call) and the Authorizer (authz.go; Permissions under session), passed again by the
// resolvers of factories, definitions, items and runs. Lists and aggregates show only the
// factories the principal sees (permissions.go: visible). Wherever a path takes an item, it takes its id or <factory>#<number>. Errors are RFC 9457
// problem details: unknown organizations, items and factories are 404, invalid requests 400, a
// broken rule (a taken slug) or an action the item's state does not allow (a decision on an item
// not waiting for a human, a retry of an item not blocked, a merge that is not the item's step or
// that the pull request refuses) 409, a refusal of the Authorizer 403, a body over the limit 413;
// definition refusals carry the code and status authoring gives them (definitions.go); anything
// else is a 500 whose detail stays in the log.
//
// The stream is served from a Feed: one LISTEN per goald instance for item and organization
// events and one for definition changes, fanned out to that instance's clients of each
// organization, so any instance serves any client.
//
// GOAL_AUTH=none, one developer's laptop, makes every request the local user's on a loopback
// listener; GOAL_AUTH=session resolves each request's session or API key in the database
// (spec 3, ADR 0007).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Engine moves items; *engine.Engine satisfies it.
type Engine interface {
	Submit(ctx context.Context, n domain.NewItem) (domain.Item, error)
	Approve(ctx context.Context, id domain.ItemID, by domain.Actor) (domain.Item, error)
	Reject(ctx context.Context, id domain.ItemID, by domain.Actor, reason string) (domain.Item, error)
	Retry(ctx context.Context, id domain.ItemID, by domain.Actor) (domain.Item, error)
	Move(ctx context.Context, id domain.ItemID, to string, by domain.Actor, reason string) (domain.Item, error)
	Pause(ctx context.Context, id domain.ItemID, by domain.Actor, reason string) (domain.Item, error)
	Cancel(ctx context.Context, id domain.ItemID, by domain.Actor, reason string) (domain.Item, error)
	Merge(ctx context.Context, id domain.ItemID, by domain.Actor, method string) (domain.Item, error)
	Note(ctx context.Context, id domain.ItemID, by domain.Actor, text string) (domain.Event, error)
	Link(ctx context.Context, id, needs domain.ItemID, by domain.Actor) (domain.Edge, error)
	Unlink(ctx context.Context, id, needs domain.ItemID, by domain.Actor) error
	AddPart(ctx context.Context, assembly domain.ItemID, by domain.Actor, part domain.NewPart) (domain.Item, domain.Parts, error)
	PullAndon(ctx context.Context, f domain.FactoryID, by domain.Actor, reason string) (domain.AndonPull, error)
	ClearAndon(ctx context.Context, f domain.FactoryID, by domain.Actor, note string) (domain.AndonPull, error)
}

// Items reads items, their history and runs, a factory's latest milestone events, the leases on them, a factory's flow and its
// stations' times, an item's spans and run in progress, what the runs in progress have done so far, the
// human touches, and what the organization's factories need from people, and sets a touch's
// reason; a store that implements ports.Store and ports.FlowStats satisfies it.
type Items interface {
	Item(ctx context.Context, id domain.ItemID) (domain.Item, error)
	ItemByNumber(ctx context.Context, factory domain.FactoryID, number int64) (domain.Item, error)
	ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error)
	FactoryActivity(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.ActivityEvent, error)
	Runs(ctx context.Context, item domain.ItemID, limit int) ([]domain.Run, error)
	Run(ctx context.Context, id domain.RunID) (domain.Run, error)
	OrgRuns(ctx context.Context, q ports.RunQuery) ([]domain.ListedRun, domain.RunSummary, error)
	ListItems(ctx context.Context, q ports.ItemQuery) ([]domain.Item, error)
	SearchItems(ctx context.Context, q ports.ItemSearch) ([]domain.Item, error)
	Assemblies(ctx context.Context, factory domain.FactoryID, ids []domain.ItemID) (map[domain.ItemID]domain.Assembly, error)
	Leases(ctx context.Context, factory domain.FactoryID, station string) ([]domain.Lease, error)
	StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error)
	FlowMetrics(ctx context.Context, q ports.FlowQuery, longWaitsAt string) (domain.FlowMetrics, error)
	DoneByBucket(ctx context.Context, q ports.FlowQuery, d time.Duration, n int) ([]int, error)
	SpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error)
	ItemTimes(ctx context.Context, q ports.FlowQuery, limit int) ([]domain.ItemTime, error)
	Attention(ctx context.Context) ([]domain.Attention, error)
	InboxItems(ctx context.Context, factories []domain.FactoryID, limit int) ([]domain.AttentionItem, error)
	ItemSpans(ctx context.Context, id domain.ItemID) ([]domain.Span, error)
	CurrentRun(ctx context.Context, id domain.ItemID) (*domain.CurrentRun, error)
	LiveRuns(ctx context.Context, factory domain.FactoryID) (map[domain.ItemID]domain.LiveRun, error)
	StationTimes(ctx context.Context, factory domain.FactoryID, since, until time.Time) ([]domain.StationTimes, time.Time, error)
	OpenAndon(ctx context.Context, factory domain.FactoryID) (*domain.AndonPull, error)
	AndonPulls(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.AndonPull, error)
	GateDecisions(ctx context.Context, factory domain.FactoryID, station string, after time.Time, n int) (domain.GateTally, error)
	AgentStats(ctx context.Context, factory domain.FactoryID, agent string, since, until time.Time) (domain.AgentStats, error)
	Touches(ctx context.Context, q ports.FlowQuery) (domain.TouchStats, error)
	OrgTouches(ctx context.Context, factories []domain.FactoryID, q ports.FlowQuery) (domain.TouchStats, []domain.FactoryTouchStats, error)
	TouchesByBucket(ctx context.Context, q ports.FlowQuery, d time.Duration, n int) ([]domain.TouchStats, error)
	ItemTouches(ctx context.Context, item domain.ItemID) ([]domain.Touch, error)
	TouchCounts(ctx context.Context, items []domain.ItemID) (map[domain.ItemID]int, error)
	LabelTouch(ctx context.Context, event int64, reason domain.TouchReason, by domain.Actor) error
	Parts(ctx context.Context, parent domain.ItemID) (domain.Parts, error)
	Edges(ctx context.Context, item domain.ItemID) (domain.ItemEdges, error)
	OpenNeeds(ctx context.Context, factory domain.FactoryID, ids []domain.ItemID) (map[domain.ItemID][]domain.OpenNeed, error)
}

// Factories are the definitions goald runs; *definitions.Cache satisfies it.
type Factories interface {
	// Factories lists the factories of the organization in ctx.
	Factories(ctx context.Context) []*config.Factory
	// Lookup finds a factory of the organization in ctx by id or name, or returns an error
	// wrapping ports.ErrNotFound.
	Lookup(ctx context.Context, ref string) (*config.Factory, error)
}

// Deps are the API's collaborators. Logger may be nil.
type Deps struct {
	Engine    Engine
	Items     Items
	Factories Factories
	// Definitions authors factory definitions (*authoring.Service).
	Definitions Definitions
	// Versions numbers the definition versions runs ran on.
	Versions Versions
	Orgs     Orgs
	Audit    AuditLog
	// OrgRetention is how long a deleted organization is kept before the purge removes it
	// (GOAL_ORG_RETENTION); a deletion answers when that will be.
	OrgRetention time.Duration
	// Authorizer decides every request: AllowAll under GOAL_AUTH=none only, else Permissions.
	Authorizer Authorizer
	// Auth is how requests are authenticated (GOAL_AUTH): Mode is required.
	Auth AuthConfig
	// Identity resolves each request's credential under AuthSession; unused under AuthNone.
	Identity Identity
	// Access keeps the organization's people and keys.
	Access Access
	// Platform keeps the platform admins and the security log.
	Platform Platform
	// TrustedProxies are the proxies in front of goald whose X-Forwarded-For names the client
	// (GOAL_TRUSTED_PROXIES), as for the /api/auth proxy.
	TrustedProxies []netip.Prefix
	// Feed serves /v1/orgs/{org}/stream; the caller runs it (Feed.Run).
	Feed *Feed
	// InboxSources add entries to /v1/orgs/{org}/inbox after the built-in work entries (inbox.go);
	// later phases register theirs here.
	InboxSources []InboxSource
	// Builder serves the factory editor's metadata and the skills catalog.
	Builder Builder
	// Connections are the organization's connections and kinds (spec 4); ParseKind reads an
	// organization's kind file as goald's own (connkind.Parse); ConnectionTransport sends
	// connection tests: it must dial public addresses only (egress.PublicDialer).
	Connections         ports.Connections
	ParseKind           KindParser
	ConnectionTransport http.RoundTripper
	// CodeHosts are the code host adapters by the name a connection kind gives (KindSpec.CodeHost),
	// as the engine has them; they list what a connection reaches. Without one, no kind is a code
	// host here.
	CodeHosts map[string]ports.CodeHost
	// Knowledge is the organization's knowledge base (spec 4 §4.8); KnowledgeUploads keeps its
	// uploaded files, of at most UploadMaxBytes each (GOAL_UPLOAD_MAX_BYTES); KnowledgeSearch
	// searches it as the gateway's knowledge__search does.
	Knowledge        ports.Knowledge
	KnowledgeUploads KnowledgeUploads
	KnowledgeSearch  KnowledgeSearch
	UploadMaxBytes   int64
	// Clock dates ages and metric windows and paces stream heartbeats.
	Clock  ports.Clock
	Logger *slog.Logger
}

// complete reports whether every required collaborator is set.
func (d Deps) complete() bool {
	missing := []bool{
		d.Engine == nil, d.Items == nil, d.Factories == nil, d.Definitions == nil, d.Versions == nil, d.Orgs == nil,
		d.Audit == nil, d.Access == nil, d.Authorizer == nil, d.Platform == nil, d.Feed == nil, d.Builder == nil, d.Clock == nil,
		d.Connections == nil, d.ParseKind == nil, d.ConnectionTransport == nil, d.OrgRetention <= 0,
		d.Knowledge == nil, d.KnowledgeUploads == nil, d.KnowledgeSearch == nil, d.UploadMaxBytes <= 0,
	}

	return !slices.Contains(missing, true)
}

// failsClosed refuses an authentication setup that could let a request through unchecked: a mode
// other than none or session, session without an Identity or with AllowAll.
func (d Deps) failsClosed() error {
	switch d.Auth.Mode {
	case AuthNone:
		return nil
	case AuthSession:
		if _, all := d.Authorizer.(AllowAll); all || d.Identity == nil {
			return errors.New("api: GOAL_AUTH=session needs an Identity and an Authorizer that is not AllowAll")
		}

		return nil
	default:
		return fmt.Errorf("api: auth mode %q is not none or session", d.Auth.Mode)
	}
}

// Limits on requests.
const (
	// maxRequestBytes bounds every request body but the definitions routes'.
	maxRequestBytes = 1 << 20
	// maxDefinitionBytes bounds the bodies of the definitions routes: a whole tree is up to
	// 16 MiB, which base64 in JSON makes about a third larger. authoring refuses more than 16 MiB
	// of files, and more than 1 MiB per file.
	maxDefinitionBytes = 24 << 20
	defaultEventLimit  = 200
	maxEventLimit      = 1000
)

// errInvalid marks a request the API refuses as malformed (400).
var errInvalid = errors.New("invalid request")

// New returns the API's handler for the /v1/ routes.
func New(d Deps) (http.Handler, error) {
	if !d.complete() {
		return nil, errors.New("api: Engine, Items, Factories, Definitions, Versions, Orgs, Audit, Access, Authorizer, Platform, Feed, Builder, Clock, " +
			"Connections, ParseKind, ConnectionTransport, Knowledge, KnowledgeUploads, KnowledgeSearch, a positive UploadMaxBytes " +
			"and a positive OrgRetention are required")
	}

	if err := d.failsClosed(); err != nil {
		return nil, err
	}

	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	s := &server{
		engine: d.Engine, items: d.Items, factories: d.Factories, defs: d.Definitions, versions: d.Versions, orgs: d.Orgs, audit: d.Audit,
		retention: d.OrgRetention, authz: d.Authorizer, auth: d.Auth, identity: d.Identity, access: d.Access, platform: d.Platform, feed: d.Feed,
		builder: d.Builder, connections: d.Connections, parseKind: d.ParseKind, codehosts: d.CodeHosts,
		connectionClient: &http.Client{Transport: d.ConnectionTransport, CheckRedirect: noRedirects, Timeout: connectionTestTimeout},
		knowledge:        d.Knowledge, uploads: d.KnowledgeUploads, search: d.KnowledgeSearch, uploadMax: d.UploadMaxBytes,
		clock: d.Clock, log: log,
	}
	s.inbox = append([]InboxSource{workInbox{s: s}}, d.InboxSources...)

	strict := gen.NewStrictHandlerWithOptions(s, []gen.StrictMiddlewareFunc{s.scope}, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.problem(w, r, fmt.Errorf("%w: %w", errInvalid, err))
		},
		ResponseErrorHandlerFunc: s.problem,
	})

	// Paths that are no route answer a problem too, as an unknown resource.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.problem(w, r, fmt.Errorf("no route %s %s: %w", r.Method, r.URL.Path, ports.ErrNotFound))
	})

	h := gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.problem(w, r, fmt.Errorf("%w: %w", errInvalid, err))
		},
	})

	// A browser page of another origin may read nothing it is not allowed to and change nothing:
	// unsafe methods must come from goald's own origin (Sec-Fetch-Site, else Origin = Host).
	// Clients without those headers (the CLI) pass.
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.problem(w, r, errCrossOrigin)
	}))

	return s.accessLog(cop.Handler(limitBodies(d.UploadMaxBytes, withRequestID(withClientAddr(d.TrustedProxies, s.withCredential(h)))))), nil
}

// limitBodies refuses request bodies over the route's limit (413): the definitions routes take
// whole trees, the knowledge upload route files of up to uploadMax bytes, every other route
// takes small JSON objects.
func limitBodies(uploadMax int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxRequestBytes)

		switch {
		case definitionRoute(r.URL.Path):
			limit = maxDefinitionBytes
		case uploadRoute(r):
			limit = uploadMax + uploadSlack
		}

		r2 := *r
		r2.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, &r2)
	})
}

// definitionRoute reports whether path is one of the definitions routes that take files:
// /v1/orgs/{org}/definitions/…, POST /v1/orgs/{org}/factories (an import) and
// /v1/orgs/{org}/factories/{factory}/draft/….
func definitionRoute(path string) bool {
	rest, ok := strings.CutPrefix(path, "/v1/orgs/")
	if !ok {
		return false
	}

	parts := strings.Split(rest, "/") // org, collection, …
	if len(parts) < 2 {
		return false
	}

	switch parts[1] {
	case "definitions":
		return true
	case "factories":
		return len(parts) == 2 || (len(parts) >= 4 && parts[3] == "draft")
	default:
		return false
	}
}

// uploadRoute reports whether r uploads knowledge documents:
// POST /v1/orgs/{org}/knowledge/sources/{source}/documents.
func uploadRoute(r *http.Request) bool {
	rest, ok := strings.CutPrefix(r.URL.Path, "/v1/orgs/")
	if !ok || r.Method != http.MethodPost {
		return false
	}

	parts := strings.Split(rest, "/") // org, knowledge, sources, source, documents

	return len(parts) == 5 && parts[1] == "knowledge" && parts[2] == "sources" && parts[4] == "documents"
}

// errCrossOrigin marks an unsafe request from another origin, which cross-origin protection
// refuses (403).
var errCrossOrigin = errors.New("cross-origin request refused: change things from goald's own origin")

type server struct {
	engine    Engine
	items     Items
	factories Factories
	defs      Definitions
	versions  Versions
	orgs      Orgs
	audit     AuditLog
	retention time.Duration
	authz     Authorizer
	auth      AuthConfig
	identity  Identity
	access    Access
	platform  Platform
	feed      *Feed
	inbox     []InboxSource
	builder   Builder
	// connections and parseKind serve the connection routes; connectionClient sends their tests.
	connections      ports.Connections
	parseKind        KindParser
	connectionClient *http.Client
	// codehosts list what code host connections reach.
	codehosts map[string]ports.CodeHost
	// knowledge, uploads and search serve the knowledge routes; uploadMax bounds an uploaded file.
	knowledge ports.Knowledge
	uploads   KnowledgeUploads
	search    KnowledgeSearch
	uploadMax int64
	clock     ports.Clock
	log       *slog.Logger
}

var _ gen.StrictServerInterface = (*server)(nil)

func (s *server) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{Status: "ok"}, nil
}

func (s *server) SubmitItem(ctx context.Context, req gen.SubmitItemRequestObject) (gen.SubmitItemResponseObject, error) {
	b := req.Body

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(b.Title) == "" {
		return nil, fmt.Errorf("%w: an item needs a title", errInvalid)
	}

	typ := deref(b.Type)
	if types := f.Spec.Board.WorkTypes; len(types) > 0 {
		if typ == "" {
			typ = types[0]
		}

		if !slices.Contains(types, typ) {
			return nil, fmt.Errorf("%w: type %q is not one of factory %s's work types %v", errInvalid, typ, f.Metadata.Name, types)
		}
	}

	it, err := s.engine.Submit(ctx, domain.NewItem{
		FactoryID: domain.FactoryID(f.Metadata.ID), Type: typ, Title: strings.TrimSpace(b.Title),
		Body: deref(b.Body), Lane: deref(b.Lane), Priority: deref(b.Priority),
	})
	if err != nil {
		return nil, err
	}

	return gen.SubmitItem201JSONResponse(s.itemOf(ctx, it)), nil
}

func (s *server) GetItem(ctx context.Context, req gen.GetItemRequestObject) (gen.GetItemResponseObject, error) {
	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	out := s.itemOf(ctx, it)
	if out.Flow, err = s.itemFlow(ctx, it); err != nil {
		return nil, err
	}

	touches, err := s.items.ItemTouches(ctx, it.ID)
	if err != nil {
		return nil, err
	}

	out.Touches = s.itemTouchesOf(touches)

	needs, dependents, err := s.itemLinks(ctx, it.ID)
	if err != nil {
		return nil, err
	}

	out.Needs, out.Dependents = &needs, &dependents

	if out.Assembly, err = s.assemblyOf(ctx, it); err != nil {
		return nil, err
	}

	waiting, err := s.waiting(ctx, it.FactoryID, []domain.Item{it}, func(n int64) string { return s.ref(ctx, it.FactoryID, n) })
	if err != nil {
		return nil, err
	}

	out.Waiting = waiting[it.ID]

	return gen.GetItem200JSONResponse(out), nil
}

func (s *server) ListItemEvents(ctx context.Context, req gen.ListItemEventsRequestObject) (gen.ListItemEventsResponseObject, error) {
	after, limit := deref(req.Params.After), defaultEventLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if after < 0 || limit < 1 || limit > maxEventLimit {
		return nil, fmt.Errorf("%w: after must be ≥ 0 and limit in [1, %d]", errInvalid, maxEventLimit)
	}

	// Unknown items are 404, not an empty history.
	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	events, err := s.items.ItemEvents(ctx, it.ID, after, limit)
	if err != nil {
		return nil, err
	}

	page := gen.ListItemEvents200JSONResponse{Events: make([]gen.Event, 0, len(events)), NextAfter: after}
	for _, e := range events {
		page.Events = append(page.Events, s.eventOf(e))
		page.NextAfter = e.ID
	}

	return page, nil
}

func (s *server) ApproveItem(ctx context.Context, req gen.ApproveItemRequestObject) (gen.ApproveItemResponseObject, error) {
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Approve(ctx, it.ID, by); err != nil {
		return nil, err
	}

	return gen.ApproveItem200JSONResponse(s.itemOf(ctx, it)), nil
}

func (s *server) RejectItem(ctx context.Context, req gen.RejectItemRequestObject) (gen.RejectItemResponseObject, error) {
	reason := strings.TrimSpace(req.Body.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: a rejection needs a reason", errInvalid)
	}

	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Reject(ctx, it.ID, by, reason); err != nil {
		return nil, err
	}

	return gen.RejectItem200JSONResponse(s.itemOf(ctx, it)), nil
}

func (s *server) RetryItem(ctx context.Context, req gen.RetryItemRequestObject) (gen.RetryItemResponseObject, error) {
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Retry(ctx, it.ID, by); err != nil {
		return nil, err
	}

	return gen.RetryItem200JSONResponse(s.itemOf(ctx, it)), nil
}

func (s *server) MergeItem(ctx context.Context, req gen.MergeItemRequestObject) (gen.MergeItemResponseObject, error) {
	if b := req.Body; b != nil && b.Method != nil && *b.Method != gen.Squash {
		return nil, fmt.Errorf("%w: merge method %q is not squash", errInvalid, *b.Method)
	}

	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Merge(ctx, it.ID, by, engine.MergeSquash); err != nil {
		return nil, err
	}

	return gen.MergeItem200JSONResponse(s.itemOf(ctx, it)), nil
}

// decide resolves the item a decision is about, and who decides: the request's actor.
func (s *server) decide(ctx context.Context, ref string) (domain.Actor, domain.Item, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return domain.Actor{}, domain.Item{}, errors.New("decision: the request has no actor")
	}

	it, err := s.item(ctx, ref)

	return by, it, err
}

// problem writes err as problem details with the status its kind maps to.
func (s *server) problem(w http.ResponseWriter, r *http.Request, err error) {
	p := problemOf(err)
	if p.Status >= http.StatusInternalServerError {
		s.log.ErrorContext(r.Context(), "api request failed", slog.String("method", r.Method),
			slog.String("path", r.URL.Path), slog.String("request_id", w.Header().Get(requestIDHeader)), slog.Any("error", err))
	}

	if p.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="goal"`)
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)

	if err := json.NewEncoder(w).Encode(p); err != nil {
		s.log.WarnContext(r.Context(), "write problem response", slog.Any("error", err))
	}
}

// problemOf maps an error to its problem details. An unknown and a deleted organization get the
// same body, so nobody learns which organizations once existed; so do an unknown factory, item or
// run and one the principal may not see, and every credential refusal.
func problemOf(err error) gen.Problem {
	var tooLarge *http.MaxBytesError

	// The answers with a code of their own, the fixed bodies first.
	// The connection and knowledge routes' answers come before the definitions' (all wrap
	// ports.ErrNameTaken).
	for _, answer := range []func(error) (gen.Problem, bool){
		fixedProblem, edgeProblem, partProblem, connectionProblem, knowledgeProblem, definitionProblem, controlProblem, agentProblem, apiKeyProblem, personalTokenProblem,
	} {
		if p, ok := answer(err); ok {
			return p
		}
	}

	switch {
	case errors.Is(err, domain.ErrSkillNotFound):
		p := detailed(http.StatusNotFound, err)
		p.Code = optional("skill-not-found")

		return p
	case errors.Is(err, domain.ErrCatalogUnavailable):
		detail := "the skills catalog cannot be read now; see goald's log"

		return gen.Problem{
			Title: http.StatusText(http.StatusServiceUnavailable), Status: http.StatusServiceUnavailable, Detail: &detail,
			Code: optional("catalog-unavailable"),
		}
	case errors.Is(err, errPreconditionRequired):
		p := detailed(http.StatusPreconditionRequired, err)
		p.Code = optional("precondition-required")

		return p
	case errors.Is(err, errFactoryArchived):
		detail := archivedDetail

		return gen.Problem{
			Title: http.StatusText(http.StatusConflict), Status: http.StatusConflict, Detail: &detail,
			Code: optional("factory-archived"),
		}
	case errors.Is(err, errTouchNotFound):
		p := detailed(http.StatusNotFound, err)
		p.Code = optional("touch-not-found")

		return p
	case errors.Is(err, ports.ErrNotFound):
		return detailed(http.StatusNotFound, err)
	case errors.Is(err, errForbidden), errors.Is(err, errCrossOrigin):
		p := detailed(http.StatusForbidden, err)
		p.Code = optional("forbidden")

		return p
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, engine.ErrNotWaitingHuman), errors.Is(err, engine.ErrNotBlocked),
		errors.Is(err, engine.ErrNotMergeStep), errors.Is(err, ports.ErrNotMergeable), errors.Is(err, engine.ErrNoCodeHost),
		errors.Is(err, engine.ErrItemClosed), errors.Is(err, engine.ErrAndonPulled), errors.Is(err, engine.ErrAndonNotPulled):
		p := detailed(http.StatusConflict, err)
		p.Code = optional(problemCode(err))

		return p
	case errors.As(err, &tooLarge):
		p := detailed(http.StatusRequestEntityTooLarge, err)
		p.Code = optional(authoring.CodeTooLarge)

		return p
	case errors.Is(err, errInvalid):
		return detailed(http.StatusBadRequest, err)
	default:
		detail := "see goald's log"

		return gen.Problem{Title: http.StatusText(http.StatusInternalServerError), Status: http.StatusInternalServerError, Detail: &detail}
	}
}

// fixedProblem is the one body, without detail, of the answers that must not tell anything
// apart: whether an organization, factory, item or run exists, or why a credential failed.
func fixedProblem(err error) (gen.Problem, bool) {
	fixed := []struct {
		err    error
		status int
		title  string
		code   string
	}{
		{err: domain.ErrUnauthenticated, status: http.StatusUnauthorized, title: "Not signed in", code: "unauthenticated"},
		{err: domain.ErrReauthRequired, status: http.StatusForbidden, title: "Sign in again to do this", code: "reauth-required"},
		{err: errOrgNotFound, status: http.StatusNotFound, title: "Organization not found", code: "org-not-found"},
		{err: errFactoryNotFound, status: http.StatusNotFound, title: "Factory not found", code: "factory-not-found"},
		{err: errItemNotFound, status: http.StatusNotFound, title: "Item not found", code: "item-not-found"},
		{err: errRunNotFound, status: http.StatusNotFound, title: "Run not found", code: "run-not-found"},
		{err: errDeviceCodeNotFound, status: http.StatusNotFound, title: "Code not valid", code: "device-code-not-found"},
	}

	for _, f := range fixed {
		if errors.Is(err, f.err) {
			return gen.Problem{Title: f.title, Status: f.status, Code: optional(f.code)}, true
		}
	}

	return gen.Problem{}, false
}

func detailed(status int, err error) gen.Problem {
	detail := err.Error()

	return gen.Problem{Title: http.StatusText(status), Status: status, Detail: &detail}
}

func actionOf(a string) *gen.StationAction {
	if a == "" {
		return nil
	}

	action := gen.StationAction(a)

	return &action
}

// ref writes an item's reference with its factory's current name, or its id when the factory
// is not loaded.
func (s *server) ref(ctx context.Context, factory domain.FactoryID, number int64) string {
	name := string(factory)

	for _, f := range s.factories.Factories(ctx) {
		if f.Metadata.ID == string(factory) {
			name = f.Metadata.Name
			break
		}
	}

	return domain.ItemRef{Factory: name, Number: number}.String()
}

func (s *server) itemOf(ctx context.Context, it domain.Item) gen.Item {
	out := gen.Item{
		Id: string(it.ID), Ref: s.ref(ctx, it.FactoryID, it.Number), FactoryId: string(it.FactoryID), Number: it.Number,
		ParentId: optional(string(it.ParentID)), Type: it.Type, Lane: optional(it.Lane), Title: it.Title, Body: it.Body,
		Station: it.Station, State: gen.ItemState(it.State), ClassOfService: gen.ClassOfService(it.Class), Priority: it.Priority,
		ReworkCount: it.ReworkCount, CostUsd: it.CostUSD, Outputs: map[string]any{},
		CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}

	for station, raw := range it.Outputs {
		out.Outputs[station] = raw
	}

	if it.PartKey != "" {
		out.PartKey, out.PartPaths = &it.PartKey, new(append([]string{}, it.PartPaths...))
	}

	if !it.DoneAt.IsZero() {
		out.DoneAt = &it.DoneAt
	}

	return out
}

func (s *server) eventOf(e domain.Event) gen.Event {
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}

	return gen.Event{
		Id: e.ID, ItemId: string(e.ItemID), RunId: optional(string(e.RunID)), At: e.At, Kind: string(e.Kind),
		Station: optional(e.Station), Actor: s.eventActor(e), Data: data,
	}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}
