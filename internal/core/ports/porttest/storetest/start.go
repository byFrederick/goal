package storetest

import (
	"errors"
	"reflect"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Releasable lists, in pull order, only the waiting items with no open need and no start after
// now, filtered before the limit; Waiting still lists them all.
func testReleasableSkipsOpenNeedsAndFutureStarts(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	need := f.create(t, domain.NewItem{Title: "need", Station: stNext})
	f.create(t, domain.NewItem{Title: "later", Station: stInbox, Class: domain.ClassExpedite, StartAt: t1.Add(time.Second)})
	f.create(t, domain.NewItem{Title: "after", Station: stInbox, Priority: 5, After: []domain.ItemID{need.ID}})
	due := f.create(t, domain.NewItem{Title: "due", Station: stInbox, StartAt: t1, CreatedAt: t0.Add(time.Second)})
	now := f.create(t, domain.NewItem{Title: "now", Station: stInbox})

	got, err := f.Store.Releasable(ctx, f.factory, stInbox, t1, 1)
	if err != nil || len(got) != 1 || got[0].ID != now.ID {
		t.Errorf("Releasable limit 1 = %v, %v; want [now]: held items do not take the limit", titlesOf(got), err)
	}

	got, err = f.Store.Releasable(ctx, f.factory, stInbox, t1, 10)
	if want := []string{now.Title, due.Title}; err != nil || !reflect.DeepEqual(titlesOf(got), want) {
		t.Errorf("Releasable = %v, %v; want %v", titlesOf(got), err, want)
	}

	if w, err := f.Store.Waiting(ctx, f.factory, stInbox, 10); err != nil || len(w) != 4 {
		t.Errorf("Waiting = %v, %v; want all four", titlesOf(w), err)
	}

	if got, err := f.Store.Releasable(ctx, f.factory, stInbox, t1.Add(time.Second), 10); err != nil || len(got) != 3 {
		t.Errorf("Releasable once the start passed = %v, %v; want later too", titlesOf(got), err)
	}
}

// ClaimNext skips an item whose start is after the claim's now, and takes it once it passed.
func testClaimSkipsAFutureStart(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	later := f.create(t, domain.NewItem{Title: "later", Class: domain.ClassExpedite, StartAt: t1.Add(time.Minute)})
	now := f.create(t, domain.NewItem{Title: "now"})

	if got, _ := f.claim(t, work, 0, t1); got.ID != now.ID {
		t.Fatalf("claimed %q; want %q, the first item whose start passed", got.Title, now.Title)
	}

	if _, _, err := f.Store.ClaimNext(ctx, ports.Claim{FactoryID: f.factory, Station: work, Slot: 1, Now: t1, ExpiresAt: t1.Add(ttl)}); !errors.Is(err, ports.ErrNoWork) {
		t.Fatalf("ClaimNext before the start = %v; want ErrNoWork", err)
	}

	if got, _ := f.claim(t, work, 1, t1.Add(time.Minute)); got.ID != later.ID {
		t.Fatalf("claimed %q at its start; want %q", got.Title, later.Title)
	}
}

// CreateItem keeps the start, the started time and a linked edge by the filer to each after item.
func testCreateItemWithAStart(f *fixture) {
	t := f.t

	need := f.create(t, domain.NewItem{Title: "need"})
	by := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}
	it := f.create(t, domain.NewItem{Title: "later", Station: stInbox, StartAt: t1, After: []domain.ItemID{need.ID}, CreatedBy: by})

	if got := f.item(t, it.ID); !got.StartAt.Equal(t1) || !got.StartedAt.IsZero() {
		t.Errorf("start = %v, started %v; want %v, not started", got.StartAt, got.StartedAt, t1)
	}

	edges, err := f.Store.Edges(f.ctx(t), it.ID)
	if err != nil || len(edges.Needs) != 1 || edges.Needs[0].Needs != need.ID || edges.Needs[0].Kind != domain.EdgeLinked || edges.Needs[0].By != by.Ref() {
		t.Errorf("edges = %+v, %v; want one linked edge to need by %s", edges, err, by.Ref())
	}

	placed := f.create(t, domain.NewItem{Title: "placed", StartedAt: t0})
	if got := f.item(t, placed.ID); !got.StartedAt.Equal(t0) {
		t.Errorf("started = %v; want %v", got.StartedAt, t0)
	}
}

// SetStart changes and clears an item's start with a start_changed event, guarded by its
// position.
func testSetStart(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	by := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}

	it := f.create(t, domain.NewItem{Title: "later", Station: stInbox, StartAt: t1})

	changed, err := f.Store.SetStart(ctx, it.ID, it.Position(), t1.Add(time.Hour), by, t1)
	if err != nil || !changed.StartAt.Equal(t1.Add(time.Hour)) || !changed.UpdatedAt.Equal(t1) {
		t.Fatalf("SetStart = %+v, %v; want the start an hour later", changed, err)
	}

	cleared, err := f.Store.SetStart(ctx, it.ID, it.Position(), time.Time{}, by, t1)
	if err != nil || !cleared.StartAt.IsZero() || !f.item(t, it.ID).StartAt.IsZero() {
		t.Fatalf("SetStart zero = %+v, %v; want no start", cleared, err)
	}

	events := f.Events(t, it.ID)
	if n := len(events); n < 2 || events[n-2].Kind != domain.EventStartChanged || events[n-2].Data["start_at"] != "2026-10-23T11:00:00Z" ||
		events[n-1].Kind != domain.EventStartChanged || events[n-1].Data["start_at"] != nil || events[n-1].Data["actor"] != by.Ref() {
		t.Errorf("events = %+v; want two start_changed, the last without start_at", events)
	}

	if _, err := f.Store.SetStart(ctx, it.ID, domain.Position{Station: stWork, State: domain.StateQueued}, t1, by, t1); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("SetStart from elsewhere = %v; want ErrConflict", err)
	}

	if _, err := f.Store.SetStart(ctx, missingItem, it.Position(), t1, by, t1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("SetStart of a missing item = %v; want ErrNotFound", err)
	}
}
