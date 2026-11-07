package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// WebhookSender is how an organization's webhook is signed and names its events (ADR 0040
// decision 12). The catalog of how each one signs is the receiver's.
type WebhookSender string

// The senders a webhook may have.
const (
	SenderGitHub    WebhookSender = "github"
	SenderGitLab    WebhookSender = "gitlab"
	SenderStandard  WebhookSender = "standard"
	SenderPagerDuty WebhookSender = "pagerduty"
	SenderSentry    WebhookSender = "sentry"
	// SenderToken is a sender that can only add a header holding a shared value.
	SenderToken WebhookSender = "token"
)

// WebhookSenders are every sender, in the order the screens list them.
var WebhookSenders = []WebhookSender{SenderGitHub, SenderGitLab, SenderStandard, SenderPagerDuty, SenderSentry, SenderToken}

// Valid reports whether s is one of WebhookSenders.
func (s WebhookSender) Valid() bool { return slices.Contains(WebhookSenders, s) }

// reservedWebhookNames are the names no organization webhook takes: a trigger's on names a sender
// or a webhook by the same word (ADR 0040 decision 14).
var reservedWebhookNames = []string{"github", "gitlab", "standard", "pagerduty", "sentry", "token", "schedule", "webhook"}

// CheckWebhookName refuses a name that is not a slug or that is reserved; the error wraps
// ErrInvalid.
func CheckWebhookName(name string) error {
	if err := CheckConnectionName(name); err != nil {
		return err
	}

	if slices.Contains(reservedWebhookNames, name) {
		return fmt.Errorf("webhook name %q is reserved: the sender names, schedule and webhook are: %w", name, ErrInvalid)
	}

	return nil
}

// WebhookSecretName is the secret New webhook creates for webhook name: webhook-<name>, cut to a
// secret name's 40 characters without a trailing hyphen.
func WebhookSecretName(name string) string {
	return strings.TrimRight(truncate("webhook-"+name, maxWebhookName), "-")
}

// maxWebhookName is the longest name a webhook or a secret takes.
const maxWebhookName = 40

// DefaultTokenHeader is the header a token sender's value comes in when its settings name none,
// as "Authorization: Bearer <value>".
const DefaultTokenHeader = "Authorization"

// TokenSettings are the token sender's settings (ADR 0040 decision 12): the header that carries
// the value ("" is DefaultTokenHeader), and the body's dot paths of the event and of a delivery
// id ("" for none).
type TokenSettings struct {
	Header    string
	EventPath string
	IDPath    string
}

// IsZero reports whether t sets nothing.
func (t TokenSettings) IsZero() bool { return t == TokenSettings{} }

var (
	headerPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)
	bodyPath      = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)
)

// maxBodyPath is the longest dot path a token sender's settings take.
const maxBodyPath = 200

// check refuses settings on a sender other than token, a header that is not a header name, and a
// path that is not a dot path; the error wraps ErrInvalid.
func (t TokenSettings) check(sender WebhookSender) error {
	switch {
	case t.IsZero():
		return nil
	case sender != SenderToken:
		return fmt.Errorf("only the token sender takes a header and body paths, not %s: %w", sender, ErrInvalid)
	case t.Header != "" && !headerPattern.MatchString(t.Header):
		return fmt.Errorf("token header %q is not a header name: %w", t.Header, ErrInvalid)
	}

	for _, p := range []string{t.EventPath, t.IDPath} {
		if p != "" && (len(p) > maxBodyPath || !bodyPath.MatchString(p)) {
			return fmt.Errorf("body path %q must be dot-separated keys, at most %d characters: %w", p, maxBodyPath, ErrInvalid)
		}
	}

	return nil
}

// WebhookOpening opens a webhook to a factory, optionally limited to some repositories (ADR 0040
// decision 12): a delivery from a repository outside them never reaches the factory.
type WebhookOpening struct {
	FactoryID FactoryID
	// Repositories are owner/name; nil is every repository.
	Repositories []string
}

// MaxOpeningRepositories is the most repositories one opening lists.
const MaxOpeningRepositories = 100

// maxRepository is the longest repository name an opening takes.
const maxRepository = 200

// Check refuses an opening without a factory, or with an empty, oversized or repeated
// repository; the error wraps ErrInvalid.
func (o WebhookOpening) Check() error {
	if o.FactoryID == "" {
		return fmt.Errorf("an opening needs a factory: %w", ErrInvalid)
	}

	if len(o.Repositories) > MaxOpeningRepositories {
		return fmt.Errorf("an opening lists %d repositories, over %d: %w", len(o.Repositories), MaxOpeningRepositories, ErrInvalid)
	}

	for i, r := range o.Repositories {
		switch {
		case strings.TrimSpace(r) == "" || utf8.RuneCountInString(r) > maxRepository:
			return fmt.Errorf("repository %q must be 1 to %d characters: %w", r, maxRepository, ErrInvalid)
		case slices.Contains(o.Repositories[:i], r):
			return fmt.Errorf("repository %q is listed twice: %w", r, ErrInvalid)
		}
	}

	return nil
}

