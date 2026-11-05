package porttest

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Intake = (*Intake)(nil)

// Intake keeps intake triggers' state in memory with the database's rules: endpoint names unique
// per factory, each secret sealed with the master key under domain.WebhookSecretAAD, signature
// headers never kept, deliveries going with their endpoint, one firing per (factory, trigger,
// idempotency key), and every row of one organization. It reads items, their merged events and
// their cost from store. Views made with WithKey share the data, as goalds with different keys
// share one database.
type Intake struct {
	d     *intakeData
	key   ports.MasterKey
	store *Store
}

type intakeData struct {
	// schedules is the schedule lock (WithScheduleLock), fires every trigger's lock (Fire).
	schedules   sync.Mutex
	fires       sync.Mutex
	mu          sync.Mutex
	endpoints   map[domain.WebhookEndpointID]*endpointRow
	deliveries  []deliveryRow
	firings     []firingRow
	evaluations []evaluationRow
	// ledger holds each webhook's accepted delivery ids with when they came, and counts the
	// deliveries no trigger named per webhook and UTC day.
	ledger map[ledgerKey]time.Time
	counts map[countKey]int
	// secrets are the organization secrets webhooks are signed by (UseSecrets); nil has none.
	secrets *OrgSecrets
	// states are the triggers' saved check states (trigger_state).
	states map[stateKey]json.RawMessage
	// keys are the triggers' keys (trigger_keys): each key's last item and last note.
	keys map[triggerKey]keyRow
}

type triggerKey struct {
	stateKey
	key string
}

type keyRow struct {
	item     domain.ItemID
	noteAt   time.Time
	noteText string
}

type stateKey struct {
	org     domain.OrgID
	factory domain.FactoryID
	trigger string
}

type endpointRow struct {
	org               domain.OrgID
	e                 domain.WebhookEndpoint
	keyID             string
	nonce, ciphertext []byte
}

type deliveryRow struct {
	org domain.OrgID
	d   domain.WebhookDelivery
}

type ledgerKey struct {
	org      domain.OrgID
	webhook  domain.WebhookEndpointID
	delivery string
}

type countKey struct {
	org     domain.OrgID
	webhook domain.WebhookEndpointID
	day     time.Time
}

type evaluationRow struct {
	org domain.OrgID
	e   domain.DeliveryEvaluation
}

type firingRow struct {
	org domain.OrgID
	f   domain.TriggerFiring
	// lease is a running check's (ClaimCheck); zero otherwise.
	lease domain.CheckLease
}

// NewIntake returns intake state sealed with key, over store's factories and items.
func NewIntake(key ports.MasterKey, store *Store) *Intake {
	return &Intake{key: key, store: store, d: &intakeData{
		endpoints: map[domain.WebhookEndpointID]*endpointRow{}, ledger: map[ledgerKey]time.Time{}, counts: map[countKey]int{},
		states: map[stateKey]json.RawMessage{}, keys: map[triggerKey]keyRow{},
	}}
}

// UseSecrets makes s the secrets the organization's webhooks are signed by, as the secrets table
// is beside webhook_endpoints: CreateWebhook names one of them, and s refuses to delete one a
// webhook uses and keeps its previous value on a replace.
func (in *Intake) UseSecrets(s *OrgSecrets) {
	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	in.d.secrets = s
}

// WithKey returns a view of in's data sealed with key.
func (in *Intake) WithKey(key ports.MasterKey) *Intake {
	return &Intake{d: in.d, key: key, store: in.store}
}

