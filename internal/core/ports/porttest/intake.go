package porttest

import (
	"cmp"
	"context"
	"crypto/rand"
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
	// schedules is the schedule lock (WithScheduleLock).
	schedules  sync.Mutex
	mu         sync.Mutex
	endpoints  map[domain.WebhookEndpointID]*endpointRow
	deliveries []deliveryRow
	firings    []firingRow
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

type firingRow struct {
	org domain.OrgID
	f   domain.TriggerFiring
}

// NewIntake returns intake state sealed with key, over store's factories and items.
func NewIntake(key ports.MasterKey, store *Store) *Intake {
	return &Intake{key: key, store: store, d: &intakeData{endpoints: map[domain.WebhookEndpointID]*endpointRow{}}}
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
		ID: domain.WebhookEndpointID(id.String()), FactoryID: n.FactoryID, Name: n.Name, Kind: n.Kind,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
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
		if r.org == org && r.e.FactoryID == factory {
			out = append(out, cloneEndpoint(r.e))
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

	return cloneEndpoint(r.e), nil
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

	if _, err := in.endpointLocked(org, id); err != nil {
		return err
	}

	delete(in.d.endpoints, id)
	in.d.deliveries = slices.DeleteFunc(in.d.deliveries, func(r deliveryRow) bool { return r.d.EndpointID == id })

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
		if r.org != org || !ok || e.e.FactoryID != factory || r.d.ReceivedAt.Before(since) {
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
func (in *Intake) PruneDeliveries(_ context.Context, before time.Time) (int, error) {
	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	n := len(in.d.deliveries)
	in.d.deliveries = slices.DeleteFunc(in.d.deliveries, func(r deliveryRow) bool { return r.d.ReceivedAt.Before(before) })

	return n - len(in.d.deliveries), nil
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

func cloneEndpoint(e domain.WebhookEndpoint) domain.WebhookEndpoint {
	if e.RotatedAt != nil {
		at := *e.RotatedAt
		e.RotatedAt = &at
	}

	return e
}

func cloneDelivery(d domain.WebhookDelivery) domain.WebhookDelivery {
	d.Headers = maps.Clone(d.Headers)
	d.Body = slices.Clone(d.Body)

	return d
}
