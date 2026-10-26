package api_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const acme1 = "/v1/orgs/acme/items/acme%231"

// personNote appends the local user's note on it at `at` straight to the store.
func (h *harness) personNote(t *testing.T, it domain.Item, at time.Time) {
	t.Helper()

	err := h.store.AppendEvents(inOrg(t), domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: domain.EventNote, Station: it.Station,
		Actor: domain.LocalUser.Name, Data: map[string]any{"actor": domain.LocalUser.Ref(), "text": "see the docs"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// itemTouches reads acme#1's touches.
func (h *harness) itemTouches(t *testing.T) gen.ItemTouches {
	t.Helper()

	var it gen.Item
	if code := h.call(t, http.MethodGet, acme1, "", &it); code != http.StatusOK || it.Touches == nil {
		t.Fatalf("item = %d, touches %+v; want them", code, it.Touches)
	}

	return *it.Touches
}

// notedItem submits acme#1 and leaves a note on it through the API: one touch. It returns the
// path of that touch's reason.
func (h *harness) notedItem(t *testing.T) string {
	t.Helper()

	h.submit(t)

	if code := h.call(t, http.MethodPost, acmeNotes, noteBody(t, "use the staging bucket"), nil); code != http.StatusAccepted {
		t.Fatalf("note = %d", code)
	}

	return acme1 + "/touches/" + strconv.FormatInt(h.itemTouches(t).List[0].EventId, 10) + "/reason"
}

// An item lists its touches and its card counts them.
func TestAPI_ItemTouchesAndCards(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.notedItem(t)

	touches := h.itemTouches(t)
	if touches.Count != 1 || touches.Unplanned != 1 || touches.List[0].Kind != string(domain.EventNote) ||
		touches.List[0].Actor != (gen.Actor{Kind: gen.ActorKindUser, Id: "local", Name: domain.LocalUser.Name}) ||
		touches.List[0].Reason != nil || touches.List[0].Planned {
		t.Fatalf("touches %s; want the local user's unplanned note without a reason", asJSON(t, touches))
	}

	var page gen.ItemPage
	if code := h.call(t, http.MethodGet, acmeItems, "", &page); code != http.StatusOK || len(page.Items) != 1 || page.Items[0].TouchCount != 1 {
		t.Errorf("cards = %d %s; want the item's card with touch_count 1", code, asJSON(t, page.Items))
	}
}

// A person sets a touch's reason, audited as touch.labelled.
func TestAPI_TouchReasonIsSetAndAudited(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	reason := h.notedItem(t)

	if code := h.call(t, http.MethodPut, reason, `{"reason":"missing_knowledge"}`, nil); code != http.StatusNoContent {
		t.Fatalf("set reason = %d, want 204", code)
	}

	got := h.itemTouches(t).List[0]
	if got.Reason == nil || *got.Reason != gen.TouchReasonMissingKnowledge || deref(got.ReasonBy) != domain.LocalUser.Ref() {
		t.Errorf("touch after the reason %s; want missing_knowledge by user:local", asJSON(t, got))
	}

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditTouchLabelled, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].FactoryID != acmeID {
		t.Errorf("audit %+v; want one touch.labelled on acme's item", entries)
	}
}

// An event that is no touch of the item is 404 touch-not-found; a reason must be one of four.
func TestAPI_TouchReasonRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	reason := h.notedItem(t)

	for _, tt := range []struct {
		name, path, body string
		want             int
		code             string
	}{
		{name: "the created event", path: acme1 + "/touches/1/reason", body: `{"reason":"other"}`, want: http.StatusNotFound, code: "touch-not-found"},
		{name: "unknown reason", path: reason, body: `{"reason":"bad luck"}`, want: http.StatusBadRequest},
		{name: "unknown item", path: "/v1/orgs/acme/items/acme%2399/touches/1/reason", body: `{"reason":"other"}`, want: http.StatusNotFound, code: "item-not-found"},
	} {
		var p gen.Problem
		if code := h.call(t, http.MethodPut, tt.path, tt.body, &p); code != tt.want || deref(p.Code) != tt.code {
			t.Errorf("%s: %d %+v; want %d %q", tt.name, code, p, tt.want, tt.code)
		}
	}
}

// touchedLine finishes two line items in the week to Oct 23: one that ran on the current version
// with a person's note, one no agent ran; and one before it.
func (h *harness) touchedLine(t *testing.T) {
	t.Helper()

	var current gen.VersionDetail
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/versions/current", "", &current); code != http.StatusOK {
		t.Fatalf("current version = %d", code)
	}

	touched := h.seed(t, stBuild, domain.StateQueued, oct(20, 0))
	if _, err := h.store.StartRun(inOrg(t), ports.NewRun{
		FactoryID: lineID, DefinitionVersionID: domain.DefinitionVersionID(current.Id), ItemID: touched.ID, Station: stBuild,
		Agent: agBackend, Model: "m", StartedAt: oct(20, 1),
	}); err != nil {
		t.Fatal(err)
	}

	h.personNote(t, touched, oct(20, 2))
	h.move(t, touched, domain.Position{Station: stDone, State: domain.StateDone}, func(tr *ports.Transition) { tr.DoneAt = oct(20, 3) })
	h.finished(t, oct(21, 0), oct(21, 1), false)
	h.finished(t, oct(9, 0), oct(9, 1), false)
}

