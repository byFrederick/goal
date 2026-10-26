package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ItemTouches implements ports.FlowStats.
func (s *Store) ItemTouches(ctx context.Context, item domain.ItemID) ([]domain.Touch, error) {
	id, err := uuid.Parse(string(item))
	if err != nil {
		if _, ok := domain.OrgFrom(ctx); !ok {
			return nil, domain.ErrNoOrg
		}

		return []domain.Touch{}, nil
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.ItemTouch, error) { return q.ItemsTouches(ctx, []uuid.UUID{id}) })
	if err != nil {
		return nil, fmt.Errorf("touches of item %s: %w", item, err)
	}

	out := make([]domain.Touch, 0, len(rows))
	for _, r := range rows {
		out = append(out, touchFrom(r))
	}

	return out, nil
}

// TouchCounts implements ports.FlowStats.
func (s *Store) TouchCounts(ctx context.Context, items []domain.ItemID) (map[domain.ItemID]int, error) {
	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.TouchCountsRow, error) { return q.TouchCounts(ctx, uuids(items)) })
	if err != nil {
		return nil, fmt.Errorf("touch counts: %w", err)
	}

	out := make(map[domain.ItemID]int, len(rows))
	for _, r := range rows {
		out[domain.ItemID(r.ItemID.String())] = int(r.Touches)
	}

	return out, nil
}

// Touches implements ports.FlowStats.
func (s *Store) Touches(ctx context.Context, q ports.FlowQuery) (domain.TouchStats, error) {
	total, _, err := s.OrgTouches(ctx, []domain.FactoryID{q.FactoryID}, q)

	return total, err
}

// OrgTouches implements ports.FlowStats. Its reads share one snapshot.
func (s *Store) OrgTouches(ctx context.Context, factories []domain.FactoryID, q ports.FlowQuery) (domain.TouchStats, []domain.FactoryTouchStats, error) {
	var (
		items []domain.FinishedItem
		stops map[uuid.UUID]int
	)

	err := s.snapshot(ctx, func(tx *db.Queries) error {
		var err error
		if items, err = finishedTouches(ctx, tx, factories, q, q.Since); err != nil {
			return err
		}

		rows, err := tx.LineStopsByPeople(ctx, db.LineStopsByPeopleParams{Factories: uuids(factories), Since: q.Since, Until: q.Until})
		if err != nil {
			return fmt.Errorf("line stops: %w", err)
		}

		stops = make(map[uuid.UUID]int, len(rows))
		for _, r := range rows {
			stops[r.FactoryID] = int(r.Stops)
		}

		return nil
	})
	if err != nil {
		return domain.TouchStats{}, nil, fmt.Errorf("touches: %w", err)
	}

	byFactory := map[domain.FactoryID][]domain.FinishedItem{}
	for _, it := range items {
		byFactory[it.FactoryID] = append(byFactory[it.FactoryID], it)
	}

	total := domain.TallyTouches(items)
	each := make([]domain.FactoryTouchStats, 0, len(factories))

	for _, f := range factories {
		st := domain.TallyTouches(byFactory[f])
		if id, err := uuid.Parse(string(f)); err == nil {
			st.LineStops = stops[id]
		}

		total.LineStops += st.LineStops
		each = append(each, domain.FactoryTouchStats{FactoryID: f, TouchStats: st})
	}

	return total, each, nil
}

