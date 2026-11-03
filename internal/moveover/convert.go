// Package moveover converts every organization from connections and credentials to secrets and
// environment files, once, with no person needed (ADR 0037 decision 19). Convert is the pure part:
// a published version's files and the organization's opened connections in, the converted files,
// the connection-to-secret names and the problems out. Run applies it to every organization at
// goald's start (run.go).
package moveover

import (
	"bytes"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing/fstest"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// Message is the changelog of every version the conversion publishes.
const Message = "Moved connections to environments (ADR 0037)"

// The kinds Goal does not convert: their keys are minted per run, which secrets do not do
// (decision 2).
var unconverted = []string{"aws", "bedrock"}

// envRef is a ${NAME} in a kind's MCP header, which the box's env filled in.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// wholeRef is a value that is one {{secret}} reference and nothing else.
var wholeRef = regexp.MustCompile(`^\{\{[a-z][a-z0-9-]{1,38}[a-z0-9]\}\}$`)

// SecretName is the secret connection c's secret field becomes: the credential it refers to, the
// connection's name when its kind has one secret field, or <connection>-<field> (underscores as
// dashes) when it has several.
func SecretName(c domain.Connection, k domain.ConnectionKind, field string) string {
	if ref, ok := c.SecretRefs[field]; ok {
		return ref
	}

	if len(secretFields(k)) == 1 {
		return c.Name
	}

	return c.Name + "-" + strings.ReplaceAll(field, "_", "-")
}

// secretFields are k's secret fields' names, in its order.
func secretFields(k domain.ConnectionKind) []string {
	var out []string

	for _, f := range k.Spec.Fields {
		if f.Secret {
			out = append(out, f.Name)
		}
	}

	return out
}

// refused is why connection rc cannot be converted, or "".
func refused(rc domain.ResolvedConnection) string {
	k := rc.Kind

	switch {
	case slices.Contains(unconverted, k.Name) || k.Spec.Mint != "":
		return fmt.Sprintf("connection %s uses the %s kind, which Goal no longer converts: remove it from the factory", rc.Connection.Name, k.Name)
	case !k.Platform && k.Spec.MCP != nil && len(k.Spec.MCP.Command) > 0 && len(k.Spec.Fields) == 0:
		return fmt.Sprintf("connection %s uses the custom kind %s, a stdio command with no fields: add the server to an environment file's mcp yourself",
			rc.Connection.Name, k.Name)
	}

	for _, f := range secretFields(k) {
		if name := SecretName(rc.Connection, k, f); domain.CheckSecret(domain.Secret{Name: name, Source: domain.SecretSourceValue, Scope: domain.SecretScope{All: true}}) != nil {
			return fmt.Sprintf("connection %s's field %s would become secret %q, which is not a valid secret name: rename the connection",
				rc.Connection.Name, f, name)
		}
	}

	return ""
}

// Secrets are the secrets the organization's connections become: one per sealed secret field
// that is set, named by SecretName, open to every factory. A field that refers to a credential
// is that credential already (migration 00086 made credentials secrets), and a connection
// refused converts nothing.
func Secrets(conns []domain.ResolvedConnection) []domain.NewSecret {
	var out []domain.NewSecret

	for _, rc := range conns {
		if refused(rc) != "" {
			continue
		}

		for _, f := range secretFields(rc.Kind) {
			if _, ref := rc.Connection.SecretRefs[f]; ref || rc.Values[f] == "" {
				continue
			}

			out = append(out, domain.NewSecret{
				Name: SecretName(rc.Connection, rc.Kind, f), Value: rc.Values[f], Scope: domain.SecretScope{All: true},
				Description: fmt.Sprintf("Moved from connection %s, field %s (ADR 0037).", rc.Connection.Name, f),
			})
		}
	}

	return out
}

// Result is a converted version.
type Result struct {
	// Files are the new version's whole tree; nil when Problems is not empty or the version is on
	// secrets already.
	Files map[string][]byte
	// Names maps each connection the version used to its secret fields' secret names.
	Names map[string]map[string]string
	// Problems say what keeps the version from converting, each naming what to fix.
	Problems []string
	// OnSecrets reports a version that has environment files or a model key already: nothing to do.
	OnSecrets bool
}

// Convert converts a version's files, given the organization's opened connections (their kinds
// and values, which it reads only to know which fields are set: no value reaches the result).
// factory.yaml loses spec.connections, the agents' connections, runtime.modelConnection,
// runtime.size, runtime.tools, repository.connection and a trigger's modelAgrees.connection;
// environments/default.yaml gets the factory's connections, and each agent whose connections
// differ from the factory's gets environments/<agent>.yaml and environment: <agent>.
func Convert(files map[string][]byte, conns []domain.ResolvedConnection) Result {
	fs := fstest.MapFS{}
	for p, b := range files {
		fs[p] = &fstest.MapFile{Data: b}
	}

	f, problems := config.Check(fs)
	if f == nil {
		msgs := []string{}

		for _, p := range problems {
			if p.Blocking() {
				msgs = append(msgs, fmt.Sprintf("%s: %s", p.File, p.Message))
			}
		}

		return Result{Problems: append(msgs, "the version does not load: fix it and publish")}
	}

	if f.Line().OnSecrets() {
		return Result{OnSecrets: true}
	}

	c := &converter{byName: map[string]domain.ResolvedConnection{}, names: map[string]map[string]string{}}
	for _, rc := range conns {
		c.byName[rc.Connection.Name] = rc
	}

	out := c.convert(f, files[config.FileName])
	if len(c.problems) > 0 {
		return Result{Problems: c.problems, Names: c.names}
	}

	tree := maps.Clone(files)
	maps.Copy(tree, out)

	return Result{Files: tree, Names: c.names}
}

type converter struct {
	byName   map[string]domain.ResolvedConnection
	names    map[string]map[string]string
	problems []string
}

func (c *converter) fail(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

// connection is the opened connection name, recording its secret names; false, with a problem,
// when the organization has none or it cannot be converted.
func (c *converter) connection(name string) (domain.ResolvedConnection, bool) {
	rc, ok := c.byName[name]
	if !ok {
		c.fail("connection %s does not exist in the organization: remove it from the factory", name)

		return rc, false
	}

	if why := refused(rc); why != "" {
		c.fail("%s", why)

		return rc, false
	}

	if _, seen := c.names[name]; !seen {
		m := map[string]string{}
		for _, f := range secretFields(rc.Kind) {
			m[f] = SecretName(rc.Connection, rc.Kind, f)
		}

		c.names[name] = m
	}

	return rc, true
}

// render is what connection rc gives a box, its secret fields as {{secret}} references.
func (c *converter) render(rc domain.ResolvedConnection) (domain.RunConnection, bool) {
	values := map[string]string{}
	maps.Copy(values, rc.Connection.Fields)

	for _, f := range secretFields(rc.Kind) {
		if _, ref := rc.Connection.SecretRefs[f]; ref || rc.Values[f] != "" {
			values[f] = "{{" + SecretName(rc.Connection, rc.Kind, f) + "}}"
		}
	}

	run, err := rc.Kind.Render(values, nil)
	if err != nil {
		c.fail("connection %s does not render: %v", rc.Connection.Name, err)

		return run, false
	}

	return run, true
}

// secretOf is the {{secret}} reference of rc's one secret field that is set, for the model key.
func (c *converter) secretOf(rc domain.ResolvedConnection, what string) string {
	fields := secretFields(rc.Kind)
	if len(fields) != 1 {
		c.fail("connection %s, the %s, has %d secret fields, not one: set %s yourself", rc.Connection.Name, what, len(fields), what)

		return ""
	}

	return "{{" + SecretName(rc.Connection, rc.Kind, fields[0]) + "}}"
}

// envSpec is an environment file's spec as the conversion writes it.
type envSpec struct {
	Size  string               `yaml:"size,omitempty"`
	Tools []string             `yaml:"tools,omitempty,flow"`
	Env   map[string]string    `yaml:"env,omitempty"`
	MCP   map[string]mcpServer `yaml:"mcp,omitempty"`
}

type mcpServer struct {
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Command []string          `yaml:"command,omitempty,flow"`
	Tools   map[string]string `yaml:"tools,omitempty"`
}

type envFile struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   map[string]string `yaml:"metadata"`
	Spec       envSpec           `yaml:"spec"`
}

// environment renders the connections names into one environment file's spec, with the
// runtime's size and tools.
func (c *converter) environment(names []string, size string, tools []string) envSpec {
	spec := envSpec{Size: size, Tools: slices.Clone(tools), Env: map[string]string{}, MCP: map[string]mcpServer{}}
	from := map[string]string{}

	for _, name := range names {
		rc, ok := c.connection(name)
		if !ok {
			continue
		}

		run, ok := c.render(rc)
		if !ok {
			continue
		}

		for _, k := range slices.Sorted(maps.Keys(run.Env)) {
			if prev, taken := from[k]; taken && spec.Env[k] != run.Env[k] {
				c.fail("connections %s and %s both set %s: keep one of them in the factory", prev, name, k)
			}

			spec.Env[k], from[k] = run.Env[k], name
		}

		if run.CLI != "" && !slices.Contains(spec.Tools, run.CLI) {
			spec.Tools = append(spec.Tools, run.CLI)
		}

		if run.MCP != nil {
			spec.MCP[name] = mcpOf(rc, run)
		}
	}

	return spec
}

// mcpOf is connection rc's MCP server: remote, its headers' ${NAME} filled with the env's
// references, or else stdio; its tool modes those set on the connection but allow.
func mcpOf(rc domain.ResolvedConnection, run domain.RunConnection) mcpServer {
	var s mcpServer

	if run.MCP.URL != "" {
		s.URL = run.MCP.URL

		for h, v := range run.MCP.Headers {
			if s.Headers == nil {
				s.Headers = map[string]string{}
			}

			s.Headers[h] = envRef.ReplaceAllStringFunc(v, func(m string) string { return run.Env[envRef.FindStringSubmatch(m)[1]] })
		}
	} else {
		s.Command = slices.Clone(run.MCP.Command)
	}

	for tool, mode := range rc.Connection.ToolModes {
		if mode == domain.ToolModeAllow {
			continue
		}

		if s.Tools == nil {
			s.Tools = map[string]string{}
		}

		s.Tools[tool] = string(mode)
	}

	return s
}

// convert edits factory.yaml and returns it with the environment files.
func (c *converter) convert(f *config.Factory, raw []byte) map[string][]byte {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 {
		c.fail("factory.yaml does not parse: %v", err)

		return nil
	}

	spec := get(doc.Content[0], "spec")
	runtime := get(spec, "runtime")
	rt := f.Spec.Runtime

	factoryConns := c.factoryConnections(f)
	out := map[string][]byte{}
	write := func(name string, s envSpec) {
		b, err := marshal(envFile{APIVersion: "goal/v1", Kind: "Environment", Metadata: map[string]string{"name": name}, Spec: s})
		if err != nil {
			c.fail("environment %s: %v", name, err)
		}

		out[config.EnvironmentsDir+"/"+name+".yaml"] = b
	}

	write(config.DefaultEnvironment, c.environment(factoryConns, rt.Size, rt.Tools))
	c.agents(f, get(spec, "agents"), factoryConns, write)

	remove(spec, "connections")
	remove(runtime, "size")
	remove(runtime, "tools")

	c.model(runtime, rt.ModelConnection)

	if r := rt.Repository; r != nil && r.Connection != "" {
		c.repository(get(runtime, "repository"), *r)
	}

	c.triggers(get(get(spec, "board"), "triggers"))

	b, err := marshal(&doc)
	if err != nil {
		c.fail("factory.yaml: %v", err)
	}

	out[config.FileName] = b

	return out
}

// factoryConnections are spec.connections' names; an inline one is a problem.
func (c *converter) factoryConnections(f *config.Factory) []string {
	out := make([]string, 0, len(f.Spec.Connections))

	for _, conn := range f.Spec.Connections {
		if conn.Inline() {
			c.fail("connection %s is an inline connection, removed in phase 4: remove it from the factory", conn.Name)

			continue
		}

		out = append(out, conn.Name)
	}

	return out
}

// agents drop each agent's connections, and write an environment named after each agent whose
// connections, those the factory lists too, differ from the factory's.
func (c *converter) agents(f *config.Factory, nodes *yaml.Node, factoryConns []string, write func(string, envSpec)) {
	rt := f.Spec.Runtime

	for i, a := range f.Spec.Agents {
		own := slices.DeleteFunc(slices.Clone(a.Connections), func(n string) bool { return !slices.Contains(factoryConns, n) })
		node := nodes.Content[i]
		remove(node, "connections")

		if sameSet(own, factoryConns) {
			continue
		}

		if a.Name == config.DefaultEnvironment {
			c.fail("agent default's connections differ from the factory's, and its environment cannot be named default: rename the agent")

			continue
		}

		write(a.Name, c.environment(own, rt.Size, rt.Tools))
		set(node, "environment", a.Name)
	}
}

// model turns runtime.modelConnection into runtime.modelKey, the model connection's secret.
func (c *converter) model(runtime *yaml.Node, name string) {
	if name == "" {
		return
	}

	remove(runtime, "modelConnection")

	rc, ok := c.connection(name)

	switch {
	case !ok:
	case !rc.Kind.Spec.Model:
		c.fail("connection %s, the model connection, gives no model: set runtime.modelKey yourself", name)
	default:
		if ref := c.secretOf(rc, "runtime.modelKey"); ref != "" {
			set(runtime, "modelKey", ref)
		}
	}
}

// repository turns repository.connection into repository.token, the connection's git password,
// with the forge its kind's code host names when the URL's host does not.
func (c *converter) repository(node *yaml.Node, r config.Repository) {
	remove(node, "connection")

	rc, ok := c.connection(r.Connection)
	if !ok {
		return
	}

	run, ok := c.render(rc)
	if !ok {
		return
	}

	if run.Git == nil || !wholeRef.MatchString(run.Env[run.Git.PasswordEnv]) {
		c.fail("connection %s, the repository's, has no git login: set repository.token yourself", r.Connection)

		return
	}

	set(node, "token", run.Env[run.Git.PasswordEnv])

	if u, err := url.Parse(r.URL); err == nil && r.Forge == "" && u.Hostname() != "github.com" && u.Hostname() != "gitlab.com" && rc.Kind.Spec.CodeHost != "" {
		set(node, "forge", rc.Kind.Spec.CodeHost)
	}
}

// triggers turn each modelAgrees.connection into modelAgrees.key.
func (c *converter) triggers(seq *yaml.Node) {
	if seq == nil {
		return
	}

	for _, t := range seq.Content {
		agrees := get(get(t, "only"), "modelAgrees")

		conn := get(agrees, "connection")
		if conn == nil {
			continue
		}

		remove(agrees, "connection")

		if rc, ok := c.connection(conn.Value); ok {
			if ref := c.secretOf(rc, "modelAgrees.key"); ref != "" {
				set(agrees, "key", ref)
			}
		}
	}
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)

	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// get is mapping m's value at key, or nil.
func get(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	return nil
}

// remove deletes key from mapping m.
func remove(m *yaml.Node, key string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = slices.Delete(m.Content, i, i+2)

			return
		}
	}
}

// set sets mapping m's key to the string value, in place when it has one.
func set(m *yaml.Node, key, value string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}

	if v := get(m, key); v != nil {
		v.Kind, v.Tag, v.Value, v.Style, v.Content = yaml.ScalarNode, "!!str", value, yaml.DoubleQuotedStyle, nil

		return
	}

	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: yaml.DoubleQuotedStyle})
}
