package domain

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ConnectionKind is a template for connections (spec 4 §4.2): the fields an admin fills in, which
// of them are secret, and what a run's box gets from them. The platform ships some, embedded in
// goald (internal/connkind); an organization's admins add their own. A kind has no versions.
type ConnectionKind struct {
	// Name is a slug, unique among the platform's kinds and the organization's.
	Name        string
	Title       string
	Description string
	// Platform marks a kind shipped with goald; an organization's own kind of the same name wins
	// inside that organization.
	Platform bool
	Spec     KindSpec
}

// KindSpec is what a kind declares. Its strings may hold templates: {{field}} names one of
// Fields, {{minted.<name>}} a value goald mints for the run (kinds with Mint only); MCP header
// values may also hold ${NAME}, one of Env's variables, which goald leaves for Claude Code to
// expand in the box, so no secret is in the run's bundle. Nothing else is evaluated. A value that names an optional field without a value is left out, with the env
// variable or header it belongs to; a git login, MCP url or MCP command with such a value is.
// The JSON names are the kind file's (contracts/definitions/connection-kind.schema.json).
type KindSpec struct {
	Fields []KindField `json:"fields"`
	// Env is what a run's box gets as environment variables: API and CLI use, stdio MCP servers.
	Env map[string]string `json:"env,omitempty"`
	// CLI installs a tool at prepare (a mise tool spec).
	CLI *KindCLI `json:"cli,omitempty"`
	// Git is git over https to one host, its user and password from the box's env.
	Git *KindGit `json:"git,omitempty"`
	// MCP is a remote MCP server called from the box (URL, Headers) or a stdio server the worker
	// starts inside the box with Env (Command), or both.
	MCP *KindMCP `json:"mcp,omitempty"`
	// Model marks a kind that gives Claude Code its model; only such a kind may be a factory's
	// model connection.
	Model bool `json:"model,omitempty"`
	// CodeHost, Mint and Sync name the adapters behind a kind, its only provider code: a code host
	// that reads and merges pull requests (github, gitlab), a minter of per-run credentials
	// (aws-sts), a knowledge source reader (gitdocs, confluence).
	CodeHost string `json:"codeHost,omitempty"`
	Mint     string `json:"mint,omitempty"`
	Sync     string `json:"sync,omitempty"`
	// Test is the request that tells whether a connection works.
	Test *KindTest `json:"test,omitempty"`
	// Docs tells agents how to use the connection; it is never templated.
	Docs string `json:"docs,omitempty"`
}

// KindField is one value of a kind's connections. A secret field is write-only and sealed.
type KindField struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	// Default is the value of a field left empty; a secret field has none.
	Default string `json:"default,omitempty"`
}

// KindCLI is a tool the box installs.
type KindCLI struct {
	Tool string `json:"tool"`
}

// KindGit is a git login for https to Host: Username and the password in the env variable
// PasswordEnv, one of the kind's Env names.
type KindGit struct {
	Host        string `json:"host"`
	Username    string `json:"username"`
	PasswordEnv string `json:"passwordEnv"`
}

// KindMCP is a kind's MCP server: remote (URL and Headers, which name secrets as ${NAME}) or stdio
// (Command, an argv the worker runs inside the box).
type KindMCP struct {
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command []string          `json:"command,omitempty"`
}

// KindTest is a connection's test: a request whose status must be Expect. Basic, when set, is
// HTTP basic authentication (an Atlassian API token's form).
type KindTest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Basic   *KindBasicAuth    `json:"basic,omitempty"`
	Expect  int               `json:"expect"`
}

// KindBasicAuth is HTTP basic authentication.
type KindBasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ConnectionID is a connection's id (a UUID).
type ConnectionID string

// Connection is one organization's connection as everyone who may see it sees it: its secret
// values never, only which secret fields are set.
type Connection struct {
	ID          ConnectionID
	Name        string
	Kind        string
	Description string
	// Fields are the non-secret values an admin set (a kind's defaults are not copied in).
	Fields map[string]string
	// SecretFields names the secret fields that are set, sealed or a reference, sorted.
	SecretFields []string
	// SecretRefs maps each secret field that refers to a credential to the credential's name
	// (ADR 0017 decision 5): stored in the clear, never sealed. Its fields are in SecretFields.
	SecretRefs map[string]string
	CreatedBy  Actor
	CreatedAt  time.Time
	UpdatedBy  Actor
	UpdatedAt  time.Time
	LastUsedAt *time.Time
	LastTest   *ConnectionTest
}

