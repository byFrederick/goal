package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// WebhookEndpointID is a webhook endpoint's id (a UUID), the only thing in its address
// POST /hooks/{id} (ADR 0026 decision 3).
type WebhookEndpointID string

// WebhookKind is how an endpoint's requests are signed and name their event (ADR 0026 decision 3).
type WebhookKind string

const (
	// WebhookGeneric takes X-Goal-Signature and an event name in the body's event field.
	WebhookGeneric WebhookKind = "generic"
	// WebhookGitHub takes GitHub's X-Hub-Signature-256 and X-GitHub-Event.
	WebhookGitHub WebhookKind = "github"
)

// WebhookSecretSize is the length in bytes of a signing secret goald generates.
const WebhookSecretSize = 32

// WebhookEndpoint is one webhook endpoint of a factory (ADR 0026 decision 2). Its signing secret
// is sealed on its row and is never part of it.
type WebhookEndpoint struct {
	ID        WebhookEndpointID
	FactoryID FactoryID
	// Name is a slug unique within the factory, the one a trigger's on: webhook.<name> names.
	Name      string
	Kind      WebhookKind
	CreatedAt time.Time
	// RotatedAt is when its secret was last replaced; nil when it never was.
	RotatedAt *time.Time
}

// NewWebhookEndpoint is what creates an endpoint.
type NewWebhookEndpoint struct {
	FactoryID FactoryID
	Name      string
	Kind      WebhookKind
}

// Check refuses a name that is not a slug or an unknown kind; the error wraps ErrInvalid.
func (n NewWebhookEndpoint) Check() error {
	if n.FactoryID == "" {
		return fmt.Errorf("an endpoint needs a factory: %w", ErrInvalid)
	}

	if err := CheckConnectionName(n.Name); err != nil {
		return err
	}

	if n.Kind != WebhookGeneric && n.Kind != WebhookGitHub {
		return fmt.Errorf("endpoint kind %q must be generic or github: %w", n.Kind, ErrInvalid)
	}

	return nil
}

// WebhookSecret is an endpoint with its signing secret in the clear: only the answers that create
// an endpoint and rotate its secret hold one (ADR 0026 decision 2).
type WebhookSecret struct {
	Endpoint WebhookEndpoint
	Secret   []byte
}

// WebhookSecretAAD is the associated data an endpoint's sealed secret is bound to: its
// organization, its id and the key id, so a ciphertext copied to another row, organization or key
// id does not open.
func WebhookSecretAAD(org OrgID, id WebhookEndpointID, keyID string) []byte {
	return []byte("goal/webhook-secret/v1|" + string(org) + "|" + string(id) + "|" + keyID)
}

// WebhookDeliveryID is a delivery's id (a UUID).
type WebhookDeliveryID string

// DeliveryOutcome is what became of a delivery (ADR 0026 decision 2).
type DeliveryOutcome string

const (
	// DeliveryRefused is a request refused, with a reason such as "signature did not match".
	DeliveryRefused DeliveryOutcome = "refused"
	// DeliveryAccepted is a request accepted that no trigger turned into an item.
	DeliveryAccepted DeliveryOutcome = "accepted"
	// DeliveryCreated is a request that created an item.
	DeliveryCreated DeliveryOutcome = "created"
)

// MaxDeliveryBody is the largest body a delivery keeps, in bytes.
const MaxDeliveryBody = 1 << 20

// DeliveryRetention is how long deliveries are kept (ADR 0026 decision 2).
const DeliveryRetention = 30 * 24 * time.Hour

// signatureHeaders are the headers that authenticate a request; a delivery never keeps them.
var signatureHeaders = map[string]bool{"x-goal-signature": true, "x-hub-signature-256": true, "x-hub-signature": true}

// WebhookDelivery is one request an endpoint received.
type WebhookDelivery struct {
	ID         WebhookDeliveryID
	EndpointID WebhookEndpointID
	ReceivedAt time.Time
	// Headers are the request's headers, names lower-cased, without its signature.
	Headers map[string]string
	Body    []byte
	Outcome DeliveryOutcome
	Reason  string
	// ItemID is the item it created, for DeliveryCreated; "" otherwise or once that item is gone.
	ItemID ItemID
}

// NewWebhookDelivery is a delivery to record. Headers may hold any names; recording lower-cases
// them and drops the signature headers.
type NewWebhookDelivery struct {
	EndpointID WebhookEndpointID
	ReceivedAt time.Time
	Headers    map[string]string
	Body       []byte
	Outcome    DeliveryOutcome
	Reason     string
	ItemID     ItemID
}

// Check refuses an unknown outcome, an item on an outcome other than created, a body over
// MaxDeliveryBody or a reason over 500 characters; the error wraps ErrInvalid.
func (n NewWebhookDelivery) Check() error {
	switch {
	case n.Outcome != DeliveryRefused && n.Outcome != DeliveryAccepted && n.Outcome != DeliveryCreated:
		return fmt.Errorf("delivery outcome %q must be refused, accepted or created: %w", n.Outcome, ErrInvalid)
	case n.ItemID != "" && n.Outcome != DeliveryCreated:
		return fmt.Errorf("only a created delivery names an item: %w", ErrInvalid)
	case len(n.Body) > MaxDeliveryBody:
		return fmt.Errorf("a delivery body of %d bytes is over %d: %w", len(n.Body), MaxDeliveryBody, ErrInvalid)
	case utf8.RuneCountInString(n.Reason) > maxIntakeText:
		return fmt.Errorf("a delivery reason is over %d characters: %w", maxIntakeText, ErrInvalid)
	}

	return nil
}

