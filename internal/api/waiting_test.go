package api_test

import (
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// A waiting item's card on the board and the list, and getItem, say what holds it: after the
// items it needs (planned or linked) or, held only by overlap needs, files with its own paths
// that overlap theirs. An item with no open need waits on nothing. The page's open needs are
// read once.
func TestAPI_WaitingSaysWhatHoldsAnItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, parts := h.assembledOf(t,
		domain.NewPart{Key: "contract", Title: "the contract", Paths: []string{"api/openapi.yaml", "web/**"}},
		domain.NewPart{Key: "client", Title: "the client", Paths: []string{"web/src/a.ts"}, Needs: []string{"contract"}},
		domain.NewPart{Key: "docs", Title: "the docs", Paths: []string{"docs/a.md", "api/*.yaml"}},
	)
	contract, client, docs := parts[0], parts[1], parts[2]
	h.edge(t, docs, contract, domain.EdgeOverlap)

	need := h.seed(t, stBuild, domain.StateQueued, t0)
	linked := h.seed(t, stBuild, domain.StateQueued, t0)
	h.edge(t, linked, need, domain.EdgeLinked)

	contractRef := "line#" + strconv.FormatInt(contract.Number, 10)
	needRef := "line#" + strconv.FormatInt(need.Number, 10)

	tests := []struct {
		name string
		it   domain.Item
		want *gen.ItemWaiting
	}{
		{"planned need", client, &gen.ItemWaiting{Kind: gen.WaitingAfter, Items: []string{contractRef}, Files: []string{}}},
		{"linked need", linked, &gen.ItemWaiting{Kind: gen.WaitingAfter, Items: []string{needRef}, Files: []string{}}},
		{"overlap need", docs, &gen.ItemWaiting{Kind: gen.WaitingFiles, Items: []string{contractRef}, Files: []string{"api/*.yaml"}}},
		{"no open need", need, nil},
	}

	var (
		list  gen.ItemPage
		board gen.Board
	)

	h.getReadingOnce(t, "OpenNeeds", "/v1/orgs/acme/factories/line/items", &list)
	h.getReadingOnce(t, "OpenNeeds", "/v1/orgs/acme/factories/line/board", &board)

	for from, cards := range map[string]map[int64]gen.ItemCard{"the list": byNumber(list.Items), "the board": cardsOf(board)} {
		for _, tt := range tests {
			c, ok := cards[tt.it.Number]
			if !ok || !reflect.DeepEqual(c.Waiting, tt.want) {
				t.Errorf("%s: %s card waiting = %+v (on it: %t); want %+v", from, tt.name, c.Waiting, ok, tt.want)
			}
		}
	}

	for _, tt := range tests {
		var it gen.Item
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(tt.it.ID), "", &it); code != http.StatusOK {
			t.Fatalf("GET %s = %d; want 200", tt.it.ID, code)
		}

		if !reflect.DeepEqual(it.Waiting, tt.want) {
			t.Errorf("getItem: %s waiting = %+v; want %+v", tt.name, it.Waiting, tt.want)
		}
	}
}

// edge records that it needs need, by kind, as the engine or a person would have.
func (h *harness) edge(t *testing.T, it, need domain.Item, kind domain.EdgeKind) {
	t.Helper()

	if _, err := h.store.AddEdge(inOrg(t), domain.NewEdge{
		Item: it.ID, Needs: need.ID, Kind: kind, By: domain.LocalUser, At: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}
