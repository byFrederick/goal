package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/domain"
)

// Checks: the validation steps a problem comes from (DefinitionProblem.Check).
const (
	checkYAML       = "yaml"       // every file parses
	checkSchema     = "schema"     // factory.yaml satisfies factory.schema.json
	checkLine       = "line"       // the line is well formed
	checkReferences = "references" // every name and path points at something that exists
	checkImages     = "images"     // connection images are pinned
	checkSecrets    = "secrets"    // secrets are references, not values
	// checkAvailability: what the definition declares that this install cannot serve yet.
	checkAvailability = "availability"
)

// Problem codes config reports. They are stable: the UI, the CLI and tests key on them.
const (
	codeYAMLInvalid      = "yaml-invalid"
	codeJSONInvalid      = "json-invalid"
	codeFactoryMissing   = "factory-yaml-missing"
	codeSchemaInvalid    = "schema-invalid"
	codeSchemaRequired   = "schema-required"
	codeSchemaUnknownKey = "schema-unknown-key"
	codeSchemaEnum       = "schema-enum"
	codeSchemaType       = "schema-type"

	codeLastNotDone     = "last-station-not-done"
	codeGateNotUpstream = "gate-not-upstream"
	codeAgentsOverWIP   = "agents-exceed-wip"
	codeCheckRunMissing = "check-run-missing"
	codeMergeNeedsRepo  = "merge-needs-repository"

	codeDuplicateName     = "duplicate-name"
	codeUnknownAgent      = "unknown-agent"
	codeUnknownSkill      = "unknown-skill"
	codeUnknownConnection = "unknown-connection"
	codeUnknownContract   = "unknown-contract"
	codeUnknownStation    = "unknown-station"
	codeUnknownCollection = "unknown-collection"
	codeToolAgent         = "tool-agent-not-on-connection"
	codePromptNotFound    = "prompt-not-found"
	codeSkillNotFound     = "skill-not-found"
	codeContractNotFound  = "contract-not-found"

	codeImageUnpinned = "image-unpinned"
	codeLiteralSecret = "literal-secret"

	codeBuiltinAsk         = "builtin-ask-unavailable"
	codeConnectorLater     = "connector-unavailable"
	codeRepoConnectionTodo = "repository-connection-unavailable"
)

// checker runs the checks that need a decoded factory and collects their problems.
type checker struct {
	f        *Factory
	agents   []string
	stations []string
	problems []domain.DefinitionProblem
}

func newChecker(f *Factory) *checker {
	return &checker{
		f:        f,
		agents:   names(f.Spec.Agents, func(a Agent) string { return a.Name }),
		stations: names(f.Spec.Board.Stations, func(s Station) string { return s.ID }),
	}
}

func (c *checker) run() []domain.DefinitionProblem {
	c.duplicates()
	c.files()
	c.agentRefs()
	c.line()
	c.flowAndLearning()
	c.connections()
	c.images()
	c.secrets()

	return c.problems
}

// add records a problem at path (a factory.yaml path) and returns it, so a caller can attach fixes.
func (c *checker) add(
	check, code string, sev domain.ProblemSeverity, path []string, format string, a ...any,
) *domain.DefinitionProblem {
	c.problems = append(c.problems, c.f.doc.locate(path).Problem(check, code, sev, fmt.Sprintf(format, a...)))

	return &c.problems[len(c.problems)-1]
}

func (c *checker) errorf(check, code string, path []string, format string, a ...any) *domain.DefinitionProblem {
	return c.add(check, code, domain.SeverityError, path, format, a...)
}

func (c *checker) warnf(check, code string, path []string, format string, a ...any) {
	c.add(check, code, domain.SeverityWarning, path, format, a...)
}

// duplicates reports a station id, agent, skill or connection name used twice: references
// would silently pick the first.
func (c *checker) duplicates() {
	s := c.f.Spec
	dup(c, "station", names(s.Board.Stations, func(st Station) string { return st.ID }), "board", "stations", "id")
	dup(c, "agent", names(s.Agents, func(a Agent) string { return a.Name }), keyAgents, "", keyName)
	dup(c, "skill", names(s.Skills, func(sk Skill) string { return sk.Name }), "skills", "", keyName)

	for i, conn := range s.Connections {
		key := keyName
		if conn.Connector != "" && !c.f.hasKey(at("spec", "connections", i, keyName)) {
			key = "connector"
		}

		if slices.IndexFunc(s.Connections[:i], func(o Connection) bool { return o.Name == conn.Name }) >= 0 {
			c.errorf(checkReferences, codeDuplicateName, at("spec", "connections", i, key),
				"connection %s: the name is used by another connection", conn.Name)
		}
	}
}

