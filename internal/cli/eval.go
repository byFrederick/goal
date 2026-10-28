package cli

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/byFrederick/goal/internal/api/gen"
)

// evalCommand is goal eval: Jonah's eval cases of a factory (ADR 0025), run on the current
// version or the draft, and an eval run's results.
func evalCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{"run": evalRun, "show": evalShow}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// evalRun is goal eval run [--case C] [--draft] [--factory F] [--json]: start an eval run of every
// case, or of one, and print what it is about to cost.
func evalRun(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("eval run")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")
	name := fs.String("case", "", "run only this case")
	draft := fs.Bool("draft", false, "run the open draft's cases instead of the current version's")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

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

	on := gen.EvalRunRequestOnCurrent
	if *draft {
		on = gen.EvalRunRequestOnDraft
	}

	res, err := c.StartEvalRunWithResponse(ctx, c.org, f, gen.StartEvalRunJSONRequestBody{Case: optional(*name), On: &on})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON202 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	run, est := res.JSON202.Run, res.JSON202.Estimate
	cases := evalRunCases(run)

	noun := "cases"
	if len(cases) == 1 {
		noun = "case"
	}

	out.printf("started eval run %s of %d %s, about %s\n", run.Id, len(cases), noun, usd(est.Usd))

	if len(est.Unpriced) > 0 {
		out.printf("not priced, with no tries and no runs of their agent yet: %s\n", strings.Join(est.Unpriced, ", "))
	}

	out.printf("follow it with: goal eval show %s --factory %s\n", run.Id, f)

	return nil
}

// evalShow is goal eval show <run> [--factory F] [--json]: an eval run's state and cost, and each
// case's tries.
func evalShow(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("eval show")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
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

	res, err := c.GetEvalRunWithResponse(ctx, c.org, f, pos[0])
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

	printEvalRun(out, res.JSON200)

	return nil
}

// printEvalRun prints the run's state, what it ran on and cost, then one line per case with its
// tries in order and whether the case passed.
func printEvalRun(out *printer, r *gen.EvalRun) {
	on := "the draft"
	if r.Version != nil {
		on = fmt.Sprintf("v%d", *r.Version)
	}

	out.printf("eval run %s: %s, on %s (%s), %s of about %s\n", r.Id, r.State, on, r.Trigger, usd(r.CostUsd), usd(r.EstimateUsd))

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "CASE\tTRIES\tRESULT")

	for _, name := range evalRunCases(*r) {
		var (
			tries  []string
			passed = true
			done   = true
		)

		for _, res := range r.Results {
			if res.Case != name {
				continue
			}

			tries = append(tries, string(res.State))
			passed = passed && res.State == gen.EvalResultStatePass
			done = done && res.State != gen.EvalResultStatePending && res.State != gen.EvalResultStateRunning
		}

		result := "fails"

		switch {
		case passed:
			result = "passes"
		case !done:
			result = "-"
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", name, strings.Join(tries, " "), result)
	}

	_ = tw.Flush()

	out.printf("%s", b.String())
}

// evalRunCases are the run's cases, each once, in the order of its results.
func evalRunCases(r gen.EvalRun) []string {
	var out []string

	for _, res := range r.Results {
		if !slices.Contains(out, res.Case) {
			out = append(out, res.Case)
		}
	}

	return out
}

// commandFactory is the factory an eval or judge command acts on: --factory, else the
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
