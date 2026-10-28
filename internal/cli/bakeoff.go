package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The refusals the bake-off commands explain beyond goald's problem (ADR 0029).
const (
	codeBudgetExhausted = "learning-budget-exhausted"
	codeDraftConflict   = "draft-conflict"

	subRun   = "run"
	subApply = "apply"
)

// bakeOffCommand is goal bake-off: Jonah's bake-offs of one agent across models (ADR 0029),
// started, read and applied to the draft.
func bakeOffCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	return bakeOffGroup(pollEvery)(ctx, out, getenv, args)
}

// bakeOffGroup is goal bake-off with run --follow reading the bake-off every interval.
func bakeOffGroup(every time.Duration) command {
	return func(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
		if len(args) == 0 {
			return errUsage
		}

		run := func(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
			return bakeOffRun(ctx, out, getenv, args, every)
		}

		handler, ok := map[string]command{subRun: run, subShow: bakeOffShow, subApply: bakeOffApply}[args[0]]
		if !ok {
			return errUsage
		}

		return handler(ctx, out, getenv, args[1:])
	}
}

// bakeOffRun is goal bake-off run <agent> [--model M]... [--trials N] [--follow] [--factory F]
// [--json]: start the bake-off, print its estimate and arms and, with --follow, wait for its
// result.
func bakeOffRun(ctx context.Context, out *printer, getenv func(string) string, args []string, every time.Duration) error {
	fs := newFlags("bake-off run")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")

	var models listFlag

	fs.Var(&models, "model", "a model to run the agent on (repeatable; default the agent's model and goald's default models)")
	trials := fs.Int("trials", 0, "tries per case on every model, 1 to 10 (default 3)")
	followIt := fs.Bool("follow", false, "wait for the result and print it")
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

	body := gen.StartBakeOffJSONRequestBody{Agent: pos[0]}
	if len(models) > 0 {
		body.Models = (*[]string)(&models)
	}

	if *trials != 0 {
		body.Trials = trials
	}

	res, err := c.StartBakeOffWithResponse(ctx, c.org, f, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON202 == nil {
		return bakeOffProblem(res.HTTPResponse, res.ApplicationproblemJSONDefault, nil)
	}

	id := res.JSON202.BakeOff.Id

	switch {
	case *asJSON && *followIt:
		out.printf("%s", res.Body)

		return followBakeOffJSON(ctx, out, c, f, id, every)
	case *asJSON:
		out.printf("%s", res.Body)

		return nil
	}

	printBakeOffStarted(out, res.JSON202)

	if !*followIt {
		out.printf("follow it with: goal bake-off show %s --factory %s\n", id, f)

		return nil
	}

	out.printf("\n")

	return followBakeOff(ctx, out, c, f, id, every)
}

// printBakeOffStarted prints what the bake-off is about to cost and its arms, Now marking the
// agent's current model.
func printBakeOffStarted(out *printer, s *gen.BakeOffStarted) {
	b, est := s.BakeOff, s.Estimate

	out.printf("started bake-off %s of %s (%s) on v%d: %s, %s each on %d models, about %s\n",
		b.Id, b.Agent, b.Station, b.Version, plural(b.Cases, "case"), plural(b.Trials, "try"), len(b.Arms), usd(est.Usd))

	if len(est.Unpriced) > 0 {
		out.printf("not priced, with no tries and no runs of their agent yet: %s\n", strings.Join(est.Unpriced, ", "))
	}

	var tb bytes.Buffer

	tw := tabwriter.NewWriter(&tb, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ARM\tMODEL\t")

	for _, a := range b.Arms {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\n", a.Arm, a.Model, armMark(a, nil))
	}

	_ = tw.Flush()

	out.printf("%s", trimLines(tb.String()))
}

// followBakeOff reads the bake-off every interval until it is done, failed or cancelled, then
// prints it as goal bake-off show does.
func followBakeOff(ctx context.Context, out *printer, c orgClient, factory, id string, every time.Duration) error {
	res, err := awaitBakeOff(ctx, c, factory, id, every)
	if err != nil {
		return err
	}

	printBakeOff(out, res.JSON200)

	return nil
}

// followBakeOffJSON is followBakeOff printing goald's last answer as it came.
func followBakeOffJSON(ctx context.Context, out *printer, c orgClient, factory, id string, every time.Duration) error {
	res, err := awaitBakeOff(ctx, c, factory, id, every)
	if err != nil {
		return err
	}

	out.printf("%s", res.Body)

	return nil
}

func awaitBakeOff(ctx context.Context, c orgClient, factory, id string, every time.Duration) (*gen.GetBakeOffResponse, error) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		res, err := c.GetBakeOffWithResponse(ctx, c.org, factory, id)
		if err != nil {
			return nil, followingBakeOff(ctx, apiDown(err))
		}

		if res.JSON200 == nil {
			return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		if s := res.JSON200.State; s != gen.BakeOffStateQueued && s != gen.BakeOffStateRunning {
			return res, nil
		}

		select {
		case <-ctx.Done():
			return nil, followingBakeOff(ctx, nil)
		case <-t.C:
		}
	}
}

// followingBakeOff ends a follow cut short by Ctrl-C: the bake-off keeps running without us.
func followingBakeOff(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.New("stopped following; the bake-off keeps running in goald")
	}

	return err
}

