package cli

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The policy subcommands.
const (
	policyGetVerb = "get"
	policySetVerb = "set"
)

// policyCommand reads or changes the organization's policy (ADR 0024): goal policy get prints it
// with who uses each model and box size and the egress addresses; goal policy set reads it,
// changes what its flags name and saves it on the revision it read, so a change made in between
// is refused instead of lost, and everything it does not name (the tool defaults among them) stays
// as it was read.
func policyCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	switch cmd, rest := args[0], args[1:]; cmd {
	case policyGetVerb:
		return policyGet(ctx, out, getenv, rest)
	case policySetVerb:
		return policySet(ctx, out, getenv, rest)
	default:
		return errUsage
	}
}

func policyGet(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("policy get")
	org := addOrgFlag(fs)

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	view, _, err := readPolicy(ctx, c)
	if err != nil {
		return err
	}

	printPolicy(out, view)

	return nil
}

// textFlag is a flag that may be given more than once, each value taken whole: a denied command
// may hold commas.
type textFlag []string

func (l *textFlag) String() string { return strings.Join(*l, "\n") }

func (l *textFlag) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("a blank value")
	}

	*l = append(*l, v)

	return nil
}

// policyChange is what goal policy set changes: each field set by its flag, the rest left as read.
type policyChange struct {
	models, denied []string
	def            string
	minutes        int
}

// apply is p with the change.
func (c policyChange) apply(p gen.OrgPolicyInput) gen.OrgPolicyInput {
	if len(c.models) > 0 {
		p.AllowedModels = c.models
	}

	if c.def != "" {
		p.DefaultModel = c.def
	}

	if len(c.denied) > 0 {
		p.DeniedCommands = c.denied
	}

	if c.minutes > 0 {
		p.BoxTimeLimitMinutes = c.minutes
	}

	return p
}

// parsePolicyChange reads goal policy set's flags: at least one, and a box limit in whole
// positive minutes.
func parsePolicyChange(args []string) (policyChange, string, error) {
	fs := newFlags("policy set")
	org := addOrgFlag(fs)

	var models listFlag

	var denied textFlag

	fs.Var(&models, "models", "the models agents may use, comma-separated (replaces the list)")
	fs.Var(&denied, "deny-command", "a Claude Code Bash pattern no agent may run, such as Bash(terraform apply:*) (repeatable; replaces the list)")
	def := fs.String("default", "", "the model of an agent that names none: one of the allowed models")
	boxLimit := fs.String("box-limit", "", "how long a box may run before it is stopped, in whole minutes: 90m, 3h")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 0 || (len(models) == 0 && len(denied) == 0 && *def == "" && *boxLimit == "") {
		return policyChange{}, "", errUsage
	}

	c := policyChange{models: models, denied: denied, def: *def}

	if *boxLimit != "" {
		d, err := time.ParseDuration(*boxLimit)
		if err != nil || d < time.Minute || d%time.Minute != 0 {
			return policyChange{}, "", errUsage
		}

		c.minutes = int(d / time.Minute)
	}

	return c, *org, nil
}

func policySet(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	change, org, err := parsePolicyChange(args)
	if err != nil {
		return err
	}

	c, err := connect(ctx, getenv, org)
	if err != nil {
		return err
	}

	view, etag, err := readPolicy(ctx, c)
	if err != nil {
		return err
	}

	res, err := c.PutOrgPolicyWithResponse(ctx, c.org, &gen.PutOrgPolicyParams{IfMatch: &etag}, change.apply(view.Policy))
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ policy saved at revision %d\n", res.JSON200.Revision)
	printPolicy(out, *res.JSON200)

	return nil
}

// readPolicy is the organization's policy and its ETag, the If-Match of a save.
func readPolicy(ctx context.Context, c orgClient) (gen.OrgPolicyView, string, error) {
	res, err := c.GetOrgPolicyWithResponse(ctx, c.org)
	if err != nil {
		return gen.OrgPolicyView{}, "", apiDown(err)
	}

	if res.JSON200 == nil {
		return gen.OrgPolicyView{}, "", problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return *res.JSON200, res.HTTPResponse.Header.Get("ETag"), nil
}

func printPolicy(out *printer, v gen.OrgPolicyView) {
	p := v.Policy

	if v.UpdatedAt != nil {
		out.printf("policy at revision %d, saved %s\n", v.Revision, stamp(*v.UpdatedAt))
	} else {
		out.printf("policy at revision 0: the default, never saved\n")
	}

	out.printf("models (* the default):\n")

	for _, m := range v.Models {
		mark, allowed := " ", "not allowed"
		if m.Id == p.DefaultModel {
			mark = "*"
		}

		if slices.Contains(p.AllowedModels, m.Id) {
			allowed = "allowed"
		}

		out.printf("  %s %-20s %-20s %-12s %s\n", mark, m.Id, m.Label, allowed, usedBy(m.UsedBy))
	}

	out.printf("denied commands:")

	if len(p.DeniedCommands) == 0 {
		out.printf(" none\n")
	} else {
		out.printf("\n")
	}

	for _, d := range p.DeniedCommands {
		out.printf("  %s\n", d)
	}

	t := p.Tools
	out.printf("tools: write outside the item's branch %s, merge pull requests %s, read the web %s, use credentials %s\n",
		t.WriteOutsideBranch, t.MergePullRequest, t.ReadWeb, t.UseCredentials)
	out.printf("box time limit: %d minutes\n", p.BoxTimeLimitMinutes)
	out.printf("box sizes:\n")

	for _, b := range v.BoxSizes {
		names := make([]string, 0, len(b.Factories))
		for _, f := range b.Factories {
			names = append(names, f.Name)
		}

		used := "not used yet"
		if len(names) > 0 {
			used = "used by " + strings.Join(names, ", ")
		}

		out.printf("  %-7s %s vCPU, %s  %s\n", b.Size, strconv.FormatFloat(b.Vcpu, 'f', -1, 64), gibibytes(b.MemoryBytes), used)
	}

	if len(v.EgressAddresses) == 0 {
		out.printf("egress addresses: none set (GOAL_EGRESS_ADDRESSES)\n")
	} else {
		out.printf("egress addresses: %s\n", strings.Join(v.EgressAddresses, ", "))
	}
}

// usedBy says who uses a model: "used by support's responder; web's backend, frontend".
func usedBy(uses []gen.ModelUse) string {
	if len(uses) == 0 {
		return "not used yet"
	}

	parts := make([]string, 0, len(uses))
	for _, u := range uses {
		parts = append(parts, u.Name+"'s "+strings.Join(u.Agents, ", "))
	}

	return "used by " + strings.Join(parts, "; ")
}

// gibibytes writes a memory size in GiB: 4 GiB, 0.5 GiB.
func gibibytes(n int64) string {
	return strconv.FormatFloat(float64(n)/(1<<30), 'f', -1, 64) + " GiB"
}
