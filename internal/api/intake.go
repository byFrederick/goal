package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Intake's reads (ADR 0026): how many of the newest items the sources count by default and at
// most, the days of a trigger's daily counts, its events on its page, and the deliveries of each
// endpoint its refused-deliveries problem reads.
const (
	defaultIntakeItems = 48
	maxIntakeItems     = 500
	intakeDays         = 7
	triggerEvents      = 20
	problemDeliveries  = 100
)

var errTriggerNotFound = fmt.Errorf("trigger %w", ports.ErrNotFound)

// triggerProblem is the problem of a trigger the current definition does not have, of a firing a
// try cannot name and of one whose event a try cannot rebuild, when err is one.
func triggerProblem(err error) (gen.Problem, bool) {
	var p gen.Problem

	switch {
	case errors.Is(err, errTriggerNotFound), errors.Is(err, engine.ErrTriggerNotFound):
		p = detailed(http.StatusNotFound, err)
		p.Code = optional("trigger-not-found")
	case errors.Is(err, engine.ErrFiringNotFound):
		p = detailed(http.StatusNotFound, err)
		p.Code = optional("firing-not-found")
	case errors.Is(err, engine.ErrEventNotStored):
		p = detailed(http.StatusUnprocessableEntity, err)
		p.Code = optional("event-not-stored")
	default:
		return gen.Problem{}, false
	}

	return p, true
}

