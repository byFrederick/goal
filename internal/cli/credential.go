package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// credentialCommand runs goal credential list | create | replace | rm | import: the
// organization's named credentials, sealed in goald and never answered (ADR 0017). A value comes
// only from stdin (create, replace) or an import file, never from the command line, and no output
// shows one. Changing credentials needs an owner or admin signed in within 10 minutes.
func credentialCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{
		subList: credentialList, verbCreate: credentialCreate, verbReplace: credentialReplace, "rm": credentialRm, verbImport: credentialImport,
	}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// The verbs goal credential adds to connection's.
const (
	verbReplace = "replace"
	verbImport  = "import"
)

// errNoCredentialValue is a create or replace whose stdin holds no value.
var errNoCredentialValue = errors.New("no value on stdin: pipe the credential in, as in `goal credential create <name> --type api_key < value-file`")

// readCredentialValue is the value on stdin, without its final newline.
func readCredentialValue(in io.Reader) (string, error) {
	if in == nil {
		return "", errNoCredentialValue
	}

	raw, err := io.ReadAll(io.LimitReader(in, 64<<10))
	if err != nil {
		return "", fmt.Errorf("read the value from stdin: %w", err)
	}

	v := trimNewline(string(raw))
	if strings.TrimSpace(v) == "" {
		return "", errNoCredentialValue
	}

	return v, nil
}

func credentialList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("credential list")
	orgFlag := addOrgFlag(fs)

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.ListCredentialsWithResponse(ctx, c.org)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if len(res.JSON200.Credentials) == 0 {
		out.printf("no credentials in %s; create one with `goal credential create <name> --type api_key < value-file`\n", c.org)

		return nil
	}

	for _, cred := range res.JSON200.Credentials {
		out.printf("%s\n", credentialLine(cred, time.Now()))
	}

	return nil
}

// credentialLine is one credential as goal credential list prints it: never its value.
func credentialLine(c gen.Credential, now time.Time) string {
	factories := "none"

	switch {
	case c.Scope.AllFactories:
		factories = "all"
	case len(c.Scope.Factories) > 0:
		factories = strings.Join(c.Scope.Factories, ",")
	}

	users := make([]string, 0, len(c.UsedBy.Connections)+len(c.UsedBy.Agents))
	for _, conn := range c.UsedBy.Connections {
		users = append(users, "connection "+conn)
	}

	for _, a := range c.UsedBy.Agents {
		users = append(users, "agent "+a.FactoryId+"/"+a.Agent)
	}

	used := "nothing"
	if len(users) > 0 {
		used = strings.Join(users, ", ")
	}

	expires := "none"
	if c.ExpiresAt != nil {
		expires = c.ExpiresAt.UTC().Format(time.DateOnly)
	}

	return fmt.Sprintf("%-24s %-12s factories %s  used by %s  last used %s  expires %s", c.Name, c.Type, factories, used,
		lastUsed(c.LastUsedAt, now), expires)
}