// CreateEndpoint implements ports.Intake.
func (in *Intake) CreateEndpoint(ctx context.Context, n domain.NewWebhookEndpoint) (domain.WebhookSecret, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	if err := n.Check(); err != nil {
		return domain.WebhookSecret{}, err
	}

	if !in.ownsFactory(org, n.FactoryID) {
		return domain.WebhookSecret{}, fmt.Errorf("factory %s: %w", n.FactoryID, ports.ErrNotFound)
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	for _, r := range in.d.endpoints {
		if r.org == org && r.e.FactoryID == n.FactoryID && r.e.Name == n.Name {
			return domain.WebhookSecret{}, fmt.Errorf("endpoint %q: %w", n.Name, ports.ErrNameTaken)
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	r := &endpointRow{org: org, e: domain.WebhookEndpoint{
		ID: domain.WebhookEndpointID(id.String()), FactoryID: n.FactoryID, Name: n.Name, Kind: n.Kind, Sender: n.Kind.Sender(),
		Enabled: true, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}}

	secret, err := in.sealLocked(r)
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	in.d.endpoints[r.e.ID] = r

	return domain.WebhookSecret{Endpoint: cloneEndpoint(r.e), Secret: secret}, nil
}

// ListEndpoints implements ports.Intake.
func (in *Intake) ListEndpoints(ctx context.Context, factory domain.FactoryID) ([]domain.WebhookEndpoint, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	out := []domain.WebhookEndpoint{}

	for _, r := range in.d.endpoints {
		if r.org == org && r.e.OpenTo(factory) {
			out = append(out, in.outLocked(r))
		}
	}

	slices.SortFunc(out, func(a, b domain.WebhookEndpoint) int { return cmp.Compare(a.Name, b.Name) })

	return out, nil
}

// Endpoint implements ports.Intake.
func (in *Intake) Endpoint(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookEndpoint, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	r, err := in.endpointLocked(org, id)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	return in.outLocked(r), nil
}

// CreateWebhook implements ports.Intake.
func (in *Intake) CreateWebhook(ctx context.Context, n domain.NewWebhook) (domain.WebhookEndpoint, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	if err := n.Check(); err != nil {
		return domain.WebhookEndpoint{}, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	if in.nameTakenLocked(org, n.Name, "") {
		return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", n.Name, ports.ErrNameTaken)
	}

	var secret domain.SecretID
	if in.d.secrets != nil {
		secret, _ = in.d.secrets.webhookSecret(org, n.Secret)
	}

	if secret == "" {
		return domain.WebhookEndpoint{}, fmt.Errorf("secret %q: %w", n.Secret, ports.ErrNotFound)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	r := &endpointRow{org: org, e: domain.WebhookEndpoint{
		ID: domain.WebhookEndpointID(id.String()), Name: n.Name, Sender: n.Sender, SecretID: secret, Enabled: true, Token: n.Token,
		Openings: []domain.WebhookOpening{}, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}}
	in.d.endpoints[r.e.ID] = r
	in.d.secrets.sign(org, secret, n.Name, true)

	return in.outLocked(r), nil
}

// Webhooks implements ports.Intake.
func (in *Intake) Webhooks(ctx context.Context) ([]domain.WebhookEndpoint, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	out := []domain.WebhookEndpoint{}

	for _, r := range in.d.endpoints {
		if r.org == org && r.e.OrgLevel() {
			out = append(out, in.outLocked(r))
		}
	}

	slices.SortFunc(out, func(a, b domain.WebhookEndpoint) int { return cmp.Compare(a.Name, b.Name) })

	return out, nil
}

// WebhookByName implements ports.Intake.
func (in *Intake) WebhookByName(ctx context.Context, name string) (domain.WebhookEndpoint, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	for _, r := range in.d.endpoints {
		if r.org == org && r.e.OrgLevel() && r.e.Name == name {
			return in.outLocked(r), nil
		}
	}

	return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", name, ports.ErrNotFound)
}

// PatchWebhook implements ports.Intake.
func (in *Intake) PatchWebhook(ctx context.Context, id domain.WebhookEndpointID, p domain.WebhookPatch) (domain.WebhookEndpoint, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	if err := p.Check(); err != nil {
		return domain.WebhookEndpoint{}, err
	}

	for _, o := range p.Open {
		if !in.ownsFactory(org, o.FactoryID) {
			return domain.WebhookEndpoint{}, fmt.Errorf("factory %s: %w", o.FactoryID, ports.ErrNotFound)
		}
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	r, err := in.endpointLocked(org, id)
	if err != nil || !r.e.OrgLevel() {
		return domain.WebhookEndpoint{}, fmt.Errorf("webhook %s: %w", id, ports.ErrNotFound)
	}

	next := p.Apply(cloneEndpoint(r.e))
	if next.Name != r.e.Name && in.nameTakenLocked(org, next.Name, id) {
		return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", next.Name, ports.ErrNameTaken)
	}

	if next.Name != r.e.Name {
		in.d.secrets.sign(org, r.e.SecretID, r.e.Name, false)
		in.d.secrets.sign(org, r.e.SecretID, next.Name, true)
	}

	r.e = next

	return in.outLocked(r), nil
}

// WebhookSecretUses implements ports.Intake.
func (in *Intake) WebhookSecretUses(ctx context.Context) ([]domain.WebhookSecretUse, error) {
	hooks, err := in.Webhooks(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]domain.WebhookSecretUse, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, domain.WebhookSecretUse{Secret: h.Secret, Webhook: h.Name})
	}

	slices.SortFunc(out, func(a, b domain.WebhookSecretUse) int {
		return cmp.Or(cmp.Compare(a.Secret, b.Secret), cmp.Compare(a.Webhook, b.Webhook))
	})

	return out, nil
}

// ConvertEndpoints implements ports.Intake.
func (in *Intake) ConvertEndpoints(ctx context.Context, by domain.Actor) ([]domain.ConvertedEndpoint, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	var rows []*endpointRow

	for _, r := range in.d.endpoints {
		if r.org == org && !r.e.OrgLevel() {
			rows = append(rows, r)
		}
	}

	slices.SortFunc(rows, func(a, b *endpointRow) int { return cmp.Compare(a.e.ID, b.e.ID) })

	out := []domain.ConvertedEndpoint{}

	for _, r := range rows {
		c, err := in.convertLocked(ctx, r, by)
		if err != nil {
			return out, fmt.Errorf("convert endpoint %s: %w", r.e.ID, err)
		}

		out = append(out, c)
	}

	return out, nil
}

// convertLocked moves the factory endpoint r to the organization, signed by a new secret holding
// its sealed value. Callers hold in.d.mu.
func (in *Intake) convertLocked(ctx context.Context, r *endpointRow, by domain.Actor) (domain.ConvertedEndpoint, error) {
	if in.key.Active() == "" {
		return domain.ConvertedEndpoint{}, ports.ErrNoMasterKey
	}

	if in.d.secrets == nil {
		return domain.ConvertedEndpoint{}, errors.New("the fake intake has no secrets (UseSecrets)")
	}

	value, err := in.key.Open(r.keyID, r.nonce, r.ciphertext, domain.WebhookSecretAAD(r.org, r.e.ID, r.keyID))
	if err != nil {
		return domain.ConvertedEndpoint{}, err
	}

	// The secrets seal with this view's key, as one goald's adapters share its master key.
	secrets := in.d.secrets.WithKey(in.key)

	name := domain.ConvertedWebhookName(r.e.Name, func(n string) bool {
		for _, o := range in.d.endpoints {
			if o.org == r.org && o.e.ID != r.e.ID && o.e.Name == n && (o.e.OrgLevel() || o.e.ID < r.e.ID) {
				return true
			}
		}

		return false
	})

	secret := domain.ConvertedSecretName(name, func(n string) bool {
		_, err := secrets.Get(ctx, n)

		return err == nil
	})

	sec, err := secrets.Create(ctx, domain.NewSecret{Name: secret, Description: "Signs the webhook " + name, Value: hex.EncodeToString(value)}, by)
	if err != nil {
		return domain.ConvertedEndpoint{}, err
	}

	c := domain.ConvertedEndpoint{ID: r.e.ID, FactoryID: r.e.FactoryID, From: r.e.Name, Name: name, Secret: secret}
	r.e.Name, r.e.Sender, r.e.SecretID, r.e.FactoryID = name, r.e.Kind.Sender(), sec.ID, ""
	r.e.Openings = []domain.WebhookOpening{{FactoryID: c.FactoryID}}
	in.d.secrets.sign(r.org, sec.ID, name, true)

	return c, nil
}

// nameTakenLocked reports whether an endpoint of org other than except has name.
func (in *Intake) nameTakenLocked(org domain.OrgID, name string, except domain.WebhookEndpointID) bool {
	for _, r := range in.d.endpoints {
		if r.org == org && r.e.Name == name && r.e.ID != except {
			return true
		}
	}

	return false
}

// outLocked is r's endpoint as the port answers it, with its secret's name now.
func (in *Intake) outLocked(r *endpointRow) domain.WebhookEndpoint {
	e := cloneEndpoint(r.e)
	if e.SecretID != "" && in.d.secrets != nil {
		e.Secret = in.d.secrets.secretName(r.org, e.SecretID)
	}

	return e
}

// RotateSecret implements ports.Intake.
func (in *Intake) RotateSecret(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookSecret, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	r, err := in.endpointLocked(org, id)
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	if r.e.OrgLevel() {
		return domain.WebhookSecret{}, fmt.Errorf("webhook %s is signed by an organization secret; replace its value: %w", id, domain.ErrInvalid)
	}

	next := *r

	secret, err := in.sealLocked(&next)
	if err != nil {
		return domain.WebhookSecret{}, err
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	next.e.RotatedAt = &at
	*r = next

	return domain.WebhookSecret{Endpoint: cloneEndpoint(r.e), Secret: secret}, nil
}

// DeleteEndpoint implements ports.Intake.
func (in *Intake) DeleteEndpoint(ctx context.Context, id domain.WebhookEndpointID) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	r, err := in.endpointLocked(org, id)
	if err != nil {
		return err
	}

	if r.e.OrgLevel() && in.d.secrets != nil {
		in.d.secrets.sign(org, r.e.SecretID, r.e.Name, false)
	}

	delete(in.d.endpoints, id)
	in.d.deliveries = slices.DeleteFunc(in.d.deliveries, func(r deliveryRow) bool { return r.d.EndpointID == id })
	maps.DeleteFunc(in.d.ledger, func(k ledgerKey, _ time.Time) bool { return k.webhook == id })
	maps.DeleteFunc(in.d.counts, func(k countKey, _ int) bool { return k.webhook == id })

	return nil
}

// EndpointOrg implements ports.Intake.
func (in *Intake) EndpointOrg(_ context.Context, id domain.WebhookEndpointID) (domain.OrgID, error) {
	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	r, ok := in.d.endpoints[id]
	if !ok {
		return "", fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	}

	return r.org, nil
}

// OpenSecret implements ports.Intake.
func (in *Intake) OpenSecret(ctx context.Context, id domain.WebhookEndpointID) ([]byte, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	r, err := in.endpointLocked(org, id)
	if err != nil {
		return nil, err
	}

	if r.keyID == "" {
		return nil, fmt.Errorf("webhook %s has no sealed secret: %w", id, ports.ErrNotFound)
	}

	if in.key.Active() == "" {
		return nil, ports.ErrNoMasterKey
	}

	secret, err := in.key.Open(r.keyID, r.nonce, r.ciphertext, domain.WebhookSecretAAD(org, id, r.keyID))
	if err != nil {
		return nil, fmt.Errorf("endpoint %s: %w", id, err)
	}

	return secret, nil
}

// RecordDelivery implements ports.Intake.
func (in *Intake) RecordDelivery(ctx context.Context, n domain.NewWebhookDelivery) (domain.WebhookDelivery, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	if err := n.Check(); err != nil {
		return domain.WebhookDelivery{}, err
	}

	if n.ItemID != "" && !in.ownsItem(org, n.ItemID) {
		return domain.WebhookDelivery{}, fmt.Errorf("item %s: %w", n.ItemID, ports.ErrNotFound)
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	if _, err := in.endpointLocked(org, n.EndpointID); err != nil {
		return domain.WebhookDelivery{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	d := domain.WebhookDelivery{
		ID: domain.WebhookDeliveryID(id.String()), EndpointID: n.EndpointID, ReceivedAt: n.ReceivedAt.UTC(),
		Headers: domain.StoredHeaders(n.Headers), Body: slices.Clone(n.Body), Outcome: n.Outcome, Reason: n.Reason, ItemID: n.ItemID,
	}
	in.d.deliveries = append(in.d.deliveries, deliveryRow{org: org, d: d})

	return cloneDelivery(d), nil
}

// ListDeliveries implements ports.Intake.
func (in *Intake) ListDeliveries(ctx context.Context, endpoint domain.WebhookEndpointID, after domain.DeliveryCursor,
	limit int,
) ([]domain.WebhookDelivery, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	out := []domain.WebhookDelivery{}

	for _, r := range in.d.deliveries {
		if r.org == org && r.d.EndpointID == endpoint && (after.ReceivedAt.IsZero() || newerFirst(r.d, after) > 0) {
			out = append(out, cloneDelivery(r.d))
		}
	}

	slices.SortFunc(out, func(a, b domain.WebhookDelivery) int {
		return newerFirst(a, domain.DeliveryCursor{ReceivedAt: b.ReceivedAt, ID: b.ID})
	})

	return out[:min(len(out), max(limit, 0))], nil
}

// newerFirst orders d against the cursor c newest first: negative when d comes before c.
func newerFirst(d domain.WebhookDelivery, c domain.DeliveryCursor) int {
	return cmp.Or(c.ReceivedAt.Compare(d.ReceivedAt), cmp.Compare(c.ID, d.ID))
}

// Delivery implements ports.Intake.
func (in *Intake) Delivery(ctx context.Context, endpoint domain.WebhookEndpointID, id domain.WebhookDeliveryID) (domain.WebhookDelivery, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	for _, r := range in.d.deliveries {
		if r.org == org && r.d.EndpointID == endpoint && r.d.ID == id {
			return cloneDelivery(r.d), nil
		}
	}

	return domain.WebhookDelivery{}, fmt.Errorf("delivery %s of endpoint %s: %w", id, endpoint, ports.ErrNotFound)
}

// DeliveryBySender implements ports.Intake.
func (in *Intake) DeliveryBySender(ctx context.Context, webhook domain.WebhookEndpointID, deliveryID string) (domain.WebhookDelivery, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.WebhookDelivery{}, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	for _, r := range in.d.deliveries {
		if r.org == org && r.d.EndpointID == webhook && deliveryID != "" && r.d.DeliveryID == deliveryID {
			return cloneDelivery(r.d), nil
		}
	}

	return domain.WebhookDelivery{}, fmt.Errorf("delivery %s of webhook %s: %w", deliveryID, webhook, ports.ErrNotFound)
}

// DeliveryDays implements ports.Intake.
func (in *Intake) DeliveryDays(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.DeliveryDay, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	type key struct {
		endpoint domain.WebhookEndpointID
		day      time.Time
	}

	counts := map[key]*domain.DeliveryDay{}

	for _, r := range in.d.deliveries {
		e, ok := in.d.endpoints[r.d.EndpointID]
		if r.org != org || !ok || !reaches(e.e, factory) || r.d.ReceivedAt.Before(since) {
			continue
		}

		at := r.d.ReceivedAt.UTC()
		k := key{r.d.EndpointID, time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)}

		c, ok := counts[k]
		if !ok {
			c = &domain.DeliveryDay{EndpointID: k.endpoint, Day: k.day}
			counts[k] = c
		}

		if r.d.Outcome == domain.DeliveryRefused {
			c.Refused++
		} else {
			c.Delivered++
		}
	}

	out := make([]domain.DeliveryDay, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}

	slices.SortFunc(out, func(a, b domain.DeliveryDay) int {
		return cmp.Or(cmp.Compare(a.EndpointID, b.EndpointID), a.Day.Compare(b.Day))
	})

	return out, nil
}

// PruneDeliveries implements ports.Intake.
func (in *Intake) PruneDeliveries(_ context.Context, now time.Time) (int, error) {
	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	before, ledgerBefore := now.Add(-domain.DeliveryRetention), now.Add(-domain.LedgerRetention)
	n := len(in.d.deliveries) + len(in.d.ledger) + len(in.d.counts)

	in.d.deliveries = slices.DeleteFunc(in.d.deliveries, func(r deliveryRow) bool { return r.d.ReceivedAt.Before(before) })
	maps.DeleteFunc(in.d.ledger, func(_ ledgerKey, at time.Time) bool { return at.Before(ledgerBefore) })
	maps.DeleteFunc(in.d.counts, func(k countKey, _ int) bool { return k.day.Before(utcDay(ledgerBefore)) })

	return n - len(in.d.deliveries) - len(in.d.ledger) - len(in.d.counts), nil
}

// Receive implements ports.Intake.
func (in *Intake) Receive(ctx context.Context, n domain.NewReceipt) (domain.Received, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Received{}, err
	}

	if err := n.Check(); err != nil {
		return domain.Received{}, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	if _, err := in.endpointLocked(org, n.WebhookID); err != nil {
		return domain.Received{}, err
	}

	out := domain.Received{Receipt: n.Receipt, DeliveryID: n.DeliveryID}
	key := ledgerKey{org: org, webhook: n.WebhookID, delivery: n.DeliveryID}
	_, taken := in.d.ledger[key]

	switch {
	case n.Receipt == domain.ReceiptRefused:
	case taken:
		out.Receipt = domain.ReceiptDuplicate

		for i, r := range in.d.deliveries {
			if r.org == org && r.d.EndpointID == n.WebhookID && r.d.DeliveryID == n.DeliveryID {
				in.d.deliveries[i].d.Duplicates++
				out.Row = r.d.ID
			}
		}

		return out, nil
	case !n.Named:
		in.d.ledger[key] = n.ReceivedAt.UTC()
		in.d.counts[countKey{org: org, webhook: n.WebhookID, day: utcDay(n.ReceivedAt)}]++

		return out, nil
	default:
		in.d.ledger[key] = n.ReceivedAt.UTC()
	}

	row, err := in.keepReceivedLocked(org, n)
	out.Row = row

	if err != nil {
		return out, err
	}

	return out, in.addEvaluationsLocked(org, row, n)
}

// addEvaluationsLocked adds n's evaluations of the delivery row.
func (in *Intake) addEvaluationsLocked(org domain.OrgID, row domain.WebhookDeliveryID, n domain.NewReceipt) error {
	for _, e := range n.Evaluations {
		if !in.ownsFactory(org, e.FactoryID) {
			return fmt.Errorf("factory %s: %w", e.FactoryID, ports.ErrNotFound)
		}

		id, err := uuid.NewV7()
		if err != nil {
			return err
		}

		ev := domain.DeliveryEvaluation{
			ID: id.String(), FactoryID: e.FactoryID, Delivery: row, WebhookID: n.WebhookID, DeliveryID: n.DeliveryID, Event: n.Event,
			Triggers: slices.Clone(e.Triggers), State: domain.EvaluationPending, NextAt: n.ReceivedAt.UTC(), CreatedAt: n.ReceivedAt.UTC(),
		}
		if e.Skip != "" {
			ev.State, ev.Outcome, ev.Reason = domain.EvaluationDone, domain.EvaluationSkipped, e.Skip
		}

		in.d.evaluations = append(in.d.evaluations, evaluationRow{org: org, e: ev})
	}

	return nil
}

// DeliveryEvaluations implements ports.Intake.
func (in *Intake) DeliveryEvaluations(ctx context.Context, delivery domain.WebhookDeliveryID) ([]domain.DeliveryEvaluation, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	out := []domain.DeliveryEvaluation{}

	for _, r := range in.d.evaluations {
		if r.org == org && r.e.Delivery == delivery {
			out = append(out, cloneEvaluation(r.e))
		}
	}

	slices.SortFunc(out, func(a, b domain.DeliveryEvaluation) int { return cmp.Compare(a.FactoryID, b.FactoryID) })

	return out, nil
}

// ClaimEvaluations implements ports.Intake. Views made with WithKey share the rows, as goalds
// share one database.
func (in *Intake) ClaimEvaluations(ctx context.Context, holder string, now time.Time, lease time.Duration, limit int) ([]domain.ClaimedEvaluation, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	var due []int

	for i, r := range in.d.evaluations {
		e := r.e
		if r.org == org && e.State == domain.EvaluationPending && !e.NextAt.After(now) && (e.LeaseUntil == nil || !e.LeaseUntil.After(now)) {
			due = append(due, i)
		}
	}

	slices.SortFunc(due, func(a, b int) int {
		x, y := in.d.evaluations[a].e, in.d.evaluations[b].e

		return cmp.Or(x.NextAt.Compare(y.NextAt), cmp.Compare(x.ID, y.ID))
	})

	out := []domain.ClaimedEvaluation{}

	for _, i := range due[:min(len(due), limit)] {
		e := &in.d.evaluations[i].e
		until := now.Add(lease).UTC()
		e.LeaseHolder, e.LeaseUntil, e.Attempts = holder, &until, e.Attempts+1

		c := domain.ClaimedEvaluation{DeliveryEvaluation: cloneEvaluation(*e)}

		for _, d := range in.d.deliveries {
			if d.org == org && d.d.ID == e.Delivery {
				c.Body = append([]byte{}, d.d.Body...)
			}
		}

		out = append(out, c)
	}

	return out, nil
}

// FinishEvaluation implements ports.Intake.
func (in *Intake) FinishEvaluation(ctx context.Context, f domain.EvaluationFinish) (bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return false, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	for i := range in.d.evaluations {
		e := &in.d.evaluations[i].e
		if in.d.evaluations[i].org != org || e.ID != f.ID {
			continue
		}

		if e.State != domain.EvaluationPending || e.LeaseHolder != f.Holder || e.Attempts != f.Attempts {
			return false, nil
		}

		e.State, e.Outcome, e.Reason, e.ItemID, e.LeaseHolder, e.LeaseUntil = f.State, f.Outcome, f.Reason, f.ItemID, "", nil
		if !f.NextAt.IsZero() {
			e.NextAt = f.NextAt.UTC()
		}

		return true, nil
	}

	return false, nil
}

func cloneEvaluation(e domain.DeliveryEvaluation) domain.DeliveryEvaluation {
	e.Triggers = slices.Clone(e.Triggers)
	if e.LeaseUntil != nil {
		until := *e.LeaseUntil
		e.LeaseUntil = &until
	}

	return e
}

// Fire implements ports.Intake: fires is the trigger lock, which views made with WithKey share,
// as goalds share one database.
func (in *Intake) Fire(ctx context.Context, f domain.TriggerFiring, item *domain.NewItem, limits domain.FireLimits) (domain.TriggerFiring, bool, error) {
	if err := f.Check(); err != nil {
		return domain.TriggerFiring{}, false, err
	}

	in.d.fires.Lock()
	defer in.d.fires.Unlock()

	if got, ok, err := in.Firing(ctx, f.FactoryID, f.Trigger, f.IdempotencyKey); err != nil || ok {
		return got, false, err
	}

	if item != nil && f.Outcome == domain.FiringCreated {
		if err := in.fileFiring(ctx, &f, *item, limits); err != nil {
			return domain.TriggerFiring{}, false, err
		}
	}

	if f.Outcome == domain.FiringChecked {
		live, err := in.liveCheck(ctx, f.FactoryID, f.Trigger)
		if err != nil {
			return domain.TriggerFiring{}, false, err
		}

		switch {
		case live && f.Try:
			return domain.TriggerFiring{}, false, fmt.Errorf("try %s: %s: %w", f.Trigger, domain.StillChecking, ports.ErrConflict)
		case live:
			f.Outcome, f.CheckState, f.Reason = domain.FiringSkipped, "", domain.StillChecking
		}
	}

	return in.RecordFiring(ctx, f)
}

// fileFiring reads the trigger's counts and files item for f, or makes f skipped by limits.
func (in *Intake) fileFiring(ctx context.Context, f *domain.TriggerFiring, item domain.NewItem, limits domain.FireLimits) error {
	{
		var c domain.FireCounts

		open, _, err := in.OpenItemByDedupe(ctx, f.FactoryID, f.Trigger, f.Dedupe)
		if err != nil {
			return err
		}

		c.OpenDedupe = open

		if c.Today, err = in.CreatedSince(ctx, f.FactoryID, f.Trigger, f.At.UTC().Truncate(24*time.Hour)); err != nil {
			return err
		}

		if c.LastHour, err = in.CreatedSince(ctx, f.FactoryID, f.Trigger, f.At.Add(-time.Hour)); err != nil {
			return err
		}

		if why := limits.Skip(c); why != "" {
			f.Outcome, f.Reason = domain.FiringSkipped, why
		} else {
			it, err := in.store.CreateItem(ctx, item)
			if err != nil {
				return err
			}

			f.ItemID = it.ID
		}
	}

	return nil
}

// keepReceivedLocked keeps the row of a delivery the receiver accepted or refused.
func (in *Intake) keepReceivedLocked(org domain.OrgID, n domain.NewReceipt) (domain.WebhookDeliveryID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}

	outcome := domain.DeliveryAccepted
	if n.Receipt == domain.ReceiptRefused {
		outcome = domain.DeliveryRefused
	}

	d := domain.WebhookDelivery{
		ID: domain.WebhookDeliveryID(id.String()), EndpointID: n.WebhookID, ReceivedAt: n.ReceivedAt.UTC(),
		Headers: domain.StoredHeaders(n.Headers), Body: append([]byte{}, n.Body...), Outcome: outcome, Reason: n.Reason,
		DeliveryID: n.DeliveryID, Event: n.Event, Receipt: n.Receipt,
	}
	in.d.deliveries = append(in.d.deliveries, deliveryRow{org: org, d: d})

	return d.ID, nil
}

// UnnamedDays implements ports.Intake.
func (in *Intake) UnnamedDays(ctx context.Context, webhook domain.WebhookEndpointID, since time.Time) ([]domain.DeliveryCount, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	out := []domain.DeliveryCount{}

	for k, n := range in.d.counts {
		if k.org == org && k.webhook == webhook && !k.day.Before(utcDay(since)) {
			out = append(out, domain.DeliveryCount{WebhookID: webhook, Day: k.day, Count: n})
		}
	}

	slices.SortFunc(out, func(a, b domain.DeliveryCount) int { return a.Day.Compare(b.Day) })

	return out, nil
}

// RecordFiring implements ports.Intake.
func (in *Intake) RecordFiring(ctx context.Context, f domain.TriggerFiring) (domain.TriggerFiring, bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	if err := f.Check(); err != nil {
		return domain.TriggerFiring{}, false, err
	}

	if !in.ownsFactory(org, f.FactoryID) {
		return domain.TriggerFiring{}, false, fmt.Errorf("factory %s: %w", f.FactoryID, ports.ErrNotFound)
	}

	if f.ItemID != "" && !in.ownsItem(org, f.ItemID) {
		return domain.TriggerFiring{}, false, fmt.Errorf("item %s: %w", f.ItemID, ports.ErrNotFound)
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	for _, r := range in.d.firings {
		if r.org == org && r.f.FactoryID == f.FactoryID && r.f.Trigger == f.Trigger && r.f.IdempotencyKey == f.IdempotencyKey {
			return r.f, false, nil
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	f.ID, f.At = id.String(), f.At.UTC().Truncate(time.Microsecond)
	in.d.firings = append(in.d.firings, firingRow{org: org, f: f})

	return f, true, nil
}

// Firing implements ports.Intake.
func (in *Intake) Firing(ctx context.Context, factory domain.FactoryID, trigger, key string) (domain.TriggerFiring, bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	for _, r := range in.d.firings {
		if r.org == org && r.f.FactoryID == factory && r.f.Trigger == trigger && r.f.IdempotencyKey == key {
			return r.f, true, nil
		}
	}

	return domain.TriggerFiring{}, false, nil
}

// FailureStreak implements ports.Intake.
func (in *Intake) FailureStreak(ctx context.Context, factory domain.FactoryID, trigger, dedupe string) (int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	var mine []domain.TriggerFiring

	for _, r := range in.d.firings {
		if r.org == org && r.f.FactoryID == factory && r.f.Trigger == trigger && r.f.Dedupe == dedupe {
			mine = append(mine, r.f)
		}
	}

	slices.SortFunc(mine, func(a, b domain.TriggerFiring) int { return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.ID, b.ID)) })

	n := 0

	for _, f := range mine {
		if f.Outcome == domain.FiringCreated {
			n = 0
		} else {
			n++
		}
	}

	return n, nil
}

// WithScheduleLock implements ports.Intake. Views made with WithKey share the lock, as goalds
// share one database.
func (in *Intake) WithScheduleLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error) {
	if !in.d.schedules.TryLock() {
		return false, nil
	}
	defer in.d.schedules.Unlock()

	return true, fn(ctx)
}

// CreatedSince implements ports.Intake.
func (in *Intake) CreatedSince(ctx context.Context, factory domain.FactoryID, trigger string, since time.Time) (int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	n := 0

	for _, r := range in.d.firings {
		if r.org == org && r.f.FactoryID == factory && r.f.Trigger == trigger && r.f.Outcome == domain.FiringCreated && !r.f.At.Before(since) {
			n++
		}
	}

	return n, nil
}

// OpenItemByDedupe implements ports.Intake.
func (in *Intake) OpenItemByDedupe(ctx context.Context, factory domain.FactoryID, trigger, value string) (domain.ItemID, bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return "", false, err
	}

	if value == "" {
		return "", false, nil
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	var (
		found domain.ItemID
		at    time.Time
		id    string
	)

	for _, r := range in.d.firings {
		f := r.f
		if r.org != org || f.FactoryID != factory || f.Trigger != trigger || f.Dedupe != value || !in.openLocked(org, f.ItemID) {
			continue
		}

		if found == "" || cmp.Or(f.At.Compare(at), cmp.Compare(f.ID, id)) > 0 {
			found, at, id = f.ItemID, f.At, f.ID
		}
	}

	return found, found != "", nil
}

// SourceTally implements ports.Intake.
func (in *Intake) SourceTally(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.SourceCount, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	counts := map[domain.ItemSource]int{}

	for _, it := range in.items(org, factory, since) {
		counts[it.Source]++
	}

	out := []domain.SourceCount{}
	for _, s := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, domain.SourceCount{Source: s, Items: counts[s]})
	}

	return out, nil
}

// TriggerStats implements ports.Intake.
func (in *Intake) TriggerStats(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.TriggerStat, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	items := in.items(org, factory, since)

	in.store.mu.Lock()
	merged := map[domain.ItemID]bool{}

	for _, e := range in.store.events {
		if e.Kind == domain.EventMerged {
			merged[e.ItemID] = true
		}
	}
	in.store.mu.Unlock()

	stats := map[string]*domain.TriggerStat{}

	for _, it := range items {
		if it.Trigger == "" {
			continue
		}

		s, ok := stats[it.Trigger]
		if !ok {
			s = &domain.TriggerStat{Trigger: it.Trigger}
			stats[it.Trigger] = s
		}

		s.Created++
		s.SpendUSD += it.CostUSD

		if merged[it.ID] {
			s.Merged++
		}
	}

	out := []domain.TriggerStat{}
	for _, name := range slices.Sorted(maps.Keys(stats)) {
		out = append(out, *stats[name])
	}

	return out, nil
}

// ItemSources implements ports.Intake.
func (in *Intake) ItemSources(ctx context.Context, factory domain.FactoryID, last int) (domain.SourceWindow, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.SourceWindow{}, err
	}

	items := in.items(org, factory, time.Time{})
	slices.SortFunc(items, func(a, b domain.Item) int { return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID)) })
	items = items[:min(len(items), max(last, 0))]

	w := domain.SourceWindow{Counts: []domain.SourceCount{}}
	counts := map[domain.ItemSource]int{}

	for i, it := range items {
		counts[it.Source]++

		if i == 0 {
			w.To = it.CreatedAt
		}

		w.From = it.CreatedAt
	}

	for _, src := range slices.Sorted(maps.Keys(counts)) {
		w.Counts = append(w.Counts, domain.SourceCount{Source: src, Items: counts[src]})
	}

	return w, nil
}

