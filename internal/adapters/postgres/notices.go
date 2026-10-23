package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// noticeChannel is where the item_events trigger notifies (migrations 00001 and 00003).
const noticeChannel = "goal_events"

// Reconnect backoff after the listening connection is lost.
const (
	firstRetry = 100 * time.Millisecond
	maxRetry   = 10 * time.Second
)

var _ ports.ItemNotices = (*Notices)(nil)

// Notices implements ports.ItemNotices with LISTEN on a connection of its own, outside the
// store's pool, so a long-lived listener never holds a pooled connection.
type Notices struct {
	cfg *pgx.ConnConfig
	log *slog.Logger
}

// NewNotices listens with cfg, usually the store pool's connection config
// (pool.Config().ConnConfig). log may be nil.
func NewNotices(cfg *pgx.ConnConfig, log *slog.Logger) *Notices {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Notices{cfg: cfg, log: log}
}

// Listen implements ports.ItemNotices. Every time it starts listening, the first time
// included, it delivers a Resync notice before any event, so a caller knows from when on it
// sees every event.
func (n *Notices) Listen(ctx context.Context, deliver func(ports.ItemNotice)) error {
	retry := firstRetry

	for ctx.Err() == nil {
		if n.listen(ctx, deliver, retry) {
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
func (n *Notices) listen(ctx context.Context, deliver func(ports.ItemNotice), retry time.Duration) bool {
	listened, err := n.serve(ctx, deliver)
	if ctx.Err() == nil {
		if listened {
			retry = firstRetry
		}

		n.log.WarnContext(ctx, "item notices: listening connection lost; reconnecting",
			slog.Duration("in", retry), slog.Any("error", err))
	}

	return listened
}

func (n *Notices) serve(ctx context.Context, deliver func(ports.ItemNotice)) (bool, error) {
	conn, err := pgx.ConnectConfig(ctx, n.cfg)
	if err != nil {
		return false, fmt.Errorf("connect: %w", err)
	}

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		_ = conn.Close(closeCtx)
	}()

	if _, err := conn.Exec(ctx, "LISTEN "+noticeChannel); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}

	deliver(ports.ItemNotice{Resync: true})

	for {
		msg, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
		}

		notice, err := noticeFrom(msg.Payload)
		if err != nil {
			n.log.WarnContext(ctx, "item notices: unreadable notification", slog.Any("error", err))
			continue
		}

		deliver(notice)
	}
}

// payload is what notify_item_event() sends.
type payload struct {
	ID        int64     `json:"id"`
	FactoryID string    `json:"factory_id"`
	ItemID    string    `json:"item_id"`
	Number    int64     `json:"number"`
	Kind      string    `json:"kind"`
	Station   *string   `json:"station"`
	State     string    `json:"state"`
	At        time.Time `json:"at"`
}

func noticeFrom(raw string) (ports.ItemNotice, error) {
	var p payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ports.ItemNotice{}, fmt.Errorf("decode %q: %w", raw, err)
	}

	if p.ItemID == "" || p.FactoryID == "" {
		return ports.ItemNotice{}, errors.New("notification without an item or factory")
	}

	return ports.ItemNotice{
		EventID: p.ID, FactoryID: domain.FactoryID(p.FactoryID), ItemID: domain.ItemID(p.ItemID), Number: p.Number,
		Kind: domain.EventKind(p.Kind), Station: deref(p.Station), State: domain.ItemState(p.State), At: p.At.UTC(),
	}, nil
}
