package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/contracts/definitions"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The folder of a definition's environment files (ADR 0037 decision 5), one
// environments/<name>.yaml per environment, and the one every factory has, which an agent, judge
// or eval case that names none gets.
const (
	EnvironmentsDir    = "environments"
	DefaultEnvironment = "default"
)

// Environment is one environment file: what an agent's box installs and what it can reach.
type Environment struct {
	// Name is the file name without its extension.
	Name string
	Spec EnvironmentSpec

	file string
	doc  *document
}

// EnvironmentSpec is an environment file's spec. Values of Env, Files and the MCP servers'
// Headers and Env may hold {{secret}} references.
type EnvironmentSpec struct {
	Size  string               `json:"size,omitempty"`
	Image string               `json:"image,omitempty"`
	Tools []string             `json:"tools,omitempty"`
	Setup []string             `json:"setup,omitempty"`
	Env   map[string]string    `json:"env,omitempty"`
	Files map[string]string    `json:"files,omitempty"`
	MCP   map[string]MCPServer `json:"mcp,omitempty"`
}

// MCPServer is an MCP server an environment gives: remote (URL and Headers) or stdio (Command
// and Env). Tools maps a tool name or glob to its mode, allow, ask or off.
type MCPServer struct {
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Tools   map[string]string `json:"tools,omitempty"`
}

// environmentFile is an environment file as decoded: its spec and the name it says it has.
type environmentFile struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec EnvironmentSpec `json:"spec"`
}

// File is the environment's path in the definition, environments/<name>.yaml.
func (e Environment) File() string { return e.file }

// Locate places path, keys from the file's root ("spec", "env", "GH_TOKEN"), in the
// environment's file. Its Visual place is the Environments section, at /environments/<name>
// followed by the path below spec as written.
func (e Environment) Locate(path ...string) Location {
	var l Location
	if e.doc != nil {
		l = e.doc.locate(path)
	}

	l.Section, l.Pointer, l.File = domain.SectionEnvironments, e.pointer(path), e.file

	return l
}

// pointer is the Environments section's pointer of a file path: /environments/<name>/<keys
// below spec>.
func (e Environment) pointer(path []string) string {
	return "/" + domain.SectionEnvironments + e.sectionPath(path)
}

// sectionPath is the pointer of a file path relative to the Environments section, the form a
// ProblemFix's patch uses: /<name>/<keys below spec>.
func (e Environment) sectionPath(path []string) string {
	if len(path) > 0 && path[0] == "spec" {
		path = path[1:]
	}

	return joinPointer(append([]string{e.Name}, path...))
}

// ReplaceFix is a fix that replaces the value at path, keys from the file's root, with v, as a
// patch on the Environments section.
func (e Environment) ReplaceFix(label string, path []string, v any) domain.ProblemFix {
	value, _ := json.Marshal(v)

	return domain.ProblemFix{
		Label: label, Section: domain.SectionEnvironments,
		Patch: []domain.PatchOp{{Op: domain.PatchReplace, Path: e.sectionPath(path), Value: value}},
	}
}

// Environments are the definition's environment files that parse and satisfy the schema, by name.
func (f *Factory) Environments() []Environment { return slices.Clone(f.environments) }

// EnvironmentName is the environment the agent's box gets: its own, or DefaultEnvironment.
func (a Agent) EnvironmentName() string {
	if a.Environment != "" {
		return a.Environment
	}

	return DefaultEnvironment
}

// secretName is the form of a secret's name (domain's secret name pattern).
var secretName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// reference is one {{…}} in a value, spaces inside the braces allowed; its group is the name.
var reference = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// SecretNames are the secrets value refers to, in order, each once: the names of its {{secret}}
// references.
func SecretNames(value string) []string {
	var out []string

	for _, m := range reference.FindAllStringSubmatch(value, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}

	return out
}

// badReference is the first {{ of value that is not a {{secret}} reference with a secret's name,
// "" when every one is. A lone {{ or }} counts: a literal {{ is not supported (ADR 0037 decision 6).
func badReference(value string) string {
	for _, loc := range reference.FindAllStringSubmatchIndex(value, -1) {
		if !secretName.MatchString(value[loc[2]:loc[3]]) {
			return value[loc[0]:loc[1]]
		}
	}

	if rest := reference.ReplaceAllString(value, ""); strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		return "{{"
	}

	return ""
}

