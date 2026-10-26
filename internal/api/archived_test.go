package api_test

import (
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

const archivedDetail = "This factory is archived; restore it first"

// archive pauses and archives factory f, as ArchiveFactory requires.
func (h *harness) archive(t *testing.T, f domain.FactoryID) {
	t.Helper()

	if _, err := h.defs.SetPaused(inOrg(t), f, true, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if _, err := h.defs.ArchiveFactory(inOrg(t), f, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

// wantArchived fails unless the answer is 409 factory-archived with its fixed detail.
func wantArchived(t *testing.T, what string, code int, p gen.Problem) {
	t.Helper()

	if code != http.StatusConflict || deref(p.Code) != "factory-archived" || deref(p.Detail) != archivedDetail {
		t.Errorf("%s on an archived factory = %d %+v; want 409 factory-archived %q", what, code, p, archivedDetail)
	}
}

// An archived factory is read-only: new items, parts and item writes answer 409 factory-archived,
// reads keep answering 200, and a principal who cannot see the factory still gets 404.
func TestArchivedFactory_RefusesItemWrites(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t) // acme#1, queued at the first station

	assembly := h.assemble(t, []domain.NewPart{
		{Key: "a", Title: "Schema", Lane: laneAPI},
		{Key: "b", Title: "Queries", Lane: laneAPI, Needs: []string{"a"}},
	})
	assemblyRef := "line#" + strconv.FormatInt(assembly.Number, 10)
	partsBefore := h.parts(t, assemblyRef)
	itemBefore := h.getItem(t, "acme#1")

	h.archive(t, acmeID)
	h.archive(t, lineID)

	t.Run("submit", func(t *testing.T) {
		t.Parallel()

		var p gen.Problem

		code := h.call(t, http.MethodPost, acmeItems, `{"title":"Add a version command","type":"feature"}`, &p)
		wantArchived(t, "submitItem", code, p)

		if n := h.items(t); n != 1 {
			t.Errorf("items after a refused submit = %d; want 1", n)
		}
	})

	t.Run("move", func(t *testing.T) {
		t.Parallel()

		var p gen.Problem

		code := h.call(t, http.MethodPost, acmeItem1+"/move", `{"to":"approve"}`, &p)
		wantArchived(t, "moveItem", code, p)
	})

	t.Run("decide", func(t *testing.T) {
		t.Parallel()

		var p gen.Problem

		code := h.call(t, http.MethodPost, acmeItem1+"/reject", `{"reason":"not this way"}`, &p)
		wantArchived(t, "rejectItem", code, p)

		if got := h.getItem(t, "acme#1"); got.Station != itemBefore.Station || got.State != itemBefore.State {
			t.Errorf("item after refused writes at %s %s; want %s %s", got.Station, got.State, itemBefore.Station, itemBefore.State)
		}
	})

	t.Run("add-part", func(t *testing.T) {
		t.Parallel()

		code, _, p := h.addPart(t, assemblyRef, `{"title":"Docs page","lane":"api","brief":"Document it"}`)
		wantArchived(t, "addItemPart", code, p)

		if got := h.parts(t, assemblyRef); !reflect.DeepEqual(got, partsBefore) {
			t.Errorf("parts after a refused add = %+v; want %+v", got, partsBefore)
		}
	})

	t.Run("reads", func(t *testing.T) {
		t.Parallel()

		for _, path := range []string{acmeItem1, "/v1/orgs/acme/factories/" + acmeID + "/board"} {
			if code := h.call(t, http.MethodGet, path, "", nil); code != http.StatusOK {
				t.Errorf("GET %s on an archived factory = %d; want 200", path, code)
			}
		}
	})
}

// A principal who cannot see an archived factory learns nothing: its writes answer the fixed 404.
func TestArchivedFactory_RefusesItemWrites_Hidden(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.archive(t, lineID)

	res := h.as(t, viewerToken, http.MethodPost, "/v1/orgs/acme/factories/line/items", `{"title":"t","type":"fix"}`)
	if p := problem(t, res); res.status != http.StatusNotFound || p.Title != "Factory not found" {
		t.Errorf("submit to a hidden archived factory = %d %+v; want the fixed 404 Factory not found", res.status, p)
	}
}
