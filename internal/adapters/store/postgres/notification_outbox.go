package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.NotificationOutbox = (*Store)(nil)

// notifyRoles are the factory roles that make a member a recipient of each event, besides the
// organization's owners and admins, who count as every role (ADR 0023, decision 2).
var notifyRoles = map[domain.NotificationEvent][]string{
	domain.NotifyApproval: {string(domain.FactoryApprover), string(domain.FactoryEditor)},
	domain.NotifyQuestion: {string(domain.FactoryApprover), string(domain.FactoryEditor)},
	domain.NotifyAndon:    {string(domain.FactoryEditor)},
	domain.NotifyBudget:   {},
}

// Keys of the outbox payloads.
const (
	payloadFactory = "factory_id"
	payloadKind    = "kind"
)

// outboxEvent is one event to queue in the notification outbox.
type outboxEvent struct {
	id      string // event/<item event id>, andon/<pull id> or budget/<factory>/<crossing>
	event   domain.NotificationEvent
	factory uuid.UUID
	actor   string // the Actor ref of who caused it, who hears nothing of it; "" for nobody
	at      time.Time
	payload map[string]any // ids and the event's facts, never an address or a secret
}

// queueNotifications queues n for its recipients in the transaction of q: one pending row per
// recipient and per channel that is on, and nothing already queued for the same event.
func queueNotifications(ctx context.Context, q *db.Queries, n outboxEvent) error {
	raw, err := json.Marshal(n.payload)
	if err != nil {
		return fmt.Errorf("notification %s payload: %w", n.id, err)
	}

	def := domain.DefaultNotificationPreferences().Events[n.event]

	if _, err := q.QueueNotifications(ctx, db.QueueNotificationsParams{
		EventID: n.id, Event: string(n.event), Payload: raw, At: n.at,
		DefaultEmail: def.Email, DefaultSlack: def.Slack,
		FactoryID: n.factory, FactoryRoles: notifyRoles[n.event], Actor: n.actor,
	}); err != nil {
		return fmt.Errorf("queue notification %s: %w", n.id, err)
	}

	return nil
}

// itemNotifications queues the approval and question notifications of events, which appendEvents
// has just inserted: an item's question_asked event is a question; its waiting_human event,
// unless the same batch asks a question, an approval. The actor excluded is the decider an
// event of the same item names (data.actor), when there is one.
func itemNotifications(ctx context.Context, q *db.Queries, events []domain.Event) error {
	type pending struct {
		asked, waiting *domain.Event
		actor          string
	}

	var order []domain.ItemID

	items := map[domain.ItemID]*pending{}

	for i := range events {
		e := &events[i]

		p := items[e.ItemID]
		if p == nil {
			p = &pending{}
			items[e.ItemID] = p
			order = append(order, e.ItemID)
		}

		if ref, ok := e.Data["actor"].(string); ok && p.actor == "" {
			p.actor = ref
		}

		if slot := map[domain.EventKind]**domain.Event{
			domain.EventQuestionAsked: &p.asked, domain.EventWaitingHuman: &p.waiting,
		}[e.Kind]; slot != nil {
			*slot = e
		}
	}

	for _, id := range order {
		p := items[id]

		e, kind := p.waiting, domain.NotifyApproval
		if p.asked != nil {
			e, kind = p.asked, domain.NotifyQuestion
		}

		if e == nil {
			continue
		}

		if err := queueItemNotification(ctx, q, *e, kind, p.actor); err != nil {
			return err
		}
	}

	return nil
}

// queueItemNotification queues the notification of item event e, the newest of its kind on its
// item in this transaction.
func queueItemNotification(ctx context.Context, q *db.Queries, e domain.Event, kind domain.NotificationEvent, actor string) error {
	factory, err := parseID("factory", string(e.FactoryID))
	if err != nil {
		return err
	}

	item, err := parseID("item", string(e.ItemID))
	if err != nil {
		return err
	}

	id, err := q.LatestItemEventID(ctx, db.LatestItemEventIDParams{ItemID: item, Kind: string(e.Kind)})
	if err != nil {
		return fmt.Errorf("the %s event of item %s: %w", e.Kind, e.ItemID, err)
	}

	payload := map[string]any{payloadFactory: string(e.FactoryID), "item_id": string(e.ItemID), "station": e.Station}
	if e.RunID != "" {
		payload["run_id"] = string(e.RunID)
	}

	return queueNotifications(ctx, q, outboxEvent{
		id: "event/" + strconv.FormatInt(id, 10), event: kind, factory: factory, actor: actor, at: e.At, payload: payload,
	})
}

// andonNotification is the andon event of pull p, pulled by by.
func andonNotification(p domain.AndonPull, factory uuid.UUID, by domain.Actor) outboxEvent {
	payload := map[string]any{payloadFactory: string(p.FactoryID), "andon_id": p.ID, payloadKind: string(p.Kind)}
	if p.Station != "" {
		payload["station"] = p.Station
	}

	return outboxEvent{
		id: "andon/" + strconv.FormatInt(p.ID, 10), event: domain.NotifyAndon, factory: factory, actor: by.Ref(),
		at: p.PulledAt, payload: payload,
	}
}