// wholeReference reports whether value is one {{secret}} reference and nothing else.
func wholeReference(value string) bool {
	m := reference.FindStringSubmatchIndex(strings.TrimSpace(value))

	return m != nil && m[0] == 0 && m[1] == len(strings.TrimSpace(value)) && secretName.MatchString(strings.TrimSpace(value)[m[2]:m[3]])
}

// SecretRef is one {{secret}} reference a definition makes in a place decision 6 allows.
type SecretRef struct {
	// Name is the secret's name.
	Name string
	// Where names the key for a message: runtime.modelKey, or environments/default.yaml env.GH_TOKEN.
	Where string
	// Location places the key in both editors.
	Location Location
}

// factoryRefPlaces are the factory.yaml keys a {{secret}} may be in (ADR 0037 decision 6); a *
// is any one key or index. Every other key that has one is a secret-placement problem.
var factoryRefPlaces = [][]string{
	{"spec", "runtime", "repository", "token"},
	{"spec", "runtime", "modelKey"},
	{"spec", "board", "triggers", "*", "only", "modelAgrees", "key"},
}

// factoryTemplatePlaces are the factory.yaml keys whose {{field}} placeholders take an event's
// fields, so they are not secret references.
var factoryTemplatePlaces = [][]string{
	{"spec", "board", "triggers", "*", "create", "title"},
	{"spec", "board", "triggers", "*", "create", "brief"},
}

// environmentRefPlaces are the keys of an environment file a {{secret}} may be in.
var environmentRefPlaces = [][]string{
	{"spec", "env", "*"},
	{"spec", "files", "*"},
	{"spec", "mcp", "*", "headers", "*"},
	{"spec", "mcp", "*", "env", "*"},
}

// SecretRefs are every {{secret}} reference the definition makes in a place decision 6 allows:
// factory.yaml's, then each environment file's, in the order they are written.
func (f *Factory) SecretRefs() []SecretRef {
	var out []SecretRef

	if f.doc != nil {
		walkScalars(f.doc.root, nil, func(p []string, value string) {
			if matchesAny(p, factoryRefPlaces) {
				for _, name := range SecretNames(value) {
					out = append(out, SecretRef{Name: name, Where: displayPath(p[1:]), Location: f.doc.locate(p)})
				}
			}
		})
	}

	for _, e := range f.environments {
		walkScalars(e.doc.root, nil, func(p []string, value string) {
			if matchesAny(p, environmentRefPlaces) {
				for _, name := range SecretNames(value) {
					out = append(out, SecretRef{Name: name, Where: e.file + " " + displayPath(p[1:]), Location: e.Locate(p...)})
				}
			}
		})
	}

	return out
}

// walkScalars calls fn with the path and value of every string scalar below n, mapping keys
// included as their own value's path.
func walkScalars(n *yaml.Node, p []string, fn func(path []string, value string)) {
	n = deref(n)
	if n == nil {
		return
	}

	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			walkScalars(n.Content[i+1], append(slices.Clone(p), n.Content[i].Value), fn)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			walkScalars(c, append(slices.Clone(p), fmt.Sprint(i)), fn)
		}
	case yaml.ScalarNode:
		if n.Tag == "!!str" || n.Tag == "" {
			fn(p, n.Value)
		}
	case yaml.DocumentNode, yaml.AliasNode:
	}
}

// matchesAny reports whether path p is one of places, where a * matches any one key or index.
func matchesAny(p []string, places [][]string) bool {
	return slices.ContainsFunc(places, func(place []string) bool {
		if len(place) != len(p) {
			return false
		}

		for i, k := range place {
			if k != "*" && k != p[i] {
				return false
			}
		}

		return true
	})
}

// compiledEnvironment compiles environment.schema.json once.
var compiledEnvironment = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileSchema(definitions.Environment, "environment.schema.json", "")
})

// knownForges are the code hosts whose host names say which adapter they take; any other host
// needs runtime.repository.forge.
var knownForges = map[string]string{"github.com": "github", "gitlab.com": "gitlab"}

