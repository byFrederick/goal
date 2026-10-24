package api

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Limits of the board and of item listings.
const (
	boardOpenItems   = 500
	boardDoneItems   = 20
	defaultItemLimit = 50
	maxItemLimit     = 200
	maxWindowDays    = 90
	day              = 24 * time.Hour
)

// openStates are the states of items that are on the line.
var openStates = []domain.ItemState{
	domain.StateQueued, domain.StateReady, domain.StateInProgress, domain.StateWaitingHuman, domain.StateBlocked,
}

// window is a metrics window: the last N UTC days, today included.
var window = regexp.MustCompile(`^([1-9][0-9]?)d$`)

func (s *server) GetBoard(ctx context.Context, req gen.GetBoardRequestObject) (gen.GetBoardResponseObject, error) {
	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	loadList, err := s.items.StationLoads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	loads := make(map[string]domain.StationLoad, len(loadList))
	for _, ld := range loadList {
		loads[ld.Station] = ld
	}

	open, err := s.items.ListItems(ctx, ports.ItemQuery{FactoryID: l.FactoryID, States: openStates, Limit: boardOpenItems})
	if err != nil {
		return nil, err
	}

	done, err := s.items.ListItems(ctx, ports.ItemQuery{
		FactoryID: l.FactoryID, States: []domain.ItemState{domain.StateDone}, Limit: boardDoneItems,
	})
	if err != nil {
		return nil, err
	}

	cards, err := s.cards(ctx, f, l, append(open, done...))
	if err != nil {
		return nil, err
	}

	byStation := map[string][]gen.ItemCard{}
	for _, c := range cards {
		byStation[c.Station] = append(byStation[c.Station], c)
	}

	board := gen.GetBoard200JSONResponse{Stations: make([]gen.BoardStation, 0, len(l.Stations))}
	board.Factory.Id, board.Factory.Name, board.Factory.Title = f.Metadata.ID, f.Metadata.Name, optional(f.Metadata.Title)

	for _, st := range l.Stations {
		bs := gen.BoardStation{
			Id: st.ID, Kind: gen.StationKind(st.Kind), Action: actionOf(string(st.Action)), Agents: agentsAt(st),
			Waiting: loads[st.ID].Waiting, InProgress: loads[st.ID].InProgress, Items: byStation[st.ID],
		}

		if st.WIP > 0 {
			bs.WipLimit = &st.WIP
		}

		if bs.Items == nil {
			bs.Items = []gen.ItemCard{}
		}

		board.Stations = append(board.Stations, bs)
	}

	if c, ok := l.Constraint(loads); ok {
		board.Constraint = &c
	}

	return board, nil
}

// agentsAt names the agents that work at a station: its agent, or every agent its route uses.
func agentsAt(st domain.Station) []string {
	if st.Agent != "" {
		return []string{st.Agent}
	}

	return append([]string{}, slices.Compact(slices.Sorted(maps.Values(st.Route)))...)
}