// OriginTally implements ports.Intake.
func (in *Intake) OriginTally(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.OriginCount, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	counts := map[domain.Origin]int{}
	for _, it := range in.items(org, factory, since) {
		counts[cmp.Or(it.Origin, domain.OriginPerson)]++
	}

	out := []domain.OriginCount{}
	for _, o := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, domain.OriginCount{Origin: o, Items: counts[o]})
	}

	return out, nil
}

// ComingUp implements ports.Intake.
func (in *Intake) ComingUp(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.ComingUp, int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, 0, err
	}

	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	var all []domain.ComingUp

	for _, it := range in.store.items {
		if it.OrgID != org || it.FactoryID != factory {
			continue
		}

		if c, ok := in.comingUpLocked(org, it); ok {
			all = append(all, c)
		}
	}

	slices.SortFunc(all, comingUpOrder)

	return all[:min(len(all), max(limit, 0))], len(all), nil
}

// comingUpLocked is it as coming up, when it is open, hasn't started and has a start or an open
// planned or linked need. Callers hold in.store.mu.
func (in *Intake) comingUpLocked(org domain.OrgID, it *domain.Item) (domain.ComingUp, bool) {
	if !it.StartedAt.IsZero() || it.State == domain.StateDone || it.State == domain.StateCancelled {
		return domain.ComingUp{}, false
	}

	c := domain.ComingUp{Item: clone(it), Needs: []int64{}}

	for _, e := range in.store.edges {
		if e.Item == it.ID && e.Kind != domain.EdgeOverlap && in.openLocked(org, e.Needs) {
			c.Needs = append(c.Needs, in.store.items[e.Needs].Number)
		}
	}

	if it.StartAt.IsZero() && len(c.Needs) == 0 {
		return domain.ComingUp{}, false
	}

	if !it.Triage {
		c.FromFactory, c.FromNumber = in.filedFromLocked(org, it.ID)
	}

	return c, true
}

