// Package api serves goald's HTTP API, the contract in api/openapi.yaml: health, the factories
// goald runs, their boards and flow metrics, submitting and listing work items, reading items
// and their history, human decisions (approve, reject, retry, merge) and a Server-Sent Events
// stream of item events.
//
// The handlers implement the strict server interface generated into internal/api/gen
// (`task api:gen`). They reach the engine, the store and the factory definitions only through
// the small interfaces below; cmd/goald wires the real ones. Wherever a path takes an item,
// it takes its id or <factory>#<number>. Errors are RFC 9457 problem details: unknown items
// and factories are 404, invalid requests 400, an action the item's state does not allow (a
// decision on an item not waiting for a human, a retry of an item not blocked, a merge that is
// not the item's step or that the pull request refuses) 409; anything else is a 500 whose
// detail stays in the log.
//
// The stream is served from a Feed: one LISTEN per goald instance, fanned out to that
// instance's clients, so any instance serves any client.
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
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
)

// Engine moves items; *engine.Engine satisfies it.
type Engine interface {
	Submit(ctx context.Context, n domain.NewItem) (domain.Item, error)
	Approve(ctx context.Context, id domain.ItemID, by string) (domain.Item, error)
	Reject(ctx context.Context, id domain.ItemID, by, reason string) (domain.Item, error)
	Retry(ctx context.Context, id domain.ItemID, by string) (domain.Item, error)
	Merge(ctx context.Context, id domain.ItemID, by, method string) (domain.Item, error)
}

// Items reads items, their history, the leases on them and a factory's flow; ports.Store
// satisfies it.
type Items interface {
	Item(ctx context.Context, id domain.ItemID) (domain.Item, error)
	ItemByNumber(ctx context.Context, factory domain.FactoryID, number int64) (domain.Item, error)
	ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error)
	ListItems(ctx context.Context, q ports.ItemQuery) ([]domain.Item, error)
	Leases(ctx context.Context, factory domain.FactoryID, station string) ([]domain.Lease, error)
	StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error)
	FlowMetrics(ctx context.Context, factory domain.FactoryID, since, until time.Time) (domain.FlowMetrics, error)
}

// Factories are the definitions goald runs; *definitions.Cache satisfies it.
type Factories interface {
	Factories() []*config.Factory
	// Lookup finds a factory by id or name, or returns an error wrapping ports.ErrNotFound.
	Lookup(ctx context.Context, ref string) (*config.Factory, error)
}

// Deps are the API's collaborators. Logger may be nil.
type Deps struct {
	Engine    Engine
	Items     Items
	Factories Factories
	// Feed serves /v1/stream; the caller runs it (Feed.Run).
	Feed *Feed
	// Clock dates ages and metric windows and paces stream heartbeats.
	Clock ports.Clock
	// LegacyOrg is the organization every request runs in until the routes move under
	// /v1/orgs/{org} (phase 2.2 A5 removes it).
	LegacyOrg domain.OrgID
	Logger    *slog.Logger
}

// Limits on requests.
const (
	maxRequestBytes   = 1 << 20
	defaultEventLimit = 200
	maxEventLimit     = 1000
)

// errInvalid marks a request the API refuses as malformed (400).
var errInvalid = errors.New("invalid request")

// New returns the API's handler for the /v1/ routes.
func New(d Deps) (http.Handler, error) {
	if d.Engine == nil || d.Items == nil || d.Factories == nil || d.Feed == nil || d.Clock == nil || d.LegacyOrg == "" {
		return nil, errors.New("api: Engine, Items, Factories, Feed, Clock and LegacyOrg are required")
	}

	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	s := &server{engine: d.Engine, items: d.Items, factories: d.Factories, feed: d.Feed, clock: d.Clock, log: log}

	strict := gen.NewStrictHandlerWithOptions(s, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.problem(w, r, fmt.Errorf("%w: %w", errInvalid, err))
		},
		ResponseErrorHandlerFunc: s.problem,
	})

	h := gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.problem(w, r, fmt.Errorf("%w: %w", errInvalid, err))
		},
	})

	return http.MaxBytesHandler(legacyOrg(d.LegacyOrg, h), maxRequestBytes), nil
}

type server struct {
	engine    Engine
	items     Items
	factories Factories
	feed      *Feed
	clock     ports.Clock
	log       *slog.Logger
}

var _ gen.StrictServerInterface = (*server)(nil)

func (s *server) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{Status: "ok"}, nil
}

func (s *server) ListFactories(context.Context, gen.ListFactoriesRequestObject) (gen.ListFactoriesResponseObject, error) {
	out := gen.ListFactories200JSONResponse{Factories: []gen.Factory{}}
	for _, f := range s.factories.Factories() {
		out.Factories = append(out.Factories, factoryOf(f))
	}

	return out, nil
}

func (s *server) SubmitItem(ctx context.Context, req gen.SubmitItemRequestObject) (gen.SubmitItemResponseObject, error) {
	b := req.Body

	f, err := s.factories.Lookup(ctx, req.Factory)
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

	return gen.SubmitItem201JSONResponse(s.itemOf(it)), nil
}

func (s *server) GetItem(ctx context.Context, req gen.GetItemRequestObject) (gen.GetItemResponseObject, error) {
	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	return gen.GetItem200JSONResponse(s.itemOf(it)), nil
}

