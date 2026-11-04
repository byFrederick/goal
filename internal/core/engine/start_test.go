package engine_test

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

var ada = domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}

// submitWithStart files a person's item with start s.
func (h *harness) submitWithStart(t *testing.T, title string, s *domain.StartRequest) (domain.Item, error) {
	t.Helper()

	return h.e.Submit(inOrg(t), domain.FileRequest{FactoryID: factoryID, Origin: domain.OriginPerson, By: ada, Title: title, Start: s})
}

// A submitted start keeps its time and becomes one linked edge by the filer per open after item;
// a done one is met and left out.
func TestEngine_SubmitWithStartKeepsTheTimeAndLinksAfter(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))

	open := h.submit(t, domain.NewItem{Title: "open"})
	done := h.submit(t, domain.NewItem{Title: "done"})
	h.setState(t, done, domain.StateDone)

	it, err := h.submitWithStart(t, "later", &domain.StartRequest{
		In: "2d", After: []domain.After{{Ref: "acme#1", Item: &open}, {Ref: "acme#2", Item: ptr(h.item(t, done.ID))}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := h.item(t, it.ID)
	if want := t0.Add(48 * time.Hour); !got.StartAt.Equal(want) || got.Station != stInbox {
		t.Errorf("item = start %v at %s; want start %v at %s", got.StartAt, got.Station, want, stInbox)
	}

	edges, err := h.store.Edges(inOrg(t), it.ID)
	if err != nil || len(edges.Needs) != 1 || edges.Needs[0].Needs != open.ID || edges.Needs[0].Kind != domain.EdgeLinked || edges.Needs[0].By != ada.Ref() {
		t.Errorf("needs = %+v, %v; want one linked edge to %s by %s", edges.Needs, err, open.ID, ada.Ref())
	}
}

// A start that cannot be is refused with domain.ErrInvalid and nothing is filed.
func TestEngine_SubmitWithStartRefuses(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))

	for name, s := range map[string]domain.StartRequest{
		"a year and a day": {In: "366d"},
		"unknown after":    {After: []domain.After{{Ref: "acme#9"}}},
	} {
		if _, err := h.submitWithStart(t, name, &s); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: %v, want domain.ErrInvalid", name, err)
		}
	}

	if items, _ := h.store.Waiting(inOrg(t), factoryID, stInbox, 10); len(items) != 0 {
		t.Errorf("filed %d items; want none", len(items))
	}
}

// The inbox holds an item until its start passes and one held by after until its need is done,
// whatever comes behind them; both then leave on the next tick, and lead time starts there.
func TestEngine_ReleaseHoldsAFutureStartAndAnAfterAtTheFirstStation(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))

	need := h.submit(t, domain.NewItem{Title: "need"})
	h.setState(t, need, domain.StateBlocked) // stays at the inbox, open

	later, err := h.submitWithStart(t, "later", &domain.StartRequest{In: "1h"})
	if err != nil {
		t.Fatal(err)
	}

	after, err := h.submitWithStart(t, "after", &domain.StartRequest{After: []domain.After{{Ref: "acme#1", Item: &need}}})
	if err != nil {
		t.Fatal(err)
	}

	now := h.submit(t, domain.NewItem{Title: "now"})

	h.step(t)

	for _, it := range []domain.Item{later, after} {
		if got := h.item(t, it.ID); got.Station != stInbox || !got.StartedAt.IsZero() {
			t.Errorf("%s at %s, started %v; want held at %s", got.Title, got.Station, got.StartedAt, stInbox)
		}
	}

	if got := h.item(t, now.ID); got.Station == stInbox || !got.StartedAt.Equal(t0) {
		t.Errorf("now at %s, started %v; want released and started at %v", got.Station, got.StartedAt, t0)
	}

	h.clock.Advance(time.Hour)
	h.setState(t, h.item(t, need.ID), domain.StateDone)
	h.step(t)

	for _, it := range []domain.Item{later, after} {
		if got := h.item(t, it.ID); got.Station == stInbox || !got.StartedAt.Equal(t0.Add(time.Hour)) {
			t.Errorf("%s at %s, started %v; want released at %v", got.Title, got.Station, got.StartedAt, t0.Add(time.Hour))
		}
	}
}

// A person changes a start (a time, or only more after items, which keeps the time) and clears
// it (Start now), which releases the item on the next tick.
func TestEngine_SetStartChangesAndStartNowReleases(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))

	it, err := h.submitWithStart(t, "later", &domain.StartRequest{In: "1w"})
	if err != nil {
		t.Fatal(err)
	}

	changed, err := h.e.SetStart(inOrg(t), it.ID, domain.StartRequest{At: "2026-11-01T09:00:00Z"}, ada)
	if want := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC); err != nil || !changed.StartAt.Equal(want) {
		t.Fatalf("SetStart = %v, %v; want %v", changed.StartAt, err, want)
	}

	need := h.submit(t, domain.NewItem{Title: "need"})

	kept, err := h.e.SetStart(inOrg(t), it.ID, domain.StartRequest{After: []domain.After{{Ref: "acme#2", Item: &need}}}, ada)
	if err != nil || !kept.StartAt.Equal(changed.StartAt) {
		t.Fatalf("SetStart with only after = %v, %v; want the time kept", kept.StartAt, err)
	}

	if edges, _ := h.store.Edges(inOrg(t), it.ID); len(edges.Needs) != 1 {
		t.Errorf("needs = %+v; want the after edge", edges.Needs)
	}

	if _, err := h.e.SetStart(inOrg(t), it.ID, domain.StartRequest{After: []domain.After{{Ref: "acme#2", Item: &need}}}, ada); err != nil {
		t.Errorf("SetStart with the same after again = %v; want accepted", err)
	}

	cleared, err := h.e.StartNow(inOrg(t), it.ID, ada)
	if err != nil || !cleared.StartAt.IsZero() {
		t.Fatalf("StartNow = %v, %v; want no start", cleared.StartAt, err)
	}

	if got := h.eventsOf(it.ID, domain.EventStartChanged); len(got) != 4 {
		t.Errorf("start_changed events = %d; want one per change", len(got))
	}
}

// An item that left the first station, or is done or cancelled, keeps its start.
func TestEngine_SetStartRefusesAStartedOrClosedItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(domain.Station{ID: "sign-off", Kind: domain.KindHuman}), porttest.Succeed(`{}`))

	started := h.submit(t, domain.NewItem{Title: "started"})
	h.step(t)

	if _, err := h.e.StartNow(inOrg(t), started.ID, ada); !errors.Is(err, engine.ErrItemStarted) {
		t.Errorf("StartNow of a started item = %v; want ErrItemStarted", err)
	}

	cancelled := h.submit(t, domain.NewItem{Title: "cancelled"})
	h.setState(t, cancelled, domain.StateCancelled)

	if _, err := h.e.SetStart(inOrg(t), cancelled.ID, domain.StartRequest{In: "1d"}, ada); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("SetStart of a cancelled item = %v; want ErrItemClosed", err)
	}
}

func ptr[T any](v T) *T { return &v }

// setState puts it in state where it is, as if it got there.
func (h *harness) setState(t *testing.T, it domain.Item, state domain.ItemState) {
	t.Helper()

	cur := h.item(t, it.ID)
	if _, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: it.ID, From: cur.Position(), To: domain.Position{Station: cur.Station, State: state}, At: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}