// TouchesByBucket implements ports.FlowStats.
func (s *Store) TouchesByBucket(ctx context.Context, q ports.FlowQuery, d time.Duration, n int) ([]domain.TouchStats, error) {
	start := q.Until.Add(-time.Duration(n) * d)

	var items []domain.FinishedItem

	err := s.snapshot(ctx, func(tx *db.Queries) error {
		var err error

		items, err = finishedTouches(ctx, tx, []domain.FactoryID{q.FactoryID}, q, start)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("touches by bucket of factory %s: %w", q.FactoryID, err)
	}

	return domain.BucketTouches(items, start, d, n), nil
}

// snapshot runs fn in a read-only transaction whose reads share one snapshot.
func (s *Store) snapshot(ctx context.Context, fn func(q *db.Queries) error) error {
	return s.orgTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

// finishedTouches reads the items of factories of q.Type (and q.VersionID) done in
// [since, q.Until) with their touches.
func finishedTouches(ctx context.Context, tx *db.Queries, factories []domain.FactoryID, q ports.FlowQuery, since time.Time) ([]domain.FinishedItem, error) {
	var version *uuid.UUID

	if q.VersionID != "" {
		// A version id that is no UUID names no version: uuid.Nil matches none.
		v, _ := uuid.Parse(string(q.VersionID))
		version = &v
	}

	done, err := tx.FinishedItems(ctx, db.FinishedItemsParams{
		Factories: uuids(factories), Since: since, Until: q.Until, Type: q.Type, Version: version,
	})
	if err != nil {
		return nil, fmt.Errorf("finished items: %w", err)
	}

	ids := make([]uuid.UUID, 0, len(done))
	for _, d := range done {
		ids = append(ids, d.ItemID)
	}

	rows, err := tx.ItemsTouches(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("touches of finished items: %w", err)
	}

	touches := map[uuid.UUID][]domain.Touch{}
	for _, r := range rows {
		touches[r.ItemID] = append(touches[r.ItemID], touchFrom(r))
	}

	out := make([]domain.FinishedItem, 0, len(done))
	for _, d := range done {
		it := domain.FinishedItem{
			FactoryID: domain.FactoryID(d.FactoryID.String()), ItemID: domain.ItemID(d.ItemID.String()), DoneAt: d.DoneAt.UTC(),
			Touches: touches[d.ItemID],
		}

		if d.VersionID != nil {
			it.VersionID = domain.DefinitionVersionID(d.VersionID.String())
		}

		out = append(out, it)
	}

	return out, nil
}

// LabelTouch implements ports.Store.
func (s *Store) LabelTouch(ctx context.Context, event int64, reason domain.TouchReason, by domain.Actor) error {
	if !reason.Valid() {
		return fmt.Errorf("touch reason %q: %w", reason, domain.ErrInvalid)
	}

	err := s.inTx(ctx, func(q *db.Queries) error {
		ev, err := q.TouchEvent(ctx, event)
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return fmt.Errorf("read event: %w", err)
		}

		n, err := q.LabelTouch(ctx, db.LabelTouchParams{Reason: string(reason), ReasonBy: by.Ref(), ItemID: ev.ItemID, EventID: event})
		if err != nil {
			return fmt.Errorf("set reason: %w", err)
		}

		if n == 0 {
			return ports.ErrNotFound
		}

		return appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditTouchLabelled, TargetKind: domain.AuditTargetItem, TargetID: ev.ItemID.String(),
			FactoryID: domain.FactoryID(ev.FactoryID.String()), Data: map[string]any{"event_id": event, "reason": string(reason)},
		})
	})
	if err != nil {
		return fmt.Errorf("label touch %d: %w", event, err)
	}

	return nil
}

func touchFrom(r db.ItemTouch) domain.Touch {
	return domain.Touch{
		EventID: r.EventID, FactoryID: domain.FactoryID(r.FactoryID.String()), ItemID: domain.ItemID(r.ItemID.String()),
		Kind: domain.EventKind(r.Kind), Station: deref(r.Station), At: r.At.UTC(), Actor: r.Actor, ActorName: r.ActorName,
		Planned: r.Planned, Reason: domain.TouchReason(deref(r.Reason)), ReasonBy: deref(r.ReasonBy),
	}
}

// uuids parses ids, leaving out those that are no UUID: they name nothing.
func uuids[T ~string](ids []T) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))

	for _, id := range ids {
		if u, err := uuid.Parse(string(id)); err == nil {
			out = append(out, u)
		}
	}

	return out
}
