package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Names of the line factory.
const (
	stBuild   = "build"
	stMerge   = "merge"
	stDone    = "done"
	agBackend = "backend"
	laneAPI   = "api"
	prURL     = "https://github.com/acme/app/pull/7"
)

// build is the line factory's agent station, as the store sees it.
var build = domain.Station{
	ID: stBuild, Kind: domain.KindAgent, Route: map[string]string{laneAPI: agBackend, "web": "frontend"}, Agents: 2, WIP: 2,
}

// seed creates an item of the line factory straight in the store, at station in state.
func (h *harness) seed(t *testing.T, station string, state domain.ItemState, created time.Time) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: lineID, Type: typeFeature, Lane: laneAPI, Title: "item at " + station, Station: station,
		State: state, Class: domain.ClassStandard, CreatedAt: created,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// move changes an item in the store, as the engine would have.
func (h *harness) move(t *testing.T, it domain.Item, to domain.Position, mutate func(*ports.Transition)) domain.Item {
	t.Helper()

	tr := ports.Transition{ItemID: it.ID, From: it.Position(), To: to, At: h.clock.Now()}
	if mutate != nil {
		mutate(&tr)
	}

	got, err := h.store.Transition(inOrg(t), tr)
	if err != nil {
		t.Fatal(err)
	}

	return got
}

func (h *harness) claim(t *testing.T, slot int) domain.Item {
	t.Helper()

	it, _, err := h.store.ClaimNext(inOrg(t), ports.Claim{
		FactoryID: lineID, Station: build, Slot: slot, Now: h.clock.Now(), ExpiresAt: h.clock.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// atMerge seeds an item waiting at the merge station whose build output names prURL.
func (h *harness) atMerge(t *testing.T) domain.Item {
	t.Helper()

	it := h.seed(t, stBuild, domain.StateQueued, t0)

	return h.move(t, it, domain.Position{Station: stMerge, State: domain.StateWaitingHuman}, func(tr *ports.Transition) {
		tr.OutputStation, tr.Output = stBuild, json.RawMessage(`{"pull_request":"`+prURL+`"}`)
	})
}

// finished seeds a line item created at created and done at done.
func (h *harness) finished(t *testing.T, created, done time.Time, rework bool) domain.Item {
	t.Helper()

	return h.move(t, h.seed(t, stMerge, domain.StateWaitingHuman, created), domain.Position{Station: stDone, State: domain.StateDone},
		func(tr *ports.Transition) { tr.DoneAt, tr.Rework = done, rework })
}

func TestAPI_ItemsByReference(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	submitted := h.submit(t)

	if submitted.Ref != "acme#1" {
		t.Errorf("ref = %q, want acme#1", submitted.Ref)
	}

	for _, ref := range []string{"acme%231", acmeID + "%231", submitted.Id} {
		var it gen.Item
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+ref, "", &it); code != http.StatusOK || it.Id != submitted.Id || it.Ref != "acme#1" {
			t.Errorf("GET /v1/items/%s = %d %s %s, want the item", ref, code, it.Id, it.Ref)
		}
	}

	for _, ref := range []string{"acme%232", "nope%231"} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+ref, "", nil); code != http.StatusNotFound {
			t.Errorf("GET /v1/items/%s = %d, want 404", ref, code)
		}
	}

	if err := h.engine.Tick(inOrg(t)); err != nil { // inbox → approve
		t.Fatal(err)
	}

	var page gen.EventPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/acme%231/events", "", &page); code != http.StatusOK || len(page.Events) == 0 {
		t.Errorf("events by reference = %d %+v", code, page)
	}

	var approved gen.Item
	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/acme%231/approve", ``, &approved); code != http.StatusOK || approved.Station != stDone {
		t.Errorf("approve by reference = %d at %s, want 200 at done", code, approved.Station)
	}
}

// boardStation is what a test checks of one station on the board.
type boardStation struct {
	ID, Action          string
	Agents              []string
	WIP                 int
	Waiting, InProgress int
	Items               []int64
}

