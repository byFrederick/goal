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

// Intake's reads (ADR 0026, ADR 0040 decision 22): how many of the newest items the sources count
// by default and at most, the days of a trigger's daily counts, its events on its page, the items
// coming up and the triage item's waiting sources it shows.
const (
	defaultIntakeItems = 48
	maxIntakeItems     = 500
	intakeDays         = 7
	triggerEvents      = 20
	triggerChecks      = 20
	maxTriggerChecks   = 100
	comingUpItems      = 50
	triageSources      = 50
)

// intakeWindows are the days each window origins count over.
var intakeWindows = map[gen.IntakeWindow]int{gen.IntakeWindowN7d: 7, gen.IntakeWindowN30d: 30, gen.IntakeWindowN90d: 90}

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
	case errors.Is(err, engine.ErrStillChecking):
		p = detailed(http.StatusConflict, err)
		p.Code = optional("still-checking")
	case errors.Is(err, engine.ErrNoCheck):
		p = detailed(http.StatusUnprocessableEntity, err)
		p.Code = optional("no-check")
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

	window := gen.IntakeWindowN30d
	if w := req.Params.Window; w != nil {
		window = gen.IntakeWindow(*w)
	}

	days, ok := intakeWindows[window]
	if !ok {
		return nil, fmt.Errorf("%w: window must be 7d, 30d or 90d", errInvalid)
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

	out := gen.GetIntake200JSONResponse{Triggers: []gen.IntakeTrigger{}, Suggestions: []gen.TriggerSuggestion{}, Window: &window}
	tallySources(&out, w)

	origins, err := s.intake.OriginTally(ctx, l.FactoryID, s.clock.Now().AddDate(0, 0, -days))
	if err != nil {
		return nil, err
	}

	out.Origins = originsOf(origins)

	if err := s.comingUp(ctx, &out, f.Metadata.Name, l.FactoryID); err != nil {
		return nil, err
	}

	if out.Triage, err = s.triage(ctx, f.Metadata.Name, l.FactoryID); err != nil {
		return nil, err
	}

	for _, t := range l.Suggestions() {
		out.Suggestions = append(out.Suggestions, suggestion(t))
	}

	r, err := s.triggerReads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	for _, t := range l.Triggers {
		out.Triggers = append(out.Triggers, r.trigger(t))
	}

	out.Webhooks = openWebhooks(r.endpoints, l.Triggers)

	return out, nil
}

// originsOf is counts by origin as Intake answers them, every origin answered.
func originsOf(counts []domain.OriginCount) *gen.IntakeOrigins {
	out := &gen.IntakeOrigins{}
	by := map[domain.Origin]*int{
		domain.OriginPerson: &out.Person, domain.OriginPlan: &out.Plan, domain.OriginTrigger: &out.Trigger, domain.OriginFollowUp: &out.FollowUp,
	}

	for _, c := range counts {
		if n, ok := by[c.Origin]; ok {
			*n += c.Items
		}
	}

	return out
}

// comingUp fills out's coming up with the items of factory (named name) that haven't started.
func (s *server) comingUp(ctx context.Context, out *gen.GetIntake200JSONResponse, name string, factory domain.FactoryID) error {
	items, total, err := s.intake.ComingUp(ctx, factory, comingUpItems)
	if err != nil {
		return err
	}

	ref := func(n int64) string { return domain.ItemRef{Factory: name, Number: n}.String() }
	list := make([]gen.IntakeComingUp, 0, len(items))

	for _, c := range items {
		it := c.Item
		e := gen.IntakeComingUp{
			Id: string(it.ID), Ref: ref(it.Number), Title: it.Title, Origin: gen.IntakeComingUpOrigin(cmp.Or(it.Origin, domain.OriginPerson)),
			CreatedBy: s.createdBy(it), Start: itemStartOf(it), Needs: make([]string, 0, len(c.Needs)), CreatedAt: it.CreatedAt,
		}

		if it.Via != "" {
			e.Via = new(gen.IntakeComingUpVia(it.Via))
		}

		for _, n := range c.Needs {
			e.Needs = append(e.Needs, ref(n))
		}

		if c.FromNumber > 0 {
			e.FromItem = new(s.ref(ctx, c.FromFactory, c.FromNumber))
		}

		list = append(list, e)
	}

	out.ComingUp, out.ComingUpTotal = &list, &total

	return nil
}

