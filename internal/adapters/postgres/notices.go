package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
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

var _ ports.ItemNotices = (*Notices)(nil)

// Notices implements ports.ItemNotices with LISTEN on a connection of its own, outside the
// store's pool, so a long-lived listener never holds a pooled connection. Every notification is
// read back through the row-level security of the organization it names before it is delivered
// (review 2.2 M4): an item event from its item_events row, an organization event by the reader of
// its name (orgEventReaders); one with nothing to read back is dropped.
type Notices struct {
	l listener[ports.ItemNotice]
}

// NewNotices listens with the connection config of s's pool and reads notices back through s.
// log may be nil.
func NewNotices(s *Store, log *slog.Logger) *Notices {
	return &Notices{l: listener[ports.ItemNotice]{
		cfg: s.pool.Config().ConnConfig, log: discardIfNil(log), channels: []string{noticeChannel, orgEventChannel},
		name: "item notices", resync: ports.ItemNotice{Resync: true}, decode: itemNoticeFrom, readBack: s.readBackItemNotice,
	}}
}

// Listen implements ports.ItemNotices. Every time it starts listening, the first time
// included, it delivers a Resync notice before any event, so a caller knows from when on it
// sees every event.
func (n *Notices) Listen(ctx context.Context, deliver func(ports.ItemNotice)) error {
	return n.l.Listen(ctx, deliver)
}

// itemNoticeFrom reads a notification of either channel.
func itemNoticeFrom(channel, raw string) (ports.ItemNotice, error) {
	if channel == orgEventChannel {
		return orgEventFrom(raw)
	}

	return noticeFrom(raw)
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

	if p.OrgID == "" || !domain.IsEventName(p.Event) {
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

// readBackItemNotice returns the notice the rows of n's organization give for n: an item event's
// fields from its row (the payload only names the event), an organization event from the reader
// of its name. It reports false when there is nothing to read: no such row in that organization,
// or an organization event goald has no reader for.
func (s *Store) readBackItemNotice(ctx context.Context, n ports.ItemNotice) (ports.ItemNotice, bool, error) {
	if n.Org != nil {
		read, ok := orgEventReaders[n.Org.Name]
		if !ok {
			return ports.ItemNotice{}, false, nil
		}

		return readBack(ctx, s, n.OrgID, func(q *db.Queries) (ports.ItemNotice, error) { return read(ctx, q, n) })
	}

	return readBack(ctx, s, n.OrgID, func(q *db.Queries) (ports.ItemNotice, error) {
		r, err := q.NoticeEvent(ctx, n.EventID)
		if err != nil {
			return ports.ItemNotice{}, err
		}

		return ports.ItemNotice{
			EventID: n.EventID, OrgID: n.OrgID, FactoryID: domain.FactoryID(r.FactoryID.String()), ItemID: domain.ItemID(r.ItemID.String()),
			Number: r.Number, Kind: domain.EventKind(r.Kind), Station: deref(r.Station), State: domain.ItemState(r.State), At: r.At.UTC(),
		}, nil
	})
}

// readBack runs read in a read-only transaction bound to org and reports false, with no error,
// when the row it reads is not there for org (or org names no organization).
func readBack[N any](ctx context.Context, s *Store, org domain.OrgID, read func(q *db.Queries) (N, error)) (N, bool, error) {
	var zero N

	out, err := fetch(domain.WithOrg(ctx, org), s.read, read)
	if notFound(err) || errors.Is(err, ports.ErrNotFound) || errors.Is(err, errNothingToReadBack) {
		return zero, false, nil
	}

	if err != nil {
		return zero, false, err
	}

	return out, true, nil
}

// errNothingToReadBack is a reader's answer to a notice whose rows do not bear it out.
var errNothingToReadBack = errors.New("the notice names nothing")

// orgEventReaders read each organization event goald sends back from its rows, by event name.
// An event without a reader is dropped; a phase that adds one adds its reader.
var orgEventReaders = map[string]func(context.Context, *db.Queries, ports.ItemNotice) (ports.ItemNotice, error){
	"factory": readFactoryEvent,
}

// andonEvent is a factory event's data (migration 00032): an andon pull or clear.
type andonEvent struct {
	OrgID     string    `json:"org_id"`
	FactoryID string    `json:"factory_id"`
	Kind      string    `json:"kind"`
	AndonID   int64     `json:"andon_id"`
	At        time.Time `json:"at"`
}

// readFactoryEvent reads an andon notice back from its pull: the pull must be the factory's, and
// cleared for a clear; the time is the pull's or the clear's.
func readFactoryEvent(ctx context.Context, q *db.Queries, n ports.ItemNotice) (ports.ItemNotice, error) {
	var claim andonEvent
	if err := json.Unmarshal(n.Org.Data, &claim); err != nil {
		return ports.ItemNotice{}, errNothingToReadBack
	}

	r, err := q.NoticeAndon(ctx, claim.AndonID)
	if err != nil {
		return ports.ItemNotice{}, err
	}

	e := andonEvent{OrgID: string(n.OrgID), FactoryID: r.FactoryID.String(), Kind: claim.Kind, AndonID: claim.AndonID}

	switch {
	case e.FactoryID != string(n.FactoryID):
		return ports.ItemNotice{}, errNothingToReadBack
	case claim.Kind == "andon_pulled":
		e.At = r.PulledAt.UTC()
	case claim.Kind == "andon_cleared" && r.ClearedAt != nil:
		e.At = r.ClearedAt.UTC()
	default:
		return ports.ItemNotice{}, errNothingToReadBack
	}

	data, err := json.Marshal(e)
	if err != nil {
		return ports.ItemNotice{}, fmt.Errorf("encode factory event: %w", err)
	}

	factory := domain.FactoryID(e.FactoryID)

	return ports.ItemNotice{OrgID: n.OrgID, FactoryID: factory, Org: &ports.OrgEvent{Name: n.Org.Name, FactoryID: factory, Data: data}}, nil
}
