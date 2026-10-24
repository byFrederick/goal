package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// leaseExpiredError is what a run that lost its lease records as its error.
const leaseExpiredError = "lease expired"

// CreateItem implements ports.Store.
func (s *Store) CreateItem(ctx context.Context, n domain.NewItem) (domain.Item, error) {
	factory, err := parseID("factory", string(n.FactoryID))
	if err != nil {
		return domain.Item{}, err
	}

	var parent *uuid.UUID

	if n.ParentID != "" {
		id, err := parseID("parent item", string(n.ParentID))
		if err != nil {
			return domain.Item{}, err
		}

		parent = &id
	}

	id, err := newID()
	if err != nil {
		return domain.Item{}, err
	}

	var it domain.Item

	err = s.inTx(ctx, func(q *db.Queries) error {
		number, err := q.NextItemNumber(ctx, factory)
		if notFound(err) {
			return fmt.Errorf("factory %s: %w", n.FactoryID, ports.ErrNotFound)
		}

		if err != nil {
			return fmt.Errorf("next item number: %w", err)
		}

		row, err := q.InsertItem(ctx, db.InsertItemParams{
			ID: id, FactoryID: factory, Number: number, ParentID: parent, Type: n.Type, Lane: optional(n.Lane),
			Title: n.Title, Body: n.Body, Station: n.Station, State: string(n.State), ClassOfService: string(n.Class),
			Priority: int64(n.Priority), CreatedAt: n.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert item: %w", err)
		}

		if it, err = itemFrom(row); err != nil {
			return err
		}

		return appendEvents(ctx, q, []domain.Event{{
			FactoryID: it.FactoryID, ItemID: it.ID, At: n.CreatedAt, Kind: domain.EventCreated,
			Station: it.Station, Actor: domain.ActorEngine,
		}})
	})
	if err != nil {
		return domain.Item{}, fmt.Errorf("create item: %w", err)
	}

	return it, nil
}

// Item implements ports.Store.
func (s *Store) Item(ctx context.Context, id domain.ItemID) (domain.Item, error) {
	uid, err := parseID("item", string(id))
	if err != nil {
		return domain.Item{}, err
	}

	row, err := fetch(ctx, s.read, func(q *db.Queries) (db.WorkItem, error) {
		return q.GetItem(ctx, uid)
	})
	if notFound(err) {
		return domain.Item{}, fmt.Errorf("item %s: %w", id, ports.ErrNotFound)
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("item %s: %w", id, err)
	}

	return itemFrom(row)
}

// Waiting implements ports.Store.
func (s *Store) Waiting(ctx context.Context, factory domain.FactoryID, station string, limit int) ([]domain.Item, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	items, err := fetch(ctx, s.read, func(q *db.Queries) ([]domain.Item, error) {
		rows, err := q.WaitingItems(ctx, db.WaitingItemsParams{FactoryID: fid, Station: station, MaxItems: int64(limit)})
		if err != nil {
			return nil, err
		}

		return convertAll(rows, itemFrom)
	})
	if err != nil {
		return nil, fmt.Errorf("station %s: waiting items: %w", station, err)
	}

	return items, nil
}

// Leases implements ports.Store.
func (s *Store) Leases(ctx context.Context, factory domain.FactoryID, station string) ([]domain.Lease, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.Lease, error) {
		return q.StationLeases(ctx, db.StationLeasesParams{FactoryID: fid, Station: station})
	})
	if err != nil {
		return nil, fmt.Errorf("station %s: leases: %w", station, err)
	}

	out := make([]domain.Lease, 0, len(rows))
	for _, r := range rows {
		out = append(out, leaseFrom(r))
	}

	return out, nil
}