// QueueBudgetNotification implements ports.NotificationOutbox. A transaction lock on the factory
// serializes the instances that see the crossing at once; the first queues it, and every call
// within domain.BudgetWindow of a queued one finds its rows and does nothing.
func (s *Store) QueueBudgetNotification(ctx context.Context, factory domain.FactoryID, spentUSD, weeklyUSD float64, at time.Time) error {
	id, err := parseID("factory", string(factory))
	if err != nil {
		return err
	}

	at = at.UTC()

	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockBudgetNotification(ctx, id); err != nil {
			return fmt.Errorf("lock: %w", err)
		}

		queued, err := q.BudgetNotificationSince(ctx, db.BudgetNotificationSinceParams{Since: domain.BudgetSince(at), FactoryID: id})
		if err != nil || queued {
			return err
		}

		return queueNotifications(ctx, q, outboxEvent{
			id: "budget/" + id.String() + "/" + at.Format(time.RFC3339Nano), event: domain.NotifyBudget, factory: id, at: at,
			payload: map[string]any{
				payloadFactory: string(factory), "spent_usd": spentUSD, "weekly_usd": weeklyUSD, "since": domain.BudgetSince(at),
			},
		})
	})
	if err != nil {
		return fmt.Errorf("queue the budget notification of factory %s: %w", factory, err)
	}

	return nil
}

var _ ports.NotificationDeliveries = (*Store)(nil)

// ClaimNotifications implements ports.NotificationDeliveries.
func (s *Store) ClaimNotifications(ctx context.Context, channel string, now time.Time, lease time.Duration, limit int) ([]ports.Notification, error) {
	var rows []db.ClaimNotificationsRow

	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error

		rows, err = q.ClaimNotifications(ctx, db.ClaimNotificationsParams{
			Channel: channel, Now: now, Until: now.Add(lease), Max: int32(min(limit, maxClaim)), //nolint:gosec // bounded by maxClaim
		})

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("claim %s notifications: %w", channel, err)
	}

	out := make([]ports.Notification, 0, len(rows))

	for _, r := range rows {
		n, err := claimedNotification(r)
		if err != nil {
			return nil, err
		}

		out = append(out, n)
	}

	return out, nil
}

// maxClaim bounds one claim, whatever the caller asks for.
const maxClaim = 1000

// claimedNotification is the claim's row r as the sender reads it.
func claimedNotification(r db.ClaimNotificationsRow) (ports.Notification, error) {
	var p struct {
		FactoryID string  `json:"factory_id"`
		Station   string  `json:"station"`
		Kind      string  `json:"kind"`
		SpentUSD  float64 `json:"spent_usd"`
		WeeklyUSD float64 `json:"weekly_usd"`
	}

	if err := json.Unmarshal(r.Payload, &p); err != nil {
		return ports.Notification{}, fmt.Errorf("notification %d payload: %w", r.ID, err)
	}

	event := domain.NotificationEvent(r.Event)

	return ports.Notification{
		ID: r.ID, EventID: r.EventID, Event: event, Channel: r.Channel, UserID: domain.UserID(r.UserID.String()),
		Attempts: int(r.Attempts), At: r.CreatedAt,
		Qualifies: stillQualifies(r, event), Address: r.Address,
		OrgSlug: r.OrgSlug, FactoryID: domain.FactoryID(p.FactoryID), FactoryName: r.FactoryName, FactoryTitle: r.FactoryTitle,
		ItemNumber: r.ItemNumber, ItemTitle: r.ItemTitle, Station: p.Station, AndonKind: p.Kind,
		SpentUSD: p.SpentUSD, WeeklyUSD: p.WeeklyUSD,
	}, nil
}

// stillQualifies applies the queue's recipient rule to r again: a member who is an owner or an
// admin, or holds one of the event's factory roles, with the row's channel on (their saved
// choice, else the default).
func stillQualifies(r db.ClaimNotificationsRow, event domain.NotificationEvent) bool {
	roles, known := notifyRoles[event]
	if !known || r.OrgRole == "" || r.Address == "" {
		return false
	}

	if r.OrgRole != string(domain.OrgOwner) && r.OrgRole != string(domain.OrgAdmin) && !slices.Contains(roles, r.FactoryRole) {
		return false
	}

	def := domain.DefaultNotificationPreferences().Events[event]

	switch r.Channel {
	case ports.ChannelEmail:
		return valueOr(r.PrefersEmail, def.Email)
	case ports.ChannelSlack:
		return valueOr(r.PrefersSlack, def.Slack)
	default:
		return false
	}
}

func valueOr(b *bool, def bool) bool {
	if b == nil {
		return def
	}

	return *b
}

// FinishNotification implements ports.NotificationDeliveries.
func (s *Store) FinishNotification(ctx context.Context, id int64, attempts int, o ports.NotificationOutcome) (bool, error) {
	c := outcomeColumnsOf(o)
	p := db.FinishNotificationParams{
		State: o.State, ID: id, Attempts: int32(attempts), //nolint:gosec // attempts come from a claim's int32
		SentAt: c.sentAt, NextAttemptAt: c.nextAttemptAt, LastError: c.lastError,
	}

	var n int64

	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error

		n, err = q.FinishNotification(ctx, p)

		return err
	})
	if err != nil {
		return false, fmt.Errorf("finish notification %d: %w", id, err)
	}

	return n == 1, nil
}

// outcomeColumns are the columns a claim's outcome sets besides its state, NULL where it sets
// none: an outbox row's and a digest's alike.
type outcomeColumns struct {
	sentAt, nextAttemptAt *time.Time
	lastError             *string
}

// outcomeColumnsOf is o's columns: sent_at when sent, next_attempt_at when back to pending, and
// last_error with a reason.
func outcomeColumnsOf(o ports.NotificationOutcome) outcomeColumns {
	var c outcomeColumns

	switch o.State {
	case ports.NotificationSent:
		at := o.At
		c.sentAt = &at
	case ports.NotificationPending:
		at := o.RetryAt
		c.nextAttemptAt = &at
	}

	if o.Reason != "" {
		reason := o.Reason
		c.lastError = &reason
	}

	return c
}
