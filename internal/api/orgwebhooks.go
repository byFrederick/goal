package api

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The organization's webhooks (ADR 0040 decisions 12 and 15): owners and admins list and read
// them (webhook:read), and create, open, close, limit, rename and delete them (webhook:manage,
// a fresh sign-in). Each is signed by an organization secret named by the webhook: New either
// makes webhook-<name> with a generated value, answered once, or uses an existing secret. A
// rename, a close or a delete that would leave published triggers without the webhook answers
// 409 webhook-in-use naming them. POST /hooks/{webhook id}, which receives deliveries, is not
// served here.

// webhookInUseError is a change refused while published triggers use the webhook; its problem
// names each.
type webhookInUseError struct {
	name     string
	triggers []gen.WebhookTrigger
}

func (e *webhookInUseError) Error() string {
	names := make([]string, 0, len(e.triggers))
	for _, t := range e.triggers {
		names = append(names, t.FactoryName+" "+t.Trigger)
	}

	return fmt.Sprintf("webhook %q is used by the published triggers %s; publish them without it first", e.name, strings.Join(names, ", "))
}

// webhookInUseProblem answers a webhookInUseError: 409 webhook-in-use with its triggers.
func webhookInUseProblem(err error) (gen.Problem, bool) {
	inUse, ok := errors.AsType[*webhookInUseError](err)
	if !ok {
		return gen.Problem{}, false
	}

	p := detailed(http.StatusConflict, err)
	p.Code, p.Triggers = optional("webhook-in-use"), &inUse.triggers

	return p, true
}

func (s *server) ListWebhooks(ctx context.Context, _ gen.ListWebhooksRequestObject) (gen.ListWebhooksResponseObject, error) {
	hooks, err := s.intake.Webhooks(ctx)
	if err != nil {
		return nil, err
	}

	days, err := s.webhookDays(ctx, "")
	if err != nil {
		return nil, err
	}

	uses, err := s.webhookUses(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListWebhooks200JSONResponse{Webhooks: make([]gen.Webhook, 0, len(hooks))}
	for _, w := range hooks {
		out.Webhooks = append(out.Webhooks, webhookOut(w, days.of(w.ID), uses(w)))
	}

	return out, nil
}

func (s *server) CreateWebhook(ctx context.Context, req gen.CreateWebhookRequestObject) (gen.CreateWebhookResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create webhook: the request has no actor")
	}

	b := req.Body
	n := domain.NewWebhook{Name: b.Name, Sender: domain.WebhookSender(b.Sender), Secret: deref(b.Secret), Token: tokenIn(b.Token)}

	if b.Secret == nil {
		n.Secret = domain.WebhookSecretName(b.Name)
	}

	if err := n.Check(); err != nil {
		return nil, fmt.Errorf("%w: %w", errWebhookInvalid, err)
	}

	if _, err := s.intake.WebhookByName(ctx, n.Name); err == nil {
		return nil, fmt.Errorf("%w: webhook %q", errWebhookTaken, n.Name)
	} else if !errors.Is(err, ports.ErrNotFound) {
		return nil, err
	}

	var value *string

	if b.Secret == nil {
		v, err := signingValue(n.Sender)
		if err != nil {
			return nil, err
		}

		// Open to no factory: a run never opens a webhook's secret (ADR 0040 decision 15).
		if _, err := s.secrets.Create(ctx, domain.NewSecret{Name: n.Secret, Description: "Signs the webhook " + n.Name, Value: v}, by); err != nil {
			return nil, secretError(err)
		}

		value = &v
	}

	w, err := s.intake.CreateWebhook(ctx, n)
	if err != nil {
		if value != nil {
			err = errors.Join(err, s.secrets.Delete(ctx, n.Secret, by))
		}

		if errors.Is(err, ports.ErrNotFound) {
			return nil, fmt.Errorf("%w: %w", errSecretNotFound, err)
		}

		return nil, webhookError(err)
	}

	o := webhookOut(w, nil, webhookUse{named: []gen.WebhookTrigger{}, triggers: []gen.WebhookTrigger{}, events: []string{}})

	return gen.CreateWebhook201JSONResponse{
		Id: o.Id, Name: o.Name, Sender: o.Sender, Address: o.Address, Secret: o.Secret, Enabled: o.Enabled, Token: o.Token,
		Openings: o.Openings, Triggers: o.Triggers, CreatedAt: o.CreatedAt, Days: o.Days, PingedAt: o.PingedAt,
		SetupEvents: o.SetupEvents, SetupAllEvents: o.SetupAllEvents, SecretValue: value,
	}, nil
}

