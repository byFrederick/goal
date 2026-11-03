package config

import (
	"bytes"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/byFrederick/goal/contracts/definitions"
	"github.com/byFrederick/goal/internal/core/domain"
)

// english prints schema messages, as the validator does by default.
var english = message.NewPrinter(language.English)

// compiledSchema compiles the embedded schema once; Check is safe for concurrent use.
var compiledSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileSchema(definitions.Factory, "factory.schema.json", "")
})

// compileSchema compiles the schema raw, added as name, at fragment ("" for its root, or a
// pointer such as "#/$defs/case").
func compileSchema(raw []byte, name, fragment string) (*jsonschema.Schema, error) {
	s, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, s); err != nil {
		return nil, err
	}

	return c.Compile(name + fragment)
}

// schemaProblems validates factory.yaml against the schema: one problem per failed rule, at
// the line of the value it is about.
func (d *document) schemaProblems() []domain.DefinitionProblem {
	compiled, err := compiledSchema()
	if err != nil {
		return []domain.DefinitionProblem{d.schemaProblem(nil, codeSchemaInvalid, "the schema does not compile: "+err.Error())}
	}

	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(d.json))
	if err != nil {
		return []domain.DefinitionProblem{*yamlInvalid(FileName, err)}
	}

	failed, err := failedRules(compiled, v)
	if err != nil {
		return []domain.DefinitionProblem{d.schemaProblem(nil, codeSchemaInvalid, err.Error())}
	}

	out := make([]domain.DefinitionProblem, 0, len(failed))

	for _, l := range failed {
		p := d.schemaProblem(l.path, l.code, l.message)
		p.Fixes = l.fixes
		out = appendNew(out, p)
	}

	return out
}

// failedRules validates v against s: the rules it fails, none when it satisfies s. The error is
// a failure other than a rule's.
func failedRules(s *jsonschema.Schema, v any) ([]leaf, error) {
	err := s.Validate(v)

	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return nil, err
	}

	out := leaves(ve)
	for i, l := range out {
		if l.propertyName != "" {
			out[i] = l.at(keyPath(v, l.propertyName))
		}
	}

	return out, nil
}

// keyPath is the path of the first object key named name in v, keys in sorted order, or nil.
// The schema library validates a propertyNames name on its own, so its error has no place.
func keyPath(v any, name string) []string {
	switch t := v.(type) {
	case map[string]any:
		keys := slices.Sorted(maps.Keys(t))
		if slices.Contains(keys, name) {
			return []string{name}
		}

		for _, k := range keys {
			if p := keyPath(t[k], name); p != nil {
				return append([]string{k}, p...)
			}
		}
	case []any:
		for i, e := range t {
			if p := keyPath(e, name); p != nil {
				return append([]string{strconv.Itoa(i)}, p...)
			}
		}
	}

	return nil
}

// appendNew appends p unless out has a problem at the same place saying the same.
func appendNew(out []domain.DefinitionProblem, p domain.DefinitionProblem) []domain.DefinitionProblem {
	if slices.ContainsFunc(out, func(q domain.DefinitionProblem) bool {
		return q.File == p.File && q.Pointer == p.Pointer && q.Line == p.Line && q.Message == p.Message
	}) {
		return out
	}

	return append(out, p)
}

func (d *document) schemaProblem(path []string, code, message string) domain.DefinitionProblem {
	return d.locate(path).Problem(checkSchema, code, domain.SeverityError, message)
}

// leaf is one failed schema rule: where, which code, what it says and the fixes it offers.
type leaf struct {
	path          []string
	code, message string
	fixes         []domain.ProblemFix
	// propertyName is the key a failed propertyNames rule is about, whose path failedRules finds.
	propertyName string
}

// at is a propertyNames leaf placed at the key's path, when one was found.
func (l leaf) at(path []string) leaf {
	if path != nil {
		l.path, l.message = path, where(path)+l.message
	}

	return l
}