func credentialCreate(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("credential create")
	orgFlag := addOrgFlag(fs)
	typ := fs.String("type", "", "what the credential is: api_key, oauth_token, ssh_key, cloud_role or app_key")
	description := fs.String("description", "", "what the credential is for")
	allFactories := fs.Bool("all-factories", false, "every factory's runs may open it")
	expires := fs.String("expires", "", "the day the value expires, YYYY-MM-DD (UTC)")
	remindDays := fs.Int("remind-days", 0, "days before --expires a rotation reminder is due")

	var factories listFlag

	fs.Var(&factories, "factory", "a factory's id whose runs may open it (repeatable)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *typ == "" || (*allFactories && len(factories) > 0) {
		return errUsage
	}

	body, err := newCredentialBody(fs, pos[0], *typ, *description, *expires, *remindDays, *allFactories, factories)
	if err != nil {
		return err
	}

	value, err := readCredentialValue(out.in)
	if err != nil {
		return err
	}

	body.Value = &value

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.CreateCredentialWithResponse(ctx, c.org, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ created credential %s (%s) in %s; a connection refers to it as {{%s}}\n", res.JSON201.Name, res.JSON201.Type, c.org, res.JSON201.Name)

	return nil
}

// newCredentialBody is the NewCredential goal credential create's flags make, without its value:
// --expires is UTC midnight of its day, and remind_days is sent only when --remind-days is given.
func newCredentialBody(fs *flag.FlagSet, name, typ, description, expires string, remindDays int, allFactories bool,
	factories []string,
) (gen.NewCredential, error) {
	body := gen.NewCredential{
		Name: name, Type: gen.CredentialType(typ),
		Scope: &gen.CredentialScope{AllFactories: allFactories, Factories: append([]string{}, factories...)},
	}

	if description != "" {
		body.Description = &description
	}

	if expires != "" {
		day, err := time.ParseInLocation(time.DateOnly, expires, time.UTC)
		if err != nil {
			return gen.NewCredential{}, errUsage
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

// credentialReplace replaces a credential's value with the one on stdin; nothing else changes.
func credentialReplace(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("credential replace")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	value, err := readCredentialValue(out.in)
	if err != nil {
		return err
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.PatchCredentialWithResponse(ctx, c.org, pos[0], gen.CredentialPatch{Value: &value})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ replaced the value of credential %s; runs from now on get the new one\n", res.JSON200.Name)

	return nil
}

// credentialRm deletes a credential. goald refuses while a connection refers to it; the error
// then names those connections, from the credential's used_by when goald answers it.
func credentialRm(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	return deleteNamed(ctx, out, getenv, args, "credential", func(c orgClient, name string) error {
		res, err := c.DeleteCredentialWithResponse(ctx, c.org, name)
		if err != nil {
			return apiDown(err)
		}

		if p := res.ApplicationproblemJSONDefault; p != nil && deref(p.Code) == "credential-in-use" {
			if conns := credentialConnections(ctx, c, name); len(conns) > 0 {
				return fmt.Errorf("credential %s is in use by connections %s; change them to stop referring to {{%s}}, then delete it",
					name, strings.Join(conns, ", "), name)
			}
		}

		return deleted(res.StatusCode(), res.HTTPResponse, res.ApplicationproblemJSONDefault)
	})
}

// credentialConnections are the connections that refer to credential name, or none when goald
// does not answer them.
func credentialConnections(ctx context.Context, c orgClient, name string) []string {
	res, err := c.GetCredentialWithResponse(ctx, c.org, name)
	if err != nil || res.JSON200 == nil {
		return nil
	}

	return res.JSON200.UsedBy.Connections
}

// credentialImport imports the credentials of a .env or YAML file: --dry-run says which names are
// new and which exist, a real run what it did with each.
func credentialImport(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("credential import")
	orgFlag := addOrgFlag(fs)
	format := fs.String("format", "", "env or yaml (default: from the file's extension)")
	onConflict := fs.String("on-conflict", "", "what to do with a name that exists: replace or skip (default skip)")
	dryRun := fs.Bool("dry-run", false, "say what the import would do and store nothing")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	body := gen.CredentialImport{Format: gen.CredentialImportFormat(*format), DryRun: dryRun}
	if *format == "" {
		body.Format = importFormatOf(pos[0])
	}

	if !body.Format.Valid() {
		return errUsage
	}

	if *onConflict != "" {
		oc := gen.CredentialImportOnConflict(*onConflict)
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

	res, err := c.ImportCredentialsWithResponse(ctx, c.org, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	for _, o := range res.JSON200.Credentials {
		out.printf("%s\n", importLine(o))
	}

	if res.JSON200.DryRun {
		out.printf("dry run: nothing stored; run it again without --dry-run to import\n")
	}

	return nil
}

// importFormatOf is the import format a file's extension names, or "" for another extension.
func importFormatOf(path string) gen.CredentialImportFormat {
	switch ext := strings.ToLower(filepath.Ext(path)); {
	case ext == ".env" || strings.HasPrefix(filepath.Base(path), ".env"):
		return gen.CredentialImportFormatEnv
	case ext == ".yaml" || ext == ".yml":
		return gen.CredentialImportFormatYaml
	default:
		return ""
	}
}

// importLine is one name of an import: in a dry run new or exists, in a real one what it did.
func importLine(o gen.CredentialImportOutcome) string {
	if o.Outcome != nil {
		return fmt.Sprintf("%-8s %s", *o.Outcome, o.Name)
	}

	if o.Exists {
		return fmt.Sprintf("exists %s (used by %d connections)", o.Name, o.Connections)
	}

	return "new    " + o.Name
}