func (s *server) ListItems(ctx context.Context, req gen.ListItemsRequestObject) (gen.ListItemsResponseObject, error) {
	p := req.Params

	limit := defaultItemLimit
	if p.Limit != nil {
		limit = *p.Limit
	}

	if limit < 1 || limit > maxItemLimit || (p.After != nil && *p.After < 1) || (p.State != nil && !p.State.Valid()) {
		return nil, fmt.Errorf("%w: limit must be in [1, %d], after ≥ 1 and state an item state", errInvalid, maxItemLimit)
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	q := ports.ItemQuery{FactoryID: domain.FactoryID(f.Metadata.ID), Station: deref(p.Station), Before: deref(p.After), Limit: limit}
	if p.State != nil {
		q.States = []domain.ItemState{domain.ItemState(*p.State)}
	}

	items, err := s.items.ListItems(ctx, q)
	if err != nil {
		return nil, err
	}

	cards, err := s.cards(ctx, f, f.Line(), items)
	if err != nil {
		return nil, err
	}

	page := gen.ListItems200JSONResponse{Items: cards}

	if len(items) == limit {
		last := items[len(items)-1].Number
		page.NextAfter = &last
	}

	return page, nil
}

// cards renders items of factory f as cards, with the agent of every item in progress.
func (s *server) cards(ctx context.Context, f *config.Factory, l domain.Line, items []domain.Item) ([]gen.ItemCard, error) {
	agents := map[domain.ItemID]string{}
	seen := map[string]bool{}

	for _, it := range items {
		if it.State != domain.StateInProgress || seen[it.Station] {
			continue
		}

		seen[it.Station] = true

		leases, err := s.items.Leases(ctx, l.FactoryID, it.Station)
		if err != nil {
			return nil, err
		}

		for _, ls := range leases {
			agents[ls.ItemID] = ls.Agent
		}
	}

	now := s.clock.Now()
	out := make([]gen.ItemCard, 0, len(items))

	for _, it := range items {
		age := now.Sub(it.CreatedAt)
		if !it.DoneAt.IsZero() {
			age = it.DoneAt.Sub(it.CreatedAt)
		}

		out = append(out, gen.ItemCard{
			Id: string(it.ID), Ref: domain.ItemRef{Factory: f.Metadata.Name, Number: it.Number}.String(), Number: it.Number,
			Title: it.Title, Type: it.Type, Lane: optional(it.Lane), Station: it.Station, State: gen.ItemState(it.State),
			ClassOfService: gen.ClassOfService(it.Class), ReworkCount: it.ReworkCount, CostUsd: it.CostUSD,
			CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt, AgeSeconds: int64(max(age, 0) / time.Second),
			Agent: optional(agents[it.ID]), PrUrl: optional(it.PullRequest(l)),
		})
	}

	return out, nil
}

func (s *server) GetMetrics(ctx context.Context, req gen.GetMetricsRequestObject) (gen.GetMetricsResponseObject, error) {
	w := "7d"
	if req.Params.Window != nil {
		w = *req.Params.Window
	}

	var days int
	if m := window.FindStringSubmatch(w); m != nil {
		days, _ = strconv.Atoi(m[1])
	}

	if days < 1 || days > maxWindowDays {
		return nil, fmt.Errorf("%w: window %q is not 1d to %dd", errInvalid, w, maxWindowDays)
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now().UTC()
	since := now.Truncate(day).Add(-time.Duration(days-1) * day)

	fm, err := s.items.FlowMetrics(ctx, domain.FactoryID(f.Metadata.ID), since, now)
	if err != nil {
		return nil, err
	}

	return metricsOf(w, days, since, fm), nil
}

func metricsOf(w string, days int, since time.Time, fm domain.FlowMetrics) gen.GetMetrics200JSONResponse {
	out := gen.GetMetrics200JSONResponse{
		Window: w, Wip: fm.WIP, SpendUsd: fm.SpendUSD, ItemsDone: fm.ItemsDone,
	}

	done := make(map[time.Time]int, len(fm.DonePerDay))
	for _, d := range fm.DonePerDay {
		done[d.Day.UTC()] = d.Count
	}

	for i := range days {
		d := since.Add(time.Duration(i) * day)
		out.Throughput = append(out.Throughput, struct {
			Day  openapi_types.Date `json:"day"`
			Done int                `json:"done"`
		}{Day: openapi_types.Date{Time: d}, Done: done[d]})
	}

	if fm.ItemsDone > 0 {
		out.ReworkRate = float64(fm.Reworked) / float64(fm.ItemsDone)
		out.LeadTimeHours = &struct {
			P50 float64 `json:"p50"`
			P85 float64 `json:"p85"`
		}{P50: fm.LeadTimeP50.Hours(), P85: fm.LeadTimeP85.Hours()}
	}

	return out
}