func (s *server) GetWebhook(ctx context.Context, req gen.GetWebhookRequestObject) (gen.GetWebhookResponseObject, error) {
	w, err := s.orgWebhook(ctx, req.Webhook)
	if err != nil {
		return nil, err
	}

	out, err := s.oneWebhookOut(ctx, w)
	if err != nil {
		return nil, err
	}

	return gen.GetWebhook200JSONResponse(out), nil
}

func (s *server) PatchWebhook(ctx context.Context, req gen.PatchWebhookRequestObject) (gen.PatchWebhookResponseObject, error) {
	w, err := s.orgWebhook(ctx, req.Webhook)
	if err != nil {
		return nil, err
	}

	p := patchIn(req.Body)
	if err := p.Check(); err != nil {
		return nil, fmt.Errorf("%w: %w", errWebhookInvalid, err)
	}

	if err := s.knownFactories(ctx, p.Open); err != nil {
		return nil, err
	}

	if err := s.unused(ctx, w, p); err != nil {
		return nil, err
	}

	next, err := s.intake.PatchWebhook(ctx, w.ID, p)
	if err != nil {
		return nil, webhookError(err)
	}

	out, err := s.oneWebhookOut(ctx, next)
	if err != nil {
		return nil, err
	}

	return gen.PatchWebhook200JSONResponse(out), nil
}

func (s *server) DeleteWebhook(ctx context.Context, req gen.DeleteWebhookRequestObject) (gen.DeleteWebhookResponseObject, error) {
	w, err := s.orgWebhook(ctx, req.Webhook)
	if err != nil {
		return nil, err
	}

	if err := s.deletable(ctx, w); err != nil {
		return nil, err
	}

	if err := s.intake.DeleteEndpoint(ctx, w.ID); err != nil {
		return nil, webhookError(err)
	}

	return gen.DeleteWebhook204Response{}, nil
}

func (s *server) ListOrgWebhookDeliveries(ctx context.Context, req gen.ListOrgWebhookDeliveriesRequestObject) (gen.ListOrgWebhookDeliveriesResponseObject, error) {
	w, err := s.orgWebhook(ctx, req.Webhook)
	if err != nil {
		return nil, err
	}

	page, err := s.deliveryPage(ctx, w.ID, "", req.Params.Limit, req.Params.After)
	if err != nil {
		return nil, err
	}

	if page.UnnamedDays, err = s.unnamedDays(ctx, w.ID); err != nil {
		return nil, err
	}

	return gen.ListOrgWebhookDeliveries200JSONResponse(page), nil
}

// unnamedDays are webhook id's daily counts of deliveries no trigger named over the delivery
// log's window, from the UTC day domain.DeliveryRetention ago through today, oldest first.
func (s *server) unnamedDays(ctx context.Context, id domain.WebhookEndpointID) ([]gen.WebhookUnnamedDay, error) {
	counts, err := s.intake.UnnamedDays(ctx, id, s.clock.Now().Add(-domain.DeliveryRetention))
	if err != nil {
		return nil, webhookError(err)
	}

	out := make([]gen.WebhookUnnamedDay, 0, len(counts))
	for _, c := range counts {
		out = append(out, gen.WebhookUnnamedDay{Day: openapi_types.Date{Time: c.Day}, Count: c.Count})
	}

	return out, nil
}

func (s *server) GetOrgWebhookDelivery(ctx context.Context, req gen.GetOrgWebhookDeliveryRequestObject) (gen.GetOrgWebhookDeliveryResponseObject, error) {
	w, err := s.orgWebhook(ctx, req.Webhook)
	if err != nil {
		return nil, err
	}

	out, err := s.deliveryRequest(ctx, w.ID, "", req.Delivery)
	if err != nil {
		return nil, err
	}

	return gen.GetOrgWebhookDelivery200JSONResponse(out), nil
}

// orgWebhook is the organization's webhook ref, a name or an id.
func (s *server) orgWebhook(ctx context.Context, ref string) (domain.WebhookEndpoint, error) {
	w, err := s.intake.WebhookByName(ctx, ref)
	if err == nil || !errors.Is(err, ports.ErrNotFound) {
		return w, webhookError(err)
	}

	if _, perr := uuid.Parse(ref); perr == nil {
		w, err = s.intake.Endpoint(ctx, domain.WebhookEndpointID(ref))
		if err == nil {
			return w, nil
		}

		if !errors.Is(err, ports.ErrNotFound) {
			return domain.WebhookEndpoint{}, err
		}
	}

	return domain.WebhookEndpoint{}, fmt.Errorf("%w: webhook %q", errWebhookNotFound, ref)
}

