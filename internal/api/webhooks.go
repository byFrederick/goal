package api

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// A factory's webhook endpoints and their delivery log (ADR 0026 decisions 2 and 3): anyone who
// sees the factory lists endpoints and reads deliveries (factory:read); its editors create,
// rotate and delete endpoints (webhook:manage, a fresh sign-in, never a key or a personal token).
// A signing secret is in the answers of create and rotate only, hex-encoded, and never in a log,
// a problem or another answer. POST /hooks/{endpoint}, which receives deliveries, is not served
// here.

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

// webhookProblem answers the webhook rules with their codes.
func webhookProblem(err error) (gen.Problem, bool) {
	if errors.Is(err, errWebhooksUnavailable) {
		detail := "signing secrets are unavailable: goald has no master key (set GOAL_MASTER_KEYS; .env.example shows how)"

		return gen.Problem{
			Title: http.StatusText(http.StatusServiceUnavailable), Status: http.StatusServiceUnavailable, Detail: &detail,
			Code: optional("webhooks-unavailable"),
		}, true
	}

	answers := []struct {
		err    error
		status int
		code   string
	}{
		{errWebhookNotFound, http.StatusNotFound, "webhook-not-found"},
		{errWebhookDeliveryNotFound, http.StatusNotFound, "webhook-delivery-not-found"},
		{errWebhookTaken, http.StatusConflict, "webhook-name-taken"},
		{errWebhookInvalid, http.StatusBadRequest, "webhook-invalid"},
	}

	for _, a := range answers {
		if errors.Is(err, a.err) {
			p := detailed(a.status, err)
			p.Code = optional(a.code)

			return p, true
		}
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

func (s *server) ListWebhookEndpoints(ctx context.Context, req gen.ListWebhookEndpointsRequestObject) (gen.ListWebhookEndpointsResponseObject, error) {
	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	fid := domain.FactoryID(f.Metadata.ID)

	endpoints, err := s.intake.ListEndpoints(ctx, fid)
	if err != nil {
		return nil, err
	}

	today := utcDay(s.clock.Now())
	since := today.AddDate(0, 0, 1-deliveryDays)

	days, err := s.intake.DeliveryDays(ctx, fid, since)
	if err != nil {
		return nil, err
	}

	counts := map[domain.WebhookEndpointID]map[time.Time]domain.DeliveryDay{}
	for _, d := range days {
		if counts[d.EndpointID] == nil {
			counts[d.EndpointID] = map[time.Time]domain.DeliveryDay{}
		}

		counts[d.EndpointID][d.Day] = d
	}

	out := gen.ListWebhookEndpoints200JSONResponse{Endpoints: make([]gen.WebhookEndpoint, 0, len(endpoints))}

	for _, e := range endpoints {
		o := endpointOut(e, f)
		for i := range deliveryDays {
			day := since.AddDate(0, 0, i)
			c := counts[e.ID][day]
			o.Days = append(o.Days, gen.WebhookDeliveryDay{Day: openapi_types.Date{Time: day}, Delivered: c.Delivered, Refused: c.Refused})
		}

		out.Endpoints = append(out.Endpoints, o)
	}

	return out, nil
}

func (s *server) CreateWebhookEndpoint(ctx context.Context, req gen.CreateWebhookEndpointRequestObject) (gen.CreateWebhookEndpointResponseObject, error) {
	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	n := domain.NewWebhookEndpoint{FactoryID: domain.FactoryID(f.Metadata.ID), Name: req.Body.Name, Kind: domain.WebhookKind(req.Body.Kind)}

	secret, err := s.intake.CreateEndpoint(ctx, n)
	if err != nil {
		return nil, webhookError(err)
	}

	return gen.CreateWebhookEndpoint201JSONResponse(createdOut(secret, f)), nil
}

func (s *server) RotateWebhookSecret(ctx context.Context, req gen.RotateWebhookSecretRequestObject) (gen.RotateWebhookSecretResponseObject, error) {
	f, e, err := s.webhookEndpoint(ctx, req.Factory, req.Endpoint)
	if err != nil {
		return nil, err
	}

	secret, err := s.intake.RotateSecret(ctx, e.ID)
	if err != nil {
		return nil, webhookError(err)
	}

	return gen.RotateWebhookSecret200JSONResponse(createdOut(secret, f)), nil
}

func (s *server) DeleteWebhookEndpoint(ctx context.Context, req gen.DeleteWebhookEndpointRequestObject) (gen.DeleteWebhookEndpointResponseObject, error) {
	_, e, err := s.webhookEndpoint(ctx, req.Factory, req.Endpoint)
	if err != nil {
		return nil, err
	}

	if err := s.intake.DeleteEndpoint(ctx, e.ID); err != nil {
		return nil, webhookError(err)
	}

	return gen.DeleteWebhookEndpoint204Response{}, nil
}

func (s *server) ListWebhookDeliveries(ctx context.Context, req gen.ListWebhookDeliveriesRequestObject) (gen.ListWebhookDeliveriesResponseObject, error) {
	_, e, err := s.webhookEndpoint(ctx, req.Factory, req.Endpoint)
	if err != nil {
		return nil, err
	}

	limit := defaultDeliveryLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxDeliveryLimit {
		return nil, fmt.Errorf("%w: limit %d is not between 1 and %d", errWebhookInvalid, limit, maxDeliveryLimit)
	}

	var after domain.DeliveryCursor
	if req.Params.After != nil {
		if after, err = cursorIn(*req.Params.After); err != nil {
			return nil, err
		}
	}

	// One more than the page tells whether another page follows.
	ds, err := s.intake.ListDeliveries(ctx, e.ID, after, limit+1)
	if err != nil {
		return nil, webhookError(err)
	}

	out := gen.ListWebhookDeliveries200JSONResponse{Deliveries: make([]gen.WebhookDelivery, 0, min(len(ds), limit))}

	if len(ds) > limit {
		ds = ds[:limit]
		last := ds[limit-1]
		out.NextAfter = optional(cursorOut(domain.DeliveryCursor{ReceivedAt: last.ReceivedAt, ID: last.ID}))
	}

	for _, d := range ds {
		o, err := s.deliveryOut(ctx, d)
		if err != nil {
			return nil, err
		}

		out.Deliveries = append(out.Deliveries, o)
	}

	return out, nil
}

func (s *server) GetWebhookDelivery(ctx context.Context, req gen.GetWebhookDeliveryRequestObject) (gen.GetWebhookDeliveryResponseObject, error) {
	_, e, err := s.webhookEndpoint(ctx, req.Factory, req.Endpoint)
	if err != nil {
		return nil, err
	}

	d, err := s.intake.Delivery(ctx, e.ID, domain.WebhookDeliveryID(req.Delivery))
	if errors.Is(err, ports.ErrNotFound) {
		return nil, fmt.Errorf("%w: %w", errWebhookDeliveryNotFound, err)
	}

	if err != nil {
		return nil, err
	}

	o, err := s.deliveryOut(ctx, d)
	if err != nil {
		return nil, err
	}

	// Signature headers were never kept; dropping them again keeps that true whatever a store holds.
	out := gen.GetWebhookDelivery200JSONResponse{
		Id: o.Id, ReceivedAt: o.ReceivedAt, Outcome: o.Outcome, Reason: o.Reason, Item: o.Item,
		Headers: domain.StoredHeaders(d.Headers), Body: string(d.Body), BodyEncoding: gen.BodyText,
	}

	if !utf8.Valid(d.Body) {
		out.Body, out.BodyEncoding = base64.StdEncoding.EncodeToString(d.Body), gen.BodyBase64
	}

	return out, nil
}

// webhookEndpoint resolves a path's factory and its endpoint; an endpoint of another factory or
// organization answers as unknown.
func (s *server) webhookEndpoint(ctx context.Context, factoryRef, id string) (*config.Factory, domain.WebhookEndpoint, error) {
	f, err := s.factory(ctx, factoryRef)
	if err != nil {
		return nil, domain.WebhookEndpoint{}, err
	}

	e, err := s.intake.Endpoint(ctx, domain.WebhookEndpointID(id))
	if err != nil {
		return nil, domain.WebhookEndpoint{}, webhookError(err)
	}

	if e.FactoryID != domain.FactoryID(f.Metadata.ID) {
		return nil, domain.WebhookEndpoint{}, fmt.Errorf("%w: endpoint %s is not factory %s's", errWebhookNotFound, id, f.Metadata.ID)
	}

	return f, e, nil
}

// deliveryOut is a delivery as the log lists it, with the number of the item it created while
// that item exists.
func (s *server) deliveryOut(ctx context.Context, d domain.WebhookDelivery) (gen.WebhookDelivery, error) {
	out := gen.WebhookDelivery{Id: string(d.ID), ReceivedAt: d.ReceivedAt, Outcome: gen.WebhookDeliveryOutcome(d.Outcome), Reason: d.Reason}

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

// endpointOut is an endpoint as the contract has it, without its days, never its secret: what it
// feeds is the triggers of f's current definition whose on names it.
func endpointOut(e domain.WebhookEndpoint, f *config.Factory) gen.WebhookEndpoint {
	out := gen.WebhookEndpoint{
		Id: string(e.ID), Name: e.Name, Kind: gen.WebhookKind(e.Kind), Address: "/hooks/" + string(e.ID), SecretName: e.Name + "-hook-secret",
		CreatedAt: e.CreatedAt, RotatedAt: e.RotatedAt, Triggers: []string{}, Days: make([]gen.WebhookDeliveryDay, 0, deliveryDays),
	}

	on := "webhook." + e.Name
	for _, t := range f.Spec.Board.Triggers {
		if t.On == on || strings.HasPrefix(t.On, on+".") {
			out.Triggers = append(out.Triggers, t.Name)
		}
	}

	return out
}

// createdOut is the answer of create and rotate, the only ones holding the secret. A new or
// rotated endpoint's days are not read: its counts start empty or carry on, and the list has them.
func createdOut(s domain.WebhookSecret, f *config.Factory) gen.CreatedWebhookEndpoint {
	e := endpointOut(s.Endpoint, f)

	return gen.CreatedWebhookEndpoint{
		Id: e.Id, Name: e.Name, Kind: e.Kind, Address: e.Address, SecretName: e.SecretName, CreatedAt: e.CreatedAt, RotatedAt: e.RotatedAt,
		Triggers: e.Triggers, Days: e.Days, Secret: hex.EncodeToString(s.Secret),
	}
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
