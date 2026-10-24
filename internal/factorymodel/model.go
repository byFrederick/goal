// Package factorymodel is the Visual editor's view of a factory definition: a structured model
// of factory.yaml in five sections, read from the YAML (FromYAML) and written back into it
// (Apply) as a patch on the YAML node tree, so that everything a section does not own, every
// comment, the key order, flow and block style, quotes, anchors, blank lines and indentation
// survive a Visual save. Format normalizes a whole file the way Apply writes changed lines.
//
// The model's keys are factory.yaml's keys in snake_case (FactoryModel in api/openapi.yaml);
// keys that are data (route lanes, tool names, connector settings) are kept as written. A key
// the YAML does not have is absent from the model, so a model read from a file and written
// back unchanged leaves the file byte for byte as it was. config.ModelPointer maps a
// factory.yaml path to the same pointers Ranges uses.
package factorymodel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/config"
)

// Section is one tab of the Visual editor and the part of factory.yaml it owns.
type Section string

// The sections. Everything else in factory.yaml (apiVersion, metadata.owner,
// runtime.runner/image/auth, knowledgeBase, secrets, contracts, learning other than the
// librarian's agent, unknown keys) is edited in YAML mode only.
const (
	Basics      Section = "basics"      // metadata.id/name/title/purpose, board.workTypes, runtime.repository
	Line        Section = "line"        // board.stations/intake/flow, learning.librarian.agent
	Agents      Section = "agents"      // agents, skills, runtime.model
	Connections Section = "connections" // connections
	Review      Section = "review"      // runtime.budget
)

// Sections lists every section in the editor's order.
var Sections = []Section{Basics, Line, Agents, Connections, Review}

// Model is the factory model. Values are JSON values (objects are map[string]any, numbers
// json.Number), lenient by design: a value the schema rejects appears as it is written.
type Model struct {
	Basics      map[string]any `json:"basics"`
	Line        map[string]any `json:"line"`
	Agents      map[string]any `json:"agents"`
	Connections []any          `json:"connections"`
	Review      map[string]any `json:"review"`
}

// Section returns one section's value.
func (m Model) Section(s Section) any {
	switch s {
	case Basics:
		return m.Basics
	case Line:
		return m.Line
	case Agents:
		return m.Agents
	case Connections:
		return m.Connections
	case Review:
		return m.Review
	}

	return nil
}

// Ranges maps model pointers ("/line/stations/3") to the first and last line of factory.yaml
// that hold the value, so the editor can move between the Visual and YAML views.
type Ranges map[string][2]int

// Section is the lines a section spans: the first and last line of any of its values.
func (r Ranges) Section(s Section) (from, to int) {
	prefix := "/" + string(s)

	for p, lines := range r {
		if p != prefix && !strings.HasPrefix(p, prefix+"/") {
			continue
		}

		if from == 0 || lines[0] < from {
			from = lines[0]
		}

		to = max(to, lines[1])
	}

	return from, to
}

// ParseError is a factory.yaml that does not parse; the Visual editor is read-only until it is
// fixed in YAML mode.
type ParseError struct {
	Line, Column int
	Message      string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("factory.yaml does not parse at line %d: %s", e.Line, e.Message)
	}

	return "factory.yaml does not parse: " + e.Message
}

// ErrModelInvalid is a section value of the wrong shape: not an object (a list for
// connections), or keys that are not the model's snake_case keys.
var ErrModelInvalid = errors.New("model value invalid")

// binding places one key of a section's value in factory.yaml.
type binding struct {
	key   string   // the key in the section's value; "" for the whole value (connections)
	path  []string // where it is in factory.yaml
	shape *shape
}

