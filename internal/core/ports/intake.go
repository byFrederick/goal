package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Intake is the state of intake triggers (ADR 0026 decision 2), for the organization in ctx unless
// a method says otherwise: webhook endpoints with their signing secrets, the deliveries they
// received, the log of trigger firings, and what Intake shows about where items came from. A
// signing secret is generated here, sealed with MasterKey under domain.WebhookSecretAAD
// (organization, endpoint id and key id), and returned in the clear only by CreateEndpoint,
// RotateSecret and OpenSecret; no error holds it.
type Intake interface {
	// CreateEndpoint creates an endpoint with a new 32-byte secret, returned once. n must pass
	// Check (domain.ErrInvalid); ErrNameTaken for a name the factory already uses, ErrNotFound for
	// a factory the organization does not have, ErrNoMasterKey without a master key.
	CreateEndpoint(ctx context.Context, n domain.NewWebhookEndpoint) (domain.WebhookSecret, error)
	// ListEndpoints are the factory's endpoints, by name.
	ListEndpoints(ctx context.Context, factory domain.FactoryID) ([]domain.WebhookEndpoint, error)
	// Endpoint is endpoint id. ErrNotFound when there is none.
	Endpoint(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookEndpoint, error)
	// RotateSecret replaces the endpoint's secret with a new one, returned once, and sets
	// RotatedAt. ErrNotFound when there is none, ErrNoMasterKey without a master key.
	RotateSecret(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookSecret, error)
	// DeleteEndpoint deletes the endpoint and its deliveries. ErrNotFound when there is none.
	DeleteEndpoint(ctx context.Context, id domain.WebhookEndpointID) error
	// EndpointOrg is the organization of endpoint id, whatever organization ctx holds or none:
	// POST /hooks/{id} carries no session. ErrNotFound when there is none.
	EndpointOrg(ctx context.Context, id domain.WebhookEndpointID) (domain.OrgID, error)
	// OpenSecret is the endpoint's secret in the clear, for checking a signature. ErrNotFound
	// when there is none, ErrNoMasterKey without a master key.
	OpenSecret(ctx context.Context, id domain.WebhookEndpointID) ([]byte, error)
	// RecordDelivery records a delivery with domain.StoredHeaders of its headers. n must pass
	// Check (domain.ErrInvalid); ErrNotFound for an unknown endpoint.
	RecordDelivery(ctx context.Context, n domain.NewWebhookDelivery) (domain.WebhookDelivery, error)
	// ListDeliveries are at most limit of the endpoint's deliveries after the cursor, newest
	// first (received_at, then id).
	ListDeliveries(ctx context.Context, endpoint domain.WebhookEndpointID, after domain.DeliveryCursor, limit int) ([]domain.WebhookDelivery, error)
	// PruneDeliveries deletes the deliveries of every organization received before before, and
	// returns how many; ctx needs no organization.
	PruneDeliveries(ctx context.Context, before time.Time) (int, error)
	// RecordFiring records f (with a new ID) and reports true, or reports false and records
	// nothing when the factory's trigger already has a firing with f's idempotency key (ADR 0026
	// decision 4); the firing returned is then the one recorded first. f must pass Check
	// (domain.ErrInvalid).
	RecordFiring(ctx context.Context, f domain.TriggerFiring) (domain.TriggerFiring, bool, error)
	// CreatedSince counts the trigger's firings that created an item at or after since
	// (limits.perDay).
	CreatedSince(ctx context.Context, factory domain.FactoryID, trigger string, since time.Time) (int, error)
	// OpenItemByDedupe is the newest item the trigger created from an event whose dedupe value is
	// value and that is neither done nor cancelled (only.dedupe); false when there is none.
	OpenItemByDedupe(ctx context.Context, factory domain.FactoryID, trigger, value string) (domain.ItemID, bool, error)
	// SourceTally counts the factory's items created at or after since by source, by source.
	SourceTally(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.SourceCount, error)
	// TriggerStats are, for each trigger with items in the factory created at or after since, by
	// trigger name: how many, how many of those have a merged event, and the sum of their cost.
	TriggerStats(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.TriggerStat, error)
}
