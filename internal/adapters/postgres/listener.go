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
	// readBack replaces a decoded notice with what the database says, read through the row-level
	// security of the organization it names: any role that may connect can NOTIFY, so a payload
	// is a claim and never data. It reports false for a notice to drop (nothing to read back).
	readBack func(ctx context.Context, n N) (N, bool, error)
}

// Listen delivers notices until ctx ends, then returns nil. Every time it starts listening,
// the first time included, it delivers the resync notice before any other, so a caller knows
// from when on it sees every notification.
func (l *listener[N]) Listen(ctx context.Context, deliver func(N)) error {
	retry := firstRetry

	for ctx.Err() == nil {
		// A connection that got as far as listening starts the backoff over.
		listened, err := l.serve(ctx, deliver)
		if listened {
			retry = firstRetry
		}

		if ctx.Err() == nil {
			l.log.WarnContext(ctx, l.name+": listening connection lost; reconnecting",
				slog.Duration("in", retry), slog.Any("error", err))
			l.sleep(ctx, retry)
		}

		retry = min(2*retry, maxRetry)
	}

	return nil
}

// sleep waits d or until ctx ends.
func (l *listener[N]) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
	case <-t.C:
	}
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

		notice, ok, err := l.readBack(ctx, notice)
		switch {
		case err != nil && ctx.Err() != nil:
			return true, err
		case err != nil:
			// The notice is lost, so listeners re-read everything instead.
			l.log.WarnContext(ctx, l.name+": notification not read back; resyncing", slog.String("channel", msg.Channel), slog.Any("error", err))
			deliver(l.resync)
		case !ok:
			l.log.DebugContext(ctx, l.name+": notification names nothing; dropped", slog.String("channel", msg.Channel))
		default:
			deliver(notice)
		}
	}
}

func discardIfNil(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.New(slog.DiscardHandler)
	}

	return log
}
