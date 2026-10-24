package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// Work commands talk to goald's HTTP API (GOAL_API_URL, default http://127.0.0.1:8080).
const (
	defaultAPI = "http://127.0.0.1:8080"
	pollEvery  = 2 * time.Second
	eventPage  = 500
)

// stops are the item states at which following an item ends: someone else has to act.
var stops = map[gen.ItemState]bool{
	gen.ItemState("waiting_human"): true, gen.ItemState("done"): true,
	gen.ItemState("blocked"): true, gen.ItemState("cancelled"): true,
}

// newAPI opens goald's API: GOAL_API_URL, else api_url in the config file, else the local goald.
func newAPI(getenv func(string) string) (*gen.ClientWithResponses, error) {
	base := strings.TrimSpace(getenv("GOAL_API_URL"))
	if base == "" {
		cfg, err := loadConfig(getenv)
		if err != nil {
			return nil, err
		}

		base = cmp.Or(cfg.APIURL, defaultAPI)
	}

	return gen.NewClientWithResponses(base, gen.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}))
}

// parseInterspersed parses flags wherever they appear among the positional arguments, so
// `goal run goal "brief" --type fix` works, and returns the positional arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string

	for {
		if err := fs.Parse(args); err != nil {
			return nil, errUsage
		}

		if fs.NArg() == 0 {
			return pos, nil
		}

		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	return fs
}

// runItem submits an item and follows it: goal run <factory> "<brief>" [--type T] [--body B].
// The brief's first line is the title; the rest, and --body, is the body.
func runItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("run")
	typ := fs.String("type", "feature", "work type")
	body := fs.String("body", "", "item body")
	priority := fs.Int("priority", 0, "priority (higher first)")
	detach := fs.Bool("detach", false, "submit without following the item")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 || strings.TrimSpace(pos[1]) == "" {
		return errUsage
	}

	title, rest, _ := strings.Cut(strings.TrimSpace(pos[1]), "\n")
	text := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(rest), *body}, "\n\n"))

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	res, err := c.SubmitItemWithResponse(ctx, c.org, pos[0], gen.NewItem{
		Title: strings.TrimSpace(title), Type: *typ, Body: &text, Priority: priority,
	})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	it := res.JSON201
	out.printf("submitted %s %q (%s) at %s\n", it.Ref, it.Title, it.Id, it.Station)

	if *detach {
		return nil
	}

	return follow(ctx, out, c, it.Id, 0, pollEvery)
}

// followItem follows an existing item: goal follow <item> [--factory F].
func followItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("follow")
	factory := fs.String("factory", "", "the factory of a bare item number")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	ref, err := itemRef(ctx, c, pos[0], *factory)
	if err != nil {
		return err
	}

	return follow(ctx, out, c, ref, 0, pollEvery)
}