// bakeOffShow is goal bake-off show <id> [--factory F] [--json]: the recommendation, one row per
// model and the tries passed per case.
func bakeOffShow(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("bake-off show")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

	_, _, res, err := readBakeOff(ctx, getenv, fs, args)
	if err != nil {
		return err
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	printBakeOff(out, res.JSON200)

	return nil
}

// readBakeOff parses a command line that names one bake-off, adding --org and --factory to fs,
// and reads the bake-off.
func readBakeOff(ctx context.Context, getenv func(string) string, fs *flag.FlagSet, args []string) (orgClient, string, *gen.GetBakeOffResponse, error) {
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "the factory (name or id); default the organization's only one")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return orgClient{}, "", nil, errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return orgClient{}, "", nil, err
	}

	f, err := commandFactory(ctx, c, *factory)
	if err != nil {
		return orgClient{}, "", nil, err
	}

	res, err := c.GetBakeOffWithResponse(ctx, c.org, f, pos[0])
	if err != nil {
		return orgClient{}, "", nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return orgClient{}, "", nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return c, f, res, nil
}

// bakeOffApply is goal bake-off apply <id> [--model M] [--factory F] [--json]: write the
// recommended arm's model, or M, as the agent's model into the factory's draft.
func bakeOffApply(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("bake-off apply")
	model := fs.String("model", "", "the model to write; default the recommended one")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

	c, f, got, err := readBakeOff(ctx, getenv, fs, args)
	if err != nil {
		return err
	}

	b := got.JSON200

	arm, err := applyArm(b, *model)
	if err != nil {
		return err
	}

	res, err := c.ApplyBakeOffWithResponse(ctx, c.org, f, b.Id, gen.ApplyBakeOffJSONRequestBody{Arm: arm.Arm})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return bakeOffProblem(res.HTTPResponse, res.ApplicationproblemJSONDefault, b)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	d := res.JSON200
	out.printf("wrote %s as %s's model in draft %s (the draft of v%d, revision %d)\n", arm.Model, b.Agent, d.Id, d.NextNumber, d.Revision)
	out.printf("nothing is published: the draft publishes through the eval gate as any other (goal factory publish %s)\n", f)

	return nil
}

