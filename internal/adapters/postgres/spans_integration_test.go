//go:build integration

package postgres

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Migration 00030 rebuilds spans from item events for the items that existed before it. A scripted
// history is written through the store on the current schema, where the trigger makes its spans;
// its factories, items and events are copied into a database at version 11, which then migrates:
// every item gets the same spans from the backfill.
func TestMigrate_BackfillMatchesTheTrigger(t *testing.T) {
	t.Parallel()

	now := newTestDB(t, OwnerRole)
	migrate(t, now.owner)
	triggered := New(openPool(t, now.app))
	items := writeHistory(t, triggered)

	before := newTestDB(t, OwnerRole)
	migrateTo(t, openPool(t, before.owner), 11)
	copyHistory(t, before, now)
	migrate(t, before.owner)
	backfilled := New(openPool(t, before.app))

	ctx := domain.WithOrg(t.Context(), DefaultOrgID)

	for i, id := range items {
		got, err := backfilled.ItemSpans(ctx, id)
		if err != nil {
			t.Fatal(err)
		}

		want, err := triggered.ItemSpans(ctx, id)
		if err != nil {
			t.Fatal(err)
		}

		if len(want) == 0 {
			t.Fatalf("item %d: the trigger made no spans", i+1)
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("item %d: backfilled spans =\n  %+v\nwant (trigger)\n  %+v", i+1, got, want)
		}
	}
}

// copyHistory copies the factories, items and events of from into to, a database before 00030,
// naming only columns that database has; event ids are kept, so their order is (COPY FROM writes
// identity columns as given).
func copyHistory(t *testing.T, to, from testDB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	src, err := pgx.Connect(ctx, from.admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close(context.Background()) }()

	dst, err := pgx.Connect(ctx, to.admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dst.Close(context.Background()) }()

	for _, table := range []struct{ name, columns string }{
		{"factories", "id, org_id, name, title, definition, definition_sha256, created_at, updated_at, last_item_number"},
		{"work_items", "id, org_id, factory_id, number, parent_id, type, lane, title, body, station, state, class_of_service, " +
			"priority, size, rework_count, cost_usd, outputs, created_at, updated_at, done_at"},
		{"item_events", "id, org_id, factory_id, item_id, run_id, at, kind, station, actor, data"},
	} {
		var rows bytes.Buffer
		if _, err := src.PgConn().CopyTo(ctx, &rows, "COPY (SELECT "+table.columns+" FROM "+table.name+") TO STDOUT"); err != nil {
			t.Fatalf("copy %s out: %v", table.name, err)
		}

		if _, err := dst.PgConn().CopyFrom(ctx, &rows, "COPY "+table.name+" ("+table.columns+") FROM STDIN"); err != nil {
			t.Fatalf("copy %s in: %v", table.name, err)
		}
	}
}

// Stations of the scripted line: inbox (queue), work (agent), review (agent with a gate back to
// work), approve (human), done.
const (
	stInbox   = "inbox"
	stReview  = "review"
	stApprove = "approve"
	stDone    = "done"
)

// scriptActor acts in the scripted history; agWorker works at its agent stations.
const (
	scriptActor = "scripted-agent"
	agWorker    = "worker"
)

// history writes items through the store with the events the engine writes for each step, at
// times one minute apart; each item in a factory of its own, so items never pull each other.
type history struct {
	t       *testing.T
	s       *Store
	factory domain.FactoryID
	now     time.Time
}

// ctx binds the test's context to the default organization, where the history is written.
func (h *history) ctx() context.Context { return domain.WithOrg(h.t.Context(), DefaultOrgID) }

// writeHistory writes the scripted items in the default organization and returns them in order.
func writeHistory(t *testing.T, s *Store) []domain.ItemID {
	t.Helper()

	h := &history{t: t, s: s, now: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}

	return []domain.ItemID{
		h.through(), h.reworked(), h.atHuman(), h.movedByPublish(), h.inProgress(), h.createdAtHuman(),
	}
}

// through goes down the whole line: release, claim, advance, gate approval, human approval, done.
func (h *history) through() domain.ItemID {
	it := h.create(stInbox, domain.StateQueued)
	it = h.arrive(it, stWork, nil, false)
	it, l := h.claim(it, stWork)
	it = h.arrive(it, stReview, &l, false)
	it, l = h.claim(it, stReview)
	it = h.arrive(it, stApprove, &l, false, h.event(it, domain.EventGateApproved, nil))
	it = h.arrive(it, stDone, nil, false, h.event(it, domain.EventApproved, nil))

	return it.ID
}

// reworked is sent back by the gate, blocked and retried, released at shutdown, loses its lease,
// is escalated past its rework allowance, sent back by a person and cancelled by the budget.
func (h *history) reworked() domain.ItemID {
	it := h.create(stInbox, domain.StateQueued)
	it = h.arrive(it, stWork, nil, false)
	it, l := h.claim(it, stWork)
	it = h.arrive(it, stReview, &l, false)
	it, l = h.claim(it, stReview)
	it = h.arrive(it, stWork, &l, true, h.event(it, domain.EventGateRejected, map[string]any{"reason": "no tests"}))

	it, l = h.claim(it, stWork)
	it = h.stay(it, domain.StateBlocked, &l, domain.EventBlocked)
	it = h.stay(it, domain.StateQueued, nil, domain.EventRetried)
	it, l = h.claim(it, stWork)
	it = h.stay(it, domain.StateQueued, &l, domain.EventReleased)

	it, _ = h.claim(it, stWork)
	h.now = h.now.Add(2 * time.Minute) // past the lease's expiry

	if _, err := h.s.RequeueExpiredLeases(h.ctx(), h.now, 10); err != nil {
		h.t.Fatal(err)
	}

	it, l = h.claim(h.item(it.ID), stWork)
	it = h.arrive(it, stReview, &l, false)
	it, l = h.claim(it, stReview)
	it = h.stay(it, domain.StateWaitingHuman, &l, domain.EventGateRejected, domain.EventWaitingHuman)
	it = h.arrive(it, stWork, nil, true, h.event(it, domain.EventRejected, map[string]any{"reason": "still no tests"}))
	it, l = h.claim(it, stWork)
	it = h.stay(it, domain.StateQueued, &l, domain.EventAgentCancelled)

	return it.ID
}