func stationsOf(b gen.Board) []boardStation {
	out := make([]boardStation, 0, len(b.Stations))

	for _, st := range b.Stations {
		s := boardStation{ID: st.Id, Agents: st.Agents, Waiting: st.Waiting, InProgress: st.InProgress, Items: []int64{}}
		if st.Action != nil {
			s.Action = string(*st.Action)
		}

		if st.WipLimit != nil {
			s.WIP = *st.WipLimit
		}

		for _, c := range st.Items {
			s.Items = append(s.Items, c.Number)
		}

		out = append(out, s)
	}

	return out
}

func cardsOf(b gen.Board) map[int64]gen.ItemCard {
	cards := map[int64]gen.ItemCard{}

	for _, st := range b.Stations {
		for _, c := range st.Items {
			cards[c.Number] = c
		}
	}

	return cards
}

func TestAPI_BoardShowsEveryStationInLineOrder(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, "inbox", domain.StateQueued, t0)

	for range 3 {
		h.seed(t, stBuild, domain.StateQueued, t0)
	}

	h.claim(t, 0)
	h.claim(t, 1)
	h.atMerge(t)
	h.finished(t, t0.Add(-2*time.Hour), t0.Add(-time.Hour), false)

	var board gen.Board
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); code != http.StatusOK {
		t.Fatalf("board = %d", code)
	}

	want := []boardStation{
		{ID: "inbox", Agents: []string{}, Waiting: 1, Items: []int64{1}},
		{ID: stBuild, Agents: []string{agBackend, "frontend"}, WIP: 2, Waiting: 1, InProgress: 2, Items: []int64{4, 3, 2}},
		// An item waiting for a person's decision is the human station's work in progress.
		{ID: stMerge, Action: stMerge, Agents: []string{}, InProgress: 1, Items: []int64{5}},
		{ID: stDone, Agents: []string{}, Items: []int64{6}},
	}
	if got := stationsOf(board); !reflect.DeepEqual(got, want) {
		t.Errorf("stations =\n  %+v\nwant\n  %+v", got, want)
	}

	if board.Factory.Id != lineID || board.Factory.Name != "line" || board.Constraint == nil || *board.Constraint != stBuild {
		t.Errorf("board factory %+v constraint %v, want line with build as the constraint", board.Factory, board.Constraint)
	}
}

// A merge lands every pull request the outputs name: the card says which, at the merge station only.
func TestAPI_BoardCardsNameEveryPullRequestAMergeLands(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, stBuild, domain.StateQueued, t0)
	working := h.claim(t, 0)
	merging := h.atMerge(t)

	var board gen.Board
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); code != http.StatusOK {
		t.Fatalf("board = %d", code)
	}

	cards := cardsOf(board)

	if c := cards[merging.Number]; !reflect.DeepEqual(c.PullRequests, []string{prURL}) {
		t.Errorf("merge card pull_requests %v, want [%s]", c.PullRequests, prURL)
	}

	if c := cards[working.Number]; c.PullRequests == nil || len(c.PullRequests) != 0 {
		t.Errorf("in-progress card pull_requests %#v, want [] away from a merge station", c.PullRequests)
	}
}

func TestAPI_BoardCardsShowAgentPullRequestAndAge(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, stBuild, domain.StateQueued, t0)
	working := h.claim(t, 0)
	merging := h.atMerge(t)
	finished := h.finished(t, t0.Add(-2*time.Hour), t0.Add(-time.Hour), false)

	var board gen.Board
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); code != http.StatusOK {
		t.Fatalf("board = %d", code)
	}

	cards := cardsOf(board)

	if c := cards[working.Number]; c.Agent == nil || *c.Agent != agBackend || c.State != gen.ItemState(domain.StateInProgress) {
		t.Errorf("in-progress card %+v, want agent backend", c)
	}

	if c := cards[merging.Number]; c.PrUrl == nil || *c.PrUrl != prURL || c.Agent != nil || c.Ref != "line#2" {
		t.Errorf("merge card %+v, want line#2 with its pull request and no agent", c)
	}

	if c := cards[finished.Number]; c.AgeSeconds != int64(time.Hour/time.Second) {
		t.Errorf("done card age %d s, want created → done (3600)", c.AgeSeconds)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/nope/board", "", nil); code != http.StatusNotFound {
		t.Errorf("board of an unknown factory = %d, want 404", code)
	}
}

