package postgres

import (
	"context"
	"encoding/json"
	"fmt"
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