// atHuman waits for a person at the human station.
func (h *history) atHuman() domain.ItemID {
	it := h.create(stInbox, domain.StateQueued)
	it = h.arrive(it, stWork, nil, false)
	it, l := h.claim(it, stWork)
	it = h.arrive(it, stReview, &l, false)
	it, l = h.claim(it, stReview)

	return h.arrive(it, stApprove, &l, false, h.event(it, domain.EventGateApproved, nil)).ID
}

// movedByPublish was moved off a removed station by a publish: one moved event, no waiting_human
// event at the human station it landed on.
func (h *history) movedByPublish() domain.ItemID {
	it := h.create(stInbox, domain.StateQueued)
	at := h.tick()
	moved := h.event(it, domain.EventMoved, map[string]any{"from": stInbox, "to": stApprove, "reason": "definition v2"})
	moved.Station = stApprove

	return h.transition(ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stApprove, State: domain.StateWaitingHuman},
		At: at, Events: []domain.Event{moved},
	}).ID
}

// inProgress is being worked on.
func (h *history) inProgress() domain.ItemID {
	it := h.create(stInbox, domain.StateQueued)
	it = h.arrive(it, stWork, nil, false)
	it, _ = h.claim(it, stWork)

	return it.ID
}

// createdAtHuman entered a line whose first station is a person's.
func (h *history) createdAtHuman() domain.ItemID {
	return h.create(stApprove, domain.StateWaitingHuman).ID
}

func (h *history) tick() time.Time {
	h.now = h.now.Add(time.Minute)

	return h.now
}

func (h *history) create(station string, state domain.ItemState) domain.Item {
	h.factory = saveFactory(h.ctx(), h.t, h.s)

	it, err := h.s.CreateItem(h.ctx(), domain.NewItem{
		FactoryID: h.factory, Type: typeTask, Title: "scripted", Station: station, State: state,
		Class: domain.ClassStandard, CreatedAt: h.tick(),
	})
	if err != nil {
		h.t.Fatal(err)
	}

	return it
}

func (h *history) item(id domain.ItemID) domain.Item {
	it, err := h.s.Item(h.ctx(), id)
	if err != nil {
		h.t.Fatal(err)
	}

	return it
}

func (h *history) claim(it domain.Item, station string) (domain.Item, domain.Lease) {
	now := h.tick()

	got, l, err := h.s.ClaimNext(h.ctx(), ports.Claim{
		FactoryID: h.factory, Station: domain.Station{ID: station, Kind: domain.KindAgent, Agent: agWorker, Agents: 1},
		Now: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil || got.ID != it.ID {
		h.t.Fatalf("claim at %s = %s, %v; want item %s", station, got.ID, err, it.ID)
	}

	return got, l
}

func (h *history) event(it domain.Item, kind domain.EventKind, data map[string]any) domain.Event {
	return domain.Event{FactoryID: h.factory, ItemID: it.ID, Kind: kind, Station: it.Station, Actor: scriptActor, Data: data}
}

// arrive moves it to station as the engine does (engine.arrive): a moved event, then waiting_human
// at a human station or done at the done station, after the events that lead to the move.
func (h *history) arrive(it domain.Item, station string, l *domain.Lease, rework bool, lead ...domain.Event) domain.Item {
	at, state := h.tick(), domain.StateQueued

	events := append(slices.Clone(lead), h.event(it, domain.EventMoved, map[string]any{"from": it.Station, "to": station}))
	arrival := domain.Event{FactoryID: h.factory, ItemID: it.ID, Station: station, Actor: scriptActor}

	switch station {
	case stApprove:
		state, arrival.Kind = domain.StateWaitingHuman, domain.EventWaitingHuman
	case stDone:
		state, arrival.Kind = domain.StateDone, domain.EventDone
	}

	if arrival.Kind != "" {
		events = append(events, arrival)
	}

	return h.transition(ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: station, State: state}, Lease: l,
		Outcome: outcomeOf(l, rework), Rework: rework, At: at, DoneAt: at, Events: events,
	})
}

// stay changes it's state where it is, with events of the given kinds.
func (h *history) stay(it domain.Item, state domain.ItemState, l *domain.Lease, kinds ...domain.EventKind) domain.Item {
	at := h.tick()
	events := make([]domain.Event, 0, len(kinds))

	for _, k := range kinds {
		events = append(events, h.event(it, k, nil))
	}

	return h.transition(ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: it.Station, State: state}, Lease: l,
		Outcome: outcomeOf(l, false), At: at, Events: events,
	})
}

// outcomeOf is the outcome a scripted step gives the lease it releases, if any.
func outcomeOf(l *domain.Lease, rework bool) domain.LeaseOutcome {
	switch {
	case l == nil:
		return ""
	case rework:
		return domain.LeaseRejected
	default:
		return domain.LeaseAdvanced
	}
}

func (h *history) transition(tr ports.Transition) domain.Item {
	for i := range tr.Events {
		tr.Events[i].At = tr.At
	}

	it, err := h.s.Transition(h.ctx(), tr)
	if err != nil {
		h.t.Fatalf("transition to %s %s: %v", tr.To.Station, tr.To.State, err)
	}

	return it
}
