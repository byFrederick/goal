// Package api serves goald's HTTP API, the contract in api/openapi.yaml: health, the install's
// organizations and, under /v1/orgs/{org}, each organization's factories, their boards and flow
// metrics, submitting and listing work items, reading items, their history and runs, human decisions
// (approve, reject, retry, merge), factory definition authoring (definitions.go: drafts,
// validation, publishing, versions, templates), a Server-Sent Events stream of the
// organization's events, and the factory editor's metadata and skills catalog (builder.go).
//
// The handlers implement the strict server interface generated into internal/api/gen
// (`task api:gen`). They reach the engine, the store, the organizations and the factory
// definitions only through the small interfaces below and Definitions; cmd/goald wires the real
// ones.
//
// Every request goes through the same layers: cross-origin protection (a browser may change
// things only from goald's own origin), a body limit, a request id (X-Request-Id), the request's
// actor (actor.go: always the local user in phase 2.2, which has no identity), then, per
// operation, the organization of the path (org.go: unknown or deleted is 404, and the request is
// bound to it with domain.WithOrg so row-level security scopes every store call) and the
// Authorizer seam (authz.go), passed again by the resolvers of factories, definitions, items and
// runs. Wherever a path takes an item, it takes its id or <factory>#<number>. Errors are RFC 9457
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
// No authentication yet: goald binds the API to 127.0.0.1.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
)

// Engine moves items; *engine.Engine satisfies it.
type Engine interface {
	Submit(ctx context.Context, n domain.NewItem) (domain.Item, error)
	Approve(ctx context.Context, id domain.ItemID, by domain.Actor) (domain.Item, error)
	Reject(ctx context.Context, id domain.ItemID, by domain.Actor, reason string) (domain.Item, error)
	Retry(ctx context.Context, id domain.ItemID, by domain.Actor) (domain.Item, error)
	Merge(ctx context.Context, id domain.ItemID, by domain.Actor, method string) (domain.Item, error)
	Note(ctx context.Context, id domain.ItemID, by domain.Actor, text string) (domain.Event, error)
}

// Items reads items, their history and runs, the leases on them, a factory's flow, an item's
// spans and run in progress, and what the organization's factories need from people; a store
// that implements ports.Store and ports.FlowStats satisfies it.
type Items interface {
	Item(ctx context.Context, id domain.ItemID) (domain.Item, error)
	ItemByNumber(ctx context.Context, factory domain.FactoryID, number int64) (domain.Item, error)
	ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error)
	Runs(ctx context.Context, item domain.ItemID, limit int) ([]domain.Run, error)
	Run(ctx context.Context, id domain.RunID) (domain.Run, error)
	ListItems(ctx context.Context, q ports.ItemQuery) ([]domain.Item, error)
	Leases(ctx context.Context, factory domain.FactoryID, station string) ([]domain.Lease, error)
	StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error)
	FlowMetrics(ctx context.Context, factory domain.FactoryID, since, until time.Time) (domain.FlowMetrics, error)
	Attention(ctx context.Context) ([]domain.Attention, error)
	WaitingHuman(ctx context.Context, factories []domain.FactoryID, limit int) ([]domain.Item, error)
	ItemSpans(ctx context.Context, id domain.ItemID) ([]domain.Span, error)
	CurrentRun(ctx context.Context, id domain.ItemID) (*domain.CurrentRun, error)
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
	// Authorizer decides every request (AllowAll in phase 2.2).
	Authorizer Authorizer
	// Feed serves /v1/orgs/{org}/stream; the caller runs it (Feed.Run).
	Feed *Feed
	// InboxSources add entries to /v1/orgs/{org}/inbox after the built-in decisions (shell.go);
	// later phases register theirs here.
	InboxSources []InboxSource
	// Builder serves the factory editor's metadata and the skills catalog.
	Builder Builder
	// Clock dates ages and metric windows and paces stream heartbeats.
	Clock  ports.Clock
	Logger *slog.Logger
}

// complete reports whether every required collaborator is set.
func (d Deps) complete() bool {
	missing := []bool{
		d.Engine == nil, d.Items == nil, d.Factories == nil, d.Definitions == nil, d.Versions == nil, d.Orgs == nil,
		d.Audit == nil, d.Authorizer == nil, d.Feed == nil, d.Builder == nil, d.Clock == nil, d.OrgRetention <= 0,
	}

	return !slices.Contains(missing, true)
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
		return nil, errors.New("api: Engine, Items, Factories, Definitions, Versions, Orgs, Audit, Authorizer, Feed, Builder, Clock " +
			"and a positive OrgRetention are required")
	}

	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	s := &server{
		engine: d.Engine, items: d.Items, factories: d.Factories, defs: d.Definitions, versions: d.Versions, orgs: d.Orgs, audit: d.Audit,
		retention: d.OrgRetention, authz: d.Authorizer, feed: d.Feed, builder: d.Builder, clock: d.Clock, log: log,
	}
	s.inbox = append([]InboxSource{decisions{s: s}}, d.InboxSources...)

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

	return cop.Handler(limitBodies(withRequestID(withActor(h)))), nil
}

// limitBodies refuses request bodies over the route's limit (413): the definitions routes take
// whole trees, every other route takes small JSON objects.
func limitBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxRequestBytes)
		if definitionRoute(r.URL.Path) {
			limit = maxDefinitionBytes
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
	feed      *Feed
	inbox     []InboxSource
	builder   Builder
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

	if types := f.Spec.Board.WorkTypes; len(types) > 0 && !slices.Contains(types, b.Type) {
		return nil, fmt.Errorf("%w: type %q is not one of factory %s's work types %v", errInvalid, b.Type, f.Metadata.Name, types)
	}

	it, err := s.engine.Submit(ctx, domain.NewItem{
		FactoryID: domain.FactoryID(f.Metadata.ID), Type: b.Type, Title: strings.TrimSpace(b.Title),
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
		page.Events = append(page.Events, eventOf(e))
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

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)

	if err := json.NewEncoder(w).Encode(p); err != nil {
		s.log.WarnContext(r.Context(), "write problem response", slog.Any("error", err))
	}
}

// problemOf maps an error to its problem details. An unknown and a deleted organization get the
// same body, so nobody learns which organizations once existed.
func problemOf(err error) gen.Problem {
	var tooLarge *http.MaxBytesError

	if p, ok := definitionProblem(err); ok {
		return p
	}

	switch {
	case errors.Is(err, errOrgNotFound):
		return gen.Problem{Title: "Organization not found", Status: http.StatusNotFound, Code: optional("org-not-found")}
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
	case errors.Is(err, ports.ErrNotFound):
		return detailed(http.StatusNotFound, err)
	case errors.Is(err, errForbidden), errors.Is(err, errCrossOrigin):
		p := detailed(http.StatusForbidden, err)
		p.Code = optional("forbidden")

		return p
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, engine.ErrNotWaitingHuman), errors.Is(err, engine.ErrNotBlocked),
		errors.Is(err, engine.ErrNotMergeStep), errors.Is(err, ports.ErrNotMergeable), errors.Is(err, engine.ErrItemClosed):
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

	if !it.DoneAt.IsZero() {
		out.DoneAt = &it.DoneAt
	}

	return out
}

func eventOf(e domain.Event) gen.Event {
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}

	return gen.Event{
		Id: e.ID, ItemId: string(e.ItemID), RunId: optional(string(e.RunID)), At: e.At, Kind: string(e.Kind),
		Station: optional(e.Station), Actor: e.Actor, Data: data,
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
