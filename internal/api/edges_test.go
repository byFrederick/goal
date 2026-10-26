package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The problem codes of an unknown item and a closed one.
const (
	codeItemNotFound = "item-not-found"
	codeItemClosed   = "item-closed"
)

// needsPath is the path of ref's needs.
func needsPath(ref string) string { return "/v1/orgs/acme/items/" + url.PathEscape(ref) + "/needs" }

// link asks that item need needs and returns the answer's status and body.
func (h *harness) link(t *testing.T, item, needs string) (int, gen.ItemLink, gen.Problem) {
	t.Helper()

	body, err := json.Marshal(gen.NewLink{Needs: needs})
	if err != nil {
		t.Fatal(err)
	}

	var raw json.RawMessage

	code := h.call(t, http.MethodPost, needsPath(item), string(body), &raw)

	var (
		l gen.ItemLink
		p gen.Problem
	)

	out := any(&p)
	if code == http.StatusCreated {
		out = &l
	}

	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("link %s → %s: %d %s: %v", item, needs, code, raw, err)
	}

	return code, l, p
}

func (h *harness) getItem(t *testing.T, ref string) gen.Item {
	t.Helper()

	var it gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+url.PathEscape(ref), "", &it); code != http.StatusOK {
		t.Fatalf("GET %s = %d; want 200", ref, code)
	}

	return it
}

func TestLinkItem_AddsALinkedEdgeAndGetItemListsIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	need, it := h.submit(t), h.submit(t) // acme#1, acme#2

	code, l, p := h.link(t, it.Ref, need.Id)
	if code != http.StatusCreated {
		t.Fatalf("link = %d %+v; want 201", code, p)
	}

	want := gen.ItemLink{Id: need.Id, Ref: need.Ref, Kind: gen.Linked, State: need.State, By: domain.LocalUser.Ref(), At: l.At}
	if l != want || l.At.IsZero() {
		t.Errorf("link = %+v; want %+v", l, want)
	}

	got := h.getItem(t, it.Ref)
	if got.Needs == nil || len(*got.Needs) != 1 || (*got.Needs)[0] != want {
		t.Errorf("needs of %s = %+v; want the linked edge", it.Ref, got.Needs)
	}
}

func TestLinkItem_RefusesACycle(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	a, b := h.submit(t), h.submit(t)

	if code, _, p := h.link(t, b.Ref, a.Ref); code != http.StatusCreated {
		t.Fatalf("link b → a = %d %+v", code, p)
	}

	other := h.seed(t, stBuild, domain.StateQueued, h.clock.Now()) // of the line factory
	done := h.finished(t, t0, t0.Add(1), false)

	tests := []struct {
		name, item, needs string
		want              int
		code              string
	}{
		{name: "cycle", item: a.Ref, needs: b.Ref, want: http.StatusConflict, code: "edge-cycle"},
		{name: "itself", item: a.Ref, needs: a.Id, want: http.StatusBadRequest, code: "edge-invalid"},
		{name: "another factory", item: a.Ref, needs: string(other.ID), want: http.StatusBadRequest, code: "edge-invalid"},
		{name: "unknown need", item: a.Ref, needs: "acme#99", want: http.StatusNotFound, code: codeItemNotFound},
		{name: "closed item", item: string(done.ID), needs: string(other.ID), want: http.StatusConflict, code: codeItemClosed},
	}

	for _, tt := range tests {
		if code, _, p := h.link(t, tt.item, tt.needs); code != tt.want || deref(p.Code) != tt.code {
			t.Errorf("%s: %d %+v; want %d %q", tt.name, code, p, tt.want, tt.code)
		}
	}
}

func TestLinkItem_RefusesADuplicate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	need, it := h.submit(t), h.submit(t)

	if code, _, p := h.link(t, it.Ref, need.Ref); code != http.StatusCreated {
		t.Fatalf("link = %d %+v", code, p)
	}

	if code, _, p := h.link(t, it.Ref, need.Ref); code != http.StatusConflict || deref(p.Code) != "edge-exists" {
		t.Errorf("second link = %d %+v; want 409 edge-exists", code, p)
	}
}

