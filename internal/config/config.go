// Package config loads and validates a factory definition: factory.yaml at the root of a
// file tree that holds its prompts, skills, rules, contracts and evals. The tree is an fs.FS, so the
// same loader reads a local working copy or a version's files from the bucket.
//
// Check validates in passes and reports every finding as a located domain.DefinitionProblem
// (a stable code, a severity, the Visual section and model pointer, the file, line and
// column): every file parses, factory.yaml satisfies the JSON Schema, and then what a schema
// cannot say: every reference (agent, skill, connection, station, contract, file) points at
// something that exists, the line is well formed, tool specs are mise's, and the forms
// connections replaced are named with a quick fix. Whether the organization connections and
// knowledge sources it names exist is authoring's connections check. Load is Check for callers
// that run published versions.
package config

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Factory is one parsed factory.yaml.
type Factory struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	// Starter is what a new factory made from the definition starts with (ADR 0032); nil when
	// factory.yaml has none.
	Starter *Starter `json:"starter,omitempty"`

	// files is the factory's file tree; relative paths in the spec resolve against its root.
	files fs.FS
	// doc is factory.yaml's node tree, which locates problems; nil for a Factory not loaded.
	doc *document
	// warnings are the problems found at load that do not block it.
	warnings []domain.DefinitionProblem
	// removed names each form connections replaced that the definition has (domain.Line.Removed).
	removed []string
}

// Metadata identifies a factory.
type Metadata struct {
	// ID never changes; everything persistent (rows, bucket keys, AWS resources) keys on it.
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	// Purpose says what the factory is for; agents read it. Description is its deprecated
	// alias, read when Purpose is empty.
	Purpose     string `json:"purpose,omitempty"`
	Description string `json:"description,omitempty"`
	Owner       string `json:"owner,omitempty"`
}

// Spec is everything a factory declares.
type Spec struct {
	Runtime Runtime `json:"runtime"`
	// Connections are the organization's connections the factory uses, and the removed inline
	// ones of versions published before the cutover.
	Connections []Connection      `json:"connections,omitempty"`
	Knowledge   *Knowledge        `json:"knowledge,omitempty"`
	Skills      []Skill           `json:"skills,omitempty"`
	Rules       []Rule            `json:"rules,omitempty"`
	Contracts   map[string]string `json:"contracts,omitempty"`
	Agents      []Agent           `json:"agents"`
	Board       Board             `json:"board"`
	Learning    *Learning         `json:"learning,omitempty"`
	// KnowledgeBase is removed (field-removed); the librarian's writes still name its collections.
	KnowledgeBase *KnowledgeBase `json:"knowledgeBase,omitempty"`
}

// Runtime says what every run of the factory gets: its model and the connection that gives it,
// the size of its box, the tools it installs and the repository it works on.
type Runtime struct {
	Model           string   `json:"model,omitempty"`
	ModelConnection string   `json:"modelConnection,omitempty"`
	Size            string   `json:"size,omitempty"`
	Tools           []string `json:"tools,omitempty"`
	// Repository is the git repository runs work on; nil means an empty workspace.
	Repository *Repository `json:"repository,omitempty"`
	Budget     *Budget     `json:"budget,omitempty"`
}

// Repository is the git repository cloned into every run: a URL and the organization connection
// that clones it (none for a public repository). The removed token (env-reference-removed) is
// found in the document, not decoded.
type Repository struct {
	URL        string `json:"url"`
	Connection string `json:"connection,omitempty"`
}

// Budget caps spend and agent turns.
type Budget struct {
	DailyUSD  float64 `json:"dailyUsd,omitempty"`
	WeeklyUSD float64 `json:"weeklyUsd,omitempty"`
	// OnExhausted is drain (the default) or stop.
	OnExhausted     string  `json:"onExhausted,omitempty"`
	PerItemUSD      float64 `json:"perItemUsd,omitempty"`
	MaxTurnsPerTask int     `json:"maxTurnsPerTask,omitempty"`
}

// Knowledge lists the organization's knowledge sources the factory's agents may search.
type Knowledge struct {
	Sources []string `json:"sources,omitempty"`
}

