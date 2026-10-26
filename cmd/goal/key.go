package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// keyCommand runs goal key list | create <name> --scope S... [--factory F]... [--role R]
// [--expires Nd] | revoke <name|id>: the organization's API keys (spec 3 §9). Managing keys needs
// an owner or admin signed in within 10 minutes; a key never manages keys.
func keyCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	switch cmd, rest := args[0], args[1:]; cmd {
	case "list":
		return keyList(ctx, out, getenv, rest)
	case "create":
		return keyCreate(ctx, out, getenv, rest)
	case "revoke":
		return keyRevoke(ctx, out, getenv, rest)
	default:
		return errUsage
	}
}

// listKeys reads the organization's keys.
func listKeys(ctx context.Context, c orgClient) ([]gen.ApiKey, error) {
	res, err := c.ListAPIKeysWithResponse(ctx, c.org)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return res.JSON200.Keys, nil
}

// factoryNames maps the organization's factory ids to their names, for a key's factories.
func factoryNames(ctx context.Context, c orgClient) (map[string]string, error) {
	res, err := c.ListFactoriesWithResponse(ctx, c.org, nil)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	names := map[string]string{}
	for _, f := range res.JSON200.Factories {
		names[f.Id] = f.Name
	}

	return names, nil
}

func keyList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("key list")
	orgFlag := addOrgFlag(fs)

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	keys, err := listKeys(ctx, c)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		out.printf("no API keys in %s; create one with `goal key create <name> --scope S`\n", c.org)

		return nil
	}

	names, err := factoryNames(ctx, c)
	if err != nil {
		return err
	}

	for _, k := range keys {
		out.printf("%s\n", keyLine(k, names, time.Now()))
	}

	return nil
}

// keyLine is one key as goal key list prints it.
func keyLine(k gen.ApiKey, names map[string]string, now time.Time) string {
	factories := "all factories"

	if k.Factories != nil {
		fs := make([]string, 0, len(*k.Factories))
		for _, id := range *k.Factories {
			fs = append(fs, cmp.Or(names[id], id))
		}

		factories = strings.Join(fs, ",")
	}

	state := "expires " + k.ExpiresAt.Local().Format(time.DateOnly) + ", last used " + lastUsed(k.LastUsedAt, now)
	if k.RevokedAt != nil {
		state = "revoked " + k.RevokedAt.Local().Format(time.DateOnly)
		if k.RevokedBy != nil {
			state += " by " + k.RevokedBy.Name
		}
	}

	return fmt.Sprintf("%-24s %s…  %s  %s  at most %s  by %s  %s", k.Name, k.Hint, strings.Join(k.Scopes, ","), factories, k.RoleCeiling,
		k.CreatedBy.Name, state)
}

func lastUsed(at *time.Time, now time.Time) string {
	if at == nil {
		return "never"
	}

	switch d := now.Sub(*at); {
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + " min ago"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " h ago"
	default:
		return at.Local().Format(time.DateOnly)
	}
}

// listFlag is a flag that may be given more than once, each value possibly comma-separated.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(v string) error {
	for part := range strings.SplitSeq(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*l = append(*l, p)
		}
	}

	return nil
}

// errExpires is a --expires goal key create cannot read.
var errExpires = errors.New("--expires is a number of days from 1d to 90d, as in 30d")

// parseDays reads --expires: 30d or 30.
func parseDays(v string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), "d"))
	if err != nil || n < 1 || n > 90 {
		return 0, errExpires
	}

	return n, nil
}

func keyCreate(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("key create")
	orgFlag := addOrgFlag(fs)

	var scopes, factories listFlag

	fs.Var(&scopes, "scope", "a permission the key carries, such as factory:read or item:create (repeatable)")
	fs.Var(&factories, "factory", "a factory the key may reach (repeatable; default: every factory, now and later)")
	role := fs.String("role", string(gen.Viewer), "the most the key acts as on a factory: viewer, approver or editor")
	expires := fs.String("expires", "90d", "when the key stops working: 1d to 90d")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || len(scopes) == 0 {
		return errUsage
	}

	days, err := parseDays(*expires)
	if err != nil {
		return err
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	body := gen.NewApiKey{Name: pos[0], Scopes: scopes, RoleCeiling: gen.FactoryRole(*role), ExpiresInDays: &days}
	if len(factories) > 0 {
		body.Factories = (*[]string)(&factories)
	}

	res, err := c.CreateAPIKeyWithResponse(ctx, c.org, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	k := res.JSON201.Key
	where := "every factory"

	if k.Factories != nil {
		where = strings.Join(factories, ", ")
	}

	// The secret alone on stdout, so `goal key create … > file` keeps only it; the rest to stderr.
	out.printf("%s\n", res.JSON201.Secret)
	out.noticef("✓ created API key %s (%s…) in %s: at most %s on %s, until %s\n", k.Name, k.Hint, c.org, k.RoleCeiling, where,
		k.ExpiresAt.Local().Format(time.DateOnly))
	out.noticef("  store the secret now as %s in your CI's secret store: goald never shows it again\n", envAPIKey)

	return nil
}

func keyRevoke(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("key revoke")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	keys, err := listKeys(ctx, c)
	if err != nil {
		return err
	}

	k, err := liveKey(keys, pos[0])
	if err != nil {
		return err
	}

	res, err := c.RevokeAPIKeyWithResponse(ctx, c.org, k.Id)
	if err != nil {
		return apiDown(err)
	}

	if res.StatusCode() != http.StatusNoContent {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ revoked %s (%s…): anything using it stops working at once\n", k.Name, k.Hint)

	return nil
}

// liveKey finds a live key by id or name (case ignored).
func liveKey(keys []gen.ApiKey, ref string) (gen.ApiKey, error) {
	for _, k := range keys {
		if k.RevokedAt == nil && (k.Id == ref || strings.EqualFold(k.Name, ref)) {
			return k, nil
		}
	}

	return gen.ApiKey{}, fmt.Errorf("no live API key named %q: `goal key list` shows them", ref)
}
