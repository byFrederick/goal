package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
)

// spendWindows are the windows goal spend takes, by flag value, with the heading it prints.
var spendWindows = map[string]string{"week": "This week", "month": "This month", "90d": "Last 90 days"}

// spend is goal spend [--window week|month|90d]: what the organization's agent runs cost over
// the window, the cost per merged item, rework, and each factory's budget with its state; --json
// writes goald's answer as it came.
func spend(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("spend")
	orgFlag := addOrgFlag(fs)
	window := fs.String("window", "week", "week, month or 90d")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 0 {
		return errUsage
	}

	if _, ok := spendWindows[*window]; !ok {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	w := gen.SpendWindow(*window)

	res, err := c.GetOrgSpendWithResponse(ctx, c.org, &gen.GetOrgSpendParams{Window: &w})
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

	printSpend(out, res.JSON200)

	return nil
}

// printSpend prints the window's totals, the cost per merged item and rework (when the answer
// has them), then one line per factory's budget.
func printSpend(out *printer, s *gen.OrgSpend) {
	out.printf("%s (%s to %s): %s\n", spendWindows[string(s.Window)], s.Since.UTC().Format("2006-01-02"), s.Until.UTC().Format("2006-01-02"), usd(s.TotalUsd))

	if s.ThisMonthUsd != nil && s.LastMonthUsd != nil {
		out.printf("  This month %s, last month %s\n", usd(*s.ThisMonthUsd), usd(*s.LastMonthUsd))
	}

	if s.CostPerMergedUsd != nil && s.Merged != nil {
		out.printf("  Per merged item %s (%d merged)\n", usd(*s.CostPerMergedUsd), *s.Merged)
	}

	if s.ReworkUsd != nil && s.ReworkShare != nil {
		out.printf("  Rework %s (%.0f%%)\n", usd(*s.ReworkUsd), *s.ReworkShare*100)
	}

	if !s.Complete {
		out.printf("  Only the factories you see are counted\n")
	}

	if len(s.Budgets) == 0 {
		return
	}

	out.printf("\nBudgets\n")

	for _, b := range s.Budgets {
		week := usd(b.WeekUsd) + " in 7 days"
		if b.WeeklyUsd != nil {
			week += " of " + usd(*b.WeeklyUsd)
		}

		parts := []string{b.Factory.Name, string(b.State), "today " + usd(b.TodayUsd), week}

		if b.Reason != nil {
			parts = append(parts, oneLine(*b.Reason))
		}

		var models []string
		for _, m := range b.ModelConnections {
			models = append(models, fmt.Sprintf("%s (%s)", m.Name, m.Kind))
		}

		if len(models) > 0 {
			parts = append(parts, "models: "+strings.Join(models, ", "))
		}

		out.printf("  %s\n", strings.Join(parts, "  "))
	}
}
