// Package config loads and validates a factory definition: factory.yaml at the root of a
// file tree that holds its prompts, skills, contracts and evals. The tree is an fs.FS, so the
// same loader reads a local working copy or a version's files from the bucket.
//
// Check validates in passes and reports every finding as a located domain.DefinitionProblem
// (a stable code, a severity, the Visual section and model pointer, the file, line and
// column): every file parses, factory.yaml satisfies the JSON Schema, and then what a schema
// cannot say: every reference (agent, skill, connection, station, contract, file) points at
// something that exists, the line is well formed, images are pinned and secrets are
// references. Load is Check for callers that only need a valid factory.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/byFrederick/goal/internal/domain"
)

// Factory is one parsed factory.yaml.
type Factory struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`

	// files is the factory's file tree; relative paths in the spec resolve against its root.
	files fs.FS
	// doc is factory.yaml's node tree, which locates problems; nil for a Factory not loaded.
	doc *document
	// warnings are the problems found at load that do not block it.
	warnings []domain.DefinitionProblem
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
	Runtime       Runtime           `json:"runtime"`
	KnowledgeBase KnowledgeBase     `json:"knowledgeBase"`
	Secrets       *Secrets          `json:"secrets,omitempty"`
	Connections   []Connection      `json:"connections,omitempty"`
	Skills        []Skill           `json:"skills,omitempty"`
	Contracts     map[string]string `json:"contracts,omitempty"`
	Agents        []Agent           `json:"agents"`
	Board         Board             `json:"board"`
	Learning      *Learning         `json:"learning,omitempty"`
}

// Runtime says where agents run and how they authenticate to the model.
type Runtime struct {
	Runner string `json:"runner"`
	Image  string `json:"image"`
	Auth   string `json:"auth"`
	Model  string `json:"model,omitempty"`
	// Repository is the git repository agent tasks work on; nil means an empty workspace.
	Repository *Repository `json:"repository,omitempty"`
	Budget     *Budget     `json:"budget,omitempty"`
}

// Repository is the git repository goald clones into every task's workspace: a URL and a token
// reference, or a code-host connection and the repository's owner/name (phase 4).
type Repository struct {
	URL string `json:"url,omitempty"`
	// Token is a secret reference used for the clone on the host only.
	Token string `json:"token,omitempty"`
	// Connection names a connection of the factory whose connector is a code host.
	Connection string `json:"connection,omitempty"`
	// Repo is owner/name on that connection's code host.
	Repo string `json:"repo,omitempty"`
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

// KnowledgeBase lists the factory's knowledge base collections.
type KnowledgeBase struct {
	Collections []string `json:"collections"`
	Retrieval   *struct {
		TopK int `json:"topK,omitempty"`
	} `json:"retrieval,omitempty"`
	Ingestion *struct {
		Schedule string `json:"schedule,omitempty"`
	} `json:"ingestion,omitempty"`
}

// Secrets names where the factory's credentials are resolved.
type Secrets struct {
	Provider string `json:"provider"`
	Vault    string `json:"vault,omitempty"`
}

// Connection is a tool source served to agents through the gateway, in one of two forms: the
// inline form (Type and its transport fields, served today) or the connector form (Connector
// from the catalog, with Credential, Agents, Tools and connector Settings; phase 4).
type Connection struct {
	// Name is unique in the factory. In the connector form it defaults to the connector's name,
	// which Check fills in, so Name is always the name agents and tokens use.
	Name      string            `json:"name"`
	Type      string            `json:"type,omitempty"`
	Transport string            `json:"transport,omitempty"`
	URL       string            `json:"url,omitempty"`
	Image     string            `json:"image,omitempty"`
	Command   []string          `json:"command,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	// Network gives a stdio connection's sidecar outbound network access.
	Network bool   `json:"network,omitempty"`
	RoleARN string `json:"roleArn,omitempty"`
	Region  string `json:"region,omitempty"`

	// Connector is the catalog connector of the connector form: name or name@version.
	Connector string `json:"connector,omitempty"`
	// Credential is the organization credential the connection signs in with (phase 4).
	Credential string              `json:"credential,omitempty"`
	Agents     []string            `json:"agents,omitempty"`
	Tools      map[string]ToolRule `json:"tools,omitempty"`
	// Settings are the connector form's other keys, the connector's own settings.
	Settings map[string]json.RawMessage `json:"-"`
}

// connectionKeys are the keys of Connection itself; any other key of a connector-form
// connection is a setting.
var connectionKeys = []string{
	keyName, "type", "transport", "url", "image", "command", "headers", "env", "network", "roleArn", "region",
	"connector", "credential", keyAgents, "tools",
}

