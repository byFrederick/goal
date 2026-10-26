package cli

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
)

// maxStatusDays is the longest window goal status reads, as goald's ranges.
const maxStatusDays = 90

// statusWindow is --window: the last N days, N from 1 to maxStatusDays.
var statusWindow = regexp.MustCompile(`^([0-9]{1,2})d$`)

// status prints how each factory's line did over the last days: goal status [--factory F]
// [--window 7d]. Per factory its work in progress, the items done, lead time p85, human touches
// per finished item, the touchless share and the station with the most unplanned touches (spec 4
// §4.9, §9); without --factory, every factory the caller may read and the organization's total.
func status(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("status")
	factory := fs.String("factory", "", "only this factory (name or id)")
	window := fs.String("window", "7d", "the last N days, 1d to 90d")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	days, ok := windowDays(*window)

	if err != nil || len(pos) != 0 || !ok {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	// goald's days are UTC; the window ends today.
	from := openapi_types.Date{Time: time.Now().UTC().AddDate(0, 0, 1-days)}

	return printStatus(ctx, out, c, *factory, from, days)
}

// windowDays is the number of days of a --window such as 7d.
func windowDays(window string) (int, bool) {
	m := statusWindow.FindStringSubmatch(window)
	if m == nil {
		return 0, false
	}

	n, _ := strconv.Atoi(m[1])

	return n, n >= 1 && n <= maxStatusDays
}

// statusRow is one factory's line in goal status.
type statusRow struct {
	name    string
	ref     string
	flow    gen.Metrics
	touches gen.TouchStats
}

func printStatus(ctx context.Context, out *printer, c orgClient, factory string, from openapi_types.Date, days int) error {
	rows, total, err := statusTouches(ctx, c, factory, from)
	if err != nil {
		return err
	}

	for i := range rows {
		res, err := c.GetMetricsWithResponse(ctx, c.org, rows[i].ref, &gen.GetMetricsParams{From: &from})
		if err != nil {
			return apiDown(err)
		}

		if res.JSON200 == nil {
			return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		rows[i].flow = *res.JSON200
	}

	out.printf("%s, the last %d days (from %s, UTC)\n", c.org, days, from.Format(time.DateOnly))

	if len(rows) == 0 {
		out.printf("no factories\n")

		return nil
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "FACTORY\tWIP\tDONE\tLEAD TIME P85\tTOUCHES/ITEM\tTOUCHLESS\tMOST UNPLANNED")

	wip, done := 0, 0

	for _, r := range rows {
		wip, done = wip+r.flow.Wip, done+r.flow.ItemsDone
		lead := "-"

		if r.flow.LeadTimeHours != nil {
			lead = fmt.Sprintf("%.1f h", r.flow.LeadTimeHours.P85)
		}

		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n", r.name, r.flow.Wip, r.flow.ItemsDone, lead, touchColumns(r.touches))
	}

	if total != nil && len(rows) > 1 {
		_, _ = fmt.Fprintf(tw, "all\t%d\t%d\t-\t%s\n", wip, done, touchColumns(*total))
	}

	_ = tw.Flush()

	out.printf("%s", b.String())

	return nil
}

// statusTouches reads the touch metrics: one factory's, or every factory's the caller may read
// with the organization's total.
func statusTouches(ctx context.Context, c orgClient, factory string, from openapi_types.Date) ([]statusRow, *gen.TouchStats, error) {
	if factory != "" {
		weeks := 1

		res, err := c.GetTouchMetricsWithResponse(ctx, c.org, factory, &gen.GetTouchMetricsParams{From: &from, Weeks: &weeks})
		if err != nil {
			return nil, nil, apiDown(err)
		}

		if res.JSON200 == nil {
			return nil, nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		return []statusRow{{name: factory, ref: factory, touches: res.JSON200.Stats}}, nil, nil
	}

	res, err := c.GetOrgTouchMetricsWithResponse(ctx, c.org, &gen.GetOrgTouchMetricsParams{From: &from})
	if err != nil {
		return nil, nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	rows := make([]statusRow, 0, len(res.JSON200.Factories))
	for _, f := range res.JSON200.Factories {
		rows = append(rows, statusRow{name: f.Name, ref: f.FactoryId, touches: f.Stats})
	}

	return rows, &res.JSON200.Total, nil
}

// touchColumns are the touch columns of a row: per finished item, touchless share, and the station
// with the most unplanned touches with their count ("-" where nothing finished or none was).
func touchColumns(s gen.TouchStats) string {
	if s.Finished == 0 {
		return "-\t-\t-"
	}

	most, top := "-", 0

	for _, st := range s.ByStation {
		if st.Unplanned > top && st.Key != "" {
			most, top = fmt.Sprintf("%s (%d)", st.Key, st.Unplanned), st.Unplanned
		}
	}

	return fmt.Sprintf("%.1f\t%.0f%%\t%s", s.PerItemMean, s.TouchlessRate*100, most)
}

// printTouches writes an item's human touches for the person reading goal item, beside the item's
// JSON on stdout: on stderr, so a script reading the JSON gets the JSON alone.
func printTouches(out *printer, t *gen.ItemTouches) {
	if t == nil || t.Count == 0 {
		out.noticef("no human touches\n")

		return
	}

	noun := "human touches"
	if t.Count == 1 {
		noun = "human touch"
	}

	out.noticef("%d %s (%d planned, %d unplanned):\n", t.Count, noun, t.Planned, t.Unplanned)

	for _, x := range t.List {
		station, planned, reason := "no station", "unplanned", "no reason"

		if x.Station != nil {
			station = "at " + *x.Station
		}

		if x.Planned {
			planned = "planned"
		}

		if x.Reason != nil {
			reason = "reason: " + strings.ReplaceAll(string(*x.Reason), "_", " ")
		}

		out.noticef("  %s  %s  %s %s, %s, %s\n", stamp(x.At), x.Actor.Name, x.Kind, station, planned, reason)
	}
}
