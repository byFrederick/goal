package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The intake commands' subcommands that other groups do not have.
const (
	subShow       = "show"
	subDeliveries = "deliveries"
)

// intakeTitleWidth is how much of an event's title a row of goal triggers show keeps.
const intakeTitleWidth = 60

// triggersCommand is goal triggers list|show|try: a factory's triggers (ADR 0026) as its current
// definition declares them, with what they created, and a dry run of one against a past event
// (ADR 0028 decision 2). Triggers change only through the definition (goal factory push).
func triggersCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{subList: triggersList, subShow: triggersShow, "try": triggersTry}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// webhooksCommand is goal webhooks list|create|rotate|deliveries: a factory's webhook endpoints,
// their signing secrets and the deliveries they received.
func webhooksCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{
		subList: webhooksList, verbCreate: webhooksCreate, "rotate": webhooksRotate, subDeliveries: webhooksDeliveries,
	}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// intakeFlags are the flags every intake command takes: --org and --json.
func intakeFlags(name string) (fs *flag.FlagSet, org *string, asJSON *bool) {
	fs = newFlags(name)

	return fs, addOrgFlag(fs), fs.Bool("json", false, "print goald's answer as it came")
}

// triggersList is goal triggers list <factory> [--json]: one row per trigger in definition order.
func triggersList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("triggers list")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.GetIntakeWithResponse(ctx, c.org, pos[0], &gen.GetIntakeParams{})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	printTriggers(out, res.JSON200.Triggers)

	return nil
}