// triage is factory's (named name) follow-ups for the architect as Intake answers them; nil
// without a collecting triage item or a source waiting for one.
func (s *server) triage(ctx context.Context, name string, factory domain.FactoryID) (*gen.IntakeTriage, error) {
	q, err := s.intake.TriageQueue(ctx, factory, triageSources)
	if err != nil {
		return nil, err
	}

	if q.Item == nil && len(q.Waiting) == 0 {
		return nil, nil
	}

	ref := func(n int64) string { return domain.ItemRef{Factory: name, Number: n}.String() }
	out := &gen.IntakeTriage{}

	if it := q.Item; it != nil {
		out.Item = &struct {
			FollowUps int    `json:"follow_ups"`
			Id        string `json:"id"`
			Ref       string `json:"ref"`
			Station   string `json:"station"`
			Title     string `json:"title"`
		}{FollowUps: q.FollowUps, Id: string(it.ID), Ref: ref(it.Number), Station: it.Station, Title: it.Title}
	}

	out.Waiting = make([]struct {
		FollowUps int    `json:"follow_ups"`
		Id        string `json:"id"`
		Ref       string `json:"ref"`
		Title     string `json:"title"`
	}, len(q.Waiting))

	for i, w := range q.Waiting {
		out.Waiting[i].FollowUps, out.Waiting[i].Id, out.Waiting[i].Ref, out.Waiting[i].Title = w.FollowUps, string(w.ID), ref(w.Number), w.Title
	}

	return out, nil
}

