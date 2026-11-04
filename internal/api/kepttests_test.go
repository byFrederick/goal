package api_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// lineTests are the line factory's kept tests; line has one agent station, build.
const lineTests = "/v1/orgs/acme/factories/line/jonah/tests"

// builtItem seeds an item of line with these events at build, and a finished first run of a
// claim there (its branch head recorded) when run is set.
func (h *harness) builtItem(t *testing.T, run bool, kinds ...domain.EventKind) domain.Item {
	t.Helper()

	ctx := inOrg(t)
	it := h.seed(t, stBuild, domain.StateQueued, t0)

	for _, k := range kinds {
		e := domain.Event{FactoryID: lineID, ItemID: it.ID, At: h.clock.Now(), Kind: k, Station: stBuild}
		if k == domain.EventMoved {
			e.Data = map[string]any{"from": stBuild, "to": stMerge}
		}

		if err := h.store.AppendEvents(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	if !run {
		return it
	}

	id, err := h.store.StartRun(ctx, ports.NewRun{
		FactoryID: lineID, ItemID: it.ID, Station: stBuild, Agent: agBackend, Model: runModel, StartedAt: t0, Kind: domain.RunKindStation,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.RecordRunInput(ctx, id, domain.RunInput{BranchHead: strings.Repeat("c", 40), BaseHead: strings.Repeat("b", 40)}); err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	return it
}

// keepBody is a keep request of item number n at station.
func keepBody(n int64, station string) string {
	return `{"item":` + strconv.FormatInt(n, 10) + `,"station":"` + station + `"}`
}

func (h *harness) keptTests(t *testing.T, path string) []gen.KeptTest {
	t.Helper()

	var list gen.KeptTestList
	if code := h.call(t, http.MethodGet, path, "", &list); code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, code)
	}

	return list.Tests
}

func TestJonah_KeptTests(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	passed := h.builtItem(t, true, domain.EventClaimed, domain.EventMoved)

	var kept gen.KeptTest
	if code := h.call(t, http.MethodPost, lineTests, keepBody(passed.Number, stBuild), &kept); code != http.StatusCreated {
		t.Fatalf("keep = %d, want 201", code)
	}

	if kept.Id == "" || kept.Item.Id != string(passed.ID) || kept.Item.Number != passed.Number || kept.Item.Title != passed.Title ||
		kept.Station != stBuild || kept.KeptBy == "" || !kept.KeptAt.Equal(h.clock.Now()) {
		t.Errorf("kept = %+v, want item %d at %s, kept now", kept, passed.Number, stBuild)
	}

	h.clock.Advance(time.Minute)

	second := h.builtItem(t, true, domain.EventClaimed, domain.EventMoved)
	if code := h.call(t, http.MethodPost, lineTests, keepBody(second.Number, stBuild), nil); code != http.StatusCreated {
		t.Fatalf("keep a second item = %d, want 201", code)
	}

	if got := h.keptTests(t, lineTests); len(got) != 2 || got[0].Item.Number != second.Number || got[1].Id != kept.Id {
		t.Errorf("kept tests = %+v, want the second item's first, then %s", got, kept.Id)
	}

	code, p := problemOfCall(t, h, http.MethodPost, lineTests, keepBody(passed.Number, stBuild))
	wantProblem(t, "keep the item at build again", code, p, http.StatusConflict, "kept-test-exists")

	h.wantKeepRefusals(t)

	code, p = problemOfCall(t, h, http.MethodDelete, acmeJonah+"/tests/"+kept.Id, "")
	wantProblem(t, "remove line's test through acme", code, p, http.StatusNotFound, "kept-test-not-found")

	code, p = problemOfCall(t, h, http.MethodDelete, lineTests+"/019a0000-0000-7000-8000-00000000dead", "")
	wantProblem(t, "remove an unknown test", code, p, http.StatusNotFound, "kept-test-not-found")

	if code := h.call(t, http.MethodDelete, lineTests+"/"+kept.Id, "", nil); code != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", code)
	}

	if got := h.keptTests(t, lineTests); len(got) != 1 || got[0].Item.Number != second.Number {
		t.Errorf("kept tests after removing = %+v, want the second item's only", got)
	}
}

// wantKeepRefusals checks the keeps that are refused, none of them stored.
func (h *harness) wantKeepRefusals(t *testing.T) {
	t.Helper()

	passed := h.builtItem(t, true, domain.EventClaimed, domain.EventMoved)
	rejected := h.builtItem(t, true, domain.EventClaimed, domain.EventGateRejected, domain.EventMoved)
	twice := h.builtItem(t, true, domain.EventClaimed, domain.EventMoved, domain.EventClaimed, domain.EventMoved)
	notMoved := h.builtItem(t, true, domain.EventClaimed)
	noRun := h.builtItem(t, false, domain.EventClaimed, domain.EventMoved)

	for _, c := range []struct {
		what, body string
		status     int
		code       string
	}{
		{"an unknown item", keepBody(9999, stBuild), http.StatusNotFound, "item-not-found"},
		{"a human station", keepBody(passed.Number, stMerge), http.StatusBadRequest, ""},
		{"a station not in the line", keepBody(passed.Number, "nowhere"), http.StatusBadRequest, ""},
		{"an item a gate rejected", keepBody(rejected.Number, stBuild), http.StatusConflict, "not-passed-first-time"},
		{"an item claimed there twice", keepBody(twice.Number, stBuild), http.StatusConflict, "not-passed-first-time"},
		{"an item not moved on", keepBody(notMoved.Number, stBuild), http.StatusConflict, "not-passed-first-time"},
		{"an item with no finished first run", keepBody(noRun.Number, stBuild), http.StatusConflict, "not-passed-first-time"},
	} {
		code, p := problemOfCall(t, h, http.MethodPost, lineTests, c.body)
		wantProblem(t, "keep "+c.what, code, p, c.status, c.code)
	}

	if got := h.keptTests(t, lineTests); len(got) != 2 {
		t.Errorf("after refusals %d kept tests, want the 2 kept", len(got))
	}
}