// A card whose item has a running run says what the run is doing now, from its latest tool call,
// and how far it is through its latest todo list, on the board and in the items list; a queued
// card says neither.
func TestAPI_BoardCardsSayWhatTheRunIsDoing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, stBuild, domain.StateQueued, t0)
	queued := h.seed(t, stBuild, domain.StateQueued, t0)
	working := h.claim(t, 0)

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{FactoryID: lineID, ItemID: working.ID, Station: stBuild, Agent: agBackend, StartedAt: t0})
	if err != nil {
		t.Fatal(err)
	}

	call := func(tool, input string) domain.Event {
		return domain.Event{
			FactoryID: lineID, ItemID: working.ID, RunID: run, At: t0, Kind: domain.EventToolCall, Station: stBuild,
			Data: map[string]any{"id": tool, domain.ToolCallTool: tool, domain.ToolCallInput: input},
		}
	}

	todos := `{"todos":[{"status":"completed"},{"status":"completed"},{"status":"in_progress"},{"status":"pending"},{"status":"pending"}]}`
	if err := h.store.AppendEvents(inOrg(t),
		call("Read", `{"file_path":"AGENTS.md"}`), call(domain.ToolTodoWrite, todos), call("Edit", `{"file_path":"internal/x.go"}`),
	); err != nil {
		t.Fatal(err)
	}

	check := func(where string, c gen.ItemCard) {
		t.Helper()

		if c.Activity == nil || *c.Activity != "Editing internal/x.go" || c.PlanSteps == nil || *c.PlanSteps != (gen.PlanSteps{Done: 2, Total: 5}) {
			t.Errorf("%s: in-progress card activity %v, plan steps %v; want 'Editing internal/x.go', 2 of 5", where, c.Activity, c.PlanSteps)
		}
	}

	var board gen.Board
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); code != http.StatusOK {
		t.Fatalf("board = %d", code)
	}

	cards := cardsOf(board)
	check("board", cards[working.Number])

	if c := cards[queued.Number]; c.Activity != nil || c.PlanSteps != nil {
		t.Errorf("queued card activity %v, plan steps %v; want neither", c.Activity, c.PlanSteps)
	}

	var page gen.ItemPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/items?state=in_progress", "", &page); code != http.StatusOK || len(page.Items) != 1 {
		t.Fatalf("items = %d with %d cards, want one", code, len(page.Items))
	}

	check("items", page.Items[0])
}

// stationStats is what a test checks of one station's statistics on the board.
type stationStats struct {
	Busy, SendBack *float64
	Trend          *gen.QueueTrend
	Oldest         *int64
	DoneToday      *int
}

