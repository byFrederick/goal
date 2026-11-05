package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Intake = (*Intake)(nil)

// Intake implements ports.Intake over 00071's webhook_endpoints, webhook_deliveries and
// trigger_firings, as goal_app under row-level security: every statement sees the organization in
// ctx, except EndpointOrg and PruneDeliveries, which go through their definers. Each signing
// secret is sealed with key under domain.WebhookSecretAAD (organization, endpoint id and key id);
// no error the adapter builds holds it. Without a master key (the zero masterkey.Keys) endpoints
// can be read, listed and deleted, and creating one, rotating or opening its secret fail with
// ports.ErrNoMasterKey.
type Intake struct {
	s   *Store
	key ports.MasterKey
}

// NewIntake returns the intake state on s's pool, sealed with key.
func NewIntake(s *Store, key ports.MasterKey) *Intake { return &Intake{s: s, key: key} }

// endpointNameKey keeps an endpoint's name unique in its factory.
const endpointNameKey = "webhook_endpoints_org_id_factory_id_name_key"

// CreateEndpoint implements ports.Intake.
func (in *Intake) CreateEndpoint(ctx context.Context, n domain.NewWebhookEndpoint) (domain.WebhookSecret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.WebhookSecret{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.WebhookSecret{}, fmt.Errorf("create endpoint: %w", err)
	}

	factory, err := parseID("factory", string(n.FactoryID))
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	id, err := newID()
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	e := domain.WebhookEndpoint{
		ID: domain.WebhookEndpointID(id.String()), FactoryID: n.FactoryID, Name: n.Name, Kind: n.Kind, Sender: n.Kind.Sender(), Enabled: true,
	}

	secret, s, err := in.newSecret(org, e.ID)
	if err != nil {
		return domain.WebhookSecret{}, fmt.Errorf("create endpoint %s: %w", n.Name, err)
	}

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		at, err := q.InsertWebhookEndpoint(ctx, db.InsertWebhookEndpointParams{
			ID: id, FactoryID: &factory, Name: n.Name, Kind: new(string(n.Kind)), KeyID: &s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext,
		})

		switch {
		case violates(err, endpointNameKey):
			return fmt.Errorf("endpoint %q: %w", n.Name, ports.ErrNameTaken)
		case isForeignKeyViolation(err):
			return fmt.Errorf("factory %s: %w", n.FactoryID, ports.ErrNotFound)
		case err != nil:
			return err
		}

		e.CreatedAt = at.UTC()

		return nil
	})
	if err != nil {
		return domain.WebhookSecret{}, fmt.Errorf("create endpoint %s: %w", n.Name, ruleError(err, ""))
	}

	return domain.WebhookSecret{Endpoint: e, Secret: secret}, nil
}

// ListEndpoints implements ports.Intake.
func (in *Intake) ListEndpoints(ctx context.Context, factory domain.FactoryID) ([]domain.WebhookEndpoint, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	out, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]domain.WebhookEndpoint, error) {
		rows, err := q.WebhookEndpoints(ctx, &fid)
		if err != nil {
			return nil, err
		}

		return withOpenings(ctx, q, convertRows(rows, func(r db.WebhookEndpointsRow) db.WebhookEndpointRow { return db.WebhookEndpointRow(r) }))
	})
	if err != nil {
		return nil, fmt.Errorf("endpoints of factory %s: %w", factory, err)
	}

	return out, nil
}

// Endpoint implements ports.Intake.
func (in *Intake) Endpoint(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookEndpoint, error) {
	uid, err := parseID("endpoint", string(id))
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	e, err := fetch(ctx, in.s.read, func(q *db.Queries) (domain.WebhookEndpoint, error) { return endpointIn(ctx, q, uid) })

	switch {
	case notFound(err):
		return domain.WebhookEndpoint{}, fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	case err != nil:
		return domain.WebhookEndpoint{}, fmt.Errorf("endpoint %s: %w", id, err)
	default:
		return e, nil
	}
}

// RotateSecret implements ports.Intake.
func (in *Intake) RotateSecret(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookSecret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.WebhookSecret{}, domain.ErrNoOrg
	}

	uid, err := parseID("endpoint", string(id))
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	secret, s, err := in.newSecret(org, id)
	if err != nil {
		return domain.WebhookSecret{}, fmt.Errorf("rotate endpoint %s: %w", id, err)
	}

	e, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (domain.WebhookEndpoint, error) {
		if _, err := q.RotateWebhookSecret(ctx, db.RotateWebhookSecretParams{KeyID: &s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext, ID: uid}); !notFound(err) {
			if err != nil {
				return domain.WebhookEndpoint{}, err
			}

			return endpointIn(ctx, q, uid)
		}

		// No factory endpoint: an organization webhook is signed by a secret, replaced on its own.
		if _, err := q.WebhookEndpoint(ctx, uid); err != nil {
			return domain.WebhookEndpoint{}, err
		}

		return domain.WebhookEndpoint{}, fmt.Errorf("webhook %s is signed by an organization secret; replace its value: %w", id, domain.ErrInvalid)
	})

	switch {
	case notFound(err):
		return domain.WebhookSecret{}, fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	case err != nil:
		return domain.WebhookSecret{}, fmt.Errorf("rotate endpoint %s: %w", id, err)
	default:
		return domain.WebhookSecret{Endpoint: e, Secret: secret}, nil
	}
}