// ClaimNext implements ports.Store. The slot is checked before the item is locked, so a taken
// slot reports ErrSlotTaken without touching the queue; a claim that races another onto the
// same slot gets ErrSlotTaken from the unique lease index and rolls back.
func (s *Store) ClaimNext(ctx context.Context, c ports.Claim) (domain.Item, domain.Lease, error) {
	factory, err := parseID("factory", string(c.FactoryID))
	if err != nil {
		return domain.Item{}, domain.Lease{}, err
	}

	slotTaken := fmt.Errorf("slot %d at %s: %w", c.Slot, c.Station.ID, ports.ErrSlotTaken)

	var (
		it    domain.Item
		lease domain.Lease
	)

	err = s.inTx(ctx, func(q *db.Queries) error {
		taken, err := q.SlotTaken(ctx, db.SlotTakenParams{FactoryID: factory, Station: c.Station.ID, Slot: int64(c.Slot)})
		if err != nil {
			return fmt.Errorf("check slot: %w", err)
		}

		if taken {
			return slotTaken
		}

		next, err := q.LockNextWaitingItem(ctx, db.LockNextWaitingItemParams{FactoryID: factory, Station: c.Station.ID})
		if notFound(err) {
			return fmt.Errorf("station %s: %w", c.Station.ID, ports.ErrNoWork)
		}

		if err != nil {
			return fmt.Errorf("lock next item: %w", err)
		}

		row, err := q.StartItem(ctx, db.StartItemParams{ID: next.ID, UpdatedAt: c.Now})
		if err != nil {
			return fmt.Errorf("start item: %w", err)
		}

		if it, err = itemFrom(row); err != nil {
			return err
		}

		// An unrouted lane leases to "" (as documented on ports.Claim); the engine then blocks it.
		agent, _ := c.Station.AgentFor(it.Lane)

		l, err := q.InsertLease(ctx, db.InsertLeaseParams{
			ItemID: row.ID, FactoryID: factory, Station: it.Station, Agent: agent, Slot: int64(c.Slot),
			AcquiredAt: c.Now, ExpiresAt: c.ExpiresAt,
		})
		if violates(err, slotKey) {
			return slotTaken
		}

		if err != nil {
			return fmt.Errorf("insert lease: %w", err)
		}

		lease = leaseFrom(l)

		return appendEvents(ctx, q, []domain.Event{{
			FactoryID: it.FactoryID, ItemID: it.ID, At: c.Now, Kind: domain.EventClaimed, Station: it.Station,
			Actor: domain.ActorEngine, Data: map[string]any{"slot": c.Slot, "agent": agent},
		}})
	})
	if err != nil {
		return domain.Item{}, domain.Lease{}, fmt.Errorf("claim: %w", err)
	}

	return it, lease, nil
}

// HeartbeatLease implements ports.Store.
func (s *Store) HeartbeatLease(ctx context.Context, l domain.Lease, now, expiresAt time.Time) (domain.Lease, error) {
	item, err := parseID("item", string(l.ItemID))
	if err != nil {
		return domain.Lease{}, err
	}

	row, err := fetch(ctx, s.inTx, func(q *db.Queries) (db.Lease, error) {
		return q.HeartbeatLease(ctx, db.HeartbeatLeaseParams{
			ItemID: item, AcquiredAt: l.AcquiredAt, HeartbeatAt: now, ExpiresAt: expiresAt,
		})
	})
	if notFound(err) {
		return domain.Lease{}, fmt.Errorf("item %s: %w", l.ItemID, ports.ErrLeaseLost)
	}

	if err != nil {
		return domain.Lease{}, fmt.Errorf("heartbeat item %s: %w", l.ItemID, err)
	}

	return leaseFrom(row), nil
}

// RequeueExpiredLeases implements ports.Store. Leases whose item is locked by a transaction in
// flight are skipped; a later sweep sees them if they are still expired.
func (s *Store) RequeueExpiredLeases(ctx context.Context, now time.Time, limit int) ([]domain.Lease, error) {
	var expired []domain.Lease

	err := s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.LockExpiredLeases(ctx, db.LockExpiredLeasesParams{Now: now, MaxLeases: int64(limit)})
		if err != nil || len(rows) == 0 {
			return err
		}

		ids := make([]uuid.UUID, 0, len(rows))
		events := make([]domain.Event, 0, len(rows))

		for _, r := range rows {
			l := leaseFrom(r)
			expired = append(expired, l)
			ids = append(ids, r.ItemID)
			events = append(events, domain.Event{
				FactoryID: l.FactoryID, ItemID: l.ItemID, At: now, Kind: domain.EventLeaseExpired,
				Station: l.Station, Actor: domain.ActorEngine, Data: map[string]any{"agent": l.Agent},
			})
		}

		if err := q.RequeueLeases(ctx, db.RequeueLeasesParams{Now: now, ItemIds: ids}); err != nil {
			return err
		}

		msg := leaseExpiredError
		if err := q.FailRunningRuns(ctx, db.FailRunningRunsParams{Error: &msg, EndedAt: &now, ItemIds: ids}); err != nil {
			return err
		}

		return appendEvents(ctx, q, events)
	})
	if err != nil {
		return nil, fmt.Errorf("requeue expired leases: %w", err)
	}

	return expired, nil
}

// StartRun implements ports.Store.
func (s *Store) StartRun(ctx context.Context, r ports.NewRun) (domain.RunID, error) {
	factory, err := parseID("factory", string(r.FactoryID))
	if err != nil {
		return "", err
	}

	item, err := parseID("item", string(r.ItemID))
	if err != nil {
		return "", err
	}

	id, err := newID()
	if err != nil {
		return "", err
	}

	runID := domain.RunID(id.String())

	err = s.inTx(ctx, func(q *db.Queries) error {
		err := q.InsertRun(ctx, db.InsertRunParams{
			ID: id, FactoryID: factory, ItemID: item, Station: r.Station, Agent: r.Agent, Model: r.Model, StartedAt: r.StartedAt,
		})
		if err != nil {
			return fmt.Errorf("insert run: %w", err)
		}

		return appendEvents(ctx, q, []domain.Event{{
			FactoryID: r.FactoryID, ItemID: r.ItemID, RunID: runID, At: r.StartedAt,
			Kind: domain.EventRunStarted, Station: r.Station, Actor: r.Agent,
		}})
	})
	if err != nil {
		return "", fmt.Errorf("start run: %w", err)
	}

	return runID, nil
}

