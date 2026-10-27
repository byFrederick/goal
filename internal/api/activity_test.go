package api_test

import (
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

const (
	acmeActivity = "/v1/orgs/acme/factories/acme/activity"
	// codeFactoryNotFound is the problem code of an unknown factory.
	codeFactoryNotFound = "factory-not-found"
)

// seedActivity creates an item of acme with a move, a per-turn tool call and its done event,
// returning the item and the move's data.
func (h *harness) seedActivity(t *testing.T) (domain.Item, map[string]any) {
	t.Helper()

	ctx := inOrg(t)

	it, err := h.store.CreateItem(ctx, domain.NewItem{
		FactoryID: acmeID, Type: typeFeature, Title: "Add a version command", Station: stBuild, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	moved := map[string]any{"from": "spec", "to": stBuild, "reason": "ready"}
	event := func(at time.Duration, kind domain.EventKind, actor string, data map[string]any) domain.Event {
		return domain.Event{FactoryID: acmeID, ItemID: it.ID, At: t0.Add(at), Kind: kind, Station: stBuild, Actor: actor, Data: data}
	}

	if err := h.store.AppendEvents(ctx,
		event(time.Minute, domain.EventMoved, "planner", moved),
		event(2*time.Minute, "tool_call", "builder", nil),
		event(3*time.Minute, domain.EventDone, domain.ActorEngine, nil),
	); err != nil {
		t.Fatal(err)
	}

	return it, moved
}

func TestAPI_FactoryActivity(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it, moved := h.seedActivity(t)

	var got gen.Activity
	if code := h.call(t, http.MethodGet, acmeActivity, "", &got); code != http.StatusOK {
		t.Fatalf("activity = %d; want 200", code)
	}

	if kinds := namesItem(t, got.Events, it); !reflect.DeepEqual(kinds, []string{"done", "moved", "created"}) {
		t.Fatalf("activity kinds = %v; want done, moved and created, newest first and no tool_call", kinds)
	}

	if e := got.Events[1]; !reflect.DeepEqual(e.Data, moved) || !e.At.Equal(t0.Add(time.Minute)) || e.Actor != "planner" || deref(e.Station) != stBuild {
		t.Errorf("moved event = %+v; want the stored event", e)
	}

	// The data is the same as the item's history returns.
	var page gen.EventPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(it.ID)+"/events", "", &page); code != http.StatusOK {
		t.Fatalf("item events = %d", code)
	}

	for _, e := range page.Events {
		if e.Id == got.Events[1].Id && !reflect.DeepEqual(e.Data, got.Events[1].Data) {
			t.Errorf("activity data %v; listItemEvents returns %v", got.Events[1].Data, e.Data)
		}
	}
}

// namesItem checks that every event names it by id, ref, number and title, and returns their kinds.
func namesItem(t *testing.T, events []gen.ActivityEvent, it domain.Item) []string {
	t.Helper()

	ref := "acme#" + strconv.FormatInt(it.Number, 10)
	kinds := make([]string, 0, len(events))

	for _, e := range events {
		kinds = append(kinds, e.Kind)

		if e.ItemId != string(it.ID) || e.Ref != ref || e.Number != it.Number || e.Title != it.Title {
			t.Errorf("%s event names item %s %s #%d %q; want %s %s #%d %q", e.Kind, e.ItemId, e.Ref, e.Number, e.Title, it.ID, ref, it.Number, it.Title)
		}
	}

	return kinds
}

func TestAPI_FactoryActivityLimit(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seedActivity(t)

	var got gen.Activity
	if code := h.call(t, http.MethodGet, acmeActivity+"?limit=1", "", &got); code != http.StatusOK ||
		len(got.Events) != 1 || got.Events[0].Kind != string(domain.EventDone) {
		t.Errorf("activity limit 1 = %d %+v; want the done event only", code, got.Events)
	}

	for _, limit := range []string{"0", "51"} {
		if code := h.call(t, http.MethodGet, acmeActivity+"?limit="+limit, "", nil); code != http.StatusBadRequest {
			t.Errorf("activity limit %s = %d; want 400", limit, code)
		}
	}
}

func TestAPI_FactoryActivityUnknownFactory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var p gen.Problem
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/nowhere/activity", "", &p); code != http.StatusNotFound ||
		deref(p.Code) != codeFactoryNotFound {
		t.Errorf("activity of an unknown factory = %d %+v; want 404 %s", code, p, codeFactoryNotFound)
	}
}