// DeleteEndpoint implements ports.Intake.
func (in *Intake) DeleteEndpoint(ctx context.Context, id domain.WebhookEndpointID) error {
	uid, err := parseID("endpoint", string(id))
	if err != nil {
		return err
	}

	n, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (int64, error) { return q.DeleteWebhookEndpoint(ctx, uid) })

	switch {
	case err != nil:
		return fmt.Errorf("delete endpoint %s: %w", id, err)
	case n == 0:
		return fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	default:
		return nil
	}
}

// EndpointOrg implements ports.Intake.
func (in *Intake) EndpointOrg(ctx context.Context, id domain.WebhookEndpointID) (domain.OrgID, error) {
	uid, err := parseID("endpoint", string(id))
	if err != nil {
		return "", err
	}

	org, err := fetch(ctx, in.s.systemTx, func(q *db.Queries) (uuid.UUID, error) { return q.WebhookEndpointOrg(ctx, uid) })

	switch {
	case notFound(err):
		return "", fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	case err != nil:
		return "", fmt.Errorf("organization of endpoint %s: %w", id, err)
	default:
		return domain.OrgID(org.String()), nil
	}
}

// OpenSecret implements ports.Intake.
func (in *Intake) OpenSecret(ctx context.Context, id domain.WebhookEndpointID) ([]byte, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	uid, err := parseID("endpoint", string(id))
	if err != nil {
		return nil, err
	}

	r, err := fetch(ctx, in.s.read, func(q *db.Queries) (db.SealedWebhookSecretRow, error) { return q.SealedWebhookSecret(ctx, uid) })

	switch {
	case notFound(err):
		return nil, fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	case err != nil:
		return nil, fmt.Errorf("secret of endpoint %s: %w", id, err)
	case in.key.Active() == "":
		return nil, ports.ErrNoMasterKey
	}

	secret, err := in.key.Open(r.KeyID, r.Nonce, r.Ciphertext, domain.WebhookSecretAAD(org, id, r.KeyID))
	if err != nil {
		return nil, fmt.Errorf("secret of endpoint %s: %w", id, err)
	}

	return secret, nil
}

// RecordDelivery implements ports.Intake.
func (in *Intake) RecordDelivery(ctx context.Context, n domain.NewWebhookDelivery) (domain.WebhookDelivery, error) {
	if err := n.Check(); err != nil {
		return domain.WebhookDelivery{}, fmt.Errorf("record delivery: %w", err)
	}

	endpoint, err := parseID("endpoint", string(n.EndpointID))
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	item, err := optionalID("item", string(n.ItemID))
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	id, err := newID()
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	d := domain.WebhookDelivery{
		ID: domain.WebhookDeliveryID(id.String()), EndpointID: n.EndpointID, ReceivedAt: n.ReceivedAt.UTC(),
		Headers: domain.StoredHeaders(n.Headers), Body: n.Body, Outcome: n.Outcome, Reason: n.Reason, ItemID: n.ItemID,
	}

	headers, err := json.Marshal(d.Headers)
	if err != nil {
		return domain.WebhookDelivery{}, fmt.Errorf("record delivery: headers: %w", err)
	}

	body := d.Body
	if body == nil {
		body = []byte{}
	}

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		err := q.InsertWebhookDelivery(ctx, db.InsertWebhookDeliveryParams{
			ID: id, EndpointID: endpoint, ReceivedAt: d.ReceivedAt, Headers: headers, Body: body, Outcome: string(d.Outcome),
			Reason: d.Reason, ItemID: item,
		})
		if isForeignKeyViolation(err) {
			return fmt.Errorf("endpoint %s or item %q: %w", n.EndpointID, n.ItemID, ports.ErrNotFound)
		}

		return err
	})
	if err != nil {
		return domain.WebhookDelivery{}, fmt.Errorf("record delivery: %w", ruleError(err, ""))
	}

	return d, nil
}