// comingUpOrder puts items with a start first, soonest first, then the rest oldest first.
func comingUpOrder(a, b domain.ComingUp) int {
	x, y := a.Item, b.Item
	if x.StartAt.IsZero() != y.StartAt.IsZero() {
		if x.StartAt.IsZero() {
			return 1
		}

		return -1
	}

	return cmp.Or(x.StartAt.Compare(y.StartAt), x.CreatedAt.Compare(y.CreatedAt), cmp.Compare(x.ID, y.ID))
}

// filedFromLocked is the factory and number of the source item of the oldest follow-up filed as
// or onto item; "" and 0 for none. Callers hold in.store.mu.
func (in *Intake) filedFromLocked(org domain.OrgID, item domain.ItemID) (domain.FactoryID, int64) {
	var found *domain.FollowUp

	for i, f := range in.store.followUps {
		if f.FiledItem == item && f.State == domain.FollowUpFiled &&
			(found == nil || cmp.Or(f.CreatedAt.Compare(found.CreatedAt), cmp.Compare(f.ID, found.ID)) < 0) {
			found = &in.store.followUps[i]
		}
	}

	if found == nil {
		return "", 0
	}

	src, ok := in.store.visible(found.ItemID, org)
	if !ok {
		return "", 0
	}

	return src.FactoryID, src.Number
}

