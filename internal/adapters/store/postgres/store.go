package postgres

import (
	"cmp"
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
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
			Priority: int64(n.Priority), CreatedAt: n.CreatedAt, Source: optional(string(n.Source)), Trigger: optional(n.Trigger),
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

// ClaimNext implements ports.Store. The slot caps are checked first and the slot next, both
// before the item is locked, so a full organization or factory and a taken slot report their
// error without touching the queue; a claim that races another onto the same slot gets
// ErrSlotTaken from the unique lease index and rolls back.
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
		if err := checkSlotCaps(ctx, q, factory); err != nil {
			return err
		}

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

// checkSlotCaps returns ErrOrgFull or ErrFactoryFull when the organization's or the factory's
// leases reach its cap. While either cap is set it first takes the organization's slot lock, held
// until the claim commits, and counts the leases under it: claims of the organization then run
// one after another, each seeing the leases of those before it, so no cap is ever exceeded.
// Without caps claims take no lock.
func checkSlotCaps(ctx context.Context, q *db.Queries, factory uuid.UUID) error {
	caps, err := q.SlotCaps(ctx, factory)
	if err != nil {
		return fmt.Errorf("slot caps: %w", err)
	}

	if caps.OrgMax == 0 && caps.FactoryMax == 0 {
		return nil
	}

	if err := q.LockOrgSlots(ctx); err != nil {
		return fmt.Errorf("lock org slots: %w", err)
	}

	if caps, err = q.SlotCaps(ctx, factory); err != nil {
		return fmt.Errorf("slot caps: %w", err)
	}

	switch {
	case caps.OrgMax > 0 && caps.OrgLeases >= int64(caps.OrgMax):
		return fmt.Errorf("%d of %d slots: %w", caps.OrgLeases, caps.OrgMax, ports.ErrOrgFull)
	case caps.FactoryMax > 0 && caps.FactoryLeases >= int64(caps.FactoryMax):
		return fmt.Errorf("factory %s, %d of %d slots: %w", factory, caps.FactoryLeases, caps.FactoryMax, ports.ErrFactoryFull)
	default:
		return nil
	}
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

// FinishLostRuns implements ports.Store. Runs whose item a transaction in flight holds are
// skipped; a later sweep sees them if they are still lost.
func (s *Store) FinishLostRuns(ctx context.Context, endedBefore, now time.Time, limit int) ([]domain.Run, error) {
	var out []domain.Run

	err := s.inTx(ctx, func(q *db.Queries) error {
		ids, err := q.LockLostRuns(ctx, db.LockLostRunsParams{EndedBefore: endedBefore, MaxRuns: int64(limit)})
		if err != nil || len(ids) == 0 {
			return err
		}

		msg := domain.RunLostError

		rows, err := q.CancelLostRuns(ctx, db.CancelLostRunsParams{Error: &msg, EndedAt: &now, Ids: ids})
		if err != nil {
			return err
		}

		byID := make(map[uuid.UUID]domain.Run, len(rows))
		for _, r := range rows {
			byID[r.ID] = runFrom(r)
		}

		events := make([]domain.Event, 0, len(ids))

		for _, id := range ids { // in the order the runs' leases ended
			r, ok := byID[id]
			if !ok {
				continue
			}

			out = append(out, r)
			events = append(events, domain.Event{
				FactoryID: r.FactoryID, ItemID: r.ItemID, RunID: r.ID, At: now, Kind: domain.EventRunFinished,
				Station: r.Station, Actor: r.Agent, Data: runFinished(r.Result),
			})
		}

		return appendEvents(ctx, q, events)
	})
	if err != nil {
		return nil, fmt.Errorf("finish lost runs: %w", err)
	}

	return out, nil
}

// RunsOver implements ports.Store. An id that is not a UUID names no run.
func (s *Store) RunsOver(ctx context.Context, runs []domain.RunID) ([]domain.RunID, error) {
	ids := make([]uuid.UUID, 0, len(runs))

	for _, r := range runs {
		if id, err := uuid.Parse(string(r)); err == nil {
			ids = append(ids, id)
		}
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]uuid.UUID, error) { return q.RunsOver(ctx, ids) })
	if err != nil {
		return nil, fmt.Errorf("runs over: %w", err)
	}

	over := make([]domain.RunID, len(rows))
	for i, id := range rows {
		over[i] = domain.RunID(id.String())
	}

	return over, nil
}

