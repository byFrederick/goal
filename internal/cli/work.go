package cli

import (
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
	"github.com/byFrederick/goal/internal/core/domain"
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
// Requests carry the credential of the API's origin (credentials.go), from the user's keychain
// when goal login stored it there.
func newAPI(getenv func(string) string) (*gen.ClientWithResponses, error) {
	return newAPIWith(getenv, osKeychain{})
}

// newAPIWith is newAPI with the keychain to read a stored credential from.
func newAPIWith(getenv func(string) string, kc keychain) (*gen.ClientWithResponses, error) {
	c, _, err := openAPI(getenv, kc)

	return c, err
}

// openAPI is newAPIWith, also reporting whether its credential acts in its own organization only:
// an API key or a personal token, from GOAL_API_KEY or `goal login --token`.
func openAPI(getenv func(string) string, kc keychain) (*gen.ClientWithResponses, bool, error) {
	tg, err := apiTarget(getenv)
	if err != nil {
		return nil, false, err
	}

	cred, err := resolveCredential(getenv, tg.cfg, tg.origin, kc)
	if err != nil {
		return nil, false, err
	}

	c, err := gen.NewClientWithResponses(tg.base, gen.WithHTTPClient(httpClient(tg.origin, cred)))

	return c, cred != nil && cred.cred.Kind == kindToken, err
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
	typ := fs.String("type", "", "work type (default: the factory's first)")
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
		Title: strings.TrimSpace(title), Type: optional(*typ), Body: &text, Priority: priority,
	})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
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

	res, err := c.ListFactoriesWithResponse(ctx, c.org, nil)
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
	out.printf("\n%s is %s at %s (cost %s)\n", it.Ref, it.State, it.Station, usd(it.CostUsd))

	if a := it.ToolApproval; it.State == "waiting_human" && a != nil {
		out.printf("  %s\n", toolAsk(a.Agent, a.Connection, a.Tool, a.Input))
		out.printf("  goal item %s           # read it, with the outputs so far\n", it.Ref)
		out.printf("  goal approve %s        # let the agent make this one call\n", it.Ref)

		if a.AlwaysAllowOffered {
			out.printf("  goal approve %s --always-allow   # and let %s call it without asking from now on\n", it.Ref, a.Agent)
		}

		out.printf("  goal deny %s --note \"...\"\n", it.Ref)
	} else if it.State == "waiting_human" {
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

// toolAsk is the line of an agent's call that waits for a person: its role, the connection, the
// tool and the input cut to one short line.
func toolAsk(agent, connection, tool string, input any) string {
	in, ok := input.(string)
	if !ok {
		b, _ := json.Marshal(input)
		in = string(b)
	}

	return fmt.Sprintf("%s asks to call %s %s: %s", agent, connection, tool, cut(oneLine(in), 100))
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
	"approved":       func(e gen.Event) string { return "approved by " + e.Actor },
	"tool_approval_requested": func(e gen.Event) string {
		line := toolAsk(str(e, "agent"), str(e, "connection"), str(e, "tool"), e.Data["input"])
		if offered, _ := e.Data["always_allow_offered"].(bool); offered {
			line += " (always allow offered)"
		}

		return line
	},
	"tool_approval_granted": func(e gen.Event) string {
		if always, _ := e.Data["always_allow"].(bool); always {
			return "tool call approved by " + e.Actor + ", always allow"
		}

		return "tool call approved by " + e.Actor
	},
	"tool_approval_denied": func(e gen.Event) string {
		if note := str(e, "note"); note != "" {
			return cut(oneLine("tool call denied by "+e.Actor+": "+note), 300)
		}

		return "tool call denied by " + e.Actor
	},
	"lease_taken":    func(e gen.Event) string { return "run stopped by " + e.Actor + " (" + str(e, "action") + ")" },
	"paused":         withReason,
	"note":           func(e gen.Event) string { return "note from " + e.Actor + ": " + cut(oneLine(str(e, "text")), 300) },
	"question_asked": questionAsked,
	"question_answered": func(e gen.Event) string {
		return cut(oneLine("answer from "+e.Actor+": "+str(e, "answer")), 300)
	},
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

// questionAsked is an agent's question, with its choices numbered from 1 as goal answer
// --choice takes them.
func questionAsked(e gen.Event) string {
	line := str(e, "agent") + " asked: " + str(e, "question")

	if choices, _ := e.Data["choices"].([]any); len(choices) > 0 {
		numbered := make([]string, len(choices))
		for i, c := range choices {
			text, _ := c.(string)
			if text == "" && c != nil {
				text = fmt.Sprint(c)
			}

			numbered[i] = fmt.Sprintf("%d. %s", i+1, text)
		}

		line += " (choices: " + strings.Join(numbered, ", ") + ")"
	}

	return cut(oneLine(line), 300)
}

func runFinished(e gen.Event) string {
	cost, _ := e.Data["cost_usd"].(float64)

	line := fmt.Sprintf("run %s (%s, %v turns)", str(e, "status"), usd(cost), e.Data["turns"])
	if s := str(e, "error"); s != "" {
		line += ": " + cut(oneLine(s), 200)
	}

	return line
}

// usd formats a cost in dollars: cents, or four decimals below a cent so a cheap run does not
// read as free ($0.0007), and "<$0.0001" below that.
func usd(v float64) string {
	switch {
	case v == 0 || v >= 0.01:
		return fmt.Sprintf("$%.2f", v)
	case v < 0.0001:
		return "<$0.0001"
	default:
		return fmt.Sprintf("$%.4f", v)
	}
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

// showItem prints an item: goal item <item> [--factory F]. Its JSON goes to stdout, its human
// touches, for the person reading it, to stderr.
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
	printTouches(out, res.JSON200.Touches)

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
	// verbDeny denies the call an item's agent asked to make; reject sends work back instead.
	verbDeny = "deny"
	// verbNote leaves a note for the agents; not a decision.
	verbNote = "note"
	// verbAnswer answers the question an item's agent asked; not a decision either.
	verbAnswer = "answer"
	// verbMove and verbPause are a person's controls of an item's flow.
	verbMove  = "move"
	verbPause = "pause"
)

// decide acts on an item: goal approve <item> [--always-allow]; goal reject <item> --reason "..."; goal retry
// <item>; goal merge <item>. Each takes --factory for a bare item number and --org. The decider
// is the request's actor, the local user (phase 2.2 has no sign-in).
func decide(ctx context.Context, out *printer, getenv func(string) string, verb string, args []string) error {
	fs := newFlags(verb)
	reason := fs.String("reason", "", "what must change (reject)")
	always := fs.Bool("always-allow", false, "approve the waiting tool call and let the agent make it without asking from now on (approve)")
	factory := fs.String("factory", "", "the factory of a bare item number")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || (verb == verbReject) != (*reason != "") || (*always && verb != verbApprove) {
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

	if verb == verbApprove {
		return approve(ctx, out, c, ref, *always)
	}

	it, err := act(ctx, c, verb, ref, *reason)
	if err != nil {
		return err
	}

	out.printf("✓ %s %s; now %s at %s\n", done[verb], it.Ref, it.State, it.Station)

	return nil
}

// codeToolApprovalOpen is goald's refusal to approve an item while a tool call of its agent
// waits: the call is decided on its own (ADR 0031 decision 6).
const codeToolApprovalOpen = "tool-approval-open"

// approve approves an item, or the tool call it waits on. --always-allow decides the call alone;
// a plain approve that goald refuses because a call waits approves that call, so one verb answers
// either kind of wait.
func approve(ctx context.Context, out *printer, c orgClient, ref string, always bool) error {
	if !always {
		res, err := c.ApproveItemWithResponse(ctx, c.org, ref)
		if err != nil {
			return apiDown(err)
		}

		if it := res.JSON200; it != nil {
			out.printf("✓ %s %s; now %s at %s\n", done[verbApprove], it.Ref, it.State, it.Station)

			return nil
		}

		if problemCode(res.ApplicationproblemJSON409) != codeToolApprovalOpen {
			return problem(res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
		}
	}

	d := gen.ToolDecision{Decision: gen.Approve}
	if always {
		d.AlwaysAllow = &always
	}

	it, err := decideTool(ctx, c, ref, d)
	if err != nil {
		return err
	}

	also := ""
	if always {
		also = ", and always allowed it"
	}

	out.printf("✓ approved %s's tool call%s; now %s at %s\n", it.Ref, also, it.State, it.Station)

	return nil
}

// denyItem denies the call an item's agent asked to make: goal deny <item> [--note "..."]
// [--factory F]. The item goes back to its station, and the next run's brief carries the denial
// and the note.
func denyItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags(verbDeny)
	note := fs.String("note", "", "why, for the agent's next run")
	factory := fs.String("factory", "", "the factory of a bare item number")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || (*note != "" && strings.TrimSpace(*note) == "") {
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

	it, err := decideTool(ctx, c, ref, gen.ToolDecision{Decision: gen.Deny, Note: optional(*note)})
	if err != nil {
		return err
	}

	out.printf("✓ denied %s's tool call; now %s at %s\n", it.Ref, it.State, it.Station)

	return nil
}

// decideTool sends a decision on the item's open tool approval and returns the item after it.
func decideTool(ctx context.Context, c orgClient, ref string, d gen.ToolDecision) (*gen.Item, error) {
	res, err := c.DecideToolApprovalWithResponse(ctx, c.org, ref, d)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, toolRefused(res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault), ref)
	}

	return res.JSON200, nil
}

// toolRefused says goald's refusal of a tool decision: the two a decision can run into in plain
// words, by the problem's code, anything else as the other verbs say it. The call may have been
// decided or withdrawn since it was read.
func toolRefused(res *http.Response, p *gen.Problem, ref string) error {
	switch problemCode(p) {
	case "no-open-tool-approval":
		return fmt.Errorf("%s has no tool call waiting for a decision", ref)
	case "always-allow-not-offered":
		return errors.New("Always allow is not offered for this call: its Ask first comes from the factory definition")
	default:
		return problem(res, p)
	}
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
		return problem(res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	}

	out.printf("✓ note left on %s; the next run reads it\n", ref)

	return nil
}

// answerItem answers the question an item's agent asked: goal answer <item> "<text>" | --choice
// <n> [--factory F]. Choices are numbered from 1, as goal follow prints them; goald counts from 0.
func answerItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags(verbAnswer)
	choice := fs.Int("choice", 0, "the number of the picked choice, from 1")
	factory := fs.String("factory", "", "the factory of a bare item number")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return errUsage
	}

	body, ok := answerBody(fs, pos, *choice)
	if !ok {
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

	res, err := c.AnswerItemWithResponse(ctx, c.org, ref, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return answerRefused(res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault), ref, *choice)
	}

	it := res.JSON200
	out.printf("✓ answered %s; now %s at %s\n", it.Ref, it.State, it.Station)

	return nil
}