// printTriggers prints a row per trigger, then each trigger's problem on a line of its own.
func printTriggers(out *printer, triggers []gen.IntakeTrigger) {
	if len(triggers) == 0 {
		out.printf("No triggers: a factory's triggers are its definition's board.triggers\n")

		return
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tSTATE\tWHEN\tCREATES\t7 DAYS")

	for _, tr := range triggers {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", tr.Name, onOff(tr.Enabled), tr.When, creates(tr), sum(tr.Days))
	}

	_ = tw.Flush()

	out.printf("%s", b.String())

	for _, tr := range triggers {
		if tr.Problem != nil {
			out.printf("! %s: %s\n", tr.Name, oneLine(tr.Problem.Message))
		}
	}
}

// triggersShow is goal triggers show <factory> <trigger> [--json]: the trigger, its week and its
// recent events.
func triggersShow(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("triggers show")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.GetIntakeTriggerWithResponse(ctx, c.org, pos[0], pos[1])
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	printTrigger(out, pos[0], res.JSON200)

	return nil
}

func printTrigger(out *printer, factory string, d *gen.IntakeTriggerDetail) {
	tr := d.Trigger

	out.printf("%s: %s\n", tr.Name, onOff(tr.Enabled))
	out.printf("  On       %s\n", tr.On)
	out.printf("  When     %s\n", tr.When)
	out.printf("  Creates  %s\n", creates(tr))

	if tr.Problem != nil {
		out.printf("  Problem  %s\n", oneLine(tr.Problem.Message))
	}

	out.printf("Last 7 days: %d created, %d merged, %s spent\n", d.Week.Created, d.Week.Merged, usd(d.Week.SpendUsd))

	if len(d.Events) == 0 {
		out.printf("No events\n")

		return
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tAT\tOUTCOME\tTITLE\tITEM")

	for _, e := range d.Events {
		item := "-"

		switch {
		case e.ItemNumber != nil:
			item = factory + "#" + strconv.FormatInt(*e.ItemNumber, 10)
		case e.Reason != nil:
			item = oneLine(*e.Reason)
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Id, stamp(e.At), e.Outcome, cut(oneLine(e.Title), intakeTitleWidth), item)
	}

	_ = tw.Flush()

	out.printf("%s", b.String())
}

// triggersTry is goal triggers try <factory> <trigger> --event ID [--json]: what the trigger would
// do now with one of its 20 newest events. Nothing is created, but the model check runs and its
// cost is spent.
func triggersTry(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("triggers try")
	event := fs.String("event", "", "the event's id (the ID column of goal triggers show)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 || *event == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.TryIntakeTriggerWithResponse(ctx, c.org, pos[0], pos[1], gen.TriggerTryRequest{Firing: *event})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	printTry(out, pos[1], res.JSON200)

	return nil
}

// printTry prints a dry run: whether the event matched, its outcome, each step, the item it would
// create and the model check, then that nothing was created. Event-shaped text goes through
// oneLine, so it cannot break the layout.
func printTry(out *printer, trigger string, tr *gen.TriggerTry) {
	matched := "did not match"
	if tr.Matched {
		matched = "matched"
	}

	out.printf("%s with event %s: %s\n", trigger, tr.Firing, matched)

	outcome := string(tr.Outcome)
	if tr.Reason != nil {
		outcome += " (" + oneLine(*tr.Reason) + ")"
	}

	out.printf("Outcome  %s\n", outcome)

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "STEP\tOUTCOME\tREASON")

	for _, s := range tr.Steps {
		reason := "-"
		if s.Reason != nil && *s.Reason != "" {
			reason = oneLine(*s.Reason)
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Step, s.Outcome, reason)
	}

	_ = tw.Flush()

	out.printf("%s", b.String())

	if it := tr.Item; it != nil {
		lane := "-"
		if it.Lane != nil && *it.Lane != "" {
			lane = *it.Lane
		}

		out.printf("Item: %s at %s, lane %s, expedite %s\n", it.Type, it.Station, lane, yesNo(it.Expedite))
		out.printf("  Title  %s\n", oneLine(it.Title))
	} else {
		out.printf("Item: none would be created\n")
	}

	if m := tr.ModelCheck; m != nil {
		out.printf("Model check: worth %s, %s, %d in and %d out tokens, cost %s\n",
			yesNo(m.Worth), m.Model, m.InputTokens, m.OutputTokens, fmt.Sprintf("$%.4f", m.CostUsd))
		out.printf("  Reason  %s\n", oneLine(m.Reason))
	} else {
		out.printf("Model check: did not run\n")
	}

	out.printf("Nothing was created: this was a dry run (a model check's cost is still spent)\n")
}

// creates is what a trigger creates: its work type, the station it starts at, and expedite and
// the lane when it sets them.
func creates(tr gen.IntakeTrigger) string {
	s := tr.Create.Type + " → " + tr.Create.Station

	if tr.Create.Expedite {
		s += ", expedite"
	}

	if tr.Create.Lane != nil && *tr.Create.Lane != "" {
		s += ", lane " + *tr.Create.Lane
	}

	return s
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}

	return "off"
}

func sum(days []int) int {
	n := 0
	for _, d := range days {
		n += d
	}

	return n
}

// webhooksList is goal webhooks list <factory> [--json]: the endpoints, never a secret.
func webhooksList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks list")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.ListWebhookEndpointsWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	printEndpoints(out, pos[0], res.JSON200.Endpoints)

	return nil
}

