package api

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// What the organization's webhook routes (orgwebhooks.go) share: their problems and the delivery
// log's pages, counts and requests. POST /hooks/{webhook}, which receives deliveries, is not
// served here.

// Errors of the webhook routes, answered with their codes by webhookProblem.
var (
	errWebhookNotFound         = errors.New("webhook endpoint not found")
	errWebhookDeliveryNotFound = errors.New("webhook delivery not found")
	errWebhookTaken            = errors.New("webhook endpoint name taken")
	errWebhookInvalid          = errors.New("webhook invalid")
	// errWebhooksUnavailable is a secret to seal while goald has no master key.
	errWebhooksUnavailable = errors.New("webhooks unavailable")
)

// The delivery log's pages.
const (
	defaultDeliveryLimit = 50
	maxDeliveryLimit     = 200
)

// deliveryDays is how many UTC days, today the last, an endpoint's counts cover.
const deliveryDays = 7

// codeWebhookNotFound answers a webhook the organization does not have.
const codeWebhookNotFound = "webhook-not-found"

// webhookProblem answers the webhook rules with their codes. Without a master key the detail is
// fixed: it says what to set, never what the store said.
func webhookProblem(err error) (gen.Problem, bool) {
	if p, ok := webhookInUseProblem(err); ok {
		return p, true
	}

	answers := []struct {
		err    error
		status int
		code   string
	}{
		{errWebhooksUnavailable, http.StatusServiceUnavailable, "webhooks-unavailable"},
		{errWebhookNotFound, http.StatusNotFound, codeWebhookNotFound},
		{errWebhookDeliveryNotFound, http.StatusNotFound, "webhook-delivery-not-found"},
		{errWebhookTaken, http.StatusConflict, "webhook-name-taken"},
		{errWebhookInvalid, http.StatusBadRequest, "webhook-invalid"},
	}

	for _, a := range answers {
		if !errors.Is(err, a.err) {
			continue
		}

		p := detailed(a.status, err)
		p.Code = optional(a.code)

		if a.status == http.StatusServiceUnavailable {
			p.Detail = optional("signing secrets are unavailable: goald has no master key (set GOAL_MASTER_KEYS; .env.example shows how)")
		}

		return p, true
	}

	return gen.Problem{}, false
}

// webhookError is err of the store about an endpoint as the API answers it. The store's errors
// never hold a secret.
func webhookError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("%w: %w", errWebhookNotFound, err)
	case errors.Is(err, ports.ErrNameTaken):
		return fmt.Errorf("%w: %w", errWebhookTaken, err)
	case errors.Is(err, ports.ErrNoMasterKey):
		return fmt.Errorf("%w: %w", errWebhooksUnavailable, err)
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %w", errWebhookInvalid, err)
	default:
		return err
	}
}

// deliveryPage is one page of endpoint id's deliveries, newest first, after the cursor after, with
// the evaluations of factory, or of every factory for "".
func (s *server) deliveryPage(ctx context.Context, id domain.WebhookEndpointID, factory domain.FactoryID, limitIn *int, afterIn *string,
) (gen.WebhookDeliveryPage, error) {
	limit := defaultDeliveryLimit
	if limitIn != nil {
		limit = *limitIn
	}

	if limit < 1 || limit > maxDeliveryLimit {
		return gen.WebhookDeliveryPage{}, fmt.Errorf("%w: limit %d is not between 1 and %d", errWebhookInvalid, limit, maxDeliveryLimit)
	}

	var (
		after domain.DeliveryCursor
		err   error
	)

	if afterIn != nil {
		if after, err = cursorIn(*afterIn); err != nil {
			return gen.WebhookDeliveryPage{}, err
		}
	}

	// One more than the page tells whether another page follows.
	ds, err := s.intake.ListDeliveries(ctx, id, after, limit+1)
	if err != nil {
		return gen.WebhookDeliveryPage{}, webhookError(err)
	}

	out := gen.WebhookDeliveryPage{Deliveries: make([]gen.WebhookDelivery, 0, min(len(ds), limit))}

	if len(ds) > limit {
		ds = ds[:limit]
		last := ds[limit-1]
		out.NextAfter = optional(cursorOut(domain.DeliveryCursor{ReceivedAt: last.ReceivedAt, ID: last.ID}))
	}

	rows := make([]domain.WebhookDeliveryID, 0, len(ds))
	for _, d := range ds {
		rows = append(rows, d.ID)
	}

	evals, err := s.deliveryEvaluations(ctx, factory, rows)
	if err != nil {
		return gen.WebhookDeliveryPage{}, err
	}

	for _, d := range ds {
		o, err := s.deliveryOut(ctx, d, evals[d.ID])
		if err != nil {
			return gen.WebhookDeliveryPage{}, err
		}

		out.Deliveries = append(out.Deliveries, o)
	}

	return out, nil
}