// environments checks the environment files and what refers to them (ADR 0037 decisions 5-7):
// each file is named as a slug, satisfies environment.schema.json, says its own name, lists
// mise tool specs and writes references well; environments/default.yaml exists; every agent's
// environment exists; a repository on another host names its forge; and no {{secret}} is
// outside the places decision 6 allows. It records the names of the environments that exist for
// the judges' and cases' checks.
func (c *checker) environments() {
	for _, p := range evalFiles(c.f.files, EnvironmentsDir, ".yaml", ".yml") {
		name := strings.TrimSuffix(path.Base(p), path.Ext(p))
		if !evalName.MatchString(name) {
			c.problems = append(c.problems, Environment{file: p, Name: name}.Locate().Problem(checkEnvironments, codeEnvironmentNameInvalid,
				domain.SeverityError, fmt.Sprintf("%s: the file name %q is the name agents refer to it by, so it must be a slug: "+
					"3 to 40 lowercase letters, digits and hyphens, starting with a letter", p, name)))
		}

		c.envNames = append(c.envNames, name)

		if e, ok := c.environment(p, name); ok {
			c.f.environments = append(c.f.environments, e)
		}
	}

	if !slices.Contains(c.envNames, DefaultEnvironment) {
		c.problems = append(c.problems, Environment{file: path.Join(EnvironmentsDir, DefaultEnvironment+".yaml"), Name: DefaultEnvironment}.
			Locate().Problem(checkEnvironments, codeEnvironmentDefaultMissing, domain.SeverityError,
			"environments/default.yaml is missing: every factory has one, and an agent that names no environment gets it"))
	}

	for i, a := range c.f.Spec.Agents {
		if a.Environment != "" && !slices.Contains(c.envNames, a.Environment) {
			c.errorf(checkEnvironments, codeEnvironmentUnknown, at("spec", keyAgents, i, "environment"),
				"agent %s: unknown environment %q; an environment is a file %s/<name>.yaml", a.Name, a.Environment, EnvironmentsDir)
		}
	}

	c.forge()
	c.factoryPlacement()
}

// environment checks one environment file and returns it when it satisfies the schema.
func (c *checker) environment(p, name string) (Environment, bool) {
	raw, err := fs.ReadFile(c.f.files, p)
	if err != nil {
		return Environment{}, false
	}

	var n yaml.Node
	if yaml.Unmarshal(raw, &n) != nil {
		return Environment{}, false // otherFileProblems reports it
	}

	doc, bad := parseYAML(p, raw)
	if bad != nil {
		bad.Check, bad.Section = checkYAML, domain.SectionEnvironments
		c.problems = append(c.problems, *bad)

		return Environment{}, false
	}

	e := Environment{Name: name, file: p, doc: doc}
	if !c.environmentSchema(e) {
		return Environment{}, false
	}

	var ef environmentFile
	if err := json.Unmarshal(doc.json, &ef); err != nil {
		c.problems = append(c.problems, e.Locate().Problem(checkYAML, codeYAMLInvalid, domain.SeverityError, fmt.Sprintf("%s: %v", p, err)))

		return Environment{}, false
	}

	e.Spec = ef.Spec

	if ef.Metadata.Name != "" && ef.Metadata.Name != name {
		c.envError(e, codeEnvironmentNameMismatch, at("metadata", keyName),
			"%s: metadata.name is %q, but an environment's name is its file name, %s", p, ef.Metadata.Name, name)
	}

	for i, t := range e.Spec.Tools {
		if !toolSpec.MatchString(t) {
			at := at("spec", "tools", i)
			c.envError(e, codeToolSpecInvalid, at,
				"%s: tools: %q is not a mise tool spec, such as go, go@1.26.10 or npm:@acme/cli@2", p, t).
				Fixes = []domain.ProblemFix{{Label: "Remove " + t, Section: domain.SectionEnvironments,
				Patch: []domain.PatchOp{{Op: domain.PatchRemove, Path: e.sectionPath(at)}}}}
		}
	}

	walkScalars(doc.root, nil, func(at []string, value string) {
		if !strings.Contains(value, "{{") && !strings.Contains(value, "}}") {
			return
		}

		if !matchesAny(at, environmentRefPlaces) {
			c.envError(e, codeSecretPlacement, at, "%s %s: a {{secret}} reference is valid only in env, files and mcp headers "+
				"and a stdio server's env; a literal {{ is not supported", p, displayPath(at[1:]))

			return
		}

		if bad := badReference(value); bad != "" {
			c.envError(e, codeSecretRefInvalid, at, "%s %s: %q is not a {{secret}} reference; it names a secret: "+
				"3 to 40 lowercase letters, digits and hyphens, starting with a letter", p, displayPath(at[1:]), bad)
		}
	})

	return e, true
}

