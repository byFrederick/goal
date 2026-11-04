package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// access runs goal access [--factory <f>] [--group reach|factory|secret]: what the organization's
// agents reach (ADR 0037 decisions 16 and 17), from the published versions, the environment tests
// and the secrets; secrets by name, never a value.
func access(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("access")
	orgFlag := addOrgFlag(fs)
	factoryFlag := fs.String("factory", "", "only this factory's")
	by := fs.String("group", string(gen.GetAccessParamsByReach), "reach, factory or secret")

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	params := &gen.GetAccessParams{By: new(gen.GetAccessParamsBy(*by))}
	if !params.By.Valid() {
		return errUsage
	}

	if *factoryFlag != "" {
		params.Factory = factoryFlag
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.GetAccessWithResponse(ctx, c.org, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	a := res.JSON200
	out.printf("%d reached, %d need attention\n", a.Reached, a.NeedAttention)

	now := time.Now()

	for _, g := range a.Groups {
		out.printf("\n%s\n", g.Title)

		for _, r := range g.Rows {
			out.printf("  %s\n", accessLine(r, now))
		}
	}

	return nil
}

// accessLine is one row as goal access prints it: its kind and name, status, secrets and uses.
func accessLine(r gen.AccessRow, now time.Time) string {
	uses := make([]string, 0, len(r.Uses))
	for _, u := range r.Uses {
		uses = append(uses, u.Factory.Name+"/"+u.Environment)
	}

	secrets := "no secret"
	if len(r.Secrets) > 0 {
		secrets = strings.Join(r.Secrets, ",")
	}

	line := fmt.Sprintf("%-4s %-24s %-9s %-24s %s", r.Kind, r.Name, r.Status, secrets, strings.Join(uses, ", "))

	switch {
	case r.Status == gen.AccessStatusFailed && r.TestError != nil:
		line += "  (" + *r.TestError + ")"
	case r.Status == gen.AccessStatusExpiring || r.Status == gen.AccessStatusExpired:
		line += "  (expires " + secretExpiry(r.ExpiresAt) + ")"
	case r.TestedAt != nil:
		line += "  (tested " + lastUsed(r.TestedAt, now) + ")"
	}

	return line
}
