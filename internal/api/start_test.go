package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

const acmeItem2Start = "/v1/orgs/acme/items/acme%232/start"

// Submitting with a start keeps its time, answers it as start.at and links the after item.
func TestItemStart_SubmitWithAStart(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	first := h.submit(t)

	var it gen.Item
	if code := h.call(t, http.MethodPost, acmeItems, `{"title":"Later","start":{"in":"2d","after":["`+acmeItemRef+`"]}}`, &it); code != http.StatusCreated {
		t.Fatalf("submit with a start = %d", code)
	}

	if it.Start == nil || !it.Start.At.Equal(t0.Add(48*time.Hour)) {
		t.Errorf("start = %+v; want at %v", it.Start, t0.Add(48*time.Hour))
	}

	var got gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+it.Id, "", &got); code != http.StatusOK ||
		got.Needs == nil || len(*got.Needs) != 1 || (*got.Needs)[0].Id != first.Id || (*got.Needs)[0].Kind != gen.Linked {
		t.Errorf("get = %d, needs %+v; want one linked need on %s", code, got.Needs, first.Id)
	}

	if first.Start != nil {
		t.Errorf("an item without a start answers start %+v; want none", first.Start)
	}
}

// A start that cannot be is a bad request, and nothing is filed.
func TestItemStart_SubmitRefusesABadStart(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for name, start := range map[string]string{
		"both at and in":       `{"at":"2026-11-05","in":"1d"}`,
		"a bad wait":           `{"in":"3 days"}`,
		"too far ahead":        `{"in":"60w"}`,
		"an unknown after":     `{"after":["acme#99"]}`,
		"a time it can't read": `{"at":"next tuesday"}`,
	} {
		var prob gen.Problem
		if code := h.call(t, http.MethodPost, acmeItems, `{"title":"Later","start":`+start+`}`, &prob); code != http.StatusBadRequest {
			t.Errorf("%s: submit = %d %+v; want 400", name, code, prob)
		}
	}

	if items, _ := h.store.Waiting(inOrg(t), domain.FactoryID(acmeID), stInbox, 10); len(items) != 0 {
		t.Errorf("filed %d items; want none", len(items))
	}
}

// PUT changes a start, DELETE clears it (Start now) and the inbox then releases the item; an item
// that started keeps its start.
func TestItemStart_PutAndDelete(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t)

	if code := h.call(t, http.MethodPost, acmeItems, `{"title":"Later","start":{"in":"1w"}}`, nil); code != http.StatusCreated {
		t.Fatalf("submit = %d", code)
	}

	var it gen.Item
	if code := h.call(t, http.MethodPut, acmeItem2Start, `{"at":"2026-11-01T09:00:00Z"}`, &it); code != http.StatusOK ||
		it.Start == nil || !it.Start.At.Equal(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("put = %d, start %+v; want 2026-11-01T09:00Z", code, it.Start)
	}

	var prob gen.Problem
	if code := h.call(t, http.MethodPut, acmeItem2Start, `{}`, &prob); code != http.StatusBadRequest {
		t.Errorf("put of an empty start = %d; want 400", code)
	}

	var now gen.Item
	if code := h.call(t, http.MethodDelete, acmeItem2Start, "", &now); code != http.StatusOK || now.Start != nil {
		t.Fatalf("delete = %d, start %+v; want none", code, now.Start)
	}

	if err := h.engine.Tick(inOrg(t)); err != nil { // inbox → approve
		t.Fatal(err)
	}

	if code := h.call(t, http.MethodDelete, acmeItem2Start, "", &prob); code != http.StatusConflict || deref(prob.Code) != "item-started" {
		t.Errorf("delete after it started = %d %+v; want 409 item-started", code, prob)
	}
}

// A done or cancelled item keeps its start.
func TestItemStart_RefusesAClosedItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t)

	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/acme%231/cancel", `{}`, nil); code != http.StatusOK {
		t.Fatalf("cancel = %d", code)
	}

	var prob gen.Problem

	if code := h.call(t, http.MethodPut, "/v1/orgs/acme/items/acme%231/start", `{"in":"1d"}`, &prob); code != http.StatusConflict || deref(prob.Code) != codeItemClosed {
		t.Errorf("put on a cancelled item = %d %+v; want 409 %s", code, prob, codeItemClosed)
	}
}

// A time without a zone is read in the person's time zone.
func TestItemStart_ReadsATimeInThePersonsZone(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata:", err)
	}

	h := keyed(t)

	p := domain.DefaultNotificationPreferences()
	p.TimeZone = "Europe/Berlin"

	if err := h.notifications.SetNotificationPreferences(t.Context(), ownerID, p); err != nil {
		t.Fatal(err)
	}

	res := h.as(t, ownerToken, http.MethodPost, acmeItems, `{"title":"At nine","start":{"at":"2026-11-05T09:00"}}`)
	it := decode[gen.Item](t, res, http.StatusCreated)

	if want := time.Date(2026, 11, 5, 9, 0, 0, 0, berlin); it.Start == nil || !it.Start.At.Equal(want) {
		t.Errorf("start = %+v; want %v", it.Start, want)
	}
}
