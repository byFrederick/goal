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
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Store = (*Store)(nil)

// Store keeps items, leases, runs and events in memory behind one mutex, so every method is
// atomic like a PostgreSQL transaction. Like row-level security, every method needs an
// organization in ctx (domain.ErrNoOrg otherwise) and sees only that organization's rows. A
// factory belongs to the organization AddFactory names, or to the one its first item is created
// in. Slot caps are set with SetSlotCaps (Tenancy.UpdateOrg does it for a linked store); a linked
// Tenancy also keeps the store's audit entries.
type Store struct {
	mu        sync.Mutex
	audit     ports.Audit
	factories map[domain.FactoryID]domain.OrgID
	orgCaps   map[domain.OrgID]int
	factCaps  map[domain.FactoryID]int
	items     map[domain.ItemID]*domain.Item
	numbers   map[domain.FactoryID]int64
	leases    map[domain.ItemID]domain.Lease
	runs      map[domain.RunID]*domain.Run
	events    []domain.Event
	labels    map[int64]touchLabel
	spans     map[domain.ItemID][]domain.Span
	leaseLog  map[domain.ItemID][]domain.LeaseSpan
	andon     []andonRecord
	edges     []domain.Edge // item needs, in the order they were added
	ids       int
	changes   changes
}

// andonRecord is one andon pull of a factory of an organization.
type andonRecord struct {
	org  domain.OrgID
	pull domain.AndonPull
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{
		factories: map[domain.FactoryID]domain.OrgID{},
		orgCaps:   map[domain.OrgID]int{},
		factCaps:  map[domain.FactoryID]int{},
		items:     map[domain.ItemID]*domain.Item{},
		numbers:   map[domain.FactoryID]int64{},
		leases:    map[domain.ItemID]domain.Lease{},
		runs:      map[domain.RunID]*domain.Run{},
		labels:    map[int64]touchLabel{},
		spans:     map[domain.ItemID][]domain.Span{},
		leaseLog:  map[domain.ItemID][]domain.LeaseSpan{},
	}
}

// auditTo makes a keep the audit entries the store appends.
func (s *Store) auditTo(a ports.Audit) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.audit = a
}

// AddFactory registers a factory in an organization, as SaveFactory does for PostgreSQL.
func (s *Store) AddFactory(org domain.OrgID, id domain.FactoryID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.factories[id] = org
}

// SetSlotCaps sets an organization's slot cap and those of the named factories, as
// Tenancy.UpdateOrg does in PostgreSQL; 0 clears a cap.
func (s *Store) SetSlotCaps(org domain.OrgID, maxSlots int, factories map[domain.FactoryID]int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.orgCaps[org] = maxSlots
	maps.Copy(s.factCaps, factories)
}

// factorySlots returns every factory of org with its cap (0: none).
func (s *Store) factorySlots(org domain.OrgID) map[domain.FactoryID]int {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := map[domain.FactoryID]int{}

	for id, owner := range s.factories {
		if owner == org {
			out[id] = s.factCaps[id]
		}
	}

	return out
}

// purge removes every row of org, as goal_purge_org does.
func (s *Store) purge(org domain.OrgID) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	gone := map[domain.ItemID]bool{}

	for id, it := range s.items {
		if it.OrgID == org {
			gone[id] = true

			delete(s.items, id)
			delete(s.leases, id)
			delete(s.spans, id)
			delete(s.leaseLog, id)
		}
	}

	maps.DeleteFunc(s.runs, func(_ domain.RunID, r *domain.Run) bool { return gone[r.ItemID] })
	s.edges = slices.DeleteFunc(s.edges, func(e domain.Edge) bool { return gone[e.Item] || gone[e.Needs] })
	s.events = slices.DeleteFunc(s.events, func(e domain.Event) bool {
		if gone[e.ItemID] {
			delete(s.labels, e.ID)
		}

		return gone[e.ItemID]
	})

	for id, owner := range s.factories {
		if owner == org {
			delete(s.factories, id)
			delete(s.numbers, id)
			delete(s.factCaps, id)
		}
	}

	delete(s.orgCaps, org)
	s.andon = slices.DeleteFunc(s.andon, func(a andonRecord) bool { return a.org == org })
}

// orgOf returns the organization in ctx, as row-level security requires.
func orgOf(ctx context.Context) (domain.OrgID, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return "", domain.ErrNoOrg
	}

	return org, nil
}

