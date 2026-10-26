package cli

import (
	"context"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The andon subcommands.
const (
	andonPull  = "pull"
	andonClear = "clear"
)

// andon reads or works a factory's andon cord: goal andon <factory> shows whether the line runs,
// its gate tallies and recent pulls; goal andon pull <factory> --reason "…" stops the line;
// goal andon clear <factory> [--note "…"] restarts it. The local user pulls and clears (phase 2.2
// has no sign-in). A factory named pull or clear is shown with its name alone.
func andon(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	verb := ""
	if len(args) > 1 && (args[0] == andonPull || args[0] == andonClear) {
		verb, args = args[0], args[1:]
	}

	fs := newFlags("andon")
	reason := fs.String("reason", "", "why the line must stop (pull)")
	note := fs.String("note", "", "what was done about it (clear)")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)

	switch {
	case err != nil, len(pos) != 1:
		return errUsage
	case verb == andonPull && strings.TrimSpace(*reason) == "", verb != andonPull && *reason != "", verb != andonClear && *note != "":
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	switch verb {
	case andonPull:
		return pullCord(ctx, out, c, pos[0], *reason)
	case andonClear:
		return clearCord(ctx, out, c, pos[0], *note)
	default:
		return showCord(ctx, out, c, pos[0])
	}
}

func pullCord(ctx context.Context, out *printer, c orgClient, factory, reason string) error {
	res, err := c.PullAndonWithResponse(ctx, c.org, factory, gen.NewAndonPull{Reason: reason})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	}

	out.printf("✓ andon pulled on %s: the line releases and claims nothing until it is cleared\n", factory)

	return nil
}

func clearCord(ctx context.Context, out *printer, c orgClient, factory, note string) error {
	res, err := c.ClearAndonWithResponse(ctx, c.org, factory, gen.AndonClear{Note: optionalText(note)})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	}

	out.printf("✓ andon cleared on %s: the line runs again\n", factory)

	return nil
}

func showCord(ctx context.Context, out *printer, c orgClient, factory string) error {
	res, err := c.GetAndonWithResponse(ctx, c.org, factory, &gen.GetAndonParams{})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	printAndon(out, factory, *res.JSON200)

	return nil
}

func printAndon(out *printer, factory string, s gen.AndonState) {
	if p := s.Open; p != nil {
		out.printf("%s: line stopped since %s by %s: %s\n", factory, stamp(p.PulledAt), p.PulledBy, oneLine(p.Reason))
	} else {
		out.printf("%s: line running\n", factory)
	}

	for _, g := range s.Gates {
		line := "  gate " + g.Station + ": "
		if g.Verdicts == 0 {
			out.printf("%sno verdicts since the last clear\n", line)
			continue
		}

		out.printf("%s%d of the last %d verdicts sent back", line, g.Rejected, g.Verdicts)

		if g.TopReason != nil {
			out.printf(" (most often: %s)", cut(oneLine(*g.TopReason), 80))
		}

		if g.Warning {
			out.printf("; one more reaches the rework rate")
		}

		out.printf("\n")
	}

	if len(s.Recent) > 0 {
		out.printf("recent pulls:\n")
	}

	for _, p := range s.Recent {
		out.printf("  %s by %s: %s", stamp(p.PulledAt), p.PulledBy, cut(oneLine(p.Reason), 80))

		if p.ClearedAt != nil {
			out.printf("; cleared %s by %s", stamp(*p.ClearedAt), deref(p.ClearedBy))

			if n := deref(p.Note); n != "" {
				out.printf(": %s", cut(oneLine(n), 80))
			}
		}

		out.printf("\n")
	}
}

// stamp writes a time in UTC to the minute.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func optionalText(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}

	return &s
}
