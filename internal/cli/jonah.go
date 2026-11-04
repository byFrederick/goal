package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/byFrederick/goal/internal/api/gen"
)

// jonahCommand is goal jonah: Jonah's proposals of definition changes made from sent-back runs
// (ADR 0034), listed, shown as diffs and decided, and a manual review queued. With no
// subcommand it lists the proposals.
func jonahCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) > 0 {
		if handler, ok := map[string]command{
			subShow: jonahShow, verbApprove: jonahApprove, jonahEditCmd: jonahEdit, verbReject: jonahReject, jonahLookCmd: jonahLook,
			jonahTryCmd: jonahTry,
		}[args[0]]; ok {
			return handler(ctx, out, getenv, args[1:])
		}
	}

	return jonahList(ctx, out, getenv, args)
}

// The goal jonah subcommands without a constant elsewhere in the package.
const (
	jonahEditCmd = "edit"
	jonahLookCmd = "look"
	jonahTryCmd  = "try"
)

// jonahConflicts are the one-line messages of the 409 codes the Jonah operations document.
var jonahConflicts = map[string]string{
	"draft-open":                "the factory has an open draft: publish or discard it, then approve again, or use goal jonah edit",
	"proposal-stale":            "a file changed since Jonah made this proposal, so it no longer applies: reject it",
	"draft-conflict":            "the draft's copy of a file changed since Jonah made this proposal: nothing was written",
	"proposal-decided":          "the proposal was already decided: goal jonah show %d says how",
	"review-in-flight":          "a review is already pending or running: wait for it to finish",
	"proposal-not-tried":        "Jonah has not tried this proposal on past items yet: its first tries are queued soon, then try again",
	"learning-budget-exhausted": "Jonah's runs used up its budget, learning.jonah.share of what the line spent in the last 7 days: try again when the window moves on",
}

// jonahProblem is goald's refusal of a Jonah operation on proposal number (0 for none): a
// documented 409 is its one-line message, anything else what problem says.
func jonahProblem(res *http.Response, p *gen.Problem, number int64) error {
	if res != nil && res.StatusCode == http.StatusConflict {
		if msg, ok := jonahConflicts[problemCode(p)]; ok {
			if strings.Contains(msg, "%d") {
				msg = fmt.Sprintf(msg, number)
			}

			return errors.New(msg)
		}
	}

	return problem(res, p)
}

