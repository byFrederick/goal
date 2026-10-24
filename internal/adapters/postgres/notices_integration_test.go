//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// stWork is where the notices tests create items.
const stWork = "work"

// typeTask is the work type of the items the tests create.
const typeTask = "task"

// factoryEvent is the stream event of a change of a factory's line (migration 00032).
const factoryEvent = "factory"

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
	org := newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), org)
	factory := saveFactory(ctx, t, s)
	notices := listening(t, NewNotices(pool.Config().ConnConfig, nil), factory)

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync once listening", n)
	}

	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	it, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: factory, Type: typeTask, Title: "x", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	moved := at.Add(time.Minute)

	_, err = s.Transition(ctx, ports.Transition{
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
		{OrgID: org, FactoryID: factory, ItemID: it.ID, Number: 1, Kind: domain.EventCreated, Station: stWork, State: domain.StateQueued, At: at},
		{OrgID: org, FactoryID: factory, ItemID: it.ID, Number: 1, Kind: domain.EventMoved, Station: stWork, State: domain.StateWaitingHuman, At: moved},
		{OrgID: org, FactoryID: factory, ItemID: it.ID, Number: 1, Kind: domain.EventWaitingHuman, Station: "approve", State: domain.StateWaitingHuman, At: moved},
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

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	pool := openPool(t, db.app)
	s := New(pool)
	org := newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), org)
	factory := saveFactory(ctx, t, s)

	cfg := pool.Config().ConnConfig
	app := "goal_test_listener_" + randomHex(t)
	cfg.RuntimeParams["application_name"] = app

	notices := listening(t, NewNotices(cfg, nil), factory)

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync", n)
	}

	// Only the bootstrap superuser may end a session that logged in as it.
	var killed int
	if err := openPool(t, db.admin).QueryRow(t.Context(),
		"SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE application_name = $1", app).Scan(&killed); err != nil || killed != 1 {
		t.Fatalf("terminate the listener's connection: %d killed, %v", killed, err)
	}

	if n := next(t, notices); !n.Resync {
		t.Fatalf("notice after the connection was lost = %+v; want Resync", n)
	}

	it, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: factory, Type: typeTask, Title: "x", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if n := next(t, notices); n.ItemID != it.ID || n.Kind != domain.EventCreated {
		t.Errorf("notice after reconnecting = %+v; want the item's created event", n)
	}
}

func TestNotices_OrgEventsArriveAsOrgNotices(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	org := newOrgID(t, s)
	factory := saveFactory(domain.WithOrg(t.Context(), org), t, s)
	notices := listening(t, NewNotices(pool.Config().ConnConfig, nil), factory)

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync", n)
	}

	for _, payload := range []string{
		`{"org_id":"` + string(org) + `","event":"bad name\ndata: x","factory_id":"` + string(factory) + `","data":{}}`,
		`{"org_id":"` + string(org) + `","event":"credential","factory_id":"` + string(factory) + `","data":{"name":"repo"}}`,
	} {
		if _, err := pool.Exec(t.Context(), "SELECT pg_notify('goal_org_events', $1)", payload); err != nil {
			t.Fatal(err)
		}
	}

	n := next(t, notices)
	if n.OrgID != org || n.FactoryID != factory || n.Org == nil || n.Org.Name != "credential" ||
		n.Org.FactoryID != factory || string(n.Org.Data) != `{"name":"repo"}` {
		t.Errorf("notice = %+v (org event %+v); want the credential event, the malformed one skipped", n, n.Org)
	}
}

// A pull and a clear of the andon cord are the organization's factory events, each with its kind
// and time (migration 00032), sent on commit.
func TestNotices_AndonPullAndClearAreFactoryEvents(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	org := newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), org)
	factory := saveFactory(ctx, t, s)
	notices := listening(t, NewNotices(pool.Config().ConnConfig, nil), factory)

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync", n)
	}

	pulledAt := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	clearedAt := pulledAt.Add(time.Hour)

	pull, err := s.PullAndon(ctx, domain.AndonPull{FactoryID: factory, Kind: domain.AndonManual, Reason: "stop", PulledAt: pulledAt}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.ClearAndon(ctx, factory, domain.LocalUser, "", clearedAt); err != nil {
		t.Fatal(err)
	}

	for _, want := range []factoryPayload{
		{OrgID: string(org), FactoryID: string(factory), Kind: "andon_pulled", AndonID: pull.ID, At: pulledAt},
		{OrgID: string(org), FactoryID: string(factory), Kind: "andon_cleared", AndonID: pull.ID, At: clearedAt},
	} {
		n := next(t, notices)
		if n.OrgID != org || n.Org == nil || n.Org.Name != factoryEvent || n.Org.FactoryID != factory {
			t.Fatalf("notice = %+v (org event %+v); want the factory event %s", n, n.Org, want.Kind)
		}

		var got factoryPayload
		if err := json.Unmarshal(n.Org.Data, &got); err != nil {
			t.Fatal(err)
		}

		if got.At = got.At.UTC(); got != want {
			t.Errorf("factory event data = %+v; want %+v", got, want)
		}
	}
}

// factoryPayload is the data of a factory event.
type factoryPayload struct {
	OrgID     string    `json:"org_id"`
	FactoryID string    `json:"factory_id"`
	Kind      string    `json:"kind"`
	AndonID   int64     `json:"andon_id"`
	At        time.Time `json:"at"`
}
