package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Checks: the validation steps a problem comes from (DefinitionProblem.Check).
const (
	checkYAML       = "yaml"       // every file parses
	checkSchema     = "schema"     // factory.yaml satisfies factory.schema.json
	checkLine       = "line"       // the line is well formed
	checkReferences = "references" // every name and path points at something that exists
	checkTools      = "tools"      // runtime.tools are mise tool specs
	checkRemoved    = "removed"    // no form connections replaced is left
	checkIgnored    = "ignored"    // no retired key that does nothing is left
	checkContracts  = "contracts"  // every output contract is a schema the model API takes as a tool's input
	// checkAvailability: what the definition declares that this install cannot serve yet.
	checkAvailability = "availability"
	checkTriggers     = "triggers" // intake triggers' crons, time zones and templates are well formed (ADR 0026)
	// checkEnvironments: environment files and {{secret}} references are well formed (ADR 0037).
	checkEnvironments = "environments"
	// checkMoved: no key ADR 0037 moved to environment files is left. Load skips it, so versions
	// published before still run on the old path until Goal converts them.
	checkMoved = "moved"
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
	codeRouteOverAgents = "route-agents-exceed-station"
	codeCheckRunMissing = "check-run-missing"
	codeBufferOffDrum   = "buffer-not-at-constraint"

	codeIntegrationLandAtNotMerge = "integration-land-at-not-merge"
	codeIntegrationWithoutMerge   = "integration-without-merge-station"

	codeDuplicateName     = "duplicate-name"
	codeUnknownAgent      = "unknown-agent"
	codeUnknownSkill      = "unknown-skill"
	codeUnknownConnection = "unknown-connection"
	codeUnknownContract   = "unknown-contract"
	codeUnknownStation    = "unknown-station"
	codePromptNotFound    = "prompt-not-found"
	codeSkillNotFound     = "skill-not-found"
	codeContractNotFound  = "contract-not-found"
	codeUnknownLane       = "unknown-lane"
	codeUnknownWorkType   = "unknown-work-type"
	codeRuleNotFound      = "rule-not-found"
	codeRuleInvalid       = "rule-invalid"

	// codeKnowledgeTooMany: knowledge.sources lists more than domain.MaxFactorySources.
	codeKnowledgeTooMany = "knowledge-too-many"
	// codeKnowledgeSourceDuplicate: knowledge.sources lists a source name more than once.
	codeKnowledgeSourceDuplicate = "knowledge-source-duplicate"
	// codeMemoryTooLong and codeMemoryTooBig: a memory.yaml entry's text is over
	// domain.MaxMemoryText characters; the file is over domain.MaxMemoryBytes or MaxMemoryEntries.
	codeMemoryTooLong = "memory-too-long"
	codeMemoryTooBig  = "memory-too-big"

	codeContractNotToolSchema = "contract-not-tool-schema"

	codeToolSpecInvalid = "tool-spec-invalid"

	// The forms connections replaced (spec 4 §4.4): errors, each with a quick fix. A version
	// published before the cutover may have them; it loads but does not run (Load).
	codeFieldRemoved        = "field-removed"
	codeEnvReferenceRemoved = "env-reference-removed"
	codeInlineRemoved       = "connection-inline-removed"

	// codeFieldIgnored: a key a decision retired, which stays in the schema marked deprecated and
	// does nothing. A warning, never a removed form: a live version that has it keeps running.
	codeFieldIgnored = "field-ignored"

	codeBuiltinAsk = "builtin-ask-unavailable"

	// codeEvalsIgnored: an evals/ folder, which ADR 0039 decision 21 retired with judges and eval
	// cases. A warning like field-ignored, never a removed form: a live version that has it keeps
	// running.
	codeEvalsIgnored = "evals-ignored"

	codeCronInvalid          = "cron-invalid"
	codeTimeZoneInvalid      = "time-zone-invalid"
	codePlaceholderInvalid   = "placeholder-invalid"
	codeModelQuestionInvalid = "model-question-invalid"

	codeEnvironmentNameInvalid    = "environment-name-invalid"
	codeEnvironmentNameMismatch   = "environment-name-mismatch"
	codeEnvironmentDefaultMissing = "environment-default-missing"
	codeEnvironmentUnknown        = "environment-unknown"
	// codeEnvironmentImage: an environment names an image, which a box on Cloudflare cannot run
	// (the control Worker runs only its own images). A warning: Compose runs it.
	codeEnvironmentImage = "environment-image"
	codeForgeMissing     = "forge-missing"
	codeSecretPlacement  = "secret-placement"
	codeReferenceInvalid = "secret-ref-invalid"

	// codeMovedToEnvironment: a key ADR 0037 moved (spec.connections, an agent's connections,
	// runtime.modelConnection, size, tools and repository.connection, modelAgrees.connection). An
	// error with a fix that removes it; Load skips it.
	codeMovedToEnvironment = "moved-to-environment"
)

