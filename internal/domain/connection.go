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
// The JSON names are the kind file's (schema/connection-kind.schema.json).
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
	// SecretFields names the secret fields that are set, sorted.
	SecretFields []string
	CreatedBy    Actor
	CreatedAt    time.Time
	UpdatedBy    Actor
	UpdatedAt    time.Time
	LastUsedAt   *time.Time
	LastTest     *ConnectionTest
}

// ConnectionTest is the outcome of a connection's test.
type ConnectionTest struct {
	At    time.Time
	OK    bool
	Error string
}

// NewConnection is a connection to create: its kind's non-secret values in Fields, its secret
// ones in Secrets.
type NewConnection struct {
	Name, Kind, Description string
	Fields, Secrets         map[string]string
}

// ConnectionPatch changes a connection. Name, when set, renames it (its sealed values stay: they
// are bound to its id); Description, when set, replaces it; Fields, when not nil, replaces every
// non-secret value; Secrets replaces the named secret values only, an empty value clearing an
// optional one. The kind never changes.
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
	// Values are the connection's fields and secrets by field name (defaults are applied by Render).
	Values map[string]string
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

// template is one templated string of a kind and where it may name secret fields.
type template struct {
	where   string
	s       string
	secrets bool
}

// templates are every templated string of k with whether it may name a secret field: only env
// values, test headers and the test's basic password carry secrets, so none reaches a URL, a
// command line, the run's bundle (MCP headers name env variables instead), a log or a prompt.
func (k ConnectionKind) templates() []template {
	s := k.Spec

	var out []template

	for _, name := range slices.Sorted(maps.Keys(s.Env)) {
		out = append(out, template{"env name", name, false}, template{"env " + name, s.Env[name], true})
	}

	if s.Git != nil {
		out = append(out, template{"git.host", s.Git.Host, false}, template{"git.username", s.Git.Username, false})
	}

	if s.MCP != nil {
		out = append(out, template{"mcp.url", s.MCP.URL, false})
		out = append(out, headerTemplates("mcp.headers", s.MCP.Headers, false)...)

		for _, word := range s.MCP.Command {
			out = append(out, template{"mcp.command", word, false})
		}
	}

	if s.Test != nil {
		out = append(out, template{"test.url", s.Test.URL, false})
		out = append(out, headerTemplates("test.headers", s.Test.Headers, true)...)

		if s.Test.Basic != nil {
			out = append(out, template{"test.basic.username", s.Test.Basic.Username, false},
				template{"test.basic.password", s.Test.Basic.Password, true})
		}
	}

	return out
}

func headerTemplates(where string, headers map[string]string, secrets bool) []template {
	out := make([]template, 0, 2*len(headers))

	for _, name := range slices.Sorted(maps.Keys(headers)) {
		out = append(out, template{where + " name", name, false}, template{where + " " + name, headers[name], secrets})
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
			if k.Spec.Mint == "" {
				errs = append(errs, fmt.Errorf("%s: {{minted.%s}} in a kind that mints nothing: %w", t.where, r.name, ErrInvalid))
			}

			continue
		}

		f, ok := k.Field(r.name)

		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: {{%s}} names no field of the kind: %w", t.where, r.name, ErrInvalid))
		case f.Secret && !t.secrets:
			errs = append(errs, fmt.Errorf("%s: secret field {{%s}} may appear only in env values, a test's headers and its basic password (an MCP header names an env variable, ${NAME}): %w",
				t.where, r.name, ErrInvalid))
		}
	}

	return errors.Join(errs...)
}

// CheckValues enforces a connection's values against its kind: fields holds the non-secret
// values an admin set, secrets the names of the secret fields that are set. Every name is a field
// of the kind, of the right sort; every required field has a value (or a default); and the env
// names the values make are ones a box may get. The error wraps ErrInvalid; it names fields,
// never values.
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

	return errors.Join(errs...)
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

	if p, err := url.Parse(out.URL); err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" {
		return nil, fmt.Errorf("render %s: test.url is not an http or https URL: %w", k.Name, ErrInvalid)
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
// its env variables, sorted, and the host of its git login ("" without one).
type ConnectionOutline struct {
	Env     []string
	GitHost string
}

// outlinePlaceholder stands in for the secret and minted values Outline never sees.
const outlinePlaceholder = "set"

// Outline renders k for c from what members may see of it, its non-secret fields and which secret
// fields are set, with a placeholder for every secret and minted value, so a definition's checks
// know which variables and git host a run would get (spec 4 §4.4). A connection its kind cannot
// render outlines as nothing.
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

	out := ConnectionOutline{Env: slices.Sorted(maps.Keys(rc.Env))}
	if rc.Git != nil {
		out.GitHost = rc.Git.Host
	}

	return out
}

// EngineActor is who uses a connection for a run in the audit log (connection.used names the run).
var EngineActor = Actor{Kind: ActorSystem, ID: "engine", Name: "engine"}

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