// ConnectionTest is the outcome of a connection's test.
type ConnectionTest struct {
	At    time.Time
	OK    bool
	Error string
}

// NewConnection is a connection to create: its kind's non-secret values in Fields, its secret
// ones in Secrets. A secret value that is exactly {{name}} refers to credential name
// (ConnectionSecrets).
type NewConnection struct {
	Name, Kind, Description string
	Fields, Secrets         map[string]string
}

// ConnectionPatch changes a connection. Name, when set, renames it (its sealed values stay: they
// are bound to its id); Description, when set, replaces it; Fields, when not nil, replaces every
// non-secret value; Secrets replaces the named secret values only, an empty value clearing an
// optional one and {{name}} making it a reference to credential name (ConnectionSecrets). The kind
// never changes.
type ConnectionPatch struct {
	Name        *string
	Description *string
	Fields      map[string]string
	Secrets     map[string]string
}

// Empty reports whether p changes nothing.
func (p ConnectionPatch) Empty() bool {
	return p.Name == nil && p.Description == nil && p.Fields == nil && len(p.Secrets) == 0
}

// ResolvedConnection is a connection opened for one run: its kind and every value, secret ones
// included. Only ports.Connections.Resolve returns one.
type ResolvedConnection struct {
	Connection Connection
	Kind       ConnectionKind
	// Values are the connection's fields and sealed secrets by field name (defaults are applied by
	// Render).
	Values map[string]string
	// SecretRefs maps each secret field that refers to a credential to its name, unopened: the
	// engine opens them at run start and puts their values in Values.
	SecretRefs map[string]string
}

// RunConnection is what a run's box gets from one connection: Render's output.
type RunConnection struct {
	Env map[string]string
	// Git is nil without a git login.
	Git *KindGit
	// MCP is nil without an MCP server; its URL or Command may be empty.
	MCP *KindMCP
	// CLI is a mise tool spec to install, or "".
	CLI  string
	Docs string
}

// Limits of connections and kinds (00051's checks).
const (
	MaxConnectionDescription = 500
	MaxKindFields            = 20
)

var (
	namePattern      = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)
	fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	envNamePattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	templateRef      = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	envRef           = regexp.MustCompile(`\$\{([^{}]*)\}`)
)

// The field rules CheckValues names; both wrap ErrInvalid.
var (
	// ErrFieldRequired: a required field of the kind has no value (and no default).
	ErrFieldRequired = fmt.Errorf("field required: %w", ErrInvalid)
	// ErrFieldUnknown: a value names no field of the kind, or a field of the other sort (a secret
	// field as a field, a field as a secret).
	ErrFieldUnknown = fmt.Errorf("field unknown: %w", ErrInvalid)
)

// ConnectionSecrets splits a connection's secret values into the ones to seal and its references
// to credentials (ADR 0017 decision 5): a value that is exactly {{name}} refers to credential
// name, which refs maps its field to; every other value, an empty one included, is in sealed. A
// {{…}} anywhere else in a secret value, or anywhere in a non-secret field's value, is refused.
// The error wraps ErrInvalid and names fields, never a value.
func ConnectionSecrets(fields, secrets map[string]string) (sealed, refs map[string]string, err error) {
	var errs []error

	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if anyRef.MatchString(fields[name]) {
			errs = append(errs, fmt.Errorf("field %q is not secret, so it cannot refer to a credential: %w", name, ErrInvalid))
		}
	}

	sealed, refs = map[string]string{}, map[string]string{}

	for _, name := range slices.Sorted(maps.Keys(secrets)) {
		cred, ok, err := CredentialRef(secrets[name])

		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("secret field %q: %w", name, err))
		case ok:
			refs[name] = cred
		default:
			sealed[name] = secrets[name]
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}

	return sealed, refs, nil
}

// CheckConnectionName enforces the name of a connection or a kind: 3 to 40 lowercase letters,
// digits and inner dashes, starting with a letter. The error wraps ErrInvalid.
func CheckConnectionName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("name %q must be 3 to 40 lowercase letters, digits or dashes, starting with a letter and not ending with a dash: %w",
			name, ErrInvalid)
	}

	return nil
}

// CheckConnectionDescription enforces at most MaxConnectionDescription characters. The error
// wraps ErrInvalid.
func CheckConnectionDescription(d string) error {
	if utf8.RuneCountInString(d) > MaxConnectionDescription {
		return fmt.Errorf("a description has at most %d characters: %w", MaxConnectionDescription, ErrInvalid)
	}

	return nil
}