func TestUnlinkItem_RemovesTheEdge(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	need, it := h.submit(t), h.submit(t)

	if code, _, p := h.link(t, it.Ref, need.Ref); code != http.StatusCreated {
		t.Fatalf("link = %d %+v", code, p)
	}

	if code := h.call(t, http.MethodDelete, needsPath(it.Ref)+"/"+url.PathEscape(need.Ref), "", nil); code != http.StatusNoContent {
		t.Fatalf("unlink = %d; want 204", code)
	}

	if got := h.getItem(t, it.Ref); got.Needs == nil || len(*got.Needs) != 0 {
		t.Errorf("needs after unlink = %+v; want none", got.Needs)
	}

	var page gen.EventPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+url.PathEscape(it.Ref)+"/events", "", &page); code != http.StatusOK {
		t.Fatalf("events = %d", code)
	}

	if last := page.Events[len(page.Events)-1]; last.Kind != string(domain.EventEdgeRemoved) {
		t.Errorf("last event %+v; want edge_removed", last)
	}
}

func TestUnlinkItem_UnknownEdgeIs404(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	need, it := h.submit(t), h.submit(t)

	var p gen.Problem
	if code := h.call(t, http.MethodDelete, needsPath(it.Ref)+"/"+url.PathEscape(need.Ref), "", &p); code != http.StatusNotFound ||
		deref(p.Code) != "edge-not-found" {
		t.Errorf("unlink without an edge = %d %+v; want 404 edge-not-found", code, p)
	}

	if code := h.call(t, http.MethodDelete, needsPath(it.Ref)+"/"+url.PathEscape("acme#99"), "", &p); code != http.StatusNotFound ||
		deref(p.Code) != codeItemNotFound {
		t.Errorf("unlink of an unknown need = %d %+v; want 404 item-not-found", code, p)
	}
}

func TestGetItem_ListsNeedsAndDependents(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	a, b, c := h.submit(t), h.submit(t), h.submit(t)

	for _, l := range [][2]gen.Item{{b, a}, {c, b}} {
		if code, _, p := h.link(t, l[0].Ref, l[1].Ref); code != http.StatusCreated {
			t.Fatalf("link %s → %s = %d %+v", l[0].Ref, l[1].Ref, code, p)
		}
	}

	tests := []struct {
		item              gen.Item
		needs, dependents string
	}{
		{item: a, needs: "[]", dependents: `["` + b.Ref + `"]`},
		{item: b, needs: `["` + a.Ref + `"]`, dependents: `["` + c.Ref + `"]`},
		{item: c, needs: `["` + b.Ref + `"]`, dependents: "[]"},
	}

	for _, tt := range tests {
		got := h.getItem(t, tt.item.Ref)
		if asJSON(t, linkRefs(got.Needs)) != tt.needs || asJSON(t, linkRefs(got.Dependents)) != tt.dependents {
			t.Errorf("%s: needs %v dependents %v; want %s and %s", tt.item.Ref, linkRefs(got.Needs), linkRefs(got.Dependents), tt.needs, tt.dependents)
		}
	}

	if d := h.getItem(t, b.Ref).Dependents; d == nil || len(*d) != 1 || (*d)[0].Id != c.Id || (*d)[0].State != c.State {
		t.Errorf("dependents of %s = %+v; want %s with its state", b.Ref, d, c.Ref)
	}

	// Only getItem fills them: other answers with an Item, as submitItem's, leave them out.
	if a.Needs != nil || a.Dependents != nil {
		t.Errorf("submitted %s has needs %v and dependents %v; want neither", a.Ref, a.Needs, a.Dependents)
	}
}

// linkRefs are the refs of links, nil without links.
func linkRefs(links *[]gen.ItemLink) []string {
	if links == nil {
		return nil
	}

	out := []string{}
	for _, l := range *links {
		out = append(out, l.Ref)
	}

	return out
}