// ListDeliveries implements ports.Intake.
func (in *Intake) ListDeliveries(ctx context.Context, endpoint domain.WebhookEndpointID, after domain.DeliveryCursor,
	limit int,
) ([]domain.WebhookDelivery, error) {
	uid, err := parseID("endpoint", string(endpoint))
	if err != nil {
		return nil, err
	}

	p := db.WebhookDeliveriesParams{EndpointID: uid, MaxRows: int32(min(max(limit, 0), math.MaxInt32))}

	if !after.ReceivedAt.IsZero() {
		at := after.ReceivedAt
		p.AfterAt = &at

		if p.AfterID, err = uuid.Parse(string(after.ID)); err != nil {
			return nil, fmt.Errorf("delivery cursor %q: %w", after.ID, domain.ErrInvalid)
		}
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.WebhookDeliveriesRow, error) { return q.WebhookDeliveries(ctx, p) })
	if err != nil {
		return nil, fmt.Errorf("deliveries of endpoint %s: %w", endpoint, err)
	}

	return convertAll(rows, deliveryFrom)
}

// Delivery implements ports.Intake.
func (in *Intake) Delivery(ctx context.Context, endpoint domain.WebhookEndpointID, id domain.WebhookDeliveryID) (domain.WebhookDelivery, error) {
	eid, err := parseID("endpoint", string(endpoint))
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	did, err := parseID("delivery", string(id))
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	r, err := fetch(ctx, in.s.read, func(q *db.Queries) (db.WebhookDeliveryRow, error) {
		return q.WebhookDelivery(ctx, db.WebhookDeliveryParams{EndpointID: eid, ID: did})
	})

	switch {
	case notFound(err):
		return domain.WebhookDelivery{}, fmt.Errorf("delivery %s of endpoint %s: %w", id, endpoint, ports.ErrNotFound)
	case err != nil:
		return domain.WebhookDelivery{}, fmt.Errorf("delivery %s of endpoint %s: %w", id, endpoint, err)
	default:
		return deliveryFrom(db.WebhookDeliveriesRow(r))
	}
}

// DeliveryBySender implements ports.Intake.
func (in *Intake) DeliveryBySender(ctx context.Context, webhook domain.WebhookEndpointID, deliveryID string) (domain.WebhookDelivery, error) {
	wid, err := parseID("webhook", string(webhook))
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	r, err := fetch(ctx, in.s.read, func(q *db.Queries) (db.WebhookDeliveryBySenderRow, error) {
		return q.WebhookDeliveryBySender(ctx, db.WebhookDeliveryBySenderParams{EndpointID: wid, DeliveryID: deliveryID})
	})

	switch {
	case notFound(err):
		return domain.WebhookDelivery{}, fmt.Errorf("delivery %s of webhook %s: %w", deliveryID, webhook, ports.ErrNotFound)
	case err != nil:
		return domain.WebhookDelivery{}, fmt.Errorf("delivery %s of webhook %s: %w", deliveryID, webhook, err)
	default:
		return deliveryFrom(db.WebhookDeliveriesRow(r))
	}
}

// DeliveryDays implements ports.Intake.
func (in *Intake) DeliveryDays(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.DeliveryDay, error) {
	var fid *uuid.UUID

	if factory != "" {
		id, err := parseID("factory", string(factory))
		if err != nil {
			return nil, err
		}

		fid = &id
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.WebhookDeliveryDaysRow, error) {
		return q.WebhookDeliveryDays(ctx, db.WebhookDeliveryDaysParams{FactoryID: fid, Since: since})
	})
	if err != nil {
		return nil, fmt.Errorf("delivery days of factory %s: %w", factory, err)
	}

	out := make([]domain.DeliveryDay, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.DeliveryDay{
			EndpointID: domain.WebhookEndpointID(r.EndpointID.String()), Day: r.Day.UTC(), Delivered: int(r.Delivered), Refused: int(r.Refused),
		})
	}

	return out, nil
}

// PruneDeliveries implements ports.Intake.
func (in *Intake) PruneDeliveries(ctx context.Context, now time.Time) (int, error) {
	n, err := fetch(ctx, in.s.systemTx, func(q *db.Queries) (int64, error) {
		deliveries, err := q.PruneWebhookDeliveries(ctx, now.Add(-domain.DeliveryRetention))
		if err != nil {
			return 0, err
		}

		ledger, err := q.PruneWebhookLedger(ctx, now.Add(-domain.LedgerRetention))

		return deliveries + ledger, err
	})
	if err != nil {
		return 0, fmt.Errorf("prune deliveries: %w", err)
	}

	return int(n), nil
}