// knownFactories refuses an opening of a factory the organization does not have.
func (s *server) knownFactories(ctx context.Context, open []domain.WebhookOpening) error {
	if len(open) == 0 {
		return nil
	}

	recs, err := s.records(ctx)
	if err != nil {
		return err
	}

	for _, o := range open {
		if _, ok := recs[o.FactoryID]; !ok {
			return errFactoryNotFound
		}
	}

	return nil
}

// unused refuses p when it renames w or closes it to a factory while published triggers use it
// there.
func (s *server) unused(ctx context.Context, w domain.WebhookEndpoint, p domain.WebhookPatch) error {
	renamed := p.Name != nil && *p.Name != w.Name
	closes := p.Closes(w)

	if !renamed && len(closes) == 0 {
		return nil
	}

	uses, err := s.webhookUses(ctx)
	if err != nil {
		return err
	}

	triggers := uses(w).named
	if !renamed {
		triggers = slices.DeleteFunc(triggers, func(t gen.WebhookTrigger) bool { return !slices.Contains(closes, domain.FactoryID(t.Factory)) })
	}

	if len(triggers) > 0 {
		return &webhookInUseError{name: w.Name, triggers: triggers}
	}

	return nil
}

// deletable refuses to delete the organization's webhook w while published triggers use it.
func (s *server) deletable(ctx context.Context, w domain.WebhookEndpoint) error {
	uses, err := s.webhookUses(ctx)
	if err != nil {
		return err
	}

	if triggers := uses(w).named; len(triggers) > 0 {
		return &webhookInUseError{name: w.Name, triggers: triggers}
	}

	return nil
}

// webhookUse is what the published definitions of the factories a webhook reaches have of it:
// the triggers whose on names it (named, which hold it against a rename, a close and a delete),
// those and the enabled ones that name its sender with an event (triggers, which it lists), and
// the events its sender must send for the enabled triggers that name it or its sender (setup
// events), or every event (all).
type webhookUse struct {
	named    []gen.WebhookTrigger
	triggers []gen.WebhookTrigger
	events   []string
	all      bool
}

// webhookUses reads the published definitions once and returns what of them uses a webhook: the
// enabled or disabled triggers of the factories it reaches whose on names it, the enabled ones
// whose on names its sender with an event (ADR 0040 decision 14), each by factory name then
// trigger, and its setup events, sorted and distinct. Archived factories are left out, as for a
// secret's uses.
func (s *server) webhookUses(ctx context.Context) (func(domain.WebhookEndpoint) webhookUse, error) {
	recs, err := s.records(ctx)
	if err != nil {
		return nil, err
	}

	factories := s.factories.Factories(ctx)

	return func(w domain.WebhookEndpoint) webhookUse {
		out := webhookUse{named: []gen.WebhookTrigger{}, triggers: []gen.WebhookTrigger{}, events: []string{}}

		for _, f := range factories {
			id := domain.FactoryID(f.Metadata.ID)
			if recs[id].Archived() || !slices.Contains(w.Factories(), id) {
				continue
			}

			for _, t := range f.Spec.Board.Triggers {
				out.add(w, gen.WebhookTrigger{Factory: string(id), FactoryName: f.Metadata.Name, Trigger: t.Name}, t.On, t.Enabled == nil || *t.Enabled)
			}
		}

		slices.SortStableFunc(out.named, byFactoryThenTrigger)
		slices.SortStableFunc(out.triggers, byFactoryThenTrigger)
		slices.Sort(out.events)
		out.events = slices.Compact(out.events)

		return out
	}, nil
}

// add counts the trigger use, whose on is on, in what of it uses w.
func (u *webhookUse) add(w domain.WebhookEndpoint, use gen.WebhookTrigger, on string, enabled bool) {
	named := domain.TriggerNames(on, w.Name)
	if named {
		u.named = append(u.named, use)
		u.triggers = append(u.triggers, use)
	}

	if !enabled {
		return
	}

	if sender, _, event := domain.ParseOn(on); !named && sender == w.Sender && event != "" {
		u.triggers = append(u.triggers, use)
	}

	if event, all, ok := domain.SetupEvent(on, w); ok {
		u.all = u.all || all
		if event != "" {
			u.events = append(u.events, event)
		}
	}
}

// byFactoryThenTrigger orders trigger uses by factory name, then trigger.
func byFactoryThenTrigger(a, b gen.WebhookTrigger) int {
	return cmp.Or(cmp.Compare(a.FactoryName, b.FactoryName), cmp.Compare(a.Trigger, b.Trigger))
}

// oneWebhookOut is w with its uses and its days.
func (s *server) oneWebhookOut(ctx context.Context, w domain.WebhookEndpoint) (gen.Webhook, error) {
	days, err := s.webhookDays(ctx, "")
	if err != nil {
		return gen.Webhook{}, err
	}

	uses, err := s.webhookUses(ctx)
	if err != nil {
		return gen.Webhook{}, err
	}

	return webhookOut(w, days.of(w.ID), uses(w)), nil
}