// TriageQueue implements ports.Intake.
func (in *Intake) TriageQueue(ctx context.Context, factory domain.FactoryID, limit int) (domain.TriageQueue, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.TriageQueue{}, err
	}

	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	q := domain.TriageQueue{Item: in.collectingLocked(org, factory)}

	if q.Item != nil {
		for _, f := range in.store.followUps {
			if f.FiledItem == q.Item.ID && f.State == domain.FollowUpFiled && f.Target == domain.FollowUpTriage {
				q.FollowUps++
			}
		}
	}

	q.Waiting = in.triageSourcesLocked(org, factory, limit)

	return q, nil
}

// collectingLocked is the factory's oldest open, trusted triage item without a run; nil for none.
// Callers hold in.store.mu.
func (in *Intake) collectingLocked(org domain.OrgID, factory domain.FactoryID) *domain.Item {
	var found *domain.Item

	for _, it := range in.store.items {
		if it.OrgID != org || it.FactoryID != factory || !it.Triage || it.Untrusted || !in.openLocked(org, it.ID) || in.hasRunLocked(it.ID) {
			continue
		}

		if found == nil || cmp.Or(it.CreatedAt.Compare(found.CreatedAt), cmp.Compare(it.ID, found.ID)) < 0 {
			c := clone(it)
			found = &c
		}
	}

	return found
}