// reservedRunEnv are the variables a box's env keeps for the platform; reservedRunEnvPrefixes
// the prefixes.
var (
	reservedRunEnv = []string{
		"HOME", "PATH", "USER", "SHELL", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "NODE_USE_ENV_PROXY", "CLAUDE_CONFIG_DIR",
	}
	reservedRunEnvPrefixes = []string{"GOAL_", "LD_", "MISE_"}
)

// ReservedRunEnv reports whether a connection may not set the env variable name in a run's box:
// GOAL_*, HOME, PATH, USER, SHELL, the proxy variables, NODE_USE_ENV_PROXY, CLAUDE_CONFIG_DIR,
// LD_* and MISE_*, in any case.
func ReservedRunEnv(name string) bool {
	upper := strings.ToUpper(name)

	return slices.Contains(reservedRunEnv, upper) ||
		slices.ContainsFunc(reservedRunEnvPrefixes, func(p string) bool { return strings.HasPrefix(upper, p) })
}

// checkEnvName refuses an env variable a connection may not set.
func checkEnvName(name string) error {
	switch {
	case !envNamePattern.MatchString(name):
		return fmt.Errorf("env %q is not a variable name: %w", name, ErrInvalid)
	case ReservedRunEnv(name):
		return fmt.Errorf("env %q is reserved for the platform: %w", name, ErrInvalid)
	default:
		return nil
	}
}

// Field returns the kind's field name.
func (k ConnectionKind) Field(name string) (KindField, bool) {
	i := slices.IndexFunc(k.Spec.Fields, func(f KindField) bool { return f.Name == name })
	if i < 0 {
		return KindField{}, false
	}

	return k.Spec.Fields[i], true
}

// ref is one {{…}} of a template: a field's name or, minted, a minted value's.
type ref struct {
	name   string
	minted bool
}

// refs parses s's templates. Anything between {{ and }} that is not a field name or
// minted.<name>, and a {{ or }} left over, is an error.
func refs(s string) ([]ref, error) {
	var out []ref

	for _, m := range templateRef.FindAllStringSubmatch(s, -1) {
		name, minted := strings.CutPrefix(m[1], "minted.")
		if !fieldNamePattern.MatchString(name) {
			return nil, fmt.Errorf("template %q: {{%s}} is neither {{<field>}} nor {{minted.<name>}}: %w", s, m[1], ErrInvalid)
		}

		out = append(out, ref{name: name, minted: minted})
	}

	if rest := templateRef.ReplaceAllString(s, ""); strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		return nil, fmt.Errorf("template %q has an unclosed {{ or a stray }}: %w", s, ErrInvalid)
	}

	return out, nil
}

// template is one templated string of a kind and the secret values it may name.
type template struct {
	where   string
	s       string
	carries carries
}

// carries is which secret values a templated string may name.
type carries int

const (
	carriesNone carries = iota
	// carriesStored is a connection's secret fields: a test's headers and basic password (goald
	// tests a connection without minting).
	carriesStored
	// carriesAll is secret fields and minted values: env values.
	carriesAll
)

// templates are every templated string of k with the secret values it may name: env values carry
// secret fields and minted values, a test's headers and basic password secret fields, and nothing
// else carries either, so none reaches a URL, a command line, a git login, the run's bundle (MCP
// headers name env variables instead), a log or a prompt.
func (k ConnectionKind) templates() []template {
	s := k.Spec

	var out []template

	for _, name := range slices.Sorted(maps.Keys(s.Env)) {
		out = append(out, template{"env name", name, carriesNone}, template{"env " + name, s.Env[name], carriesAll})
	}

	if s.Git != nil {
		out = append(out, template{"git.host", s.Git.Host, carriesNone}, template{"git.username", s.Git.Username, carriesNone})
	}

	if s.MCP != nil {
		out = append(out, template{"mcp.url", s.MCP.URL, carriesNone})
		out = append(out, headerTemplates("mcp.headers", s.MCP.Headers, carriesNone)...)

		for _, word := range s.MCP.Command {
			out = append(out, template{"mcp.command", word, carriesNone})
		}
	}

	if s.Test != nil {
		out = append(out, template{"test.url", s.Test.URL, carriesNone})
		out = append(out, headerTemplates("test.headers", s.Test.Headers, carriesStored)...)

		if s.Test.Basic != nil {
			out = append(out, template{"test.basic.username", s.Test.Basic.Username, carriesNone},
				template{"test.basic.password", s.Test.Basic.Password, carriesStored})
		}
	}

	return out
}