// Receive implements ports.Intake: the ledger's insert decides a duplicate, so two goalds that
// receive one delivery at once keep one, and a kept delivery's evaluations go in with its row.
func (in *Intake) Receive(ctx context.Context, n domain.NewReceipt) (domain.Received, error) {
	if err := n.Check(); err != nil {
		return domain.Received{}, fmt.Errorf("receive delivery: %w", err)
	}

	webhook, err := parseID("webhook", string(n.WebhookID))
	if err != nil {
		return domain.Received{}, err
	}

	headers, err := json.Marshal(domain.StoredHeaders(n.Headers))
	if err != nil {
		return domain.Received{}, fmt.Errorf("receive delivery: headers: %w", err)
	}

	at := n.ReceivedAt.UTC()
	out := domain.Received{Receipt: n.Receipt, DeliveryID: n.DeliveryID}

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		if n.Receipt == domain.ReceiptAccepted {
			_, err := q.InsertLedgerID(ctx, db.InsertLedgerIDParams{WebhookID: webhook, DeliveryID: n.DeliveryID, ReceivedAt: at})

			switch {
			case notFound(err):
				out.Receipt = domain.ReceiptDuplicate

				// The first row is gone, or there was none: the id was only counted.
				row, err := q.CountDuplicate(ctx, db.CountDuplicateParams{EndpointID: webhook, DeliveryID: n.DeliveryID})
				if notFound(err) {
					return nil
				}

				out.Row = domain.WebhookDeliveryID(row.String())

				return err
			case err != nil:
				return err
			}

			if n.Ping {
				if err := q.PingWebhook(ctx, db.PingWebhookParams{ID: webhook, At: at}); err != nil {
					return err
				}
			}

			if !n.Named {
				return q.CountUnnamedDelivery(ctx, db.CountUnnamedDeliveryParams{WebhookID: webhook, ReceivedAt: at})
			}
		}

		row, err := in.insertReceived(ctx, q, webhook, headers, n)
		out.Row = row

		if err != nil || len(n.Evaluations) == 0 {
			return err
		}

		return insertEvaluations(ctx, q, uuid.MustParse(string(row)), n)
	})

	switch {
	case isForeignKeyViolation(err):
		return domain.Received{}, fmt.Errorf("webhook %s: %w", n.WebhookID, ports.ErrNotFound)
	case err != nil:
		return domain.Received{}, fmt.Errorf("receive delivery: %w", ruleError(err, ""))
	default:
		return out, nil
	}
}

// insertReceived keeps the row of a delivery the receiver accepted or refused, with its body for an
// accepted one.
func (in *Intake) insertReceived(ctx context.Context, q *db.Queries, webhook uuid.UUID, headers []byte, n domain.NewReceipt,
) (domain.WebhookDeliveryID, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}

	p := db.InsertReceivedDeliveryParams{
		ID: id, EndpointID: webhook, ReceivedAt: n.ReceivedAt.UTC(), Headers: headers, Body: n.Body, Outcome: string(domain.DeliveryAccepted),
		Reason: n.Reason, Event: n.Event, Receipt: new(string(n.Receipt)),
	}

	if n.Receipt == domain.ReceiptRefused {
		p.Outcome = string(domain.DeliveryRefused)
	} else {
		p.DeliveryID = &n.DeliveryID
	}

	if p.Body == nil {
		p.Body = []byte{}
	}

	if _, err := q.InsertReceivedDelivery(ctx, p); err != nil {
		return "", err
	}

	return domain.WebhookDeliveryID(id.String()), nil
}

// UnnamedDays implements ports.Intake.
func (in *Intake) UnnamedDays(ctx context.Context, webhook domain.WebhookEndpointID, since time.Time) ([]domain.DeliveryCount, error) {
	uid, err := parseID("webhook", string(webhook))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.UnnamedDeliveryDaysRow, error) {
		return q.UnnamedDeliveryDays(ctx, db.UnnamedDeliveryDaysParams{WebhookID: uid, Since: since})
	})
	if err != nil {
		return nil, fmt.Errorf("unnamed deliveries of webhook %s: %w", webhook, err)
	}

	out := make([]domain.DeliveryCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.DeliveryCount{WebhookID: webhook, Day: r.Day.UTC(), Count: int(r.Count)})
	}

	return out, nil
}

