package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// secretCommand runs goal secret list | show | create | replace | delete | import: the
// organization's secrets, sealed in goald and never answered (ADR 0037 decision 17). A value comes
// only from stdin (create, replace) or an import file, never from the command line, and no output
// shows one. Changing secrets needs an owner or admin signed in within 10 minutes.
func secretCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{
		subList: secretList, subShow: secretShow, verbCreate: secretCreate, verbReplace: secretReplace, verbDelete: secretDelete,
		verbImport: secretImport,
	}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// The verbs goal secret adds to connection's.
const (
	verbReplace = "replace"
	verbImport  = "import"
)

// errNoSecretValue is a create or replace whose stdin holds no value.
var errNoSecretValue = errors.New("no value on stdin: pipe the secret in, as in `goal secret create <name> < value-file`")

// readSecretValue is the value on stdin, without its final newline.
func readSecretValue(in io.Reader) (string, error) {
	if in == nil {
		return "", errNoSecretValue
	}

	raw, err := io.ReadAll(io.LimitReader(in, 64<<10))
	if err != nil {
		return "", fmt.Errorf("read the value from stdin: %w", err)
	}

	v := trimNewline(string(raw))
	if strings.TrimSpace(v) == "" {
		return "", errNoSecretValue
	}

	return v, nil
}

func secretList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("secret list")
	orgFlag := addOrgFlag(fs)

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.ListSecretsWithResponse(ctx, c.org)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if len(res.JSON200.Secrets) == 0 {
		out.printf("no secrets in %s; create one with `goal secret create <name> < value-file`\n", c.org)

		return nil
	}

	for _, s := range res.JSON200.Secrets {
		out.printf("%s\n", secretLine(s, time.Now()))
	}

	return nil
}

// secretLine is one secret as goal secret list prints it: never its value.
func secretLine(s gen.Secret, now time.Time) string {
	return fmt.Sprintf("%-24s factories %s  used by %s  last used %s  expires %s", s.Name, secretFactories(s.Scope),
		secretUses(s.Uses), lastUsed(s.LastUsedAt, now), secretExpiry(s.ExpiresAt))
}

// secretFactories is a scope as goal secret prints it.
func secretFactories(sc gen.SecretScope) string {
	switch {
	case sc.AllFactories:
		return "all"
	case len(sc.Factories) > 0:
		return strings.Join(sc.Factories, ",")
	default:
		return "none"
	}
}

// secretUses is what uses a secret as goal secret prints it: "connection github, …" or nothing.
func secretUses(uses []gen.SecretUse) string {
	if len(uses) == 0 {
		return "nothing"
	}

	parts := make([]string, 0, len(uses))
	for _, u := range uses {
		parts = append(parts, string(u.Kind)+" "+u.Name)
	}

	return strings.Join(parts, ", ")
}

// secretExpiry is an expiry's day, or none.
func secretExpiry(at *time.Time) string {
	if at == nil {
		return "none"
	}

	return at.UTC().Format(time.DateOnly)
}

// secretShow prints one secret: its metadata and uses, never its value.
func secretShow(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("secret show")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.GetSecretWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	s := res.JSON200

	expires := secretExpiry(s.ExpiresAt)
	if s.ExpiresAt != nil && s.RemindDays > 0 {
		expires += fmt.Sprintf(", reminder %d days before", s.RemindDays)
	}

	description := s.Description
	if description == "" {
		description = "none"
	}

	out.printf("%s\n", s.Name)

	for _, row := range [][2]string{
		{"source", string(s.Source)},
		{"description", description},
		{"factories", secretFactories(s.Scope)},
		{"used by", secretUses(s.Uses)},
		{"expires", expires},
		{"last used", lastUsed(s.LastUsedAt, time.Now())},
		{"created on", s.CreatedAt.UTC().Format(time.DateOnly) + " by " + s.CreatedBy.Name},
		{"changed on", s.UpdatedAt.UTC().Format(time.DateOnly) + " by " + s.UpdatedBy.Name},
	} {
		out.printf("  %-12s %s\n", row[0], row[1])
	}

	return nil
}

