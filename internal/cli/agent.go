package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The refusals goal agent try-model explains beyond goald's problem (ADR 0039 decision 13).
const (
	codeModelNotAllowed = "model-not-allowed"
	codeAgentNotTriable = "agent-not-triable"
	codeNoFinishedItems = "no-finished-items"

	subTryModel = "try-model"
)

// agentCommand is goal agent: commands on one agent of a factory.
func agentCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 || args[0] != subTryModel {
		return errUsage
	}

	return agentTryModel(ctx, out, getenv, args[1:])
}

// agentTryModel is goal agent try-model <agent> <model> [--factory F] [--yes] [--json]: print the
// estimate of trying model for agent on its newest finished items, ask, then make the proposal
// (Try another model, ADR 0039 decision 13). --yes makes it without asking; --json prints goald's
// answers as they came, and without --yes the estimate only.
func agentTryModel(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("agent try-model")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")
	yes := fs.Bool("yes", false, "make the proposal without asking")
	asJSON := fs.Bool("json", false, "print goald's answers as they came; without --yes, the estimate only")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
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

	t := tryModel{c: c, factory: f, agent: pos[0], model: pos[1], json: *asJSON}

	est, err := t.propose(ctx, out, false)
	if err != nil || (*asJSON && !*yes) {
		return err
	}

	if !*yes {
		if ok, err := askYes(out, fmt.Sprintf("Propose %s for %s and queue its %s? [y/N] ", t.model, t.agent, plural(est.Tries, "try")),
			"nothing was proposed"); err != nil || !ok {
			return err
		}
	}

	_, err = t.propose(ctx, out, true)

	return err
}

// tryModel is one goal agent try-model: model for agent of factory.
type tryModel struct {
	c                     orgClient
	factory, agent, model string
	json                  bool
}

// propose asks goald for the estimate, or with confirm for the proposal, and prints the answer:
// as it came with --json, else the estimate or the proposal's number.
func (t tryModel) propose(ctx context.Context, out *printer, confirm bool) (gen.ModelProposalEstimate, error) {
	body := gen.ProposeAgentModelJSONRequestBody{Model: t.model}
	if confirm {
		body.Confirm = &confirm
	}

	res, err := t.c.ProposeAgentModelWithResponse(ctx, t.c.org, t.factory, t.agent, body)
	if err != nil {
		return gen.ModelProposalEstimate{}, apiDown(err)
	}

	answer := res.JSON200
	if confirm {
		answer = res.JSON201
	}

	if answer == nil || (confirm && answer.Proposal == nil) {
		return gen.ModelProposalEstimate{}, modelProposalProblem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	switch {
	case t.json:
		out.printf("%s", res.Body)
	case confirm:
		p := answer.Proposal
		out.printf("✓ proposal %d: %s, %s queued\n", p.Number, p.Title, plural(answer.Estimate.Tries, "try"))
		out.printf("see it with: goal jonah show %d --factory %s\n", p.Number, t.factory)
	default:
		printModelEstimate(out, answer.Estimate)
	}

	return answer.Estimate, nil
}

// askYes asks question and reports whether the answer is y or yes; any other answer prints
// declined.
func askYes(out *printer, question, declined string) (bool, error) {
	out.printf("%s", question)

	answer, err := readLine(out.in)
	if err != nil {
		return false, err
	}

	if a := strings.ToLower(strings.TrimSpace(answer)); a == "y" || a == "yes" {
		return true, nil
	}

	out.printf("%s\n", declined)

	return false, nil
}

// printModelEstimate prints what a model proposal will try: the items, each with how it went on
// the line, then the tries, their cost and what is left of Jonah's budget.
func printModelEstimate(out *printer, e gen.ModelProposalEstimate) {
	out.printf("try %s for %s (now %s) on its %s, %d tries each:\n", e.Model, e.Agent, e.CurrentModel,
		plural(len(e.Items), "newest finished item"), e.Tries/max(len(e.Items), 1))

	var tb bytes.Buffer

	tw := tabwriter.NewWriter(&tb, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ITEM\tSTATION\tON THE LINE\tTITLE")

	for _, it := range e.Items {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", it.ItemRef, it.Station, it.LineResult, cut(oneLine(it.ItemTitle), 60))
	}

	_ = tw.Flush()

	out.printf("%s", trimLines(tb.String()))
	out.printf("%s, about %s; %s left of Jonah's budget\n", plural(e.Tries, "try"), usd(e.EstimateUsd), usd(e.BudgetLeftUsd))
}

// modelProposalProblem is goald's refusal of a model proposal in one plain sentence.
func modelProposalProblem(res *http.Response, p *gen.Problem) error {
	switch problemCode(p) {
	case codeBudgetExhausted:
		if est := p.ModelEstimate; est != nil {
			return fmt.Errorf("refused: the %s would cost about %s, more than the %s left of Jonah's budget; wait for the budget to renew",
				plural(est.Tries, "try"), usd(est.EstimateUsd), usd(est.BudgetLeftUsd))
		}
	case codeModelNotAllowed:
		return errors.New("refused: the model is not one the organization allows, or the agent runs on it already (see goal policy get)")
	case codeAgentNotTriable:
		return errors.New("refused: the agent needs MCP servers or secrets, so it can't be tried on past items")
	case codeNoFinishedItems:
		return errors.New("refused: the agent has no finished item to try the model on yet")
	}

	return problem(res, p)
}