// StoredHeaders are h as a delivery keeps them: names lower-cased, the signature headers dropped.
func StoredHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))

	for name, v := range h {
		name = strings.ToLower(name)
		if !signatureHeaders[name] {
			out[name] = v
		}
	}

	return out
}

// DeliveryCursor is where a page of an endpoint's deliveries, newest first, continues: after the
// delivery received at ReceivedAt with ID. The zero cursor starts at the newest.
type DeliveryCursor struct {
	ReceivedAt time.Time
	ID         WebhookDeliveryID
}

// DeliveryDay counts one endpoint's deliveries received on one UTC day: Delivered the accepted
// ones, with or without an item created, Refused the refused ones.
type DeliveryDay struct {
	EndpointID WebhookEndpointID
	// Day is the day's midnight, UTC.
	Day       time.Time
	Delivered int
	Refused   int
}

// ItemSource is where a work item came from, for Intake.
type ItemSource string

// The sources an item may come from: people in the UI, the CLI, the API, or a trigger's schedule,
// webhook or GitHub event.
const (
	ItemFromPeople   ItemSource = "people"
	ItemFromCLI      ItemSource = "cli"
	ItemFromAPI      ItemSource = "api"
	ItemFromSchedule ItemSource = "schedule"
	ItemFromWebhook  ItemSource = "webhook"
	ItemFromGitHub   ItemSource = "github"
)

// TriggerSource is what kind of event reached a trigger.
type TriggerSource string

// The kinds of event that reach a trigger: a schedule's instant, a webhook delivery or a GitHub
// event.
const (
	TriggerSchedule TriggerSource = "schedule"
	TriggerWebhook  TriggerSource = "webhook"
	TriggerGitHub   TriggerSource = "github"
)

// FiringOutcome is what a trigger did with an event.
type FiringOutcome string

const (
	// FiringCreated created an item.
	FiringCreated FiringOutcome = "created"
	// FiringSkipped created none; the reason names the step and why.
	FiringSkipped FiringOutcome = "skipped"
)

// maxIntakeText is the longest reason, dedupe value or idempotency key the log keeps.
const maxIntakeText = 500

// TriggerFiring is one event that reached a trigger (ADR 0026 decision 2).
type TriggerFiring struct {
	ID        string
	FactoryID FactoryID
	// Trigger is the trigger's name in the factory definition.
	Trigger string
	Source  TriggerSource
	// EventKey is the trigger's on: key the event matched, such as github.check_run.failed.
	EventKey string
	// IdempotencyKey is the event's id, or the scheduled instant: one firing per (factory,
	// trigger, key) (ADR 0026 decision 4).
	IdempotencyKey string
	// Dedupe is the event's value of the trigger's only.dedupe field; "" without one.
	Dedupe  string
	Outcome FiringOutcome
	Reason  string
	// ItemID is the item it created, for FiringCreated; "" otherwise or once that item is gone.
	ItemID ItemID
	At     time.Time
}

// Check refuses a firing that misses its factory, trigger, event or key, has an unknown source or
// outcome, or names an item when it created none; the error wraps ErrInvalid.
func (f TriggerFiring) Check() error {
	if err := f.checkEvent(); err != nil {
		return err
	}

	switch {
	case utf8.RuneCountInString(f.Dedupe) > maxIntakeText || utf8.RuneCountInString(f.Reason) > maxIntakeText:
		return fmt.Errorf("a firing's dedupe value and reason are at most %d characters: %w", maxIntakeText, ErrInvalid)
	case f.Outcome != FiringCreated && f.Outcome != FiringSkipped:
		return fmt.Errorf("firing outcome %q must be created or skipped: %w", f.Outcome, ErrInvalid)
	case f.ItemID != "" && f.Outcome != FiringCreated:
		return fmt.Errorf("only a created firing names an item: %w", ErrInvalid)
	case f.At.IsZero():
		return fmt.Errorf("a firing needs its instant: %w", ErrInvalid)
	}

	return nil
}

// checkEvent checks what names the firing's factory, trigger and event.
func (f TriggerFiring) checkEvent() error {
	switch {
	case f.FactoryID == "":
		return fmt.Errorf("a firing needs a factory: %w", ErrInvalid)
	case CheckConnectionName(f.Trigger) != nil:
		return fmt.Errorf("trigger name %q is not a slug: %w", f.Trigger, ErrInvalid)
	case f.Source != TriggerSchedule && f.Source != TriggerWebhook && f.Source != TriggerGitHub:
		return fmt.Errorf("firing source %q must be schedule, webhook or github: %w", f.Source, ErrInvalid)
	case f.EventKey == "" || len(f.EventKey) > 200:
		return fmt.Errorf("a firing needs an event key of at most 200 bytes: %w", ErrInvalid)
	case f.IdempotencyKey == "" || utf8.RuneCountInString(f.IdempotencyKey) > maxIntakeText:
		return fmt.Errorf("a firing needs an idempotency key of at most %d characters: %w", maxIntakeText, ErrInvalid)
	}

	return nil
}

// SourceCount is how many of a factory's items came from Source; Source "" counts the items whose
// creator recorded none.
type SourceCount struct {
	Source ItemSource
	Items  int
}

// TriggerStat is one trigger's items over a window: how many it created, how many of those have
// a merged event, and what they cost.
type TriggerStat struct {
	Trigger  string
	Created  int
	Merged   int
	SpendUSD float64
}