// deliveryRequest is endpoint id's delivery with its headers and body.
func (s *server) deliveryRequest(ctx context.Context, id domain.WebhookEndpointID, factory domain.FactoryID, delivery string,
) (gen.WebhookDeliveryRequest, error) {
	d, err := s.intake.Delivery(ctx, id, domain.WebhookDeliveryID(delivery))
	if errors.Is(err, ports.ErrNotFound) {
		return gen.WebhookDeliveryRequest{}, fmt.Errorf("%w: %w", errWebhookDeliveryNotFound, err)
	}

	if err != nil {
		return gen.WebhookDeliveryRequest{}, err
	}

	evals, err := s.deliveryEvaluations(ctx, factory, []domain.WebhookDeliveryID{d.ID})
	if err != nil {
		return gen.WebhookDeliveryRequest{}, err
	}

	o, err := s.deliveryOut(ctx, d, evals[d.ID])
	if err != nil {
		return gen.WebhookDeliveryRequest{}, err
	}

	// Signature headers were never kept; dropping them again keeps that true whatever a store holds.
	out := gen.WebhookDeliveryRequest{
		Id: o.Id, ReceivedAt: o.ReceivedAt, Outcome: o.Outcome, Reason: o.Reason, Item: o.Item,
		Event: o.Event, Receipt: (*gen.WebhookDeliveryRequestReceipt)(o.Receipt), Duplicates: o.Duplicates, Evaluations: o.Evaluations,
		Headers: domain.StoredHeaders(d.Headers), Body: string(d.Body), BodyEncoding: gen.BodyText,
	}

	if !utf8.Valid(d.Body) {
		out.Body, out.BodyEncoding = base64.StdEncoding.EncodeToString(d.Body), gen.BodyBase64
	}

	return out, nil
}

// deliveryEvaluations are the evaluations of the delivery rows by factory, or by every factory
// for "", as the log lists them, by delivery, each by factory name then id. A factory's name is
// the organization's record of it, as a webhook reaches factories the caller may not see; its id
// once the factory is gone.
func (s *server) deliveryEvaluations(ctx context.Context, factory domain.FactoryID, rows []domain.WebhookDeliveryID,
) (map[domain.WebhookDeliveryID][]gen.WebhookDeliveryEvaluation, error) {
	es, err := s.intake.DeliveryEvaluationsOf(ctx, rows)
	if err != nil || len(es) == 0 {
		return nil, err
	}

	recs, err := s.records(ctx)
	if err != nil {
		return nil, err
	}

	out := map[domain.WebhookDeliveryID][]gen.WebhookDeliveryEvaluation{}

	for _, e := range es {
		if factory != "" && e.FactoryID != factory {
			continue
		}

		name := cmp.Or(recs[e.FactoryID].Name, string(e.FactoryID))
		g := gen.WebhookDeliveryEvaluation{
			Factory: string(e.FactoryID), FactoryName: name, State: gen.WebhookDeliveryEvaluationState(e.State), Reason: e.Reason,
		}

		if e.Outcome != "" {
			g.Outcome = new(gen.WebhookDeliveryEvaluationOutcome(e.Outcome))
		}

		if e.ItemID != "" && e.ItemNumber > 0 {
			g.Item = &gen.WebhookEvaluationItem{Id: string(e.ItemID), Number: e.ItemNumber, Ref: fmt.Sprintf("%s#%d", name, e.ItemNumber)}
		}

		out[e.Delivery] = append(out[e.Delivery], g)
	}

	for _, evals := range out {
		slices.SortStableFunc(evals, func(a, b gen.WebhookDeliveryEvaluation) int {
			return cmp.Or(cmp.Compare(a.FactoryName, b.FactoryName), cmp.Compare(a.Factory, b.Factory))
		})
	}

	return out, nil
}

// deliveryOut is a delivery as the log lists it, with the number of the item it created while
// that item exists, and evals, its evaluations.
func (s *server) deliveryOut(ctx context.Context, d domain.WebhookDelivery, evals []gen.WebhookDeliveryEvaluation) (gen.WebhookDelivery, error) {
	if evals == nil {
		evals = []gen.WebhookDeliveryEvaluation{}
	}

	out := gen.WebhookDelivery{
		Id: string(d.ID), ReceivedAt: d.ReceivedAt, Outcome: gen.WebhookDeliveryOutcome(d.Outcome), Reason: d.Reason,
		Event: d.Event, Duplicates: d.Duplicates, Evaluations: evals,
	}

	if d.Receipt != "" {
		out.Receipt = new(gen.WebhookDeliveryReceipt(d.Receipt))
	}

	if d.ItemID == "" {
		return out, nil
	}

	it, err := s.items.Item(ctx, d.ItemID)
	if errors.Is(err, ports.ErrNotFound) {
		return out, nil
	}

	if err != nil {
		return gen.WebhookDelivery{}, fmt.Errorf("item of delivery %s: %w", d.ID, err)
	}

	out.Item = &gen.WebhookDeliveryItem{Id: string(it.ID), Number: it.Number}

	return out, nil
}

// cursorOut is the next_after of a page ending at c: opaque to clients.
func cursorOut(c domain.DeliveryCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.ReceivedAt.UTC().Format(time.RFC3339Nano) + "|" + string(c.ID)))
}

// cursorIn reads a next_after cursorOut made; anything else is errWebhookInvalid.
func cursorIn(after string) (domain.DeliveryCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(after)
	if err != nil {
		return domain.DeliveryCursor{}, fmt.Errorf("%w: after %q is not a cursor of this list", errWebhookInvalid, after)
	}

	at, id, ok := strings.Cut(string(raw), "|")

	t, err := time.Parse(time.RFC3339Nano, at)
	if !ok || err != nil || id == "" {
		return domain.DeliveryCursor{}, fmt.Errorf("%w: after %q is not a cursor of this list", errWebhookInvalid, after)
	}

	return domain.DeliveryCursor{ReceivedAt: t, ID: domain.WebhookDeliveryID(id)}, nil
}

// utcDay is t's UTC day's midnight.
func utcDay(t time.Time) time.Time {
	t = t.UTC()

	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