func headerTemplates(where string, headers map[string]string, values carries) []template {
	out := make([]template, 0, 2*len(headers))

	for _, name := range slices.Sorted(maps.Keys(headers)) {
		out = append(out, template{where + " name", name, carriesNone}, template{where + " " + name, headers[name], values})
	}

	return out
}

// envRefs are the variables an MCP header value names as ${NAME}.
func envRefs(s string) []string {
	var out []string
	for _, m := range envRef.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}

	return out
}

// checkEnvRefs refuses an MCP header's ${NAME} that is not one of the kind's own env variables.
func (k ConnectionKind) checkEnvRefs() []error {
	if k.Spec.MCP == nil {
		return nil
	}

	var errs []error

	for _, header := range slices.Sorted(maps.Keys(k.Spec.MCP.Headers)) {
		for _, name := range envRefs(k.Spec.MCP.Headers[header]) {
			if _, ok := k.Spec.Env[name]; !ok {
				errs = append(errs, fmt.Errorf("mcp.headers %s: ${%s} is not one of the kind's env variables: %w", header, name, ErrInvalid))
			}
		}
	}

	return errs
}

// Check enforces a kind's rules beyond its schema: a valid name, unique field names, no default
// on a secret field, templates of the closed grammar naming declared fields (minted values only
// with Mint), secret fields only where secrets may go, env names a box may get and a git password
// from one of them. The error wraps ErrInvalid and lists every broken rule.
func (k ConnectionKind) Check() error {
	errs := []error{CheckConnectionName(k.Name)}

	if len(k.Spec.Fields) > MaxKindFields {
		errs = append(errs, fmt.Errorf("a kind has at most %d fields: %w", MaxKindFields, ErrInvalid))
	}

	seen := map[string]bool{}

	for _, f := range k.Spec.Fields {
		switch {
		case !fieldNamePattern.MatchString(f.Name):
			errs = append(errs, fmt.Errorf("field %q: a field name is a lowercase letter, then letters, digits or underscores: %w", f.Name, ErrInvalid))
		case seen[f.Name]:
			errs = append(errs, fmt.Errorf("field %q is declared twice: %w", f.Name, ErrInvalid))
		case f.Secret && f.Default != "":
			errs = append(errs, fmt.Errorf("field %q: a secret field has no default: %w", f.Name, ErrInvalid))
		}

		seen[f.Name] = true
	}

	for _, t := range k.templates() {
		errs = append(errs, k.checkTemplate(t))
	}

	errs = append(errs, k.checkEnvRefs()...)

	for name := range k.Spec.Env {
		if len(mustRefs(name)) == 0 {
			errs = append(errs, checkEnvName(name))
		}
	}

	if g := k.Spec.Git; g != nil {
		if _, ok := k.Spec.Env[g.PasswordEnv]; !ok {
			errs = append(errs, fmt.Errorf("git.passwordEnv %q is not one of the kind's env names: %w", g.PasswordEnv, ErrInvalid))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("connection kind %q: %w", k.Name, err)
	}

	return nil
}

// mustRefs is refs for a template Check already parsed (nil when it does not parse).
func mustRefs(s string) []ref {
	out, _ := refs(s)

	return out
}

func (k ConnectionKind) checkTemplate(t template) error {
	rs, err := refs(t.s)
	if err != nil {
		return fmt.Errorf("%s: %w", t.where, err)
	}

	var errs []error

	for _, r := range rs {
		if r.minted {
			switch {
			case k.Spec.Mint == "":
				errs = append(errs, fmt.Errorf("%s: {{minted.%s}} in a kind that mints nothing: %w", t.where, r.name, ErrInvalid))
			case t.carries != carriesAll:
				errs = append(errs, fmt.Errorf("%s: minted value {{minted.%s}} may appear only in env values: %w", t.where, r.name, ErrInvalid))
			}

			continue
		}

		f, ok := k.Field(r.name)

		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: {{%s}} names no field of the kind: %w", t.where, r.name, ErrInvalid))
		case f.Secret && t.carries == carriesNone:
			errs = append(errs, fmt.Errorf("%s: secret field {{%s}} may appear only in env values, a test's headers and its basic password (an MCP header names an env variable, ${NAME}): %w",
				t.where, r.name, ErrInvalid))
		}
	}

	return errors.Join(errs...)
}

