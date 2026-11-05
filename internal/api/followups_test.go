package api_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

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

// keepFollowUps keeps fs on item it, as an accepted run of writer at its station would.
func (h *harness) keepFollowUps(t *testing.T, it gen.Item, fs ...domain.FollowUp) {
	t.Helper()

	for i := range fs {
		fs[i].Station, fs[i].Agent, fs[i].State = it.Station, "writer", domain.FollowUpWaiting
	}

	at := domain.Position{Station: it.Station, State: domain.ItemState(it.State)}
	if _, err := h.store.Transition(inOrg(t), ports.Transition{ItemID: domain.ItemID(it.Id), From: at, To: at, At: t0, FollowUps: fs}); err != nil {
		t.Fatal(err)
	}
}

// whereItGoes is a follow-up's text, state and where it went or will go, on one line.
func whereItGoes(f gen.FollowUp) string {
	start := "-"

	if s := f.Start; s != nil {
		var after []string
		if s.After != nil {
			after = *s.After
		}

		start = fmt.Sprintf("in %s after %v in %s", deref(s.In), after, deref(s.Factory))
	}

	return fmt.Sprintf("%s %s for %s start %s filed %s", f.Text, f.State, deref(f.For), start, deref(f.FiledItem))
}

// An item's follow-ups answer oldest first, each saying where it went or will go: a note its
// item, a start its plan (another factory named) and the item it was filed as there, a triage
// one the triage item it joined. The item says how it was filed and by whom.
func TestGetItem_FollowUpsSayWhereTheyGo(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	src, target := h.submit(t), h.submit(t)

	h.keepFollowUps(t, src,
		domain.FollowUp{Text: "Mind the cache", Target: domain.FollowUpNote, For: &domain.ItemRef{Factory: acmeName, Number: target.Number}},
		domain.FollowUp{Text: "Check the line", Target: domain.FollowUpStart, Start: &domain.FollowUpPlan{In: "7d", Title: "Check the line", Factory: lineSlug}},
		domain.FollowUp{Text: "Retry after it", Target: domain.FollowUpStart, Start: &domain.FollowUpPlan{After: []string{target.Ref}, Title: "Retry"}},
		domain.FollowUp{Text: "Tidy the docs", Target: domain.FollowUpTriage},
	)

	acmeNotes := domain.NoteFactory{ID: acmeID, Name: acmeName}
	if _, err := h.store.DeliverNotes(inOrg(t), []domain.NoteFactory{acmeNotes, {ID: lineID, Name: lineSlug}}, t0, 10); err != nil {
		t.Fatal(err)
	}

	h.land(t, src, domain.Line{
		FactoryID: lineID, Name: lineSlug, Stations: []domain.Station{{ID: stInbox, Kind: domain.KindQueue}},
		Flow: domain.FlowPolicy{FollowUpsFrom: []domain.FactoryID{acmeID}},
	})

	got := h.getItem(t, src.Id)
	if got.FollowUps == nil || len(*got.FollowUps) != 4 {
		t.Fatalf("follow_ups = %+v; want 4", got.FollowUps)
	}

	lines := make([]string, 0, len(*got.FollowUps))
	for _, f := range *got.FollowUps {
		lines = append(lines, whereItGoes(f))
	}

	want := []string{
		"Mind the cache delivered for acme#2 start - filed ",
		"Check the line filed for  start in 7d after [] in line filed line#1",
		"Retry after it filed for  start in  after [acme#2] in  filed acme#3",
		"Tidy the docs filed for  start - filed acme#4",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("follow_ups =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	if got.Via == nil || *got.Via != gen.ItemViaWeb && *got.Via != gen.ItemViaCli || got.CreatedBy == nil || got.Untrusted == nil || *got.Untrusted {
		t.Errorf("item via %v by %+v untrusted %v; want filed by a person, trusted", got.Via, got.CreatedBy, got.Untrusted)
	}

	if triage := h.getItem(t, deref((*got.FollowUps)[3].FiledItemId)); triage.CreatedBy != nil {
		t.Errorf("triage item created_by = %+v; want none for the engine's filing", triage.CreatedBy)
	}
}

// The notes on an item answer oldest first with their source: another item's (its ref, factory,
// station and agent) or a trigger's check (its trigger and agent).
func TestGetItem_NotesFromOtherItems(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	src, target := h.submit(t), h.submit(t)

	h.keepFollowUps(t, src, domain.FollowUp{Text: "Mind the cache", Target: domain.FollowUpNote, For: &domain.ItemRef{Factory: acmeName, Number: target.Number}})

	if _, err := h.store.DeliverNotes(inOrg(t), []domain.NoteFactory{{ID: acmeID, Name: acmeName}}, t0, 10); err != nil {
		t.Fatal(err)
	}

	if err := h.store.AppendEvents(inOrg(t), domain.Event{
		ItemID: domain.ItemID(target.Id), Kind: domain.EventItemNote, At: t0.Add(time.Minute),
		Data: domain.CheckNoteData("new-incidents", "INC-1", "writer", "run-1", "Seen again"),
	}); err != nil {
		t.Fatal(err)
	}

	got := h.getItem(t, target.Id)
	if got.Notes == nil {
		t.Fatal("notes absent; want two")
	}

	var lines []string
	for _, n := range *got.Notes {
		lines = append(lines, fmt.Sprintf("%s from %s of %s at %s by %s trigger %s", n.Text, deref(n.FromItem), deref(n.FromFactoryId), deref(n.Station), n.Agent, deref(n.Trigger)))
	}

	want := []string{
		"Mind the cache from acme#1 of " + acmeID + " at inbox by writer trigger ",
		"Seen again from  of  at  by writer trigger new-incidents",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("notes =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	if s := h.getItem(t, src.Id); s.Notes == nil || len(*s.Notes) != 0 {
		t.Errorf("source notes = %+v; want an empty list", s.Notes)
	}
}

// A follow-up's item says where it came from: each follow-up filed as it, with its source item,
// the agent and station that wrote it and the item whose landing filed it, another factory's
// item included. A person's item has no came_from, and a follow-up's item none of whose rows
// resolves an empty list.
func TestGetItem_CameFromFollowUps(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	src := h.submit(t)

	h.keepFollowUps(t, src,
		domain.FollowUp{Text: "Look at the line", Target: domain.FollowUpStart, Start: &domain.FollowUpPlan{Title: "Look at the line", Factory: lineSlug}},
		domain.FollowUp{Text: "Retry after it", Target: domain.FollowUpStart, Start: &domain.FollowUpPlan{Title: "Retry"}},
	)
	h.land(t, src, domain.Line{
		FactoryID: lineID, Name: lineSlug, Stations: []domain.Station{{ID: stInbox, Kind: domain.KindQueue}},
		Flow: domain.FlowPolicy{FollowUpsFrom: []domain.FactoryID{acmeID}},
	})

	got := h.getItem(t, src.Id)
	if got.CameFrom != nil {
		t.Errorf("a person's item came_from = %+v; want none", got.CameFrom)
	}

	if got.FollowUps == nil || len(*got.FollowUps) != 2 {
		t.Fatalf("follow_ups = %+v; want 2", got.FollowUps)
	}

	for _, f := range *got.FollowUps {
		filed := h.getItem(t, deref(f.FiledItemId))

		want := []gen.CameFromFollowUp{{
			Id: f.Id, FromItem: src.Ref, FromItemId: src.Id, Agent: writerAgent, Station: src.Station, LandedItem: src.Ref, LandedItemId: src.Id,
		}}
		if c := filed.CameFrom; c == nil || c.Trigger != nil || c.FollowUps == nil || !slices.Equal(*c.FollowUps, want) {
			t.Errorf("%s came_from = %+v; want %+v", filed.Ref, c, want)
		}
	}

	orphan := h.seedFrom(t, domain.OriginFollowUp, "")
	if c := h.getItem(t, string(orphan.ID)).CameFrom; c == nil || c.FollowUps == nil || len(*c.FollowUps) != 0 {
		t.Errorf("a follow-up's item with no source came_from = %+v; want an empty list", c)
	}
}

// seedFrom creates an item of acme at the inbox with origin and trigger, as the door would.
func (h *harness) seedFrom(t *testing.T, origin domain.Origin, trigger string) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: acmeID, Type: typeFix, Title: "seeded", Station: stInbox, State: domain.StateQueued, Class: domain.ClassStandard,
		CreatedAt: t0, Origin: origin, Trigger: trigger,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// A trigger's item names its trigger and kind: check, with the key it was filed under, for a
// trigger with a check, schedule for another schedule, webhook otherwise, and no kind for a
// trigger the definition no longer has. A plan's part has no came_from.
func TestGetItem_CameFromTrigger(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.check(t, oct(22, 4), `{"file": [{"key": "INC-1", "title": "API down", "brief": "5xx"}]}`, "")

	checked := h.getItem(t, acmeName+"#1")
	if checked.Origin == nil || *checked.Origin != gen.ItemOriginTrigger {
		t.Fatalf("checked item = %+v; want the check's item", checked)
	}

	for _, c := range []struct {
		what, id, want string
	}{
		{"a check's item", checked.Id, incidentsName + " check INC-1"},
		{"a schedule's item", string(h.seedFrom(t, domain.OriginTrigger, "nightly-audit").ID), "nightly-audit schedule "},
		{"a webhook's item", string(h.seedFrom(t, domain.OriginTrigger, "flaky-test-on-main").ID), "flaky-test-on-main webhook "},
		{"a removed trigger's item", string(h.seedFrom(t, domain.OriginTrigger, "gone-trigger").ID), "gone-trigger  "},
		{"a plan's part", string(h.seedFrom(t, domain.OriginPlan, "").ID), "absent"},
	} {
		if got := cameFromTrigger(h.getItem(t, c.id).CameFrom); got != c.want {
			t.Errorf("%s came_from = %q; want %q", c.what, got, c.want)
		}
	}
}

// cameFromTrigger is c's trigger as its name, kind and key, "absent" when c is absent and "not a
// trigger" when c has follow-ups or no trigger.
func cameFromTrigger(c *gen.ItemCameFrom) string {
	switch {
	case c == nil:
		return "absent"
	case c.FollowUps != nil || c.Trigger == nil:
		return "not a trigger"
	}

	kind := ""
	if c.Trigger.Kind != nil {
		kind = string(*c.Trigger.Kind)
	}

	return c.Trigger.Name + " " + kind + " " + deref(c.Trigger.Key)
}
