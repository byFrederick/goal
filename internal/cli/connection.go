package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api/gen"
)

// connectionCommand runs goal connection list | show | create | update | delete | test | kinds |
// kind push | kind delete: the organization's connections and their kinds (spec 4 §9). Secret
// values come only from stdin (--secret k=-, one value) or a file (--secret-file k=<path>), never
// from the command line, and goald never shows them again. Changing connections needs an owner or
// admin signed in within 10 minutes.
func connectionCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	cmd, rest := args[0], args[1:]

	if cmd == "kind" {
		if len(rest) == 0 {
			return errUsage
		}

		switch rest[0] {
		case "push":
			return kindPush(ctx, out, getenv, rest[1:])
		case verbDelete:
			return kindDelete(ctx, out, getenv, rest[1:])
		default:
			return errUsage
		}
	}

	handler, ok := map[string]command{
		subList: connectionList, "show": connectionShow, verbCreate: connectionCreate, "update": connectionUpdate,
		verbDelete: connectionDelete, "test": connectionTest, "kinds": kindList, "import-env": connectionImportEnv,
	}[cmd]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, rest)
}

// The verbs goal connection shares.
const (
	verbCreate = "create"
	verbDelete = "delete"
)

// errSecretOnCommandLine refuses a secret value in argv, where shell history and ps would keep it.
var errSecretOnCommandLine = errors.New("a secret value comes from stdin (--secret <field>=-) or a file (--secret-file <field>=<path>), " +
	"never from the command line")

// valuesFlag collects repeated k=v flags.
type valuesFlag map[string]string

func (v valuesFlag) String() string { return strings.Join(slices.Sorted(maps.Keys(v)), ",") }

func (v valuesFlag) Set(s string) error {
	k, val, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("%q is not <field>=<value>", s)
	}

	v[strings.TrimSpace(k)] = val

	return nil
}

// secretFlags are a command's --secret and --secret-file flags.
type secretFlags struct {
	stdin, files valuesFlag
}

func addSecretFlags(fs *flag.FlagSet) *secretFlags {
	s := &secretFlags{stdin: valuesFlag{}, files: valuesFlag{}}
	fs.Var(s.stdin, "secret", "a secret field read from stdin: <field>=- (one value)")
	fs.Var(s.files, "secret-file", "a secret field read from a file: <field>=<path>")

	return s
}

// read is the secret values: one from stdin, any number from files, each without its final
// newline. A value on the command line is refused.
func (s *secretFlags) read(in io.Reader) (map[string]string, error) {
	out := map[string]string{}

	for field, v := range s.stdin {
		if v != "-" {
			return nil, errSecretOnCommandLine
		}

		if len(s.stdin) > 1 {
			return nil, errors.New("stdin holds one secret: read the others with --secret-file")
		}

		raw, err := io.ReadAll(io.LimitReader(in, 64<<10))
		if err != nil {
			return nil, fmt.Errorf("read %s from stdin: %w", field, err)
		}

		out[field] = trimNewline(string(raw))
	}

	for field, path := range s.files {
		raw, err := os.ReadFile(path) //nolint:gosec // a file the user names for their own secret
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", field, err)
		}

		out[field] = trimNewline(string(raw))
	}

	return out, nil
}