// visible returns the item when it exists in org. Callers hold s.mu.
func (s *Store) visible(id domain.ItemID, org domain.OrgID) (*domain.Item, bool) {
	it, ok := s.items[id]
	if !ok || it.OrgID != org {
		return nil, false
	}

	return it, true
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

// CreateItem implements ports.Store.
func (s *Store) CreateItem(ctx context.Context, n domain.NewItem) (domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Item{}, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	if owner, ok := s.factories[n.FactoryID]; ok && owner != org {
		return domain.Item{}, fmt.Errorf("factory %s: %w", n.FactoryID, ports.ErrNotFound)
	}

	s.factories[n.FactoryID] = org
	s.numbers[n.FactoryID]++
	it := &domain.Item{
		ID: domain.ItemID(s.newID()), OrgID: org, FactoryID: n.FactoryID, Number: s.numbers[n.FactoryID],
		ParentID: n.ParentID, Type: n.Type, Lane: n.Lane, Title: n.Title, Body: n.Body,
		Station: n.Station, State: n.State, Class: n.Class, Priority: n.Priority,
		Outputs: map[string]json.RawMessage{}, CreatedAt: n.CreatedAt, UpdatedAt: n.CreatedAt,
	}
	s.items[it.ID] = it
	s.trackSpan(it, nil, n.CreatedAt)
	s.append(domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: n.CreatedAt, Kind: domain.EventCreated,
		Station: it.Station, Actor: domain.ActorEngine,
	})

	return clone(it), nil
}

