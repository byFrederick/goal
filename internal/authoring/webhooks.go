package authoring

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// OrgWebhooks lists the organization's webhooks with their openings; ports.Intake satisfies it.
type OrgWebhooks interface {
	Webhooks(ctx context.Context) ([]domain.WebhookEndpoint, error)
}

// CodeWebhookNotOpen is the problem of a trigger that names a webhook the organization doesn't have, or one that isn't
// open to the factory (ADR 0040 decision 14).
const CodeWebhookNotOpen = "webhook-not-open"

// webhookProblems checks the triggers of factory's definition f that name one webhook (the
// webhook form of on:, ADR 0040 decision 14) against the organization's webhooks: the webhook is
// open to the factory, its sender's event supplies every field the filters read, and
// create.trusted is allowed for its sender (config.DeliveryRefusals). Config checks the sender
// form itself.
func (s *Service) webhookProblems(ctx context.Context, factory domain.FactoryID, f *config.Factory) ([]domain.DefinitionProblem, error) {
	ws, err := s.webhooks.Webhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}

	byName := make(map[string]domain.WebhookEndpoint, len(ws))
	for _, w := range ws {
		byName[w.Name] = w
	}

	var out []domain.DefinitionProblem

	for i, t := range f.Line().Triggers {
		_, name, event := domain.ParseOn(t.On)
		if name == "" || t.On == domain.OnSchedule {
			continue
		}

		at := fmt.Sprintf("/spec/board/triggers/%d", i)

		w, ok := byName[name]
		if !ok || !w.OpenTo(factory) {
			out = append(out, f.Locate(at+"/on").Problem(config.CheckDeliveries, CodeWebhookNotOpen, domain.SeverityError,
				fmt.Sprintf("trigger %s: webhook %s is not open to this factory; an organization admin opens it on the Webhooks page", t.Name, name)))

			continue
		}

		for _, r := range config.DeliveryRefusals(t, w.Sender, event) {
			out = append(out, f.Locate(at+"/"+r.Key).Problem(config.CheckDeliveries, r.Code, domain.SeverityError,
				fmt.Sprintf("trigger %s: %s", t.Name, r.Message)))
		}
	}

	return out, nil
}