// leaves flattens a validation error into the rules that failed. A failed oneOf or anyOf is
// one leaf listing its alternatives, since each alternative's own failures mean nothing alone.
func leaves(e *jsonschema.ValidationError) []leaf {
	switch k := e.ErrorKind.(type) {
	case *kind.OneOf:
		if k.Subschemas == nil && len(e.Causes) > 0 {
			return []leaf{alternatives(e)}
		}
	case *kind.AnyOf:
		if len(e.Causes) > 0 {
			return []leaf{alternatives(e)}
		}
	case *kind.PropertyNames:
		return []leaf{propertyNameLeaf(e, k.Property)}
	}

	if len(e.Causes) == 0 {
		return []leaf{ruleLeaf(e)}
	}

	var out []leaf
	for _, c := range e.Causes {
		out = append(out, leaves(c)...)
	}

	return out
}

func alternatives(e *jsonschema.ValidationError) leaf {
	branches := make([]string, 0, len(e.Causes))

	for _, c := range e.Causes {
		var msgs []string
		for _, l := range leaves(c) {
			msgs = append(msgs, l.message)
		}

		branches = append(branches, strings.Join(msgs, "; "))
	}

	return leaf{
		path: e.InstanceLocation, code: codeSchemaInvalid,
		message: where(e.InstanceLocation) + "none of the allowed forms matches: " + strings.Join(branches, " / "),
	}
}

// propertyNameLeaf is a key that breaks propertyNames. Its causes validate the name alone, so
// they carry no place of their own; failedRules places it at the key.
func propertyNameLeaf(e *jsonschema.ValidationError, name string) leaf {
	var msgs []string

	for _, c := range e.Causes {
		for _, l := range leaves(c) {
			msgs = append(msgs, l.message)
		}
	}

	if len(msgs) == 0 {
		msgs = append(msgs, e.ErrorKind.LocalizedString(english))
	}

	return leaf{code: codeSchemaInvalid, message: "the name " + strings.Join(msgs, "; "), propertyName: name}
}

// ruleLeaf names one failed rule. Its path is the value it is about: the instance, or the
// offending or missing key below it.
func ruleLeaf(e *jsonschema.ValidationError) leaf {
	path := slices.Clone(e.InstanceLocation)
	code := codeSchemaInvalid

	switch k := e.ErrorKind.(type) {
	case *kind.Required:
		code, path = codeSchemaRequired, append(path, k.Missing[0])
	case *kind.AdditionalProperties:
		code, path = codeSchemaUnknownKey, append(path, k.Properties[0])
	case *kind.Enum, *kind.Const:
		code = codeSchemaEnum
	case *kind.Type:
		code = codeSchemaType
	}

	msg := e.ErrorKind.LocalizedString(english)
	if _, missing := e.ErrorKind.(*kind.Required); missing {
		return leaf{path: path, code: code, message: where(e.InstanceLocation) + msg}
	}

	return leaf{path: path, code: code, message: where(path) + msg, fixes: enumFixes(path, e.ErrorKind)}
}

// enumFixes offers, for a string outside an enum of strings, the allowed value nearest to it.
func enumFixes(path []string, k jsonschema.ErrorKind) []domain.ProblemFix {
	e, ok := k.(*kind.Enum)
	if !ok {
		return nil
	}

	got, ok := e.Got.(string)
	if !ok {
		return nil
	}

	best, ok := nearest(got, e.Want)
	if !ok {
		return nil
	}

	return []domain.ProblemFix{ReplaceFix("Use "+best, path, best)}
}

// nearest is the string in want closest to got by edit distance, the first on a tie; false when
// want has no string.
func nearest(got string, want []any) (string, bool) {
	best, bestDistance, found := "", 0, false

	for _, w := range want {
		s, ok := w.(string)
		if !ok {
			continue
		}

		if d := editDistance(got, s); !found || d < bestDistance {
			best, bestDistance, found = s, d, true
		}
	}

	return best, found
}

// editDistance is the Levenshtein distance between a and b, in runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		cur[0] = i

		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}

			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}

		prev, cur = cur, prev
	}

	return prev[len(rb)]
}

// where prefixes a message with the YAML path it is about.
func where(path []string) string {
	if len(path) == 0 {
		return ""
	}

	return displayPath(path) + ": "
}