var sections = map[Section][]binding{
	Basics: {
		{"id", []string{keyMetadata, "id"}, renamedShape},
		{keyName, []string{keyMetadata, keyName}, renamedShape},
		{"title", []string{keyMetadata, "title"}, renamedShape},
		{keyPurpose, []string{keyMetadata, keyPurpose}, renamedShape}, // or its alias description: see purposePath
		{"work_types", []string{keySpec, keyBoard, "workTypes"}, verbatimShape},
		{"repository", []string{keySpec, keyRuntime, "repository"}, repositoryShape},
	},
	Line: {
		{keyStations, []string{keySpec, keyBoard, keyStations}, stationsShape},
		{"intake", []string{keySpec, keyBoard, "intake"}, intakeShape},
		{keyFlow, []string{keySpec, keyBoard, keyFlow}, flowShape},
		{keyLibrarian, []string{keySpec, keyLearning, keyLibrarian, "agent"}, renamedShape},
	},
	Agents: {
		{"default_model", []string{keySpec, keyRuntime, "model"}, renamedShape},
		{keyAgents, []string{keySpec, keyAgents}, agentsShape},
		{keySkills, []string{keySpec, keySkills}, skillsShape},
	},
	Connections: {{"", []string{keySpec, keyConnections}, connectionsShape}},
	Review:      {{"budget", []string{keySpec, keyRuntime, "budget"}, budgetShape}},
}

// FromYAML reads the model of a factory.yaml. files, when not nil, is the definition's tree:
// each agent's prompt then carries its file's sha256 and word count. A document that does not
// parse is a *ParseError.
func FromYAML(doc []byte, files fs.FS) (Model, Ranges, error) {
	t, err := parse(doc)
	if err != nil {
		return Model{}, nil, err
	}

	return readModel(t.root, files), ranges(t.root), nil
}

func readModel(root *yaml.Node, files fs.FS) Model {
	m := Model{Connections: []any{}}

	for _, s := range Sections {
		v := readSection(root, s, files)

		switch s {
		case Connections:
			if l, ok := v.([]any); ok {
				m.Connections = l
			}
		case Basics:
			m.Basics, _ = v.(map[string]any)
		case Line:
			m.Line, _ = v.(map[string]any)
		case Agents:
			m.Agents, _ = v.(map[string]any)
		case Review:
			m.Review, _ = v.(map[string]any)
		}
	}

	return m
}

func readSection(root *yaml.Node, s Section, files fs.FS) any {
	if s == Connections {
		b := sections[s][0]
		if n := lookup(root, b.path); n != nil {
			return read(n, b.shape, files)
		}

		return []any{}
	}

	out := map[string]any{}

	for _, b := range sections[s] {
		p := b.path
		if b.key == keyPurpose {
			p = purposePath(root)
		}

		if n := lookup(root, p); n != nil {
			out[b.key] = read(n, b.shape, files)
		}
	}

	return out
}

// purposePath is where the purpose is written: metadata.purpose, or metadata.description when
// only the deprecated alias is there.
func purposePath(root *yaml.Node) []string {
	if lookup(root, []string{keyMetadata, keyPurpose}) == nil && lookup(root, []string{keyMetadata, keyDescription}) != nil {
		return []string{keyMetadata, keyDescription}
	}

	return []string{keyMetadata, keyPurpose}
}

// read is the model value of node n at a position of shape s.
func read(n *yaml.Node, s *shape, files fs.FS) any {
	n = deref(n)

	switch s.kind {
	case promptRef:
		return readPrompt(n, files)
	case runMode:
		if n.Kind == yaml.ScalarNode {
			return map[string]any{"mode": scalar(n)}
		}
	case generic, agentItem, connectItem:
	}

	switch n.Kind {
	case yaml.MappingNode:
		return readMapping(n, s, files)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, read(c, s.child(""), files))
		}

		return out
	case yaml.ScalarNode:
		return scalar(n)
	case yaml.DocumentNode, yaml.AliasNode:
	}

	return nil
}

func readMapping(n *yaml.Node, s *shape, files fs.FS) map[string]any {
	out := map[string]any{}

	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, n.Content[i+1]

		switch {
		case k == "<<":
			mergeKeys(out, read(v, s, files))

			continue
		case s.kind == agentItem && k == keyTools:
			out["custom_tools"] = true

			continue
		case s.kind == connectItem && !slices.Contains(connectionKeys, k):
			settings, _ := out[keySettings].(map[string]any)
			if settings == nil {
				settings = map[string]any{}
				out[keySettings] = settings
			}

			settings[k] = read(v, verbatimShape, files)

			continue
		}

		out[s.modelKey(k)] = read(v, s.child(k), files)
	}

	if s.kind == connectItem {
		if _, ok := out[keyName]; !ok && connectorName(n) != "" {
			out[keyName] = connectorName(n)
		}

		_, typed := out["type"]
		out[keyLegacy] = typed
	}

	return out
}

