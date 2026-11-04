package api_test

import (
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// keepFollowUp keeps a follow-up for triage on item it, as a run's output would, and returns it.
func (h *harness) keepFollowUp(t *testing.T, it gen.Item, text string) domain.FollowUp {
	t.Helper()

	at := domain.Position{Station: it.Station, State: domain.ItemState(it.State)}
	if _, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: domain.ItemID(it.Id), From: at, To: at, At: t0,
		FollowUps: []domain.FollowUp{{Text: text, Station: it.Station, Agent: "writer", Target: domain.FollowUpTriage, State: domain.FollowUpWaiting}},
	}); err != nil {
		t.Fatal(err)
	}

	fs, err := h.store.FollowUps(inOrg(t), domain.ItemID(it.Id))
	if err != nil || len(fs) == 0 {
		t.Fatalf("FollowUps = %+v, %v", fs, err)
	}

	return fs[len(fs)-1]
}

func followUpPath(item string, id domain.FollowUpID, action string) string {
	return "/v1/orgs/acme/items/" + item + "/follow-ups/" + string(id) + "/" + action
}

// Drop answers the dropped follow-up, its reason naming the caller; File then answers 201 with
// an item by the caller with origin follow-up, a generation above its source's, and the
// follow-up is filed with it.
func TestFollowUps_FileAndDrop(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	src := h.submit(t)
	f := h.keepFollowUp(t, src, "Look at the logs")

	var dropped gen.FollowUp
	if code := h.call(t, http.MethodPost, followUpPath(src.Id, f.ID, "drop"), "", &dropped); code != http.StatusOK ||
		dropped.State != gen.FollowUpStateDropped || dropped.Reason == nil || *dropped.Reason == "" {
		t.Fatalf("drop = %d %+v; want 200 dropped with a reason", code, dropped)
	}

	var it gen.Item
	if code := h.call(t, http.MethodPost, followUpPath(src.Id, f.ID, "file"), `{"title":"Read the logs","start":{"in":"1d"}}`, &it); code != http.StatusCreated {
		t.Fatalf("file = %d; want 201", code)
	}

	if it.Title != "Read the logs" || it.Origin == nil || *it.Origin != gen.ItemOriginFollowUp || it.Start == nil {
		t.Errorf("filed item = %+v; want a follow-up item with a start", it)
	}

	h.expectFiledAs(t, src, it)
}

// expectFiledAs checks that src's first follow-up is filed as it, by the caller at generation 1.
func (h *harness) expectFiledAs(t *testing.T, src, it gen.Item) {
	t.Helper()

	got, err := h.store.Item(inOrg(t), domain.ItemID(it.Id))
	if err != nil || got.Generation != 1 || got.CreatedBy == "" {
		t.Errorf("stored item = %+v, %v; want generation 1 by the caller", got, err)
	}

	fs, _ := h.store.FollowUps(inOrg(t), domain.ItemID(src.Id))
	if fs[0].State != domain.FollowUpFiled || fs[0].FiledItem != domain.ItemID(it.Id) {
		t.Errorf("follow-up = %+v; want filed as %s", fs[0], it.Id)
	}
}

// Drop applies only to a waiting follow-up and File only to a dropped one or one in a closed
// triage item: anything else is 409 follow-up-state. A follow-up that is not the item's is 404
// follow-up-not-found, and a body the line does not take 400.
func TestFollowUps_FileAndDropRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	src := h.submit(t)
	other := h.submit(t)
	f := h.keepFollowUp(t, src, "Look at the logs")

	cases := []struct {
		name, item, action, body string
		status                   int
		code                     string
	}{
		{"file a waiting one", src.Id, "file", `{"title":"a"}`, http.StatusConflict, "follow-up-state"},
		{"drop another item's", other.Id, "drop", "", http.StatusNotFound, "follow-up-not-found"},
		{"file another item's", other.Id, "file", `{"title":"a"}`, http.StatusNotFound, "follow-up-not-found"},
	}

	for _, c := range cases {
		var p gen.Problem
		if code := h.call(t, http.MethodPost, followUpPath(c.item, f.ID, c.action), c.body, &p); code != c.status || p.Code == nil || *p.Code != c.code {
			t.Errorf("%s = %d %+v; want %d %s", c.name, code, p, c.status, c.code)
		}
	}

	if code := h.call(t, http.MethodPost, followUpPath(src.Id, f.ID, "drop"), "", nil); code != http.StatusOK {
		t.Fatalf("drop = %d", code)
	}

	var p gen.Problem
	if code := h.call(t, http.MethodPost, followUpPath(src.Id, f.ID, "drop"), "", &p); code != http.StatusConflict || p.Code == nil || *p.Code != "follow-up-state" {
		t.Errorf("drop twice = %d %+v; want 409 follow-up-state", code, p)
	}

	if code := h.call(t, http.MethodPost, followUpPath(src.Id, f.ID, "file"), `{"title":"a","type":"no-such-type"}`, nil); code != http.StatusBadRequest {
		t.Errorf("file with a type the line does not take = %d; want 400", code)
	}
}