func dup(c *checker, what string, ids []string, list, sub, key string) {
	for i, id := range ids {
		if slices.Index(ids[:i], id) < 0 {
			continue
		}

		p := at("spec", list)
		if sub != "" {
			p = append(p, sub)
		}

		c.errorf(checkReferences, codeDuplicateName, append(p, strconv.Itoa(i), key),
			"%s %s: the name is used by another %s", what, id, what)
	}
}

func (c *checker) files() {
	for i, s := range c.f.Spec.Skills {
		if s.Local() && !c.f.exists(path.Join(s.Source, "SKILL.md")) {
			c.errorf(checkReferences, codeSkillNotFound, at("spec", "skills", i, "source"),
				"skill %s: %s not found", s.Name, path.Join(s.Source, "SKILL.md"))
		}
	}

	for _, name := range sortedKeys(c.f.Spec.Contracts) {
		if p := c.f.Spec.Contracts[name]; !c.f.exists(p) {
			c.errorf(checkReferences, codeContractNotFound, at("spec", "contracts", name), "contract %s: %s not found", name, p)
		}
	}

	for i, a := range c.f.Spec.Agents {
		if !c.f.exists(a.Prompt) {
			c.errorf(checkReferences, codePromptNotFound, at("spec", keyAgents, i, "prompt"),
				"agent %s prompt: %s not found", a.Name, a.Prompt)
		}
	}
}

func (c *checker) agentRefs() {
	skills := names(c.f.Spec.Skills, func(s Skill) string { return s.Name })
	conns := names(c.f.Spec.Connections, func(cn Connection) string { return cn.Name })

	for i, a := range c.f.Spec.Agents {
		for j, s := range a.Skills {
			if !slices.Contains(skills, s) {
				c.errorf(checkReferences, codeUnknownSkill, at("spec", keyAgents, i, "skills", j),
					"agent %s: unknown skill %q", a.Name, s)
			}
		}

		for j, cn := range a.Connections {
			if !slices.Contains(conns, cn) {
				c.errorf(checkReferences, codeUnknownConnection, at("spec", keyAgents, i, "connections", j),
					"agent %s: unknown connection %q", a.Name, cn)
			}
		}

		c.builtinsAvailable(i, a)
	}
}

// builtinsAvailable warns about ask: nobody can approve a built-in tool call yet, so it is
// served as off.
func (c *checker) builtinsAvailable(i int, a Agent) {
	b := a.Builtins
	if b == nil {
		return
	}

	groups := []struct{ key, mode string }{
		{"read", b.Read}, {"edit", b.Edit}, {"run", b.Run.Mode}, {"web", b.Web}, {"subagents", b.Subagents},
	}

	for _, g := range groups {
		if g.mode == BuiltinAsk {
			c.warnf(checkAvailability, codeBuiltinAsk, at("spec", keyAgents, i, "builtins", g.key),
				"agent %s: %s is ask, which is served as off until built-in tools can be approved", a.Name, g.key)
		}
	}
}

func (c *checker) line() {
	st := c.f.Spec.Board.Stations
	if last := len(st) - 1; st[last].Kind != string(domain.KindDone) {
		c.errorf(checkLine, codeLastNotDone, at("spec", "board", "stations", last, "kind"),
			"board: the last station must be kind done")
	}

	for i, s := range st {
		c.station(i, s)
	}
}

func (c *checker) station(i int, s Station) {
	base := at("spec", "board", "stations", i)

	if s.Agent != "" && !slices.Contains(c.agents, s.Agent) {
		c.errorf(checkReferences, codeUnknownAgent, append(base, "agent"), "station %s: unknown agent %q", s.ID, s.Agent)
	}

	for _, lane := range sortedKeys(s.Route) {
		if a := s.Route[lane]; !slices.Contains(c.agents, a) {
			c.errorf(checkReferences, codeUnknownAgent, append(base, "route", lane),
				"station %s: route %s → unknown agent %q", s.ID, lane, a)
		}
	}

	if _, ok := c.f.Spec.Contracts[s.Output]; s.Output != "" && !ok {
		c.errorf(checkReferences, codeUnknownContract, append(base, "output"),
			"station %s: output %q is not a declared contract", s.ID, s.Output)
	}

	for j, ch := range s.Checks {
		if ch.Kind == CheckCommand && strings.TrimSpace(ch.Run) == "" {
			c.errorf(checkLine, codeCheckRunMissing, append(base, "checks", strconv.Itoa(j)), "station %s: a command check needs run", s.ID)
		}
	}

	c.gate(i, s)
	c.action(i, s)
	c.capacity(i, s)
}

