package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// Reconnect backoff after a listening connection is lost.
const (
	firstRetry = 100 * time.Millisecond
	maxRetry   = 10 * time.Second
)

// listener LISTENs on NOTIFY channels with a connection of its own, outside the store's pool,
// so a long-lived listener never holds a pooled connection. It decodes each payload into a
// notice N and reconnects with backoff when the connection is lost. Notices and
// DefinitionNotices are listeners.
type listener[N any] struct {
	cfg      *pgx.ConnConfig
	log      *slog.Logger
	channels []string
	// name prefixes log messages ("item notices").
	name string
	// resync is the notice delivered each time listening starts.
	resync N
	// decode reads the payload of a notification on channel.
	decode func(channel, payload string) (N, error)
}

// Listen delivers notices until ctx ends, then returns nil. Every time it starts listening,
// the first time included, it delivers the resync notice before any other, so a caller knows
// from when on it sees every notification.
func (l *listener[N]) Listen(ctx context.Context, deliver func(N)) error {
	retry := firstRetry

	for ctx.Err() == nil {
		if l.listen(ctx, deliver, retry) {
			retry = firstRetry
		}

		t := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			t.Stop()
		case <-t.C:
		}

		retry = min(2*retry, maxRetry)
	}

	return nil
}

// listen connects, listens and delivers notices until the connection fails or ctx ends. It
// reports whether it got as far as listening, and logs why it stopped unless ctx ended.
func (l *listener[N]) listen(ctx context.Context, deliver func(N), retry time.Duration) bool {
	listened, err := l.serve(ctx, deliver)
	if ctx.Err() == nil {
		if listened {
			retry = firstRetry
		}

		l.log.WarnContext(ctx, l.name+": listening connection lost; reconnecting",
			slog.Duration("in", retry), slog.Any("error", err))
	}

	return listened
}

func (l *listener[N]) serve(ctx context.Context, deliver func(N)) (bool, error) {
	conn, err := pgx.ConnectConfig(ctx, l.cfg)
	if err != nil {
		return false, fmt.Errorf("connect: %w", err)
	}

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		_ = conn.Close(closeCtx)
	}()

	for _, channel := range l.channels {
		if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
			return false, fmt.Errorf("listen %s: %w", channel, err)
		}
	}

	deliver(l.resync)

	for {
		msg, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
		}

		notice, err := l.decode(msg.Channel, msg.Payload)
		if err != nil {
			l.log.WarnContext(ctx, l.name+": unreadable notification", slog.Any("error", err))
			continue
		}

		deliver(notice)
	}
}

func discardIfNil(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.New(slog.DiscardHandler)
	}

	return log
}
