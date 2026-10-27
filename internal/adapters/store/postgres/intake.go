package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Intake = (*Intake)(nil)

// Intake implements ports.Intake over 00068's webhook_endpoints, webhook_deliveries and
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

	e := domain.WebhookEndpoint{ID: domain.WebhookEndpointID(id.String()), FactoryID: n.FactoryID, Name: n.Name, Kind: n.Kind}

	secret, s, err := in.newSecret(org, e.ID)
	if err != nil {
		return domain.WebhookSecret{}, fmt.Errorf("create endpoint %s: %w", n.Name, err)
	}

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		at, err := q.InsertWebhookEndpoint(ctx, db.InsertWebhookEndpointParams{
			ID: id, FactoryID: factory, Name: n.Name, Kind: string(n.Kind), KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext,
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

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.WebhookEndpointsRow, error) { return q.WebhookEndpoints(ctx, fid) })
	if err != nil {
		return nil, fmt.Errorf("endpoints of factory %s: %w", factory, err)
	}

	out := make([]domain.WebhookEndpoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, endpointFrom(db.WebhookEndpointRow(r)))
	}

	return out, nil
}

// Endpoint implements ports.Intake.
func (in *Intake) Endpoint(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookEndpoint, error) {
	uid, err := parseID("endpoint", string(id))
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	r, err := fetch(ctx, in.s.read, func(q *db.Queries) (db.WebhookEndpointRow, error) { return q.WebhookEndpoint(ctx, uid) })

	switch {
	case notFound(err):
		return domain.WebhookEndpoint{}, fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	case err != nil:
		return domain.WebhookEndpoint{}, fmt.Errorf("endpoint %s: %w", id, err)
	default:
		return endpointFrom(r), nil
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

	r, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (db.RotateWebhookSecretRow, error) {
		return q.RotateWebhookSecret(ctx, db.RotateWebhookSecretParams{KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext, ID: uid})
	})

	switch {
	case notFound(err):
		return domain.WebhookSecret{}, fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	case err != nil:
		return domain.WebhookSecret{}, fmt.Errorf("rotate endpoint %s: %w", id, err)
	default:
		return domain.WebhookSecret{Endpoint: endpointFrom(db.WebhookEndpointRow(r)), Secret: secret}, nil
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

// PruneDeliveries implements ports.Intake.
func (in *Intake) PruneDeliveries(ctx context.Context, before time.Time) (int, error) {
	n, err := fetch(ctx, in.s.systemTx, func(q *db.Queries) (int64, error) { return q.PruneWebhookDeliveries(ctx, before) })
	if err != nil {
		return 0, fmt.Errorf("prune deliveries: %w", err)
	}

	return int(n), nil
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
			IdempotencyKey: f.IdempotencyKey, Dedupe: f.Dedupe, Outcome: string(f.Outcome), Reason: f.Reason, ItemID: item, At: f.At,
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
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.ItemSourceTallyRow, error) {
		return q.ItemSourceTally(ctx, db.ItemSourceTallyParams{FactoryID: fid, Since: since})
	})
	if err != nil {
		return nil, fmt.Errorf("item sources of factory %s: %w", factory, err)
	}

	out := make([]domain.SourceCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.SourceCount{Source: domain.ItemSource(r.Source), Items: int(r.Items)})
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

func endpointFrom(r db.WebhookEndpointRow) domain.WebhookEndpoint {
	return domain.WebhookEndpoint{
		ID: domain.WebhookEndpointID(r.ID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()), Name: r.Name,
		Kind: domain.WebhookKind(r.Kind), CreatedAt: r.CreatedAt.UTC(), RotatedAt: utc(r.RotatedAt),
	}
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

	return d, nil
}

func firingFrom(r db.TriggerFiringByKeyRow) domain.TriggerFiring {
	f := domain.TriggerFiring{
		ID: r.ID.String(), FactoryID: domain.FactoryID(r.FactoryID.String()), Trigger: r.Trigger, Source: domain.TriggerSource(r.Source),
		EventKey: r.EventKey, IdempotencyKey: r.IdempotencyKey, Dedupe: r.Dedupe, Outcome: domain.FiringOutcome(r.Outcome),
		Reason: r.Reason, At: r.At.UTC(),
	}

	if r.ItemID != nil {
		f.ItemID = domain.ItemID(r.ItemID.String())
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

	stale := make([]db.StaleSealedCredentialsRow, 0, len(rows))
	for _, r := range rows {
		stale = append(stale, db.StaleSealedCredentialsRow(r))
	}

	return resealRows(key, "webhook endpoint", stale, webhookSecretAAD, func(id uuid.UUID, s sealedValue) error {
		return q.ResealWebhookSecret(ctx, db.ResealWebhookSecretParams{KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext, ID: id})
	})
}

func webhookSecretAAD(org domain.OrgID, id uuid.UUID, keyID string) []byte {
	return domain.WebhookSecretAAD(org, domain.WebhookEndpointID(id.String()), keyID)
}
