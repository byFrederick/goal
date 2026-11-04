package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The intake commands' subcommands that other groups do not have.
const (
	subShow       = "show"
	subDeliveries = "deliveries"
	subOpen       = "open"
	subClose      = "close"
)

// intakeTitleWidth is how much of an event's title a row of goal triggers show keeps.
const intakeTitleWidth = 60

// triggersCommand is goal triggers list|show|try|checks: a factory's triggers (ADR 0026) as its
// current definition declares them, with what they created, a dry run of one against a past event
// (ADR 0028 decision 2) or of its check, and its checks (ADR 0040 decision 19). Triggers change
// only through the definition (goal factory push).
func triggersCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{subList: triggersList, subShow: triggersShow, "try": triggersTry, "checks": triggersChecks}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// webhooksCommand is goal webhooks list|create|open|close|deliveries|delete: the organization's
// webhooks (ADR 0040 decisions 12 and 22), the factories they are open to and the deliveries they
// received. A webhook's secret is an organization secret: its value changes through goal secret.
func webhooksCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{
		subList: webhooksList, verbCreate: webhooksCreate, subOpen: webhooksOpen, subClose: webhooksClose,
		subDeliveries: webhooksDeliveries, "delete": webhooksDelete,
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
// do now with one of its 20 newest events. Nothing is created. Without --event it tries the
// trigger's check (tryCheck).
func triggersTry(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("triggers try")
	event := fs.String("event", "", "the event's id (the ID column of goal triggers show)")
	wait := fs.Duration("wait", defaultCheckWait, "for a check: how long to wait for it")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 || *wait <= 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	if *event == "" {
		return tryCheck(ctx, out, c, pos[0], pos[1], *asJSON, *wait, pollEvery)
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
// create, then that nothing was created. Event-shaped text goes through
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

	out.printf("Nothing was created: this was a dry run\n")
}

// defaultCheckWait is how long goal triggers try waits for a check by default: a check's default
// timeout, 5 minutes, and a minute for it to be leased and recorded.
const defaultCheckWait = 6 * time.Minute

// tryCheck posts a try check of the trigger and reads its checks every interval until the try is
// done or failed, or wait has passed, then prints it (printTriggerCheck). A try still queued or running
// then says so and where to read it later.
func tryCheck(ctx context.Context, out *printer, c orgClient, factory, trigger string, asJSON bool, wait, every time.Duration) error {
	res, err := c.TryTriggerCheckWithResponse(ctx, c.org, factory, trigger)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON202 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	id := res.JSON202.Id
	deadline := time.Now().Add(wait)

	t := time.NewTicker(every)
	defer t.Stop()

	for {
		list, err := c.ListTriggerChecksWithResponse(ctx, c.org, factory, trigger, &gen.ListTriggerChecksParams{})
		if err != nil {
			return apiDown(err)
		}

		if list.JSON200 == nil {
			return problem(list.HTTPResponse, list.ApplicationproblemJSONDefault)
		}

		i := slices.IndexFunc(list.JSON200.Checks, func(ch gen.TriggerCheck) bool { return ch.Id == id })
		if i >= 0 && finished(list.JSON200.Checks[i]) {
			return printTried(out, factory, trigger, list.JSON200.Checks[i], asJSON)
		}

		if time.Now().After(deadline) {
			out.printf("The try check of %s is still %s after %s; read it later with goal triggers checks %s %s\n", trigger, checkState(list.JSON200.Checks, i), wait, factory, trigger)

			return nil
		}

		select {
		case <-ctx.Done():
			return errors.New("stopped waiting; the try check keeps running in goald")
		case <-t.C:
		}
	}
}

// finished reports whether ch is done or failed.
func finished(ch gen.TriggerCheck) bool {
	return ch.State == gen.TriggerCheckStateDone || ch.State == gen.TriggerCheckStateFailed
}

// checkState is checks[i]'s state, queued when it is not among them yet.
func checkState(checks []gen.TriggerCheck, i int) string {
	if i < 0 {
		return string(gen.TriggerCheckStateQueued)
	}

	return string(checks[i].State)
}

// printTried prints a finished try check, then that nothing was filed, noted or saved.
func printTried(out *printer, factory, trigger string, ch gen.TriggerCheck, asJSON bool) error {
	if asJSON {
		b, err := json.Marshal(ch)
		if err != nil {
			return fmt.Errorf("print the try check: %w", err)
		}

		out.printf("%s\n", b)

		return nil
	}

	out.printf("Try check of %s:\n", trigger)
	printTriggerCheck(out, factory, ch)
	out.printf("Nothing was filed, noted or saved: this was a try\n")

	return nil
}

// triggersChecks is goal triggers checks <factory> <trigger> [--limit N] [--json]: the trigger's
// checks, newest first.
func triggersChecks(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("triggers checks")
	limit := fs.Int("limit", 0, "how many checks, 1 to 100 (default 20)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 || *limit < 0 || *limit > 100 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	params := &gen.ListTriggerChecksParams{}
	if *limit > 0 {
		params.Limit = limit
	}

	res, err := c.ListTriggerChecksWithResponse(ctx, c.org, pos[0], pos[1], params)
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

	if len(res.JSON200.Checks) == 0 {
		out.printf("No checks yet\n")

		return nil
	}

	for _, ch := range res.JSON200.Checks {
		printTriggerCheck(out, pos[0], ch)
	}

	return nil
}

// printTriggerCheck prints one check: when, its state (a try marked), its cost, then a row for each
// item it filed, note it wrote and key it skipped, with why, "Nothing new" for a done check with
// none, or why it failed. Text from the source goes through oneLine, so it cannot break the layout.
func printTriggerCheck(out *printer, factory string, ch gen.TriggerCheck) {
	state := string(ch.State)
	if ch.Try {
		state += ", try"
	}

	out.printf("%s  %s  %s\n", stamp(ch.At), state, usd(ch.CostUsd))

	if ch.Reason != nil {
		out.printf("  Failed: %s\n", oneLine(*ch.Reason))
	}

	if ch.State != gen.TriggerCheckStateDone {
		return
	}

	if len(ch.Filed)+len(ch.Noted)+len(ch.Skipped) == 0 {
		out.printf("  Nothing new\n")

		return
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	for _, f := range ch.Filed {
		item := "would file"
		if f.Ref != nil {
			item = factory + *f.Ref
		}

		if f.Reopens != nil {
			item += ", reopens " + factory + *f.Reopens
		}

		_, _ = fmt.Fprintf(tw, "  Filed\t%s\t%s\t%s\n", cut(oneLine(f.Key), intakeTitleWidth), item, cut(oneLine(f.Title), intakeTitleWidth))
	}

	for _, n := range ch.Noted {
		_, _ = fmt.Fprintf(tw, "  Noted\t%s\t%s\t%s\n", cut(oneLine(n.Key), intakeTitleWidth), factory+n.Ref, cut(oneLine(n.Text), intakeTitleWidth))
	}

	for _, s := range ch.Skipped {
		_, _ = fmt.Fprintf(tw, "  Skipped\t%s\t-\t%s\n", cut(oneLine(s.Key), intakeTitleWidth), cut(oneLine(s.Reason), intakeTitleWidth))
	}

	_ = tw.Flush()

	out.printf("%s", b.String())
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

// webhooksList is goal webhooks list [--json]: the organization's webhooks, never a secret's
// value.
func webhooksList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks list")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.ListWebhooksWithResponse(ctx, c.org)
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

	names, err := openedFactoryNames(ctx, c, res.JSON200.Webhooks)
	if err != nil {
		return err
	}

	printWebhooks(out, res.JSON200.Webhooks, names)

	return nil
}

// openedFactoryNames are factoryNames when a webhook has an opening, and none otherwise, so
// goald is asked only when there is a factory to name.
func openedFactoryNames(ctx context.Context, c orgClient, hooks []gen.Webhook) (map[string]string, error) {
	if !slices.ContainsFunc(hooks, func(w gen.Webhook) bool { return len(w.Openings) > 0 }) {
		return map[string]string{}, nil
	}

	return factoryNames(ctx, c)
}

func printWebhooks(out *printer, hooks []gen.Webhook, names map[string]string) {
	if len(hooks) == 0 {
		out.printf("No webhooks: goal webhooks create <name> --sender <sender> adds one\n")

		return
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tSENDER\tSTATE\tADDRESS\tSECRET\tOPEN TO\tTRIGGERS\t7 DAYS")

	for _, w := range hooks {
		delivered, refused := 0, 0
		for _, d := range w.Days {
			delivered, refused = delivered+d.Delivered, refused+d.Refused
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d delivered, %d refused\n",
			w.Name, w.Sender, onOff(w.Enabled), w.Address, w.Secret, openTo(w.Openings, names), triggersOf(w.Triggers), delivered, refused)
	}

	_ = tw.Flush()

	out.printf("%s", b.String())
}

// openTo names the factories of openings, each with its repositories when it is limited to some.
func openTo(openings []gen.WebhookOpening, names map[string]string) string {
	if len(openings) == 0 {
		return "-"
	}

	parts := make([]string, 0, len(openings))

	for _, o := range openings {
		name := cmp.Or(names[o.Factory], o.Factory)
		if o.Repositories != nil && len(*o.Repositories) > 0 {
			name += " (" + strings.Join(*o.Repositories, ", ") + ")"
		}

		parts = append(parts, name)
	}

	return strings.Join(parts, ", ")
}

// triggersOf names the published triggers that use a webhook as factory/trigger.
func triggersOf(triggers []gen.WebhookTrigger) string {
	if len(triggers) == 0 {
		return "-"
	}

	parts := make([]string, 0, len(triggers))
	for _, t := range triggers {
		parts = append(parts, t.FactoryName+"/"+t.Trigger)
	}

	return strings.Join(parts, ", ")
}

// senderOfKind maps the factory endpoints' --kind to its sender: generic is standard's alias for
// one release (ADR 0040 decision 22).
var senderOfKind = map[string]gen.WebhookSender{"generic": gen.WebhookSenderStandard, "github": gen.WebhookSenderGithub}

// webhooksCreate is goal webhooks create <name> [--sender S | --kind generic|github]
// [--secret <name>] [--json]: an enabled webhook open to no factory, signed by the secret named or
// by a new webhook-<name> whose generated value is printed once.
func webhooksCreate(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks create")
	sender := fs.String("sender", "", "how deliveries are signed: github, gitlab, standard (the default), pagerduty, sentry or token")
	kind := fs.String("kind", "", "generic or github: the old name of --sender, generic being standard")
	secret := fs.String("secret", "", "the existing secret that signs deliveries; default a new webhook-<name> with a generated value")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	n, ok := newWebhook(pos[0], *sender, *kind, *secret)
	if !ok {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.CreateWebhookWithResponse(ctx, c.org, n)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	w := res.JSON201
	if *asJSON {
		out.printf("%s", res.Body)
	} else if w.SecretValue != nil {
		out.printf("%s\n", *w.SecretValue)
	}

	out.noticef("✓ created webhook %s (%s), signed by the secret %s: senders post to %s on goald's public origin\n", w.Name, w.Sender, w.Secret, w.Address)
	out.noticef("  it is open to no factory yet: goal webhooks open %s <factory>\n", w.Name)

	if w.SecretValue != nil {
		out.noticef("  the secret's value is shown once: store it now in the sender (for github, the webhook's Secret field); goald never shows it again\n")
	}

	return nil
}

// newWebhook is create's body: the sender from --sender or its alias --kind (standard when neither
// is given), false for both, an unknown one or a name the secret flag leaves empty.
func newWebhook(name, sender, kind, secret string) (gen.NewWebhook, bool) {
	n := gen.NewWebhook{Name: name, Sender: gen.WebhookSenderStandard}

	switch {
	case sender != "" && kind != "":
		return n, false
	case kind != "":
		s, ok := senderOfKind[kind]
		if !ok {
			return n, false
		}

		n.Sender = s
	case sender != "":
		n.Sender = gen.WebhookSender(sender)
		if !slices.Contains([]gen.WebhookSender{
			gen.WebhookSenderGithub, gen.WebhookSenderGitlab, gen.WebhookSenderStandard, gen.WebhookSenderPagerduty, gen.WebhookSenderSentry, gen.WebhookSenderToken,
		}, n.Sender) {
			return n, false
		}
	}

	if secret != "" {
		n.Secret = &secret
	}

	return n, true
}

// webhooksOpen is goal webhooks open <webhook> <factory> [--repos a,b] [--json]: the factory
// receives the webhook's deliveries, only those of the repositories listed when --repos is given.
// Opening it again replaces the repositories.
func webhooksOpen(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks open")
	repos := fs.String("repos", "", "owner/name of the repositories whose deliveries reach the factory, comma-separated; default every one")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	var list []string

	for r := range strings.SplitSeq(*repos, ",") {
		if r = strings.TrimSpace(r); r != "" {
			list = append(list, r)
		}
	}

	return patchOpening(ctx, out, getenv, *orgFlag, *asJSON, pos, func(factory string) gen.WebhookPatch {
		o := gen.WebhookOpening{Factory: factory}
		if len(list) > 0 {
			o.Repositories = &list
		}

		return gen.WebhookPatch{Open: &[]gen.WebhookOpening{o}}
	}, func(w *gen.Webhook) string {
		if len(list) == 0 {
			return fmt.Sprintf("✓ opened webhook %s to %s: every repository's deliveries reach it\n", w.Name, pos[1])
		}

		return fmt.Sprintf("✓ opened webhook %s to %s for %s\n", w.Name, pos[1], strings.Join(list, ", "))
	})
}

// webhooksClose is goal webhooks close <webhook> <factory> [--json]: the factory no longer
// receives the webhook's deliveries.
func webhooksClose(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks close")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	return patchOpening(ctx, out, getenv, *orgFlag, *asJSON, pos, func(factory string) gen.WebhookPatch {
		return gen.WebhookPatch{Close: &[]string{factory}}
	}, func(w *gen.Webhook) string {
		return fmt.Sprintf("✓ closed webhook %s to %s\n", w.Name, pos[1])
	})
}

// patchOpening patches the webhook pos[0] with what patch makes of the id of the factory pos[1],
// then prints goald's answer (--json) or done's line.
func patchOpening(ctx context.Context, out *printer, getenv func(string) string, org string, asJSON bool, pos []string,
	patch func(factory string) gen.WebhookPatch, done func(w *gen.Webhook) string,
) error {
	c, err := connect(ctx, getenv, org)
	if err != nil {
		return err
	}

	f, err := getFactory(ctx, c, pos[1])
	if err != nil {
		return err
	}

	res, err := c.PatchWebhookWithResponse(ctx, c.org, pos[0], patch(f.Id))
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	out.noticef("%s", done(res.JSON200))

	return nil
}

// webhooksDelete is goal webhooks delete <webhook>: the webhook and its deliveries; its secret
// stays, and the line names it. goald refuses while published triggers use it, naming them.
func webhooksDelete(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("webhooks delete")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	// Read first, so the line names the secret that stays.
	got, err := c.GetWebhookWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if got.JSON200 == nil {
		return problem(got.HTTPResponse, got.ApplicationproblemJSONDefault)
	}

	res, err := c.DeleteWebhookWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if res.StatusCode() != http.StatusNoContent {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	w := got.JSON200
	out.noticef("✓ deleted webhook %s and its deliveries; its secret %s stays (goal secret delete %s removes it)\n", w.Name, w.Secret, w.Secret)

	return nil
}

// webhooksDeliveries is goal webhooks deliveries <webhook> [--limit N] [--after C] [--json]: the
// deliveries a webhook received, newest first.
func webhooksDeliveries(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs, orgFlag, asJSON := intakeFlags("webhooks deliveries")
	limit := fs.Int("limit", 0, "how many deliveries (default goald's page)")
	after := fs.String("after", "", "the next page's cursor, as the previous page printed it")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
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

	params := &gen.ListOrgWebhookDeliveriesParams{}
	if limitGiven {
		params.Limit = limit
	}

	if *after != "" {
		params.After = after
	}

	res, err := c.ListOrgWebhookDeliveriesWithResponse(ctx, c.org, pos[0], params)
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

	printDeliveries(out, pos[0], res.JSON200)

	return nil
}

func printDeliveries(out *printer, webhook string, p *gen.WebhookDeliveryPage) {
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
				item = "#" + strconv.FormatInt(d.Item.Number, 10)
			}

			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", stamp(d.ReceivedAt), d.Outcome, reason, item)
		}

		_ = tw.Flush()

		out.printf("%s", b.String())
	}

	if p.NextAfter != nil && *p.NextAfter != "" {
		out.printf("next: goal webhooks deliveries %s --after %s\n", webhook, *p.NextAfter)
	}
}
