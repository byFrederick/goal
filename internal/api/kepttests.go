package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Kept tests (ADR 0039 decision 12): a past item a person keeps, at a station it passed the first
// time, as a test the item must keep passing there.

// KeptTestStore keeps kept tests and finds the run a test of an item at a station reruns; a
// *postgres.Store and a *porttest.Store satisfy it.
type KeptTestStore interface {
	ports.KeptTests
	PastTryTarget(ctx context.Context, item domain.ItemID, station string) (domain.Run, error)
}

var (
	errNotPassedFirstTime = errors.New("the item did not pass this station the first time")
	errKeptTestExists     = errors.New("the item is kept as a test at this station already")
	errKeptTestNotFound   = errors.New("no such kept test")
)

// keptTestProblem answers the kept-test routes' own refusals.
func keptTestProblem(err error) (gen.Problem, bool) {
	var status int

	var code string

	switch {
	case errors.Is(err, errNotPassedFirstTime):
		status, code = http.StatusConflict, "not-passed-first-time"
	case errors.Is(err, errKeptTestExists):
		status, code = http.StatusConflict, "kept-test-exists"
	case errors.Is(err, errKeptTestNotFound):
		status, code = http.StatusNotFound, "kept-test-not-found"
	default:
		return gen.Problem{}, false
	}

	p := detailed(status, err)
	p.Code = &code

	return p, true
}

// keptTests is the store of the kept-test routes: errJonahUnavailable when this goald has none.
func (s *server) keptTests() (KeptTestStore, error) {
	if s.jonah.Tests == nil {
		return nil, errJonahUnavailable
	}

	return s.jonah.Tests, nil
}

func (s *server) ListKeptTests(ctx context.Context, req gen.ListKeptTestsRequestObject) (gen.ListKeptTestsResponseObject, error) {
	rec, err := s.jonahFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	store, err := s.keptTests()
	if err != nil {
		return nil, err
	}

	tests, err := store.KeptTests(ctx, rec.ID, "")
	if err != nil {
		return nil, fmt.Errorf("list kept tests: %w", err)
	}

	// By station in line order, a station no longer in the line last; newest first within one,
	// as the store lists them.
	order := map[string]int{}

	if fc, err := s.factories.Lookup(ctx, string(rec.ID)); err == nil {
		for i, st := range fc.Line().Stations {
			order[st.ID] = i + 1
		}
	}

	rank := func(station string) int {
		if i, ok := order[station]; ok {
			return i
		}

		return len(order) + 1
	}

	slices.SortStableFunc(tests, func(a, b domain.KeptTest) int {
		return cmp.Or(cmp.Compare(rank(a.Station), rank(b.Station)), cmp.Compare(a.Station, b.Station))
	})

	out := gen.KeptTestList{Tests: make([]gen.KeptTest, len(tests))}
	for i, k := range tests {
		out.Tests[i] = keptTestOut(k)
	}

	return gen.ListKeptTests200JSONResponse(out), nil
}

func (s *server) KeepTest(ctx context.Context, req gen.KeepTestRequestObject) (gen.KeepTestResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	store, err := s.keptTests()
	if err != nil {
		return nil, err
	}

	if req.Body == nil {
		return nil, fmt.Errorf("keep test: no body: %w", errInvalid)
	}

	by, err := actorOf(ctx, "keep test")
	if err != nil {
		return nil, err
	}

	it, err := s.items.ItemByNumber(ctx, rec.ID, req.Body.Item)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, errItemNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("keep test: read item %d: %w", req.Body.Item, err)
	}

	station := req.Body.Station
	if !s.agentStation(ctx, rec.ID, station) {
		return nil, fmt.Errorf("keep test: %q is not an agent station of the current line: %w", station, errInvalid)
	}

	if err := s.passedFirstTime(ctx, store, it.ID, station); err != nil {
		return nil, err
	}

	k, err := store.KeepTest(ctx, domain.NewKeptTest{FactoryID: rec.ID, ItemID: it.ID, Station: station, KeptBy: by, KeptAt: s.clock.Now()})
	if errors.Is(err, ports.ErrConflict) {
		return nil, fmt.Errorf("keep item %d at %s: %w", it.Number, station, errKeptTestExists)
	}

	if err != nil {
		return nil, fmt.Errorf("keep test: %w", err)
	}

	return gen.KeepTest201JSONResponse(keptTestOut(k)), nil
}

func (s *server) RemoveKeptTest(ctx context.Context, req gen.RemoveKeptTestRequestObject) (gen.RemoveKeptTestResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	store, err := s.keptTests()
	if err != nil {
		return nil, err
	}

	err = store.RemoveKeptTest(ctx, rec.ID, domain.KeptTestID(req.Test))
	if errors.Is(err, ports.ErrNotFound) {
		return nil, fmt.Errorf("kept test %s: %w", req.Test, errKeptTestNotFound)
	}

	if err != nil {
		return nil, fmt.Errorf("remove kept test: %w", err)
	}

	return gen.RemoveKeptTest204Response{}, nil
}

// agentStation reports whether station is an agent station of f's current line.
func (s *server) agentStation(ctx context.Context, f domain.FactoryID, station string) bool {
	fc, err := s.factories.Lookup(ctx, string(f))
	if err != nil {
		return false
	}

	st, ok := fc.Line().Station(station)

	return ok && st.Kind == domain.KindAgent
}

// passedFirstTime refuses with errNotPassedFirstTime unless item passed station the first time
// (domain.PassedFirstTime) and a claim's first run there finished with its commit recorded, the
// run a try of the test reruns.
func (s *server) passedFirstTime(ctx context.Context, store KeptTestStore, item domain.ItemID, station string) error {
	var events []domain.Event

	for after := int64(0); ; {
		page, err := s.items.ItemEvents(ctx, item, after, maxEventLimit)
		if err != nil {
			return fmt.Errorf("read the events of item %s: %w", item, err)
		}

		events = append(events, page...)

		if len(page) < maxEventLimit {
			break
		}

		after = page[len(page)-1].ID
	}

	if !domain.PassedFirstTime(events, station) {
		return fmt.Errorf("keep test at %s: %w", station, errNotPassedFirstTime)
	}

	_, err := store.PastTryTarget(ctx, item, station)
	if errors.Is(err, ports.ErrNotFound) {
		return fmt.Errorf("keep test at %s: no finished first run there: %w", station, errNotPassedFirstTime)
	}

	if err != nil {
		return fmt.Errorf("keep test: read the run at %s: %w", station, err)
	}

	return nil
}

// keptTestOut is k as the API shows it.
func keptTestOut(k domain.KeptTest) gen.KeptTest {
	return gen.KeptTest{
		Id: string(k.ID), Item: gen.KeptTestItem{Id: string(k.ItemID), Number: k.ItemNumber, Title: k.ItemTitle},
		Station: k.Station, KeptBy: k.KeptBy.Ref(), KeptAt: k.KeptAt,
	}
}
