package config

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/byFrederick/goal/contracts/definitions"
	"github.com/byFrederick/goal/internal/domain"
)

// english prints schema messages, as the validator does by default.
var english = message.NewPrinter(language.English)

// compiledSchema compiles the embedded schema once; Check is safe for concurrent use.
var compiledSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	s, err := jsonschema.UnmarshalJSON(bytes.NewReader(definitions.Factory))
	if err != nil {
		return nil, err
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("factory.schema.json", s); err != nil {
		return nil, err
	}

	return c.Compile("factory.schema.json")
})

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

	err = compiled.Validate(v)

	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		if err != nil {
			return []domain.DefinitionProblem{d.schemaProblem(nil, codeSchemaInvalid, err.Error())}
		}

		return nil
	}

	var out []domain.DefinitionProblem

	for _, l := range leaves(ve) {
		p := d.schemaProblem(l.path, l.code, l.message)
		if !slices.ContainsFunc(out, func(q domain.DefinitionProblem) bool {
			return q.Pointer == p.Pointer && q.Line == p.Line && q.Message == p.Message
		}) {
			out = append(out, p)
		}
	}

	return out
}

func (d *document) schemaProblem(path []string, code, message string) domain.DefinitionProblem {
	return d.locate(path).Problem(checkSchema, code, domain.SeverityError, message)
}

// leaf is one failed schema rule: where, which code and what it says.
type leaf struct {
	path          []string
	code, message string
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

	return leaf{path: path, code: code, message: where(path) + msg}
}

// where prefixes a message with the YAML path it is about.
func where(path []string) string {
	if len(path) == 0 {
		return ""
	}

	return displayPath(path) + ": "
}
