package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// FlowMetrics implements ports.FlowStats. Its reads share one snapshot.
func (s *Store) FlowMetrics(ctx context.Context, q ports.FlowQuery, longWaitsAt string) (domain.FlowMetrics, error) {
	fid, err := parseID("factory", string(q.FactoryID))
	if err != nil {
		return domain.FlowMetrics{}, err
	}

	var m domain.FlowMetrics

	err = s.orgTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx *db.Queries) error {
		days, err := tx.FlowDonePerDay(ctx, db.FlowDonePerDayParams{FactoryID: fid, Since: q.Since, Until: q.Until, Type: q.Type})
		if err != nil {
			return fmt.Errorf("done per day: %w", err)
		}

		for _, d := range days {
			m.DonePerDay = append(m.DonePerDay, domain.DayCount{Day: d.Day.UTC(), Count: int(d.Done)})
		}

		stats, err := tx.FlowDoneStats(ctx, db.FlowDoneStatsParams{FactoryID: fid, Since: q.Since, Until: q.Until, Type: q.Type})
		if err != nil {
			return fmt.Errorf("done stats: %w", err)
		}

		m.ItemsDone, m.Reworked = int(stats.ItemsDone), int(stats.Reworked)
		m.LeadTimeP50, m.LeadTimeP85 = seconds(stats.P50Seconds), seconds(stats.P85Seconds)

		wip, err := tx.FlowOpenItems(ctx, db.FlowOpenItemsParams{FactoryID: fid, Type: q.Type})
		if err != nil {
			return fmt.Errorf("open items: %w", err)
		}

		m.WIP = int(wip)

		if m.SpendUSD, err = tx.FlowSpend(ctx, db.FlowSpendParams{FactoryID: fid, Since: q.Since, Until: q.Until, Type: q.Type}); err != nil {
			return fmt.Errorf("spend: %w", err)
		}

		if longWaitsAt == "" {
			return nil
		}

		long, err := tx.FlowLongWaits(ctx, db.FlowLongWaitsParams{
			FactoryID: fid, Station: longWaitsAt, Since: q.Since, Until: q.Until,
			ThresholdSeconds: domain.LongWait.Seconds(), Type: q.Type,
		})
		if err != nil {
			return fmt.Errorf("long waits: %w", err)
		}

		m.LongWaits = int(long)

		return nil
	})
	if err != nil {
		return domain.FlowMetrics{}, fmt.Errorf("flow metrics of factory %s: %w", q.FactoryID, err)
	}

	return m, nil
}

// ItemTimes implements ports.FlowStats.
func (s *Store) ItemTimes(ctx context.Context, q ports.FlowQuery) ([]domain.ItemTime, error) {
	fid, err := parseID("factory", string(q.FactoryID))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(tx *db.Queries) ([]db.FlowItemTimesRow, error) {
		return tx.FlowItemTimes(ctx, db.FlowItemTimesParams{FactoryID: fid, Since: q.Since, Until: q.Until, Type: q.Type})
	})
	if err != nil {
		return nil, fmt.Errorf("item times of factory %s: %w", q.FactoryID, err)
	}

	out := []domain.ItemTime{}

	for _, r := range rows {
		id := domain.ItemID(r.ItemID.String())
		if len(out) == 0 || out[len(out)-1].Item != id {
			out = append(out, domain.ItemTime{Item: id, LeadTime: seconds(r.LeadSeconds), Times: map[domain.Component]time.Duration{}})
		}

		if r.Station == nil || r.State == nil {
			continue // an item without closed spans
		}

		c := domain.ComponentOf(domain.Span{Station: *r.Station, State: domain.ItemState(*r.State), ReworkBy: deref(r.ReworkBy)})
		out[len(out)-1].Times[c] += seconds(r.Seconds)
	}

	return out, nil
}

// DoneByBucket implements ports.FlowStats.
func (s *Store) DoneByBucket(ctx context.Context, q ports.FlowQuery, d time.Duration, n int) ([]int, error) {
	fid, err := parseID("factory", string(q.FactoryID))
	if err != nil {
		return nil, err
	}

	start := q.Until.Add(-time.Duration(n) * d)

	rows, err := fetch(ctx, s.read, func(tx *db.Queries) ([]db.FlowDoneByBucketRow, error) {
		return tx.FlowDoneByBucket(ctx, db.FlowDoneByBucketParams{
			FactoryID: fid, Start: start, Until: q.Until, BucketSeconds: d.Seconds(), Type: q.Type,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("done by bucket of factory %s: %w", q.FactoryID, err)
	}

	out := make([]int, n)
	for _, r := range rows {
		if b := int(r.Bucket); b >= 0 && b < n {
			out[b] = int(r.Done)
		}
	}

	return out, nil
}
