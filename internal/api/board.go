package api

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
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

	touches, err := s.items.Touches(ctx, w.query(l.FactoryID, "", now))
	if err != nil {
		return nil, err
	}

	line.Touches = touchHeadline(touches)

	board := gen.GetBoard200JSONResponse{
		Stations: make([]gen.BoardStation, 0, len(l.Stations)), Line: line, Constraint: optional(constraint),
		ConstraintSource: gen.ConstraintSource(source),
	}
	board.Factory.Id, board.Factory.Name, board.Factory.Title = f.Metadata.ID, f.Metadata.Name, optional(f.Metadata.Title)

	labels := s.modelLabels(ctx)

	for _, st := range l.Stations {
		lanes, err := s.lanes(ctx, l, st, loads[st.ID], labels)
		if err != nil {
			return nil, err
		}

		bs := gen.BoardStation{
			Id: st.ID, Kind: gen.StationKind(st.Kind), Action: actionOf(string(st.Action)), Agents: st.LaneAgents(),
			Waiting: loads[st.ID].Waiting, InProgress: loads[st.ID].Busy(), Items: byStation[st.ID], Lanes: &lanes,
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

// personLane is the key of a human station's one lane, the person who decides there.
const personLane = "person"

// lanes are station st's lanes, sorted by role: one per agent that works at an agent station,
// with the station's shared slots and the live leases that agent holds there, one for the person
// at a human station with the items waiting on them, none elsewhere.
func (s *server) lanes(ctx context.Context, l domain.Line, st domain.Station, load domain.StationLoad, labels map[string]string) ([]gen.BoardLane, error) {
	switch st.Kind {
	case domain.KindHuman:
		return []gen.BoardLane{{Key: personLane, Kind: gen.BoardLaneKindPerson, Role: domain.Humanize(st.ID), Busy: load.WaitingHuman}}, nil
	case domain.KindQueue, domain.KindDone:
		return []gen.BoardLane{}, nil
	case domain.KindAgent:
	}

	// Busy comes from the leases, not the cards: the board shows at most boardOpenItems.
	busy := map[string]int{}

	if load.InProgress > 0 {
		held, err := s.items.Leases(ctx, l.FactoryID, st.ID)
		if err != nil {
			return nil, err
		}

		for _, ls := range held {
			busy[ls.Agent]++
		}
	}

	slots := st.Capacity()
	agents := st.LaneAgents()
	out := make([]gen.BoardLane, 0, len(agents))

	for _, name := range agents {
		lane := gen.BoardLane{Key: name, Kind: gen.BoardLaneKindAgent, Role: domain.Humanize(name), Slots: &slots, Busy: busy[name]}

		if model := modelOf(l, name); model != "" {
			lane.Model, lane.ModelLabel = &model, new(cmp.Or(labels[model], model))
		}

		out = append(out, lane)
	}

	slices.SortStableFunc(out, func(a, b gen.BoardLane) int { return strings.Compare(a.Role, b.Role) })

	return out, nil
}

// modelOf is the model agent runs: its own, else the line runtime's; "" when neither names one.
func modelOf(l domain.Line, agent string) string {
	a, _ := l.Agent(agent)

	return cmp.Or(a.Model, l.Runtime.Model)
}

// modelLabels are the short labels of goald's models by id: their label without a leading
// "Claude " ("Opus 5.5").
func (s *server) modelLabels(ctx context.Context) map[string]string {
	models := s.builder.Meta(ctx).Models
	labels := make(map[string]string, len(models))

	for _, m := range models {
		if label := strings.TrimSpace(strings.TrimPrefix(m.Label, "Claude ")); label != "" {
			labels[m.ID] = label
		}
	}

	return labels
}

// boardLane is the key of the lane of station st that item it belongs to, the one holding it or
// to pick it up: ls's agent while in progress (held), else the agent st routes its lane to, the
// person at a human station; nil at queue and done stations, without a route for its lane, and
// for a done or cancelled item.
func boardLane(st domain.Station, it domain.Item, ls domain.Lease, held bool) *string {
	if it.State == domain.StateDone || it.State == domain.StateCancelled {
		return nil
	}

	switch st.Kind {
	case domain.KindQueue, domain.KindDone:
	case domain.KindHuman:
		return new(personLane)
	case domain.KindAgent:
		if held && ls.Agent != "" {
			return new(ls.Agent)
		}

		if a, err := st.AgentFor(it.Lane); err == nil {
			return &a
		}
	}

	return nil
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
// the pull request and the summary its outputs name, its human touches, and whether it waits in the buffer of drum
// (none when drum is ""), and for an assembly where its parts stand, for a part its assembly and
// which part it is, read for the whole page at once. Nothing is held by the rope until
// drum-buffer-rope is enforced.
func (s *server) cards(ctx context.Context, f *config.Factory, l domain.Line, drum string, items []domain.Item) ([]gen.ItemCard, error) {
	leases, turns, err := s.inProgress(ctx, l, items)
	if err != nil {
		return nil, err
	}

	touches, err := s.touchCounts(ctx, items)
	if err != nil {
		return nil, err
	}

	asms, err := s.assemblies(ctx, l.FactoryID, items)
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
			InBuffer: drum != "" && it.Station == drum && it.State.Waiting(), TouchCount: touches[it.ID],
			PullRequests: []string{},
		}

		// Whoever merges sees every pull request the merge lands (spec 4 §4.7), not just the latest.
		st, _ := l.Station(it.Station)
		if st.Action == domain.ActionMerge {
			c.PullRequests = it.PullRequests(l)
		}

		ls, held := leases[it.ID]
		c.BoardLane = boardLane(st, it, ls, held)

		if held {
			c.Agent = optional(ls.Agent)

			if st, ok := l.Station(ls.Station); ok && st.Capacity() > 1 {
				slot := ls.Slot + 1
				c.Slot = &slot
			}
		}

		if n, ok := turns[it.ID]; ok {
			c.Turn = &n
		}

		c.Assembly, c.Parts = assemblyFields(f, it, asms)

		out = append(out, c)
	}

	return out, nil
}

// touchCounts counts the touches of each of items that has any, by item.
func (s *server) touchCounts(ctx context.Context, items []domain.Item) (map[domain.ItemID]int, error) {
	ids := make([]domain.ItemID, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}

	return s.items.TouchCounts(ctx, ids)
}

// assemblyFields are an item's card's assembly, when it is a part of one of asms, and parts,
// when it is one of asms.
func assemblyFields(f *config.Factory, it domain.Item, asms map[domain.ItemID]domain.Assembly) (*gen.PartOf, *gen.AssemblyParts) {
	var (
		of    *gen.PartOf
		parts *gen.AssemblyParts
	)

	if a, ok := asms[it.ParentID]; ok && it.ParentID != "" {
		of = &gen.PartOf{
			Id: string(a.ID), Ref: domain.ItemRef{Factory: f.Metadata.Name, Number: a.Number}.String(), Title: a.Title,
			Part: a.PartNumber(it.ID), Parts: len(a.Parts),
		}
	}

	if a, ok := asms[it.ID]; ok {
		p := a.Progress()
		parts = &gen.AssemblyParts{Total: p.Total, Merged: p.Merged, InProgress: p.InProgress, Waiting: p.Waiting, Stations: p.Stations}
	}

	return of, parts
}

// assemblies reads, in one call, the assemblies among items and those items' parents are, with
// all their parts.
func (s *server) assemblies(ctx context.Context, f domain.FactoryID, items []domain.Item) (map[domain.ItemID]domain.Assembly, error) {
	ids := make([]domain.ItemID, 0, len(items))
	seen := map[domain.ItemID]bool{}

	for _, it := range items {
		for _, id := range []domain.ItemID{it.ID, it.ParentID} {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}

	asms, err := s.items.Assemblies(ctx, f, ids)
	if err != nil {
		return nil, fmt.Errorf("assemblies of the cards: %w", err)
	}

	return asms, nil
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