// boardHistory seeds the line factory with a history for its board's statistics, all before t0:
// an item waiting at inbox for 2 h 10 min; at build, a lease from 4 h to 3 h ago that sent its
// item (merging) on to merge with a summary, a lease held from 2 h ago on slot 0 whose run
// (working) reported three turns, and an item waiting there for an hour; at done, an item done an
// hour ago and one done yesterday.
func (h *harness) boardHistory(t *testing.T) (working, merging domain.Item) {
	t.Helper()

	ago := func(d time.Duration) time.Time { return t0.Add(-d) }

	h.seed(t, stInbox, domain.StateQueued, ago(2*time.Hour+10*time.Minute))
	merging = h.seed(t, stBuild, domain.StateQueued, ago(5*time.Hour))
	working = h.seed(t, stBuild, domain.StateQueued, ago(5*time.Hour))

	claim := func(slot int, at time.Time) (domain.Item, domain.Lease) {
		it, l, err := h.store.ClaimNext(inOrg(t), ports.Claim{
			FactoryID: lineID, Station: build, Slot: slot, Now: at, ExpiresAt: at.Add(time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}

		return it, l
	}

	first, l := claim(1, ago(4*time.Hour))
	if first.ID != merging.ID {
		t.Fatalf("claimed #%d first, want #%d", first.Number, merging.Number)
	}

	if _, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: first.ID, From: first.Position(), To: domain.Position{Station: stMerge, State: domain.StateWaitingHuman},
		Lease: &l, Outcome: domain.LeaseAdvanced, OutputStation: stBuild, Output: json.RawMessage(`{"summary":"Built it"}`),
		At: ago(3 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	claim(0, ago(2*time.Hour))

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{
		FactoryID: lineID, ItemID: working.ID, Station: stBuild, Agent: agBackend, Model: "claude-opus-5-5", StartedAt: ago(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	usage := domain.Event{FactoryID: lineID, ItemID: working.ID, RunID: run, At: ago(time.Hour), Kind: domain.EventUsage, Station: stBuild}
	if err := h.store.AppendEvents(inOrg(t), usage, usage, usage); err != nil {
		t.Fatal(err)
	}

	h.seed(t, stBuild, domain.StateQueued, ago(time.Hour))
	h.finished(t, ago(5*time.Hour), ago(time.Hour), false)
	h.finished(t, ago(30*time.Hour), ago(20*time.Hour), false)

	return working, merging
}

// Every station carries its statistics: the busy share of an agent station since its lease
// history began (inside the window), the trend and oldest wait of the queues, the items done
// today at the done station; an item in progress shows the copy working it and its run's turns, a
// card its latest summary.
func TestAPI_BoardStationStatistics(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	working, merging := h.boardHistory(t)

	var board gen.Board
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); code != http.StatusOK {
		t.Fatalf("board = %d", code)
	}

	ratio := func(f float64) *float64 { return &f }
	secs := func(d time.Duration) *int64 { n := int64(d / time.Second); return &n }
	growing, today := gen.Growing, 1

	got := map[string]stationStats{}
	for _, st := range board.Stations {
		got[st.Id] = stationStats{st.BusyRatio, st.SendBackRatio, st.QueueTrend, st.OldestWaitingSeconds, st.DoneToday}
	}

	want := map[string]stationStats{
		stInbox: {Trend: &growing, Oldest: secs(2*time.Hour + 10*time.Minute)},
		// 1 h of the first lease and 2 h of the live one, over 2 slots × the 4 h since the first.
		stBuild: {Busy: ratio(3.0 / 8), Trend: &growing, Oldest: secs(time.Hour)},
		stMerge: {},
		stDone:  {DoneToday: &today},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("station statistics =\n  %s\nwant\n  %s", statsString(got), statsString(want))
	}

	cards := cardsOf(board)

	if c := cards[working.Number]; c.Slot == nil || *c.Slot != 1 || c.Turn == nil || *c.Turn != 3 {
		t.Errorf("in-progress card slot %v, turn %v; want the first copy (1) on turn 3", c.Slot, c.Turn)
	}

	if c := cards[merging.Number]; c.Summary == nil || *c.Summary != "Built it" || c.Slot != nil || c.Turn != nil {
		t.Errorf("merge card summary %v, slot %v, turn %v; want its build summary and no copy or turn", c.Summary, c.Slot, c.Turn)
	}
}

// expedite creates an expedite fix of the line factory at station in state.
func (h *harness) expedite(t *testing.T, station string, state domain.ItemState) {
	t.Helper()

	if _, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: lineID, Type: "fix", Title: "expedite at " + station, Station: station, State: state,
		Class: domain.ClassExpedite, CreatedAt: t0,
	}); err != nil {
		t.Fatal(err)
	}
}

// spend records an agent run of item started at `at` that cost usd.
func (h *harness) spend(t *testing.T, item domain.ItemID, at time.Time, usd float64) {
	t.Helper()

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{FactoryID: lineID, ItemID: item, Station: stBuild, Agent: agBackend, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(inOrg(t), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

// The line shows its work in progress against the stations' limits, its drum-buffer-rope release
// as configured (not enforced: nothing is held by the rope) with the items in the drum's buffer,
// the expedite lane's use against its limit and the weekly budget over the last 7 days.
func TestAPI_BoardLine(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	queued := h.seed(t, stInbox, domain.StateQueued, t0)

	for range 3 {
		h.seed(t, stBuild, domain.StateQueued, t0)
	}

	h.claim(t, 0)

	h.expedite(t, stInbox, domain.StateQueued) // not released yet
	h.expedite(t, stMerge, domain.StateWaitingHuman)

	h.spend(t, queued.ID, t0.Add(-2*24*time.Hour), 30)
	h.spend(t, queued.ID, t0.Add(-8*24*time.Hour), 50) // before the week

	board := func() gen.Board {
		var b gen.Board
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &b); code != http.StatusOK {
			t.Fatalf("board = %d", code)
		}

		return b
	}

	drum, buffer, limit, wipLimit := stBuild, 2, 1, 2
	want := gen.BoardLine{
		Status:   gen.BoardLineStatusRunning,
		Wip:      gen.LineWip{InProgress: 1, Limit: &wipLimit},
		Release:  gen.LineRelease{Policy: gen.DrumBufferRope, Enforced: false, Drum: &drum, Buffer: &buffer, InBuffer: 2},
		Expedite: &gen.LineExpedite{Limit: &limit, InUse: 1},
		Budget:   &gen.LineBudget{WeeklyUsd: 70, SpentUsd: 30, Exhausted: false},
	}

	b := board()
	if got := normalize(t, b.Line); got != normalize(t, want) {
		t.Errorf("line = %s\nwant   %s", got, normalize(t, want))
	}

	for _, c := range cardsOf(b) {
		inBuffer := c.Station == stBuild && c.State == gen.ItemState(domain.StateQueued)
		if c.InBuffer != inBuffer || c.RopeHeld {
			t.Errorf("card #%d at %s %s: in_buffer %v, rope_held %v; want in_buffer %v and never held", c.Number, c.Station, c.State, c.InBuffer, c.RopeHeld, inBuffer)
		}
	}

	h.spend(t, queued.ID, t0.Add(-time.Hour), 45)

	if got := board().Line.Budget; got == nil || got.SpentUsd != 75 || !got.Exhausted {
		t.Errorf("budget after $45 more = %+v, want $75 spent, exhausted", got)
	}
}

// The board's statistics window is 1 to 90 days, written Nd.
func TestAPI_BoardRefusesBadWindows(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, window := range []string{"0d", "91d", "week"} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board?window="+window, "", nil); code != http.StatusBadRequest {
			t.Errorf("board over window %q = %d, want 400", window, code)
		}
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board?window=30d", "", nil); code != http.StatusOK {
		t.Errorf("board over 30 days = %d, want 200", code)
	}
}

func statsString(m map[string]stationStats) string {
	var b strings.Builder

	for _, id := range []string{stInbox, stBuild, stMerge, stDone} {
		s := m[id]
		fmt.Fprintf(&b, "%s{busy %v send-back %v trend %v oldest %v done today %v} ", id,
			show(s.Busy), show(s.SendBack), show(s.Trend), show(s.Oldest), show(s.DoneToday))
	}

	return b.String()
}

func show[T any](p *T) string {
	if p == nil {
		return "null"
	}

	return fmt.Sprint(*p)
}

// list asks for one page of line items and returns their numbers and the next cursor.
func (h *harness) list(t *testing.T, query string) ([]int64, *int64) {
	t.Helper()

	var p gen.ItemPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/items"+query, "", &p); code != http.StatusOK {
		t.Fatalf("list %s = %d", query, code)
	}

	numbers := []int64{}
	for _, c := range p.Items {
		numbers = append(numbers, c.Number)
	}

	return numbers, p.NextAfter
}

func TestAPI_ListItemsNewestFirstByPage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for range 5 {
		h.seed(t, stBuild, domain.StateQueued, t0.Add(-time.Hour))
	}

	h.claim(t, 0) // #1

	pages := []struct {
		query string
		want  []int64
		next  int64 // 0: the last page
	}{
		{"?limit=2", []int64{5, 4}, 4},
		{"?limit=2&after=4", []int64{3, 2}, 2},
		{"?limit=2&after=2", []int64{1}, 0},
		{"?state=in_progress&station=build", []int64{1}, 0},
		{"?station=merge", []int64{}, 0},
	}

	for _, p := range pages {
		got, next := h.list(t, p.query)
		if !reflect.DeepEqual(got, p.want) || (next == nil) != (p.next == 0) || (next != nil && *next != p.next) {
			t.Errorf("list %s = %v next %v; want %v next %d", p.query, got, next, p.want, p.next)
		}
	}

	var page gen.ItemPage
	h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/items?limit=1", "", &page)

	if c := page.Items[0]; c.Ref != "line#5" || c.AgeSeconds != 3600 || c.Lane == nil || *c.Lane != laneAPI || c.Agent != nil || c.PrUrl != nil {
		t.Errorf("card %+v, want line#5, an hour old, lane api, no agent, no pull request", c)
	}
}