// answerBody is the request goal answer sends for its positionals (the item, then the text) and
// --choice, numbered from 1: the text or the choice, never both, and no blank text.
func answerBody(fs *flag.FlagSet, pos []string, choice int) (gen.ItemAnswer, bool) {
	picked := false

	fs.Visit(func(f *flag.Flag) { picked = picked || f.Name == "choice" })

	switch {
	case picked && len(pos) == 1 && choice >= 1:
		index := choice - 1

		return gen.ItemAnswer{Choice: &index}, true
	case !picked && len(pos) == 2 && strings.TrimSpace(pos[1]) != "":
		return gen.ItemAnswer{Answer: &pos[1]}, true
	default:
		return gen.ItemAnswer{}, false
	}
}

// answerRefused says goald's refusal of an answer: the two an answer can run into in plain
// words, by the problem's code, anything else as the other verbs say it.
func answerRefused(res *http.Response, p *gen.Problem, ref string, choice int) error {
	switch problemCode(p) {
	case "no-open-question":
		return fmt.Errorf("%s has no open question to answer", ref)
	case "bad-choice":
		return fmt.Errorf("choice %d is not one of the question's choices", choice)
	default:
		return problem(res, p)
	}
}

// moveItem puts an item at another station of its line: goal move <item> <station> [--reason
// "..."] [--factory F]. goald refuses an upstream move without a reason, and a move to a done
// station; a run in progress is stopped.
func moveItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags(verbMove)
	reason := fs.String("reason", "", "why (required upstream, where it counts as rework)")
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

	res, err := c.MoveItemWithResponse(ctx, c.org, ref, gen.MoveRequest{To: pos[1], Reason: optional(*reason)})
	if err != nil {
		return apiDown(err)
	}

	it, err := answer(res.JSON200, res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	if err != nil {
		return err
	}

	out.printf("✓ moved %s; now %s at %s\n", it.Ref, it.State, it.Station)

	return nil
}