// itemRef turns the item a user names into what the API takes. An id or <factory>#<number>
// passes as is; a bare number is that item of --factory, or of the one factory that has an
// item with that number.
func itemRef(ctx context.Context, c orgClient, arg, factory string) (string, error) {
	n, bare := bareNumber(arg)
	if !bare {
		return arg, nil
	}

	if factory != "" {
		return domain.ItemRef{Factory: factory, Number: n}.String(), nil
	}

	res, err := c.ListFactoriesWithResponse(ctx, c.org)
	if err != nil {
		return "", apiDown(err)
	}

	if res.JSON200 == nil {
		return "", problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	var found []string

	for _, f := range res.JSON200.Factories {
		ref := domain.ItemRef{Factory: f.Name, Number: n}.String()

		it, err := c.GetItemWithResponse(ctx, c.org, ref)
		if err != nil {
			return "", apiDown(err)
		}

		if it.JSON200 != nil {
			found = append(found, ref)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("no factory has an item #%d", n)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("items %s all have number %d; name one, or pass --factory", strings.Join(found, ", "), n)
	}
}

// follow prints the item's events as they happen until it reaches a state where someone else
// has to act: a human station, done, blocked or cancelled. A blocked item is an error.
func follow(ctx context.Context, out *printer, c orgClient, id string, after int64, every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()

	limit := eventPage

	for {
		item, err := c.GetItemWithResponse(ctx, c.org, id)
		if err != nil {
			return following(ctx, apiDown(err))
		}

		if item.JSON200 == nil {
			return problem(item.HTTPResponse, item.ApplicationproblemJSONDefault)
		}

		for full := true; full; {
			page, err := c.ListItemEventsWithResponse(ctx, c.org, id, &gen.ListItemEventsParams{After: &after, Limit: &limit})
			if err != nil {
				return following(ctx, apiDown(err))
			}

			if page.JSON200 == nil {
				return problem(page.HTTPResponse, page.ApplicationproblemJSONDefault)
			}

			for _, e := range page.JSON200.Events {
				if line := describe(e); line != "" {
					out.printf("%s  %-14s %s\n", e.At.Local().Format(time.TimeOnly), deref(e.Station), line)
				}
			}

			after, full = page.JSON200.NextAfter, len(page.JSON200.Events) == limit
		}

		if it := item.JSON200; stops[it.State] {
			return stopped(out, it)
		}

		select {
		case <-ctx.Done():
			return following(ctx, nil)
		case <-t.C:
		}
	}
}

// following ends a follow cut short by Ctrl-C: the item keeps moving without us.
func following(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.New("stopped following; the item keeps moving in goald")
	}

	return err
}

func stopped(out *printer, it *gen.Item) error {
	out.printf("\n%s is %s at %s (cost $%.2f)\n", it.Ref, it.State, it.Station, it.CostUsd)

	if it.State == "waiting_human" {
		out.printf("  goal item %s           # read it, with the outputs so far\n", it.Ref)
		out.printf("  goal approve %s        # or goal merge %s at a merge station\n", it.Ref, it.Ref)
		out.printf("  goal reject %s --reason \"...\"\n", it.Ref)
	}

	if it.State == "blocked" {
		return fmt.Errorf("%s is blocked at %s; the blocked event says why (goal retry %s sends it back to the queue)",
			it.Ref, it.Station, it.Ref)
	}

	return nil
}

// describe renders one event as a short line, or "" for noise (token usage, successful tool
// results).
func describe(e gen.Event) string {
	if f, ok := describers[e.Kind]; ok {
		return f(e)
	}

	b, _ := json.Marshal(e.Data)

	return e.Kind + " " + cut(string(b), 120)
}

// describers render the event kinds worth a short line of their own.
var describers = map[string]func(gen.Event) string{
	"created":       func(gen.Event) string { return "created" },
	"moved":         func(e gen.Event) string { return fmt.Sprintf("moved %s → %s", str(e, "from"), str(e, "to")) },
	"claimed":       func(e gen.Event) string { return "claimed for " + str(e, "agent") },
	"run_started":   func(e gen.Event) string { return "run started (" + e.Actor + ")" },
	"run_finished":  runFinished,
	"agent_started": func(e gen.Event) string { return fmt.Sprintf("agent %s on %s", e.Actor, str(e, "model")) },
	"tool_call": func(e gen.Event) string {
		return fmt.Sprintf("→ %s %s", str(e, "tool"), cut(oneLine(str(e, "input")), 100))
	},
	"tool_result": func(e gen.Event) string {
		if failed, _ := e.Data["is_error"].(bool); failed {
			return "✗ " + cut(oneLine(str(e, "summary")), 160)
		}

		return ""
	},
	"agent_message": func(e gen.Event) string { return "“" + cut(oneLine(str(e, "text")), 160) + "”" },
	"usage":         func(gen.Event) string { return "" },
	"gateway_tool_call": func(e gen.Event) string {
		return fmt.Sprintf("gateway %s__%s %s", str(e, "connection"), str(e, "tool"), str(e, "outcome"))
	},
	"check_passed": func(e gen.Event) string { return fmt.Sprintf("check `%s` passed", str(e, "command")) },
	"check_failed": func(e gen.Event) string {
		return fmt.Sprintf("check `%s` failed (exit %v)", str(e, "command"), e.Data["exit_code"])
	},
	"approved":        func(e gen.Event) string { return "approved by " + e.Actor },
	"note":            func(e gen.Event) string { return "note from " + e.Actor + ": " + cut(oneLine(str(e, "text")), 300) },
	"contract_failed": withReason,
	"blocked":         withReason,
	"rejected":        withReason,
	"gate_approved":   withReason,
	"gate_rejected":   withReason,
	"waiting_human":   withReason,
	"released":        withReason,
	"lease_expired":   withReason,
	"done":            withReason,
}

func runFinished(e gen.Event) string {
	line := fmt.Sprintf("run %s ($%.2f, %v turns)", str(e, "status"), e.Data["cost_usd"], e.Data["turns"])
	if s := str(e, "error"); s != "" {
		line += ": " + cut(oneLine(s), 200)
	}

	return line
}

func withReason(e gen.Event) string {
	if s := str(e, "reason") + str(e, "error"); s != "" {
		return e.Kind + ": " + cut(oneLine(s), 300)
	}

	return e.Kind
}

func str(e gen.Event, key string) string {
	s, _ := e.Data[key].(string)

	return s
}

// showItem prints an item: goal item <item> [--factory F].
func showItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("item")
	factory := fs.String("factory", "", "the factory of a bare item number")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	ref, err := itemRef(ctx, c, pos[0], *factory)
	if err != nil {
		return err
	}

	res, err := c.GetItemWithResponse(ctx, c.org, ref)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	b, err := json.MarshalIndent(res.JSON200, "", "  ")
	if err != nil {
		return err
	}

	out.printf("%s\n", b)

	return nil
}