// CheckValues enforces a connection's values against its kind: fields holds the non-secret
// values an admin set, secrets the names of the secret fields that are set. Every name is a field
// of the kind, of the right sort; every required field has a value (or a default); and the env
// names the values make are ones a box may get; and a kind with a git login is tested on its git
// host (checkTestHost). The error wraps ErrInvalid; it names fields and hosts, never a secret.
func (k ConnectionKind) CheckValues(fields map[string]string, secrets []string) error {
	errs := k.checkValueNames(fields, secrets)

	for _, f := range k.Spec.Fields {
		set := fields[f.Name] != "" || f.Default != ""
		if f.Secret {
			set = slices.Contains(secrets, f.Name)
		}

		if f.Required && !set {
			errs = append(errs, fmt.Errorf("field %q is required: %w", f.Name, ErrFieldRequired))
		}
	}

	for name := range k.Spec.Env {
		if rendered, ok, err := k.expand(name, fields, nil); err == nil && ok && rendered != name {
			errs = append(errs, checkEnvName(rendered))
		}
	}

	errs = append(errs, k.checkTestHost(fields))

	return errors.Join(errs...)
}

// checkTestHost refuses a test that would send a connection's credentials off the host its git
// login names: a kind that signs git in to a host is tested on that host or its api. subdomain (a
// GitHub Enterprise connection left with github.com's API URL is refused, not tested there).
// values are the connection's, defaults filling empty ones; a test URL or git host that does not
// render (a test or secret field they need is missing) is left to the test. Neither carries a
// secret (Check), so the error names them.
func (k ConnectionKind) checkTestHost(values map[string]string) error {
	t, g := k.Spec.Test, k.Spec.Git
	if t == nil || g == nil {
		return nil
	}

	host, hostOK, hostErr := k.expand(g.Host, values, nil)
	raw, urlOK, urlErr := k.expand(t.URL, values, nil)
	u, parseErr := url.Parse(raw)

	if rendered := hostOK && urlOK && errors.Join(hostErr, urlErr, parseErr) == nil; !rendered {
		return nil
	}

	got, want := strings.ToLower(u.Host), strings.ToLower(host)
	if got == want || got == "api."+want {
		return nil
	}

	var from []string
	for _, r := range mustRefs(t.URL) {
		from = append(from, r.name)
	}

	return fmt.Errorf("test.url (from %s) is on %s, not on %s or api.%s, the host git signs in to: set it to that host's API: %w",
		strings.Join(from, ", "), got, want, want, ErrInvalid)
}

// checkValueNames refuses names that are not fields of k of the right sort.
func (k ConnectionKind) checkValueNames(fields map[string]string, secrets []string) []error {
	var errs []error

	for _, name := range slices.Sorted(maps.Keys(fields)) {
		switch f, ok := k.Field(name); {
		case !ok:
			errs = append(errs, fmt.Errorf("kind %s has no field %q: %w", k.Name, name, ErrFieldUnknown))
		case f.Secret:
			errs = append(errs, fmt.Errorf("field %q is secret: set it as a secret: %w", name, ErrFieldUnknown))
		}
	}

	for _, name := range secrets {
		if f, ok := k.Field(name); !ok || !f.Secret {
			errs = append(errs, fmt.Errorf("kind %s has no secret field %q: %w", k.Name, name, ErrFieldUnknown))
		}
	}

	return errs
}

// CheckHeldPlain refuses k as the new kind of conns, its connections, when one of them holds a
// field k makes secret as a plain value: the value would stay readable by every member and win
// over a sealed one. It names fields and connections, never a value; the connection must clear
// the field first.
func (k ConnectionKind) CheckHeldPlain(conns []Connection) error {
	var errs []error

	for _, c := range conns {
		var held []string

		for _, name := range slices.Sorted(maps.Keys(c.Fields)) {
			if f, ok := k.Field(name); ok && f.Secret {
				held = append(held, name)
			}
		}

		if len(held) > 0 {
			errs = append(errs, fmt.Errorf("connection %s holds %s as a plain value, which kind %s makes secret: clear it on the connection first",
				c.Name, strings.Join(held, ", "), k.Name))
		}
	}

	return errors.Join(errs...)
}