// RecordFiring implements ports.Intake.
func (in *Intake) RecordFiring(ctx context.Context, f domain.TriggerFiring) (domain.TriggerFiring, bool, error) {
	if err := f.Check(); err != nil {
		return domain.TriggerFiring{}, false, fmt.Errorf("record firing: %w", err)
	}

	factory, err := parseID("factory", string(f.FactoryID))
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	item, err := optionalID("item", string(f.ItemID))
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	id, err := newID()
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	var (
		out   domain.TriggerFiring
		fresh bool
	)

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.InsertTriggerFiring(ctx, db.InsertTriggerFiringParams{
			ID: id, FactoryID: factory, Trigger: f.Trigger, Source: string(f.Source), EventKey: f.EventKey,
			IdempotencyKey: f.IdempotencyKey, Dedupe: f.Dedupe, Outcome: string(f.Outcome), Reason: f.Reason, ItemID: item,
			Missed: int32(min(max(f.Missed, 0), math.MaxInt32)), At: f.At, CheckState: checkStateArg(f.CheckState),
		})

		switch {
		case err == nil:
			out, fresh = f, true
			out.ID, out.At = id.String(), f.At.UTC().Truncate(time.Microsecond)

			return nil
		case isForeignKeyViolation(err):
			return fmt.Errorf("factory %s or item %q: %w", f.FactoryID, f.ItemID, ports.ErrNotFound)
		case !notFound(err):
			return err
		}

		// The trigger already has a firing with this key: it stands.
		r, err := q.TriggerFiringByKey(ctx, db.TriggerFiringByKeyParams{FactoryID: factory, Trigger: f.Trigger, IdempotencyKey: f.IdempotencyKey})
		if err != nil {
			return err
		}

		out = firingFrom(r)

		return nil
	})
	if err != nil {
		return domain.TriggerFiring{}, false, fmt.Errorf("record firing of %s: %w", f.Trigger, ruleError(err, ""))
	}

	return out, fresh, nil
}

// Firing implements ports.Intake.
func (in *Intake) Firing(ctx context.Context, factory domain.FactoryID, trigger, key string) (domain.TriggerFiring, bool, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	r, err := fetch(ctx, in.s.read, func(q *db.Queries) (db.TriggerFiringByKeyRow, error) {
		return q.TriggerFiringByKey(ctx, db.TriggerFiringByKeyParams{FactoryID: fid, Trigger: trigger, IdempotencyKey: key})
	})

	switch {
	case notFound(err):
		return domain.TriggerFiring{}, false, nil
	case err != nil:
		return domain.TriggerFiring{}, false, fmt.Errorf("firing of %s: %w", trigger, err)
	default:
		return firingFrom(r), true, nil
	}
}

// FailureStreak implements ports.Intake.
func (in *Intake) FailureStreak(ctx context.Context, factory domain.FactoryID, trigger, dedupe string) (int, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return 0, err
	}

	n, err := fetch(ctx, in.s.read, func(q *db.Queries) (int64, error) {
		return q.TriggerFailureStreak(ctx, db.TriggerFailureStreakParams{FactoryID: fid, Trigger: trigger, Dedupe: dedupe})
	})
	if err != nil {
		return 0, fmt.Errorf("failure streak of %s: %w", trigger, err)
	}

	return int(n), nil
}

// scheduleLock is the session advisory lock key of the schedules, per schema as Migrate's is.
const scheduleLock = "SELECT pg_try_advisory_lock(hashtext('goal.intake.schedules.' || current_schema()))"

// WithScheduleLock implements ports.Intake. The lock is a session advisory lock on a connection
// of its own (withSessionLock).
func (in *Intake) WithScheduleLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error) {
	return withSessionLock(ctx, in.s, scheduleLock, "schedule lock", fn)
}

// withSessionLock runs fn when the advisory lock query lock (a pg_try_advisory_lock) takes its
// lock, and reports whether it did. The lock is taken on a connection of its own, opened from
// the pool's config and not taken from the pool, straight to PostgreSQL (ADR 0026 decision 4: a
// pooler that drops session locks must not sit between); closing that connection frees it, even
// if goald dies while fn runs.
func withSessionLock(ctx context.Context, s *Store, lock, name string, fn func(ctx context.Context) error) (ran bool, err error) {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return false, fmt.Errorf("%s: connect: %w", name, err)
	}

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()

		if cerr := conn.Close(closeCtx); cerr != nil {
			err = errors.Join(err, fmt.Errorf("%s: close: %w", name, cerr))
		}
	}()

	var held bool
	if err := conn.QueryRow(ctx, lock).Scan(&held); err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}

	if !held {
		return false, nil
	}

	return true, fn(ctx)
}

// CreatedSince implements ports.Intake.
func (in *Intake) CreatedSince(ctx context.Context, factory domain.FactoryID, trigger string, since time.Time) (int, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return 0, err
	}

	n, err := fetch(ctx, in.s.read, func(q *db.Queries) (int64, error) {
		return q.TriggerCreatedSince(ctx, db.TriggerCreatedSinceParams{FactoryID: fid, Trigger: trigger, Since: since})
	})
	if err != nil {
		return 0, fmt.Errorf("items created by %s: %w", trigger, err)
	}

	return int(n), nil
}

