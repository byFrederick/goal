package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

const verbStart = "start"

// refsFlag is a repeatable flag of item refs, as --after a --after b.
type refsFlag []string

func (r *refsFlag) String() string { return strings.Join(*r, ",") }

func (r *refsFlag) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("an item ref is empty")
	}

	*r = append(*r, strings.TrimSpace(v))

	return nil
}

// startFlags are --at and --in, the start's time or wait.
type startFlags struct{ at, in *string }

func addStartFlags(fs interface {
	String(name, value, usage string) *string
}) startFlags {
	return startFlags{
		at: fs.String("at", "", "start at this time: RFC 3339, or a date and time in your time zone (2026-11-05T09:00)"),
		in: fs.String("in", "", "start after this wait: 36h, 7d or 2w"),
	}
}

// startOf is the start the flags and after refs name, and whether they name one. --at and --in
// together are a usage error.
func (f startFlags) startOf(after []string) (gen.Start, bool, error) {
	if *f.at != "" && *f.in != "" {
		return gen.Start{}, false, errUsage
	}

	s := gen.Start{At: optional(*f.at), In: optional(*f.in)}
	if len(after) > 0 {
		s.After = &after
	}

	return s, s.At != nil || s.In != nil || s.After != nil, nil
}

// startLine says when an item may start, for the line printed after submitting or changing it:
// "" when it may start now and has no after items.
func startLine(it *gen.Item, after []string) string {
	var parts []string

	if it.Start != nil {
		parts = append(parts, "at "+it.Start.At.UTC().Format(time.RFC3339))
	}

	if len(after) > 0 {
		parts = append(parts, "after "+strings.Join(after, ", "))
	}

	if len(parts) == 0 {
		return ""
	}

	return fmt.Sprintf("starts %s (goal start %s --now starts it now)\n", strings.Join(parts, " and "), it.Ref)
}

// startItem changes or clears when an item that has not started may start: goal start <item>
// --at <time> | --in <wait> | --now [--factory F]. goald refuses an item that has started.
func startItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags(verbStart)
	sf := addStartFlags(fs)
	now := fs.Bool("now", false, "start it now: clear its start time")
	factory := fs.String("factory", "", "the factory of a bare item number")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	start, set, err := sf.startOf(nil)
	if err != nil || set == *now {
		return errUsage // exactly one of --at, --in and --now
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	ref, err := itemRef(ctx, c, pos[0], *factory)
	if err != nil {
		return err
	}

	if *now {
		res, err := c.ClearItemStartWithResponse(ctx, c.org, ref)
		if err != nil {
			return apiDown(err)
		}

		it, err := answer(res.JSON200, res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
		if err != nil {
			return err
		}

		out.printf("✓ %s starts now: the first station releases it within seconds, once its needs are done\n", it.Ref)

		return nil
	}

	res, err := c.SetItemStartWithResponse(ctx, c.org, ref, start)
	if err != nil {
		return apiDown(err)
	}

	it, err := answer(res.JSON200, res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	if err != nil {
		return err
	}

	out.printf("✓ %s %s", it.Ref, startLine(it, nil))

	return nil
}

// startIf is &s when set, nil otherwise.
func startIf(s gen.Start, set bool) *gen.Start {
	if !set {
		return nil
	}

	return &s
}
