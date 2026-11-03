package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/contracts/definitions"
	"github.com/byFrederick/goal/internal/core/domain"
)

// MemoryFile is the factory's memory at the root of its file tree (ADR 0038 decision 6): short
// facts its runs should know, one list of entries.
const MemoryFile = "memory.yaml"

// MemoryEntry is one fact of the factory's memory: its text, the items it came from
// (<factory>#<number>) and the date it was added (YYYY-MM-DD).
type MemoryEntry struct {
	Text  string   `json:"text"`
	From  []string `json:"from,omitempty"`
	Added string   `json:"added"`
}

// Memory is memory.yaml's entries; nil when the definition has no memory.yaml or the file does
// not satisfy memory.schema.json.
func (f *Factory) Memory() []MemoryEntry { return slices.Clone(f.memory) }

// compiledMemory compiles memory.schema.json once.
var compiledMemory = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileSchema(definitions.Memory, "memory.schema.json", "")
})

// memory reads memory.yaml when the definition has one: it satisfies memory.schema.json, each
// entry's text is at most domain.MaxMemoryText characters (memory-too-long), and the file holds at
// most domain.MaxMemoryBytes and domain.MaxMemoryEntries (memory-too-big).
func (c *checker) memory() {
	raw, err := fs.ReadFile(c.f.files, MemoryFile)
	if err != nil {
		return
	}

	var n yaml.Node
	if yaml.Unmarshal(raw, &n) != nil {
		return // otherFileProblems reports it
	}

	doc, bad := parseYAMLWith(MemoryFile, raw, datesAsText)
	if bad != nil {
		c.problems = append(c.problems, *bad)

		return
	}

	if !c.memorySchema(doc) {
		return
	}

	var entries []MemoryEntry
	if err := json.Unmarshal(doc.json, &entries); err != nil {
		c.memoryError(doc, checkYAML, codeYAMLInvalid, nil, fmt.Sprintf("%s: %v", MemoryFile, err))

		return
	}

	if len(raw) > domain.MaxMemoryBytes || len(entries) > domain.MaxMemoryEntries {
		c.memoryError(doc, checkReferences, codeMemoryTooBig, nil, fmt.Sprintf(
			"%s holds %d bytes and %d entries, more than %d bytes or %d entries: every run's prompt carries it; merge or remove facts",
			MemoryFile, len(raw), len(entries), domain.MaxMemoryBytes, domain.MaxMemoryEntries))
	}

	for i, e := range entries {
		if l := utf8.RuneCountInString(e.Text); l > domain.MaxMemoryText {
			c.memoryError(doc, checkReferences, codeMemoryTooLong, at(i, "text"), fmt.Sprintf(
				"%s entry %d: the text is %d characters, more than %d: keep one short fact per entry", MemoryFile, i+1, l, domain.MaxMemoryText))
		}
	}

	c.f.memory = entries
}

// memorySchema validates memory.yaml against memory.schema.json and records each failed rule. It
// reports whether the file satisfies it.
func (c *checker) memorySchema(doc *document) bool {
	s, err := compiledMemory()
	if err != nil {
		c.memoryError(doc, checkSchema, codeSchemaInvalid, nil, "memory.schema.json does not compile: "+err.Error())

		return false
	}

	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc.json))
	if err != nil {
		c.memoryError(doc, checkYAML, codeYAMLInvalid, nil, fmt.Sprintf("%s does not parse: %v", MemoryFile, err))

		return false
	}

	failed, err := failedRules(s, v)
	if err != nil {
		c.memoryError(doc, checkSchema, codeSchemaInvalid, nil, fmt.Sprintf("%s: %v", MemoryFile, err))

		return false
	}

	for _, l := range failed {
		c.memoryError(doc, checkSchema, l.code, l.path, fmt.Sprintf("%s: %s", MemoryFile, l.message))
	}

	return len(failed) == 0
}

// memoryError records an error at path, keys from memory.yaml's root, in the Files section.
func (c *checker) memoryError(doc *document, check, code string, path []string, msg string) {
	l := doc.locate(path)
	l.Section, l.Pointer, l.File = domain.SectionFiles, "", MemoryFile

	c.problems = appendNew(c.problems, l.Problem(check, code, domain.SeverityError, msg))
}

// datesAsText keeps an unquoted date (added: 2026-11-01) as the text written, rather than a
// timestamp that JSON would write with a time of day.
func datesAsText(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!timestamp" {
		n.Tag = "!!str"
	}

	for _, child := range n.Content {
		datesAsText(child)
	}
}