// triageSourcesLocked are at most limit of the factory's trusted items with waiting triage
// follow-ups, oldest first by their first one. Callers hold in.store.mu.
func (in *Intake) triageSourcesLocked(org domain.OrgID, factory domain.FactoryID, limit int) []domain.TriageSource {
	type source struct {
		ts    domain.TriageSource
		first domain.FollowUp
	}

	sources := map[domain.ItemID]*source{}

	for _, f := range in.store.followUps {
		if f.FactoryID != factory || f.State != domain.FollowUpWaiting || f.Target != domain.FollowUpTriage {
			continue
		}

		src, ok := in.store.visible(f.ItemID, org)
		if !ok || src.Untrusted {
			continue
		}

		s, ok := sources[src.ID]
		if !ok {
			s = &source{ts: domain.TriageSource{ID: src.ID, Number: src.Number, Title: src.Title}, first: f}
			sources[src.ID] = s
		}

		s.ts.FollowUps++
	}

	all := slices.Collect(maps.Values(sources))
	slices.SortFunc(all, func(a, b *source) int {
		return cmp.Or(a.first.CreatedAt.Compare(b.first.CreatedAt), cmp.Compare(a.ts.ID, b.ts.ID))
	})

	out := []domain.TriageSource{}
	for _, s := range all[:min(len(all), max(limit, 0))] {
		out = append(out, s.ts)
	}

	return out
}

