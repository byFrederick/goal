package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

// PoolStatsInterval is how often goald logs its pool's "db pool" line.
const PoolStatsInterval = time.Minute

// PoolStat is one snapshot of a store's connections: its pool's statistics, as pgxpool's Stat
// counts them, and the connections it holds outside the pool (OutsideConns).
type PoolStat struct {
	AcquireCount, EmptyAcquireCount, CanceledAcquireCount int64
	AcquireDuration                                       time.Duration
	AcquiredConns, IdleConns, TotalConns, MaxConns        int32
	OutsideConns                                          int
}

// PoolStats reads s's connection statistics, for LogPoolStats.
func (s *Store) PoolStats() PoolStat {
	p := s.pool.Stat()

	return PoolStat{
		AcquireCount: p.AcquireCount(), EmptyAcquireCount: p.EmptyAcquireCount(), CanceledAcquireCount: p.CanceledAcquireCount(),
		AcquireDuration: p.AcquireDuration(),
		AcquiredConns:   p.AcquiredConns(), IdleConns: p.IdleConns(), TotalConns: p.TotalConns(), MaxConns: p.MaxConns(),
		OutsideConns: s.OutsideConns(),
	}
}

// LogPoolStats logs stat's "db pool" line every interval on clock's ticker until ctx ends: the
// acquires so far (acquire_count; empty_acquire_count, those that waited because no connection
// was idle; canceled_acquire_count, those whose context ended while waiting), the total time
// acquires took (acquire_duration_ms), the pool's connections now in use, idle, open and
// allowed, and the listening and lock connections open outside the pool (outside_conns, 0 to 2).
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
				slog.Int("outside_conns", s.OutsideConns),
			)
		}
	}
}