// jonahList is goal jonah [--factory F] [--state S]: the factory's proposals, newest first.
func jonahList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("jonah")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")
	state := fs.String("state", "", "only proposals in this state: proposed, published, applied, rejected or superseded")

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	params := &gen.ListProposalsParams{}

	if *state != "" {
		s := gen.ProposalState(*state)
		if !s.Valid() {
			return errUsage
		}

		params.State = &s
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	f, err := commandFactory(ctx, c, *factory)
	if err != nil {
		return err
	}

	res, err := c.ListProposalsWithResponse(ctx, c.org, f, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if len(res.JSON200.Proposals) == 0 {
		out.printf("no proposals\n")

		return nil
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NUMBER\tTITLE\tSTATE\tPATHS")

	for _, p := range res.JSON200.Proposals {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", p.Number, oneLine(p.Title), p.State, strings.Join(p.Paths, ", "))
	}

	_ = tw.Flush()

	out.printf("%s", b.String())

	if res.JSON200.Next != nil {
		out.printf("more proposals are older than #%d\n", *res.JSON200.Next)
	}

	return nil
}

// jonahShow is goal jonah show <number> [--factory F]: the proposal, its evidence and a unified
// diff of each file from the version it was made against.
func jonahShow(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	c, f, number, err := jonahProposalArgs(ctx, getenv, "jonah show", args, nil)
	if err != nil {
		return err
	}

	res, err := c.GetProposalWithResponse(ctx, c.org, f, number)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	p := res.JSON200

	out.printf("proposal #%d: %s (%s), made against v%d, publishes as v%d\n", p.Number, oneLine(p.Title), p.State, p.MadeAgainstVersion, p.PublishesAsVersion)

	if p.Summary != "" {
		out.printf("\n%s\n", strings.TrimSpace(p.Summary))
	}

	if r := deref(p.RejectReason); r != "" {
		out.printf("\nrejected: %s\n", oneLine(r))
	}

	if len(p.Evidence) > 0 {
		out.printf("\nevidence:\n")

		for _, e := range p.Evidence {
			at := ""
			if s := deref(e.Station); s != "" {
				at = " at " + s
			}

			out.printf("  %s%s: %q\n", e.ItemRef, at, oneLine(e.Quote))
		}
	}

	for _, file := range p.Files {
		out.printf("\n%s", unifiedDiff(file.Path, file.BaseContent, file.Content))
	}

	return nil
}

// jonahApprove is goal jonah approve <number> [--factory F]: publish the proposal as a new version.
func jonahApprove(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	return jonahDecide(ctx, out, getenv, verbApprove, args)
}

// jonahEdit is goal jonah edit <number> [--factory F]: write the proposal's files into the draft.
func jonahEdit(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	return jonahDecide(ctx, out, getenv, jonahEditCmd, args)
}

// jonahReject is goal jonah reject <number> --reason R [--factory F]: reject the proposal; later
// reviews read the reason.
func jonahReject(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	return jonahDecide(ctx, out, getenv, verbReject, args)
}

// jonahDecide decides a proposal by verb (approve, edit or reject) and prints the outcome. A
// reject without a reason, or with a blank one, is the usage, found before goald is called.
func jonahDecide(ctx context.Context, out *printer, getenv func(string) string, verb string, args []string) error {
	var reason string

	var extra func(*flag.FlagSet) func() bool
	if verb == verbReject {
		extra = func(fs *flag.FlagSet) func() bool {
			r := fs.String("reason", "", "why it is rejected (required)")

			return func() bool {
				reason = strings.TrimSpace(*r)

				return reason != ""
			}
		}
	}

	c, f, number, err := jonahProposalArgs(ctx, getenv, "jonah "+verb, args, extra)
	if err != nil {
		return err
	}

	var (
		res     *http.Response
		d       *gen.ProposalDecided
		refusal *gen.Problem
	)

	switch verb {
	case verbApprove:
		r, err := c.ApproveProposalWithResponse(ctx, c.org, f, number)
		if err != nil {
			return apiDown(err)
		}

		res, d, refusal = r.HTTPResponse, r.JSON200, r.ApplicationproblemJSONDefault
	case jonahEditCmd:
		r, err := c.EditProposalWithResponse(ctx, c.org, f, number)
		if err != nil {
			return apiDown(err)
		}

		res, d, refusal = r.HTTPResponse, r.JSON200, r.ApplicationproblemJSONDefault
	default:
		r, err := c.RejectProposalWithResponse(ctx, c.org, f, number, gen.RejectProposalJSONRequestBody{Reason: reason})
		if err != nil {
			return apiDown(err)
		}

		res, d, refusal = r.HTTPResponse, r.JSON200, r.ApplicationproblemJSONDefault
	}

	if d == nil {
		return jonahProblem(res, refusal, number)
	}

	switch verb {
	case verbApprove:
		out.printf("proposal #%d published as v%d\n", d.Number, deref(d.Version))
	case jonahEditCmd:
		out.printf("proposal #%d written into the draft (revision %d): continue in the builder\n", d.Number, deref(d.DraftRevision))
	default:
		out.printf("proposal #%d rejected\n", d.Number)
	}

	return nil
}

// jonahLook is goal jonah look [--factory F]: it queues a manual review of the factory at once
// ("Look for improvements now", ADR 0039 decision 3) and prints the queued review. A review in
// flight or a spent weekly budget is the 409's one-line message.
func jonahLook(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("jonah look")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	f, err := commandFactory(ctx, c, *factory)
	if err != nil {
		return err
	}

	r, err := c.QueueJonahReviewWithResponse(ctx, c.org, f)
	if err != nil {
		return apiDown(err)
	}

	if r.JSON202 == nil {
		return jonahProblem(r.HTTPResponse, r.ApplicationproblemJSONDefault, 0)
	}

	out.printf("review %s queued: goal jonah lists its proposals once it finishes\n", r.JSON202.Id)

	return nil
}

// jonahTry is goal jonah try <number> [--factory F] [--yes] [--json]: print the estimate of
// trying the proposal again (ADR 0039 decision 11), one more try per item it was tried on or
// three for a model proposal, ask, then queue the tries. --yes queues them without asking; --json
// prints goald's answers as they came, and without --yes the estimate only.
func jonahTry(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	var yes, asJSON *bool

	c, f, number, err := jonahProposalArgs(ctx, getenv, "jonah try", args, func(fs *flag.FlagSet) func() bool {
		yes = fs.Bool("yes", false, "queue the tries without asking")
		asJSON = fs.Bool("json", false, "print goald's answers as they came; without --yes, the estimate only")

		return func() bool { return true }
	})
	if err != nil {
		return err
	}

	t := tryAgain{c: c, factory: f, number: number, json: *asJSON}

	est, err := t.try(ctx, out, false)
	if err != nil || (*asJSON && !*yes) {
		return err
	}

	if !*yes {
		if ok, err := askYes(out, fmt.Sprintf("Queue %s of proposal #%d? [y/N] ", plural(est.Tries, "try"), number), "nothing was queued"); err != nil || !ok {
			return err
		}
	}

	_, err = t.try(ctx, out, true)

	return err
}

// tryAgain is one goal jonah try: proposal number of factory.
type tryAgain struct {
	c       orgClient
	factory string
	number  int64
	json    bool
}

// try asks goald for the estimate, or with confirm for the tries, and prints the answer: as it
// came with --json, else the estimate or what was queued.
func (t tryAgain) try(ctx context.Context, out *printer, confirm bool) (gen.TryProposalEstimate, error) {
	body := gen.TryJonahProposalJSONRequestBody{}
	if confirm {
		body.Confirm = &confirm
	}

	res, err := t.c.TryJonahProposalWithResponse(ctx, t.c.org, t.factory, t.number, body)
	if err != nil {
		return gen.TryProposalEstimate{}, apiDown(err)
	}

	answer := res.JSON200
	if confirm {
		answer = res.JSON201
	}

	if answer == nil {
		return gen.TryProposalEstimate{}, jonahProblem(res.HTTPResponse, res.ApplicationproblemJSONDefault, t.number)
	}

	switch {
	case t.json:
		out.printf("%s", res.Body)
	case confirm:
		out.printf("✓ %s of proposal #%d queued\n", plural(answer.Estimate.Tries, "try"), t.number)
		out.printf("see them with: goal jonah show %d --factory %s\n", t.number, t.factory)
	default:
		printTryEstimate(out, answer.Estimate)
	}

	return answer.Estimate, nil
}

// printTryEstimate prints what trying a proposal again adds: each item with its station and tries,
// then the tries, their cost and what is left of Jonah's budget.
func printTryEstimate(out *printer, e gen.TryProposalEstimate) {
	out.printf("try proposal #%d again on the %s it was tried on:\n", e.Number, plural(len(e.Items), "item"))

	var tb bytes.Buffer

	tw := tabwriter.NewWriter(&tb, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ITEM\tSTATION\tTRIES\tTITLE")

	for _, it := range e.Items {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", it.ItemRef, it.Station, it.Tries, cut(oneLine(it.ItemTitle), 60))
	}

	_ = tw.Flush()

	out.printf("%s", trimLines(tb.String()))
	out.printf("%s, about %s; %s left of Jonah's budget\n", plural(e.Tries, "try"), usd(e.EstimateUsd), usd(e.BudgetLeftUsd))
}

// jonahProposalArgs parses <number> [--factory F] [--org O] and the flags extra declares (its
// check, run after parsing, says whether they are valid), then connects and resolves the
// factory. Every usage error is found before goald is called.
func jonahProposalArgs(ctx context.Context, getenv func(string) string, name string, args []string, extra func(*flag.FlagSet) func() bool) (orgClient, string, int64, error) {
	fs := newFlags(name)
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")

	valid := func() bool { return true }
	if extra != nil {
		valid = extra(fs)
	}

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || !valid() {
		return orgClient{}, "", 0, errUsage
	}

	number, err := strconv.ParseInt(strings.TrimPrefix(pos[0], "#"), 10, 64)
	if err != nil || number < 1 {
		return orgClient{}, "", 0, errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return orgClient{}, "", 0, err
	}

	f, err := commandFactory(ctx, c, *factory)
	if err != nil {
		return orgClient{}, "", 0, err
	}

	return c, f, number, nil
}

// commandFactory is the factory a factory-scoped command acts on: --factory, else the
// organization's only factory.
func commandFactory(ctx context.Context, c orgClient, factory string) (string, error) {
	if factory != "" {
		return factory, nil
	}

	res, err := c.ListFactoriesWithResponse(ctx, c.org, nil)
	if err != nil {
		return "", apiDown(err)
	}

	if res.JSON200 == nil {
		return "", problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	names := make([]string, 0, len(res.JSON200.Factories))
	for _, f := range res.JSON200.Factories {
		names = append(names, f.Name)
	}

	switch len(names) {
	case 0:
		return "", fmt.Errorf("%s has no factories", c.org)
	case 1:
		return names[0], nil
	default:
		return "", fmt.Errorf("%s has factories %s: pass --factory", c.org, strings.Join(names, ", "))
	}
}