func TestAPI_ListItemsRefusesBadQueries(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, query := range []string{"?limit=0", "?limit=201", "?after=0", "?state=sleeping"} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/items"+query, "", nil); code != http.StatusBadRequest {
			t.Errorf("list %s = %d, want 400", query, code)
		}
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/nope/items", "", nil); code != http.StatusNotFound {
		t.Errorf("items of an unknown factory = %d, want 404", code)
	}
}

func TestAPI_MetricsOverTheWindow(t *testing.T) {
	t.Parallel()

	h := newHarness(t) // the clock reads 2026-10-23 09:00 UTC
	day := func(d, hour int) time.Time { return time.Date(2026, 10, d, hour, 0, 0, 0, time.UTC) }

	h.finished(t, day(23, 5), day(23, 7), true)  // 2h, reworked
	h.finished(t, day(21, 0), day(21, 4), false) // 4h
	h.finished(t, day(10, 0), day(10, 1), false) // before the window

	open := h.seed(t, stBuild, domain.StateQueued, day(22, 0))

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{FactoryID: lineID, ItemID: open.ID, Station: stBuild, Agent: agBackend, StartedAt: day(23, 8)})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(inOrg(t), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.5}, day(23, 8)); err != nil {
		t.Fatal(err)
	}

	var m gen.Metrics
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics?window=7d", "", &m); code != http.StatusOK {
		t.Fatalf("metrics = %d", code)
	}

	want := "7d: 2026-10-17:0 2026-10-18:0 2026-10-19:0 2026-10-20:0 2026-10-21:1 2026-10-22:0 2026-10-23:1; " +
		"2 done, lead time p50 3.00h p85 3.70h, rework 0.50, wip 1, $0.50"
	if got := summary(m); got != want {
		t.Errorf("metrics\n  %s\nwant\n  %s", got, want)
	}

	var today gen.Metrics
	h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics?window=1d", "", &today)

	if want := "1d: 2026-10-23:1; 1 done, lead time p50 2.00h p85 2.00h, rework 1.00, wip 1, $0.50"; summary(today) != want {
		t.Errorf("metrics\n  %s\nwant\n  %s", summary(today), want)
	}
}