// item finds an item by its id or by <factory>#<number>.
func (s *server) item(ctx context.Context, ref string) (domain.Item, error) {
	r, ok := domain.ParseItemRef(ref)
	if !ok {
		return s.items.Item(ctx, domain.ItemID(ref))
	}

	f, err := s.factories.Lookup(ctx, r.Factory)
	if err != nil {
		return domain.Item{}, fmt.Errorf("item %s: %w", ref, err)
	}

	return s.items.ItemByNumber(ctx, domain.FactoryID(f.Metadata.ID), r.Number)
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
	if strings.TrimSpace(req.Body.By) == "" {
		return nil, fmt.Errorf("%w: say who approves (by)", errInvalid)
	}

	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Approve(ctx, it.ID, strings.TrimSpace(req.Body.By)); err != nil {
		return nil, err
	}

	return gen.ApproveItem200JSONResponse(s.itemOf(it)), nil
}

func (s *server) RejectItem(ctx context.Context, req gen.RejectItemRequestObject) (gen.RejectItemResponseObject, error) {
	by, reason := strings.TrimSpace(req.Body.By), strings.TrimSpace(req.Body.Reason)
	if by == "" || reason == "" {
		return nil, fmt.Errorf("%w: a rejection needs who rejects (by) and a reason", errInvalid)
	}

	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Reject(ctx, it.ID, by, reason); err != nil {
		return nil, err
	}

	return gen.RejectItem200JSONResponse(s.itemOf(it)), nil
}

func (s *server) RetryItem(ctx context.Context, req gen.RetryItemRequestObject) (gen.RetryItemResponseObject, error) {
	by := strings.TrimSpace(req.Body.By)
	if by == "" {
		return nil, fmt.Errorf("%w: say who retries (by)", errInvalid)
	}

	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Retry(ctx, it.ID, by); err != nil {
		return nil, err
	}

	return gen.RetryItem200JSONResponse(s.itemOf(it)), nil
}

func (s *server) MergeItem(ctx context.Context, req gen.MergeItemRequestObject) (gen.MergeItemResponseObject, error) {
	by := strings.TrimSpace(req.Body.By)
	if by == "" {
		return nil, fmt.Errorf("%w: say who merges (by)", errInvalid)
	}

	method := engine.MergeSquash

	if m := req.Body.Method; m != nil && *m != gen.Squash {
		return nil, fmt.Errorf("%w: merge method %q is not squash", errInvalid, *m)
	}

	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Merge(ctx, it.ID, by, method); err != nil {
		return nil, err
	}

	return gen.MergeItem200JSONResponse(s.itemOf(it)), nil
}

// problem writes err as problem details with the status its kind maps to.
func (s *server) problem(w http.ResponseWriter, r *http.Request, err error) {
	status, detail := http.StatusInternalServerError, "see goald's log"

	var tooLarge *http.MaxBytesError

	switch {
	case errors.Is(err, ports.ErrNotFound):
		status, detail = http.StatusNotFound, err.Error()
	case errors.Is(err, engine.ErrNotWaitingHuman), errors.Is(err, engine.ErrNotBlocked),
		errors.Is(err, engine.ErrNotMergeStep), errors.Is(err, ports.ErrNotMergeable):
		status, detail = http.StatusConflict, err.Error()
	case errors.As(err, &tooLarge):
		status, detail = http.StatusRequestEntityTooLarge, err.Error()
	case errors.Is(err, errInvalid):
		status, detail = http.StatusBadRequest, err.Error()
	default:
		s.log.ErrorContext(r.Context(), "api request failed", slog.String("method", r.Method),
			slog.String("path", r.URL.Path), slog.Any("error", err))
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(gen.Problem{Title: http.StatusText(status), Status: status, Detail: &detail}); err != nil {
		s.log.WarnContext(r.Context(), "write problem response", slog.Any("error", err))
	}
}

func factoryOf(f *config.Factory) gen.Factory {
	out := gen.Factory{
		Id: f.Metadata.ID, Name: f.Metadata.Name, Title: optional(f.Metadata.Title),
		Description: optional(f.Metadata.Description), WorkTypes: slices.Clone(f.Spec.Board.WorkTypes),
		Stations: make([]gen.Station, 0, len(f.Spec.Board.Stations)),
	}
	if out.WorkTypes == nil {
		out.WorkTypes = []string{}
	}

	for _, st := range f.Spec.Board.Stations {
		out.Stations = append(out.Stations, gen.Station{Id: st.ID, Kind: gen.StationKind(st.Kind), Action: actionOf(st.Action)})
	}

	return out
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
func (s *server) ref(factory domain.FactoryID, number int64) string {
	name := string(factory)

	for _, f := range s.factories.Factories() {
		if f.Metadata.ID == string(factory) {
			name = f.Metadata.Name
			break
		}
	}

	return domain.ItemRef{Factory: name, Number: number}.String()
}

func (s *server) itemOf(it domain.Item) gen.Item {
	out := gen.Item{
		Id: string(it.ID), Ref: s.ref(it.FactoryID, it.Number), FactoryId: string(it.FactoryID), Number: it.Number,
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