func printEndpoints(out *printer, factory string, endpoints []gen.WebhookEndpoint) {
	if len(endpoints) == 0 {
		out.printf("No webhook endpoints: goal webhooks create %s <name> adds one\n", factory)

		return
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tKIND\tADDRESS\tSECRET\tTRIGGERS\t7 DAYS")

	for _, e := range endpoints {
		delivered, refused := 0, 0
		for _, d := range e.Days {
			delivered, refused = delivered+d.Delivered, refused+d.Refused
		}

		feeds := "-"
		if len(e.Triggers) > 0 {
			feeds = strings.Join(e.Triggers, ", ")
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d delivered, %d refused\n", e.Name, e.Kind, e.Address, e.SecretName, feeds, delivered, refused)
	}

	_ = tw.Flush()

	out.printf("%s", b.String())
}

// webhooksCreate is goal webhooks create <factory> <name> [--kind generic|github] [--json].
func webhooksCreate(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks create")
	kind := fs.String("kind", string(gen.WebhookKindGeneric), "how requests are signed: generic or github")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	if k := gen.WebhookKind(*kind); k != gen.WebhookKindGeneric && k != gen.WebhookKindGithub {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.CreateWebhookEndpointWithResponse(ctx, c.org, pos[0], gen.NewWebhookEndpoint{Name: pos[1], Kind: gen.WebhookKind(*kind)})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	e := res.JSON201
	printSecret(out, *asJSON, res.Body, e)
	out.noticef("✓ created webhook endpoint %s (%s) in %s: senders post to %s on goald's public origin\n", e.Name, e.Kind, pos[0], e.Address)
	secretWarning(out)

	return nil
}

// webhooksRotate is goal webhooks rotate <factory> <name> [--json]: a new signing secret; the old
// one stops working at once.
func webhooksRotate(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks rotate")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	id, err := endpointID(ctx, c, pos[0], pos[1])
	if err != nil {
		return err
	}

	res, err := c.RotateWebhookSecretWithResponse(ctx, c.org, pos[0], id)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	e := res.JSON200
	printSecret(out, *asJSON, res.Body, e)
	out.noticef("✓ rotated the signing secret of %s (%s) in %s, which receives at %s: the old secret stops working at once,\n", e.Name, e.Kind, pos[0], e.Address)
	out.noticef("  so its sender's requests are refused until it has the new one\n")
	secretWarning(out)

	return nil
}

// printSecret prints the secret alone on stdout, so `goal webhooks create … > file` keeps only
// it, or with --json goald's whole answer, which carries it. These are the only two places a
// signing secret is printed.
func printSecret(out *printer, asJSON bool, body []byte, e *gen.CreatedWebhookEndpoint) {
	if asJSON {
		out.printf("%s", body)

		return
	}

	out.printf("%s\n", e.Secret)
}

func secretWarning(out *printer) {
	out.noticef("  the signing secret is shown once: store it now in the sender (for github, the webhook's Secret field); goald never shows it again\n")
}

// webhooksDeliveries is goal webhooks deliveries <factory> <name> [--limit N] [--after C] [--json]:
// the deliveries an endpoint received, newest first.
func webhooksDeliveries(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks deliveries")
	limit := fs.Int("limit", 0, "how many deliveries (default goald's page)")
	after := fs.String("after", "", "the next page's cursor, as the previous page printed it")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	// --limit, when given, is at least 1; goald says how many at most.
	limitGiven := false

	fs.Visit(func(f *flag.Flag) { limitGiven = limitGiven || f.Name == "limit" })

	if limitGiven && *limit < 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	id, err := endpointID(ctx, c, pos[0], pos[1])
	if err != nil {
		return err
	}

	params := &gen.ListWebhookDeliveriesParams{}
	if limitGiven {
		params.Limit = limit
	}

	if *after != "" {
		params.After = after
	}

	res, err := c.ListWebhookDeliveriesWithResponse(ctx, c.org, pos[0], id, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	printDeliveries(out, pos[0], pos[1], res.JSON200)

	return nil
}

func printDeliveries(out *printer, factory, endpoint string, p *gen.WebhookDeliveryPage) {
	if len(p.Deliveries) == 0 {
		out.printf("No deliveries\n")
	} else {
		var b bytes.Buffer

		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "RECEIVED\tOUTCOME\tREASON\tITEM")

		for _, d := range p.Deliveries {
			reason, item := "-", "-"
			if d.Reason != "" {
				reason = oneLine(d.Reason)
			}

			if d.Item != nil {
				item = factory + "#" + strconv.FormatInt(d.Item.Number, 10)
			}

			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", stamp(d.ReceivedAt), d.Outcome, reason, item)
		}

		_ = tw.Flush()

		out.printf("%s", b.String())
	}

	if p.NextAfter != nil && *p.NextAfter != "" {
		out.printf("next: goal webhooks deliveries %s %s --after %s\n", factory, endpoint, *p.NextAfter)
	}
}

// endpointID finds the id of the factory's webhook endpoint by its name (or its id), since goald's
// paths take the id.
func endpointID(ctx context.Context, c orgClient, factory, ref string) (string, error) {
	res, err := c.ListWebhookEndpointsWithResponse(ctx, c.org, factory)
	if err != nil {
		return "", apiDown(err)
	}

	if res.JSON200 == nil {
		return "", problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	for _, e := range res.JSON200.Endpoints {
		if e.Name == ref || e.Id == ref {
			return e.Id, nil
		}
	}

	return "", fmt.Errorf("no webhook endpoint named %q in %s: `goal webhooks list %s` shows them", ref, factory, factory)
}
