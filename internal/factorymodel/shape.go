package factorymodel

import (
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// factory.yaml keys the model code names.
const (
	keyMetadata    = "metadata"
	keySpec        = "spec"
	keyBoard       = "board"
	keyRuntime     = "runtime"
	keyLearning    = "learning"
	keyLibrarian   = "librarian"
	keyName        = "name"
	keyPurpose     = "purpose"
	keyDescription = "description"
	keyAgents      = "agents"
	keySkills      = "skills"
	keyRules       = "rules"
	keyTools       = "tools"
	keyConnector   = "connector"
	keyStations    = "stations"
	keyFlow        = "flow"
	keyConnections = "connections"
	keyKnowledge   = "knowledge"
	keyImage       = "image"
	keyStarter     = "starter"
	keySettings    = "settings" // model only: a connection's connector settings
	keyLegacy      = "legacy"   // model only: a connection in the inline type form

	tagStr = "!!str"
)

// kind says how a shape maps its YAML node to a model value, when that differs from the
// generic mapping (keys renamed or kept, values as they are).
type kind int

const (
	generic     kind = iota
	promptRef        // agents[].prompt: "./agents/x.md" ↔ {path, sha256, words}
	runMode          // builtins.run: "allowed" or {mode, commands} ↔ {mode, commands}
	agentItem        // agents[]: tools ↔ custom_tools; builtins replaces tools
	connectItem      // connections[]: settings flattened, default name, legacy
)

// shape describes one position of factory.yaml for the model: how its keys are named on each
// side, its children's shapes, the canonical order of its keys and how its items are matched.
type shape struct {
	kind kind
	// verbatim keeps keys as written (data keys: lanes, tool names, header names); otherwise
	// YAML's camelCase keys are the model's snake_case keys.
	verbatim bool
	fields   map[string]*shape // child shapes by YAML key
	elem     *shape            // shape of every other child, or of a sequence's items
	order    []string          // canonical YAML key order for inserted keys
	// identity, for a sequence, names an item; items are matched by it, never by index.
	identity func(*yaml.Node) string
	// identityOf names an item of the model value.
	identityOf func(any) string
}

// child is the shape of a mapping's key k, or of a sequence's items.
func (s *shape) child(k string) *shape {
	if c, ok := s.fields[k]; ok {
		return c
	}

	if s.elem != nil {
		return s.elem
	}

	if s.verbatim {
		return verbatimShape
	}

	return renamedShape
}

func (s *shape) modelKey(yamlKey string) string {
	if s.verbatim {
		return yamlKey
	}

	return snake(yamlKey)
}

func (s *shape) yamlKey(modelKey string) string {
	if s.verbatim {
		return modelKey
	}

	return camel(modelKey)
}

var (
	renamedShape  = &shape{}
	verbatimShape = &shape{verbatim: true}
)

// keyed matches sequence items by the scalar value of key.
func keyed(key string) (func(*yaml.Node) string, func(any) string) {
	return func(n *yaml.Node) string {
			if v := mapValue(n, key); v != nil && v.Kind == yaml.ScalarNode {
				return v.Value
			}

			return ""
		}, func(v any) string {
			m, _ := v.(map[string]any)
			s, _ := m[snake(key)].(string)

			return s
		}
}

// The shapes of the model's sections.
var (
	stationIdentity, stationIdentityOf = keyed("id")
	agentIdentity, agentIdentityOf     = keyed(keyName)

	gateShape         = &shape{order: []string{"onReject", "maxRework"}}
	stationCheckShape = &shape{order: []string{"kind", "run", "min"}}

	stationShape = &shape{
		order: []string{
			"id", keyName, keyDescription, "kind", "agent", "route", keyAgents, "wip", "buffer", "output", "checks",
			"gate", "action",
		},
		fields: map[string]*shape{
			"route":  verbatimShape,
			"checks": {elem: stationCheckShape},
			"gate":   gateShape,
		},
	}

	stationsShape = &shape{elem: stationShape, identity: stationIdentity, identityOf: stationIdentityOf}

	intakeShape = &shape{elem: &shape{order: []string{"source", "repo", "labels", "cron", "template", "type"}}}

	flowShape = &shape{
		order: []string{"expedite", "constraint", "release", "andon", "targets"},
		fields: map[string]*shape{
			"expedite": {order: []string{"types", "wip"}},
			"andon":    {order: []string{"reworkRate", "window"}},
			"targets":  {order: []string{"throughputPerDay", "leadTimeHours", "expediteLeadTimeMinutes"}},
		},
	}

	builtinsShape = &shape{
		order:  []string{"read", "edit", "run", "web", "subagents"},
		fields: map[string]*shape{"run": {kind: runMode, order: []string{"mode", "commands"}}},
	}

	agentShape = &shape{
		kind: agentItem,
		order: []string{
			keyName, keyDescription, "prompt", "model", "maxTurns", "perItemUsd", keySkills, keyConnections, keyTools,
			"builtins",
		},
		fields: map[string]*shape{
			"prompt":   {kind: promptRef},
			keyTools:   verbatimShape,
			"builtins": builtinsShape,
		},
	}

	agentsShape = &shape{elem: agentShape, identity: agentIdentity, identityOf: agentIdentityOf}

	skillsShape = &shape{
		elem:     &shape{order: []string{keyName, "source", "origin"}},
		identity: agentIdentity, identityOf: agentIdentityOf,
	}

	// rulesShape is spec.rules: each rule by name, its file in rules/.
	rulesShape = &shape{
		elem:     &shape{order: []string{keyName, "source"}},
		identity: agentIdentity, identityOf: agentIdentityOf,
	}

	toolRuleShape = &shape{verbatim: true, order: []string{"enabled", keyAgents, "approval", "scope", "access"}}

	connectionShape = &shape{
		kind:     connectItem,
		verbatim: true,
		order: []string{
			keyConnector, keyName, "type", "credential", keyAgents, keyTools, "transport", "url", keyImage, "command",
			"headers", "env", "network", "roleArn", "region",
		},
		fields: map[string]*shape{"tools": {verbatim: true, elem: toolRuleShape}},
	}

	connectionsShape = &shape{elem: connectionShape, identity: connectionIdentity, identityOf: agentIdentityOf}

	budgetShape     = &shape{order: []string{"dailyUsd", "weeklyUsd", "onExhausted", "perItemUsd", "maxTurnsPerTask"}}
	repositoryShape = &shape{verbatim: true, order: []string{"url", "connection", "token"}}
)

// connectionIdentity is a connection's name: an organization connection's is the item itself,
// and in the connector form it defaults to the connector's name.
func connectionIdentity(n *yaml.Node) string {
	if d := deref(n); d != nil && d.Kind == yaml.ScalarNode {
		return d.Value
	}

	if v := mapValue(n, keyName); v != nil {
		return v.Value
	}

	return connectorName(n)
}

func connectorName(n *yaml.Node) string {
	v := mapValue(n, keyConnector)
	if v == nil || v.ShortTag() != tagStr {
		return ""
	}

	name, _, _ := strings.Cut(v.Value, "@")

	return name
}

// containerOrder is the canonical key order of the mappings that hold the sections' values,
// used when an edit creates a key there.
var containerOrder = map[string][]string{
	"":          {"apiVersion", "kind", keyMetadata, keySpec},
	keyMetadata: {"id", keyName, "title", keyPurpose, keyDescription, "owner"},
	"spec": {
		keyRuntime, "knowledgeBase", "secrets", keyConnections, keyKnowledge, keySkills, keyRules, "contracts", keyAgents, keyBoard, keyLearning,
	},
	"spec.runtime":            {"runner", keyImage, "auth", "model", "modelConnection", "size", keyTools, "repository", "budget"},
	"spec.knowledge":          {"sources"},
	"spec.board":              {"workTypes", "intake", keyStations, keyFlow},
	"spec.learning":           {keyLibrarian, "kaizen"},
	"spec.learning.librarian": {"agent", "after", "writes"},
}

// snake turns a camelCase YAML key into the model's snake_case: maxRework → max_rework.
func snake(s string) string {
	var b strings.Builder

	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}

			r = unicode.ToLower(r)
		}

		b.WriteRune(r)
	}

	return b.String()
}

// camel turns a model key into YAML's camelCase: max_rework → maxRework.
func camel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}

	return strings.Join(parts, "")
}

// mapValue is the value of key in a mapping node, nil when absent.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return deref(n.Content[i+1])
		}
	}

	return nil
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}

	return n
}