// summary renders metrics as one line, so a test compares them whole.
func summary(m gen.Metrics) string {
	days := make([]string, 0, len(m.Throughput))
	for _, d := range m.Throughput {
		days = append(days, fmt.Sprintf("%s:%d", d.Day, d.Done))
	}

	lead := "no lead time"
	if l := m.LeadTimeHours; l != nil {
		lead = fmt.Sprintf("lead time p50 %.2fh p85 %.2fh", l.P50, l.P85)
	}

	return fmt.Sprintf("%s: %s; %d done, %s, rework %.2f, wip %d, $%.2f",
		m.Window, strings.Join(days, " "), m.ItemsDone, lead, m.ReworkRate, m.Wip, m.SpendUsd)
}

func TestAPI_MetricsWithNothingDoneAndBadWindows(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var m gen.Metrics
	if h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/metrics", "", &m); m.Window != "7d" || len(m.Throughput) != 7 || m.LeadTimeHours != nil || m.AssemblyLeadTimeHours != nil || m.ReworkRate != 0 {
		t.Errorf("metrics with nothing done = %+v, want 7 empty days and no lead times", m)
	}

	for _, w := range []string{"0d", "91d", "7", "1w"} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics?window="+w, "", nil); code != http.StatusBadRequest {
			t.Errorf("window %s = %d, want 400", w, code)
		}
	}
}