// FinishRun implements ports.Store.
func (s *Store) FinishRun(ctx context.Context, id domain.RunID, res domain.RunResult, endedAt time.Time) error {
	run, err := parseID("run", string(id))
	if err != nil {
		return err
	}

	err = s.inTx(ctx, func(q *db.Queries) error {
		item, err := q.RunItem(ctx, run)
		if notFound(err) {
			return fmt.Errorf("run %s: %w", id, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		// Lock the item before the run, in the order every other method takes them.
		if _, err := q.LockItem(ctx, item); err != nil {
			return fmt.Errorf("lock item: %w", err)
		}

		row, err := q.FinishRun(ctx, db.FinishRunParams{
			ID: run, Status: string(res.Status), SessionID: optional(res.SessionID), Turns: int64(res.Turns),
			InputTokens: res.InputTokens, OutputTokens: res.OutputTokens, CostUsd: res.CostUSD,
			Error: optional(res.Error), EndedAt: &endedAt,
		})
		if notFound(err) {
			return nil // already finished
		}

		if err != nil {
			return err
		}

		if err := q.AddItemCost(ctx, db.AddItemCostParams{ID: item, CostUsd: res.CostUSD}); err != nil {
			return err
		}

		return appendEvents(ctx, q, []domain.Event{{
			FactoryID: domain.FactoryID(row.FactoryID.String()), ItemID: domain.ItemID(item.String()), RunID: id,
			At: endedAt, Kind: domain.EventRunFinished, Station: row.Station, Actor: row.Agent,
			Data: runFinished(res),
		}})
	})
	if err != nil {
		return fmt.Errorf("finish run %s: %w", id, err)
	}

	return nil
}

// Transition implements ports.Store.
func (s *Store) Transition(ctx context.Context, t ports.Transition) (domain.Item, error) {
	item, err := parseID("item", string(t.ItemID))
	if err != nil {
		return domain.Item{}, err
	}

	var it domain.Item

	err = s.inTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockItem(ctx, item)
		if notFound(err) {
			return fmt.Errorf("item %s: %w", t.ItemID, ports.ErrNotFound)
		}

		if err != nil {
			return fmt.Errorf("lock item: %w", err)
		}

		// Releasing first is safe: a failed guard below rolls the release back.
		if t.Lease != nil {
			n, err := q.ReleaseLease(ctx, db.ReleaseLeaseParams{ItemID: item, AcquiredAt: t.Lease.AcquiredAt})
			if err != nil {
				return fmt.Errorf("release lease: %w", err)
			}

			if n == 0 {
				return fmt.Errorf("item %s: %w", t.ItemID, ports.ErrLeaseLost)
			}
		}

		if cur.Station != t.From.Station || domain.ItemState(cur.State) != t.From.State {
			return fmt.Errorf("item %s is %s at %s, not %s at %s: %w",
				t.ItemID, cur.State, cur.Station, t.From.State, t.From.Station, ports.ErrConflict)
		}

		row, err := q.MoveItem(ctx, moveParams(item, t))
		if err != nil {
			return fmt.Errorf("move item: %w", err)
		}

		if it, err = itemFrom(row); err != nil {
			return err
		}

		return appendEvents(ctx, q, t.Events)
	})
	if err != nil {
		return domain.Item{}, fmt.Errorf("transition: %w", err)
	}

	return it, nil
}

func moveParams(item uuid.UUID, t ports.Transition) db.MoveItemParams {
	p := db.MoveItemParams{
		ID: item, Station: t.To.Station, State: string(t.To.State), UpdatedAt: t.At, Lane: optional(t.Lane),
		Output: t.Output, OutputStation: t.OutputStation,
		SetDoneAt: t.To.State == domain.StateDone, DoneAt: optionalTime(t.DoneAt),
	}

	if t.Rework {
		p.Rework = 1
	}

	return p
}

// AppendEvents implements ports.Store.
func (s *Store) AppendEvents(ctx context.Context, events ...domain.Event) error {
	if err := s.inTx(ctx, func(q *db.Queries) error { return appendEvents(ctx, q, events) }); err != nil {
		return fmt.Errorf("append events: %w", err)
	}

	return nil
}

// ItemEvents implements ports.Store.
func (s *Store) ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error) {
	uid, err := parseID("item", string(id))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.ItemEvent, error) {
		return q.ItemEvents(ctx, db.ItemEventsParams{ItemID: uid, After: after, MaxEvents: int64(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("events of item %s: %w", id, err)
	}

	return convertAll(rows, eventFrom)
}
