//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
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
	notices := listening(t, NewNotices(s, nil), factory)

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

	app := "goal_test_listener_" + randomHex(t)
	listener := NewNotices(s, nil)
	listener.l.cfg.RuntimeParams["application_name"] = app

	notices := listening(t, listener, factory)

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

// A notification is a claim, not data (review 2.2 M4): any role that can connect may NOTIFY. Each
// one is read back through its organization's row-level security, and what reaches listeners
// comes from the rows: a forged item event with a real id carries the real event's fields, a
// factory event the andon pull's own; an id that does not exist, one of another organization and
// an event goald has no reader for are dropped.
func TestNotices_ForgedPayloadsAreReadBackFromTheRows(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	acme, labs := newOrgID(t, s), newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), acme)
	factory := saveFactory(ctx, t, s)
	notices := listening(t, NewNotices(s, nil), factory)

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync", n)
	}

	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	it, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: factory, Type: typeTask, Title: "x", Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	created := next(t, notices)

	pull, err := s.PullAndon(ctx, domain.AndonPull{FactoryID: factory, Kind: domain.AndonManual, Reason: "stop", PulledAt: at}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	if n := next(t, notices); n.Org == nil || n.Org.Name != factoryEvent {
		t.Fatalf("notice of the pull = %+v; want the factory event", n)
	}

	item := func(org domain.OrgID, id int64) string {
		return fmt.Sprintf(`{"org_id":%q,"id":%d,"factory_id":%q,"item_id":%q,"number":99,"kind":"done","station":"forged",`+
			`"state":"done","at":"2000-01-01T00:00:00Z"}`, org, id, factory, it.ID)
	}
	andon := func(kind string, id int64) string {
		return fmt.Sprintf(`{"org_id":%q,"event":"factory","factory_id":%q,"data":{"org_id":%q,"factory_id":%q,"kind":%q,`+
			`"andon_id":%d,"at":"2000-01-01T00:00:00Z","note":"forged"}}`, acme, factory, acme, factory, kind, id)
	}

	for _, n := range []struct{ channel, payload string }{
		{"goal_events", item(acme, created.EventID)},         // read back: the real created event
		{"goal_events", item(acme, created.EventID+1e6)},     // no such event: dropped
		{"goal_events", item(labs, created.EventID)},         // labs cannot see it: dropped
		{"goal_org_events", andon("andon_cleared", pull.ID)}, // the pull is not cleared: dropped
		{"goal_org_events", `{"org_id":"` + string(acme) + `","event":"credential","factory_id":"` + string(factory) + `","data":{"name":"repo"}}`},
		{"goal_org_events", andon("andon_pulled", pull.ID)}, // read back: the pull's own fields
	} {
		if _, err := pool.Exec(t.Context(), "SELECT pg_notify($1, $2)", n.channel, n.payload); err != nil {
			t.Fatal(err)
		}
	}

	if got := next(t, notices); got != created {
		t.Errorf("forged item event with a real id = %+v; want the real event %+v", got, created)
	}

	got := next(t, notices)
	if got.Org == nil || got.Org.Name != factoryEvent {
		t.Fatalf("notice after the forged ones = %+v; want the read-back factory event, the others dropped", got)
	}

	var data map[string]any
	if err := json.Unmarshal(got.Org.Data, &data); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{"org_id": string(acme), "factory_id": string(factory), "kind": "andon_pulled", "andon_id": float64(pull.ID), "at": "2026-10-23T09:00:00Z"}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("forged factory event data = %v; want the pull's own %v", data, want)
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
	notices := listening(t, NewNotices(s, nil), factory)

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
