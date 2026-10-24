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
	w, err := windowOf(req.Params.Window)
	if err != nil {
		return nil, err
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	stats, err := s.boardStats(ctx, l.FactoryID, w, s.clock.Now().UTC())
	if err != nil {
		return nil, err
	}

	loadList, err := s.items.StationLoads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	loads := make(map[string]domain.StationLoad, len(loadList))
	for _, ld := range loadList {
		loads[ld.Station] = ld
	}

	byStation, err := s.boardCards(ctx, f, l)
	if err != nil {
		return nil, err
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

		stats.fill(st, &bs)
		board.Stations = append(board.Stations, bs)
	}

	if c, ok := l.Constraint(loads); ok {
		board.Constraint = &c
	}

	return board, nil
}

// boardCards renders the board's cards by station: every open item (up to boardOpenItems) and the
// boardDoneItems newest done ones.
func (s *server) boardCards(ctx context.Context, f *config.Factory, l domain.Line) (map[string][]gen.ItemCard, error) {
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

	return byStation, nil
}

// boardStats is what the board's station statistics come from: every station measured over the
// window, and the items done today.
type boardStats struct {
	times map[string]domain.StationTimes
	now   time.Time
	// busyOver is the stretch busy shares are measured over: the window, from when the factory's
	// lease history begins if that is later; 0 without any history.
	busyOver  time.Duration
	doneToday int
}

func (s *server) boardStats(ctx context.Context, f domain.FactoryID, w metricsWindow, now time.Time) (boardStats, error) {
	since := w.since(now)

	list, history, err := s.items.StationTimes(ctx, f, since, now)
	if err != nil {
		return boardStats{}, err
	}

	today, err := s.items.FlowMetrics(ctx, f, now.Truncate(day), now)
	if err != nil {
		return boardStats{}, err
	}

	b := boardStats{times: make(map[string]domain.StationTimes, len(list)), now: now, doneToday: today.ItemsDone}
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

// cards renders items of factory f as cards: for every item in progress the agent working it, the
// copy (1-based) when its station runs several, and the turns of its running run; for every item
// the pull request and the summary its outputs name.
func (s *server) cards(ctx context.Context, f *config.Factory, l domain.Line, items []domain.Item) ([]gen.ItemCard, error) {
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

func (s *server) GetMetrics(ctx context.Context, req gen.GetMetricsRequestObject) (gen.GetMetricsResponseObject, error) {
	w, err := windowOf(req.Params.Window)
	if err != nil {
		return nil, err
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now().UTC()
	since := w.since(now)

	fm, err := s.items.FlowMetrics(ctx, domain.FactoryID(f.Metadata.ID), since, now)
	if err != nil {
		return nil, err
	}

	return metricsOf(w, since, fm), nil
}

// metricsWindow is a valid window parameter: the last days UTC days, today included.
type metricsWindow struct {
	name string
	days int
}

// windowOf checks a window parameter, 7d when absent.
func windowOf(param *string) (metricsWindow, error) {
	w := metricsWindow{name: "7d"}
	if param != nil {
		w.name = *param
	}

	if m := window.FindStringSubmatch(w.name); m != nil {
		w.days, _ = strconv.Atoi(m[1])
	}

	if w.days < 1 || w.days > maxWindowDays {
		return metricsWindow{}, fmt.Errorf("%w: window %q is not 1d to %dd", errInvalid, w.name, maxWindowDays)
	}

	return w, nil
}

// since is the window's first instant when it ends at now: midnight UTC, days-1 days before
// today's.
func (w metricsWindow) since(now time.Time) time.Time {
	return now.Truncate(day).Add(-time.Duration(w.days-1) * day)
}

// donePerDay pads fm's items done per day to one entry for every day of the window from since.
func donePerDay(w metricsWindow, since time.Time, fm domain.FlowMetrics) []struct {
	Day  openapi_types.Date `json:"day"`
	Done int                `json:"done"`
} {
	done := make(map[time.Time]int, len(fm.DonePerDay))
	for _, d := range fm.DonePerDay {
		done[d.Day.UTC()] = d.Count
	}

	out := make([]struct {
		Day  openapi_types.Date `json:"day"`
		Done int                `json:"done"`
	}, 0, w.days)

	for i := range w.days {
		d := since.Add(time.Duration(i) * day)
		out = append(out, struct {
			Day  openapi_types.Date `json:"day"`
			Done int                `json:"done"`
		}{Day: openapi_types.Date{Time: d}, Done: done[d]})
	}

	return out
}

func metricsOf(w metricsWindow, since time.Time, fm domain.FlowMetrics) gen.GetMetrics200JSONResponse {
	out := gen.GetMetrics200JSONResponse{
		Window: w.name, Wip: fm.WIP, SpendUsd: fm.SpendUSD, ItemsDone: fm.ItemsDone, Throughput: donePerDay(w, since, fm),
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