// applyArm is the arm whose model is model, or the recommended arm when model is empty.
func applyArm(b *gen.BakeOff, model string) (gen.BakeOffArm, error) {
	if model == "" {
		if b.RecommendedArm == nil {
			return gen.BakeOffArm{}, fmt.Errorf("bake-off %s is %s and recommends no model yet: pass --model", b.Id, b.State)
		}

		if a, ok := armOf(b, *b.RecommendedArm); ok {
			return a, nil
		}

		return gen.BakeOffArm{}, fmt.Errorf("bake-off %s recommends arm %d, which it does not list", b.Id, *b.RecommendedArm)
	}

	models := make([]string, 0, len(b.Arms))

	for _, a := range b.Arms {
		if a.Model == model {
			return a, nil
		}

		models = append(models, a.Model)
	}

	return gen.BakeOffArm{}, fmt.Errorf("bake-off %s has no arm with %s: its models are %s", b.Id, model, strings.Join(models, ", "))
}

// bakeOffProblem is goald's refusal of a bake-off command, explaining the learning budget's and a
// stale draft's; b is the bake-off an apply was refused for.
func bakeOffProblem(res *http.Response, p *gen.Problem, b *gen.BakeOff) error {
	switch problemCode(p) {
	case codeBudgetExhausted:
		est := deref(p.Estimate)
		msg := fmt.Sprintf("refused: the bake-off would cost about %s, more than is left of this week's learning budget", usd(est.Usd))

		if len(est.Unpriced) > 0 {
			msg += fmt.Sprintf(" (not priced: %s)", strings.Join(est.Unpriced, ", "))
		}

		return errors.New(msg + "; run it with fewer models or --trials, or wait for the budget to renew")
	case codeDraftConflict:
		if b != nil {
			return fmt.Errorf("refused: %s's file changed since v%d, the version the bake-off ran (%s); merge the change in the builder, then apply again",
				b.Agent, b.Version, problemText(p))
		}
	}

	return problem(res, p)
}

