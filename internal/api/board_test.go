package api_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// Every station shows its lanes by who works there: one per routed agent at build, labelled by
// role and model and counting that agent's live leases against the station's shared slots, one
// for the person at merge counting what waits on them, none at the queue and done stations.
func TestAPI_BoardLanesPerAgentRole(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for range 3 {
		h.seed(t, stBuild, domain.StateQueued, t0)
	}

	h.claim(t, 0)
	h.claim(t, 1)
	h.atMerge(t)

	var board gen.Board
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); code != http.StatusOK {
		t.Fatalf("board = %d", code)
	}

	slots := 2
	want := map[string][]gen.BoardLane{
		stInbox: {},
		stBuild: {
			// The runtime's model, not among goald's models, shows as its id.
			{Key: agBackend, Kind: gen.BoardLaneKindAgent, Role: "Backend", Model: new("claude-fable-5-1"), ModelLabel: new("claude-fable-5-1"), Slots: &slots, Busy: 2},
			{Key: "frontend", Kind: gen.BoardLaneKindAgent, Role: "Frontend", Model: new("claude-haiku-5-5"), ModelLabel: new("Haiku 5.5"), Slots: &slots, Busy: 0},
		},
		stMerge: {{Key: "person", Kind: gen.BoardLaneKindPerson, Role: "Merge", Busy: 1}},
		stDone:  {},
	}

	for _, st := range board.Stations {
		if st.Lanes == nil {
			t.Errorf("station %s has no lanes field", st.Id)
			continue
		}

		if !reflect.DeepEqual(*st.Lanes, want[st.Id]) {
			t.Errorf("station %s lanes =\n  %s\nwant\n  %s", st.Id, lanesString(*st.Lanes), lanesString(want[st.Id]))
		}
	}
}

// A card belongs to the lane that holds it or will pick it up: its lease's agent in progress,
// its lane's route while it waits, the person at a human station, and none at a queue or done
// station or without a route for its lane.
func TestAPI_BoardCardsBelongToTheirLane(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	queued := h.seed(t, stInbox, domain.StateQueued, t0)
	working := h.seed(t, stBuild, domain.StateQueued, t0)
	h.claim(t, 0)

	web := h.seedLane(t, "web")
	unrouted := h.seedLane(t, "ops")
	merging := h.atMerge(t)
	done := h.finished(t, t0, t0, false)

	var board gen.Board
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); code != http.StatusOK {
		t.Fatalf("board = %d", code)
	}

	var page gen.ItemPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/items", "", &page); code != http.StatusOK {
		t.Fatalf("items = %d", code)
	}

	listed := map[int64]gen.ItemCard{}
	for _, c := range page.Items {
		listed[c.Number] = c
	}

	cards := cardsOf(board)

	for _, tt := range []struct {
		name string
		it   domain.Item
		want *string
	}{
		{"queue station", queued, nil},
		{"in progress", working, new(agBackend)},
		{"waiting for its route", web, new("frontend")},
		{"no route for its lane", unrouted, nil},
		{"human station", merging, new("person")},
		{"done station", done, nil},
	} {
		for from, c := range map[string]gen.ItemCard{"board": cards[tt.it.Number], "items": listed[tt.it.Number]} {
			if c.Number != tt.it.Number {
				t.Errorf("%s: #%d not on the %s", tt.name, tt.it.Number, from)
				continue
			}

			if !reflect.DeepEqual(c.BoardLane, tt.want) {
				t.Errorf("%s: %s card board_lane = %v, want %v", tt.name, from, deref(c.BoardLane), deref(tt.want))
			}
		}
	}
}

// seedLane seeds a line item of lane queued at build.
func (h *harness) seedLane(t *testing.T, lane string) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: lineID, Type: typeFeature, Lane: lane, Title: lane + " item", Station: stBuild,
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

func lanesString(lanes []gen.BoardLane) string {
	b, err := json.Marshal(lanes)
	if err != nil {
		return err.Error()
	}

	return string(b)
}

func TestAPI_ItemCardsCarryAssemblyAndParts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	asm := h.seed(t, stInbox, domain.StateQueued, t0)
	h.seedPart(t, asm, stDone, domain.StateDone)
	second := h.seedPart(t, asm, stBuild, domain.StateInProgress)
	h.seedPart(t, asm, stMerge, domain.StateWaitingHuman)
	plain := h.seed(t, stBuild, domain.StateQueued, t0)

	wantParts := &gen.AssemblyParts{Total: 3, Merged: 1, InProgress: 1, Waiting: 1, Stations: []string{stBuild, stMerge}}

	// One read of assemblies a page, wherever the page's parents and parts are.
	cardsAt := func(path string, into any, cards func() []gen.ItemCard) map[int64]gen.ItemCard {
		t.Helper()

		h.seam.steps()

		if code := h.call(t, http.MethodGet, path, "", into); code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, code)
		}

		reads := 0

		for _, s := range h.seam.steps() {
			if s.read == "Assemblies" {
				reads++
			}
		}

		if reads != 1 {
			t.Errorf("GET %s read assemblies %d times, want once", path, reads)
		}

		out := map[int64]gen.ItemCard{}
		for _, c := range cards() {
			out[c.Number] = c
		}

		return out
	}

	var (
		list, alonePage gen.ItemPage
		board           gen.Board
	)

	listed := cardsAt("/v1/orgs/acme/factories/line/items", &list, func() []gen.ItemCard { return list.Items })
	boarded := cardsAt("/v1/orgs/acme/factories/line/board", &board, func() []gen.ItemCard { return slices.Collect(maps.Values(cardsOf(board))) })

	for from, cards := range map[string]map[int64]gen.ItemCard{"items": listed, "board": boarded} {
		a, part, other := cards[asm.Number], cards[second.Number], cards[plain.Number]

		if !reflect.DeepEqual(a.Parts, wantParts) || a.Assembly != nil {
			t.Errorf("%s: assembly card parts = %+v, assembly = %+v; want %+v and none", from, a.Parts, a.Assembly, wantParts)
		}

		want := &gen.PartOf{Id: string(asm.ID), Ref: a.Ref, Title: asm.Title, Part: 2, Parts: 3}
		if !reflect.DeepEqual(part.Assembly, want) || part.Parts != nil {
			t.Errorf("%s: second part's assembly = %+v, parts = %+v; want %+v and none", from, part.Assembly, part.Parts, want)
		}

		if other.Number != plain.Number || other.Assembly != nil || other.Parts != nil {
			t.Errorf("%s: plain card = %+v; want neither assembly nor parts", from, other)
		}
	}

	// A page holding the part alone still names its assembly.
	alone := cardsAt("/v1/orgs/acme/factories/line/items?state=in_progress", &alonePage, func() []gen.ItemCard { return alonePage.Items })
	if got := alone[second.Number].Assembly; len(alone) != 1 || got == nil || got.Part != 2 || got.Parts != 3 {
		t.Errorf("in-progress page = %+v; want only the second part, 2 of 3", alone)
	}
}

// seedPart seeds a part of assembly asm at station in state.
func (h *harness) seedPart(t *testing.T, asm domain.Item, station string, state domain.ItemState) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: lineID, ParentID: asm.ID, Type: typeFeature, Lane: "api", Title: "part at " + station, Station: station,
		State: state, Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}
