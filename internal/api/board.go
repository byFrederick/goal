package api

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

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
)

// openStates are the states of items that are on the line.
var openStates = []domain.ItemState{
	domain.StateQueued, domain.StateReady, domain.StateInProgress, domain.StateWaitingHuman, domain.StateBlocked,
}

func (s *server) GetBoard(ctx context.Context, req gen.GetBoardRequestObject) (gen.GetBoardResponseObject, error) {
	now := s.clock.Now().UTC()

	w, err := windowOf(req.Params.Window, now)
	if err != nil {
		return nil, err
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	l, now := f.Line(), s.clock.Now().UTC()

	stats, err := s.boardStats(ctx, l.FactoryID, w, now)
	if err != nil {
		return nil, err
	}

	loads, err := s.stationLoads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	constraint, source := l.ConstraintOf(l.Candidates(stats.list, stats.busyOver, 1), loads)

	byStation, err := s.boardCards(ctx, f, l, constraint)
	if err != nil {
		return nil, err
	}

	line, err := s.boardLine(ctx, l, constraint, loads, byStation, now)
	if err != nil {
		return nil, err
	}

	board := gen.GetBoard200JSONResponse{
		Stations: make([]gen.BoardStation, 0, len(l.Stations)), Line: line, Constraint: optional(constraint),
		ConstraintSource: gen.ConstraintSource(source),
	}
	board.Factory.Id, board.Factory.Name, board.Factory.Title = f.Metadata.ID, f.Metadata.Name, optional(f.Metadata.Title)

	for _, st := range l.Stations {
		bs := gen.BoardStation{
			Id: st.ID, Kind: gen.StationKind(st.Kind), Action: actionOf(string(st.Action)), Agents: agentsAt(st),
			Waiting: loads[st.ID].Waiting, InProgress: loads[st.ID].Busy(), Items: byStation[st.ID],
		}

		if st.WIP > 0 {
			bs.WipLimit = &st.WIP
		}

		if bs.Items == nil {
			bs.Items = []gen.ItemCard{}
		}

		stats.fill(st, &bs)
		board.Stations = append(board.Stations, bs)
	}

	return board, nil
}

// stationLoads reads the load of the factory's stations, by station.
func (s *server) stationLoads(ctx context.Context, f domain.FactoryID) (map[string]domain.StationLoad, error) {
	list, err := s.items.StationLoads(ctx, f)
	if err != nil {
		return nil, err
	}

	loads := make(map[string]domain.StationLoad, len(list))
	for _, ld := range list {
		loads[ld.Station] = ld
	}

	return loads, nil
}

// boardCards renders the board's cards by station: every open item (up to boardOpenItems) and the
// boardDoneItems newest done ones, those waiting at drum in its buffer.
func (s *server) boardCards(ctx context.Context, f *config.Factory, l domain.Line, drum string) (map[string][]gen.ItemCard, error) {
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

	cards, err := s.cards(ctx, f, l, drum, append(open, done...))
	if err != nil {
		return nil, err
	}

	byStation := map[string][]gen.ItemCard{}
	for _, c := range cards {
		byStation[c.Station] = append(byStation[c.Station], c)
	}

	return byStation, nil
}

// boardStats is what the board's station statistics come from: every station measured over the
// window, and the items done today.
type boardStats struct {
	list  []domain.StationTimes
	times map[string]domain.StationTimes
	now   time.Time
	// busyOver is the stretch busy shares are measured over: the window, from when the factory's
	// lease history begins if that is later; 0 without any history.
	busyOver  time.Duration
	doneToday int
}

func (s *server) boardStats(ctx context.Context, f domain.FactoryID, w dayRange, now time.Time) (boardStats, error) {
	since := w.from

	list, history, err := s.items.StationTimes(ctx, f, since, now)
	if err != nil {
		return boardStats{}, err
	}

	today, err := s.items.FlowMetrics(ctx, lastDays(1, now).query(f, "", now), "")
	if err != nil {
		return boardStats{}, err
	}

	b := boardStats{list: list, times: make(map[string]domain.StationTimes, len(list)), now: now, doneToday: today.ItemsDone}
	for _, t := range list {
		b.times[t.Station] = t
	}

	if !history.IsZero() {
		b.busyOver = now.Sub(maxTime(since, history))
	}

	return b, nil
}

// fill sets the statistics of station st on bs: the busy share of an agent station, the
// send-back share of a station with a gate, the trend and oldest wait of queue and agent
// stations, and the items done today at a done station.
func (b boardStats) fill(st domain.Station, bs *gen.BoardStation) {
	t := b.times[st.ID]

	if st.Kind == domain.KindAgent && b.busyOver > 0 {
		busy := t.BusyRatio(st.Capacity(), b.busyOver)
		bs.BusyRatio = &busy
	}

	if st.Gate != nil && t.Verdicts > 0 {
		back := float64(t.Rejected) / float64(t.Verdicts)
		bs.SendBackRatio = &back
	}

	if st.Kind == domain.KindQueue || st.Kind == domain.KindAgent {
		trend := gen.QueueTrend(domain.QueueTrend(t.WaitingNow, t.WaitingDayAgo))
		bs.QueueTrend = &trend

		if t.WaitingNow > 0 {
			oldest := int64(max(b.now.Sub(t.OldestWaiting), 0) / time.Second)
			bs.OldestWaitingSeconds = &oldest
		}
	}

	if st.Kind == domain.KindDone {
		done := b.doneToday
		bs.DoneToday = &done
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
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

	cards, err := s.listCards(ctx, f, items)
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

// listCards renders a list of factory f's items as cards, with the line's drum now.
func (s *server) listCards(ctx context.Context, f *config.Factory, items []domain.Item) ([]gen.ItemCard, error) {
	l := f.Line()

	loads, err := s.stationLoads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	drum, err := s.drumNow(ctx, l, loads, s.clock.Now().UTC())
	if err != nil {
		return nil, err
	}

	return s.cards(ctx, f, l, drum, items)
}

// cards renders items of factory f as cards: for every item in progress the agent working it, the
// copy (1-based) when its station runs several, and the turns of its running run; for every item
// the pull request and the summary its outputs name, and whether it waits in the buffer of drum
// (none when drum is ""). Nothing is held by the rope until drum-buffer-rope is enforced.
func (s *server) cards(ctx context.Context, f *config.Factory, l domain.Line, drum string, items []domain.Item) ([]gen.ItemCard, error) {
	leases, turns, err := s.inProgress(ctx, l, items)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now()
	out := make([]gen.ItemCard, 0, len(items))

	for _, it := range items {
		age := now.Sub(it.CreatedAt)
		if !it.DoneAt.IsZero() {
			age = it.DoneAt.Sub(it.CreatedAt)
		}

		c := gen.ItemCard{
			Id: string(it.ID), Ref: domain.ItemRef{Factory: f.Metadata.Name, Number: it.Number}.String(), Number: it.Number,
			Title: it.Title, Type: it.Type, Lane: optional(it.Lane), Station: it.Station, State: gen.ItemState(it.State),
			ClassOfService: gen.ClassOfService(it.Class), ReworkCount: it.ReworkCount, CostUsd: it.CostUSD,
			CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt, AgeSeconds: int64(max(age, 0) / time.Second),
			PrUrl: optional(it.PullRequest(l)), Summary: optional(it.Summary(l)),
			InBuffer: drum != "" && it.Station == drum && it.State.Waiting(),
		}

		if ls, ok := leases[it.ID]; ok {
			c.Agent = optional(ls.Agent)

			if st, ok := l.Station(ls.Station); ok && st.Capacity() > 1 {
				slot := ls.Slot + 1
				c.Slot = &slot
			}
		}

		if n, ok := turns[it.ID]; ok {
			c.Turn = &n
		}

		out = append(out, c)
	}

	return out, nil
}

// inProgress reads the leases of the stations where items are in progress, by item, and the
// turns of the runs under them; nothing when no item is in progress.
func (s *server) inProgress(ctx context.Context, l domain.Line, items []domain.Item) (map[domain.ItemID]domain.Lease, map[domain.ItemID]int, error) {
	leases := map[domain.ItemID]domain.Lease{}
	seen := map[string]bool{}

	for _, it := range items {
		if it.State != domain.StateInProgress || seen[it.Station] {
			continue
		}

		seen[it.Station] = true

		held, err := s.items.Leases(ctx, l.FactoryID, it.Station)
		if err != nil {
			return nil, nil, err
		}

		for _, ls := range held {
			leases[ls.ItemID] = ls
		}
	}

	if len(seen) == 0 {
		return leases, map[domain.ItemID]int{}, nil
	}

	turns, err := s.items.LiveTurns(ctx, l.FactoryID)
	if err != nil {
		return nil, nil, err
	}

	return leases, turns, nil
}
