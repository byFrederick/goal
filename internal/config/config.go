// Package config loads and validates a factory definition (factories/<name>/factory.yaml).
//
// Validation runs in two passes: the JSON Schema checks shape, then Validate checks
// what a schema cannot: that every reference (agent, skill, connection, station,
// contract, file) points at something that exists.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"sigs.k8s.io/yaml"

	"github.com/byFrederick/factory/schema"
)

// Factory is one parsed factories/<name>/factory.yaml.
type Factory struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`

	// Dir is the factory folder; relative paths in the spec resolve against it.
	Dir string `json:"-"`
}

// Metadata identifies a factory.
type Metadata struct {
	// ID never changes; everything persistent (rows, S3 prefix, AWS resources) keys on it.
	ID          string `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
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
	Runner string  `json:"runner"`
	Image  string  `json:"image"`
	Auth   string  `json:"auth"`
	Model  string  `json:"model,omitempty"`
	Budget *Budget `json:"budget,omitempty"`
}

// Budget caps spend and agent turns.
type Budget struct {
	DailyUSD        float64 `json:"dailyUsd,omitempty"`
	PerItemUSD      float64 `json:"perItemUsd,omitempty"`
	MaxTurnsPerTask int     `json:"maxTurnsPerTask,omitempty"`
}

// KnowledgeBase lists the collections under the factory's S3 prefix.
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

// Connection is a tool source served to agents through the gateway.
type Connection struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Transport string            `json:"transport,omitempty"`
	URL       string            `json:"url,omitempty"`
	Image     string            `json:"image,omitempty"`
	Command   []string          `json:"command,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	RoleARN   string            `json:"roleArn,omitempty"`
	Region    string            `json:"region,omitempty"`
}

// Skill is a SKILL.md folder, local or from skills.sh.
type Skill struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Local reports whether the skill ships inside the factory folder rather than from skills.sh.
func (s Skill) Local() bool { return strings.HasPrefix(s.Source, "./") }

// Agent is a worker definition: prompt, model, skills, connections and tool policy.
type Agent struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Prompt      string   `json:"prompt"`
	Model       string   `json:"model,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Connections []string `json:"connections,omitempty"`
	Tools       *struct {
		Allow []string `json:"allow,omitempty"`
		Deny  []string `json:"deny,omitempty"`
	} `json:"tools,omitempty"`
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
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	Agent  string            `json:"agent,omitempty"`
	Route  map[string]string `json:"route,omitempty"` // item lane → agent
	Agents int               `json:"agents,omitempty"`
	WIP    int               `json:"wip,omitempty"`
	Buffer int               `json:"buffer,omitempty"`
	Output string            `json:"output,omitempty"`
	Checks []Check           `json:"checks,omitempty"`
	Gate   *Gate             `json:"gate,omitempty"`
}

// Check is a deterministic gate run before any reviewer sees the output.
type Check struct {
	Kind string `json:"kind"`
	Min  int    `json:"min,omitempty"`
	Run  string `json:"run,omitempty"`
}

// Gate sends rejected work back upstream, at most MaxRework times.
type Gate struct {
	OnReject  string `json:"onReject"`
	MaxRework int    `json:"maxRework,omitempty"`
}

// Flow holds the line's pull policy: expedite lane, constraint, release and andon.
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

// Prefix is the factory's folder in the knowledge bucket. It uses the ID, so renames never move data.
func (f *Factory) Prefix() string { return f.Metadata.ID + "/" }

// Load reads dir/factory.yaml, validates it against the schema and its own references.
func Load(dir string) (*Factory, error) {
	//nolint:gosec // G304: the operator chooses factory folders; no remote input reaches this path.
	raw, err := os.ReadFile(filepath.Join(dir, "factory.yaml"))
	if err != nil {
		return nil, err
	}

	doc, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse factory.yaml: %w", err)
	}

	if err := validateSchema(doc); err != nil {
		return nil, err
	}

	var f Factory
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, err
	}

	f.Dir = dir
	if err := f.Validate(); err != nil {
		return nil, err
	}

	return &f, nil
}

// compiledSchema compiles the embedded schema once; Load is safe for concurrent use.
var compiledSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	s, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Factory))
	if err != nil {
		return nil, err
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("factory.schema.json", s); err != nil {
		return nil, err
	}

	return c.Compile("factory.schema.json")
})