// Item implements ports.Store.
func (s *Store) Item(ctx context.Context, id domain.ItemID) (domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Item{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.visible(id, org)
	if !ok {
		return domain.Item{}, fmt.Errorf("item %s: %w", id, ports.ErrNotFound)
	}

	return clone(it), nil
}

// Waiting implements ports.Store.
func (s *Store) Waiting(ctx context.Context, factory domain.FactoryID, station string, limit int) ([]domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	w := s.waiting(org, factory, station)
	out := make([]domain.Item, 0, min(limit, len(w)))

	for _, it := range w[:min(limit, len(w))] {
		out = append(out, clone(it))
	}

	return out, nil
}

// Leases implements ports.Store.
func (s *Store) Leases(ctx context.Context, factory domain.FactoryID, station string) ([]domain.Lease, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Lease{}

	for _, l := range s.leases {
		if _, ok := s.visible(l.ItemID, org); ok && l.FactoryID == factory && l.Station == station {
			out = append(out, l)
		}
	}

	return out, nil
}

// ClaimNext implements ports.Store.
func (s *Store) ClaimNext(ctx context.Context, c ports.Claim) (domain.Item, domain.Lease, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Item{}, domain.Lease{}, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	if err := s.slotCapsLocked(org, c.FactoryID); err != nil {
		return domain.Item{}, domain.Lease{}, err
	}

	for _, l := range s.leases {
		if l.FactoryID == c.FactoryID && l.Station == c.Station.ID && l.Slot == c.Slot {
			return domain.Item{}, domain.Lease{}, fmt.Errorf("slot %d at %s: %w", c.Slot, c.Station.ID, ports.ErrSlotTaken)
		}
	}

	w := slices.DeleteFunc(s.waiting(org, c.FactoryID, c.Station.ID), s.heldByNeeds)
	if len(w) == 0 {
		return domain.Item{}, domain.Lease{}, fmt.Errorf("station %s: %w", c.Station.ID, ports.ErrNoWork)
	}

	it := w[0]
	before := *it
	it.State = domain.StateInProgress
	it.UpdatedAt = c.Now
	s.trackSpan(it, &before, c.Now)

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

// slotCapsLocked returns ErrOrgFull or ErrFactoryFull when org's or the factory's leases reach
// its cap. A factory of another organization has no cap here, as row-level security hides it.
// Callers hold s.mu.
func (s *Store) slotCapsLocked(org domain.OrgID, factory domain.FactoryID) error {
	orgMax, factoryMax := s.orgCaps[org], 0
	if s.factories[factory] == org {
		factoryMax = s.factCaps[factory]
	}

	var inOrg, inFactory int

	for _, l := range s.leases {
		if _, ok := s.visible(l.ItemID, org); ok {
			inOrg++

			if l.FactoryID == factory {
				inFactory++
			}
		}
	}

	switch {
	case orgMax > 0 && inOrg >= orgMax:
		return fmt.Errorf("%d of %d slots: %w", inOrg, orgMax, ports.ErrOrgFull)
	case factoryMax > 0 && inFactory >= factoryMax:
		return fmt.Errorf("factory %s, %d of %d slots: %w", factory, inFactory, factoryMax, ports.ErrFactoryFull)
	default:
		return nil
	}
}

// HeartbeatLease implements ports.Store.
func (s *Store) HeartbeatLease(ctx context.Context, l domain.Lease, now, expiresAt time.Time) (domain.Lease, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Lease{}, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	held, ok := s.leases[l.ItemID]
	if _, mine := s.visible(l.ItemID, org); !ok || !mine || !held.AcquiredAt.Equal(l.AcquiredAt) {
		return domain.Lease{}, fmt.Errorf("item %s: %w", l.ItemID, ports.ErrLeaseLost)
	}

	held.HeartbeatAt, held.ExpiresAt = now, expiresAt
	s.leases[l.ItemID] = held

	return held, nil
}

// RequeueExpiredLeases implements ports.Store.
func (s *Store) RequeueExpiredLeases(ctx context.Context, now time.Time, limit int) ([]domain.Lease, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	var expired []domain.Lease

	for _, l := range s.leases {
		if _, ok := s.visible(l.ItemID, org); ok && l.Expired(now) {
			expired = append(expired, l)
		}
	}

	slices.SortFunc(expired, func(a, b domain.Lease) int { return a.ExpiresAt.Compare(b.ExpiresAt) })
	expired = expired[:min(limit, len(expired))]

	for _, l := range expired {
		delete(s.leases, l.ItemID)
		s.recordLease(l, domain.LeaseExpired, now)

		if it := s.items[l.ItemID]; it.State == domain.StateInProgress && it.Station == l.Station {
			before := *it
			it.State, it.UpdatedAt = domain.StateQueued, now
			s.trackSpan(it, &before, now)
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

// FinishLostRuns implements ports.Store.
func (s *Store) FinishLostRuns(ctx context.Context, endedBefore, now time.Time, limit int) ([]domain.Run, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	type lostRun struct {
		run   *domain.Run
		ended time.Time
	}

	var lost []lostRun

	for _, r := range s.runs {
		if _, ok := s.visible(r.ItemID, org); !ok || r.Result.Status != domain.RunRunning {
			continue
		}

		if ended, gone := s.leaseEnded(r); gone && ended.Before(endedBefore) {
			lost = append(lost, lostRun{run: r, ended: ended})
		}
	}

	slices.SortFunc(lost, func(a, b lostRun) int {
		return cmp.Or(a.ended.Compare(b.ended), a.run.StartedAt.Compare(b.run.StartedAt), cmp.Compare(a.run.ID, b.run.ID))
	})

	out := make([]domain.Run, 0, min(limit, len(lost)))

	for _, l := range lost[:min(limit, len(lost))] {
		r := l.run
		r.Result, r.EndedAt = domain.RunResult{Status: domain.RunCancelled, Error: domain.RunLostError}, now
		s.append(domain.Event{
			FactoryID: r.FactoryID, ItemID: r.ItemID, RunID: r.ID, At: now, Kind: domain.EventRunFinished,
			Station: r.Station, Actor: r.Agent, Data: map[string]any{
				"status": string(domain.RunCancelled), "error": domain.RunLostError, "cost_usd": 0.0, "turns": 0,
			},
		})
		out = append(out, *r)
	}

	return out, nil
}

// leaseEnded reports whether a run's lease (the item's lease at the run's station acquired no
// later than the run started) is gone and when it ended: its latest span's end, or the run's
// start when it left none. Callers hold s.mu.
func (s *Store) leaseEnded(r *domain.Run) (time.Time, bool) {
	if l, ok := s.leases[r.ItemID]; ok && l.Station == r.Station && !l.AcquiredAt.After(r.StartedAt) {
		return time.Time{}, false
	}

	ended := r.StartedAt

	for _, sp := range s.leaseLog[r.ItemID] {
		if sp.Station == r.Station && !sp.AcquiredAt.After(r.StartedAt) && sp.EndedAt.After(ended) {
			ended = sp.EndedAt
		}
	}

	return ended, true
}

// StartRun implements ports.Store.
func (s *Store) StartRun(ctx context.Context, r ports.NewRun) (domain.RunID, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	if _, ok := s.visible(r.ItemID, org); !ok {
		return "", fmt.Errorf("item %s: %w", r.ItemID, ports.ErrNotFound)
	}

	run := &domain.Run{
		ID: domain.RunID(s.newID()), FactoryID: r.FactoryID, DefinitionVersionID: r.DefinitionVersionID, ItemID: r.ItemID, Station: r.Station,
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
func (s *Store) FinishRun(ctx context.Context, id domain.RunID, res domain.RunResult, endedAt time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	r, ok := s.runs[id]
	if ok {
		_, ok = s.visible(r.ItemID, org)
	}

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
		Station: r.Station, Actor: r.Agent, Data: map[string]any{
			"status": string(res.Status), "error": res.Error, "cost_usd": res.CostUSD, "turns": res.Turns,
		},
	})

	return nil
}

// Runs implements ports.Store.
func (s *Store) Runs(ctx context.Context, item domain.ItemID, limit int) ([]domain.Run, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Run{}

	if _, ok := s.visible(item, org); !ok {
		return out, nil
	}

	for _, r := range s.runs {
		if r.ItemID == item {
			out = append(out, *r)
		}
	}

	slices.SortFunc(out, func(a, b domain.Run) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), cmp.Compare(a.ID, b.ID))
	})

	return out[:min(limit, len(out))], nil
}

// Run implements ports.Store.
func (s *Store) Run(ctx context.Context, id domain.RunID) (domain.Run, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Run{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.runs[id]
	if ok {
		_, ok = s.visible(r.ItemID, org)
	}

	if !ok {
		return domain.Run{}, fmt.Errorf("run %s: %w", id, ports.ErrNotFound)
	}

	return *r, nil
}

// Transition implements ports.Store. Its audit entry goes to the linked Tenancy, if any, after
// the change: the fake has no transaction spanning both.
func (s *Store) Transition(ctx context.Context, t ports.Transition) (domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Item{}, err
	}

	it, audit, err := s.transition(org, t)
	if err != nil || audit == nil || t.Audit == nil {
		return it, err
	}

	if err := audit.Append(ctx, *t.Audit); err != nil {
		return domain.Item{}, fmt.Errorf("audit transition: %w", err)
	}

	return it, nil
}

func (s *Store) transition(org domain.OrgID, t ports.Transition) (domain.Item, ports.Audit, error) {
	if t.To.State == domain.StateAssembly {
		return domain.Item{}, nil, fmt.Errorf("item %s: only Assemble makes an assembly: %w", t.ItemID, domain.ErrInvalid)
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	it, ok := s.visible(t.ItemID, org)
	if !ok {
		return domain.Item{}, nil, fmt.Errorf("item %s: %w", t.ItemID, ports.ErrNotFound)
	}

	if err := s.checkTransition(it, t); err != nil {
		return domain.Item{}, nil, err
	}

	s.move(it, t)
	s.closeAssembly(it, t.At, false)

	return clone(it), s.audit, nil
}

// checkTransition is Transition's guard: the lease, then the position. Callers hold s.mu.
func (s *Store) checkTransition(it *domain.Item, t ports.Transition) error {
	if err := s.checkLease(t); err != nil {
		return err
	}

	if it.Position() != t.From {
		return fmt.Errorf("item %s is %s at %s, not %s at %s: %w",
			t.ItemID, it.State, it.Station, t.From.State, t.From.Station, ports.ErrConflict)
	}

	return nil
}

// move applies a guarded transition to it. Callers hold s.mu.
func (s *Store) move(it *domain.Item, t ports.Transition) {
	if held, ok := s.leases[t.ItemID]; ok && (t.Lease != nil || t.TakeLease) {
		s.recordLease(held, t.Outcome, t.At)
		delete(s.leases, t.ItemID)
	}

	before := *it
	it.Station, it.State, it.UpdatedAt = t.To.Station, t.To.State, t.At

	if t.Lane != "" {
		it.Lane = t.Lane
	}

	if t.Paths != nil {
		it.Paths = nil
		if len(t.Paths) > 0 {
			it.Paths = slices.Clone(t.Paths)
		}
	}

	if t.BaseCommit != "" {
		it.BaseCommit = t.BaseCommit
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

	s.trackSpan(it, &before, t.At)

	for _, e := range t.Events {
		s.append(e)
	}
}

// checkLease refuses a transition that releases or takes a lease without an outcome, names both,
// or releases one no longer held (ErrLeaseLost). Callers hold s.mu.
func (s *Store) checkLease(t ports.Transition) error {
	if t.Lease == nil && !t.TakeLease {
		return nil
	}

	switch {
	case t.Outcome == "":
		return fmt.Errorf("item %s: releasing its lease needs an outcome", t.ItemID)
	case t.Lease != nil && t.TakeLease:
		return fmt.Errorf("item %s: a transition releases its lease or takes it, not both", t.ItemID)
	case t.TakeLease:
		return nil
	}

	held, ok := s.leases[t.ItemID]
	if !ok || !held.AcquiredAt.Equal(t.Lease.AcquiredAt) {
		return fmt.Errorf("item %s: %w", t.ItemID, ports.ErrLeaseLost)
	}

	return nil
}

// AppendEvents implements ports.Store.
func (s *Store) AppendEvents(ctx context.Context, events ...domain.Event) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	for _, e := range events {
		if it, ok := s.items[e.ItemID]; ok && it.OrgID != org {
			return fmt.Errorf("event of item %s: %w", e.ItemID, ports.ErrNotFound)
		}
	}

	for _, e := range events {
		s.append(e)
	}

	return nil
}

// AppendNote implements ports.Store. The audit entry goes to the linked Tenancy, if any, after
// the note: the fake has no transaction spanning both.
func (s *Store) AppendNote(ctx context.Context, e domain.Event, by domain.Actor) (domain.Event, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Event{}, err
	}

	got, audit, err := s.appendNote(org, e)
	if err != nil || audit == nil {
		return got, err
	}

	text, _ := e.Data["text"].(string)

	if err := audit.Append(ctx, domain.AuditEntry{
		At: e.At, Actor: by, Action: domain.AuditItemNoted, TargetKind: domain.AuditTargetItem, TargetID: string(e.ItemID),
		FactoryID: e.FactoryID, Data: map[string]any{"length": utf8.RuneCountInString(text)},
	}); err != nil {
		return domain.Event{}, fmt.Errorf("audit note: %w", err)
	}

	return got, nil
}

func (s *Store) appendNote(org domain.OrgID, e domain.Event) (domain.Event, ports.Audit, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	it, ok := s.visible(e.ItemID, org)
	if !ok {
		return domain.Event{}, nil, fmt.Errorf("item %s: %w", e.ItemID, ports.ErrNotFound)
	}

	if it.State == domain.StateDone || it.State == domain.StateCancelled {
		return domain.Event{}, nil, fmt.Errorf("note on item %s, %s: %w", e.ItemID, it.State, ports.ErrConflict)
	}

	s.append(e)

	return s.events[len(s.events)-1], s.audit, nil
}

// NotesSinceLastRun implements ports.Store.
func (s *Store) NotesSinceLastRun(ctx context.Context, item domain.ItemID) ([]domain.Event, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	notes := []domain.Event{}

	if _, ok := s.visible(item, org); !ok {
		return notes, nil
	}

	for _, e := range s.events {
		switch {
		case e.ItemID != item:
		case e.Kind == domain.EventRunStarted:
			notes = notes[:0]
		case e.Kind == domain.EventNote:
			notes = append(notes, e)
		}
	}

	return notes, nil
}

// ItemEvents implements ports.Store.
func (s *Store) ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Event{}

	if _, ok := s.visible(id, org); !ok {
		return out, nil
	}

	for _, e := range s.events {
		if len(out) == limit {
			break
		}

		if e.ItemID == id && e.ID > after {
			out = append(out, e)
		}
	}

	return out, nil
}

// waiting returns an organization's items waiting at a station in pull order. Callers hold s.mu.
func (s *Store) waiting(org domain.OrgID, factory domain.FactoryID, station string) []*domain.Item {
	var w []*domain.Item

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == factory && it.Station == station && it.State.Waiting() {
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
	s.labelEvent(e)
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
	c.PartPaths = slices.Clone(it.PartPaths)
	c.Paths = slices.Clone(it.Paths)

	return c
}

// RunsOver implements ports.Store.
func (s *Store) RunsOver(ctx context.Context, runs []domain.RunID) ([]domain.RunID, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var over []domain.RunID

	for _, id := range slices.Compact(slices.Sorted(slices.Values(runs))) {
		r, ok := s.runs[id]
		if !ok {
			continue
		}

		if _, ok := s.visible(r.ItemID, org); !ok {
			continue
		}

		if _, gone := s.leaseEnded(r); r.Result.Status != domain.RunRunning && gone {
			over = append(over, id)
		}
	}

	return over, nil
}