// KnowledgeBase is the removed knowledge base's collections.
type KnowledgeBase struct {
	Collections []string `json:"collections"`
}

// Connection is an entry of spec.connections: the name of an organization connection, written
// as a plain string, or a removed inline form (connection-inline-removed) that versions published
// before the cutover may still have, an object with a type or a connector, of which only the
// name is kept.
type Connection struct {
	// Name is unique in the factory; a connector-form entry without one is named after its
	// connector (name@version), as the schema documented for that form.
	Name   string `json:"name"`
	inline bool
}

// UnmarshalJSON reads an organization connection's name, or the name of an inline connection.
func (c *Connection) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &c.Name); err == nil {
		return nil
	}

	var form struct {
		Name      string `json:"name"`
		Connector string `json:"connector"`
	}

	if err := json.Unmarshal(data, &form); err != nil {
		return err
	}

	connector, _, _ := strings.Cut(form.Connector, "@")
	c.Name, c.inline = cmp.Or(form.Name, connector), true

	return nil
}

// Inline reports whether the entry is a removed inline form rather than an organization
// connection's name.
func (c Connection) Inline() bool { return c.inline }

// Skill is a SKILL.md folder, local or from skills.sh.
type Skill struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	// Origin is where a vendored skill came from (skills.sh:<owner>/<repo>@<skill>#<commit>);
	// Source is then the local folder it was copied into.
	Origin string `json:"origin,omitempty"`
}

// Local reports whether the skill ships inside the factory folder rather than from skills.sh.
func (s Skill) Local() bool { return strings.HasPrefix(s.Source, "./") }

// Rule is a factory rule (ADR 0014): its entry in spec.rules and, once Check has read the rule
// file, what the file says. A rule file that does not parse leaves those fields zero.
type Rule struct {
	Name   string `json:"name"`
	Source string `json:"source"`

	Description string           `json:"-"`
	AppliesTo   domain.RuleScope `json:"-"`
	Paths       []string         `json:"-"`
	Body        string           `json:"-"`
}

// Agent is a worker definition: prompt, model, limits, skills, connections and tool policy.
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model,omitempty"`
	// MaxTurns and PerItemUSD override the runtime budget for this agent; zero means unset.
	MaxTurns    int      `json:"maxTurns,omitempty"`
	PerItemUSD  float64  `json:"perItemUsd,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Connections []string `json:"connections,omitempty"`
	// Tools are custom rules in Claude Code's permission syntax; Builtins replaces them.
	Tools *struct {
		Allow []string `json:"allow,omitempty"`
		Deny  []string `json:"deny,omitempty"`
	} `json:"tools,omitempty"`
	Builtins *Builtins `json:"builtins,omitempty"`
}

// Built-in tool modes.
const (
	BuiltinOff     = "off"
	BuiltinAsk     = "ask"
	BuiltinAllowed = "allowed"
)

// Builtins sets an agent's built-in tools by group. Each mode is off, ask or allowed; an empty
// mode is off.
type Builtins struct {
	Read      string     `json:"read,omitempty"`
	Edit      string     `json:"edit,omitempty"`
	Run       BuiltinRun `json:"run,omitzero"`
	Web       string     `json:"web,omitempty"`
	Subagents string     `json:"subagents,omitempty"`
}

// BuiltinRun is the Bash group: a mode and, optionally, the only commands it may run.
type BuiltinRun struct {
	Mode     string   `json:"mode,omitempty"`
	Commands []string `json:"commands,omitempty"`
}

// UnmarshalJSON reads run as a mode ("allowed") or as {mode, commands}.
func (r *BuiltinRun) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &r.Mode); err == nil {
		return nil
	}

	type plain BuiltinRun

	return json.Unmarshal(data, (*plain)(r))
}

// Board is the factory's line: intake, stations and flow policy.
type Board struct {
	WorkTypes []string  `json:"workTypes,omitempty"`
	Intake    []Intake  `json:"intake,omitempty"`
	Triggers  []Trigger `json:"triggers,omitempty"`
	Stations  []Station `json:"stations"`
	Flow      *Flow     `json:"flow,omitempty"`
}

// Intake is one source of new work items. Deprecated and ignored: Triggers replaces it (ADR 0026).
type Intake struct {
	Source   string   `json:"source"`
	Repo     string   `json:"repo,omitempty"`
	Labels   []string `json:"labels,omitempty"`
	Cron     string   `json:"cron,omitempty"`
	Template string   `json:"template,omitempty"`
	Type     string   `json:"type,omitempty"`
}

// Trigger turns an event, a schedule or a webhook delivery into a work item (ADR 0026).
type Trigger struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled,omitempty"` // nil means enabled
	On      string `json:"on"`
	// Cron (five fields) and TimeZone (IANA, "" for UTC) are set only when On is "schedule".
	Cron     string         `json:"cron,omitempty"`
	TimeZone string         `json:"timeZone,omitempty"`
	Filter   *TriggerFilter `json:"filter,omitempty"`
	Only     *TriggerOnly   `json:"only,omitempty"`
	Create   TriggerCreate  `json:"create"`
	Limits   *TriggerLimits `json:"limits,omitempty"`
}

