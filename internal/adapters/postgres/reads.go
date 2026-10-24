package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ItemByNumber implements ports.Store.
func (s *Store) ItemByNumber(ctx context.Context, factory domain.FactoryID, number int64) (domain.Item, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.Item{}, err
	}

	row, err := fetch(ctx, s.read, func(q *db.Queries) (db.WorkItem, error) {
		return q.GetItemByNumber(ctx, db.GetItemByNumberParams{FactoryID: fid, Number: number})
	})
	if notFound(err) {
		return domain.Item{}, fmt.Errorf("item %d of factory %s: %w", number, factory, ports.ErrNotFound)
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("item %d of factory %s: %w", number, factory, err)
	}

	return itemFrom(row)
}

// ListItems implements ports.Store.
func (s *Store) ListItems(ctx context.Context, q ports.ItemQuery) ([]domain.Item, error) {
	fid, err := parseID("factory", string(q.FactoryID))
	if err != nil {
		return nil, err
	}

	states := make([]string, 0, len(q.States))
	for _, st := range q.States {
		states = append(states, string(st))
	}

	rows, err := fetch(ctx, s.read, func(dq *db.Queries) ([]db.WorkItem, error) {
		return dq.ListItems(ctx, db.ListItemsParams{
			FactoryID: fid, States: states, Station: q.Station, Before: q.Before, MaxItems: int64(q.Limit),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list items of factory %s: %w", q.FactoryID, err)
	}

	return convertAll(rows, itemFrom)
}

// StationLoads implements ports.Store.
func (s *Store) StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.StationLoadsRow, error) {
		return q.StationLoads(ctx, fid)
	})
	if err != nil {
		return nil, fmt.Errorf("station loads of factory %s: %w", factory, err)
	}

	out := make([]domain.StationLoad, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.StationLoad{Station: r.Station, Waiting: int(r.Waiting), InProgress: int(r.InProgress)})
	}

	return out, nil
}

// FlowMetrics implements ports.Store. Its reads share one snapshot.
func (s *Store) FlowMetrics(ctx context.Context, factory domain.FactoryID, since, until time.Time) (domain.FlowMetrics, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.FlowMetrics{}, err
	}

	var m domain.FlowMetrics

	err = s.orgTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(q *db.Queries) error {
		days, err := q.DonePerDay(ctx, db.DonePerDayParams{FactoryID: fid, Since: since, Until: until})
		if err != nil {
			return fmt.Errorf("done per day: %w", err)
		}

		for _, d := range days {
			m.DonePerDay = append(m.DonePerDay, domain.DayCount{Day: d.Day.UTC(), Count: int(d.Done)})
		}

		stats, err := q.DoneStats(ctx, db.DoneStatsParams{FactoryID: fid, Since: since, Until: until})
		if err != nil {
			return fmt.Errorf("done stats: %w", err)
		}

		m.ItemsDone, m.Reworked = int(stats.ItemsDone), int(stats.Reworked)
		m.LeadTimeP50, m.LeadTimeP85 = seconds(stats.P50Seconds), seconds(stats.P85Seconds)

		wip, err := q.OpenItemCount(ctx, fid)
		if err != nil {
			return fmt.Errorf("open items: %w", err)
		}

		m.WIP = int(wip)

		if m.SpendUSD, err = q.Spend(ctx, db.SpendParams{FactoryID: fid, Since: since, Until: until}); err != nil {
			return fmt.Errorf("spend: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.FlowMetrics{}, fmt.Errorf("flow metrics of factory %s: %w", factory, err)
	}

	return m, nil
}

func seconds(s float64) time.Duration { return time.Duration(math.Round(s * float64(time.Second))) }

// Attention implements ports.Store. Its reads share one snapshot.
func (s *Store) Attention(ctx context.Context) ([]domain.Attention, error) {
	var out []domain.Attention

	err := s.orgTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(q *db.Queries) error {
		rows, err := q.FactoryAttention(ctx)
		if err != nil {
			return fmt.Errorf("factories: %w", err)
		}

		byType, err := q.WaitingByType(ctx)
		if err != nil {
			return fmt.Errorf("waiting by type: %w", err)
		}

		types := map[uuid.UUID][]domain.TypeCount{}
		for _, r := range byType {
			types[r.FactoryID] = append(types[r.FactoryID], domain.TypeCount{Type: r.Type, Count: int(r.Waiting)})
		}

		out = make([]domain.Attention, 0, len(rows))
		for _, r := range rows {
			a := domain.Attention{
				Factory: domain.FactoryID(r.FactoryID.String()), WaitingHuman: int(r.WaitingHuman),
				WaitingByType: types[r.FactoryID], Blocked: int(r.Blocked),
			}

			if r.AndonPulledAt != nil {
				a.Andon = &domain.AndonPull{Reason: deref(r.AndonReason), PulledBy: deref(r.AndonPulledBy), PulledAt: r.AndonPulledAt.UTC()}
			}

			out = append(out, a)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("attention: %w", err)
	}

	return out, nil
}

// WaitingHuman implements ports.Store. A factory id that is not a UUID names no factory.
func (s *Store) WaitingHuman(ctx context.Context, factories []domain.FactoryID, limit int) ([]domain.Item, error) {
	ids := make([]uuid.UUID, 0, len(factories))

	for _, f := range factories {
		if id, err := uuid.Parse(string(f)); err == nil {
			ids = append(ids, id)
		}
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.WorkItem, error) {
		return q.WaitingHumanItems(ctx, db.WaitingHumanItemsParams{Factories: ids, MaxItems: int64(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("items waiting for a person: %w", err)
	}

	return convertAll(rows, itemFrom)
}
