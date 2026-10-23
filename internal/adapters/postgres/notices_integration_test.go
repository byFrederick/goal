//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// stWork is where the notices tests create items.
const stWork = "work"

// noticeTimeout bounds waiting for a notification that should arrive at once.
const noticeTimeout = 10 * time.Second

// listening starts listener in the background and returns the notices of factory (and every
// Resync notice) as they arrive. The listener stops when the test ends.
func listening(t *testing.T, listener *Notices, factory domain.FactoryID) <-chan ports.ItemNotice {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan ports.ItemNotice, 100)
	done := make(chan error, 1)

	go func() {
		// Other tests share the channel (it is per database, not per schema): keep our own.
		done <- listener.Listen(ctx, func(n ports.ItemNotice) {
			if n.Resync || n.FactoryID == factory {
				got <- n
			}
		})
	}()

	t.Cleanup(func() {
		cancel()

		if err := <-done; err != nil {
			t.Errorf("Listen returned %v after its context ended; want nil", err)
		}
	})

	return got
}

func next(t *testing.T, notices <-chan ports.ItemNotice) ports.ItemNotice {
	t.Helper()

	select {
	case n := <-notices:
		return n
	case <-time.After(noticeTimeout):
		t.Fatal("no notice")

		return ports.ItemNotice{}
	}
}

func TestNotices_EveryCommittedEventWithTheItemsStateAfterIt(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	factory := saveFactory(t, s)
	notices := listening(t, NewNotices(pool.Config().ConnConfig, nil), factory)

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync once listening", n)
	}

	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	it, err := s.CreateItem(t.Context(), domain.NewItem{
		FactoryID: factory, Type: "task", Title: "x", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	moved := at.Add(time.Minute)

	_, err = s.Transition(t.Context(), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: "approve", State: domain.StateWaitingHuman}, At: moved,
		Events: []domain.Event{
			{FactoryID: factory, ItemID: it.ID, At: moved, Kind: domain.EventMoved, Station: stWork, Actor: domain.ActorEngine},
			{FactoryID: factory, ItemID: it.ID, At: moved, Kind: domain.EventWaitingHuman, Station: "approve", Actor: domain.ActorEngine},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []ports.ItemNotice{
		{FactoryID: factory, ItemID: it.ID, Number: 1, Kind: domain.EventCreated, Station: stWork, State: domain.StateQueued, At: at},
		{FactoryID: factory, ItemID: it.ID, Number: 1, Kind: domain.EventMoved, Station: stWork, State: domain.StateWaitingHuman, At: moved},
		{FactoryID: factory, ItemID: it.ID, Number: 1, Kind: domain.EventWaitingHuman, Station: "approve", State: domain.StateWaitingHuman, At: moved},
	}

	var last int64

	for i, w := range want {
		got := next(t, notices)
		if got.EventID <= last {
			t.Errorf("notice %d has event id %d after %d; want commit order", i, got.EventID, last)
		}

		last, got.EventID = got.EventID, 0
		if got != w {
			t.Errorf("notice %d = %+v; want %+v", i, got, w)
		}
	}
}

func TestNotices_ReconnectsAndResyncsAfterTheConnectionIsLost(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	factory := saveFactory(t, s)

	cfg := pool.Config().ConnConfig
	app := "goal_test_listener_" + randomHex(t)
	cfg.RuntimeParams["application_name"] = app

	notices := listening(t, NewNotices(cfg, nil), factory)

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync", n)
	}

	var killed int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE application_name = $1", app).Scan(&killed); err != nil || killed != 1 {
		t.Fatalf("terminate the listener's connection: %d killed, %v", killed, err)
	}

	if n := next(t, notices); !n.Resync {
		t.Fatalf("notice after the connection was lost = %+v; want Resync", n)
	}

	it, err := s.CreateItem(t.Context(), domain.NewItem{
		FactoryID: factory, Type: "task", Title: "x", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if n := next(t, notices); n.ItemID != it.ID || n.Kind != domain.EventCreated {
		t.Errorf("notice after reconnecting = %+v; want the item's created event", n)
	}
}
