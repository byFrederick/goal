package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
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

// An assembly's card on the list says where its parts stand, and each part's card which part of
// which assembly it is, on the list and the board (which holds parts, not their assembly), even
// when the page holds the part alone, read once a page. A child without a part key is no part; a
// plain item's card has neither.
func TestAPI_ItemCardsCarryAssemblyAndParts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	asm, parts := h.assembled(t, "one", "two", "three")
	h.move(t, parts[0], domain.Position{Station: stDone, State: domain.StateDone}, func(tr *ports.Transition) { tr.DoneAt = t0 })
	second := h.move(t, parts[1], domain.Position{Station: stBuild, State: domain.StateInProgress}, nil)
	h.move(t, parts[2], domain.Position{Station: stMerge, State: domain.StateWaitingHuman}, nil)
	h.child(t, asm) // created without a part key: no part
	plain := h.seed(t, stBuild, domain.StateQueued, t0)

	wantParts := &gen.AssemblyParts{Total: 3, Merged: 1, InProgress: 1, Waiting: 1, Stations: []string{stBuild, stMerge}}
	wantOf := &gen.PartOf{Id: string(asm.ID), Ref: "line#" + strconv.FormatInt(asm.Number, 10), Title: asm.Title, Part: 2, Parts: 3}

	var (
		list, alone gen.ItemPage
		board       gen.Board
	)

	h.getReadingAssembliesOnce(t, "/v1/orgs/acme/factories/line/items", &list)
	h.getReadingAssembliesOnce(t, "/v1/orgs/acme/factories/line/board", &board)
	h.getReadingAssembliesOnce(t, "/v1/orgs/acme/factories/line/items?state=in_progress", &alone)

	if _, ok := cardsOf(board)[asm.Number]; ok {
		t.Errorf("board shows the assembly %d; want only its parts on the line", asm.Number)
	}

	for from, cards := range map[string]map[int64]gen.ItemCard{"items": byNumber(list.Items), "board": cardsOf(board)} {
		for _, tt := range []struct {
			name  string
			it    domain.Item
			of    *gen.PartOf
			parts *gen.AssemblyParts
		}{
			{"assembly", asm, nil, wantParts},
			{"second part", second, wantOf, nil},
			{"plain item", plain, nil, nil},
		} {
			if from == "board" && tt.it.ID == asm.ID {
				continue
			}

			c, ok := cards[tt.it.Number]
			if !ok || !reflect.DeepEqual(c.Assembly, tt.of) || !reflect.DeepEqual(c.Parts, tt.parts) {
				t.Errorf("%s: %s card assembly = %+v, parts = %+v (on it: %t); want %+v and %+v", from, tt.name, c.Assembly, c.Parts, ok, tt.of, tt.parts)
			}
		}
	}

	if len(alone.Items) != 1 || !reflect.DeepEqual(alone.Items[0].Assembly, wantOf) {
		t.Errorf("in-progress page = %+v; want only the second part, of its assembly %+v", alone.Items, wantOf)
	}
}

// getReadingAssembliesOnce GETs path into out and checks it read the assemblies once.
func (h *harness) getReadingAssembliesOnce(t *testing.T, path string, out any) {
	t.Helper()

	h.seam.steps()

	if code := h.call(t, http.MethodGet, path, "", out); code != http.StatusOK {
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
}

func byNumber(cards []gen.ItemCard) map[int64]gen.ItemCard {
	out := map[int64]gen.ItemCard{}
	for _, c := range cards {
		out[c.Number] = c
	}

	return out
}

// child creates an item under parent the way CreateItem allows, without a part key.
func (h *harness) child(t *testing.T, parent domain.Item) {
	t.Helper()

	if _, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: lineID, ParentID: parent.ID, Type: typeFeature, Title: "a child", Station: stBuild,
		State: domain.StateReady, Class: domain.ClassStandard, CreatedAt: t0,
	}); err != nil {
		t.Fatal(err)
	}
}

// assembled makes a new item at the build station an assembly of parts with the given keys, as
// the engine does: claimed, then assembled, its parts queued at build in plan order.
func (h *harness) assembled(t *testing.T, keys ...string) (domain.Item, []domain.Item) {
	t.Helper()

	h.seed(t, stBuild, domain.StateQueued, t0)

	it, l, err := h.store.ClaimNext(inOrg(t), ports.Claim{
		FactoryID: lineID, Station: build, Now: h.clock.Now(), ExpiresAt: h.clock.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := make([]domain.NewPart, 0, len(keys))
	for _, k := range keys {
		plan = append(plan, domain.NewPart{Key: k, Title: "part " + k})
	}

	asm, parts, err := h.store.Assemble(inOrg(t), ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stBuild, State: domain.StateAssembly},
			Lease: &l, Outcome: domain.LeaseAdvanced, At: h.clock.Now(),
		},
		Parts: plan,
	})
	if err != nil {
		t.Fatal(err)
	}

	return asm, parts.Items
}