// TriggerFilter holds filters on an event's fields; every one set must match.
type TriggerFilter struct {
	Branch   string   `json:"branch,omitempty"`
	Repo     string   `json:"repo,omitempty"`
	Label    string   `json:"label,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Authors  []string `json:"authors,omitempty"`
	SkipBots bool     `json:"skipBots,omitempty"`
}

// TriggerOnly holds the checks made before an item is created.
type TriggerOnly struct {
	FailuresInARow int    `json:"failuresInARow,omitempty"`
	Dedupe         string `json:"dedupe,omitempty"`
}

// TriggerCreate is the item a trigger creates. Title and Brief are templates whose {{field}}
// placeholders take the event's fields.
type TriggerCreate struct {
	Type     string `json:"type"`
	Station  string `json:"station"`
	Lane     string `json:"lane,omitempty"`
	Expedite bool   `json:"expedite,omitempty"`
	Title    string `json:"title,omitempty"`
	Brief    string `json:"brief,omitempty"`
}

// TriggerLimits caps what a trigger creates.
type TriggerLimits struct {
	PerDay        int  `json:"perDay,omitempty"`
	PauseOnBudget bool `json:"pauseOnBudget,omitempty"`
}

// Station is one step of the line.
type Station struct {
	ID string `json:"id"`
	// Name and Description are for display; ID is what everything references.
	Name        string                `json:"name,omitempty"`
	Description string                `json:"description,omitempty"`
	Kind        string                `json:"kind"`
	Agent       string                `json:"agent,omitempty"`
	Route       map[string]RouteEntry `json:"route,omitempty"` // item lane → agent
	Agents      int                   `json:"agents,omitempty"`
	WIP         int                   `json:"wip,omitempty"`
	Buffer      int                   `json:"buffer,omitempty"`
	Output      string                `json:"output,omitempty"`
	Checks      []StationCheck        `json:"checks,omitempty"`
	Gate        *Gate                 `json:"gate,omitempty"`
	// Action makes goald act when the item is approved at this station, a human station or an
	// agent station with a gate: "merge" merges the item's pull requests first.
	Action string `json:"action,omitempty"`
	// Context names what goald preloads into the station's runs: "inflight" for the factory's
	// open work.
	Context []string `json:"context,omitempty"`
}

// RouteEntry is the agent a station's route picks for a lane and, when Agents is set, the
// station's slots kept for that lane.
type RouteEntry struct {
	Agent  string `json:"agent"`
	Agents int    `json:"agents,omitempty"`
}

// UnmarshalJSON reads a route entry object or its shorthand, the agent's name.
func (r *RouteEntry) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*r = RouteEntry{Agent: s}

		return nil
	}

	type plain RouteEntry

	return json.Unmarshal(data, (*plain)(r))
}

// MarshalJSON writes the shorthand when the entry sets no count.
func (r RouteEntry) MarshalJSON() ([]byte, error) {
	if r.Agents == 0 {
		return json.Marshal(r.Agent)
	}

	type plain RouteEntry

	return json.Marshal(plain(r))
}

// ActionMerge is the station action that merges the item's pull request on approval.
const ActionMerge = "merge"

// Check kinds.
const (
	// CheckSchema validates the output against the station's contract (always done when the
	// station has an output).
	CheckSchema = "schema"
	// CheckEvidenceLinks requires links to evidence in the output.
	CheckEvidenceLinks = "evidence-links"
	// CheckCommand runs a command against the run's workspace in an isolated container.
	CheckCommand = "command"
)

// StationCheck is a deterministic gate run before any reviewer sees the output.
type StationCheck struct {
	Kind string `json:"kind"`
	Min  int    `json:"min,omitempty"`
	Run  string `json:"run,omitempty"`
}

// UnmarshalJSON reads a check object or its shorthand: "schema", "evidence-links", or any
// other string as the command line of a command check.
func (c *StationCheck) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch s {
		case CheckSchema, CheckEvidenceLinks:
			*c = StationCheck{Kind: s}
		default:
			*c = StationCheck{Kind: CheckCommand, Run: s}
		}

		return nil
	}

	type plain StationCheck

	return json.Unmarshal(data, (*plain)(c))
}

// Gate sends rejected work back upstream, at most MaxRework times.
type Gate struct {
	OnReject  string `json:"onReject"`
	MaxRework int    `json:"maxRework,omitempty"`
}

// Flow holds the line's pull policy (expedite lane, constraint, release and andon), its
// targets, its assembly policy, its retry policy and the output fields that set dependencies.
type Flow struct {
	Expedite *struct {
		Types []string `json:"types,omitempty"`
		WIP   int      `json:"wip,omitempty"`
	} `json:"expedite,omitempty"`
	Constraint string `json:"constraint,omitempty"`
	Release    string `json:"release,omitempty"`
	Andon      *struct {
		ReworkRate float64 `json:"reworkRate,omitempty"`
		Window     int     `json:"window,omitempty"`
	} `json:"andon,omitempty"`
	Targets    *Targets    `json:"targets,omitempty"`
	Assemblies *Assemblies `json:"assemblies,omitempty"`
	Retry      *Retry      `json:"retry,omitempty"`
	// Dependencies maps the output fields that set an item's dependencies.
	Dependencies *Dependencies `json:"dependencies,omitempty"`
}

// Dependencies names the output fields that set an item's dependencies: After the one listing
// the open items (<factory>#<number>) the work must follow, Paths the one whose list of file
// globs replaces the item's paths. An unset key maps no field.
type Dependencies struct {
	After string `json:"after,omitempty"`
	Paths string `json:"paths,omitempty"`
}

// Retry caps the runs again of a claim whose run failed for a transient reason: its sandbox
// was lost. Transient.Attempts unset is domain.DefaultTransientAttempts.
type Retry struct {
	Transient *struct {
		Attempts int `json:"attempts,omitempty"`
	} `json:"transient,omitempty"`
}

// Assemblies says how runs keep item branches fresh (rebase or merge), what a merge conflict
// does (rework or human) and whether items whose files overlap wait for each other (serialize
// or allow), whether a station's plan becomes an assembly with its parts (materialize) and
// whether later runs hear what changed on the default branch since a plan (changedSince); an
// unset key takes its default, rebase, rework, serialize, true and false.
type Assemblies struct {
	FreshBase    string `json:"freshBase,omitempty"`
	OnConflict   string `json:"onConflict,omitempty"`
	Overlap      string `json:"overlap,omitempty"`
	Materialize  *bool  `json:"materialize,omitempty"`
	ChangedSince bool   `json:"changedSince,omitempty"`
}

// Targets are what the line aims for; an unset target is zero.
type Targets struct {
	ThroughputPerDay float64 `json:"throughputPerDay,omitempty"`
	// LeadTimeHours is the target p85 lead time, created to done.
	LeadTimeHours           float64 `json:"leadTimeHours,omitempty"`
	ExpediteLeadTimeMinutes int     `json:"expediteLeadTimeMinutes,omitempty"`
}

// Learning configures the librarian and the kaizen report.
type Learning struct {
	Librarian *struct {
		Agent  string   `json:"agent"`
		After  []string `json:"after,omitempty"`
		Writes []string `json:"writes,omitempty"`
	} `json:"librarian,omitempty"`
	Kaizen *struct {
		Schedule string `json:"schedule,omitempty"`
	} `json:"kaizen,omitempty"`
}

// FileName is the factory definition at the root of a factory's file tree.
const FileName = "factory.yaml"

// LoadDir loads the factory in a local folder, such as a working copy pulled from goald.
func LoadDir(dir string) (*Factory, error) {
	return Load(os.DirFS(dir))
}

// Load is Check for callers that run published versions: it fails with every blocking problem,
// one per line, when there is any, but the forms connections replaced. A version published
// before the cutover may have those; it loads, so its items stay visible, and its line names
// them (domain.Line.Removed), so the engine refuses to run it. Referenced paths resolve inside
// files only; a path that leaves it is not found.
func Load(files fs.FS) (*Factory, error) {
	f, problems := Check(files)

	var errs []error

	for _, p := range problems {
		if p.Blocking() && p.Check != checkRemoved {
			errs = append(errs, problemError(p))
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return f, nil
}

// problemError writes a problem as path:line:column: message (code).
func problemError(p domain.DefinitionProblem) error {
	where := p.File
	if p.Line > 0 {
		where = fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
	}

	return fmt.Errorf("%s: %s (%s)", where, p.Message, p.Code)
}

// Check validates the factory at the root of files and reports every problem, blocking or
// not. The factory is nil when a file does not parse or factory.yaml does not satisfy the
// schema; otherwise it is returned with its problems, so a caller can still read a definition
// whose references are broken.
func Check(files fs.FS) (*Factory, []domain.DefinitionProblem) {
	problems := otherFileProblems(files)

	raw, err := fs.ReadFile(files, FileName)
	if err != nil {
		return nil, append(problems, domain.DefinitionProblem{
			Code: codeFactoryMissing, Severity: domain.SeverityError, Check: checkYAML, Section: domain.SectionFiles,
			File: FileName, Message: fmt.Sprintf("read %s: %v", FileName, err),
		})
	}

	doc, bad := parseDocument(raw)
	if bad != nil {
		return nil, append(problems, *bad)
	}

	if sp := doc.schemaProblems(); len(sp) > 0 {
		return nil, append(problems, sp...)
	}

	f := &Factory{files: files, doc: doc}
	if err := json.Unmarshal(doc.json, f); err != nil {
		return nil, append(problems, *yamlInvalid(FileName, err))
	}

	found := newChecker(f).run()
	for _, p := range found {
		if !p.Blocking() {
			f.warnings = append(f.warnings, p)
		}
	}

	return f, append(problems, found...)
}

// Warnings lists the messages of what loads but should be fixed, such as what this install
// cannot serve yet.
func (f *Factory) Warnings() []string {
	w := make([]string, 0, len(f.warnings))
	for _, p := range f.warnings {
		w = append(w, p.Message)
	}

	return w
}

// ConnectionNames are the organization's connections the definition names (spec 4 §4.4), sorted
// and unique: spec.connections but its inline entries, runtime.modelConnection and
// runtime.repository.connection. A connection a current version names is neither deleted nor
// renamed.
func (f *Factory) ConnectionNames() []string {
	names := make([]string, 0, len(f.Spec.Connections)+2)
	for _, c := range f.Spec.Connections {
		if !c.Inline() {
			names = append(names, c.Name)
		}
	}

	if m := f.Spec.Runtime.ModelConnection; m != "" {
		names = append(names, m)
	}

	if r := f.Spec.Runtime.Repository; r != nil && r.Connection != "" {
		names = append(names, r.Connection)
	}

	slices.Sort(names)

	return slices.Compact(names)
}

// Purpose is what the factory is for: metadata.purpose, or its deprecated alias description.
func (f *Factory) Purpose() string {
	if f.Metadata.Purpose != "" {
		return f.Metadata.Purpose
	}

	return f.Metadata.Description
}