// envError records an error at path in environment e and returns it, so a caller can attach fixes.
func (c *checker) envError(e Environment, code string, path []string, format string, a ...any) *domain.DefinitionProblem {
	c.problems = append(c.problems, e.Locate(path...).Problem(checkEnvironments, code, domain.SeverityError, fmt.Sprintf(format, a...)))

	return &c.problems[len(c.problems)-1]
}

// environmentSchema validates e's file against environment.schema.json and records each failed
// rule. It reports whether the file satisfies it.
func (c *checker) environmentSchema(e Environment) bool {
	problem := func(code string, at []string, msg string) domain.DefinitionProblem {
		return e.Locate(at...).Problem(checkSchema, code, domain.SeverityError, msg)
	}

	s, err := compiledEnvironment()
	if err != nil {
		c.problems = append(c.problems, problem(codeSchemaInvalid, nil, "environment.schema.json does not compile: "+err.Error()))

		return false
	}

	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(e.doc.json))
	if err != nil {
		c.problems = append(c.problems, problem(codeYAMLInvalid, nil, fmt.Sprintf("%s does not parse: %v", e.file, err)))

		return false
	}

	failed, err := failedRules(s, v)
	if err != nil {
		c.problems = append(c.problems, problem(codeSchemaInvalid, nil, fmt.Sprintf("%s: %v", e.file, err)))

		return false
	}

	for _, l := range failed {
		c.problems = appendNew(c.problems, problem(l.code, l.path, fmt.Sprintf("%s: %s", e.file, l.message)))
	}

	return len(failed) == 0
}

// forge reports a repository on a host other than github.com and gitlab.com that names no
// forge: nothing says which code host adapter clones and merges it. A repository that still has
// a connection (moved-to-environment) takes the code host of the connection's kind.
func (c *checker) forge() {
	r := c.f.Spec.Runtime.Repository
	if r == nil || r.Forge != "" || r.Connection != "" {
		return
	}

	u, err := url.Parse(r.URL)
	if err != nil {
		return
	}

	if _, ok := knownForges[strings.ToLower(u.Hostname())]; ok {
		return
	}

	base := at("spec", "runtime", "repository")
	forge := append(slices.Clone(base), "forge")
	section, _ := ModelPointer(forge)

	add := func(f string) domain.ProblemFix {
		value, _ := json.Marshal(f)

		return domain.ProblemFix{
			Label: "Set forge to " + f, Section: section,
			Patch: []domain.PatchOp{{Op: domain.PatchAdd, Path: sectionPath(forge), Value: value}},
		}
	}

	c.errorf(checkEnvironments, codeForgeMissing, append(base, "url"),
		"runtime.repository: %s is neither github.com nor gitlab.com, so set forge to github or gitlab, the code host it runs", u.Host).
		Fixes = []domain.ProblemFix{add("github"), add("gitlab")}
}

// factoryPlacement checks factory.yaml's {{…}}: a {{secret}} only in the keys decision 6 allows,
// where it is written well, and the event placeholders of a trigger's title and brief left to
// the triggers check. runtime.modelKey and modelAgrees.key are one whole reference; a
// repository token that has none is the removed form env-reference-removed.
func (c *checker) factoryPlacement() {
	walkScalars(c.f.doc.root, nil, func(p []string, value string) {
		braces := strings.Contains(value, "{{") || strings.Contains(value, "}}")

		switch {
		case matchesAny(p, factoryTemplatePlaces):
		case !matchesAny(p, factoryRefPlaces):
			if braces {
				c.errorf(checkEnvironments, codeSecretPlacement, p,
					"%s: a {{secret}} reference is valid only in runtime.repository.token, runtime.modelKey, a trigger's modelAgrees.key "+
						"and environment files; a literal {{ is not supported", displayPath(p[1:]))
			}
		case p[len(p)-1] == "token" && !braces:
			// env-reference-removed
		case !wholeReference(value):
			c.errorf(checkEnvironments, codeSecretRefInvalid, p,
				"%s: %q is not a {{secret}} reference; write the secret's name in double braces, such as {{model-key}}", displayPath(p[1:]), value)
		}
	})
}