// toolSpec is a mise tool spec: an optional backend ("aqua:", "npm:"), a tool name (a path for
// some backends, a scope for npm) and an optional @version. Nothing a shell would read.
var toolSpec = regexp.MustCompile(`^(?:[a-z][a-z0-9-]*:)?@?[A-Za-z0-9][A-Za-z0-9._/+-]*(?:@[A-Za-z0-9][A-Za-z0-9._+:-]*)?$`)

// checker runs the checks that need a decoded factory and collects their problems.
type checker struct {
	f        *Factory
	agents   []string
	stations []string
	// envNames are the names of the definition's environment files, once environments ran.
	envNames []string
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
	c.rules()
	c.knowledge()
	c.memory()
	c.line()
	c.flowAndLearning()
	c.tools()
	c.outputContracts()
	c.removedForms()
	c.ignoredFields(ignoredKeys)
	c.moved()
	c.environments()
	c.evalsIgnored()
	c.triggers()
	c.starter()

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

// duplicates reports a station id, agent, skill, rule or connection name used twice: references
// would silently pick the first.
func (c *checker) duplicates() {
	s := c.f.Spec
	dup(c, "station", names(s.Board.Stations, func(st Station) string { return st.ID }), "board", "stations", "id")
	dup(c, "agent", names(s.Agents, func(a Agent) string { return a.Name }), keyAgents, "", keyName)
	dup(c, "skill", names(s.Skills, func(sk Skill) string { return sk.Name }), "skills", "", keyName)
	dup(c, "rule", names(s.Rules, func(r Rule) string { return r.Name }), "rules", "", keyName)

	for i, conn := range s.Connections {
		p := at("spec", "connections", i)

		switch {
		case conn.Inline() && !c.f.hasKey(append(slices.Clone(p), keyName)):
			p = append(p, "connector")
		case conn.Inline():
			p = append(p, keyName)
		}

		if slices.IndexFunc(s.Connections[:i], func(o Connection) bool { return o.Name == conn.Name }) >= 0 {
			c.errorf(checkReferences, codeDuplicateName, p, "connection %s: the name is used by another connection", conn.Name)
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

// knowledge checks knowledge.sources: at most domain.MaxFactorySources, each source once, and
// each entry's appliesTo names only what the definition declares. Whether the organization has
// the sources is authoring's.
func (c *checker) knowledge() {
	k := c.f.Spec.Knowledge
	if k == nil {
		return
	}

	if n := len(k.Sources); n > domain.MaxFactorySources {
		c.errorf(checkReferences, codeKnowledgeTooMany, at("spec", "knowledge", "sources"),
			"knowledge.sources lists %d sources, more than the %d a factory may list, so that every box gets its files fast; remove some",
			n, domain.MaxFactorySources)
	}

	seen := make(map[string]bool, len(k.Sources))
	for i, s := range k.Sources {
		if seen[s.Name] {
			c.errorf(checkReferences, codeKnowledgeSourceDuplicate, at("spec", "knowledge", "sources", i),
				"knowledge source %s is listed more than once, so a box would unpack it twice; remove this entry", s.Name)
		}

		seen[s.Name] = true

		c.scope("knowledge source "+s.Name, s.AppliesTo, func(key string, j int) []string {
			return at("spec", "knowledge", "sources", i, "appliesTo", key, j)
		})
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

// topLevelRefused are the keywords the model API refuses at the top level of a tool's input_schema,
// the ones its 400 names ("does not support oneOf, allOf, or anyOf at the top level"); it takes a
// top-level not or if/then/else.
var topLevelRefused = []string{"oneOf", "anyOf", "allOf"}

// outputContracts checks each contract a station names as its output once: the worker hands it to
// the model as a tool's input_schema, which must be a plain object at the top level. A contract
// that is missing or does not parse is left to contract-not-found and json-invalid.
func (c *checker) outputContracts() {
	var used []string

	for _, s := range c.f.Spec.Board.Stations {
		if _, ok := c.f.Spec.Contracts[s.Output]; ok && !slices.Contains(used, s.Output) {
			used = append(used, s.Output)
		}
	}

	slices.Sort(used)

	for _, name := range used {
		file := c.f.Spec.Contracts[name]

		raw, err := fs.ReadFile(c.f.files, path.Clean(file))
		if err != nil || !json.Valid(raw) {
			continue
		}

		if why := notToolSchema(raw); why != "" {
			c.errorf(checkContracts, codeContractNotToolSchema, at("spec", "contracts", name),
				"contract %s (%s): %s; the model API takes only a plain object as a tool's input_schema. "+
					"Describe the alternatives in one object (optional fields, an enum field that names the form) "+
					"or nest the choice under a property, and check the alternatives in Go", name, file, why)
		}
	}
}

// notToolSchema says why a contract's top level is not a plain object, "" when it is.
func notToolSchema(raw []byte) string {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return "the top level is not an object"
	}

	for _, k := range topLevelRefused {
		if _, ok := top[k]; ok {
			return k + " at the top level"
		}
	}

	var typ any
	if t, ok := top["type"]; !ok || json.Unmarshal(t, &typ) != nil {
		return `the top level has no "type": "object"`
	}

	if typ != "object" {
		return fmt.Sprintf(`the top level has type %s, not "object"`, top["type"])
	}

	return ""
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

	kept := 0

	for _, lane := range sortedKeys(s.Route) {
		r := s.Route[lane]
		kept += r.Agents

		if !slices.Contains(c.agents, r.Agent) {
			c.errorf(checkReferences, codeUnknownAgent, append(base, "route", lane),
				"station %s: route %s → unknown agent %q", s.ID, lane, r.Agent)
		}
	}

	if agents := max(s.Agents, 1); kept > agents {
		c.errorf(checkLine, codeRouteOverAgents, append(base, "route"),
			"station %s: route keeps %d slots but the station has %d agents", s.ID, kept, agents)
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
		ReplaceFix(fmt.Sprintf("Set WIP to %d", s.Agents), append(slices.Clone(base), "wip"), s.Agents),
		ReplaceFix(fmt.Sprintf("Set agents to %d", s.WIP), append(slices.Clone(base), keyAgents), s.WIP),
	}
}

func (c *checker) flowAndLearning() {
	c.constraint()
	c.integration()
}

// constraint checks that flow.constraint names a station and warns about buffers away from it:
// a buffer is kept only in front of the drum. With a detected constraint (auto) any station
// may become the drum.
func (c *checker) constraint() {
	fl := c.f.Spec.Board.Flow
	if fl == nil || fl.Constraint == "" || fl.Constraint == "auto" {
		return
	}

	if !slices.Contains(c.stations, fl.Constraint) {
		c.errorf(checkReferences, codeUnknownStation, at("spec", "board", "flow", "constraint"),
			"flow.constraint %q is not a station", fl.Constraint)

		return
	}

	for i, s := range c.f.Spec.Board.Stations {
		if s.Buffer > 0 && s.ID != fl.Constraint {
			c.warnf(checkLine, codeBufferOffDrum, at("spec", "board", "stations", i, "buffer"),
				"station %s: a buffer matters only in front of the constraint, %s", s.ID, fl.Constraint)
		}
	}
}

// integration checks flow.assemblies.integration: landAt names an agent station with a gate and
// action merge, where the integration branch lands, and mode branch needs a merge station on the
// line (ADR 0033).
func (c *checker) integration() {
	fl := c.f.Spec.Board.Flow
	if fl == nil || fl.Assemblies == nil || fl.Assemblies.Integration == nil {
		return
	}

	in, stations := fl.Assemblies.Integration, c.f.Spec.Board.Stations
	base := at("spec", "board", "flow", "assemblies", "integration")

	if in.LandAt != "" {
		switch i := slices.IndexFunc(stations, func(s Station) bool { return s.ID == in.LandAt }); {
		case i < 0:
			c.errorf(checkReferences, codeUnknownStation, append(base, "landAt"),
				"flow.assemblies.integration.landAt %q is not a station", in.LandAt)
		case !landing(stations[i]):
			c.errorf(checkLine, codeIntegrationLandAtNotMerge, append(base, "landAt"),
				"flow.assemblies.integration.landAt %q must be an agent station with a gate and action merge", in.LandAt)
		}
	}

	merges := slices.ContainsFunc(stations, func(s Station) bool { return s.Action == string(domain.ActionMerge) })
	if in.Mode == string(domain.IntegrationModeBranch) && !merges {
		c.errorf(checkLine, codeIntegrationWithoutMerge, append(base, "mode"),
			"flow.assemblies.integration.mode branch needs a station with action merge, where the integration branch lands")
	}
}

// landing reports whether an integration branch can land at s: an agent station whose gate's
// approval merges.
func landing(s Station) bool {
	return s.Kind == string(domain.KindAgent) && s.Gate != nil && s.Action == string(domain.ActionMerge)
}

// tools refuses a runtime.tools entry that is not a mise tool spec: the box installs each one.
func (c *checker) tools() {
	for i, t := range c.f.Spec.Runtime.Tools {
		if toolSpec.MatchString(t) {
			continue
		}

		p := at("spec", "runtime", "tools", i)
		c.errorf(checkTools, codeToolSpecInvalid, p,
			"runtime.tools: %q is not a mise tool spec, such as go, go@1.26.10 or npm:@acme/cli@2", t).
			Fixes = []domain.ProblemFix{RemoveFix("Remove "+t, p)}
	}
}

// removedKeys are the keys phase 4 removed (field-removed), with what replaces each.
var removedKeys = []struct {
	path []string
	why  string
}{
	{at("spec", "runtime", "runner"), "the platform chooses where runs start"},
	{at("spec", "runtime", "image"), "every run starts from the platform's agent image and installs the repository's tools and runtime.tools"},
	{at("spec", "runtime", "auth"), "set runtime.modelConnection to an organization connection with model access"},
	{at("spec", "knowledgeBase"), "list the organization's knowledge sources the factory may search in knowledge.sources"},
	{at("spec", "secrets"), "the organization's connections hold its credentials"},
}

// removedForms names each form connections replaced that the definition still has, with a fix
// that takes it out (spec 4 §4.4). They are errors, and the factory records each by path and
// code for its line (domain.Line.Removed): goald refuses to run a version published before the
// cutover that has one.
func (c *checker) removedForms() {
	for _, k := range removedKeys {
		if !c.f.hasKey(k.path) {
			continue
		}

		name := strings.Join(k.path[1:], ".")
		c.removed(k.path, codeFieldRemoved, "%s is removed: %s", name, k.why).
			Fixes = []domain.ProblemFix{RemoveFix("Remove "+name, k.path)}
	}

	if p := at("spec", "runtime", "repository", "token"); c.f.hasKey(p) && !strings.Contains(c.f.Spec.Runtime.Repository.Token, "{{") {
		c.removed(p, codeEnvReferenceRemoved,
			"runtime.repository.token: only a {{secret}} reference is valid here, such as {{github-token}}; references of other forms are removed").
			Fixes = []domain.ProblemFix{RemoveFix("Remove the token", p)}
	}

	for i, conn := range c.f.Spec.Connections {
		if !conn.Inline() {
			continue
		}

		p := at("spec", "connections", i)
		c.removed(p, codeInlineRemoved,
			"connection %s: inline connections are removed; create an organization connection named %s (for example of kind mcp-server) "+
				"and list it here by name, with its credentials in the connection", conn.Name, conn.Name).
			Fixes = []domain.ProblemFix{ReplaceFix("Use the organization's connection "+conn.Name, p, map[string]string{keyName: conn.Name})}
	}
}

// ignoredKey is a key a decision retired, with what replaces it.
type ignoredKey struct {
	path []string
	why  string
}

// ignoredKeys are the keys ADR 0039 retired (decision 17).
var ignoredKeys = []ignoredKey{
	{at("spec", "learning", "jonah", "weeklyUsd"), "Jonah's budget is learning.jonah.share of what the line spent in the last 7 days"},
	{at("spec", "learning", "librarian"), "Jonah proposes knowledge from the line"},
	{at("spec", "learning", "kaizen"), "Jonah reviews the line once a day"},
}

// ignoredFields warns, code field-ignored, about each key of keys the definition still has. It
// never goes through removed: a removed form refuses every run of a version that has it
// (domain.Line.Refusal), and these keys must not stop a live factory.
func (c *checker) ignoredFields(keys []ignoredKey) {
	for _, k := range keys {
		if c.f.hasKey(k.path) {
			c.warnf(checkIgnored, codeFieldIgnored, k.path, "%s is ignored: %s; remove it", strings.Join(k.path[1:], "."), k.why)
		}
	}
}

// EvalsDir is the folder that held a definition's judges and eval cases (ADR 0025), which ADR 0039
// decision 21 retired: Load and Check ignore what it holds.
const EvalsDir = "evals"

// evalsIgnored warns, code evals-ignored, when the definition still has an evals/ folder. Like
// ignoredFields it never goes through removed, so a live version that has one keeps running.
func (c *checker) evalsIgnored() {
	if fi, err := fs.Stat(c.f.files, EvalsDir); err != nil || !fi.IsDir() {
		return
	}

	c.problems = append(c.problems, domain.DefinitionProblem{
		Code: codeEvalsIgnored, Severity: domain.SeverityWarning, Check: checkIgnored, Section: domain.SectionFiles,
		File:    EvalsDir + "/",
		Message: EvalsDir + "/ is ignored: Jonah's kept tests replace eval cases and judges; remove it",
	})
}

// movedKeys are the keys ADR 0037 moved to environment files and secret references, with where
// each went.
var movedKeys = []struct {
	path []string
	to   string
}{
	{at("spec", "connections"), "what agents reach is listed in environments/<name>.yaml (env, files and mcp)"},
	{at("spec", "runtime", "modelConnection"), "set runtime.modelKey to the {{secret}} that holds the model key"},
	{at("spec", "runtime", "size"), "set size in each environment file"},
	{at("spec", "runtime", "tools"), "set tools in each environment file"},
	{at("spec", "runtime", "repository", "connection"), "set runtime.repository.token to the {{secret}} that clones and merges"},
}

// movedNote ends every moved-to-environment message.
const movedNote = "Goal converts published versions itself (ADR 0037); remove it from this draft"

// moved names each key ADR 0037 moved that the definition still has, with a fix that removes it.
// They are errors, so no draft that has one publishes, but Load skips them and they are not
// removed forms: a version published before runs on the old path until Goal converts it.
func (c *checker) moved() {
	for _, k := range movedKeys {
		if !c.f.hasKey(k.path) {
			continue
		}

		name := strings.Join(k.path[1:], ".")
		fix := RemoveFix("Remove "+name, k.path)

		if k.path[1] == "connections" {
			fix = ReplaceFix("Remove "+name, k.path, []any{}) // the section is the list; an empty one writes nothing
		}

		c.errorf(checkMoved, codeMovedToEnvironment, k.path, "%s moved to environment files: %s. %s", name, k.to, movedNote).
			Fixes = []domain.ProblemFix{fix}
	}

	for i, a := range c.f.Spec.Agents {
		if p := at("spec", keyAgents, i, "connections"); c.f.hasKey(p) {
			c.errorf(checkMoved, codeMovedToEnvironment, p,
				"agent %s: connections moved to environment files: set the agent's environment to the file that lists what it reaches. %s",
				a.Name, movedNote).Fixes = []domain.ProblemFix{RemoveFix("Remove the agent's connections", p)}
		}
	}

	// Triggers are edited in YAML only (no model section), so this one has no fix to apply.
	for i, t := range c.f.Spec.Board.Triggers {
		if p := at("spec", "board", "triggers", i, "only", "modelAgrees", "connection"); c.f.hasKey(p) {
			c.errorf(checkMoved, codeMovedToEnvironment, p,
				"trigger %s: only.modelAgrees.connection moved: set only.modelAgrees.key to a {{secret}}, or leave it out for runtime.modelKey. %s",
				t.Name, movedNote)
		}
	}
}

// removed records a removed form at path: an error, and its place in the factory's removed list.
func (c *checker) removed(path []string, code, format string, a ...any) *domain.DefinitionProblem {
	c.f.removed = append(c.f.removed, fmt.Sprintf("%s (%s)", displayPath(path), code))

	return c.errorf(checkRemoved, code, path, format, a...)
}

// ReplaceFix is a fix that replaces the value at path (a factory.yaml path) with v, as a patch
// on the model section the path is in.
func ReplaceFix(label string, path []string, v any) domain.ProblemFix {
	section, _ := ModelPointer(path)
	value, _ := json.Marshal(v)

	return domain.ProblemFix{
		Label: label, Section: section,
		Patch: []domain.PatchOp{{Op: domain.PatchReplace, Path: sectionPath(path), Value: value}},
	}
}

// RemoveFix is a fix that removes the value at path (a factory.yaml path), as a patch on the model
// section the path is in.
func RemoveFix(label string, path []string) domain.ProblemFix {
	section, _ := ModelPointer(path)

	return domain.ProblemFix{Label: label, Section: section, Patch: []domain.PatchOp{{Op: domain.PatchRemove, Path: sectionPath(path)}}}
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

// otherFileProblems reports YAML and JSON files besides factory.yaml (contracts, environments)
// that do not parse. Hidden folders (a working copy's .goal/) and evals/, which is ignored, are
// skipped.
func otherFileProblems(files fs.FS) []domain.DefinitionProblem {
	var out []domain.DefinitionProblem

	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil //nolint:nilerr // an unreadable entry is reported by whatever references it
		case d.IsDir() && p != "." && strings.HasPrefix(d.Name(), "."), d.IsDir() && p == EvalsDir:
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
