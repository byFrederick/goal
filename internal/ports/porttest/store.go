package porttest

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/factory/internal/domain"
	"github.com/byFrederick/factory/internal/ports"
)

var _ ports.Store = (*Store)(nil)

// Store keeps items, leases, runs and events in memory behind one mutex, so every method is
// atomic like a PostgreSQL transaction.
type Store struct {
	mu      sync.Mutex
	items   map[domain.ItemID]*domain.Item
	numbers map[domain.FactoryID]int64
	leases  map[domain.ItemID]domain.Lease
	runs    map[domain.RunID]*domain.Run
	events  []domain.Event
	ids     int
	changes changes
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{
		items:   map[domain.ItemID]*domain.Item{},
		numbers: map[domain.FactoryID]int64{},
		leases:  map[domain.ItemID]domain.Lease{},
		runs:    map[domain.RunID]*domain.Run{},
	}
}

// Await blocks until cond holds or ctx ends; cond is re-checked after every change.
// cond may call the store's methods.
func (s *Store) Await(ctx context.Context, cond func() bool) error {
	return s.changes.await(ctx, cond)
}

// Events returns an item's events in the order they were appended.
func (s *Store) Events(id domain.ItemID) []domain.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.Event

	for _, e := range s.events {
		if e.ItemID == id {
			out = append(out, e)
		}
	}

	return out
}

// Lease returns the lease held on an item, if any.
func (s *Store) Lease(id domain.ItemID) (domain.Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.leases[id]

	return l, ok
}

// Runs returns an item's runs, oldest first.
func (s *Store) Runs(id domain.ItemID) []domain.Run {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.Run

	for _, r := range s.runs {
		if r.ItemID == id {
			out = append(out, *r)
		}
	}

	slices.SortFunc(out, func(a, b domain.Run) int { return cmp.Compare(a.ID, b.ID) })

	return out
}

// CreateItem implements ports.Store.
func (s *Store) CreateItem(_ context.Context, n domain.NewItem) (domain.Item, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	s.numbers[n.FactoryID]++
	it := &domain.Item{
		ID: domain.ItemID(s.newID()), FactoryID: n.FactoryID, Number: s.numbers[n.FactoryID],
		ParentID: n.ParentID, Type: n.Type, Lane: n.Lane, Title: n.Title, Body: n.Body,
		Station: n.Station, State: n.State, Class: n.Class, Priority: n.Priority,
		Outputs: map[string]json.RawMessage{}, CreatedAt: n.CreatedAt, UpdatedAt: n.CreatedAt,
	}
	s.items[it.ID] = it
	s.append(domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: n.CreatedAt, Kind: domain.EventCreated,
		Station: it.Station, Actor: domain.ActorEngine,
	})

	return clone(it), nil
}

// Item implements ports.Store.
func (s *Store) Item(_ context.Context, id domain.ItemID) (domain.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.items[id]
	if !ok {
		return domain.Item{}, fmt.Errorf("item %s: %w", id, ports.ErrNotFound)
	}

	return clone(it), nil
}

// Waiting implements ports.Store.
func (s *Store) Waiting(_ context.Context, factory domain.FactoryID, station string, limit int) ([]domain.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	w := s.waiting(factory, station)
	out := make([]domain.Item, 0, min(limit, len(w)))

	for _, it := range w[:min(limit, len(w))] {
		out = append(out, clone(it))
	}

	return out, nil
}

// Leases implements ports.Store.
func (s *Store) Leases(_ context.Context, factory domain.FactoryID, station string) ([]domain.Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Lease{}

	for _, l := range s.leases {
		if l.FactoryID == factory && l.Station == station {
			out = append(out, l)
		}
	}

	return out, nil
}

// ClaimNext implements ports.Store.
func (s *Store) ClaimNext(_ context.Context, c ports.Claim) (domain.Item, domain.Lease, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	for _, l := range s.leases {
		if l.FactoryID == c.FactoryID && l.Station == c.Station.ID && l.Slot == c.Slot {
			return domain.Item{}, domain.Lease{}, fmt.Errorf("slot %d at %s: %w", c.Slot, c.Station.ID, ports.ErrSlotTaken)
		}
	}

	w := s.waiting(c.FactoryID, c.Station.ID)
	if len(w) == 0 {
		return domain.Item{}, domain.Lease{}, fmt.Errorf("station %s: %w", c.Station.ID, ports.ErrNoWork)
	}

	it := w[0]
	it.State = domain.StateInProgress
	it.UpdatedAt = c.Now

	// An unrouted lane leases to "" (as documented on ports.Claim); the engine then blocks it.
	agent, _ := c.Station.AgentFor(it.Lane)
	l := domain.Lease{
		ItemID: it.ID, FactoryID: it.FactoryID, Station: it.Station, Agent: agent, Slot: c.Slot,
		AcquiredAt: c.Now, HeartbeatAt: c.Now, ExpiresAt: c.ExpiresAt,
	}
	s.leases[it.ID] = l
	s.append(domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: c.Now, Kind: domain.EventClaimed,
		Station: it.Station, Actor: domain.ActorEngine, Data: map[string]any{"slot": c.Slot, "agent": agent},
	})

	return clone(it), l, nil
}

// HeartbeatLease implements ports.Store.
func (s *Store) HeartbeatLease(_ context.Context, l domain.Lease, now, expiresAt time.Time) (domain.Lease, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	held, ok := s.leases[l.ItemID]
	if !ok || !held.AcquiredAt.Equal(l.AcquiredAt) {
		return domain.Lease{}, fmt.Errorf("item %s: %w", l.ItemID, ports.ErrLeaseLost)
	}

	held.HeartbeatAt, held.ExpiresAt = now, expiresAt
	s.leases[l.ItemID] = held

	return held, nil
}