// NewWebhook is what creates an organization's webhook, open to no factory: its name, its sender
// and the name of the secret that signs its deliveries, which must exist.
type NewWebhook struct {
	Name   string
	Sender WebhookSender
	Secret string
	Token  TokenSettings
}

// Check refuses a name CheckWebhookName refuses, an unknown sender, a secret name that is not
// one, and token settings on another sender; the error wraps ErrInvalid.
func (n NewWebhook) Check() error {
	if err := CheckWebhookName(n.Name); err != nil {
		return err
	}

	if !n.Sender.Valid() {
		return fmt.Errorf("sender %q must be github, gitlab, standard, pagerduty, sentry or token: %w", n.Sender, ErrInvalid)
	}

	if err := CheckSecretName(n.Secret); err != nil {
		return err
	}

	return n.Token.check(n.Sender)
}

// WebhookPatch changes an organization's webhook: its name, whether it is enabled, the factories
// it opens to (or whose repositories it limits again) and the ones it closes to. nil and empty
// change nothing.
type WebhookPatch struct {
	Name    *string
	Enabled *bool
	Open    []WebhookOpening
	Close   []FactoryID
}

// Check refuses a name CheckWebhookName refuses, an opening Check refuses, and a factory opened
// twice or both opened and closed; the error wraps ErrInvalid.
func (p WebhookPatch) Check() error {
	if p.Name != nil {
		if err := CheckWebhookName(*p.Name); err != nil {
			return err
		}
	}

	for i, o := range p.Open {
		if err := o.Check(); err != nil {
			return err
		}

		if slices.ContainsFunc(p.Open[:i], func(x WebhookOpening) bool { return x.FactoryID == o.FactoryID }) || slices.Contains(p.Close, o.FactoryID) {
			return fmt.Errorf("factory %s is opened twice or both opened and closed: %w", o.FactoryID, ErrInvalid)
		}
	}

	return nil
}

// Closes are the factories e is open to that p closes it to, in p's order.
func (p WebhookPatch) Closes(e WebhookEndpoint) []FactoryID {
	var out []FactoryID

	for _, f := range p.Close {
		if _, ok := e.Opening(f); ok {
			out = append(out, f)
		}
	}

	return out
}

// Apply is e with p applied: openings of p.Open replace e's for their factory or are added, by
// factory id, and those of p.Close go.
func (p WebhookPatch) Apply(e WebhookEndpoint) WebhookEndpoint {
	if p.Name != nil {
		e.Name = *p.Name
	}

	if p.Enabled != nil {
		e.Enabled = *p.Enabled
	}

	open := make([]WebhookOpening, 0, len(e.Openings)+len(p.Open))

	for _, o := range e.Openings {
		if !slices.Contains(p.Close, o.FactoryID) && !slices.ContainsFunc(p.Open, func(x WebhookOpening) bool { return x.FactoryID == o.FactoryID }) {
			open = append(open, o)
		}
	}

	for _, o := range p.Open {
		if len(o.Repositories) == 0 {
			o.Repositories = nil
		}

		open = append(open, WebhookOpening{FactoryID: o.FactoryID, Repositories: slices.Clone(o.Repositories)})
	}

	slices.SortFunc(open, func(a, b WebhookOpening) int { return strings.Compare(string(a.FactoryID), string(b.FactoryID)) })
	e.Openings = open

	return e
}

// Opening is e's opening to factory f; false when it has none.
func (e WebhookEndpoint) Opening(f FactoryID) (WebhookOpening, bool) {
	i := slices.IndexFunc(e.Openings, func(o WebhookOpening) bool { return o.FactoryID == f })
	if i < 0 {
		return WebhookOpening{}, false
	}

	return e.Openings[i], true
}

// OpenTo reports whether factory f receives e's deliveries: e is open to f.
func (e WebhookEndpoint) OpenTo(f FactoryID) bool {
	_, ok := e.Opening(f)

	return ok
}

// Factories are the factories e is open to, by id.
func (e WebhookEndpoint) Factories() []FactoryID {
	out := make([]FactoryID, 0, len(e.Openings))
	for _, o := range e.Openings {
		out = append(out, o.FactoryID)
	}

	return out
}

