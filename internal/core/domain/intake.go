package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// WebhookEndpointID is a webhook endpoint's id (a UUID), the only thing in its address
// POST /hooks/{id} (ADR 0026 decision 3).
type WebhookEndpointID string

// WebhookSecretSize is the length in bytes of a signing secret goald generates for a webhook.
const WebhookSecretSize = 32

// WebhookEndpoint is one webhook of the organization (ADR 0040 decision 12), signed by an
// organization secret and open to some factories.
type WebhookEndpoint struct {
	ID WebhookEndpointID
	// Name is a slug unique in the organization, never a reserved one: the one a trigger's on
	// names.
	Name   string
	Sender WebhookSender
	// SecretID and Secret are the organization secret that signs the webhook.
	SecretID SecretID
	Secret   string
	// Enabled is false for a webhook turned off.
	Enabled bool
	// Token is the token sender's settings; zero for another sender.
	Token TokenSettings
	// Openings are the factories the webhook is open to, by factory id.
	Openings  []WebhookOpening
	CreatedAt time.Time
	// RotatedAt is when its secret was last replaced; nil when it never was.
	RotatedAt *time.Time
	// PingedAt is when the receiver last accepted GitHub's ping to it; nil when it never did.
	PingedAt *time.Time
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

// FiringRetention is how long trigger firings are kept (ADR 0040 decision 16), but for one whose
// item is still open, which only.dedupe reads.
const FiringRetention = 90 * 24 * time.Hour

// SignatureHeaders are the headers that authenticate a request, lower-cased: every sender's
// signature or token header (internal/catalog/senders) and today's generic X-Goal-Signature. A
// delivery never keeps them, and 00118's CHECK on webhook_deliveries refuses them.
var SignatureHeaders = []string{
	"x-hub-signature-256", "x-hub-signature", "x-goal-signature", "x-gitlab-token", "webhook-signature",
	"x-pagerduty-signature", "sentry-hook-signature", "authorization",
}

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
	// DeliveryID is the sender's id of an accepted delivery to an organization's webhook (or the
	// hash of its body), Event its event and Receipt what the receiver made of it; all "" for a
	// delivery recorded before the receiver and for a refused one's id and event.
	DeliveryID string
	Event      string
	Receipt    DeliveryReceipt
	// Duplicates counts the later deliveries with the same DeliveryID, which created nothing.
	Duplicates int
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

// StoredHeaders are h as a delivery keeps them: names lower-cased, SignatureHeaders and the
// headers also names (a token webhook's own header) dropped, whatever their case.
func StoredHeaders(h map[string]string, also ...string) map[string]string {
	out := make(map[string]string, len(h))

	for name, v := range h {
		name = strings.ToLower(name)
		if !slices.Contains(SignatureHeaders, name) && !slices.ContainsFunc(also, func(a string) bool { return strings.EqualFold(a, name) }) {
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

// OriginCount is how many of a factory's items came from an origin, for Intake (ADR 0040 decision
// 20).
type OriginCount struct {
	Origin Origin
	Items  int
}

// ComingUp is an open item of a factory that hasn't started (ADR 0040 decision 20): it has a start
// (Item.StartAt) or waits for open needs.
type ComingUp struct {
	Item Item
	// Needs are the numbers of its open planned and linked needs, of its factory, oldest edge
	// first.
	Needs []int64
	// FromFactory and FromNumber are the item whose follow-up filed it, of any factory of the
	// organization; "" and 0 otherwise.
	FromFactory FactoryID
	FromNumber  int64
}

// TriageQueue is what a factory's Intake shows of its follow-ups for the architect (ADR 0040
// decision 9): its collecting triage item, if it has one, with how many follow-ups joined it, and
// the trusted items whose waiting triage follow-ups join it when they land.
type TriageQueue struct {
	Item      *Item
	FollowUps int
	Waiting   []TriageSource
}

// TriageSource is an item with waiting triage follow-ups.
type TriageSource struct {
	ID        ItemID
	Number    int64
	Title     string
	FollowUps int
}

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
	// FiringMissed is a schedule's gap (ADR 0040 decision 16): Missed of its instants passed by
	// more than the schedule's window while no goald fired them, and none is fired late.
	FiringMissed FiringOutcome = "missed"
	// FiringChecked queued a check (ADR 0040 decision 19): it files nothing itself, and its Check
	// says where the check is.
	FiringChecked FiringOutcome = "checked"
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
	// Missed is how many instants a FiringMissed covers; 0 for any other outcome.
	Missed int
	// At is when it fired; for FiringMissed, the gap's last instant.
	At time.Time
	// CheckState is where a FiringChecked's check is, "" for any other outcome; RunID, CostUSD
	// and Decisions are its run's and its answer's once it ran, and Reason says why it failed.
	CheckState CheckState
	RunID      RunID
	CostUSD    float64
	Decisions  string
	// Try marks a try check (Try on the trigger's page): Applied says what it would do, and it
	// files, notes and saves nothing. Applied is what a done check did; CheckedAt when it ended.
	Try       bool
	Applied   *CheckApplied
	CheckedAt time.Time
}

// Check refuses a firing that misses its factory, trigger, event or key, has an unknown source or
// outcome, names an item when it created none, or counts missed instants unless it is a schedule's
// missed firing, which counts at least one; the error wraps ErrInvalid.
func (f TriggerFiring) Check() error {
	if err := f.checkEvent(); err != nil {
		return err
	}

	switch {
	case utf8.RuneCountInString(f.Dedupe) > maxIntakeText || utf8.RuneCountInString(f.Reason) > maxIntakeText:
		return fmt.Errorf("a firing's dedupe value and reason are at most %d characters: %w", maxIntakeText, ErrInvalid)
	case f.Outcome != FiringCreated && f.Outcome != FiringSkipped && f.Outcome != FiringMissed && f.Outcome != FiringChecked:
		return fmt.Errorf("firing outcome %q must be created, skipped, missed or checked: %w", f.Outcome, ErrInvalid)
	case (f.Outcome == FiringChecked) != (f.CheckState != ""):
		return fmt.Errorf("only a checked firing has a check state, and it always does: %w", ErrInvalid)
	case f.ItemID != "" && f.Outcome != FiringCreated:
		return fmt.Errorf("only a created firing names an item: %w", ErrInvalid)
	case f.At.IsZero():
		return fmt.Errorf("a firing needs its instant: %w", ErrInvalid)
	}

	return f.checkMissed()
}

// checkMissed checks that only a schedule's missed firing counts missed instants, at least one,
// and that only a checked firing is a try.
func (f TriggerFiring) checkMissed() error {
	switch {
	case f.Try && f.Outcome != FiringChecked:
		return fmt.Errorf("only a checked firing is a try: %w", ErrInvalid)
	case (f.Outcome == FiringMissed) != (f.Missed > 0) || f.Missed < 0:
		return fmt.Errorf("only a missed firing counts missed instants, at least one: %w", ErrInvalid)
	case f.Outcome == FiringMissed && f.Source != TriggerSchedule:
		return fmt.Errorf("only a schedule misses instants: %w", ErrInvalid)
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

// TriggerStat is one trigger's items over a window: how many it created, how many of those have
// a merged event, and what they cost.
type TriggerStat struct {
	Trigger  string
	Created  int
	Merged   int
	SpendUSD float64
}

// ItemWindow is a factory's newest items: how many there are and when the oldest (From) and the
// newest (To) of them were created. From and To are zero without items.
type ItemWindow struct {
	Items    int
	From, To time.Time
}

// TriggerDay is how many items a trigger's firings created on one UTC day, Day its midnight.
type TriggerDay struct {
	Trigger string
	Day     time.Time
	Created int
}

// FiringEntry is one firing as a trigger's recent events show it: the firing, the number of the
// item it created (0 when it created none or that item is gone) and whether that item has a
// merged event.
type FiringEntry struct {
	Firing     TriggerFiring
	ItemNumber int64
	Merged     bool
}
