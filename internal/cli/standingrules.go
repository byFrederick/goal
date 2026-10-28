package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The standing-rules group and its revoke command.
const (
	cmdStandingRules = "standing-rules"
	subRevoke        = "revoke"
)

// standingRulesCommand is goal standing-rules list|revoke: the organization's standing rules,
// each letting one agent role call one connection tool without asking (ADR 0031 decision 7).
// They are made by Always allow on a tool call, never here.
func standingRulesCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{subList: standingRulesList, subRevoke: standingRuleRevoke}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// standingRulesList is goal standing-rules list [--revoked] [--json]: the active rules, and with
// --revoked the revoked ones too; --json writes goald's answer as it came.
func standingRulesList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("standing-rules list")
	orgFlag := addOrgFlag(fs)
	revoked := fs.Bool("revoked", false, "also list the revoked rules")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	params := &gen.ListStandingRulesParams{}
	if *revoked {
		params.Revoked = revoked
	}

	res, err := c.ListStandingRulesWithResponse(ctx, c.org, params)
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

	if len(res.JSON200.Rules) == 0 {
		out.printf("No standing rules\n")
	}

	for _, r := range res.JSON200.Rules {
		line := fmt.Sprintf("%s  %s  by %s at %s", r.Id, ruleText(r), r.CreatedBy.Name, r.CreatedAt.Local().Format(time.DateTime))
		if r.RevokedAt != nil {
			by := "someone"
			if r.RevokedBy != nil {
				by = r.RevokedBy.Name
			}

			line += fmt.Sprintf("  revoked by %s at %s", by, r.RevokedAt.Local().Format(time.DateTime))
		}

		out.printf("%s\n", line)
	}

	return nil
}

// ruleText is what a rule lets: "<agent> may call <connection> <tool>", with "(deleted)" for a
// connection that no longer exists.
func ruleText(r gen.StandingRule) string {
	return fmt.Sprintf("%s may call %s %s", r.Agent, connectionName(r), r.Tool)
}

func connectionName(r gen.StandingRule) string {
	if r.Connection == nil {
		return "(deleted)"
	}

	return *r.Connection
}

// standingRuleRevoke is goal standing-rules revoke <rule>: the agent asks first again for the
// rule's tool, from its next call.
func standingRuleRevoke(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("standing-rules revoke")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.RevokeStandingRuleWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if r := res.JSON200; r != nil {
		out.printf("✓ revoked %s: %s asks again before calling %s %s\n", r.Id, r.Agent, connectionName(*r), r.Tool)

		return nil
	}

	p := conflictOr(res.ApplicationproblemJSON409, res.ApplicationproblemJSONDefault)

	switch {
	case problemCode(p) == "standing-rule-revoked":
		return fmt.Errorf("standing rule %s is already revoked", pos[0])
	case res.StatusCode() == http.StatusNotFound:
		return errors.New("no standing rule " + pos[0])
	default:
		return problem(res.HTTPResponse, p)
	}
}