// bareNumber reports whether arg is an item number on its own, such as 2.
func bareNumber(arg string) (int64, bool) {
	n, err := strconv.ParseInt(arg, 10, 64)

	return n, err == nil && n > 0
}

// The decisions a human makes on an item.
const (
	verbApprove = "approve"
	verbReject  = "reject"
	verbRetry   = "retry"
	verbMerge   = "merge"
	// verbNote leaves a note for the agents; not a decision.
	verbNote = "note"
)

// decide acts on an item: goal approve <item>; goal reject <item> --reason "..."; goal retry
// <item>; goal merge <item>. Each takes --factory for a bare item number and --org. The decider
// is the request's actor, the local user (phase 2.2 has no sign-in).
func decide(ctx context.Context, out *printer, getenv func(string) string, verb string, args []string) error {
	fs := newFlags(verb)
	reason := fs.String("reason", "", "what must change (reject)")
	factory := fs.String("factory", "", "the factory of a bare item number")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || (verb == verbReject) != (*reason != "") {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	ref, err := itemRef(ctx, c, pos[0], *factory)
	if err != nil {
		return err
	}

	it, err := act(ctx, c, verb, ref, *reason)
	if err != nil {
		return err
	}

	out.printf("✓ %s %s; now %s at %s\n", done[verb], it.Ref, it.State, it.Station)

	return nil
}

// noteItem leaves the local user's note for the agents working an item: goal note <item>
// "<text>" [--factory F]. goald records it; the item's next run reads it in its brief.
func noteItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags(verbNote)
	factory := fs.String("factory", "", "the factory of a bare item number")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 || strings.TrimSpace(pos[1]) == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	ref, err := itemRef(ctx, c, pos[0], *factory)
	if err != nil {
		return err
	}

	res, err := c.NoteItemWithResponse(ctx, c.org, ref, gen.NewNote{Text: pos[1]})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON202 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ note left on %s; the next run reads it\n", ref)

	return nil
}

// done is what each decision did, for the line that confirms it.
var done = map[string]string{verbApprove: "approved", verbReject: "rejected", verbRetry: "retried", verbMerge: "merged"}

// act sends one decision and returns the item after it, or why it was refused.
func act(ctx context.Context, c orgClient, verb, ref, reason string) (*gen.Item, error) {
	switch verb {
	case verbApprove:
		res, err := c.ApproveItemWithResponse(ctx, c.org, ref)
		if err != nil {
			return nil, apiDown(err)
		}

		return answer(res.JSON200, res.HTTPResponse, res.ApplicationproblemJSONDefault)
	case verbReject:
		res, err := c.RejectItemWithResponse(ctx, c.org, ref, gen.Rejection{Reason: reason})
		if err != nil {
			return nil, apiDown(err)
		}

		return answer(res.JSON200, res.HTTPResponse, res.ApplicationproblemJSONDefault)
	case verbRetry:
		res, err := c.RetryItemWithResponse(ctx, c.org, ref)
		if err != nil {
			return nil, apiDown(err)
		}

		return answer(res.JSON200, res.HTTPResponse, res.ApplicationproblemJSONDefault)
	default:
		res, err := c.MergeItemWithResponse(ctx, c.org, ref, gen.MergeRequest{})
		if err != nil {
			return nil, apiDown(err)
		}

		return answer(res.JSON200, res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}
}

// answer is the item goald answered with, or the problem it answered instead.
func answer(it *gen.Item, res *http.Response, p *gen.Problem) (*gen.Item, error) {
	if it == nil {
		return nil, problem(res, p)
	}

	return it, nil
}

func problem(res *http.Response, p *gen.Problem) error {
	if p != nil {
		return fmt.Errorf("%s: %s", p.Title, deref(p.Detail))
	}

	return fmt.Errorf("goald answered %s", res.Status)
}

func apiDown(err error) error {
	return fmt.Errorf("%w (is goald running? start it with `goal up`)", err)
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return strings.ToValidUTF8(s[:n], "") + "…"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// deref is what p points to, or the zero value when it is nil.
func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}
