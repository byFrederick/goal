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
// secret of the organization; applying a template changes nothing in the organization. Its
// Parameters' otherwise lines may also refer to a {{parameter}}, which Resolve replaces.
type ToolTemplate struct {
	Name        string
	Title       string
	Description string
	// Secrets are the template's secret slots, in order.
	Secrets []TemplateSecret
	// Parameters are the template's non-secret parameters, in order.
	Parameters []TemplateParameter
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

// TemplateParameter is a non-secret value a template takes, such as a host: the {{name}} its
// otherwise lines refer to, how to ask for it, its default, and the lines used instead of the
// template's own when its value is not the default.
type TemplateParameter struct {
	Name        string
	Title       string
	Description string
	Default     string
	Otherwise   *TemplateLines
}

// TemplateLines are the lines a parameter's otherwise puts in place of the template's own: each
// field that is set replaces the template's whole field.
type TemplateLines struct {
	Tools []string
	Env   map[string]string
	Files map[string]string
	MCP   map[string]TemplateMCP
	Test  *TemplateTest
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

// ToolModes are the tool modes an environment file's mcp.<server>.tools takes (ADR 0037 decision 8).
var ToolModes = []string{"allow", "ask", "off"}

var (
	// slotReference is one {{slot}} in a template's value; its group is the slot.
	slotReference = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)
	// envReference is one ${VAR} in a template's test.
	envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	slotPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	// parameterValue is a parameter's value: a host name of letters, digits, dots and hyphens,
	// with an optional :port, so it can't change a URL's scheme or path.
	parameterValue = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?(:[0-9]{1,5})?$`)
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
// whose ${VAR} references name the template's plain env values. Parameters are named like slots,
// never like one of them, each once with a title and a default; at most one has otherwise lines,
// which follow the same rules and may refer to a {{parameter}}, while the template's own lines
// never do. The error wraps ErrInvalid and lists every broken rule.
func (t ToolTemplate) Check() error {
	var errs []error

	add := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf(format+": %w", append(a, ErrInvalid)...))
	}

	if !namePattern.MatchString(t.Name) {
		add("template name %q must be 3 to 40 lowercase letters, digits or dashes", t.Name)
	}

	if strings.TrimSpace(t.Title) == "" {
		add("template %s needs a title", t.Name)
	}

	if strings.TrimSpace(t.Description) == "" || strings.Contains(t.Description, "\n") {
		add("template %s needs a one-line description", t.Name)
	}

	slots := t.checkSlots(add)
	params := t.checkParameters(add, slots)

	refs := func(where, value string) {
		for _, slot := range Slots(value) {
			switch {
			case params[slot]:
				add("template %s: %s refers to the parameter {{%s}}, which only otherwise lines may", t.Name, where, slot)
			case !slots[slot]:
				add("template %s: %s refers to {{%s}}, which is none of its secrets", t.Name, where, slot)
			}
		}
	}

	t.checkAdds(add, refs)

	for _, name := range slices.Sorted(maps.Keys(t.MCP)) {
		errs = append(errs, t.checkMCP(name, refs)...)
	}

	if t.Test != nil {
		errs = append(errs, t.checkTest(refs)...)
	}

	for _, p := range t.Parameters {
		if p.Otherwise != nil {
			errs = append(errs, t.checkOtherwise(p, slots, params)...)
		}
	}

	return errors.Join(errs...)
}

// checkParameters reports each broken parameter through add and returns the parameters declared.
func (t ToolTemplate) checkParameters(add func(format string, a ...any), slots map[string]bool) map[string]bool {
	params := map[string]bool{}
	otherwise := 0

	for _, p := range t.Parameters {
		switch {
		case !slotPattern.MatchString(p.Name):
			add("template %s: parameter %q must be lowercase letters, digits or underscores", t.Name, p.Name)
		case slots[p.Name]:
			add("template %s: parameter %s is also a secret slot", t.Name, p.Name)
		case params[p.Name]:
			add("template %s: parameter %s is declared twice", t.Name, p.Name)
		case strings.TrimSpace(p.Title) == "":
			add("template %s: parameter %s needs a title", t.Name, p.Name)
		case !parameterValue.MatchString(p.Default):
			add("template %s: parameter %s needs a default of letters, digits, dots and hyphens, with an optional :port", t.Name, p.Name)
		}

		params[p.Name] = true

		if p.Otherwise != nil {
			otherwise++
		}
	}

	if otherwise > 1 {
		add("template %s: only one parameter may have otherwise lines", t.Name)
	}

	return params
}

// checkOtherwise checks the lines p's otherwise replaces, as the template has them with those
// lines; they may refer to a slot or a parameter.
func (t ToolTemplate) checkOtherwise(p TemplateParameter, slots, params map[string]bool) []error {
	var errs []error

	add := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf(format+": %w", append(a, ErrInvalid)...))
	}

	refs := func(where, value string) {
		for _, ref := range Slots(value) {
			if !slots[ref] && !params[ref] {
				add("template %s: otherwise of %s: %s refers to {{%s}}, which is neither a secret nor a parameter", t.Name, p.Name, where, ref)
			}
		}
	}

	o := *p.Otherwise
	alt := t.with(o)

	ToolTemplate{Name: t.Name, Tools: o.Tools, Env: o.Env, Files: o.Files}.checkAdds(add, refs)

	for _, name := range slices.Sorted(maps.Keys(o.MCP)) {
		errs = append(errs, alt.checkMCP(name, refs)...)
	}

	// A test is checked against the env it reads, so a new env checks the test again.
	if alt.Test != nil && (o.Test != nil || o.Env != nil) {
		errs = append(errs, alt.checkTest(refs)...)
	}

	return errs
}

// Resolve is the template with its parameters applied: each takes its value from params, or its
// default; one whose value is not its default (ignoring case) puts its otherwise lines in place of
// the template's own, and every {{parameter}} becomes its value. A parameter the template does not
// have, or a value that is not a host name with an optional :port, is an error wrapping
// ErrInvalid. With every value the default, the template is unchanged.
func (t ToolTemplate) Resolve(params map[string]string) (ToolTemplate, error) {
	for _, name := range slices.Sorted(maps.Keys(params)) {
		if !slices.ContainsFunc(t.Parameters, func(p TemplateParameter) bool { return p.Name == name }) {
			return ToolTemplate{}, fmt.Errorf("template %s has no parameter %q: %w", t.Name, name, ErrInvalid)
		}
	}

	out := t
	values := map[string]string{}

	for _, p := range t.Parameters {
		v, ok := params[p.Name]
		if !ok {
			v = p.Default
		}

		if !parameterValue.MatchString(v) {
			return ToolTemplate{}, fmt.Errorf("template %s: parameter %s takes letters, digits, dots and hyphens, with an optional :port: %w",
				t.Name, p.Name, ErrInvalid)
		}

		values[p.Name] = v

		if p.Otherwise != nil && !strings.EqualFold(v, p.Default) {
			out = out.with(*p.Otherwise)
		}
	}

	return out.substitute(values), nil
}

// with is t with each field o sets in place of its own.
func (t ToolTemplate) with(o TemplateLines) ToolTemplate {
	if o.Tools != nil {
		t.Tools = o.Tools
	}

	if o.Env != nil {
		t.Env = o.Env
	}

	if o.Files != nil {
		t.Files = o.Files
	}

	if o.MCP != nil {
		t.MCP = o.MCP
	}

	if o.Test != nil {
		t.Test = o.Test
	}

	return t
}

// substitute is t with each {{name}} of values replaced by its value, in copies of its lines.
func (t ToolTemplate) substitute(values map[string]string) ToolTemplate {
	sub := func(v string) string {
		return slotReference.ReplaceAllStringFunc(v, func(ref string) string {
			if value, ok := values[slotReference.FindStringSubmatch(ref)[1]]; ok {
				return value
			}

			return ref
		})
	}

	subAll := func(vs []string) []string {
		if vs == nil {
			return nil
		}

		out := make([]string, len(vs))
		for i, v := range vs {
			out[i] = sub(v)
		}

		return out
	}

	subMap := func(m map[string]string) map[string]string {
		if m == nil {
			return nil
		}

		out := make(map[string]string, len(m))
		for k, v := range m {
			out[k] = sub(v)
		}

		return out
	}

	out := t
	out.Tools, out.Env, out.Files = subAll(t.Tools), subMap(t.Env), subMap(t.Files)

	if t.MCP != nil {
		out.MCP = make(map[string]TemplateMCP, len(t.MCP))
		for name, m := range t.MCP {
			out.MCP[name] = TemplateMCP{
				URL: sub(m.URL), Headers: subMap(m.Headers), Command: subAll(m.Command), Env: subMap(m.Env), Tools: maps.Clone(m.Tools),
			}
		}
	}

	if t.Test != nil {
		tt := *t.Test
		tt.URL, tt.Headers = sub(tt.URL), subMap(tt.Headers)

		if b := tt.Basic; b != nil {
			tt.Basic = &TemplateBasicAuth{Username: sub(b.Username), Password: sub(b.Password)}
		}

		out.Test = &tt
	}

	return out
}

// checkSlots reports each broken slot through add and returns the slots declared.
func (t ToolTemplate) checkSlots(add func(format string, a ...any)) map[string]bool {
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

	return slots
}

// checkAdds reports broken tools, env names and file paths through add, and their values'
// references through refs.
func (t ToolTemplate) checkAdds(add func(format string, a ...any), refs func(where, value string)) {
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
}

// checkMCP checks the template's MCP server name, reporting its references through refs.
func (t ToolTemplate) checkMCP(name string, refs func(where, value string)) []error {
	var errs []error

	add := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf(format+": %w", append(a, ErrInvalid)...))
	}
	m := t.MCP[name]

	if !namePattern.MatchString(name) {
		add("template %s: MCP server name %q must be 3 to 40 lowercase letters, digits or dashes", t.Name, name)
	}

	if msg := m.shape(); msg != "" {
		add("template %s: MCP server %s %s", t.Name, name, msg)
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

// shape is what is wrong with m's form, or "": it is remote (an https url and headers) or stdio
// (a command and env), never both.
func (m TemplateMCP) shape() string {
	switch remote, stdio := m.URL != "", len(m.Command) > 0; {
	case remote && stdio:
		return "is remote (url) or stdio (command), never both"
	case !remote && !stdio:
		return "needs a url or a command"
	case remote && (len(m.Env) > 0 || !strings.HasPrefix(m.URL, "https://")):
		return "is remote: it needs an https url and takes headers, not env"
	case stdio && len(m.Headers) > 0:
		return "is stdio: it takes env, not headers"
	default:
		return ""
	}
}

// checkTest checks the template's test, reporting its references through refs.
func (t ToolTemplate) checkTest(refs func(where, value string)) []error {
	var errs []error

	add := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf(format+": %w", append(a, ErrInvalid)...))
	}
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