func trimNewline(s string) string { return strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r") }

func connectionList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("connection list")
	orgFlag := addOrgFlag(fs)
	kind := fs.String("kind", "", "only connections of this kind")
	factory := fs.String("factory", "", "only the connections this factory's current version names")

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	params := &gen.ListConnectionsParams{}
	if *kind != "" {
		params.Kind = kind
	}

	if *factory != "" {
		params.Factory = factory
	}

	res, err := c.ListConnectionsWithResponse(ctx, c.org, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if len(res.JSON200.Connections) == 0 {
		out.printf("no connections in %s; create one with `goal connection create <name> --kind <kind>` (`goal connection kinds` lists them)\n", c.org)

		return nil
	}

	for _, conn := range res.JSON200.Connections {
		out.printf("%s\n", connectionLine(conn, time.Now()))
	}

	return nil
}

// connectionLine is one connection as goal connection list prints it.
func connectionLine(c gen.Connection, now time.Time) string {
	secrets := "no secrets"
	if len(c.SecretsSet) > 0 {
		secrets = "secrets set: " + strings.Join(c.SecretsSet, ",")
	}

	users := make([]string, 0, len(c.UsedBy.Factories))
	for _, f := range c.UsedBy.Factories {
		users = append(users, f.Name)
	}

	used := "used by no factory"
	if len(users) > 0 {
		used = "used by " + strings.Join(users, ",")
	}

	return fmt.Sprintf("%-24s %-20s %-14s %s  %s  last used %s", c.Name, c.Kind, c.Status, secrets, used, lastUsed(c.LastUsedAt, now))
}

func connectionShow(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("connection show")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	conn, err := getConnection(ctx, c, pos[0])
	if err != nil {
		return err
	}

	out.printf("%s\n", connectionLine(conn, time.Now()))

	if conn.Description != "" {
		out.printf("  %s\n", conn.Description)
	}

	for _, k := range slices.Sorted(maps.Keys(conn.Fields)) {
		out.printf("  %s: %s\n", k, conn.Fields[k])
	}

	for _, k := range conn.SecretsSet {
		out.printf("  %s: set\n", k)
	}

	if conn.LastTest != nil {
		out.printf("  last test %s: %s\n", conn.LastTest.At.Local().Format(time.DateTime), testText(*conn.LastTest))
	}

	if conn.Env != nil && len(*conn.Env) > 0 {
		out.printf("  runs get %s\n", strings.Join(*conn.Env, ", "))
	}

	if d := deref(conn.Docs); d != "" {
		out.printf("  %s\n", d)
	}

	return nil
}

func getConnection(ctx context.Context, c orgClient, name string) (gen.Connection, error) {
	res, err := c.GetConnectionWithResponse(ctx, c.org, name)
	if err != nil {
		return gen.Connection{}, apiDown(err)
	}

	if res.JSON200 == nil {
		return gen.Connection{}, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return *res.JSON200, nil
}

func connectionCreate(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("connection create")
	orgFlag := addOrgFlag(fs)
	kind := fs.String("kind", "", "the connection's kind (goal connection kinds lists them)")
	description := fs.String("description", "", "what the connection is for")
	fields := valuesFlag{}
	fs.Var(fields, "field", "a non-secret field: <field>=<value> (repeatable)")
	secrets := addSecretFlags(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *kind == "" {
		return errUsage
	}

	values, err := secrets.read(out.in)
	if err != nil {
		return err
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	body := gen.NewConnection{Name: pos[0], Kind: *kind, Fields: (*map[string]string)(&fields), Secrets: &values}
	if *description != "" {
		body.Description = description
	}

	res, err := c.CreateConnectionWithResponse(ctx, c.org, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ created connection %s (%s) in %s; `goal connection test %s` checks it\n", res.JSON201.Name, res.JSON201.Kind, c.org, res.JSON201.Name)

	return nil
}

// connectionUpdate changes a connection: --field values are merged into the ones it has (an empty
// value clears one), --secret and --secret-file replace the named secrets only.
func connectionUpdate(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("connection update")
	orgFlag := addOrgFlag(fs)
	rename := fs.String("name", "", "a new name")
	description := fs.String("description", "", "a new description")
	fields := valuesFlag{}
	fs.Var(fields, "field", "a non-secret field: <field>=<value>, empty to clear it (repeatable)")
	secrets := addSecretFlags(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	values, err := secrets.read(out.in)
	if err != nil {
		return err
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	patch := gen.ConnectionPatch{}
	set := map[string]bool{}

	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if set["name"] {
		patch.Name = rename
	}

	if set["description"] {
		patch.Description = description
	}

	if len(values) > 0 {
		patch.Secrets = &values
	}

	if len(fields) > 0 {
		conn, err := getConnection(ctx, c, pos[0])
		if err != nil {
			return err
		}

		merged := maps.Clone(conn.Fields)
		maps.Copy(merged, fields)

		maps.DeleteFunc(merged, func(_, v string) bool { return v == "" })
		patch.Fields = &merged
	}

	res, err := c.UpdateConnectionWithResponse(ctx, c.org, pos[0], patch)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ updated connection %s\n", res.JSON200.Name)

	return nil
}

func connectionDelete(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	return deleteNamed(ctx, out, getenv, args, "connection", func(c orgClient, name string) error {
		res, err := c.DeleteConnectionWithResponse(ctx, c.org, name)
		if err != nil {
			return apiDown(err)
		}

		return deleted(res.StatusCode(), res.HTTPResponse, res.ApplicationproblemJSONDefault)
	})
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

// connectionTest runs the connection's test; a failed test exits 1.
func connectionTest(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("connection test")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.TestConnectionWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if !res.JSON200.Ok {
		return fmt.Errorf("connection %s failed its test: %s", pos[0], testText(*res.JSON200))
	}

	out.printf("✓ connection %s works: %s\n", pos[0], testText(*res.JSON200))

	return nil
}

// testText is a test's outcome in a few words.
func testText(r gen.ConnectionTestResult) string {
	switch {
	case r.Ok && r.Status != nil:
		return fmt.Sprintf("ok (%d)", *r.Status)
	case r.Ok:
		return "ok"
	default:
		return "failed: " + deref(r.Error)
	}
}

func kindList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("connection kinds")
	orgFlag := addOrgFlag(fs)

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.ListConnectionKindsWithResponse(ctx, c.org)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	for _, k := range res.JSON200.Kinds {
		out.printf("%s\n", kindLine(k))
	}

	return nil
}

// kindLine is one kind as goal connection kinds prints it: its fields, secret ones marked.
func kindLine(k gen.ConnectionKind) string {
	owner := "platform"
	if !k.Platform {
		owner = "organization"
	}

	fields := make([]string, 0, len(k.Fields))

	for _, f := range k.Fields {
		name := f.Name
		if f.Secret {
			name += " (secret)"
		}

		if f.Required {
			name += "*"
		}

		fields = append(fields, name)
	}

	modes := make([]string, 0, len(k.Modes))
	for _, m := range k.Modes {
		modes = append(modes, string(m))
	}

	return fmt.Sprintf("%-20s %-24s %-12s fields %s  modes %s", k.Name, k.Title, owner, strings.Join(fields, ", "), strings.Join(modes, ","))
}

// kindPush creates or replaces an organization kind from its file (YAML or JSON), named by its
// metadata.name.
func kindPush(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("connection kind push")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	body, name, err := readKindFile(pos[0])
	if err != nil {
		return err
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.PutConnectionKindWithResponse(ctx, c.org, name, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ pushed connection kind %s to %s\n", res.JSON200.Name, c.org)

	return nil
}

// readKindFile reads a kind file as the JSON object goald takes, and its metadata.name.
func readKindFile(path string) (gen.ConnectionKindFile, string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the kind file the user names
	if err != nil {
		return nil, "", err
	}

	var tree any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}

	// Through JSON, so YAML's maps become the object the API takes.
	asJSON, err := json.Marshal(tree)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}

	var doc gen.ConnectionKindFile

	dec := json.NewDecoder(bytes.NewReader(asJSON))
	if err := dec.Decode(&doc); err != nil {
		return nil, "", fmt.Errorf("%s is not a kind file: %w", path, err)
	}

	meta, _ := doc["metadata"].(map[string]any)

	name, _ := meta["name"].(string)
	if name == "" {
		return nil, "", fmt.Errorf("%s has no metadata.name", path)
	}

	return doc, name, nil
}

func kindDelete(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	return deleteNamed(ctx, out, getenv, args, "connection kind", func(c orgClient, name string) error {
		res, err := c.DeleteConnectionKindWithResponse(ctx, c.org, name)
		if err != nil {
			return apiDown(err)
		}

		return deleted(res.StatusCode(), res.HTTPResponse, res.ApplicationproblemJSONDefault)
	})
}

// The install's environment variables goal connection import-env reads.
const (
	envGitHubToken  = "GOAL_GITHUB_TOKEN" //nolint:gosec // a variable name, not a credential
	envGitHubWeb    = "GOAL_GITHUB_WEB_URL"
	envOAuthToken   = "CLAUDE_CODE_OAUTH_TOKEN" //nolint:gosec // a variable name, not a credential
	envAnthropicKey = "ANTHROPIC_API_KEY"
)

// connectionImportEnv runs goal connection import-env, the one-time move of an install's
// environment credentials into its organization's connections (spec 4 §14, step 4), from the
// CLI's environment (task connection:import-env reads .env): GOAL_GITHUB_TOKEN becomes the
// connection github, of kind github, on the host of GOAL_GITHUB_WEB_URL when it names a GitHub
// Enterprise; CLAUDE_CODE_OAUTH_TOKEN, or else ANTHROPIC_API_KEY, becomes claude, of kind
// claude-subscription or anthropic. A name the organization has already is left as it is, so a
// second run changes nothing. No value is printed.
func connectionImportEnv(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("connection import-env")
	orgFlag := addOrgFlag(fs)

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	imports, err := envConnections(getenv)
	if err != nil {
		return err
	}

	if len(imports) == 0 {
		out.printf("nothing to import: none of %s, %s or %s is set (task connection:import-env reads .env)\n",
			envGitHubToken, envOAuthToken, envAnthropicKey)

		return nil
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	for _, n := range imports {
		res, err := c.CreateConnectionWithResponse(ctx, c.org, n.body)
		if err != nil {
			return apiDown(err)
		}

		switch p := res.ApplicationproblemJSONDefault; {
		case res.JSON201 != nil:
			out.printf("✓ created connection %s (%s) in %s from %s\n", n.body.Name, n.body.Kind, c.org, n.from)
		case p != nil && deref(p.Code) == "connection-name-taken":
			out.printf("connection %s exists in %s; left as it is\n", n.body.Name, c.org)
		default:
			return fmt.Errorf("connection %s: %w", n.body.Name, problem(res.HTTPResponse, p))
		}
	}

	out.printf("name them in each factory: runtime.modelConnection: claude, runtime.repository.connection and connections: github; " +
		"then publish (goal factory validate shows the fixes)\n")

	return nil
}

// envConnection is one connection goal connection import-env creates, and the variable it is from.
type envConnection struct {
	body gen.NewConnection
	from string
}

// envConnections are the connections the environment's variables make.
func envConnections(getenv func(string) string) ([]envConnection, error) {
	var out []envConnection

	if token := strings.TrimSpace(getenv(envGitHubToken)); token != "" {
		fields, err := githubFields(strings.TrimSpace(getenv(envGitHubWeb)))
		if err != nil {
			return nil, err
		}

		out = append(out, envConnection{from: envGitHubToken, body: gen.NewConnection{
			Name: "github", Kind: "github", Fields: &fields, Secrets: &map[string]string{"token": token},
			Description: new("The repositories' token, imported from " + envGitHubToken),
		}})
	}

	model := func(kind, field, from string) envConnection {
		secrets := map[string]string{field: strings.TrimSpace(getenv(from))}

		return envConnection{from: from, body: gen.NewConnection{
			Name: "claude", Kind: kind, Secrets: &secrets, Description: new("The model's access, imported from " + from),
		}}
	}

	switch {
	case strings.TrimSpace(getenv(envOAuthToken)) != "":
		out = append(out, model("claude-subscription", "token", envOAuthToken))
	case strings.TrimSpace(getenv(envAnthropicKey)) != "":
		out = append(out, model("anthropic", "key", envAnthropicKey))
	}

	return out, nil
}

// githubFields are the github kind's fields for the GitHub at web (GOAL_GITHUB_WEB_URL, an
// origin; empty or github.com's needs none): a GitHub Enterprise's host, and its API for the
// connection's test (https://api.<host> on Enterprise Cloud, https://<host>/api/v3 on a Server).
func githubFields(web string) (map[string]string, error) {
	fields := map[string]string{}
	if web == "" {
		return fields, nil
	}

	u, err := url.Parse(web)
	if err != nil || u.Scheme != "https" || u.Host == "" || strings.Trim(u.Path, "/") != "" || u.User != nil {
		return nil, fmt.Errorf("%s %q is not an https origin such as https://github.com", envGitHubWeb, web)
	}

	host := strings.ToLower(u.Host)

	switch {
	case host == "github.com":
	case strings.HasSuffix(host, ".ghe.com"):
		fields["host"], fields["api_url"] = host, "https://api."+host
	default:
		fields["host"], fields["api_url"] = host, "https://"+host+"/api/v3"
	}

	return fields, nil
}
