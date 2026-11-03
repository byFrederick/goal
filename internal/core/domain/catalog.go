package domain

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// ToolTemplate is one entry of the catalog goald ships (ADR 0037 decision 15): what Add a tool
// writes into an environment file of the draft for one MCP server, command-line tool or API.
// Its values are plain text or {{slot}} references to its Secrets, which the person fills with a
// secret of the organization; applying a template changes nothing in the organization.
type ToolTemplate struct {
	Name        string
	Title       string
	Description string
	// Secrets are the template's secret slots, in order.
	Secrets []TemplateSecret
	// Tools are mise tool specs, Env variables, Files paths (absolute or ~/) and MCP servers by
	// name, as an environment file has them.
	Tools []string
	Env   map[string]string
	Files map[string]string
	MCP   map[string]TemplateMCP
	// Test is the HTTP request that checks the template's secrets, or nil.
	Test *TemplateTest
}

// TemplateSecret is one secret slot of a template: the {{slot}} its values refer to, how to ask
// for it and the name a new secret for it is suggested to have.
type TemplateSecret struct {
	Slot        string
	Title       string
	Description string
	Suggest     string
}

// TemplateMCP is an MCP server a template adds: remote (URL and Headers) or stdio (Command and
// Env), never both, with its suggested tool modes (allow, ask or off by tool name or glob).
type TemplateMCP struct {
	URL     string
	Headers map[string]string
	Command []string
	Env     map[string]string
	Tools   map[string]string
}

// TemplateTest is a template's HTTP test request. Its URL, headers and basic authentication may
// hold {{slot}} references and ${VAR} references to the template's plain Env values; the test
// passes when the answer has the Expect status.
type TemplateTest struct {
	Method  string
	URL     string
	Headers map[string]string
	Basic   *TemplateBasicAuth
	Expect  int
}

// TemplateBasicAuth is a test's HTTP basic authentication.
type TemplateBasicAuth struct {
	Username string
	Password string
}

// RenderedTest is a template's test with its references replaced, ready to send.
type RenderedTest struct {
	Method  string
	URL     string
	Headers map[string]string
	Basic   *TemplateBasicAuth
	Expect  int
}

// The tool modes an environment file's mcp.<server>.tools takes (ADR 0037 decision 8).
var ToolModes = []string{"allow", "ask", "off"}

var (
	// slotReference is one {{slot}} in a template's value; its group is the slot.
	slotReference = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)
	// envReference is one ${VAR} in a template's test.
	envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	slotPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	testMethods  = []string{"GET", "HEAD", "POST"}
)

// Slots are the slots value refers to, in order, each once.
func Slots(value string) []string {
	var out []string

	for _, m := range slotReference.FindAllStringSubmatch(value, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}

	return out
}

// Check enforces a template's rules: a slug name, a title and a one-line description; slots of
// lowercase letters, digits and underscores, each with a title and a suggested secret name; every
// {{slot}} in env, files, MCP headers and env, and the test naming one of them; env names that a
// run may set; file paths that are absolute or start with ~/; MCP servers that are remote or
// stdio, never both, with modes allow, ask or off; and a test of GET, HEAD or POST to an https URL
// whose ${VAR} references name the template's plain env values. The error wraps ErrInvalid and
// lists every broken rule.
func (t ToolTemplate) Check() error {
	var errs []error

	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format+": %w", append(a, ErrInvalid)...)) }

	if !namePattern.MatchString(t.Name) {
		add("template name %q must be 3 to 40 lowercase letters, digits or dashes", t.Name)
	}

	if strings.TrimSpace(t.Title) == "" {
		add("template %s needs a title", t.Name)
	}

	if strings.TrimSpace(t.Description) == "" || strings.Contains(t.Description, "\n") {
		add("template %s needs a one-line description", t.Name)
	}

	slots := map[string]bool{}

	for _, s := range t.Secrets {
		switch {
		case !slotPattern.MatchString(s.Slot):
			add("template %s: slot %q must be lowercase letters, digits or underscores", t.Name, s.Slot)
		case slots[s.Slot]:
			add("template %s: slot %s is declared twice", t.Name, s.Slot)
		case strings.TrimSpace(s.Title) == "":
			add("template %s: slot %s needs a title", t.Name, s.Slot)
		case !namePattern.MatchString(s.Suggest):
			add("template %s: slot %s suggests %q, which is not a secret name", t.Name, s.Slot, s.Suggest)
		}

		slots[s.Slot] = true
	}

	refs := func(where, value string) {
		for _, slot := range Slots(value) {
			if !slots[slot] {
				add("template %s: %s refers to {{%s}}, which is none of its secrets", t.Name, where, slot)
			}
		}
	}

	for _, tool := range t.Tools {
		if tool == "" || strings.ContainsAny(tool, " \t\n") {
			add("template %s: tools: %q is not a mise tool spec", t.Name, tool)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(t.Env)) {
		if err := checkEnvName(name); err != nil {
			add("template %s: env %s: %v", t.Name, name, errors.Unwrap(err))
		}

		refs("env."+name, t.Env[name])
	}

	for _, p := range slices.Sorted(maps.Keys(t.Files)) {
		if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~/") {
			add("template %s: files: %q must be an absolute path or start with ~/", t.Name, p)
		}

		refs("files."+p, t.Files[p])
	}

	for _, name := range slices.Sorted(maps.Keys(t.MCP)) {
		errs = append(errs, t.checkMCP(name, refs)...)
	}

	if t.Test != nil {
		errs = append(errs, t.checkTest(refs)...)
	}

	return errors.Join(errs...)
}