// WithDefaults is a copy of values (fields and secrets by field name) with k's defaults filling
// the empty ones: the values a connection of k has, as its templates and its minter see them.
func (k ConnectionKind) WithDefaults(values map[string]string) map[string]string {
	out := maps.Clone(values)
	if out == nil {
		out = map[string]string{}
	}

	for _, f := range k.Spec.Fields {
		if out[f.Name] == "" && f.Default != "" {
			out[f.Name] = f.Default
		}
	}

	return out
}

// Render makes what a run's box gets from a connection of k with values (fields and secrets by
// field name; a kind's defaults fill empty ones) and minted (the values a kind with Mint minted
// for the run). Errors name fields and variables, never values.
func (k ConnectionKind) Render(values, minted map[string]string) (RunConnection, error) {
	s := k.Spec

	env, err := k.expandMap(s.Env, values, minted)
	if err != nil {
		return RunConnection{}, fmt.Errorf("render %s: env: %w", k.Name, err)
	}

	for _, name := range slices.Sorted(maps.Keys(env)) {
		if err := checkEnvName(name); err != nil {
			return RunConnection{}, fmt.Errorf("render %s: %w", k.Name, err)
		}
	}

	out := RunConnection{Env: env, Docs: s.Docs}
	if s.CLI != nil {
		out.CLI = s.CLI.Tool
	}

	if out.Git, err = k.renderGit(values, minted, env); err != nil {
		return RunConnection{}, fmt.Errorf("render %s: %w", k.Name, err)
	}

	if out.MCP, err = k.renderMCP(values, minted, env); err != nil {
		return RunConnection{}, fmt.Errorf("render %s: %w", k.Name, err)
	}

	return out, nil
}

// GitLogin is a connection's rendered git login: the host it serves, in lower case, and the
// password its env variable holds. A code host's connection is chosen by that host and calls the
// host's API with that password (spec 4 §4.7, §5.5). It fails, naming the connection, when the
// kind has no git login or the connection's values leave it out; errors never hold a value.
func (rc ResolvedConnection) GitLogin() (host, password string, err error) {
	run, err := rc.Kind.Render(rc.Values, nil)
	if err != nil {
		return "", "", fmt.Errorf("connection %s: %w", rc.Connection.Name, err)
	}

	if run.Git == nil || run.Env[run.Git.PasswordEnv] == "" {
		return "", "", fmt.Errorf("connection %s has no git login: %w", rc.Connection.Name, ErrInvalid)
	}

	return strings.ToLower(run.Git.Host), run.Env[run.Git.PasswordEnv], nil
}

// SecretValues are the values of rc's secret fields that are set, in its kind's order: what a
// scrubber removes from whatever a use of rc reports.
func (rc ResolvedConnection) SecretValues() []string {
	var out []string

	for _, f := range rc.Kind.Spec.Fields {
		if v := rc.Values[f.Name]; f.Secret && v != "" {
			out = append(out, v)
		}
	}

	return out
}

func (k ConnectionKind) renderGit(values, minted, env map[string]string) (*KindGit, error) {
	g := k.Spec.Git
	if g == nil {
		return nil, nil
	}

	host, hostOK, err := k.expand(g.Host, values, minted)
	if err != nil {
		return nil, err
	}

	user, userOK, err := k.expand(g.Username, values, minted)
	if err != nil {
		return nil, err
	}

	if _, ok := env[g.PasswordEnv]; !hostOK || !userOK || !ok {
		return nil, nil
	}

	return &KindGit{Host: host, Username: user, PasswordEnv: g.PasswordEnv}, nil
}

func (k ConnectionKind) renderMCP(values, minted, env map[string]string) (*KindMCP, error) {
	m := k.Spec.MCP
	if m == nil {
		return nil, nil
	}

	out, err := k.renderRemote(values, minted, env)
	if err != nil {
		return nil, err
	}

	for _, word := range m.Command {
		w, ok, err := k.expand(word, values, minted)
		if err != nil {
			return nil, err
		}

		if !ok {
			out.Command = nil

			break
		}

		out.Command = append(out.Command, w)
	}

	if out.URL == "" && len(out.Command) == 0 {
		return nil, nil
	}

	return &out, nil
}

