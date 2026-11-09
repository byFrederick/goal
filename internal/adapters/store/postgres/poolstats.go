package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/ports"
)

// PoolStatsInterval is how often goald logs its pool's "db pool" line.
const PoolStatsInterval = time.Minute

// PoolStat is one snapshot of a pool's statistics, as pgxpool's Stat counts them.
type PoolStat struct {
	AcquireCount, EmptyAcquireCount, CanceledAcquireCount int64
	AcquireDuration                                       time.Duration
	AcquiredConns, IdleConns, TotalConns, MaxConns        int32
}

// PoolStats reads pool's statistics, for LogPoolStats.
func PoolStats(pool *pgxpool.Pool) func() PoolStat {
	return func() PoolStat {
		s := pool.Stat()

		return PoolStat{
			AcquireCount: s.AcquireCount(), EmptyAcquireCount: s.EmptyAcquireCount(), CanceledAcquireCount: s.CanceledAcquireCount(),
			AcquireDuration: s.AcquireDuration(),
			AcquiredConns:   s.AcquiredConns(), IdleConns: s.IdleConns(), TotalConns: s.TotalConns(), MaxConns: s.MaxConns(),
		}
	}
}

// LogPoolStats logs stat's "db pool" line every interval on clock's ticker until ctx ends: the
// acquires so far (acquire_count; empty_acquire_count, those that waited because no connection
// was idle; canceled_acquire_count, those whose context ended while waiting), the total time
// acquires took (acquire_duration_ms) and the connections now in use, idle, open and allowed.
// Counts are cumulative since the pool opened, so starvation shows as empty_acquire_count and
// acquire_duration_ms rising between lines while acquired_conns sits at max_conns.
func LogPoolStats(ctx context.Context, stat func() PoolStat, clock ports.Clock, interval time.Duration, log *slog.Logger) {
	t := clock.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C():
			s := stat()
			log.LogAttrs(ctx, slog.LevelInfo, "db pool",
				slog.Int64("acquire_count", s.AcquireCount),
				slog.Int64("empty_acquire_count", s.EmptyAcquireCount),
				slog.Int64("canceled_acquire_count", s.CanceledAcquireCount),
				slog.Int64("acquire_duration_ms", s.AcquireDuration.Milliseconds()),
				slog.Int("acquired_conns", int(s.AcquiredConns)),
				slog.Int("idle_conns", int(s.IdleConns)),
				slog.Int("total_conns", int(s.TotalConns)),
				slog.Int("max_conns", int(s.MaxConns)),
			)
		}
	}
}