func validateSchema(doc []byte) error {
	compiled, err := compiledSchema()
	if err != nil {
		return err
	}

	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return err
	}

	if err := compiled.Validate(v); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	return nil
}

// problems collects every validation error so the author sees them all at once.
type problems []error

func (p *problems) add(format string, a ...any) { *p = append(*p, fmt.Errorf(format, a...)) }

// Validate checks cross-references. It reports every problem, not just the first.
func (f *Factory) Validate() error {
	var p problems

	f.validateFiles(&p)
	f.validateAgents(&p)
	f.validateStations(&p)
	f.validateFlowAndLearning(&p)

	return errors.Join(p...)
}

func (f *Factory) validateFiles(p *problems) {
	for _, s := range f.Spec.Skills {
		if s.Local() {
			f.mustExist(p, filepath.Join(s.Source, "SKILL.md"), "skill "+s.Name)
		}
	}

	for name, path := range f.Spec.Contracts {
		f.mustExist(p, path, "contract "+name)
	}

	for _, a := range f.Spec.Agents {
		f.mustExist(p, a.Prompt, "agent "+a.Name+" prompt")
	}
}

func (f *Factory) validateAgents(p *problems) {
	skills := names(f.Spec.Skills, func(s Skill) string { return s.Name })
	conns := names(f.Spec.Connections, func(c Connection) string { return c.Name })

	for _, a := range f.Spec.Agents {
		for _, s := range a.Skills {
			if !slices.Contains(skills, s) {
				p.add("agent %s: unknown skill %q", a.Name, s)
			}
		}

		for _, c := range a.Connections {
			if !slices.Contains(conns, c) {
				p.add("agent %s: unknown connection %q", a.Name, c)
			}
		}
	}
}

func (f *Factory) validateStations(p *problems) {
	agents := names(f.Spec.Agents, func(a Agent) string { return a.Name })
	stations := names(f.Spec.Board.Stations, func(s Station) string { return s.ID })

	if st := f.Spec.Board.Stations; st[len(st)-1].Kind != "done" {
		p.add("board: the last station must be kind done")
	}

	for _, s := range f.Spec.Board.Stations {
		f.validateStation(p, s, agents, stations)
	}
}

func (f *Factory) validateStation(p *problems, s Station, agents, stations []string) {
	if s.Agent != "" && !slices.Contains(agents, s.Agent) {
		p.add("station %s: unknown agent %q", s.ID, s.Agent)
	}

	for lane, a := range s.Route {
		if !slices.Contains(agents, a) {
			p.add("station %s: route %s → unknown agent %q", s.ID, lane, a)
		}
	}

	if _, ok := f.Spec.Contracts[s.Output]; s.Output != "" && !ok {
		p.add("station %s: output %q is not a declared contract", s.ID, s.Output)
	}

	if s.Gate != nil && !slices.Contains(stations, s.Gate.OnReject) {
		p.add("station %s: gate.onReject %q is not a station", s.ID, s.Gate.OnReject)
	}

	if s.Kind == "agent" && s.WIP > 0 && s.Agents > s.WIP {
		p.add("station %s: %d agents exceed wip %d; extra agents would idle", s.ID, s.Agents, s.WIP)
	}
}

func (f *Factory) validateFlowAndLearning(p *problems) {
	agents := names(f.Spec.Agents, func(a Agent) string { return a.Name })
	stations := names(f.Spec.Board.Stations, func(s Station) string { return s.ID })

	if fl := f.Spec.Board.Flow; fl != nil && fl.Constraint != "" && fl.Constraint != "auto" &&
		!slices.Contains(stations, fl.Constraint) {
		p.add("flow.constraint %q is not a station", fl.Constraint)
	}

	l := f.Spec.Learning
	if l == nil || l.Librarian == nil {
		return
	}

	if !slices.Contains(agents, l.Librarian.Agent) {
		p.add("learning.librarian: unknown agent %q", l.Librarian.Agent)
	}

	for _, c := range l.Librarian.Writes {
		if !slices.Contains(f.Spec.KnowledgeBase.Collections, c) {
			p.add("learning.librarian writes %q, which is not a knowledge base collection", c)
		}
	}
}

func (f *Factory) mustExist(p *problems, rel, what string) {
	if _, err := os.Stat(filepath.Join(f.Dir, rel)); err != nil {
		p.add("%s: %s not found", what, rel)
	}
}

func names[T any](items []T, key func(T) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = key(it)
	}

	return out
}
