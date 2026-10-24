package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/domain"
)

// document is factory.yaml parsed once: the YAML node tree, which locates problems by line,
// and its JSON form, which the schema validates and the Factory decodes from.
type document struct {
	root *yaml.Node // the top-level mapping
	json []byte
}

// yamlErrorLine finds the line (and column, when given) in a YAML parser error.
var yamlErrorLine = regexp.MustCompile(`line (\d+)(?:: column (\d+))?`)

// parseDocument parses factory.yaml. A file that does not parse, or parses into something
// JSON cannot hold (non-string keys), is one yaml-invalid problem.
func parseDocument(raw []byte) (*document, *domain.DefinitionProblem) {
	var n yaml.Node
	if err := yaml.Unmarshal(raw, &n); err != nil {
		return nil, yamlInvalid(FileName, err)
	}

	if n.Kind != yaml.DocumentNode || len(n.Content) == 0 {
		return nil, &domain.DefinitionProblem{
			Code: codeYAMLInvalid, Severity: domain.SeverityError, Check: checkYAML, Section: domain.SectionFiles,
			File: FileName, Message: FileName + " is empty",
		}
	}

	var v any
	if err := n.Decode(&v); err != nil {
		return nil, yamlInvalid(FileName, err)
	}

	data, err := json.Marshal(v)
	if err != nil {
		return nil, yamlInvalid(FileName, fmt.Errorf("keys must be strings and values plain YAML: %w", err))
	}

	return &document{root: n.Content[0], json: data}, nil
}

func yamlInvalid(file string, err error) *domain.DefinitionProblem {
	p := &domain.DefinitionProblem{
		Code: codeYAMLInvalid, Severity: domain.SeverityError, Check: checkYAML, Section: domain.SectionFiles,
		File: file, Message: fmt.Sprintf("%s does not parse: %v", file, err),
	}

	if m := yamlErrorLine.FindStringSubmatch(err.Error()); m != nil {
		p.Line, _ = strconv.Atoi(m[1])
		p.Column, _ = strconv.Atoi(m[2])
		p.EndLine = p.Line
	}

	return p
}

// Location places a factory.yaml path in both views of a definition.
type Location struct {
	Section, Pointer, File           string
	Line, Column, EndLine, EndColumn int
}

// Problem is a located problem found by check.
func (l Location) Problem(check, code string, severity domain.ProblemSeverity, message string) domain.DefinitionProblem {
	return domain.DefinitionProblem{
		Code: code, Severity: severity, Check: check, Section: l.Section, Pointer: l.Pointer, File: l.File,
		Line: l.Line, Column: l.Column, EndLine: l.EndLine, EndColumn: l.EndColumn, Message: message,
	}
}

// Locate places path, an RFC 6901 pointer into factory.yaml ("/spec/connections/1/headers/
// Authorization", as SecretUse.Doc gives it), so a check outside this package can report a
// located problem. A path that does not exist (a missing key) is placed at its nearest parent.
func (f *Factory) Locate(path string) Location {
	return f.doc.locate(splitPointer(path))
}

func (d *document) locate(path []string) Location {
	section, pointer := ModelPointer(path)
	l := Location{Section: section, Pointer: pointer, File: FileName}

	if d == nil {
		return l
	}

	key, val := d.lookup(path)

	switch {
	case val != nil && val.Kind == yaml.ScalarNode:
		l.Line, l.Column = val.Line, val.Column
	case key != nil:
		l.Line, l.Column = key.Line, key.Column
	case val != nil:
		l.Line, l.Column = val.Line, val.Column
	default:
		return l
	}

	end := val
	if end == nil {
		end = key
	}

	l.EndLine, l.EndColumn = nodeEnd(end)

	return l
}

// lookup walks path as far as it exists and returns the deepest node it reached: the key node
// when that node is a mapping value (nil inside sequences) and the value node.
func (d *document) lookup(path []string) (key, val *yaml.Node) {
	val = d.root

	for _, tok := range path {
		k, v := child(val, tok)
		if v == nil {
			break
		}

		key, val = k, v
	}

	return key, val
}

// child returns the entry tok of a mapping (following merge keys) or sequence.
func child(n *yaml.Node, tok string) (key, val *yaml.Node) {
	n = deref(n)

	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == tok {
				return n.Content[i], deref(n.Content[i+1])
			}
		}

		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "<<" {
				if k, v := child(n.Content[i+1], tok); v != nil {
					return k, v
				}
			}
		}
	case yaml.SequenceNode:
		if i, err := strconv.Atoi(tok); err == nil && i >= 0 && i < len(n.Content) {
			return nil, deref(n.Content[i])
		}
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
	}

	return nil, nil
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}

	return n
}

// nodeEnd is where a node's text ends: after its last scalar.
func nodeEnd(n *yaml.Node) (line, column int) {
	if n.Kind == yaml.ScalarNode {
		width := len(n.Value)
		if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
			width += 2
		}

		if strings.Contains(n.Value, "\n") || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			return n.Line, n.Column
		}

		return n.Line, n.Column + width
	}

	line, column = n.Line, n.Column
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

// splitPointer splits an RFC 6901 pointer into its unescaped tokens.
func splitPointer(p string) []string {
	if p == "" || p == "/" {
		return nil
	}

	toks := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, t := range toks {
		toks[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~1", "/"), "~0", "~")
	}

	return toks
}

// joinPointer is the RFC 6901 pointer of tokens.
func joinPointer(toks []string) string {
	var b strings.Builder
	for _, t := range toks {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(t, "~", "~0"), "/", "~1"))
	}

	return b.String()
}

// displayPath writes tokens as a reader expects them in a message: spec.agents[1].skills[0].
func displayPath(toks []string) string {
	var b strings.Builder

	for _, t := range toks {
		if _, err := strconv.Atoi(t); err == nil {
			b.WriteString("[" + t + "]")

			continue
		}

		if b.Len() > 0 {
			b.WriteByte('.')
		}

		b.WriteString(t)
	}

	return b.String()
}

// at builds a factory.yaml path from keys and sequence indexes.
func at(parts ...any) []string {
	out := make([]string, len(parts))

	for i, p := range parts {
		switch v := p.(type) {
		case int:
			out[i] = strconv.Itoa(v)
		case string:
			out[i] = v
		default:
			out[i] = fmt.Sprint(v)
		}
	}

	return out
}
