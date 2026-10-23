package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

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

	row, err := s.q.GetItemByNumber(ctx, db.GetItemByNumberParams{FactoryID: fid, Number: number})
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

	rows, err := s.q.ListItems(ctx, db.ListItemsParams{
		FactoryID: fid, States: states, Station: q.Station, Before: q.Before, MaxItems: int64(q.Limit),
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

	rows, err := s.q.StationLoads(ctx, fid)
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

	err = pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

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
