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
	// ListEndpoints are the endpoints open to the factory, by name: its own and the
	// organization's webhooks open to it.
	ListEndpoints(ctx context.Context, factory domain.FactoryID) ([]domain.WebhookEndpoint, error)
	// Endpoint is endpoint id, a factory's endpoint or the organization's webhook. ErrNotFound
	// when there is none.
	Endpoint(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookEndpoint, error)
	// CreateWebhook creates the organization's webhook n (ADR 0040 decision 12), enabled and open
	// to no factory, signed by the existing secret n.Secret. n must pass Check
	// (domain.ErrInvalid); ErrNameTaken for a name an endpoint of the organization has,
	// ErrNotFound for a secret the organization does not have.
	CreateWebhook(ctx context.Context, n domain.NewWebhook) (domain.WebhookEndpoint, error)
	// Webhooks are the organization's webhooks, by name, each with its openings.
	Webhooks(ctx context.Context) ([]domain.WebhookEndpoint, error)
	// WebhookByName is the organization's webhook name. ErrNotFound when there is none.
	WebhookByName(ctx context.Context, name string) (domain.WebhookEndpoint, error)
	// PatchWebhook applies p to the organization's webhook id. p must pass Check
	// (domain.ErrInvalid); ErrNotFound for no such webhook (a factory's endpoint included) or a
	// factory the organization does not have, ErrNameTaken for a name another endpoint has.
	PatchWebhook(ctx context.Context, id domain.WebhookEndpointID, p domain.WebhookPatch) (domain.WebhookEndpoint, error)
	// WebhookSecretUses are the secrets the organization's webhooks are signed by, by secret then
	// webhook.
	WebhookSecretUses(ctx context.Context) ([]domain.WebhookSecretUse, error)
	// ConvertEndpoints moves each of the organization's factory endpoints to the organization
	// (ADR 0040 decision 15), oldest first, one transaction each. Its sealed secret becomes a new
	// secret open to no factory, audited as created by the actor given and named
	// domain.ConvertedSecretName, whose value is the hex the endpoint showed when it was made. The endpoint takes
	// domain.ConvertedWebhookName (a name is taken by the organization's webhooks and by older
	// factory endpoints), its kind's sender and that secret, and opens to its factory with every
	// repository. It returns what it converted, in that order; a second call finds nothing.
	// ErrNoMasterKey without a master key when there is an endpoint to convert.
	ConvertEndpoints(ctx context.Context, by domain.Actor) ([]domain.ConvertedEndpoint, error)
	// RotateSecret replaces the factory endpoint's secret with a new one, returned once, and sets
	// RotatedAt. ErrNotFound when there is none, ErrNoMasterKey without a master key; an
	// organization webhook's secret is replaced through OrgSecrets (domain.ErrInvalid).
	RotateSecret(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookSecret, error)
	// DeleteEndpoint deletes the endpoint and its deliveries. ErrNotFound when there is none.
	DeleteEndpoint(ctx context.Context, id domain.WebhookEndpointID) error
	// EndpointOrg is the organization of endpoint id, whatever organization ctx holds or none:
	// POST /hooks/{id} carries no session. ErrNotFound when there is none.
	EndpointOrg(ctx context.Context, id domain.WebhookEndpointID) (domain.OrgID, error)
	// OpenSecret is the factory endpoint's secret in the clear, for checking a signature.
	// ErrNotFound when there is none (an organization webhook's opens with
	// OrgSecrets.OpenSigning), ErrNoMasterKey without a master key.
	OpenSecret(ctx context.Context, id domain.WebhookEndpointID) ([]byte, error)
	// RecordDelivery records a delivery with domain.StoredHeaders of its headers. n must pass
	// Check (domain.ErrInvalid); ErrNotFound for an unknown endpoint.
	RecordDelivery(ctx context.Context, n domain.NewWebhookDelivery) (domain.WebhookDelivery, error)
	// ListDeliveries are at most limit of the endpoint's deliveries after the cursor, newest
	// first (received_at, then id).
	ListDeliveries(ctx context.Context, endpoint domain.WebhookEndpointID, after domain.DeliveryCursor, limit int) ([]domain.WebhookDelivery, error)
	// Delivery is the endpoint's delivery id, with its headers and body. ErrNotFound when the
	// endpoint has none with that id.
	Delivery(ctx context.Context, endpoint domain.WebhookEndpointID, id domain.WebhookDeliveryID) (domain.WebhookDelivery, error)
	// DeliveryBySender is the webhook's kept delivery whose sender's id is deliveryID
	// (domain.WebhookDelivery.DeliveryID), with its body, for trying a trigger on it. ErrNotFound
	// when the webhook keeps none with that id: never received, refused, or pruned.
	DeliveryBySender(ctx context.Context, webhook domain.WebhookEndpointID, deliveryID string) (domain.WebhookDelivery, error)
	// DeliveryDays counts the deliveries of the endpoints open to the factory (ListEndpoints), or
	// of the organization's webhooks for factory "", received at or after since,
	// per endpoint and UTC day, by endpoint then day; a day without deliveries has no entry.
	DeliveryDays(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.DeliveryDay, error)
	// PruneDeliveries deletes, in every organization, the deliveries received more than
	// domain.DeliveryRetention before now, and the ledger ids and daily counts more than
	// domain.LedgerRetention before it, and returns how many rows went; ctx needs no organization.
	PruneDeliveries(ctx context.Context, now time.Time) (int, error)
	// Receive records what the receiver of POST /hooks/{webhook id} made of a delivery to the
	// organization's webhook n.WebhookID (ADR 0040 decisions 12 and 13), in one transaction. A
	// refused one keeps a row of its metadata, without an id, event or body. An accepted one's id
	// goes into the webhook's ledger. When the ledger already holds it, nothing is created: the
	// row the id first came with, while it is kept, counts one more duplicate, and the receipt is
	// domain.ReceiptDuplicate with that row. Otherwise a delivery n.Named keeps its row and body
	// (for domain.DeliveryRetention), and one no enabled trigger names only adds one to its UTC
	// day's count (UnnamedDays). A kept delivery adds n.Evaluations in the same transaction (ADR
	// 0040 decision 13): pending, or done and skipped at once with its Skip reason. n must pass
	// Check (domain.ErrInvalid); ErrNotFound for an unknown webhook.
	Receive(ctx context.Context, n domain.NewReceipt) (domain.Received, error)
	// DeliveryEvaluations are the evaluations of the delivery row, by factory.
	DeliveryEvaluations(ctx context.Context, delivery domain.WebhookDeliveryID) ([]domain.DeliveryEvaluation, error)
	// DeliveryEvaluationsOf are the evaluations of the delivery rows, by delivery then factory,
	// each with its item's number while that item exists: a page of the delivery log at once.
	DeliveryEvaluationsOf(ctx context.Context, deliveries []domain.WebhookDeliveryID) ([]domain.DeliveryEvaluation, error)
	// ClaimEvaluations claims at most limit of the organization's pending evaluations due at now
	// (next attempt at or before now, no lease live at now), soonest first, with FOR UPDATE SKIP
	// LOCKED as the notification outbox does: each is leased to holder until now+lease with one
	// more attempt, and comes with its delivery's body. A row another instance holds is skipped;
	// one whose lease ran out is claimed again.
	ClaimEvaluations(ctx context.Context, holder string, now time.Time, lease time.Duration, limit int) ([]domain.ClaimedEvaluation, error)
	// FinishEvaluation ends f's claim as f says and reports true, or reports false and changes
	// nothing when the row is no longer pending under f.Holder at f.Attempts (a later claim took
	// it).
	FinishEvaluation(ctx context.Context, f domain.EvaluationFinish) (bool, error)
	// Fire records firing f of a trigger in one transaction under the trigger's lock (a
	// transaction advisory lock on the factory and trigger, ADR 0040 decision 16), so limits hold
	// across goald instances. When the trigger already has a firing with f's idempotency key, it
	// returns that one and false and writes nothing. Otherwise, for f created with item, it reads
	// the open item of the trigger with f's dedupe value and the items it created since the UTC
	// day began and in the hour before f.At, and when limits skip them (domain.FireLimits.Skip)
	// records f skipped with that reason; else it files item as CreateItem does and records f
	// with it. A skipped f with no item is recorded as it is. A checked f (queued, ADR 0040
	// decision 19) is recorded as it is unless the trigger already has a check queued or
	// running, when it is recorded skipped with domain.StillChecking, or, for a try check
	// (f.Try), not recorded at all: ErrConflict. f must pass Check (domain.ErrInvalid).
	Fire(ctx context.Context, f domain.TriggerFiring, item *domain.NewItem, limits domain.FireLimits) (domain.TriggerFiring, bool, error)
	// UnnamedDays are the webhook's daily counts of accepted deliveries no enabled trigger named,
	// from the UTC day of since on, by day; a day without one has no entry.
	UnnamedDays(ctx context.Context, webhook domain.WebhookEndpointID, since time.Time) ([]domain.DeliveryCount, error)
	// RecordFiring records f (with a new ID) and reports true, or reports false and records
	// nothing when the factory's trigger already has a firing with f's idempotency key (ADR 0026
	// decision 4); the firing returned is then the one recorded first. f must pass Check
	// (domain.ErrInvalid).
	RecordFiring(ctx context.Context, f domain.TriggerFiring) (domain.TriggerFiring, bool, error)
	// Firing is the factory's trigger's firing with idempotency key key; false when there is none.
	Firing(ctx context.Context, factory domain.FactoryID, trigger, key string) (domain.TriggerFiring, bool, error)
	// FailureStreak counts the trigger's firings whose dedupe value is dedupe ("" for those
	// without one) recorded after its newest firing with that value that created an item, or all
	// of them when none did (only.failuresInARow).
	FailureStreak(ctx context.Context, factory domain.FactoryID, trigger, dedupe string) (int, error)
	// WithScheduleLock runs fn and reports true when it takes the install's schedule lock, which
	// at most one caller holds at a time across every goald (ADR 0026 decision 4), and reports
	// false without running fn while another holds it. The lock is freed when fn returns. ctx
	// needs no organization.
	WithScheduleLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error)
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
	// ItemSources counts the factory's newest last items (by created_at, then id) by source, by
	// source, with when the oldest and the newest of them were created.
	ItemSources(ctx context.Context, factory domain.FactoryID, last int) (domain.SourceWindow, error)
	// OriginTally counts the factory's items created at or after since by origin, by origin; an
	// origin without items has no entry.
	OriginTally(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.OriginCount, error)
	// ComingUp is at most limit of the factory's open items (neither done nor cancelled) that
	// haven't started (no StartedAt, as the claim filters read it) and have a start or an open
	// planned or linked need: those with a start first, soonest first, then the rest oldest first
	// (created_at, then id), with how many there are in all. Each has its open needs' numbers and
	// the item whose follow-up filed it (none for a triage item). An assembly's parts (items with
	// a ParentID) are left out, of the items and of the count: their plan orders them, and their
	// assembly's page shows each part and what it waits for.
	ComingUp(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.ComingUp, int, error)
	// TriageQueue is the factory's collecting triage item (open, triage, trusted, no run yet; the
	// oldest), with how many follow-ups joined it, and at most limit of its trusted items with
	// waiting triage follow-ups, oldest first by their first one, each with how many.
	TriageQueue(ctx context.Context, factory domain.FactoryID, limit int) (domain.TriageQueue, error)
	// TriggerDays count the factory's firings that created an item at or after since, per trigger
	// and UTC day, by trigger then day; days without one are left out.
	TriggerDays(ctx context.Context, factory domain.FactoryID, since time.Time) ([]domain.TriggerDay, error)
	// Firings are at most limit of the trigger's firings, newest first (at, then id), each with
	// its item's number and whether that item has a merged event.
	Firings(ctx context.Context, factory domain.FactoryID, trigger string, limit int) ([]domain.FiringEntry, error)
	// PruneFirings deletes the factory's firings from before before, except those whose item is
	// still open (neither done nor cancelled), which only.dedupe reads, and checks still queued or
	// running, and returns how many went (ADR 0040 decision 16).
	PruneFirings(ctx context.Context, factory domain.FactoryID, before time.Time) (int, error)
	// ClaimCheck leases the factory's oldest queued check (ADR 0040 decision 19) to holder until
	// now+lease with a new token, FOR UPDATE SKIP LOCKED, and returns its firing, now running,
	// with the lease. A running check takes a slot as a lease does: the claim checks the
	// organization's and the factory's slot caps first, counting leases and running checks, and
	// returns ErrOrgFull or ErrFactoryFull when one is reached; ErrNotFound when none is queued.
	ClaimCheck(ctx context.Context, factory domain.FactoryID, holder string, now time.Time, lease time.Duration) (domain.TriggerFiring, domain.CheckLease, error)
	// HeartbeatCheck extends l's check to until; ErrLeaseLost when it no longer runs under l's
	// token.
	HeartbeatCheck(ctx context.Context, l domain.CheckLease, until time.Time) error
	// FinishCheck ends l's check as r says (r must pass Check, domain.ErrInvalid), in one
	// transaction under the trigger's lock (Fire's): its state, run, cost, decisions or failure
	// reason. A done r's answer (its Decisions) is applied as domain.ApplyCheck decides from the
	// trigger's keys (each key's last item, locked in id order, and last note), r.Items, r.Limits
	// and the items the trigger filed since the UTC day began: each filed item is filed as
	// CreateItem does and becomes its key's last item, each note is an item_note on its item
	// (domain.CheckNoteData) and its key's last note, the trigger's state is r.Saved when set, and
	// the firing keeps what was applied. A try check's finish keeps only what would be applied,
	// with no item refs for what it would file, and writes nothing else. A failed r leaves the
	// keys and the state as they were. ErrLeaseLost when the check no longer runs under l's token.
	FinishCheck(ctx context.Context, l domain.CheckLease, r domain.CheckResult) error
	// ExpireChecks fails the factory's running checks whose lease ended by now, with
	// domain.CheckLeaseExpired, and returns how many.
	ExpireChecks(ctx context.Context, factory domain.FactoryID, now time.Time) (int, error)
	// CheckInput is what the trigger's next check is given: its open items, newest first, at most
	// domain.MaxCheckOpenItems; the keys whose items closed since now-domain.CheckClosedWindow,
	// newest first, at most domain.MaxCheckClosed; its newest done check's decisions, tries left
	// out; and the state saved for it. An item's key is its trigger_keys key, else the dedupe
	// value of the firing that filed it.
	CheckInput(ctx context.Context, factory domain.FactoryID, trigger string, now time.Time) (domain.CheckInput, error)
	// CheckMemory is what the trigger carries from one check to the next (domain.CheckMemory), in
	// one read: its open items and the keys whose items closed since now-domain.CheckClosedWindow,
	// each counted in full and listed as CheckInput reads them up to domain.MaxCheckMemoryItems,
	// and its saved state with when it was saved. Lists are empty, never nil.
	CheckMemory(ctx context.Context, factory domain.FactoryID, trigger string, now time.Time) (domain.CheckMemory, error)
	// ClearCheckState deletes the trigger's saved state, so its next check starts with none, and
	// keeps its keys, which still stop a closed key from being filed again. It holds the trigger's
	// lock, as FinishCheck does; ErrConflict while the trigger's check is queued or running. Clearing
	// no state is not an error.
	ClearCheckState(ctx context.Context, factory domain.FactoryID, trigger string) error
	// ItemCheckKey is the key a check filed item under (trigger_keys), or "" when no key's last
	// item is item: an item filed without a check, one whose key a later check filed again, an
	// unknown item, or another organization's.
	ItemCheckKey(ctx context.Context, item domain.ItemID) (string, error)
	// TriggerChecks are at most limit of the trigger's checked firings, tries among them, newest
	// first (at, then id), each with what it applied once done.
	TriggerChecks(ctx context.Context, factory domain.FactoryID, trigger string, limit int) ([]domain.TriggerFiring, error)
	// CheckAlerts are what of the factories' checks Needs you shows (ADR 0040 decision 19), tries
	// left out: a trigger whose last domain.CheckFailuresShown finished checks all failed
	// (domain.CheckFailing, at the newest one, with its reason), and a trigger with a done check
	// at or after since that skipped something domain.CheckOverLimit (domain.CheckLimited, at the
	// first such check). Ordered by factory, trigger and kind.
	CheckAlerts(ctx context.Context, factories []domain.FactoryID, since time.Time) ([]domain.CheckAlert, error)
}

// SenderEvents are the senders' event tables (ADR 0040 decision 14): which normalized fields an
// event gives, as dot paths into its payload. internal/catalog/senders satisfies it; false is an
// event its sender's entry has no table for.
type SenderEvents interface {
	EventTable(sender domain.WebhookSender, event string) (domain.EventTable, bool)
}
