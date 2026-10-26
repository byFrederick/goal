package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

// The sign-in clean-up (spec 3 §12): expired sessions, verification values, device codes, idle
// rate-limit counters and old security-log events go once a day. Every goald runs it; the
// database's advisory lock lets one at a time do the work.
const (
	authCleanUpJob   = "goal.auth.cleanup"
	authCleanUpEvery = 24 * time.Hour
)

// cleanUpAuth cleans up at once and then each time the interval passes, until ctx ends, logging
// what it deleted; a failure is logged and retried on the next round.
func cleanUpAuth(ctx context.Context, log *slog.Logger, platform ports.Platform, clock ports.Clock, every time.Duration) {
	t := clock.NewTicker(every)
	defer t.Stop()

	for {
		got, err := platform.CleanUpAuth(ctx, clock.Now())

		switch {
		case err != nil:
			if ctx.Err() == nil {
				log.WarnContext(ctx, "sign-in clean-up failed", slog.String("job", authCleanUpJob), slog.Any("error", err))
			}
		case got.Skipped:
			log.DebugContext(ctx, "sign-in clean-up skipped: another goald holds it", slog.String("job", authCleanUpJob))
		default:
			log.InfoContext(ctx, "sign-in cleaned up", slog.String("job", authCleanUpJob),
				slog.Int64("sessions", got.Sessions), slog.Int64("verifications", got.Verifications),
				slog.Int64("device_codes", got.DeviceCodes), slog.Int64("rate_limits", got.RateLimits),
				slog.Int64("auth_events", got.AuthEvents))
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}