// hasRunLocked reports whether item has a run. Callers hold in.store.mu.
func (in *Intake) hasRunLocked(item domain.ItemID) bool {
	for _, r := range in.store.runs {
		if r.ItemID == item {
			return true
		}
	}

	return false
}

// TriggerDays implements ports.Intake.
func (in *Intake) TriggerDays(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.TriggerDay, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	type key struct {
		trigger string
		day     time.Time
	}

	counts := map[key]int{}

	in.d.mu.Lock()
	for _, r := range in.d.firings {
		if r.org == org && r.f.FactoryID == factory && r.f.Outcome == domain.FiringCreated && !r.f.At.Before(since) {
			counts[key{r.f.Trigger, r.f.At.UTC().Truncate(24 * time.Hour)}]++
		}
	}
	in.d.mu.Unlock()

	out := []domain.TriggerDay{}
	for k, n := range counts {
		out = append(out, domain.TriggerDay{Trigger: k.trigger, Day: k.day, Created: n})
	}

	slices.SortFunc(out, func(a, b domain.TriggerDay) int {
		return cmp.Or(cmp.Compare(a.Trigger, b.Trigger), a.Day.Compare(b.Day))
	})

	return out, nil
}

// Firings implements ports.Intake.
func (in *Intake) Firings(ctx context.Context, factory domain.FactoryID, trigger string, limit int) ([]domain.FiringEntry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	var mine []domain.TriggerFiring

	in.d.mu.Lock()

	for _, r := range in.d.firings {
		if r.org == org && r.f.FactoryID == factory && r.f.Trigger == trigger {
			mine = append(mine, r.f)
		}
	}
	in.d.mu.Unlock()

	slices.SortFunc(mine, func(a, b domain.TriggerFiring) int { return cmp.Or(b.At.Compare(a.At), cmp.Compare(b.ID, a.ID)) })
	mine = mine[:min(len(mine), max(limit, 0))]

	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	merged := map[domain.ItemID]bool{}

	for _, e := range in.store.events {
		if e.Kind == domain.EventMerged {
			merged[e.ItemID] = true
		}
	}

	out := make([]domain.FiringEntry, 0, len(mine))

	for _, f := range mine {
		e := domain.FiringEntry{Firing: f}

		if it, ok := in.store.visible(f.ItemID, org); ok && f.ItemID != "" {
			e.ItemNumber, e.Merged = it.Number, merged[f.ItemID]
		}

		out = append(out, e)
	}

	return out, nil
}

