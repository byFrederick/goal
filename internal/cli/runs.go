package cli

import (
	"bytes"
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// runsTitleWidth is how much of an item's title the ITEM column shows, so the table stays readable.
const runsTitleWidth = 40

// runs is goal runs: the organization's agent runs across the factories the caller may read, newest
// first, with the window's summary; --json writes goald's answer as it came. Without --since and
// --until goald reads the current UTC day.
func runs(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("runs")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "only this factory's runs (name or id)")
	status := fs.String("status", "", "only runs with this status: running, succeeded, failed or cancelled")
	since := fs.String("since", "", "the earliest start, included (RFC 3339; default: the start of the UTC day)")
	until := fs.String("until", "", "the latest start, excluded (RFC 3339; default: the end of the UTC day)")
	search := fs.String("search", "", "only runs whose item title, agent or model contains this, or whose item it names (#147)")
	limit := fs.Int("limit", 0, "how many runs a page holds")
	cursor := fs.String("cursor", "", "the page after the one that printed this cursor")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 0 || *limit < 0 {
		return errUsage
	}

	params := &gen.ListRunsParams{Factory: optional(*factory), Q: optional(*search), Cursor: optional(*cursor)}

	if *status != "" {
		s := gen.RunStatus(*status)
		if !s.Valid() {
			return errUsage
		}

		params.Status = &s
	}

	if !optionalTime(*since, &params.Since) || !optionalTime(*until, &params.Until) {
		return errUsage
	}

	if *limit > 0 {
		params.Limit = limit
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.ListRunsWithResponse(ctx, c.org, params)
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

	printRuns(out, res.JSON200)

	return nil
}

// printRuns prints one row per run in the order goald sent them, then the summary and, when there
// is another page, the command that reads it. An agent is named by its role, never by a copy.
func printRuns(out *printer, l *gen.OrgRunList) {
	if len(l.Runs) == 0 {
		out.printf("No runs\n")
	} else {
		var b bytes.Buffer

		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "FACTORY\tITEM\tSTATION\tAGENT\tMODEL\tSTARTED\tDURATION\tTURNS\tCOST\tSTATUS")

		for _, r := range l.Runs {
			_, _ = fmt.Fprintf(tw, "%s\t%s %s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
				r.Factory.Name, r.Item.Ref, cut(oneLine(r.Item.Title), runsTitleWidth), r.Station, r.Agent, r.Model,
				stamp(r.StartedAt), runDuration(r), r.Turns, usd(r.CostUsd), r.Status)
		}

		_ = tw.Flush()

		out.printf("%s", b.String())
	}

	s := l.Summary
	out.printf("%d runs: %d running, %d finished, %d failed, %s spent\n", s.Total, s.Running, s.Finished, s.Failed, usd(s.SpendUsd))

	if l.NextCursor != nil && *l.NextCursor != "" {
		out.printf("next: goal runs --cursor %s\n", *l.NextCursor)
	}
}

// runDuration is how long a finished run took, to the second, or running while it runs.
func runDuration(r gen.OrgRun) string {
	if r.EndedAt == nil {
		return "running"
	}

	return r.EndedAt.Sub(r.StartedAt).Round(time.Second).String()
}

// optionalTime sets *dst to an RFC 3339 flag's time, leaving it nil when the flag was left empty;
// false when the flag is not RFC 3339.
func optionalTime(s string, dst **time.Time) bool {
	if s == "" {
		return true
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return false
	}

	*dst = &t

	return true
}