func secretCreate(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("secret create")
	orgFlag := addOrgFlag(fs)
	description := fs.String("description", "", "what the secret is for")
	allFactories := fs.Bool("all-factories", false, "every factory's runs may open it")
	expires := fs.String("expires", "", "the day the value expires, YYYY-MM-DD (UTC)")
	remindDays := fs.Int("remind-days", 0, "days before --expires a reminder is due")

	var factories listFlag

	fs.Var(&factories, "factory", "a factory's id whose runs may open it (repeatable)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || (*allFactories && len(factories) > 0) {
		return errUsage
	}

	body, err := newSecretBody(fs, pos[0], *description, *expires, *remindDays, *allFactories, factories)
	if err != nil {
		return err
	}

	value, err := readSecretValue(out.in)
	if err != nil {
		return err
	}

	body.Value = &value

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.CreateSecretWithResponse(ctx, c.org, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ created secret %s in %s, open to factories: %s\n", res.JSON201.Name, c.org, secretFactories(res.JSON201.Scope))

	return nil
}

// newSecretBody is the NewSecret goal secret create's flags make, without its value: --expires is
// UTC midnight of its day, and remind_days is sent only when --remind-days is given.
func newSecretBody(fs *flag.FlagSet, name, description, expires string, remindDays int, allFactories bool, factories []string,
) (gen.NewSecret, error) {
	body := gen.NewSecret{
		Name: name, Scope: &gen.SecretScope{AllFactories: allFactories, Factories: append([]string{}, factories...)},
	}

	if description != "" {
		body.Description = &description
	}

	if expires != "" {
		day, err := time.ParseInLocation(time.DateOnly, expires, time.UTC)
		if err != nil {
			return gen.NewSecret{}, errUsage
		}

		body.ExpiresAt = &day
	}

	fs.Visit(func(f *flag.Flag) {
		if f.Name == "remind-days" {
			body.RemindDays = &remindDays
		}
	})

	return body, nil
}

// secretReplace replaces a secret's value with the one on stdin; nothing else changes.
func secretReplace(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("secret replace")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	value, err := readSecretValue(out.in)
	if err != nil {
		return err
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.ReplaceSecretValueWithResponse(ctx, c.org, pos[0], gen.SecretValue{Value: &value})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ replaced the value of secret %s; runs from now on get the new one\n", res.JSON200.Name)

	return nil
}

// secretDelete deletes a secret. goald refuses while something uses it; the error then names
// each use.
func secretDelete(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	return deleteNamed(ctx, out, getenv, args, "secret", func(c orgClient, name string) error {
		res, err := c.DeleteSecretWithResponse(ctx, c.org, name)
		if err != nil {
			return apiDown(err)
		}

		if p := res.ApplicationproblemJSONDefault; p != nil && deref(p.Code) == "secret-in-use" && p.Uses != nil {
			return fmt.Errorf("secret %s is used by %s; change them to stop using it, then delete it", name, secretUses(*p.Uses))
		}

		return deleted(res.StatusCode(), res.HTTPResponse, res.ApplicationproblemJSONDefault)
	})
}

// secretImport imports the secrets of a .env or YAML file: --dry-run says which names are new and
// which exist, a real run what it did with each. New names are open to no factory.
func secretImport(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("secret import")
	orgFlag := addOrgFlag(fs)
	format := fs.String("format", "", "env or yaml (default: from the file's extension)")
	onConflict := fs.String("on-conflict", "", "what to do with a name that exists: replace or skip (default skip)")
	dryRun := fs.Bool("dry-run", false, "say what the import would do and store nothing")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	body := gen.SecretImport{Format: gen.SecretImportFormat(*format), DryRun: dryRun}
	if *format == "" {
		body.Format = importFormatOf(pos[0])
	}

	if !body.Format.Valid() {
		return errUsage
	}

	if *onConflict != "" {
		oc := gen.SecretImportOnConflict(*onConflict)
		if !oc.Valid() {
			return errUsage
		}

		body.OnConflict = &oc
	}

	raw, err := os.ReadFile(pos[0])
	if err != nil {
		return err
	}

	text := string(raw)
	body.Text = &text

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.ImportSecretsWithResponse(ctx, c.org, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	for _, o := range res.JSON200.Secrets {
		out.printf("%s\n", importLine(o))
	}

	if res.JSON200.DryRun {
		out.printf("dry run: nothing stored; run it again without --dry-run to import\n")
	}

	return nil
}

// importFormatOf is the import format a file's extension names, or "" for another extension.
func importFormatOf(path string) gen.SecretImportFormat {
	switch ext := strings.ToLower(filepath.Ext(path)); {
	case ext == ".env" || strings.HasPrefix(filepath.Base(path), ".env"):
		return gen.SecretImportFormatEnv
	case ext == ".yaml" || ext == ".yml":
		return gen.SecretImportFormatYaml
	default:
		return ""
	}
}

// importLine is one name of an import: in a dry run new or exists, in a real one what it did.
func importLine(o gen.SecretImportOutcome) string {
	if o.Outcome != nil {
		return fmt.Sprintf("%-8s %s", *o.Outcome, o.Name)
	}

	if o.Exists {
		return fmt.Sprintf("exists %s (used by %s)", o.Name, secretUses(o.Uses))
	}

	return "new    " + o.Name
}

// deleted is a delete's answer as an error: nil for 204, else goald's refusal.
func deleted(status int, res *http.Response, p *gen.Problem) error {
	if status != http.StatusNoContent {
		return problem(res, p)
	}

	return nil
}

// deleteNamed runs goal connection delete <name> or goal connection kind delete <name>: what is
// the thing deleted, del deletes it.
func deleteNamed(ctx context.Context, out *printer, getenv func(string) string, args []string, what string,
	del func(c orgClient, name string) error,
) error {
	fs := newFlags(what + " delete")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	if err := del(c, pos[0]); err != nil {
		return err
	}

	out.printf("✓ deleted %s %s\n", what, pos[0])

	return nil
}

func trimNewline(s string) string { return strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r") }