// OpenItemByDedupe implements ports.Intake.
func (in *Intake) OpenItemByDedupe(ctx context.Context, factory domain.FactoryID, trigger, value string) (domain.ItemID, bool, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return "", false, err
	}

	id, err := fetch(ctx, in.s.read, func(q *db.Queries) (uuid.UUID, error) {
		return q.OpenItemByDedupe(ctx, db.OpenItemByDedupeParams{FactoryID: fid, Trigger: trigger, Value: value})
	})

	switch {
	case notFound(err):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("open item of %s by dedupe: %w", trigger, err)
	default:
		return domain.ItemID(id.String()), true, nil
	}
}

// SourceTally implements ports.Intake.
func (in *Intake) SourceTally(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.SourceCount, error) {
	return factoryTally(ctx, in, factory, "item sources", func(q *db.Queries, fid uuid.UUID) ([]db.ItemSourceTallyRow, error) {
		return q.ItemSourceTally(ctx, db.ItemSourceTallyParams{FactoryID: fid, Since: since})
	}, sourceCount)
}

func sourceCount(r db.ItemSourceTallyRow) domain.SourceCount {
	return domain.SourceCount{Source: domain.ItemSource(r.Source), Items: int(r.Items)}
}

// factoryTally reads a factory's counts with query and makes each row a count with count; what
// names them in an error.
func factoryTally[R, C any](ctx context.Context, in *Intake, factory domain.FactoryID, what string,
	query func(q *db.Queries, fid uuid.UUID) ([]R, error), count func(R) C,
) ([]C, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]R, error) { return query(q, fid) })
	if err != nil {
		return nil, fmt.Errorf("%s of factory %s: %w", what, factory, err)
	}

	out := make([]C, 0, len(rows))
	for _, r := range rows {
		out = append(out, count(r))
	}

	return out, nil
}

// TriggerStats implements ports.Intake.
func (in *Intake) TriggerStats(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.TriggerStat, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.TriggerStatsRow, error) {
		return q.TriggerStats(ctx, db.TriggerStatsParams{FactoryID: fid, Since: since})
	})
	if err != nil {
		return nil, fmt.Errorf("trigger stats of factory %s: %w", factory, err)
	}

	out := make([]domain.TriggerStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.TriggerStat{Trigger: r.Trigger, Created: int(r.Created), Merged: int(r.Merged), SpendUSD: r.SpendUsd})
	}

	return out, nil
}

// ItemSources implements ports.Intake.
func (in *Intake) ItemSources(ctx context.Context, factory domain.FactoryID, last int) (domain.SourceWindow, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.SourceWindow{}, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.LastItemSourcesRow, error) {
		return q.LastItemSources(ctx, db.LastItemSourcesParams{FactoryID: fid, MaxItems: int32(min(max(last, 0), math.MaxInt32))})
	})
	if err != nil {
		return domain.SourceWindow{}, fmt.Errorf("sources of factory %s's last items: %w", factory, err)
	}

	w := domain.SourceWindow{Counts: make([]domain.SourceCount, 0, len(rows))}

	for _, r := range rows {
		w.Counts = append(w.Counts, domain.SourceCount{Source: domain.ItemSource(r.Source), Items: int(r.Items)})

		if first := r.FirstAt.UTC(); w.From.IsZero() || first.Before(w.From) {
			w.From = first
		}

		if lastAt := r.LastAt.UTC(); lastAt.After(w.To) {
			w.To = lastAt
		}
	}

	return w, nil
}

// OriginTally implements ports.Intake.
func (in *Intake) OriginTally(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.OriginCount, error) {
	return factoryTally(ctx, in, factory, "item origins", func(q *db.Queries, fid uuid.UUID) ([]db.OriginTallyRow, error) {
		return q.OriginTally(ctx, db.OriginTallyParams{FactoryID: fid, Since: since})
	}, originCount)
}

func originCount(r db.OriginTallyRow) domain.OriginCount {
	return domain.OriginCount{Origin: domain.Origin(r.Origin), Items: int(r.Items)}
}

// ComingUp implements ports.Intake.
func (in *Intake) ComingUp(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.ComingUp, int, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, 0, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.ComingUpItemsRow, error) {
		return q.ComingUpItems(ctx, db.ComingUpItemsParams{FactoryID: fid, MaxItems: int32(min(max(limit, 0), math.MaxInt32))})
	})
	if err != nil {
		return nil, 0, fmt.Errorf("items coming up in factory %s: %w", factory, err)
	}

	out := make([]domain.ComingUp, 0, len(rows))
	total := 0

	for _, r := range rows {
		it, err := itemFrom(r.WorkItem)
		if err != nil {
			return nil, 0, err
		}

		needs := r.Needs
		if needs == nil {
			needs = []int64{}
		}

		out = append(out, domain.ComingUp{Item: it, Needs: needs, FromFactory: domain.FactoryID(r.FromFactoryID), FromNumber: r.FromNumber})
		total = int(r.Total)
	}

	return out, total, nil
}