// checkMCP checks the template's MCP server name, reporting its references through refs.
func (t ToolTemplate) checkMCP(name string, refs func(where, value string)) []error {
	var errs []error

	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format+": %w", append(a, ErrInvalid)...)) }
	m := t.MCP[name]

	if !namePattern.MatchString(name) {
		add("template %s: MCP server name %q must be 3 to 40 lowercase letters, digits or dashes", t.Name, name)
	}

	switch remote, stdio := m.URL != "", len(m.Command) > 0; {
	case remote && stdio:
		add("template %s: MCP server %s is remote (url) or stdio (command), never both", t.Name, name)
	case !remote && !stdio:
		add("template %s: MCP server %s needs a url or a command", t.Name, name)
	case remote && (len(m.Env) > 0 || !strings.HasPrefix(m.URL, "https://")):
		add("template %s: remote MCP server %s needs an https url and takes headers, not env", t.Name, name)
	case stdio && len(m.Headers) > 0:
		add("template %s: stdio MCP server %s takes env, not headers", t.Name, name)
	}

	for _, h := range slices.Sorted(maps.Keys(m.Headers)) {
		refs("mcp."+name+".headers."+h, m.Headers[h])
	}

	for _, e := range slices.Sorted(maps.Keys(m.Env)) {
		refs("mcp."+name+".env."+e, m.Env[e])
	}

	for _, tool := range slices.Sorted(maps.Keys(m.Tools)) {
		if !slices.Contains(ToolModes, m.Tools[tool]) {
			add("template %s: MCP server %s: tool %s has mode %q, not allow, ask or off", t.Name, name, tool, m.Tools[tool])
		}
	}

	return errs
}

// checkTest checks the template's test, reporting its references through refs.
func (t ToolTemplate) checkTest(refs func(where, value string)) []error {
	var errs []error

	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format+": %w", append(a, ErrInvalid)...)) }
	tt := t.Test

	if !slices.Contains(testMethods, tt.Method) {
		add("template %s: test method %q is not GET, HEAD or POST", t.Name, tt.Method)
	}

	if !strings.HasPrefix(tt.URL, "https://") && !strings.HasPrefix(tt.URL, "${") {
		add("template %s: test url %q must be https", t.Name, tt.URL)
	}

	if tt.Expect < 100 || tt.Expect > 599 {
		add("template %s: test expects status %d", t.Name, tt.Expect)
	}

	values := []string{tt.URL}
	refs("test.url", tt.URL)

	for _, h := range slices.Sorted(maps.Keys(tt.Headers)) {
		refs("test.headers."+h, tt.Headers[h])
		values = append(values, tt.Headers[h])
	}

	if tt.Basic != nil {
		refs("test.basic", tt.Basic.Username+tt.Basic.Password)
		values = append(values, tt.Basic.Username, tt.Basic.Password)
	}

	for _, v := range values {
		for _, m := range envReference.FindAllStringSubmatch(v, -1) {
			if plain, ok := t.Env[m[1]]; !ok || len(Slots(plain)) > 0 {
				add("template %s: the test refers to ${%s}, which is none of its plain env values", t.Name, m[1])
			}
		}
	}

	return errs
}

// RenderTest is the template's test with each {{slot}} replaced by secrets[slot] and each ${VAR}
// by env[VAR], or by the template's own env value when env has none. An error names the first
// slot without a value; it never holds one.
func (t ToolTemplate) RenderTest(secrets, env map[string]string) (RenderedTest, error) {
	if t.Test == nil {
		return RenderedTest{}, fmt.Errorf("template %s has no test: %w", t.Name, ErrInvalid)
	}

	var missing error

	render := func(v string) string {
		v = slotReference.ReplaceAllStringFunc(v, func(ref string) string {
			slot := slotReference.FindStringSubmatch(ref)[1]

			value, ok := secrets[slot]
			if !ok && missing == nil {
				missing = fmt.Errorf("template %s: no secret for {{%s}}: %w", t.Name, slot, ErrInvalid)
			}

			return value
		})

		return envReference.ReplaceAllStringFunc(v, func(ref string) string {
			name := envReference.FindStringSubmatch(ref)[1]
			if value, ok := env[name]; ok {
				return value
			}

			return t.Env[name]
		})
	}

	out := RenderedTest{Method: t.Test.Method, URL: render(t.Test.URL), Expect: t.Test.Expect, Headers: map[string]string{}}

	for h, v := range t.Test.Headers {
		out.Headers[h] = render(v)
	}

	if b := t.Test.Basic; b != nil {
		out.Basic = &TemplateBasicAuth{Username: render(b.Username), Password: render(b.Password)}
	}

	if missing != nil {
		return RenderedTest{}, missing
	}

	if u, err := url.Parse(out.URL); err != nil || u.Scheme != "https" || u.Host == "" {
		return RenderedTest{}, fmt.Errorf("template %s: the test's url is not an https URL once rendered: %w", t.Name, ErrInvalid)
	}

	return out, nil
}

// String shows r without its headers' and basic authentication's values.
func (r RenderedTest) String() string {
	return fmt.Sprintf("RenderedTest{%s %s headers:%v}", r.Method, r.URL, slices.Sorted(maps.Keys(r.Headers)))
}

// GoString is String, so %#v hides the values too.
func (r RenderedTest) GoString() string { return r.String() }