func TestAPI_RetryABlockedItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, stBuild, domain.StateQueued, t0)
	h.move(t, h.claim(t, 0), domain.Position{Station: stBuild, State: domain.StateBlocked}, nil)

	var it gen.Item
	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/line%231/retry", ``, &it); code != http.StatusOK ||
		it.State != gen.ItemState(domain.StateQueued) || it.Station != stBuild || it.ReworkCount != 0 {
		t.Errorf("retry = %d %s at %s rework %d, want 200, queued at build, no rework", code, it.State, it.Station, it.ReworkCount)
	}

	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/line%231/retry", ``, nil); code != http.StatusConflict {
		t.Errorf("retry of a queued item = %d, want 409", code)
	}
}

func TestAPI_MergeAtTheMergeStation(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.atMerge(t)
	h.codehost.Add(ports.PullRequest{
		URL: prURL, Open: true, Mergeable: true, HeadSHA: "4b825dc6", HeadBranch: "goal/1-x", SameRepository: true, BaseBranch: "main",
	})

	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/line%231/merge", `{"method":"rebase"}`, nil); code != http.StatusBadRequest {
		t.Errorf("merge with method rebase = %d, want 400", code)
	}

	var it gen.Item
	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/line%231/merge", ``, &it); code != http.StatusOK || it.Station != stDone {
		t.Errorf("merge = %d at %s, want 200 at done", code, it.Station)
	}

	if merges := h.codehost.Merges(); len(merges) != 1 || merges[0].Method != "squash" {
		t.Errorf("merges %+v, want one squash", merges)
	}
}

func TestAPI_MergeRefusalsAreConflicts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	stuck := h.atMerge(t)
	h.codehost.Add(ports.PullRequest{URL: prURL, Open: true, HeadSHA: "5c1f0e2a", SameRepository: true}) // not mergeable

	var p gen.Problem
	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/line%231/merge", ``, &p); code != http.StatusConflict ||
		p.Code == nil || *p.Code != "cannot-merge" || p.Detail == nil || !strings.Contains(*p.Detail, prURL) {
		t.Errorf("merge of an unmergeable pull request = %d %+v, want 409 cannot-merge naming it", code, p)
	}

	if got, _ := h.store.Item(inOrg(t), stuck.ID); got.Position() != stuck.Position() {
		t.Errorf("refused merge moved the item to %+v", got.Position())
	}

	// A pull request on a host none of the factory's connections serves.
	elsewhere := h.seed(t, stBuild, domain.StateQueued, t0)
	h.move(t, elsewhere, domain.Position{Station: stMerge, State: domain.StateWaitingHuman}, func(tr *ports.Transition) {
		tr.OutputStation, tr.Output = stBuild, json.RawMessage(`{"pull_requests":["https://gitlab.example.com/acme/app/-/merge_requests/1"]}`)
	})

	p = gen.Problem{}
	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/"+string(elsewhere.ID)+"/merge", ``, &p); code != http.StatusConflict ||
		p.Code == nil || *p.Code != "no-code-host-connection" {
		t.Errorf("merge on a host no connection serves = %d %+v, want 409 no-code-host-connection", code, p)
	}

	// Approving at a human station without a merge action is no merge.
	acme := h.submit(t)
	if err := h.engine.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/"+acme.Id+"/merge", ``, nil); code != http.StatusConflict {
		t.Errorf("merge at a plain human station = %d, want 409", code)
	}
}

// sse reads Server-Sent Events: each call returns the next event's lines (comments included)
// up to the blank line that ends it.
type sse struct{ r *bufio.Reader }