// PruneFirings implements ports.Intake.
func (in *Intake) PruneFirings(ctx context.Context, factory domain.FactoryID, before time.Time) (int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	in.d.mu.Lock()
	old := slices.Clone(in.d.firings)
	in.d.mu.Unlock()

	open := map[domain.ItemID]bool{}

	in.store.mu.Lock()
	for _, r := range old {
		if r.f.ItemID != "" {
			open[r.f.ItemID] = in.openLocked(r.org, r.f.ItemID)
		}
	}
	in.store.mu.Unlock()

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	n := len(in.d.firings)
	in.d.firings = slices.DeleteFunc(in.d.firings, func(r firingRow) bool {
		return r.org == org && r.f.FactoryID == factory && r.f.At.Before(before) && !open[r.f.ItemID] && !r.f.CheckState.Live()
	})

	return n - len(in.d.firings), nil
}

// items are copies of org's items of factory created at or after since.
func (in *Intake) items(org domain.OrgID, factory domain.FactoryID, since time.Time) []domain.Item {
	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	var out []domain.Item

	for _, it := range in.store.items {
		if it.OrgID == org && it.FactoryID == factory && !it.CreatedAt.Before(since) {
			out = append(out, clone(it))
		}
	}

	return out
}

// openLocked reports whether item is org's and neither done nor cancelled. Callers hold
// in.store.mu.
func (in *Intake) openLocked(org domain.OrgID, item domain.ItemID) bool {
	it, ok := in.store.visible(item, org)

	return ok && it.State != domain.StateDone && it.State != domain.StateCancelled
}

func (in *Intake) ownsFactory(org domain.OrgID, factory domain.FactoryID) bool {
	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	owner, ok := in.store.factories[factory]

	return ok && owner == org
}

func (in *Intake) ownsItem(org domain.OrgID, id domain.ItemID) bool {
	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	_, ok := in.store.visible(id, org)

	return ok
}

// endpointLocked is org's endpoint id; ErrNotFound for none. Callers hold in.d.mu.
func (in *Intake) endpointLocked(org domain.OrgID, id domain.WebhookEndpointID) (*endpointRow, error) {
	r, ok := in.d.endpoints[id]
	if !ok || r.org != org {
		return nil, fmt.Errorf("endpoint %s: %w", id, ports.ErrNotFound)
	}

	return r, nil
}

// sealLocked gives r a new secret sealed under the active key and returns it in the clear.
func (in *Intake) sealLocked(r *endpointRow) ([]byte, error) {
	if in.key.Active() == "" {
		return nil, ports.ErrNoMasterKey
	}

	secret := make([]byte, domain.WebhookSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("new signing secret: %w", err)
	}

	keyID, nonce, ct, err := in.key.Seal(secret, domain.WebhookSecretAAD(r.org, r.e.ID, in.key.Active()))
	if err != nil {
		return nil, err
	}

	r.keyID, r.nonce, r.ciphertext = keyID, nonce, ct

	return secret, nil
}

// reaches reports whether DeliveryDays of factory counts e's deliveries: those open to it, or the
// organization's webhooks for "".
func reaches(e domain.WebhookEndpoint, factory domain.FactoryID) bool {
	if factory == "" {
		return e.OrgLevel()
	}

	return e.OpenTo(factory)
}

func cloneEndpoint(e domain.WebhookEndpoint) domain.WebhookEndpoint {
	if e.RotatedAt != nil {
		at := *e.RotatedAt
		e.RotatedAt = &at
	}

	if e.Openings != nil {
		openings := make([]domain.WebhookOpening, 0, len(e.Openings))
		for _, o := range e.Openings {
			openings = append(openings, domain.WebhookOpening{FactoryID: o.FactoryID, Repositories: slices.Clone(o.Repositories)})
		}

		e.Openings = openings
	}

	return e
}

func cloneDelivery(d domain.WebhookDelivery) domain.WebhookDelivery {
	d.Headers = maps.Clone(d.Headers)
	d.Body = slices.Clone(d.Body)

	return d
}