// StartRun implements ports.Store.
func (s *Store) StartRun(ctx context.Context, r ports.NewRun) (domain.RunID, error) {
	factory, err := parseID("factory", string(r.FactoryID))
	if err != nil {
		return "", err
	}

	kind := cmp.Or(r.Kind, domain.RunKindStation)

	// Only a Jonah run may work no item (00077); it writes no run_started item event.
	var item *uuid.UUID

	if r.ItemID != "" || kind != domain.RunKindJonah {
		id, err := parseID("item", string(r.ItemID))
		if err != nil {
			return "", err
		}

		item = &id
	}

	var version *uuid.UUID

	if r.DefinitionVersionID != "" {
		v, err := parseID("definition version", string(r.DefinitionVersionID))
		if err != nil {
			return "", err
		}

		version = &v
	}

	id, err := newID()
	if err != nil {
		return "", err
	}

	runID := domain.RunID(id.String())

	err = s.inTx(ctx, func(q *db.Queries) error {
		err := q.InsertRun(ctx, db.InsertRunParams{
			ID: id, FactoryID: factory, DefinitionVersionID: version, ItemID: item, Station: r.Station, Agent: r.Agent,
			Model: r.Model, StartedAt: r.StartedAt, Kind: string(kind),
		})
		if isForeignKeyViolation(err) {
			return fmt.Errorf("insert run: its item or factory: %w", ports.ErrNotFound)
		}

		if err != nil {
			return fmt.Errorf("insert run: %w", err)
		}

		if item == nil {
			return nil
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

// Runs implements ports.Store.
func (s *Store) Runs(ctx context.Context, item domain.ItemID, limit int) ([]domain.Run, error) {
	uid, err := parseID("item", string(item))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.AgentRun, error) {
		return q.ItemRuns(ctx, db.ItemRunsParams{ItemID: &uid, MaxRuns: int64(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("runs of item %s: %w", item, err)
	}

	return convertAll(rows, func(r db.AgentRun) (domain.Run, error) { return runFrom(r), nil })
}

// Run implements ports.Store.
func (s *Store) Run(ctx context.Context, id domain.RunID) (domain.Run, error) {
	uid, err := parseID("run", string(id))
	if err != nil {
		return domain.Run{}, err
	}

	row, err := fetch(ctx, s.read, func(q *db.Queries) (db.AgentRun, error) { return q.GetRun(ctx, uid) })
	if notFound(err) {
		return domain.Run{}, fmt.Errorf("run %s: %w", id, ports.ErrNotFound)
	}

	if err != nil {
		return domain.Run{}, fmt.Errorf("run %s: %w", id, err)
	}

	return runFrom(row), nil
}

// OrgRuns implements ports.Store: the page and the summary in one read-only transaction. Ids
// that are not UUIDs name no factory.
func (s *Store) OrgRuns(ctx context.Context, q ports.RunQuery) ([]domain.ListedRun, domain.RunSummary, error) {
	factories := uuids(q.Factories)
	if len(factories) == 0 {
		return []domain.ListedRun{}, domain.RunSummary{}, nil
	}

	params := db.ListOrgRunsParams{
		Factories: factories, Since: q.Since, Until: q.Until, Status: string(q.Status),
		Pattern: likeEscaper.Replace(q.Text), Number: q.Number, MaxRuns: int64(q.Limit),
	}

	if q.NumberOf != "" {
		of, err := parseID("factory", string(q.NumberOf))
		if err != nil {
			return nil, domain.RunSummary{}, err
		}

		params.NumberOf = &of
	}

	if q.After != nil {
		id, err := parseID("run", string(q.After.ID))
		if err != nil {
			return nil, domain.RunSummary{}, err
		}

		params.Paged, params.AfterStartedAt, params.AfterID = true, q.After.StartedAt, id
	}

	var (
		rows []db.ListOrgRunsRow
		sums []db.OrgRunsSummaryRow
	)

	err := s.read(ctx, func(dq *db.Queries) error {
		var err error
		if rows, err = dq.ListOrgRuns(ctx, params); err != nil {
			return err
		}

		sums, err = dq.OrgRunsSummary(ctx, db.OrgRunsSummaryParams{Factories: factories, Since: q.Since, Until: q.Until})

		return err
	})
	if err != nil {
		return nil, domain.RunSummary{}, fmt.Errorf("runs of the organization: %w", err)
	}

	runs := make([]domain.ListedRun, 0, len(rows))
	for _, r := range rows {
		runs = append(runs, domain.ListedRun{Run: runFrom(r.AgentRun), ItemNumber: r.ItemNumber, ItemTitle: r.ItemTitle})
	}

	var sum domain.RunSummary
	for _, r := range sums {
		sum.Total += int(r.Total)
		sum.Running += int(r.Running)
		sum.Failed += int(r.Failed)
		sum.Finished += int(r.Finished)
		sum.SpendUSD += r.SpendUsd
		sum.ByFactory = append(sum.ByFactory, domain.FactorySpend{FactoryID: domain.FactoryID(r.FactoryID.String()), USD: r.SpendUsd})
	}

	return runs, sum, nil
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

		// Lock the item before the run, in the order every other method takes them. A Jonah run
		// works no item (00077): it adds no item cost and writes no run_finished item event.
		if item != nil {
			if _, err := q.LockItem(ctx, *item); err != nil {
				return fmt.Errorf("lock item: %w", err)
			}
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

		if item == nil {
			return nil
		}

		if err := q.AddItemCost(ctx, db.AddItemCostParams{ID: *item, CostUsd: res.CostUSD}); err != nil {
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
	if t.To.State == domain.StateAssembly {
		return domain.Item{}, fmt.Errorf("transition: item %s: only Assemble makes an assembly: %w", t.ItemID, domain.ErrInvalid)
	}

	item, err := parseID("item", string(t.ItemID))
	if err != nil {
		return domain.Item{}, err
	}

	var it domain.Item

	err = s.inTx(ctx, func(q *db.Queries) error {
		cur, err := lockItem(ctx, q, item, t.ItemID)
		if err != nil {
			return err
		}

		row, err := transition(ctx, q, cur, t)
		if err != nil {
			return err
		}

		if err := closeAssembly(ctx, q, row, t.At, false); err != nil {
			return err
		}

		it, err = itemFrom(row)

		return err
	})
	if err != nil {
		return domain.Item{}, fmt.Errorf("transition: %w", err)
	}

	return it, nil
}

// lockItem locks an item's row for a change: ErrNotFound when there is none.
func lockItem(ctx context.Context, q *db.Queries, item uuid.UUID, id domain.ItemID) (db.WorkItem, error) {
	cur, err := q.LockItem(ctx, item)
	if notFound(err) {
		return db.WorkItem{}, fmt.Errorf("item %s: %w", id, ports.ErrNotFound)
	}

	if err != nil {
		return db.WorkItem{}, fmt.Errorf("lock item: %w", err)
	}

	return cur, nil
}

// transition applies t to the locked item cur: the lease, the guard, the move, its events and
// audit entry.
func transition(ctx context.Context, q *db.Queries, cur db.WorkItem, t ports.Transition) (db.WorkItem, error) {
	// Releasing first is safe: a failed guard below rolls the release back, its lease span too.
	if err := releaseOrTake(ctx, q, cur.ID, t); err != nil {
		return db.WorkItem{}, err
	}

	if cur.Station != t.From.Station || domain.ItemState(cur.State) != t.From.State {
		return db.WorkItem{}, fmt.Errorf("item %s is %s at %s, not %s at %s: %w",
			t.ItemID, cur.State, cur.Station, t.From.State, t.From.Station, ports.ErrConflict)
	}

	row, err := q.MoveItem(ctx, moveParams(cur.ID, t))
	if err != nil {
		return db.WorkItem{}, fmt.Errorf("move item: %w", err)
	}

	if err := appendEvents(ctx, q, t.Events); err != nil {
		return db.WorkItem{}, err
	}

	if t.Audit != nil {
		if err := appendAudit(ctx, q, *t.Audit); err != nil {
			return db.WorkItem{}, err
		}
	}

	return row, nil
}

// releaseOrTake deletes the lease t releases (fenced by its acquired_at: ErrLeaseLost when it is
// gone) or takes (whatever its acquired_at; nothing when there is none), recording a lease span
// with t's outcome, which either needs.
func releaseOrTake(ctx context.Context, q *db.Queries, item uuid.UUID, t ports.Transition) error {
	switch {
	case t.Lease == nil && !t.TakeLease:
		return nil
	case t.Outcome == "":
		return fmt.Errorf("item %s: releasing its lease needs an outcome", t.ItemID)
	case t.Lease != nil && t.TakeLease:
		return fmt.Errorf("item %s: a transition releases its lease or takes it, not both", t.ItemID)
	case t.TakeLease:
		if _, err := q.TakeLease(ctx, db.TakeLeaseParams{ItemID: item, EndedAt: t.At, Outcome: string(t.Outcome)}); err != nil {
			return fmt.Errorf("take lease: %w", err)
		}

		return nil
	}

	n, err := q.ReleaseLease(ctx, db.ReleaseLeaseParams{
		ItemID: item, AcquiredAt: t.Lease.AcquiredAt, EndedAt: t.At, Outcome: string(t.Outcome),
	})
	if err != nil {
		return fmt.Errorf("release lease: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("item %s: %w", t.ItemID, ports.ErrLeaseLost)
	}

	return nil
}

func moveParams(item uuid.UUID, t ports.Transition) db.MoveItemParams {
	p := db.MoveItemParams{
		ID: item, Station: t.To.Station, State: string(t.To.State), UpdatedAt: t.At, Lane: optional(t.Lane),
		Paths: t.Paths, BaseCommit: optional(t.BaseCommit), Output: t.Output, OutputStation: t.OutputStation,
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

// AppendNote implements ports.Store: the item is locked first, so a note never lands on an
// item a concurrent transition just closed.
func (s *Store) AppendNote(ctx context.Context, e domain.Event, by domain.Actor) (domain.Event, error) {
	row, err := eventRow(e)
	if err != nil {
		return domain.Event{}, err
	}

	var out domain.Event

	err = s.inTx(ctx, func(q *db.Queries) error {
		it, err := q.LockItem(ctx, row.ItemID)
		if notFound(err) {
			return fmt.Errorf("item %s: %w", e.ItemID, ports.ErrNotFound)
		}

		if err != nil {
			return fmt.Errorf("lock item: %w", err)
		}

		if state := domain.ItemState(it.State); state == domain.StateDone || state == domain.StateCancelled {
			return fmt.Errorf("item %s is %s: %w", e.ItemID, state, ports.ErrConflict)
		}

		ev, err := q.InsertNote(ctx, db.InsertNoteParams(row))
		if err != nil {
			return err
		}

		if out, err = eventFrom(ev); err != nil {
			return err
		}

		text, _ := e.Data["text"].(string)

		return appendAudit(ctx, q, domain.AuditEntry{
			At: e.At, Actor: by, Action: domain.AuditItemNoted, TargetKind: domain.AuditTargetItem, TargetID: string(e.ItemID),
			FactoryID: e.FactoryID, Data: map[string]any{"length": utf8.RuneCountInString(text)},
		})
	})
	if err != nil {
		return domain.Event{}, fmt.Errorf("note on item %s: %w", e.ItemID, err)
	}

	return out, nil
}

// NotesSinceLastRun implements ports.Store.
func (s *Store) NotesSinceLastRun(ctx context.Context, item domain.ItemID) ([]domain.Event, error) {
	uid, err := parseID("item", string(item))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.ItemEvent, error) { return q.NotesSinceLastRun(ctx, uid) })
	if err != nil {
		return nil, fmt.Errorf("notes of item %s: %w", item, err)
	}

	return convertAll(rows, eventFrom)
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

// FactoryActivity implements ports.Store.
func (s *Store) FactoryActivity(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.ActivityEvent, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	kinds := make([]string, 0, len(domain.ActivityKinds))
	for _, k := range domain.ActivityKinds {
		kinds = append(kinds, string(k))
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.FactoryActivityRow, error) {
		return q.FactoryActivity(ctx, db.FactoryActivityParams{FactoryID: fid, Kinds: kinds, MaxEvents: int64(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("activity of factory %s: %w", factory, err)
	}

	return convertAll(rows, func(r db.FactoryActivityRow) (domain.ActivityEvent, error) {
		e, err := eventFrom(r.ItemEvent)
		if err != nil {
			return domain.ActivityEvent{}, err
		}

		return domain.ActivityEvent{Event: e, Number: r.Number, Title: r.Title}, nil
	})
}