func (c *checker) gate(i int, s Station) {
	if s.Gate == nil {
		return
	}

	p := at("spec", "board", "stations", i, "gate", "onReject")

	switch j := slices.Index(c.stations, s.Gate.OnReject); {
	case j < 0:
		c.errorf(checkReferences, codeUnknownStation, p, "station %s: gate.onReject %q is not a station", s.ID, s.Gate.OnReject)
	case j >= i:
		c.errorf(checkLine, codeGateNotUpstream, p,
			"station %s: gate.onReject %q must be a station before it, where the work is redone", s.ID, s.Gate.OnReject)
	}
}

func (c *checker) action(i int, s Station) {
	if s.Action != ActionMerge || c.f.Spec.Runtime.Repository != nil {
		return
	}

	p := c.errorf(checkLine, codeMergeNeedsRepo, at("spec", "board", "stations", i, "action"),
		"station %s: action merge needs runtime.repository, whose pull requests it merges", s.ID)
	p.Fixes = []domain.ProblemFix{{
		Label: "Remove the merge step", Section: domain.SectionLine,
		Patch: []domain.PatchOp{{Op: domain.PatchRemove, Path: sectionPath(at("spec", "board", "stations", i, "action"))}},
	}}
}

// capacity reports agents beyond the station's WIP: they would idle. The fixes raise WIP to the
// agents or lower the agents to WIP.
func (c *checker) capacity(i int, s Station) {
	if s.Kind != string(domain.KindAgent) || s.WIP <= 0 || s.Agents <= s.WIP {
		return
	}

	base := at("spec", "board", "stations", i)
	p := c.errorf(checkLine, codeAgentsOverWIP, append(base, keyAgents),
		"station %s: %d agents exceed wip %d; extra agents would idle", s.ID, s.Agents, s.WIP)
	p.Fixes = []domain.ProblemFix{
		replaceFix(fmt.Sprintf("Set WIP to %d", s.Agents), append(slices.Clone(base), "wip"), s.Agents),
		replaceFix(fmt.Sprintf("Set agents to %d", s.WIP), append(slices.Clone(base), keyAgents), s.WIP),
	}
}

func (c *checker) flowAndLearning() {
	if fl := c.f.Spec.Board.Flow; fl != nil && fl.Constraint != "" && fl.Constraint != "auto" &&
		!slices.Contains(c.stations, fl.Constraint) {
		c.errorf(checkReferences, codeUnknownStation, at("spec", "board", "flow", "constraint"),
			"flow.constraint %q is not a station", fl.Constraint)
	}

	l := c.f.Spec.Learning
	if l == nil || l.Librarian == nil {
		return
	}

	if !slices.Contains(c.agents, l.Librarian.Agent) {
		c.errorf(checkReferences, codeUnknownAgent, at("spec", "learning", "librarian", "agent"),
			"learning.librarian: unknown agent %q", l.Librarian.Agent)
	}

	for j, w := range l.Librarian.Writes {
		if !slices.Contains(c.f.Spec.KnowledgeBase.Collections, w) {
			c.errorf(checkReferences, codeUnknownCollection, at("spec", "learning", "librarian", "writes", j),
				"learning.librarian writes %q, which is not a knowledge base collection", w)
		}
	}
}

// connections checks the connector form's references and what of it this install can serve,
// and a repository reached through a connection.
func (c *checker) connections() {
	for i, conn := range c.f.Spec.Connections {
		if conn.Connector == "" {
			continue
		}

		base := at("spec", "connections", i)
		c.warnf(checkAvailability, codeConnectorLater, append(base, "connector"),
			"connection %s: connectors arrive with phase 3; until then its tools are not served", conn.Name)

		for j, a := range conn.Agents {
			if !slices.Contains(c.agents, a) {
				c.errorf(checkReferences, codeUnknownAgent, append(slices.Clone(base), keyAgents, strconv.Itoa(j)),
					"connection %s: unknown agent %q", conn.Name, a)
			}
		}

		for _, tool := range sortedKeys(conn.Tools) {
			for j, a := range conn.Tools[tool].Agents {
				if !slices.Contains(conn.Agents, a) {
					c.errorf(checkReferences, codeToolAgent, append(slices.Clone(base), "tools", tool, keyAgents, strconv.Itoa(j)),
						"connection %s: tool %s names agent %q, who is not one of the connection's agents", conn.Name, tool, a)
				}
			}
		}
	}

	r := c.f.Spec.Runtime.Repository
	if r == nil || r.Connection == "" {
		return
	}

	p := at("spec", "runtime", "repository", "connection")
	if !slices.ContainsFunc(c.f.Spec.Connections, func(cn Connection) bool { return cn.Name == r.Connection }) {
		c.errorf(checkReferences, codeUnknownConnection, p, "runtime.repository: unknown connection %q", r.Connection)
	}

	c.errorf(checkAvailability, codeRepoConnectionTodo, p,
		"runtime.repository: a repository through a connection arrives with connectors (phase 3); use url and token until then")
}

