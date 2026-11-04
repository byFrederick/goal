package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// HooksPath is where the receiver of the organization's webhooks serves: POST /hooks/{webhook id}
// (ADR 0040 decision 12).
const HooksPath = "/hooks/"

// The receiver's in-memory limits (ADR 0040 decision 12), per goald instance and fixed window: past
// refusedPerWebhook refused deliveries to one webhook, or unknownPerAddress requests from one
// address to webhooks that are unknown or disabled, it answers 429 until the window ends.
const (
	limitWindow       = time.Minute
	refusedPerWebhook = 30
	unknownPerAddress = 60
)

// ReceiverIntake is the intake state the receiver reads and writes; ports.Intake satisfies it.
type ReceiverIntake interface {
	EndpointOrg(ctx context.Context, id domain.WebhookEndpointID) (domain.OrgID, error)
	Endpoint(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookEndpoint, error)
	Receive(ctx context.Context, n domain.NewReceipt) (domain.Received, error)
}

// SigningSecrets open a webhook's secret for its signature; ports.OrgSecrets satisfies it.
type SigningSecrets interface {
	OpenSigning(ctx context.Context, id domain.SecretID) (domain.SigningSecret, error)
}

// WebhookSenders check a delivery's signature as the sender catalog says
// (internal/catalog/senders), wired by cmd/goald: an error wrapping domain.ErrSignature for a
// missing or wrong one.
type WebhookSenders interface {
	Check(sender domain.WebhookSender, token domain.TokenSettings, d domain.InboundDelivery, key domain.SigningSecret) (domain.CheckedDelivery, error)
}

// DeliveryNames says whether an enabled trigger of a factory the webhook is open to names a
// delivery of event; *engine.Engine satisfies it from its loaded lines.
type DeliveryNames interface {
	NamesDelivery(ctx context.Context, w domain.WebhookEndpoint, event string) bool
}

// ReceiverDeps are what the receiver needs.
type ReceiverDeps struct {
	Intake  ReceiverIntake
	Secrets SigningSecrets
	Senders WebhookSenders
	Names   DeliveryNames
	// TrustedProxies are the proxies whose X-Forwarded-For names the client, for the limit on
	// unknown webhooks; with none, the peer's address counts.
	TrustedProxies []netip.Prefix
	Clock          ports.Clock
	Logger         *slog.Logger
}

// receiver serves POST /hooks/{webhook id}.
type receiver struct {
	d       ReceiverDeps
	log     *slog.Logger
	refused *windowLimit
	unknown *windowLimit
}

// Receiver serves POST /hooks/{webhook id} without a session or an API key (ADR 0040 decision
// 12). It reads at most domain.MaxDeliveryBody bytes (413 past that) and checks the signature
// against the raw body, in constant time, before anything reads the body. It answers 202 with
// the delivery id when accepted, 200 for an id the webhook already took, 401 for a missing or
// wrong signature (recorded as refused, metadata only), 404 for an unknown or disabled webhook or
// a factory's endpoint, 405 for another method, and 429 with Retry-After past 30 refused deliveries
// a minute to one webhook or 60 requests a minute from one address to unknown webhooks. A signed
// delivery is never limited. No answer or log line holds a secret or a signature.
func Receiver(d ReceiverDeps) http.Handler {
	r := &receiver{
		d: d, log: cmp.Or(d.Logger, slog.New(slog.DiscardHandler)),
		refused: newWindowLimit(refusedPerWebhook, limitWindow), unknown: newWindowLimit(unknownPerAddress, limitWindow),
	}

	return withClientAddr(d.TrustedProxies, http.HandlerFunc(r.serve))
}

// receiptBody is the receiver's answer to a signed delivery.
type receiptBody struct {
	ID      string                 `json:"id"`
	Receipt domain.DeliveryReceipt `json:"receipt"`
}

func (rc *receiver) serve(w http.ResponseWriter, r *http.Request) {
	id, ok := strings.CutPrefix(r.URL.Path, HooksPath)
	if !ok || id == "" || strings.Contains(id, "/") {
		rc.problem(w, http.StatusNotFound, "no such webhook")
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		rc.problem(w, http.StatusMethodNotAllowed, "a webhook takes POST only")

		return
	}

	now := rc.d.Clock.Now()

	hook, ctx, err := rc.webhook(r.Context(), domain.WebhookEndpointID(id))
	if errors.Is(err, errUnknownWebhook) {
		if retry, ok := rc.unknown.take(clientAddrFrom(r.Context()).String(), now); !ok {
			rc.tooMany(w, retry)
			return
		}

		rc.problem(w, http.StatusNotFound, "no such webhook")

		return
	}

	if err != nil {
		rc.fail(w, r, id, err)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, domain.MaxDeliveryBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			rc.problem(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a delivery has at most %d bytes", domain.MaxDeliveryBody))
			return
		}

		rc.problem(w, http.StatusBadRequest, "the body could not be read")

		return
	}

	rc.check(ctx, w, r, hook, body, now)
}

// errUnknownWebhook means no enabled organization webhook has the id.
var errUnknownWebhook = errors.New("unknown webhook")