// TriggerNames reports whether a trigger's on names the webhook name: webhook.<name> and
// webhook.<name>.<event>, as today's triggers write it, and <name> and <name>.<event>, as ADR 0040
// decision 14 does.
func TriggerNames(on, name string) bool {
	for _, prefix := range []string{"webhook." + name, name} {
		if on == prefix || strings.HasPrefix(on, prefix+".") {
			return true
		}
	}

	return false
}

// SetupEvent is the event a trigger with on: needs w's sender to send (ADR 0040 decision 14):
// ok when on names w, by its name or its sender with an event, and all when it names w with no
// event, so the sender should send every one. A sender with no event names no delivery.
func SetupEvent(on string, w WebhookEndpoint) (event string, all, ok bool) {
	if on == OnSchedule {
		return "", false, false
	}

	s, name, e := ParseOn(on)

	switch {
	case s != "" && (s != w.Sender || e == ""):
		return "", false, false
	case s != "":
		return e, false, true
	case name != w.Name:
		return "", false, false
	default:
		return e, e == "", true
	}
}

// WebhookSecretUse is one organization webhook signed by a secret.
type WebhookSecretUse struct {
	Secret  string
	Webhook string
}

// PreviousSecretKept is how long a secret a webhook uses keeps its previous value after a
// replace, so deliveries signed with either pass while the sender is updated (ADR 0040 decision
// 15).
const PreviousSecretKept = 24 * time.Hour

// SigningSecret is a webhook's secret opened for checking a signature: its value and, until
// PreviousUntil, its previous one ("" when there is none or it has lapsed).
type SigningSecret struct {
	Current       string
	Previous      string
	PreviousUntil *time.Time
}

// DeliveryReceipt is what the receiver made of a delivery to an organization's webhook (ADR 0040
// decision 13): the first part of the delivery log's vocabulary.
type DeliveryReceipt string

const (
	// ReceiptAccepted is a signed delivery with an id the webhook had not taken.
	ReceiptAccepted DeliveryReceipt = "accepted"
	// ReceiptDuplicate is a signed delivery with an id the webhook had already taken; it creates
	// nothing.
	ReceiptDuplicate DeliveryReceipt = "duplicate"
	// ReceiptRefused is a delivery whose signature was missing or wrong; it keeps metadata only.
	ReceiptRefused DeliveryReceipt = "refused"
)

// LedgerRetention is how long a webhook's ledger keeps an accepted delivery id, and its daily
// counts their day (ADR 0040 decision 12): past DeliveryRetention, so a replay is caught after its
// row is gone.
const LedgerRetention = 365 * 24 * time.Hour

// MaxDeliveryID is the longest delivery id a webhook's ledger keeps, in characters.
const MaxDeliveryID = 200

// maxEvent is the longest event name a delivery keeps, in characters.
const maxEvent = 200

// NewReceipt is what the receiver made of one delivery to an organization's webhook, to record
// (ports.Intake.Receive).
type NewReceipt struct {
	WebhookID  WebhookEndpointID
	ReceivedAt time.Time
	// Receipt is ReceiptAccepted or ReceiptRefused; the store tells a duplicate.
	Receipt DeliveryReceipt
	// DeliveryID is an accepted delivery's id, the sender's or the hash of its body, and Event its
	// event ("" when the sender names none). A refused one has neither: nothing in it is trusted.
	DeliveryID string
	Event      string
	// Headers are the request's; recording keeps StoredHeaders of them. Body is an accepted one's.
	Headers map[string]string
	Body    []byte
	Reason  string
	// Named is whether an enabled trigger of a factory the webhook is open to names the delivery
	// (Trigger.NamesDelivery): an accepted delivery no trigger names keeps only its ledger id and a
	// daily count.
	Named bool
	// Ping is GitHub's ping, sent when the webhook is added there: accepted and not a duplicate,
	// it sets the webhook's PingedAt in the receipt's transaction.
	Ping bool
	// Evaluations are the factories that evaluate a named delivery (EvaluationFor), added with
	// its row in the receipt's transaction; a duplicate adds none.
	Evaluations []NewDeliveryEvaluation
}