// renderRemote is the remote part of k's MCP server: its URL and headers, or nothing. A header
// naming an env variable the run does not get is left out.
func (k ConnectionKind) renderRemote(values, minted, env map[string]string) (KindMCP, error) {
	m := k.Spec.MCP

	u, ok, err := k.expand(m.URL, values, minted)
	if err != nil || !ok || u == "" {
		return KindMCP{}, err
	}

	if p, err := url.Parse(u); err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" {
		return KindMCP{}, fmt.Errorf("mcp.url is not an http or https URL: %w", ErrInvalid)
	}

	headers, err := k.expandMap(m.Headers, values, minted)
	if err != nil {
		return KindMCP{}, fmt.Errorf("mcp.headers: %w", err)
	}

	maps.DeleteFunc(headers, func(_, v string) bool {
		return slices.ContainsFunc(envRefs(v), func(name string) bool { _, ok := env[name]; return !ok })
	})

	return KindMCP{URL: u, Headers: headers}, nil
}

// RenderTest makes k's test request for a connection with values (fields and secrets by field
// name; defaults fill empty ones): nil when k has no test. A test that names a field without a
// value, a minted value (a kind that mints is not testable from goald) or a URL that is not http or
// https is an error naming the field, never a value.
func (k ConnectionKind) RenderTest(values map[string]string) (*KindTest, error) {
	t := k.Spec.Test
	if t == nil {
		return nil, nil
	}

	out := KindTest{Method: t.Method, Expect: t.Expect, Headers: make(map[string]string, len(t.Headers))}

	var err error
	if out.URL, err = k.mustExpand("test.url", t.URL, values); err != nil {
		return nil, err
	}

	if err := k.checkTestURL(out.URL, values); err != nil {
		return nil, fmt.Errorf("render %s: %w", k.Name, err)
	}

	for _, name := range slices.Sorted(maps.Keys(t.Headers)) {
		header, err := k.mustExpand("test.headers name", name, values)
		if err != nil {
			return nil, err
		}

		if out.Headers[header], err = k.mustExpand("test.headers "+name, t.Headers[name], values); err != nil {
			return nil, err
		}
	}

	if b := t.Basic; b != nil {
		user, err := k.mustExpand("test.basic.username", b.Username, values)
		if err != nil {
			return nil, err
		}

		password, err := k.mustExpand("test.basic.password", b.Password, values)
		if err != nil {
			return nil, err
		}

		out.Basic = &KindBasicAuth{Username: user, Password: password}
	}

	return &out, nil
}

// checkTestURL refuses a rendered test URL that is not http or https, or is off the host the
// kind's git login names (checkTestHost).
func (k ConnectionKind) checkTestURL(rendered string, values map[string]string) error {
	if p, err := url.Parse(rendered); err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" {
		return fmt.Errorf("test.url is not an http or https URL: %w", ErrInvalid)
	}

	return k.checkTestHost(values)
}

// mustExpand fills s's templates for a test, which needs every value it names.
func (k ConnectionKind) mustExpand(where, s string, values map[string]string) (string, error) {
	for _, r := range mustRefs(s) {
		if r.minted {
			return "", fmt.Errorf("render %s: %s names minted value %q: %w", k.Name, where, r.name, ErrInvalid)
		}
	}

	out, ok, err := k.expand(s, values, nil)

	switch {
	case err != nil:
		return "", fmt.Errorf("render %s: %s: %w", k.Name, where, err)
	case !ok:
		return "", fmt.Errorf("render %s: %s names a field that has no value: %w", k.Name, where, ErrFieldRequired)
	default:
		return out, nil
	}
}

// expandMap expands keys and values of m, leaving out every entry whose key or value names a
// field without a value.
func (k ConnectionKind) expandMap(m, values, minted map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(m))

	for _, key := range slices.Sorted(maps.Keys(m)) {
		name, nameOK, err := k.expand(key, values, minted)
		if err != nil {
			return nil, err
		}

		value, valueOK, err := k.expand(m[key], values, minted)
		if err != nil {
			return nil, err
		}

		if nameOK && valueOK {
			out[name] = value
		}
	}

	return out, nil
}

// expand fills s's templates. ok is false when s names a field with no value and no default; a
// minted value that is missing is an error.
func (k ConnectionKind) expand(s string, values, minted map[string]string) (out string, ok bool, err error) {
	rs, err := refs(s)
	if err != nil {
		return "", false, err
	}

	ok = true

	out = templateRef.ReplaceAllStringFunc(s, func(string) string {
		r := rs[0]
		rs = rs[1:]

		if r.minted {
			v, found := minted[r.name]
			if !found {
				err = errors.Join(err, fmt.Errorf("minted value %q is missing", r.name))
			}

			return v
		}

		v := values[r.name]
		if f, _ := k.Field(r.name); v == "" {
			v = f.Default
		}

		if v == "" {
			ok = false
		}

		return v
	})

	return out, ok, err
}