func (s sse) next(t *testing.T) []string {
	t.Helper()

	var lines []string

	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream: %v (so far %q)", err, lines)
		}

		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			return lines
		}

		lines = append(lines, line)
	}
}

// stream opens /v1/stream with query and returns its status, content type and events. The
// stream is closed when the test ends.
func (h *harness) stream(t *testing.T, query string) (int, string, sse) {
	t.Helper()

	ctx, cancel := context.WithTimeout(inOrg(t), 10*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/v1/orgs/acme/stream"+query, nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = res.Body.Close() })

	return res.StatusCode, res.Header.Get("Content-Type"), sse{r: bufio.NewReader(res.Body)}
}

func TestAPI_StreamSendsItemEventsAsTheyHappen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	code, contentType, events := h.stream(t, "?factory=line")
	if code != http.StatusOK || contentType != "text/event-stream" {
		t.Fatalf("stream = %d %s, want an event stream", code, contentType)
	}

	if got := events.next(t); !reflect.DeepEqual(got, []string{"retry: 3000"}) {
		t.Fatalf("stream opens with %q, want the retry delay", got)
	}

	at := t0.Add(time.Second)
	h.notices.Publish(
		ports.ItemNotice{OrgID: testOrg, EventID: 1, FactoryID: acmeID, ItemID: "acme-item", Number: 1, Kind: domain.EventCreated, Station: "inbox", State: domain.StateQueued, At: at},
		ports.ItemNotice{OrgID: otherOrg, EventID: 3, FactoryID: lineID, ItemID: "other-item", Number: 1, Kind: domain.EventCreated, Station: "inbox", State: domain.StateQueued, At: at},
		ports.ItemNotice{Resync: true},
		ports.ItemNotice{OrgID: testOrg, EventID: 2, FactoryID: lineID, ItemID: "line-item", Number: 7, Kind: domain.EventMoved, Station: stBuild, State: domain.StateWaitingHuman, At: at},
		ports.ItemNotice{OrgID: otherOrg, FactoryID: lineID, Org: &ports.OrgEvent{Name: "credential", FactoryID: lineID, Data: json.RawMessage(`{"name":"theirs"}`)}},
		ports.ItemNotice{OrgID: testOrg, FactoryID: lineID, Org: &ports.OrgEvent{Name: "credential", FactoryID: lineID, Data: json.RawMessage("{\n \"name\": \"repo\"}")}},
	)

	if got := events.next(t); !reflect.DeepEqual(got, []string{"event: resync", "data: {}"}) {
		t.Errorf("event %q, want a resync (and no event of another factory or organization)", got)
	}

	station := stBuild
	checkItemEvent(t, events.next(t), "id: 2", gen.StreamEvent{
		OrgId: string(testOrg), ItemId: "line-item", Ref: "line#7", FactoryId: lineID, Kind: "moved", Station: &station,
		State: gen.ItemState(domain.StateWaitingHuman), At: at,
	})

	if got := events.next(t); !reflect.DeepEqual(got, []string{"event: credential", `data: {"name":"repo"}`}) {
		t.Errorf("event %q, want the organization's own credential event, on one line", got)
	}

	h.clock.Advance(15 * time.Second)

	if got := events.next(t); !reflect.DeepEqual(got, []string{": heartbeat"}) {
		t.Errorf("after 15s %q, want a heartbeat comment", got)
	}
}

// checkItemEvent checks one SSE event: an item event with the id line and the data want.
func checkItemEvent(t *testing.T, got []string, id string, want gen.StreamEvent) {
	t.Helper()

	if len(got) != 3 || got[0] != "event: item" || got[1] != id || !strings.HasPrefix(got[2], "data: ") {
		t.Fatalf("event %q, want an item event with %s", got, id)
	}

	var data gen.StreamEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[2], "data: ")), &data); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(data, want) {
		t.Errorf("data %+v, want %+v", data, want)
	}
}

func TestAPI_StreamOfAnUnknownFactoryIsNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if code, _, _ := h.stream(t, "?factory=nope"); code != http.StatusNotFound {
		t.Errorf("stream of an unknown factory = %d, want 404", code)
	}
}
