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
)

// jonahConflicts are the one-line messages of the 409 codes the Jonah operations document.
var jonahConflicts = map[string]string{
	"draft-open":                "the factory has an open draft: publish or discard it, then approve again, or use goal jonah edit",
	"proposal-stale":            "a file changed since Jonah made this proposal, so it no longer applies: reject it",
	"draft-conflict":            "the draft's copy of a file changed since Jonah made this proposal: nothing was written",
	"proposal-decided":          "the proposal was already decided: goal jonah show %d says how",
	"review-in-flight":          "a review is already pending or running: wait for it to finish",
	"learning-budget-exhausted": "Jonah's runs reached the factory's weekly budget (learning.jonah.weeklyUsd): try again when the last 7 days cost less",
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

// jonahLook is goal jonah look [--factory F]: queue a manual review of the sent-back runs.
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

	res, err := c.QueueJonahReviewWithResponse(ctx, c.org, f)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON202 == nil {
		return jonahProblem(res.HTTPResponse, res.ApplicationproblemJSONDefault, 0)
	}

	out.printf("queued Jonah review %s\n", res.JSON202.Id)

	return nil
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