// webhook is the enabled organization webhook id with ctx in its organization;
// errUnknownWebhook for an id no such webhook has.
func (rc *receiver) webhook(ctx context.Context, id domain.WebhookEndpointID) (domain.WebhookEndpoint, context.Context, error) {
	org, err := rc.d.Intake.EndpointOrg(ctx, id)

	switch {
	case errors.Is(err, ports.ErrNotFound) || errors.Is(err, domain.ErrInvalid):
		return domain.WebhookEndpoint{}, ctx, errUnknownWebhook
	case err != nil:
		return domain.WebhookEndpoint{}, ctx, err
	}

	ctx = domain.WithOrg(ctx, org)

	hook, err := rc.d.Intake.Endpoint(ctx, id)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return domain.WebhookEndpoint{}, ctx, errUnknownWebhook
	case err != nil:
		return domain.WebhookEndpoint{}, ctx, err
	case !hook.OrgLevel() || !hook.Enabled:
		// A factory's endpoint is received only once it is converted (software#651).
		return domain.WebhookEndpoint{}, ctx, errUnknownWebhook
	}

	return hook, ctx, nil
}

// check checks the delivery's signature and records its receipt.
func (rc *receiver) check(ctx context.Context, w http.ResponseWriter, r *http.Request, hook domain.WebhookEndpoint, body []byte, now time.Time) {
	key, err := rc.d.Secrets.OpenSigning(ctx, hook.SecretID)
	if err != nil {
		rc.fail(w, r, string(hook.ID), err)
		return
	}

	in := domain.InboundDelivery{Headers: make(map[string][]string, len(r.Header)), Body: body, At: now}
	for name, vs := range r.Header {
		in.Headers[strings.ToLower(name)] = vs
	}

	headers := domain.StoredHeaders(flatHeaders(r.Header), hook.Token.Header)

	checked, err := rc.d.Senders.Check(hook.Sender, hook.Token, in, key)

	switch {
	case errors.Is(err, domain.ErrSignature):
		rc.refuse(ctx, w, r, hook, headers, err, now)
		return
	case err != nil:
		rc.fail(w, r, string(hook.ID), err)
		return
	}

	n := domain.NewReceipt{
		WebhookID: hook.ID, ReceivedAt: now, Receipt: domain.ReceiptAccepted, DeliveryID: checked.ID, Event: checked.Event,
		Headers: headers, Body: body, Named: rc.d.Names.NamesDelivery(ctx, hook, checked.Event),
	}

	// GitHub's ping, sent when the webhook is added there, keeps its row to show on the webhook.
	if hook.Sender == domain.SenderGitHub && checked.Event == "ping" {
		n.Reason, n.Named = domain.GitHubPingReason, true
	}

	got, err := rc.d.Intake.Receive(ctx, n)
	if err != nil {
		rc.fail(w, r, string(hook.ID), err)
		return
	}

	status := http.StatusAccepted
	if got.Receipt == domain.ReceiptDuplicate {
		status = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(receiptBody{ID: got.DeliveryID, Receipt: got.Receipt})
}

// refuse answers a delivery whose signature failed: 401, recorded as refused with metadata only,
// or 429 past the webhook's limit, recorded as nothing.
func (rc *receiver) refuse(ctx context.Context, w http.ResponseWriter, r *http.Request, hook domain.WebhookEndpoint,
	headers map[string]string, reason error, now time.Time,
) {
	if retry, ok := rc.refused.take(string(hook.ID), now); !ok {
		rc.tooMany(w, retry)
		return
	}

	if _, err := rc.d.Intake.Receive(ctx, domain.NewReceipt{
		WebhookID: hook.ID, ReceivedAt: now, Receipt: domain.ReceiptRefused, Headers: headers, Reason: clip(reason.Error(), 500),
	}); err != nil {
		rc.fail(w, r, string(hook.ID), err)
		return
	}

	rc.log.InfoContext(ctx, "webhook delivery refused", slog.String("webhook", string(hook.ID)), slog.String("reason", reason.Error()))
	rc.problem(w, http.StatusUnauthorized, domain.ErrSignature.Error())
}

// clip is s cut to at most n characters.
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}

	return s
}

// flatHeaders are h with each header's values joined, as a delivery keeps them.
func flatHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for name, vs := range h {
		out[name] = strings.Join(vs, ", ")
	}

	return out
}

func (rc *receiver) problem(w http.ResponseWriter, status int, detail string) {
	writeProblem(w, gen.Problem{Title: http.StatusText(status), Status: status, Detail: &detail})
}

func (rc *receiver) tooMany(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(max(retry, time.Second).Seconds()))))
	rc.problem(w, http.StatusTooManyRequests, "too many requests; try again after Retry-After seconds")
}

// fail answers 500 and logs err, which holds no secret: the adapters' errors never do.
func (rc *receiver) fail(w http.ResponseWriter, r *http.Request, webhook string, err error) {
	rc.log.ErrorContext(r.Context(), "webhook receive failed", slog.String("webhook", webhook), slog.Any("error", err))
	rc.problem(w, http.StatusInternalServerError, "the delivery could not be recorded; see goald's log")
}

// windowLimit counts requests per key in fixed windows of every, allowing at most max in each.
type windowLimit struct {
	mu     sync.Mutex
	max    int
	every  time.Duration
	start  time.Time
	counts map[string]int
}

func newWindowLimit(n int, every time.Duration) *windowLimit {
	return &windowLimit{max: n, every: every, counts: map[string]int{}}
}

// take counts one request of key at now and reports whether it is within the limit, or how long
// until the window ends when it is not.
func (l *windowLimit) take(key string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.start) >= l.every || now.Before(l.start) {
		l.start, l.counts = now, map[string]int{}
	}

	if l.counts[key] >= l.max {
		return l.start.Add(l.every).Sub(now), false
	}

	l.counts[key]++

	return 0, true
}
