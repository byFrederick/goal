package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/byFrederick/goal/internal/api/gen"
)

// judgeCommand is goal judge: Jonah's judges of a factory (ADR 0025), listed with their last 30
// days, and tried on the newest finished runs.
func judgeCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{subList: judgeList, "try": judgeTry}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// judgeList is goal judge list [--factory F] [--json]: one row per judge of the factory's current
// version, then what its judge runs cost over the last 30 days.
func judgeList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("judge list")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")
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

	res, err := c.ListJudgesWithResponse(ctx, c.org, f)
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

	printJudges(out, f, res.JSON200)

	return nil
}

func printJudges(out *printer, factory string, l *gen.JudgeList) {
	if len(l.Judges) == 0 {
		out.printf("%s has no judges; a judge is a file evals/judges/<slug>.md of its definition\n", factory)

		return
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SLUG\tTITLE\tENABLED\tSAMPLE\tPASS MARK\tPASS RATE 30D\tSCORED\tCOST")

	for _, j := range l.Judges {
		rate := "-"
		if j.Summary.Scored > 0 {
			rate = percent(j.Summary.PassRate)
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n", j.Slug, oneLine(j.Title), yesNo(j.Enabled), percent(j.Sample),
			percent(j.PassMark), rate, j.Summary.Scored, dollars(j.Summary.CostUsd))
	}

	_ = tw.Flush()

	out.printf("%s%s's judge runs cost %s over the last 30 days\n", b.String(), factory, dollars(l.JudgeCostUsd))
}

// judgeTry is goal judge try <slug> [--factory F] [--json]: queue the judge on the newest
// finished runs of its agents.
func judgeTry(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("judge try")
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

	res, err := c.TryJudgeWithResponse(ctx, c.org, f, pos[0])
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

	tries := res.JSON202.Tries
	if len(tries) == 0 {
		out.printf("no finished runs for %s to try\n", pos[0])

		return nil
	}

	refs := make([]string, 0, len(tries))
	for _, t := range tries {
		refs = append(refs, t.ItemRef)
	}

	out.printf("queued %d tries of %s: %s\n", len(tries), pos[0], strings.Join(refs, ", "))

	return nil
}

// percent is a share from 0 to 1 as a whole percentage.
func percent(share float64) string { return fmt.Sprintf("%.0f%%", share*100) }

// dollars is an amount of US dollars to the cent.
func dollars(usd float64) string { return fmt.Sprintf("$%.2f", usd) }

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