func (c *checker) images() {
	for i, conn := range c.f.Spec.Connections {
		if conn.Image != "" && !Pinned(conn.Image) {
			c.warnf(checkImages, codeImageUnpinned, at("spec", "connections", i, "image"),
				"connection %s: image %s is not pinned; use a version tag or a digest (name:1.2.3@sha256:...)",
				conn.Name, conn.Image)
		}
	}
}

// secrets warns about literal values where a secret belongs. The message names the place,
// never the value.
func (c *checker) secrets() {
	for _, u := range c.f.SecretUses() {
		if u.Literal() {
			c.warnf(checkSecrets, codeLiteralSecret, splitPointer(u.Doc),
				"%s is a literal value, not a secret reference; if it is a secret, use env:NAME so it stays out of the definition", u.Path)
		}
	}
}

// replaceFix replaces the value at path (a factory.yaml path) with v in its section.
func replaceFix(label string, path []string, v any) domain.ProblemFix {
	section, _ := ModelPointer(path)
	value, _ := json.Marshal(v)

	return domain.ProblemFix{
		Label: label, Section: section,
		Patch: []domain.PatchOp{{Op: domain.PatchReplace, Path: sectionPath(path), Value: value}},
	}
}

// sectionPath is the pointer of a factory.yaml path relative to its model section, the form a
// ProblemFix's patch uses.
func sectionPath(path []string) string {
	section, pointer := ModelPointer(path)

	return strings.TrimPrefix(pointer, "/"+section)
}

// hasKey reports whether factory.yaml spells out path (rather than taking a default).
func (f *Factory) hasKey(path []string) bool {
	if f.doc == nil {
		return false
	}

	key, val := f.doc.lookup(path)

	return val != nil && key != nil && key.Value == path[len(path)-1]
}

// exists reports whether rel names a file in the factory's tree. Spec paths are written
// "./agents/x.md"; fs.FS paths have no "./" and no "..", so a path escaping the tree never matches.
func (f *Factory) exists(rel string) bool {
	_, err := fs.Stat(f.files, path.Clean(rel))

	return err == nil
}

// otherFileProblems reports YAML and JSON files besides factory.yaml (contracts, evals) that
// do not parse. Hidden folders (a working copy's .goal/) are skipped.
func otherFileProblems(files fs.FS) []domain.DefinitionProblem {
	var out []domain.DefinitionProblem

	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil //nolint:nilerr // an unreadable entry is reported by whatever references it
		case d.IsDir() && p != "." && strings.HasPrefix(d.Name(), "."):
			return fs.SkipDir
		case d.IsDir() || p == FileName:
			return nil
		}

		if bad := parseFile(files, p); bad != nil {
			out = append(out, *bad)
		}

		return nil
	})

	return out
}

func parseFile(files fs.FS, p string) *domain.DefinitionProblem {
	ext := path.Ext(p)
	if ext != ".yaml" && ext != ".yml" && ext != ".json" {
		return nil
	}

	raw, err := fs.ReadFile(files, p)
	if err != nil {
		return nil
	}

	if ext == ".json" {
		if !json.Valid(raw) {
			var v any

			err := json.Unmarshal(raw, &v)

			return &domain.DefinitionProblem{
				Code: codeJSONInvalid, Severity: domain.SeverityError, Check: checkYAML, Section: domain.SectionFiles,
				File: p, Message: fmt.Sprintf("%s does not parse: %v", p, err),
			}
		}

		return nil
	}

	var n yaml.Node
	if err := yaml.Unmarshal(raw, &n); err != nil {
		return yamlInvalid(p, err)
	}

	return nil
}

func names[T any](items []T, key func(T) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = key(it)
	}

	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}