// RequeueExpiredLeases implements ports.Store.
func (s *Store) RequeueExpiredLeases(_ context.Context, now time.Time, limit int) ([]domain.Lease, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	var expired []domain.Lease

	for _, l := range s.leases {
		if l.Expired(now) {
			expired = append(expired, l)
		}
	}

	slices.SortFunc(expired, func(a, b domain.Lease) int { return a.ExpiresAt.Compare(b.ExpiresAt) })
	expired = expired[:min(limit, len(expired))]

	for _, l := range expired {
		delete(s.leases, l.ItemID)

		if it := s.items[l.ItemID]; it.State == domain.StateInProgress && it.Station == l.Station {
			it.State, it.UpdatedAt = domain.StateQueued, now
		}

		for _, r := range s.runs {
			if r.ItemID == l.ItemID && r.Result.Status == domain.RunRunning {
				r.Result = domain.RunResult{Status: domain.RunFailed, Error: "lease expired"}
				r.EndedAt = now
			}
		}

		s.append(domain.Event{
			FactoryID: l.FactoryID, ItemID: l.ItemID, At: now, Kind: domain.EventLeaseExpired,
			Station: l.Station, Actor: domain.ActorEngine, Data: map[string]any{"agent": l.Agent},
		})
	}

	return expired, nil
}

// StartRun implements ports.Store.
func (s *Store) StartRun(_ context.Context, r ports.NewRun) (domain.RunID, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	run := &domain.Run{
		ID: domain.RunID(s.newID()), FactoryID: r.FactoryID, ItemID: r.ItemID, Station: r.Station,
		Agent: r.Agent, Model: r.Model, Result: domain.RunResult{Status: domain.RunRunning}, StartedAt: r.StartedAt,
	}
	s.runs[run.ID] = run
	s.append(domain.Event{
		FactoryID: r.FactoryID, ItemID: r.ItemID, RunID: run.ID, At: r.StartedAt,
		Kind: domain.EventRunStarted, Station: r.Station, Actor: r.Agent,
	})

	return run.ID, nil
}

// FinishRun implements ports.Store.
func (s *Store) FinishRun(_ context.Context, id domain.RunID, res domain.RunResult, endedAt time.Time) error {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	r, ok := s.runs[id]
	if !ok {
		return fmt.Errorf("run %s: %w", id, ports.ErrNotFound)
	}

	if r.Result.Status != domain.RunRunning {
		return nil
	}

	r.Result, r.EndedAt = res, endedAt
	s.items[r.ItemID].CostUSD += res.CostUSD
	s.append(domain.Event{
		FactoryID: r.FactoryID, ItemID: r.ItemID, RunID: id, At: endedAt, Kind: domain.EventRunFinished,
		Station: r.Station, Actor: r.Agent, Data: map[string]any{"status": string(res.Status), "error": res.Error},
	})

	return nil
}

// Transition implements ports.Store.
func (s *Store) Transition(_ context.Context, t ports.Transition) (domain.Item, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	it, ok := s.items[t.ItemID]
	if !ok {
		return domain.Item{}, fmt.Errorf("item %s: %w", t.ItemID, ports.ErrNotFound)
	}

	if t.Lease != nil {
		held, ok := s.leases[t.ItemID]
		if !ok || !held.AcquiredAt.Equal(t.Lease.AcquiredAt) {
			return domain.Item{}, fmt.Errorf("item %s: %w", t.ItemID, ports.ErrLeaseLost)
		}
	}

	if it.Position() != t.From {
		return domain.Item{}, fmt.Errorf("item %s is %s at %s, not %s at %s: %w",
			t.ItemID, it.State, it.Station, t.From.State, t.From.Station, ports.ErrConflict)
	}

	if t.Lease != nil {
		delete(s.leases, t.ItemID)
	}

	it.Station, it.State, it.UpdatedAt = t.To.Station, t.To.State, t.At
	if t.Lane != "" {
		it.Lane = t.Lane
	}

	if t.Output != nil {
		it.Outputs[t.OutputStation] = t.Output
	}

	if t.Rework {
		it.ReworkCount++
	}

	if t.To.State == domain.StateDone {
		it.DoneAt = t.DoneAt
	}

	for _, e := range t.Events {
		s.append(e)
	}

	return clone(it), nil
}

// AppendEvents implements ports.Store.
func (s *Store) AppendEvents(_ context.Context, events ...domain.Event) error {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	for _, e := range events {
		s.append(e)
	}

	return nil
}

// waiting returns the items waiting at a station in pull order. Callers hold s.mu.
func (s *Store) waiting(factory domain.FactoryID, station string) []*domain.Item {
	var w []*domain.Item

	for _, it := range s.items {
		if it.FactoryID == factory && it.Station == station && it.State.Waiting() {
			w = append(w, it)
		}
	}

	slices.SortFunc(w, func(a, b *domain.Item) int {
		return cmp.Or(
			-cmp.Compare(rank(a.Class), rank(b.Class)),
			-cmp.Compare(a.Priority, b.Priority),
			a.CreatedAt.Compare(b.CreatedAt),
			cmp.Compare(a.Number, b.Number),
		)
	})

	return w
}

func (s *Store) append(e domain.Event) {
	e.ID = int64(len(s.events) + 1)
	s.events = append(s.events, e)
}

// newID returns a deterministic UUIDv7-shaped id. Callers hold s.mu.
func (s *Store) newID() string {
	s.ids++

	return fmt.Sprintf("00000000-0000-7000-8000-%012d", s.ids)
}

func rank(c domain.ClassOfService) int {
	if c == domain.ClassExpedite {
		return 1
	}

	return 0
}

func clone(it *domain.Item) domain.Item {
	c := *it
	c.Outputs = maps.Clone(it.Outputs)

	return c
}