// connectionKeys are the connection keys the model shows by name; every other key is a
// setting.
var connectionKeys = []string{keyName, keyConnector, "type", "credential", keyAgents, keyTools}

// mergeKeys adds a YAML merge key's entries that the mapping does not set itself.
func mergeKeys(out map[string]any, merged any) {
	switch m := merged.(type) {
	case map[string]any:
		for k, v := range m {
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
	case []any:
		for _, e := range m {
			mergeKeys(out, e)
		}
	}
}

func readPrompt(n *yaml.Node, files fs.FS) any {
	if n.Kind != yaml.ScalarNode {
		return read(n, renamedShape, files)
	}

	p := map[string]any{"path": n.Value}
	if files == nil {
		return p
	}

	if body, err := fs.ReadFile(files, path.Clean(n.Value)); err == nil {
		sum := sha256.Sum256(body)
		p["sha256"] = hex.EncodeToString(sum[:])
		p["words"] = json.Number(strconv.Itoa(len(bytes.Fields(body))))
	}

	return p
}

// scalar is the JSON value of a scalar node.
func scalar(n *yaml.Node) any {
	switch n.ShortTag() {
	case "!!null":
		return nil
	case "!!bool":
		var b bool
		if n.Decode(&b) == nil {
			return b
		}
	case "!!int":
		var i int64
		if n.Decode(&i) == nil {
			return json.Number(strconv.FormatInt(i, 10))
		}
	case "!!float":
		var f float64
		if n.Decode(&f) == nil {
			return json.Number(strconv.FormatFloat(f, 'g', -1, 64))
		}
	}

	return n.Value
}

// lookup is the node at path below the root mapping, nil when any part is missing.
func lookup(root *yaml.Node, p []string) *yaml.Node {
	n := root
	for _, k := range p {
		if n = mapValue(n, k); n == nil {
			return nil
		}
	}

	return n
}

// ranges maps every model pointer to the lines of its value.
func ranges(root *yaml.Node) Ranges {
	r := Ranges{}

	walk(root, nil, func(p []string, key, val *yaml.Node) {
		if _, pointer := config.ModelPointer(p); pointer != "" {
			first := val.Line
			if key != nil {
				first = key.Line
			}

			last, _ := nodeEnd(val)
			if cur, ok := r[pointer]; ok {
				first, last = min(first, cur[0]), max(last, cur[1])
			}

			r[pointer] = [2]int{first, last}
		}
	})

	return r
}

// walk calls fn for every node below n with its factory.yaml path; aliases are not followed.
func walk(n *yaml.Node, p []string, fn func(p []string, key, val *yaml.Node)) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			cp := append(slices.Clip(p), n.Content[i].Value)
			fn(cp, n.Content[i], n.Content[i+1])
			walk(n.Content[i+1], cp, fn)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			cp := append(slices.Clip(p), strconv.Itoa(i))
			fn(cp, nil, c)
			walk(c, cp, fn)
		}
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
	}
}

// nodeEnd is the last line and column of a node's text.
func nodeEnd(n *yaml.Node) (line, column int) {
	line, column = n.Line, n.Column
	if n.Kind == yaml.ScalarNode {
		if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			// A block scalar's text starts on the line after its indicator.
			return line + 1 + strings.Count(strings.TrimRight(n.Value, "\n"), "\n"), 0
		}

		return line, column + len(n.Value)
	}

	for _, c := range n.Content {
		if c.Kind == yaml.AliasNode {
			continue
		}

		if l, col := nodeEnd(c); l > line || (l == line && col > column) {
			line, column = l, col
		}
	}

	return line, column
}
