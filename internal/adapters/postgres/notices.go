package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Channels the listener follows: the item_events trigger notifies goal_events (migrations
// 00001, 00003 and 00006); goal_org_events carries an organization's other stream events,
// {org_id, event, factory_id?, data}, notified in the transaction that causes them.
const (
	noticeChannel   = "goal_events"
	orgEventChannel = "goal_org_events"
)

// eventName is what an organization event may be called on the stream.
var eventName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

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

	for _, channel := range []string{noticeChannel, orgEventChannel} {
		if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
			return false, fmt.Errorf("listen %s: %w", channel, err)
		}
	}

	deliver(ports.ItemNotice{Resync: true})

	for {
		msg, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
		}

		read := noticeFrom
		if msg.Channel == orgEventChannel {
			read = orgEventFrom
		}

		notice, err := read(msg.Payload)
		if err != nil {
			n.log.WarnContext(ctx, "item notices: unreadable notification", slog.Any("error", err))
			continue
		}

		deliver(notice)
	}
}

// payload is what notify_item_event() sends.
type payload struct {
	OrgID     string    `json:"org_id"`
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

	if p.ItemID == "" || p.FactoryID == "" || p.OrgID == "" {
		return ports.ItemNotice{}, errors.New("notification without an organization, item or factory")
	}

	return ports.ItemNotice{
		EventID: p.ID, OrgID: domain.OrgID(p.OrgID), FactoryID: domain.FactoryID(p.FactoryID), ItemID: domain.ItemID(p.ItemID), Number: p.Number,
		Kind: domain.EventKind(p.Kind), Station: deref(p.Station), State: domain.ItemState(p.State), At: p.At.UTC(),
	}, nil
}

// orgPayload is what goal_org_events carries.
type orgPayload struct {
	OrgID     string          `json:"org_id"`
	Event     string          `json:"event"`
	FactoryID string          `json:"factory_id"`
	Data      json.RawMessage `json:"data"`
}

func orgEventFrom(raw string) (ports.ItemNotice, error) {
	var p orgPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ports.ItemNotice{}, fmt.Errorf("decode %q: %w", raw, err)
	}

	if p.OrgID == "" || !eventName.MatchString(p.Event) {
		return ports.ItemNotice{}, fmt.Errorf("organization event %q without an organization or a valid name", p.Event)
	}

	data := p.Data
	if len(data) == 0 || string(data) == "null" {
		data = json.RawMessage("{}")
	}

	if data[0] != '{' {
		return ports.ItemNotice{}, fmt.Errorf("organization event %s: data is not a JSON object", p.Event)
	}

	return ports.ItemNotice{
		OrgID: domain.OrgID(p.OrgID), FactoryID: domain.FactoryID(p.FactoryID),
		Org: &ports.OrgEvent{Name: p.Event, FactoryID: domain.FactoryID(p.FactoryID), Data: data},
	}, nil
}