// pauseItem stops an item where it is until goal retry: goal pause <item> [--reason "..."]
// [--factory F]. A run in progress is stopped.
func pauseItem(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags(verbPause)
	reason := fs.String("reason", "", "why it is paused")
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

	res, err := c.PauseItemWithResponse(ctx, c.org, ref, gen.PauseRequest{Reason: optional(*reason)})
	if err != nil {
		return apiDown(err)
	}

	it, err := answer(res.JSON200, res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	if err != nil {
		return err
	}

	out.printf("✓ paused %s; now %s at %s (goal retry %s resumes it)\n", it.Ref, it.State, it.Station, it.Ref)

	return nil
}

// optional is s, or nil when it is empty.
func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// done is what each decision did, for the line that confirms it.
var done = map[string]string{verbApprove: "approved", verbReject: "rejected", verbRetry: "retried", verbMerge: "merged"}

// act sends a reject, retry or merge and returns the item after it, or why it was refused.
func act(ctx context.Context, c orgClient, verb, ref, reason string) (*gen.Item, error) {
	switch verb {
	case verbReject:
		res, err := c.RejectItemWithResponse(ctx, c.org, ref, gen.Rejection{Reason: reason})
		if err != nil {
			return nil, apiDown(err)
		}

		return answer(res.JSON200, res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	case verbRetry:
		res, err := c.RetryItemWithResponse(ctx, c.org, ref)
		if err != nil {
			return nil, apiDown(err)
		}

		return answer(res.JSON200, res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	default:
		res, err := c.MergeItemWithResponse(ctx, c.org, ref, gen.MergeRequest{})
		if err != nil {
			return nil, apiDown(err)
		}

		return answer(res.JSON200, res.HTTPResponse, conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault))
	}
}

// answer is the item goald answered with, or the problem it answered instead.
func answer(it *gen.Item, res *http.Response, p *gen.Problem) (*gen.Item, error) {
	if it == nil {
		return nil, problem(res, p)
	}

	return it, nil
}

// conflictOr is goald's problem for an operation that documents its 409 apart from the
// default response: the client decodes a 409 into the first, anything else into the second.
func conflictOr(conflict *gen.Conflict, p *gen.Problem) *gen.Problem {
	if conflict != nil {
		return conflict
	}

	return p
}

// problem is goald's refusal as the CLI says it: a refused credential or permission is an
// accessError (exit 77), anything else the problem's text.
func problem(res *http.Response, p *gen.Problem) error {
	if err := refusedAccess(res, p); err != nil {
		return err
	}

	if p != nil {
		return errors.New(problemText(p))
	}

	return fmt.Errorf("goald answered %s", res.Status)
}

// problemText is a problem as the CLI prints it: its title, then its detail when it has one (a
// not-found or a refused credential has none).
func problemText(p *gen.Problem) string {
	if d := deref(p.Detail); d != "" {
		return p.Title + ": " + d
	}

	return p.Title
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