// ConnectionOutline is what a run's box gets from a connection, without any value: the names of
// its env variables, sorted, its git login, its MCP server (whose headers name env variables, never
// values) and its CLI tool. None of these can hold a secret (Check), so a run's bundle carries them.
type ConnectionOutline struct {
	Env []string
	// Git is nil without a git login, MCP without an MCP server.
	Git *KindGit
	MCP *KindMCP
	// CLI is a mise tool spec, or "".
	CLI string
}

// GitHost is the host of the outline's git login, "" without one.
func (o ConnectionOutline) GitHost() string {
	if o.Git == nil {
		return ""
	}

	return o.Git.Host
}

// outlinePlaceholder stands in for the secret and minted values Outline never sees.
const outlinePlaceholder = "set"

// Outline renders k for c from what members may see of it, its non-secret fields and which secret
// fields are set, with a placeholder for every secret and minted value, so a definition's checks
// know which variables and git host a run would get (spec 4 §4.4) and the gateway can describe a
// run's connections in its bundle (§5.9). A connection its kind cannot render outlines as nothing.
func (k ConnectionKind) Outline(c Connection) ConnectionOutline {
	values := maps.Clone(c.Fields)
	if values == nil {
		values = map[string]string{}
	}

	for _, name := range c.SecretFields {
		values[name] = outlinePlaceholder
	}

	minted := map[string]string{}

	for _, t := range k.templates() {
		for _, r := range mustRefs(t.s) {
			if r.minted {
				minted[r.name] = outlinePlaceholder
			}
		}
	}

	rc, err := k.Render(values, minted)
	if err != nil {
		return ConnectionOutline{}
	}

	return ConnectionOutline{Env: slices.Sorted(maps.Keys(rc.Env)), Git: rc.Git, MCP: rc.MCP, CLI: rc.CLI}
}

// EngineActor is who uses a connection for a run or a knowledge source's sync in the audit log.
var EngineActor = Actor{Kind: ActorSystem, ID: "engine", Name: "engine"}

// ConnectionUse is what connections are opened for, which their connection.used audit rows
// record: one run, a person's merge of an item's pull requests or a knowledge source's sync.
type ConnectionUse struct {
	// By uses them: the zero value is EngineActor (runs and syncs); a merge is the person's.
	By Actor
	// Run is the run that receives them, Item the item whose pull requests are merged and
	// Source the knowledge source synced; each is empty when the use is another.
	Run    RunID
	Item   ItemID
	Source string
}

// Actor is who uses the connections: By, or EngineActor when unset.
func (u ConnectionUse) Actor() Actor {
	if u.By == (Actor{}) {
		return EngineActor
	}

	return u.By
}

// AuditData is what the connection.used row of connection name records: the name, and the run,
// the item or the source, whichever the use has.
func (u ConnectionUse) AuditData(name string) map[string]any {
	out := map[string]any{"name": name}

	for key, v := range map[string]string{"run_id": string(u.Run), "item_id": string(u.Item), "source": u.Source} {
		if v != "" {
			out[key] = v
		}
	}

	return out
}

// ConnectionAAD is the associated data a connection's sealed secrets are bound to (spec 4 §4.3):
// its organization, its id and the key id, so a ciphertext copied to another row, organization or
// key id does not open.
func ConnectionAAD(org OrgID, id ConnectionID, keyID string) []byte {
	return []byte("goal/connection/v1|" + string(org) + "|" + string(id) + "|" + keyID)
}

// Audit actions of connections and their kinds (spec 4 §12).
const (
	AuditConnectionCreated        = "connection.created"
	AuditConnectionUpdated        = "connection.updated"
	AuditConnectionSecretReplaced = "connection.secret_replaced"
	AuditConnectionDeleted        = "connection.deleted"
	AuditConnectionTested         = "connection.tested"
	AuditConnectionUsed           = "connection.used"
	AuditConnectionKindCreated    = "connection_kind.created"
	AuditConnectionKindUpdated    = "connection_kind.updated"
	AuditConnectionKindDeleted    = "connection_kind.deleted"
)

// Audit target kinds of connections and kinds.
const (
	AuditTargetConnection     = "connection"
	AuditTargetConnectionKind = "connection_kind"
)
