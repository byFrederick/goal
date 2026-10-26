package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The goal admin subcommands that change platform admins.
const (
	subAdd    = "add"
	subRemove = "remove"
)

// adminCommand runs goal admin list | add <email> | remove <email> | events [--kind K]
// [--before N]: the install's platform admins and its security log, for platform admins.
func adminCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	c, err := newAPI(getenv)
	if err != nil {
		return err
	}

	switch cmd, rest := args[0], args[1:]; {
	case cmd == subList && len(rest) == 0:
		return adminList(ctx, out, c)
	case cmd == subAdd && len(rest) == 1:
		return adminAdd(ctx, out, c, rest[0])
	case cmd == subRemove && len(rest) == 1:
		return adminRemove(ctx, out, c, rest[0])
	case cmd == "events":
		return adminEvents(ctx, out, c, rest)
	default:
		return errUsage
	}
}

func platformAdmins(ctx context.Context, c *gen.ClientWithResponses) ([]gen.PlatformAdmin, error) {
	res, err := c.ListPlatformAdminsWithResponse(ctx)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return res.JSON200.Admins, nil
}

func adminList(ctx context.Context, out *printer, c *gen.ClientWithResponses) error {
	admins, err := platformAdmins(ctx, c)
	if err != nil {
		return err
	}

	for _, a := range admins {
		out.printf("%-24s %-32s added by %s, %s\n", a.User.Name, a.User.Email, a.AddedBy.Name, a.AddedAt.Local().Format(time.DateOnly))
	}

	return nil
}

func adminAdd(ctx context.Context, out *printer, c *gen.ClientWithResponses, email string) error {
	res, err := c.AddPlatformAdminWithResponse(ctx, gen.NewPlatformAdmin{Email: email})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ %s (%s) is a platform admin\n", res.JSON201.User.Name, res.JSON201.User.Email)

	return nil
}

// adminRemove finds the platform admin by email (any case) and removes them.
func adminRemove(ctx context.Context, out *printer, c *gen.ClientWithResponses, email string) error {
	admins, err := platformAdmins(ctx, c)
	if err != nil {
		return err
	}

	i := slices.IndexFunc(admins, func(a gen.PlatformAdmin) bool { return strings.EqualFold(a.User.Email, strings.TrimSpace(email)) })
	if i < 0 {
		return fmt.Errorf("%s is not a platform admin (goal admin list shows who is)", email)
	}

	a := admins[i]

	res, err := c.RemovePlatformAdminWithResponse(ctx, a.User.Id)
	if err != nil {
		return apiDown(err)
	}

	if res.StatusCode() != http.StatusNoContent {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ %s (%s) is no longer a platform admin\n", a.User.Name, a.User.Email)

	return nil
}

// adminEvents prints a page of the security log, newest first, one event per line.
func adminEvents(ctx context.Context, out *printer, c *gen.ClientWithResponses, args []string) error {
	fs := newFlags("admin events")
	kind := fs.String("kind", "", "only events of this kind, as in sign_in.succeeded")
	before := fs.Int64("before", 0, "events older than this id (the cursor the previous page printed)")

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	var params gen.ListAuthEventsParams
	if *kind != "" {
		params.Kind = kind
	}

	if *before > 0 {
		params.Before = before
	}

	res, err := c.ListAuthEventsWithResponse(ctx, &params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	for _, e := range res.JSON200.Events {
		data, err := json.Marshal(e.Data)
		if err != nil {
			return err
		}

		out.printf("%d  %s  %-24s user=%s ip=%s %s\n", e.Id, e.At.Local().Format(time.DateTime), e.Kind,
			cmp.Or(deref(e.UserId), "-"), cmp.Or(deref(e.Ip), "-"), data)
	}

	if next := res.JSON200.NextBefore; next != nil {
		again := fmt.Sprintf("--before %d", *next)
		if *kind != "" {
			again += " --kind " + *kind
		}

		out.printf("older events: goal admin events %s\n", again)
	}

	return nil
}