func (s *server) GetIntake(ctx context.Context, req gen.GetIntakeRequestObject) (gen.GetIntakeResponseObject, error) {
	last := defaultIntakeItems

	if n := req.Params.Items; n != nil {
		if *n < 1 || *n > maxIntakeItems {
			return nil, fmt.Errorf("%w: items must be 1 to %d", errInvalid, maxIntakeItems)
		}

		last = *n
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	w, err := s.intake.ItemSources(ctx, l.FactoryID, last)
	if err != nil {
		return nil, err
	}

	out := gen.GetIntake200JSONResponse{Triggers: []gen.IntakeTrigger{}}
	tallySources(&out, w)

	if len(l.Triggers) == 0 {
		return out, nil
	}

	r, err := s.triggerReads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	for _, t := range l.Triggers {
		out.Triggers = append(out.Triggers, r.trigger(t))
	}

	return out, nil
}

func (s *server) GetIntakeTrigger(ctx context.Context, req gen.GetIntakeTriggerRequestObject) (gen.GetIntakeTriggerResponseObject, error) {
	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	var t *domain.Trigger

	for i := range l.Triggers {
		if l.Triggers[i].Name == req.Trigger {
			t = &l.Triggers[i]
		}
	}

	if t == nil {
		return nil, fmt.Errorf("%w: %s is not in the factory's current definition", errTriggerNotFound, req.Trigger)
	}

	r, err := s.triggerReads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	out := gen.GetIntakeTrigger200JSONResponse{Trigger: r.trigger(*t), Events: []gen.IntakeEvent{}}

	stats, err := s.intake.TriggerStats(ctx, l.FactoryID, domain.BudgetSince(s.clock.Now()))
	if err != nil {
		return nil, err
	}

	for _, st := range stats {
		if st.Trigger == t.Name {
			out.Week.Created, out.Week.Merged, out.Week.SpendUsd = st.Created, st.Merged, st.SpendUSD
		}
	}

	firings, err := s.intake.Firings(ctx, l.FactoryID, t.Name, triggerEvents)
	if err != nil {
		return nil, err
	}

	for _, e := range firings {
		out.Events = append(out.Events, intakeEvent(e))
	}

	return out, nil
}

func (s *server) TryIntakeTrigger(ctx context.Context, req gen.TryIntakeTriggerRequestObject) (gen.TryIntakeTriggerResponseObject, error) {
	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if req.Body == nil || req.Body.Firing == "" {
		return nil, fmt.Errorf("%w: name the firing to try", errInvalid)
	}

	by, err := actorOf(ctx, "try a trigger")
	if err != nil {
		return nil, err
	}

	got, err := s.engine.TryTrigger(ctx, f.Line().FactoryID, req.Trigger, req.Body.Firing, by)
	if err != nil {
		return nil, err
	}

	return gen.TryIntakeTrigger200JSONResponse(triggerTry(got)), nil
}

// triggerTry is a try as the API answers it.
func triggerTry(t domain.TriggerTry) gen.TriggerTry {
	e := t.Evaluation
	out := gen.TriggerTry{
		Firing: t.Firing.ID, Matched: e.Matched(), Outcome: gen.TriggerTryOutcome(e.Outcome()), Reason: optional(e.FiringReason()),
		Steps: make([]gen.TriggerTryStep, len(e.Steps)),
	}

	for i, st := range e.Steps {
		out.Steps[i] = gen.TriggerTryStep{Step: gen.TriggerTryStepStep(st.Step), Outcome: gen.TriggerTryStepOutcome(st.Outcome), Reason: optional(st.Reason)}
	}

	if it := e.Item; it != nil {
		out.Item = &struct {
			Brief    string  `json:"brief"`
			Expedite bool    `json:"expedite"`
			Lane     *string `json:"lane"`
			Station  string  `json:"station"`
			Title    string  `json:"title"`
			Type     string  `json:"type"`
		}{Brief: it.Body, Expedite: it.Class == domain.ClassExpedite, Lane: optional(it.Lane), Station: it.Station, Title: it.Title, Type: it.Type}
	}

	if m := e.Model; m != nil {
		out.ModelCheck = &struct {
			CostUsd      float64 `json:"cost_usd"`
			InputTokens  int     `json:"input_tokens"`
			Model        string  `json:"model"`
			OutputTokens int     `json:"output_tokens"`
			Reason       string  `json:"reason"`
			Worth        bool    `json:"worth"`
		}{CostUsd: m.CostUSD, InputTokens: m.InputTokens, Model: m.Model, OutputTokens: m.OutputTokens, Reason: m.Reason, Worth: m.Worth}
	}

	return out
}

// tallySources fills out's window and source counts from w.
func tallySources(out *gen.GetIntake200JSONResponse, w domain.SourceWindow) {
	counts := map[domain.ItemSource]*int{
		domain.ItemFromPeople: &out.Sources.People, domain.ItemFromCLI: &out.Sources.Cli, domain.ItemFromAPI: &out.Sources.Api,
		domain.ItemFromSchedule: &out.Sources.Schedule, domain.ItemFromWebhook: &out.Sources.Webhook, domain.ItemFromGitHub: &out.Sources.Github,
	}

	for _, c := range w.Counts {
		out.Items += c.Items

		if n, ok := counts[c.Source]; ok {
			*n += c.Items
		}
	}

	if out.Items > 0 {
		out.From, out.To = &openapi_types.Date{Time: dayOf(w.From)}, &openapi_types.Date{Time: dayOf(w.To)}
	}
}

// triggerReads is what a factory's triggers are shown with: the UTC day today starts, the items
// each trigger created on the last intakeDays days, and the factory's webhook endpoints with how
// many deliveries each refused over those days.
type triggerReads struct {
	today     time.Time
	days      map[string][]int
	endpoints []domain.WebhookEndpoint
	refused   map[domain.WebhookEndpointID]int
}

func (s *server) triggerReads(ctx context.Context, factory domain.FactoryID) (triggerReads, error) {
	r := triggerReads{today: dayOf(s.clock.Now()), days: map[string][]int{}, refused: map[domain.WebhookEndpointID]int{}}
	since := r.today.AddDate(0, 0, 1-intakeDays)

	days, err := s.intake.TriggerDays(ctx, factory, since)
	if err != nil {
		return triggerReads{}, err
	}

	for _, d := range days {
		i := int(d.Day.Sub(since) / (24 * time.Hour))
		if i < 0 || i >= intakeDays {
			continue
		}

		if r.days[d.Trigger] == nil {
			r.days[d.Trigger] = make([]int, intakeDays)
		}

		r.days[d.Trigger][i] += d.Created
	}

	if r.endpoints, err = s.intake.ListEndpoints(ctx, factory); err != nil {
		return triggerReads{}, err
	}

	for _, e := range r.endpoints {
		ds, err := s.intake.ListDeliveries(ctx, e.ID, domain.DeliveryCursor{}, problemDeliveries)
		if err != nil {
			return triggerReads{}, err
		}

		for _, d := range ds {
			if d.Outcome == domain.DeliveryRefused && !d.ReceivedAt.Before(since) {
				r.refused[e.ID]++
			}
		}
	}

	return r, nil
}

// trigger is t as Intake shows it.
func (r triggerReads) trigger(t domain.Trigger) gen.IntakeTrigger {
	out := gen.IntakeTrigger{
		Name: t.Name, Enabled: t.Enabled, On: t.On, Source: gen.IntakeTriggerSource(triggerSource(t)), When: t.On,
		Days: r.days[t.Name], Problem: r.problem(t),
	}

	if out.Days == nil {
		out.Days = make([]int, intakeDays)
	}

	if t.On == domain.OnSchedule {
		out.When = t.Cron + ", " + cmp.Or(t.TimeZone, "UTC")
	}

	out.Create.Type, out.Create.Station, out.Create.Lane, out.Create.Expedite = t.Create.Type, t.Create.Station, optional(t.Create.Lane), t.Create.Expedite

	return out
}

// problem is why t may not be creating items: an endpoint it listens on that the factory does not
// have, or deliveries its endpoints refused; nil for a schedule or without either.
func (r triggerReads) problem(t domain.Trigger) *struct {
	Kind    gen.IntakeTriggerProblemKind `json:"kind"`
	Message string                       `json:"message"`
} {
	type problem = struct {
		Kind    gen.IntakeTriggerProblemKind `json:"kind"`
		Message string                       `json:"message"`
	}

	var (
		mine    []domain.WebhookEndpoint
		missing string
	)

	switch triggerSource(t) {
	case domain.TriggerSchedule:
		return nil
	case domain.TriggerGitHub:
		for _, e := range r.endpoints {
			if e.Kind == domain.WebhookGitHub {
				mine = append(mine, e)
			}
		}

		missing = "The factory has no GitHub webhook endpoint"
	case domain.TriggerWebhook:
		name, _, _ := strings.Cut(strings.TrimPrefix(t.On, "webhook."), ".")
		for _, e := range r.endpoints {
			if e.Name == name {
				mine = append(mine, e)
			}
		}

		missing = "The factory has no webhook endpoint named " + name
	}

	if len(mine) == 0 {
		return &problem{Kind: gen.MissingConnection, Message: missing}
	}

	refused := 0
	for _, e := range mine {
		refused += r.refused[e.ID]
	}

	switch refused {
	case 0:
		return nil
	case 1:
		return &problem{Kind: gen.RefusedDeliveries, Message: "1 delivery refused in the last 7 days"}
	default:
		return &problem{Kind: gen.RefusedDeliveries, Message: fmt.Sprintf("%d deliveries refused in the last 7 days", refused)}
	}
}

// triggerSource is the kind of event t fires on.
func triggerSource(t domain.Trigger) domain.TriggerSource {
	switch {
	case t.On == domain.OnSchedule:
		return domain.TriggerSchedule
	case strings.HasPrefix(t.On, "github."):
		return domain.TriggerGitHub
	default:
		return domain.TriggerWebhook
	}
}

// intakeEvent is a firing as a trigger's recent events show it.
func intakeEvent(e domain.FiringEntry) gen.IntakeEvent {
	f := e.Firing
	out := gen.IntakeEvent{Id: f.ID, Title: cmp.Or(f.Dedupe, f.EventKey), Event: f.EventKey, At: f.At}

	if e.ItemNumber > 0 {
		out.ItemNumber = &e.ItemNumber
	}

	switch {
	case f.Outcome == domain.FiringSkipped:
		out.Outcome, out.Reason = gen.IntakeEventOutcomeSkipped, optional(cmp.Or(f.Reason, "skipped"))
	case e.Merged:
		out.Outcome = gen.IntakeEventOutcomeMerged
	default:
		out.Outcome = gen.IntakeEventOutcomeCreated
	}

	return out
}

// dayOf is the midnight UTC of t's day.
func dayOf(t time.Time) time.Time {
	t = t.UTC()

	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