// UnmarshalJSON decodes a connection and keeps a connector-form entry's settings.
func (c *Connection) UnmarshalJSON(data []byte) error {
	type plain Connection

	if err := json.Unmarshal(data, (*plain)(c)); err != nil {
		return err
	}

	if c.Connector == "" {
		return nil
	}

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}

	for _, k := range connectionKeys {
		delete(all, k)
	}

	if len(all) > 0 {
		c.Settings = all
	}

	return nil
}

// ConnectorName is the connector's name without its version: "playwright" for
// "playwright@2"; empty for an inline connection.
func (c Connection) ConnectorName() string {
	name, _, _ := strings.Cut(c.Connector, "@")

	return name
}

// ToolRule overrides a catalog default for one tool of a connector-form connection.
type ToolRule struct {
	// Enabled false means the tool is never served; nil keeps the catalog's default.
	Enabled  *bool    `json:"enabled,omitempty"`
	Agents   []string `json:"agents,omitempty"`
	Approval string   `json:"approval,omitempty"` // required | none
	Scope    string   `json:"scope,omitempty"`
	Access   string   `json:"access,omitempty"`
}

// Pinned reports whether a container image reference names one version: it has a digest
// (name@sha256:...) or a tag other than latest.
func Pinned(image string) bool {
	if _, digest, ok := strings.Cut(image, "@"); ok {
		return strings.HasPrefix(digest, "sha256:") && len(digest) == len("sha256:")+64
	}

	name := image[strings.LastIndex(image, "/")+1:]
	_, tag, ok := strings.Cut(name, ":")

	return ok && tag != "" && tag != "latest"
}

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
	Stations  []Station `json:"stations"`
	Flow      *Flow     `json:"flow,omitempty"`
}

// Intake is one source of new work items.
type Intake struct {
	Source   string   `json:"source"`
	Repo     string   `json:"repo,omitempty"`
	Labels   []string `json:"labels,omitempty"`
	Cron     string   `json:"cron,omitempty"`
	Template string   `json:"template,omitempty"`
	Type     string   `json:"type,omitempty"`
}

// Station is one step of the line.
type Station struct {
	ID string `json:"id"`
	// Name and Description are for display; ID is what everything references.
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Kind        string            `json:"kind"`
	Agent       string            `json:"agent,omitempty"`
	Route       map[string]string `json:"route,omitempty"` // item lane → agent
	Agents      int               `json:"agents,omitempty"`
	WIP         int               `json:"wip,omitempty"`
	Buffer      int               `json:"buffer,omitempty"`
	Output      string            `json:"output,omitempty"`
	Checks      []StationCheck    `json:"checks,omitempty"`
	Gate        *Gate             `json:"gate,omitempty"`
	// Action makes goald act when a human approves at this (human) station: "merge" merges
	// the item's pull request first.
	Action string `json:"action,omitempty"`
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

// Flow holds the line's pull policy (expedite lane, constraint, release and andon) and its
// targets.
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
	Targets *Targets `json:"targets,omitempty"`
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

// Load is Check for callers that need a valid factory: it fails with every blocking problem,
// one per line, when there is any. Referenced paths resolve inside files only; a path that
// leaves it is not found.
func Load(files fs.FS) (*Factory, error) {
	f, problems := Check(files)

	var errs []error

	for _, p := range problems {
		if p.Blocking() {
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

	f.defaultConnectionNames()

	found := newChecker(f).run()
	for _, p := range found {
		if !p.Blocking() {
			f.warnings = append(f.warnings, p)
		}
	}

	return f, append(problems, found...)
}

// defaultConnectionNames names each connector-form connection without a name after its
// connector, as the schema documents.
func (f *Factory) defaultConnectionNames() {
	for i, c := range f.Spec.Connections {
		if c.Name == "" {
			f.Spec.Connections[i].Name = c.ConnectorName()
		}
	}
}

// Warnings lists the messages of what loads but should be fixed: unpinned connection images
// (the gateway does not start them), literal secrets, and what this install cannot serve yet.
func (f *Factory) Warnings() []string {
	w := make([]string, 0, len(f.warnings))
	for _, p := range f.warnings {
		w = append(w, p.Message)
	}

	return w
}

// Purpose is what the factory is for: metadata.purpose, or its deprecated alias description.
func (f *Factory) Purpose() string {
	if f.Metadata.Purpose != "" {
		return f.Metadata.Purpose
	}

	return f.Metadata.Description
}
