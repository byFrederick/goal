// Package api serves goald's HTTP API, the contract in api/openapi.yaml: health, the factories
// goald runs, submitting work items, reading items and their history, and human decisions.
//
// The handlers implement the strict server interface generated into internal/api/gen
// (`task api:gen`). They reach the engine, the store and the factory definitions only through
// the small interfaces below; cmd/goald wires the real ones. Errors are RFC 9457 problem
// details: unknown items and factories are 404, invalid requests 400, a decision on an item
// that is not waiting for a human 409; anything else is a 500 whose detail stays in the log.
//
// Phase 1 has no authentication: goald binds the API to 127.0.0.1.
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
}

// Items reads items and their history; ports.Store satisfies it.
type Items interface {
	Item(ctx context.Context, id domain.ItemID) (domain.Item, error)
	ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error)
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
	if d.Engine == nil || d.Items == nil || d.Factories == nil {
		return nil, errors.New("api: Engine, Items and Factories are required")
	}

	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	s := &server{engine: d.Engine, items: d.Items, factories: d.Factories, log: log}

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

	return http.MaxBytesHandler(h, maxRequestBytes), nil
}

type server struct {
	engine    Engine
	items     Items
	factories Factories
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

	return gen.SubmitItem201JSONResponse(itemOf(it)), nil
}

func (s *server) GetItem(ctx context.Context, req gen.GetItemRequestObject) (gen.GetItemResponseObject, error) {
	it, err := s.items.Item(ctx, domain.ItemID(req.Item))
	if err != nil {
		return nil, err
	}

	return gen.GetItem200JSONResponse(itemOf(it)), nil
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
	if _, err := s.items.Item(ctx, domain.ItemID(req.Item)); err != nil {
		return nil, err
	}

	events, err := s.items.ItemEvents(ctx, domain.ItemID(req.Item), after, limit)
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

	it, err := s.engine.Approve(ctx, domain.ItemID(req.Item), strings.TrimSpace(req.Body.By))
	if err != nil {
		return nil, err
	}

	return gen.ApproveItem200JSONResponse(itemOf(it)), nil
}

func (s *server) RejectItem(ctx context.Context, req gen.RejectItemRequestObject) (gen.RejectItemResponseObject, error) {
	by, reason := strings.TrimSpace(req.Body.By), strings.TrimSpace(req.Body.Reason)
	if by == "" || reason == "" {
		return nil, fmt.Errorf("%w: a rejection needs who rejects (by) and a reason", errInvalid)
	}

	it, err := s.engine.Reject(ctx, domain.ItemID(req.Item), by, reason)
	if err != nil {
		return nil, err
	}

	return gen.RejectItem200JSONResponse(itemOf(it)), nil
}

// problem writes err as problem details with the status its kind maps to.
func (s *server) problem(w http.ResponseWriter, r *http.Request, err error) {
	status, detail := http.StatusInternalServerError, "see goald's log"

	var tooLarge *http.MaxBytesError

	switch {
	case errors.Is(err, ports.ErrNotFound):
		status, detail = http.StatusNotFound, err.Error()
	case errors.Is(err, engine.ErrNotWaitingHuman):
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
		out.Stations = append(out.Stations, gen.Station{Id: st.ID, Kind: gen.StationKind(st.Kind)})
	}

	return out
}

func itemOf(it domain.Item) gen.Item {
	out := gen.Item{
		Id: string(it.ID), FactoryId: string(it.FactoryID), Number: it.Number, ParentId: optional(string(it.ParentID)),
		Type: it.Type, Lane: optional(it.Lane), Title: it.Title, Body: it.Body, Station: it.Station,
		State: gen.ItemState(it.State), ClassOfService: gen.ItemClassOfService(it.Class), Priority: it.Priority,
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