// printBakeOff prints the bake-off's state and recommendation, one row per arm and the tries
// passed per case.
func printBakeOff(out *printer, b *gen.BakeOff) {
	out.printf("bake-off %s of %s (%s) on v%d: %s, %s, %s each, %s of about %s\n",
		b.Id, b.Agent, b.Station, b.Version, b.State, plural(b.Cases, "case"), plural(b.Trials, "try"), usd(b.CostUsd), usd(b.EstimateUsd))
	out.printf("%s\n\n", bakeOffHeadline(b))

	judge := judgeColumn(b)

	var tb bytes.Buffer

	tw := tabwriter.NewWriter(&tb, 0, 0, 2, ' ', 0)

	head := "MODEL\tPASS RATE\tCOST/ITEM\tTIME/ITEM\tTURNS\t"
	if judge != "" {
		head += "JUDGED " + strings.ToUpper(judge) + "\t"
	}

	_, _ = fmt.Fprintln(tw, head)

	for _, a := range b.Arms {
		row := []string{a.Model, "-", "-", "-", "-"}
		judged := "-"

		if s := a.Summary; s != nil {
			row = []string{a.Model, percent(s.PassRate), usd(s.CostPerItemUsd), seconds(s.TimePerItemSeconds), fmt.Sprintf("%.1f", s.Turns)}

			if s.Judged != nil {
				judged = percent(s.Judged.Share)
			}
		}

		if judge != "" {
			row = append(row, judged)
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\n", strings.Join(row, "\t"), armMark(a, b.RecommendedArm))
	}

	_ = tw.Flush()

	out.printf("%s", trimLines(tb.String()))

	if len(b.PerCase) == 0 {
		return
	}

	out.printf("\ntries passed per case, out of %d\n%s", b.Trials, perCaseTable(b))
}

// perCaseTable is a row per case with the tries passed on each arm, out of the trials.
func perCaseTable(b *gen.BakeOff) string {
	var tb bytes.Buffer

	tw := tabwriter.NewWriter(&tb, 0, 0, 2, ' ', 0)

	head := []string{"CASE"}
	for _, a := range b.Arms {
		head = append(head, a.Model)
	}

	_, _ = fmt.Fprintln(tw, strings.Join(head, "\t"))

	for _, pc := range b.PerCase {
		row := []string{pc.Case}

		for _, a := range b.Arms {
			cell := "-"

			for _, ca := range pc.Arms {
				if ca.Arm == a.Arm {
					cell = fmt.Sprintf("%d/%d", ca.Passed, b.Trials)
				}
			}

			row = append(row, cell)
		}

		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}

	_ = tw.Flush()

	return trimLines(tb.String())
}

// bakeOffHeadline compares the recommended arm with the current one (ADR 0029 decision 4).
func bakeOffHeadline(b *gen.BakeOff) string {
	rec, now, ok := recommendation(b)
	if !ok {
		return noRecommendation(b.State)
	}

	switch {
	case rec.Arm == now.Arm:
		return rec.Model + " (Now) stays recommended: no other model passes more, or as much for less"
	case now.Summary == nil:
		return rec.Model + " is recommended"
	}

	r, n := rec.Summary, now.Summary

	pass := fmt.Sprintf("a pass rate of %s against %s", percent(r.PassRate), percent(n.PassRate))
	if percent(r.PassRate) == percent(n.PassRate) {
		pass = fmt.Sprintf("the same pass rate (%s)", percent(r.PassRate))
	}

	cost := fmt.Sprintf("%s per item against %s", usd(r.CostPerItemUsd), usd(n.CostPerItemUsd))
	if n.CostPerItemUsd > 0 {
		cost += fmt.Sprintf(" (%s of the cost)", percent(r.CostPerItemUsd/n.CostPerItemUsd))
	}

	return fmt.Sprintf("%s is recommended over %s (Now): %s, at %s", rec.Model, now.Model, pass, cost)
}

// recommendation is the recommended arm, with its summary, and the current arm.
func recommendation(b *gen.BakeOff) (rec, now gen.BakeOffArm, ok bool) {
	for _, a := range b.Arms {
		if a.IsCurrent {
			now = a
		}
	}

	if b.RecommendedArm == nil {
		return rec, now, false
	}

	rec, ok = armOf(b, *b.RecommendedArm)

	return rec, now, ok && rec.Summary != nil
}

// noRecommendation says why a bake-off in state recommends no model.
func noRecommendation(state gen.BakeOffState) string {
	switch state {
	case gen.BakeOffStateQueued, gen.BakeOffStateRunning:
		return "no recommendation yet: it is " + string(state)
	case gen.BakeOffStateDone:
		return "no recommendation: an arm errored on every try"
	case gen.BakeOffStateCancelled:
		return "no recommendation: it was cancelled"
	case gen.BakeOffStateFailed:
		return "no recommendation: it failed"
	default:
		return "no recommendation: it is " + string(state)
	}
}

// judgeColumn names the advisory judge's column, its title or else its slug; empty when no judge
// scores the agent.
func judgeColumn(b *gen.BakeOff) string {
	for _, a := range b.Arms {
		if a.Summary != nil && a.Summary.Judged != nil {
			if t := deref(a.Summary.Judged.Title); t != "" {
				return t
			}

			return a.Summary.Judged.Judge
		}
	}

	return ""
}

// armMark is Recommended, Now, both or nothing.
func armMark(a gen.BakeOffArm, recommended *int) string {
	var marks []string

	if recommended != nil && *recommended == a.Arm {
		marks = append(marks, "Recommended")
	}

	if a.IsCurrent {
		marks = append(marks, "Now")
	}

	return strings.Join(marks, ", ")
}

func armOf(b *gen.BakeOff, arm int) (gen.BakeOffArm, bool) {
	for _, a := range b.Arms {
		if a.Arm == arm {
			return a, true
		}
	}

	return gen.BakeOffArm{}, false
}

func seconds(s float64) string {
	return time.Duration(s * float64(time.Second)).Round(time.Second).String()
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}

	if stem, ok := strings.CutSuffix(noun, "y"); ok {
		return fmt.Sprintf("%d %sies", n, stem)
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

// trimLines drops the spaces tabwriter pads an empty last cell with.
func trimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}

	return strings.Join(lines, "\n")
}