// openWebhooks are the organization's webhooks among endpoints, the ones open to the factory, by
// name, each with its state as the factory's triggers make it: receiving when it is enabled and
// an enabled trigger names it, no_trigger when none does, closed when it is turned off.
func openWebhooks(endpoints []domain.WebhookEndpoint, triggers []domain.Trigger) *[]gen.IntakeWebhook {
	out := []gen.IntakeWebhook{}

	for _, e := range endpoints {
		if e.FactoryID != "" {
			continue
		}

		state := gen.Closed
		if e.Enabled {
			state = gen.NoTrigger

			for _, t := range triggers {
				if t.NamesWebhook(e.Name, e.Sender) {
					state = gen.Receiving
					break
				}
			}
		}

		out = append(out, gen.IntakeWebhook{Id: string(e.ID), Name: e.Name, Sender: gen.WebhookSender(e.Sender), State: state})
	}

	return &out
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

func (s *server) ListTriggerChecks(ctx context.Context, req gen.ListTriggerChecksRequestObject) (gen.ListTriggerChecksResponseObject, error) {
	limit := triggerChecks

	if n := req.Params.Limit; n != nil {
		if *n < 1 || *n > maxTriggerChecks {
			return nil, fmt.Errorf("%w: limit must be 1 to %d", errInvalid, maxTriggerChecks)
		}

		limit = *n
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	l := f.Line()
	if _, ok := l.Trigger(req.Trigger); !ok {
		return nil, fmt.Errorf("%w: %s is not in the factory's current definition", errTriggerNotFound, req.Trigger)
	}

	checks, err := s.intake.TriggerChecks(ctx, l.FactoryID, req.Trigger, limit)
	if err != nil {
		return nil, err
	}

	out := gen.ListTriggerChecks200JSONResponse{Checks: make([]gen.TriggerCheck, 0, len(checks))}
	for _, c := range checks {
		out.Checks = append(out.Checks, triggerCheck(c))
	}

	return out, nil
}

func (s *server) TryTriggerCheck(ctx context.Context, req gen.TryTriggerCheckRequestObject) (gen.TryTriggerCheckResponseObject, error) {
	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	by, err := actorOf(ctx, "try a check")
	if err != nil {
		return nil, err
	}

	got, err := s.engine.TryCheck(ctx, f.Line().FactoryID, req.Trigger, by)
	if err != nil {
		return nil, err
	}

	return gen.TryTriggerCheck202JSONResponse(triggerCheck(got)), nil
}

// triggerCheck is a checked firing as the API answers it: what it applied, empty lists before it
// is done.
func triggerCheck(f domain.TriggerFiring) gen.TriggerCheck {
	out := gen.TriggerCheck{
		Id: f.ID, At: f.At, State: gen.TriggerCheckState(f.CheckState), Try: f.Try, CostUsd: f.CostUSD,
		Filed: []gen.TriggerCheckFiled{}, Noted: []gen.TriggerCheckNoted{}, Skipped: []gen.TriggerCheckSkip{},
	}

	if !f.CheckedAt.IsZero() {
		out.CheckedAt = &f.CheckedAt
	}

	if f.CheckState == domain.CheckFailed {
		out.Reason = optional(f.Reason)
	}

	if a := f.Applied; a != nil {
		for _, x := range a.Filed {
			out.Filed = append(out.Filed, gen.TriggerCheckFiled{Key: x.Key, Title: x.Title, Ref: optional(x.Ref), Reopens: optional(x.Reopens)})
		}

		for _, x := range a.Noted {
			out.Noted = append(out.Noted, gen.TriggerCheckNoted{Key: x.Key, Ref: x.Ref, Text: x.Text})
		}

		for _, x := range a.Skipped {
			out.Skipped = append(out.Skipped, gen.TriggerCheckSkip{Key: x.Key, Reason: x.Reason})
		}
	}

	return out
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

	return out
}

// tallySources fills out's window and source counts from w. Items of no known source (created
// before their creator recorded one) are left out, so out.Items is always the sum of the sources.
func tallySources(out *gen.GetIntake200JSONResponse, w domain.SourceWindow) {
	counts := map[domain.ItemSource]*int{
		domain.ItemFromPeople: &out.Sources.People, domain.ItemFromCLI: &out.Sources.Cli, domain.ItemFromAPI: &out.Sources.Api,
		domain.ItemFromSchedule: &out.Sources.Schedule, domain.ItemFromWebhook: &out.Sources.Webhook, domain.ItemFromGitHub: &out.Sources.Github,
	}

	for _, c := range w.Counts {
		if n, ok := counts[c.Source]; ok {
			*n += c.Items
			out.Items += c.Items
		}
	}

	if out.Items > 0 {
		out.From, out.To = &openapi_types.Date{Time: dayOf(w.From)}, &openapi_types.Date{Time: dayOf(w.To)}
	}
}

// triggerReads is what a factory's triggers are shown with: the UTC day today starts, the items
// each trigger created on the last intakeDays days, and the endpoints open to the factory with how
// many deliveries each refused over those days, from their daily counts.
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

	deliveries, err := s.intake.DeliveryDays(ctx, factory, since)
	if err != nil {
		return triggerReads{}, err
	}

	for _, d := range deliveries {
		r.refused[d.EndpointID] += d.Refused
	}

	return r, nil
}

// trigger is t as Intake shows it.
func (r triggerReads) trigger(t domain.Trigger) gen.IntakeTrigger {
	out := gen.IntakeTrigger{
		Name: t.Name, Enabled: t.Enabled, On: t.On, Source: gen.IntakeTriggerSource(triggerSource(t)), When: triggerWhen(t),
		Days: r.days[t.Name], Problem: r.problem(t),
	}

	if out.Days == nil {
		out.Days = make([]int, intakeDays)
	}

	out.Create.Type, out.Create.Station, out.Create.Lane, out.Create.Expedite = t.Create.Type, t.Create.Station, optional(t.Create.Lane), t.Create.Expedite

	if c := t.Check; c != nil {
		out.Check = &struct {
			Agent  string  `json:"agent"`
			MaxUsd float64 `json:"max_usd"`
		}{Agent: c.Agent, MaxUsd: c.MaxUSD}
	}

	return out
}

// triggerWhen is a plain summary of when t fires: its cron and time zone for a schedule, its
// event key otherwise.
func triggerWhen(t domain.Trigger) string {
	if t.On == domain.OnSchedule {
		return t.Cron + ", " + cmp.Or(t.TimeZone, "UTC")
	}

	return t.On
}

// suggestion is a disabled trigger t as Intake offers it.
func suggestion(t domain.Trigger) gen.TriggerSuggestion {
	out := gen.TriggerSuggestion{Name: t.Name, On: t.On, Source: gen.TriggerSuggestionSource(triggerSource(t)), When: triggerWhen(t)}
	out.Create.Type, out.Create.Station, out.Create.Lane = t.Create.Type, t.Create.Station, optional(t.Create.Lane)
	out.Create.Expedite, out.Create.Title = t.Create.Expedite, t.Create.Title

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
	case f.Outcome == domain.FiringMissed:
		out.Outcome, out.Reason, out.Missed = gen.IntakeEventOutcomeMissed, optional(f.Reason), f.Missed
	case f.Outcome == domain.FiringSkipped:
		out.Outcome, out.Reason = gen.IntakeEventOutcomeSkipped, optional(cmp.Or(f.Reason, "skipped"))
	case f.Outcome == domain.FiringChecked:
		out.Outcome = gen.IntakeEventOutcomeChecked
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