// TriageQueue implements ports.Intake.
func (in *Intake) TriageQueue(ctx context.Context, factory domain.FactoryID, limit int) (domain.TriageQueue, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.TriageQueue{}, err
	}

	q := domain.TriageQueue{Waiting: []domain.TriageSource{}}

	err = in.s.read(ctx, func(dq *db.Queries) error {
		row, err := dq.TriageQueueItem(ctx, fid)

		switch {
		case notFound(err):
		case err != nil:
			return fmt.Errorf("triage item: %w", err)
		default:
			it, err := itemFrom(row.WorkItem)
			if err != nil {
				return err
			}

			q.Item, q.FollowUps = &it, int(row.FollowUps)
		}

		rows, err := dq.WaitingTriageSources(ctx, db.WaitingTriageSourcesParams{FactoryID: fid, MaxItems: int32(min(max(limit, 0), math.MaxInt32))})
		if err != nil {
			return fmt.Errorf("waiting triage sources: %w", err)
		}

		for _, r := range rows {
			q.Waiting = append(q.Waiting, domain.TriageSource{
				ID: domain.ItemID(r.ID.String()), Number: r.Number, Title: r.Title, FollowUps: int(r.FollowUps),
			})
		}

		return nil
	})
	if err != nil {
		return domain.TriageQueue{}, fmt.Errorf("triage queue of factory %s: %w", factory, err)
	}

	return q, nil
}

// TriggerDays implements ports.Intake.
func (in *Intake) TriggerDays(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.TriggerDay, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.TriggerDaysRow, error) {
		return q.TriggerDays(ctx, db.TriggerDaysParams{FactoryID: fid, Since: since})
	})
	if err != nil {
		return nil, fmt.Errorf("trigger days of factory %s: %w", factory, err)
	}

	out := make([]domain.TriggerDay, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.TriggerDay{Trigger: r.Trigger, Day: r.Day.UTC(), Created: int(r.Created)})
	}

	return out, nil
}

// Firings implements ports.Intake.
func (in *Intake) Firings(ctx context.Context, factory domain.FactoryID, trigger string, limit int) ([]domain.FiringEntry, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.TriggerFiringsRow, error) {
		return q.TriggerFirings(ctx, db.TriggerFiringsParams{FactoryID: fid, Trigger: trigger, MaxRows: int32(min(max(limit, 0), math.MaxInt32))})
	})
	if err != nil {
		return nil, fmt.Errorf("firings of %s: %w", trigger, err)
	}

	out := make([]domain.FiringEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.FiringEntry{Firing: firingFrom(db.TriggerFiringByKeyRow{
			ID: r.ID, FactoryID: r.FactoryID, Trigger: r.Trigger, Source: r.Source, EventKey: r.EventKey, IdempotencyKey: r.IdempotencyKey,
			Dedupe: r.Dedupe, Outcome: r.Outcome, Reason: r.Reason, ItemID: r.ItemID, Missed: r.Missed, At: r.At,
			CheckState: r.CheckState, RunID: r.RunID, CostUsd: r.CostUsd, Decisions: r.Decisions,
		}), ItemNumber: r.ItemNumber, Merged: r.Merged})
	}

	return out, nil
}

// PruneFirings implements ports.Intake, in the organization in ctx, through goal_app's DELETE
// grant on trigger_firings.
func (in *Intake) PruneFirings(ctx context.Context, factory domain.FactoryID, before time.Time) (int, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return 0, err
	}

	n, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (int64, error) {
		return q.PruneTriggerFirings(ctx, db.PruneTriggerFiringsParams{FactoryID: fid, Before: before})
	})
	if err != nil {
		return 0, fmt.Errorf("prune firings of factory %s: %w", factory, err)
	}

	return int(n), nil
}

// newSecret generates a signing secret for endpoint id of org and seals it under the active key.
func (in *Intake) newSecret(org domain.OrgID, id domain.WebhookEndpointID) ([]byte, sealedValue, error) {
	active := in.key.Active()
	if active == "" {
		return nil, sealedValue{}, ports.ErrNoMasterKey
	}

	secret := make([]byte, domain.WebhookSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, sealedValue{}, fmt.Errorf("new signing secret: %w", err)
	}

	keyID, nonce, ct, err := in.key.Seal(secret, domain.WebhookSecretAAD(org, id, active))
	if err != nil {
		return nil, sealedValue{}, err
	}

	if keyID != active {
		return nil, sealedValue{}, fmt.Errorf("master key sealed under %q, not the active %q", keyID, active)
	}

	return secret, sealedValue{keyID: keyID, nonce: nonce, ciphertext: ct}, nil
}