const lineTouches = "/v1/orgs/acme/factories/line/metrics/touches"

// The line's touch metrics cover the items finished in the range.
func TestAPI_TouchMetricsOfTheLine(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.touchedLine(t)

	var m gen.TouchMetrics
	if code := h.call(t, http.MethodGet, lineTouches+"?from=2026-10-17&to=2026-10-23", "", &m); code != http.StatusOK {
		t.Fatalf("touch metrics = %d", code)
	}

	s := m.Stats
	if s.Finished != 2 || s.Touches != 1 || s.Unplanned != 1 || s.PerItemMean != 0.5 || s.TouchlessRate != 0.5 || s.PerItemMedian != 0.5 {
		t.Errorf("stats %s; want 2 finished, 1 unplanned touch", asJSON(t, s))
	}

	if len(s.ByKind) != 1 || s.ByKind[0].Key != string(domain.EventNote) || len(s.ByReason) != 1 || s.ByReason[0].Reason != nil {
		t.Errorf("by kind %s, by reason %s; want one note without a reason", asJSON(t, s.ByKind), asJSON(t, s.ByReason))
	}
}

// The metrics split by the version each item last ran on, and by week.
func TestAPI_TouchMetricsByVersionAndWeek(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.touchedLine(t)

	var m gen.TouchMetrics
	h.call(t, http.MethodGet, lineTouches+"?from=2026-10-17&to=2026-10-23", "", &m)

	if v := m.Stats.ByVersion; len(v) != 2 || value(v[0].Number) != any(1) || v[0].Touches != 1 || v[1].VersionId != nil {
		t.Errorf("by version %s; want v1 with the touched item, then the item no agent ran", asJSON(t, v))
	}

	if len(m.Weekly) != 12 || m.Weekly[11].Finished != 2 || m.Weekly[11].To.Format(time.DateOnly) != "2026-10-23" {
		t.Errorf("weekly %s; want 12 weeks, the last ending on Oct 23 with both items", asJSON(t, m.Weekly))
	}
}

// ?version= narrows the metrics to the items that last ran on a published version or the current
// one; anything else is refused.
func TestAPI_TouchMetricsOfOneVersion(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.touchedLine(t)

	var v gen.TouchMetrics
	if code := h.call(t, http.MethodGet, lineTouches+"?from=2026-10-17&version=current", "", &v); code != http.StatusOK ||
		v.Stats.Finished != 1 || value(v.Version) != any(1) {
		t.Errorf("touch metrics of the current version = %d %s; want the one item that ran on v1", code, asJSON(t, v))
	}

	for query, want := range map[string]int{"version=7": http.StatusNotFound, "version=draft": http.StatusBadRequest, "weeks=27": http.StatusBadRequest} {
		if code := h.call(t, http.MethodGet, lineTouches+"?"+query, "", nil); code != want {
			t.Errorf("touch metrics ?%s = %d; want %d", query, code, want)
		}
	}
}

// The organization's metrics, the board's header and the overview count the same items.
func TestAPI_TouchesOfTheOrgBoardAndOverview(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.touchedLine(t)

	var org gen.OrgTouchMetrics
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/metrics/touches?from=2026-10-17", "", &org); code != http.StatusOK ||
		org.Total.Finished != 2 || len(org.Factories) != 2 || org.Factories[0].FactoryId != acmeID || org.Factories[1].Stats.Touches != 1 {
		t.Errorf("org touch metrics = %d %s; want the total, then acme and line in name order", code, asJSON(t, org))
	}

	var board gen.Board
	if h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/board", "", &board); board.Line.Touches.Finished != 2 || board.Line.Touches.PerItemMean != 0.5 {
		t.Errorf("board touches %+v; want the window's 2 finished items, 0.5 per item", board.Line.Touches)
	}

	var overview gen.FactoryOverviewList
	h.call(t, http.MethodGet, "/v1/orgs/acme/overview", "", &overview)

	for _, o := range overview.Factories {
		if o.Factory.Id == lineID && (o.Touches.Current.Finished != 2 || o.Touches.Current.TouchlessRate != 0.5 || o.Touches.Previous.Finished != 0) {
			t.Errorf("overview touches of line %+v; want this week's 2 finished items and none the week before", o.Touches)
		}
	}
}