// Check refuses an unknown receipt, an accepted delivery without an id or a refused one with an id,
// event or body, an id over MaxDeliveryID, an event over 200 characters, a body over
// MaxDeliveryBody and a reason over 500 characters; the error wraps ErrInvalid.
func (n NewReceipt) Check() error {
	switch {
	case n.WebhookID == "":
		return fmt.Errorf("a receipt needs its webhook: %w", ErrInvalid)
	case n.Receipt != ReceiptAccepted && n.Receipt != ReceiptRefused:
		return fmt.Errorf("receipt %q must be accepted or refused: %w", n.Receipt, ErrInvalid)
	case n.Receipt == ReceiptAccepted && n.DeliveryID == "":
		return fmt.Errorf("an accepted delivery needs its id: %w", ErrInvalid)
	case n.Receipt == ReceiptRefused && (n.DeliveryID != "" || n.Event != "" || len(n.Body) > 0):
		return fmt.Errorf("a refused delivery keeps metadata only: %w", ErrInvalid)
	}

	if err := n.checkEvaluations(); err != nil {
		return err
	}

	return n.checkSizes()
}

// checkEvaluations refuses an evaluation without its factory or triggers, or with a long reason.
func (n NewReceipt) checkEvaluations() error {
	if len(n.Evaluations) > 0 && (n.Receipt != ReceiptAccepted || !n.Named) {
		return fmt.Errorf("only an accepted delivery a trigger names is evaluated: %w", ErrInvalid)
	}

	for _, e := range n.Evaluations {
		if e.FactoryID == "" || len(e.Triggers) == 0 || utf8.RuneCountInString(e.Skip) > maxEvaluationReason {
			return fmt.Errorf("an evaluation needs its factory and triggers, and a reason of at most %d characters: %w",
				maxEvaluationReason, ErrInvalid)
		}
	}

	return nil
}

// checkSizes refuses an id, event, body or reason over its limit.
func (n NewReceipt) checkSizes() error {
	switch {
	case utf8.RuneCountInString(n.DeliveryID) > MaxDeliveryID:
		return fmt.Errorf("a delivery id is over %d characters: %w", MaxDeliveryID, ErrInvalid)
	case utf8.RuneCountInString(n.Event) > maxEvent:
		return fmt.Errorf("a delivery event is over %d characters: %w", maxEvent, ErrInvalid)
	case len(n.Body) > MaxDeliveryBody:
		return fmt.Errorf("a delivery body of %d bytes is over %d: %w", len(n.Body), MaxDeliveryBody, ErrInvalid)
	case utf8.RuneCountInString(n.Reason) > maxIntakeText:
		return fmt.Errorf("a delivery reason is over %d characters: %w", maxIntakeText, ErrInvalid)
	}

	return nil
}

// Received is what recording a receipt did: the receipt (a duplicate when the webhook had taken
// the id), the delivery id, and the delivery row it kept or, for a duplicate, the one the id first
// came with; Row is "" when there is none (an accepted delivery no trigger names, or a duplicate
// whose first row was counted only or is gone).
type Received struct {
	Receipt    DeliveryReceipt
	DeliveryID string
	Row        WebhookDeliveryID
}

// DeliveryCount is how many accepted deliveries no enabled trigger named a webhook received on one
// UTC day, Day its midnight.
type DeliveryCount struct {
	WebhookID WebhookEndpointID
	Day       time.Time
	Count     int
}

// InboundDelivery is a request to POST /hooks/{webhook id} as the sender catalog checks it:
// its headers, names lower-cased, its raw body and when it came.
type InboundDelivery struct {
	Headers map[string][]string
	Body    []byte
	At      time.Time
}

// CheckedDelivery is a delivery whose signature passed: its id (the sender's, or the hex SHA-256
// of the body when it names none) and its event ("" when it names none).
type CheckedDelivery struct {
	ID    string
	Event string
}

// ErrSignature means a delivery's signature, token or timestamp is missing or wrong. An error
// that wraps it says which, never a value.
var ErrSignature = errors.New("the signature is missing or does not match")

// NamesWebhook reports whether t is enabled and its on names deliveries to the webhook named
// webhook from sender, whatever their event (ADR 0040 decision 14): <sender>.<event> names every
// webhook of the sender, <webhook>[.<event>] the one webhook.
func (t Trigger) NamesWebhook(webhook string, sender WebhookSender) bool {
	if !t.Enabled || t.On == OnSchedule {
		return false
	}

	s, w, e := ParseOn(t.On)
	if s != "" {
		return s == sender && e != ""
	}

	return w == webhook
}

// NamesDelivery reports whether t is enabled and its on names a delivery of event from sender to
// the webhook named webhook (ADR 0040 decision 14, ParseOn): <sender>.<event> names the event
// from every webhook of the sender, <webhook>.<event> that event to the webhook and <webhook>
// every event to it.
func (t Trigger) NamesDelivery(webhook string, sender WebhookSender, event string) bool {
	if !t.Enabled || t.On == OnSchedule {
		return false
	}

	s, w, e := ParseOn(t.On)
	if s != "" {
		return s == sender && e != "" && e == event
	}

	return w == webhook && (e == "" || e == event)
}