// endpointFrom is r without its openings (withOpenings): a factory endpoint reads as its kind's
// sender, enabled.
func endpointFrom(r db.WebhookEndpointRow) domain.WebhookEndpoint {
	e := domain.WebhookEndpoint{
		ID: domain.WebhookEndpointID(r.ID.String()), Name: r.Name, Kind: domain.WebhookKind(deref(r.Kind)),
		Sender: domain.WebhookSender(deref(r.Sender)), Secret: r.SecretName, Enabled: r.Enabled,
		Token:     domain.TokenSettings{Header: deref(r.TokenHeader), EventPath: deref(r.TokenEventPath), IDPath: deref(r.TokenIDPath)},
		CreatedAt: r.CreatedAt.UTC(), RotatedAt: utc(r.RotatedAt), PingedAt: utc(r.PingedAt),
	}

	if r.FactoryID != nil {
		e.FactoryID = domain.FactoryID(r.FactoryID.String())
	}

	if r.SecretID != nil {
		e.SecretID = domain.SecretID(r.SecretID.String())
	}

	if e.Sender == "" {
		e.Sender = e.Kind.Sender()
	}

	return e
}

func deliveryFrom(r db.WebhookDeliveriesRow) (domain.WebhookDelivery, error) {
	headers := map[string]string{}
	if err := json.Unmarshal(r.Headers, &headers); err != nil {
		return domain.WebhookDelivery{}, fmt.Errorf("delivery %s: headers: %w", r.ID, err)
	}

	d := domain.WebhookDelivery{
		ID: domain.WebhookDeliveryID(r.ID.String()), EndpointID: domain.WebhookEndpointID(r.EndpointID.String()),
		ReceivedAt: r.ReceivedAt.UTC(), Headers: headers, Body: r.Body, Outcome: domain.DeliveryOutcome(r.Outcome), Reason: r.Reason,
	}

	if r.ItemID != nil {
		d.ItemID = domain.ItemID(r.ItemID.String())
	}

	if r.DeliveryID != nil {
		d.DeliveryID = *r.DeliveryID
	}

	if r.Receipt != nil {
		d.Receipt = domain.DeliveryReceipt(*r.Receipt)
	}

	d.Event, d.Duplicates = r.Event, int(r.Duplicates)

	return d, nil
}

func firingFrom(r db.TriggerFiringByKeyRow) domain.TriggerFiring {
	f := domain.TriggerFiring{
		ID: r.ID.String(), FactoryID: domain.FactoryID(r.FactoryID.String()), Trigger: r.Trigger, Source: domain.TriggerSource(r.Source),
		EventKey: r.EventKey, IdempotencyKey: r.IdempotencyKey, Dedupe: r.Dedupe, Outcome: domain.FiringOutcome(r.Outcome),
		Reason: r.Reason, Missed: int(r.Missed), At: r.At.UTC(), CostUSD: r.CostUsd, Decisions: string(r.Decisions),
	}

	if r.ItemID != nil {
		f.ItemID = domain.ItemID(r.ItemID.String())
	}

	if r.CheckState != nil {
		f.CheckState = domain.CheckState(*r.CheckState)
	}

	if r.RunID != nil {
		f.RunID = domain.RunID(r.RunID.String())
	}

	f.Try = r.Try
	if r.CheckedAt != nil {
		f.CheckedAt = r.CheckedAt.UTC()
	}

	if len(r.Applied) > 0 {
		var a domain.CheckApplied
		if json.Unmarshal(r.Applied, &a) == nil { // the column holds what FinishCheck wrote
			f.Applied = &a
		}
	}

	return f
}

// resealWebhookSecrets reseals a batch of endpoints' secrets of every organization sealed under
// another key than key's active one, and returns how many.
func resealWebhookSecrets(ctx context.Context, q *db.Queries, key ports.MasterKey) (int, error) {
	rows, err := q.StaleSealedWebhookSecrets(ctx, db.StaleSealedWebhookSecretsParams{Active: key.Active(), Batch: rekeyBatch})
	if err != nil {
		return 0, err
	}

	stale := make([]db.StaleSealedSecretsRow, 0, len(rows))
	for _, r := range rows {
		stale = append(stale, db.StaleSealedSecretsRow(r))
	}

	return resealRows(key, "webhook endpoint", stale, webhookSecretAAD, func(id uuid.UUID, s sealedValue) error {
		return q.ResealWebhookSecret(ctx, db.ResealWebhookSecretParams{KeyID: &s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext, ID: id})
	})
}

func webhookSecretAAD(org domain.OrgID, id uuid.UUID, keyID string) []byte {
	return domain.WebhookSecretAAD(org, domain.WebhookEndpointID(id.String()), keyID)
}