// endpointDays are endpoints' delivery counts by UTC day, over the deliveryDays days ending today.
type endpointDays struct {
	since  time.Time
	counts map[domain.WebhookEndpointID]map[time.Time]domain.DeliveryDay
}

// webhookDays are the counts of the endpoints open to factory, or of the organization's webhooks
// for "".
func (s *server) webhookDays(ctx context.Context, factory domain.FactoryID) (endpointDays, error) {
	since := utcDay(s.clock.Now()).AddDate(0, 0, 1-deliveryDays)

	days, err := s.intake.DeliveryDays(ctx, factory, since)
	if err != nil {
		return endpointDays{}, err
	}

	out := endpointDays{since: since, counts: map[domain.WebhookEndpointID]map[time.Time]domain.DeliveryDay{}}

	for _, d := range days {
		if out.counts[d.EndpointID] == nil {
			out.counts[d.EndpointID] = map[time.Time]domain.DeliveryDay{}
		}

		out.counts[d.EndpointID][d.Day] = d
	}

	return out, nil
}

// of are endpoint id's deliveryDays days, oldest first, each zero-filled.
func (d endpointDays) of(id domain.WebhookEndpointID) []gen.WebhookDeliveryDay {
	out := make([]gen.WebhookDeliveryDay, 0, deliveryDays)

	for i := range deliveryDays {
		day := d.since.AddDate(0, 0, i)
		c := d.counts[id][day]
		out = append(out, gen.WebhookDeliveryDay{Day: openapi_types.Date{Time: day}, Delivered: c.Delivered, Refused: c.Refused})
	}

	return out
}

// webhookOut is w as the contract has it, never a secret's value; nil days are seven empty ones.
func webhookOut(w domain.WebhookEndpoint, days []gen.WebhookDeliveryDay, use webhookUse) gen.Webhook {
	if days == nil {
		days = endpointDays{since: utcDay(w.CreatedAt).AddDate(0, 0, 1-deliveryDays)}.of(w.ID)
	}

	out := gen.Webhook{
		Id: string(w.ID), Name: w.Name, Sender: gen.WebhookSender(w.Sender), Address: "/hooks/" + string(w.ID), Secret: w.Secret,
		Enabled: w.Enabled, Openings: make([]gen.WebhookOpening, 0, len(w.Openings)), Triggers: use.triggers,
		CreatedAt: w.CreatedAt, Days: days, PingedAt: w.PingedAt, SetupEvents: use.events, SetupAllEvents: use.all,
	}

	if w.Sender == domain.SenderToken {
		header := cmp.Or(w.Token.Header, domain.DefaultTokenHeader)
		out.Token = &gen.WebhookTokenSettings{Header: &header, EventPath: &w.Token.EventPath, IdPath: &w.Token.IDPath}
	}

	for _, o := range w.Openings {
		g := gen.WebhookOpening{Factory: string(o.FactoryID)}
		if o.Repositories != nil {
			g.Repositories = new(slices.Clone(o.Repositories))
		}

		out.Openings = append(out.Openings, g)
	}

	return out
}

// tokenIn is a request's token settings; absent, none.
func tokenIn(t *gen.WebhookTokenSettings) domain.TokenSettings {
	if t == nil {
		return domain.TokenSettings{}
	}

	return domain.TokenSettings{Header: deref(t.Header), EventPath: deref(t.EventPath), IDPath: deref(t.IdPath)}
}

// patchIn is a request's patch.
func patchIn(b *gen.WebhookPatch) domain.WebhookPatch {
	p := domain.WebhookPatch{Name: b.Name, Enabled: b.Enabled}

	for _, o := range deref(b.Open) {
		p.Open = append(p.Open, domain.WebhookOpening{FactoryID: domain.FactoryID(o.Factory), Repositories: deref(o.Repositories)})
	}

	for _, f := range deref(b.Close) {
		p.Close = append(p.Close, domain.FactoryID(f))
	}

	return p
}

// signingValue is a new secret value for a webhook of sender: 32 random bytes, as whsec_ and
// base64 for a Standard Webhooks sender, as 64 hex characters otherwise.
func signingValue(sender domain.WebhookSender) (string, error) {
	b := make([]byte, domain.WebhookSecretSize)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("new webhook secret: %w", err)
	}

	if sender == domain.SenderStandard {
		return "whsec_" + base64.StdEncoding.EncodeToString(b), nil
	}

	return hex.EncodeToString(b), nil
}
