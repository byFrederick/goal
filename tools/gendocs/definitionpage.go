package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// A node is a JSON value that keeps the order of its object's keys, since the page lists fields in
// the schema's order.
type node struct {
	keys   []string
	fields map[string]*node
	items  []*node
	scalar any
	object bool
	array  bool
}

var errSchema = errors.New("not a JSON Schema the definition page can render")

func decodeNode(dec *json.Decoder) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	switch tok {
	case json.Delim('{'):
		n := &node{object: true, fields: map[string]*node{}}

		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("decode: %w", err)
			}

			value, err := decodeNode(dec)
			if err != nil {
				return nil, err
			}

			k, _ := key.(string) // an object's keys are strings
			n.keys = append(n.keys, k)
			n.fields[k] = value
		}

		_, err = dec.Token()

		return n, err
	case json.Delim('['):
		n := &node{array: true}

		for dec.More() {
			value, err := decodeNode(dec)
			if err != nil {
				return nil, err
			}

			n.items = append(n.items, value)
		}

		_, err = dec.Token()

		return n, err
	default:
		return &node{scalar: tok}, nil
	}
}

func (n *node) get(key string) *node {
	if n == nil || !n.object {
		return nil
	}

	return n.fields[key]
}

func (n *node) str(key string) string {
	s, _ := n.get(key).scalarValue().(string)

	return s
}

func (n *node) scalarValue() any {
	if n == nil {
		return nil
	}

	return n.scalar
}

// json is the compact JSON of n, keys in their order.
func (n *node) json() string {
	switch {
	case n.object:
		parts := make([]string, len(n.keys))
		for i, k := range n.keys {
			key, _ := json.Marshal(k)
			parts[i] = string(key) + ":" + n.fields[k].json()
		}

		return "{" + strings.Join(parts, ",") + "}"
	case n.array:
		parts := make([]string, len(n.items))
		for i, item := range n.items {
			parts[i] = item.json()
		}

		return "[" + strings.Join(parts, ",") + "]"
	default:
		b, _ := json.Marshal(n.scalar) // a decoded scalar always marshals
		return string(b)
	}
}

// A field is one property of an object of the definition.
type field struct {
	name      string
	schema    *node
	required  bool
	condition string // when the field applies, from an if of the schema, or empty
}

// A definitionSchema resolves $ref into its $defs and lists each object's fields.
type definitionSchema struct{ defs *node }

// resolve follows $ref, and returns the schema it names and the $defs names it went through.
func (s definitionSchema) resolve(n *node) (*node, []string, error) {
	var refs []string

	for n != nil && n.get("$ref") != nil {
		name, ok := strings.CutPrefix(n.str("$ref"), "#/$defs/")
		target := s.defs.get(name)

		if !ok || target == nil {
			return nil, nil, fmt.Errorf("%w: $ref %q names no $defs entry", errSchema, n.str("$ref"))
		}

		refs = append(refs, name)
		n = target
	}

	return n, refs, nil
}

// typeOf names the type of schema n in words: string, array of object, map of string, one of `"a"`, `"b"`.
func (s definitionSchema) typeOf(n *node, depth int) (string, error) {
	n, _, err := s.resolve(n)
	if err != nil || n == nil {
		return "any", err
	}

	if depth > 8 {
		return "object", nil
	}

	switch {
	case n.get("const") != nil:
		return "`" + n.get("const").json() + "`", nil
	case n.get("enum") != nil:
		values := make([]string, len(n.get("enum").items))
		for i, v := range n.get("enum").items {
			values[i] = "`" + v.json() + "`"
		}

		return "one of " + strings.Join(values, ", "), nil
	}

	var names []string

	switch t := n.get("type"); {
	case t != nil && t.array:
		for _, item := range t.items {
			names = append(names, item.json())
		}

		return strings.ReplaceAll(strings.Join(names, " or "), `"`, ""), nil
	case t != nil:
		return s.namedType(n, n.str("type"), depth)
	}

	if n.get("properties") != nil {
		return "object", nil
	}

	var branches []*node

	for _, key := range []string{"oneOf", "anyOf"} {
		if n.get(key) != nil {
			branches = append(branches, n.get(key).items...)
		}
	}

	if n.get("if") != nil {
		branches = append(branches, n.get("then"), n.get("else"))
	}

	for _, b := range branches {
		if b == nil {
			continue
		}

		name, err := s.typeOf(b, depth+1)
		if err != nil {
			return "", err
		}

		if name != "any" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return "any", nil
	}

	return strings.Join(names, " or "), nil
}

func (s definitionSchema) namedType(n *node, t string, depth int) (string, error) {
	switch t {
	case "array":
		if n.get("items") == nil {
			return "array", nil
		}

		of, err := s.typeOf(n.get("items"), depth+1)

		return "array of " + of, err
	case "object":
		if ap := n.get("additionalProperties"); ap != nil && ap.object && n.get("properties") == nil {
			of, err := s.typeOf(ap, depth+1)

			return "map of " + of, err
		}

		return "object", nil
	case "string":
		switch {
		case n.str("pattern") != "":
			return "string matching `" + n.str("pattern") + "`", nil
		case n.str("format") != "":
			return "string (" + n.str("format") + ")", nil
		}
	}

	return t, nil
}

// fields lists the fields of schema n in schema order: its properties, those of the variants it
// may take (oneOf, anyOf, if/then/else, allOf), those of its array's elements and of its map's
// values. Fields of a then apply only when its if holds. A name is listed once, where it first appears. seen holds the $defs being listed, so a
// schema that refers to itself ends.
func (s definitionSchema) fields(n *node, seen []string) ([]field, error) {
	n, refs, err := s.resolve(n)
	if err != nil || n == nil {
		return nil, err
	}

	for _, r := range refs {
		if slices.Contains(seen, r) {
			return nil, nil
		}
	}

	seen = append(slices.Clone(seen), refs...)

	var out []field

	add := func(fs []field, condition string) {
		for _, f := range fs {
			if slices.ContainsFunc(out, func(o field) bool { return o.name == f.name }) {
				continue
			}

			if condition != "" && f.condition == "" {
				f.condition = condition
			}

			out = append(out, f)
		}
	}

	if props := n.get("properties"); props != nil {
		required := stringList(n.get("required"))
		for _, name := range props.keys {
			add([]field{{name: name, schema: props.fields[name], required: slices.Contains(required, name)}}, "")
		}
	}

	var variants []*node

	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if n.get(key) != nil {
			variants = append(variants, n.get(key).items...)
		}
	}

	variants = append(variants, n.get("else"), n.get("items"))

	if ap := n.get("additionalProperties"); ap != nil && ap.object {
		variants = append(variants, ap)
	}

	// The fields of then apply when its if holds; else's and the others' always may.
	if then := n.get("then"); then != nil {
		fs, err := s.fields(then, seen)
		if err != nil {
			return nil, err
		}

		add(fs, condition(n.get("if")))
	}

	for _, v := range variants {
		if v == nil {
			continue
		}

		fs, err := s.fields(v, seen)
		if err != nil {
			return nil, err
		}

		add(fs, "")
	}

	return out, nil
}

// condition says in words when an if of the form {properties: {k: {const: v}}} holds, or is empty.
func condition(ifSchema *node) string {
	props := ifSchema.get("properties")
	if props == nil || len(props.keys) != 1 || props.fields[props.keys[0]].get("const") == nil {
		return ""
	}

	key := props.keys[0]

	return "only when `" + key + "` is `" + props.fields[key].get("const").json() + "`"
}

func stringList(n *node) []string {
	if n == nil {
		return nil
	}

	var out []string

	for _, item := range n.items {
		if s, ok := item.scalar.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

// summary is a field's line: its type, required, its default and when it applies, then its
// description.
func (s definitionSchema) summary(f field) (string, error) {
	resolved, _, err := s.resolve(f.schema)
	if err != nil {
		return "", err
	}

	text, err := s.typeOf(f.schema, 0)
	if err != nil {
		return "", err
	}

	if f.required {
		text += ", required"
	}

	if d := firstOf(f.schema, resolved, "default"); d != nil {
		text += ", default `" + d.json() + "`"
	}

	if f.condition != "" {
		text += "; " + f.condition
	}

	text += "."

	if d := firstOf(f.schema, resolved, "description"); d != nil {
		desc, _ := d.scalar.(string)
		text += " " + escapeProse(strings.Join(strings.Fields(desc), " "))
	}

	return text, nil
}

// firstOf is key of the referring schema, else of the schema it resolves to.
func firstOf(referring, resolved *node, key string) *node {
	if v := referring.get(key); v != nil {
		return v
	}

	return resolved.get(key)
}

func (s definitionSchema) writeFields(b *strings.Builder, n *node, depth int, seen []string) error {
	fs, err := s.fields(n, seen)
	if err != nil {
		return err
	}

	for _, f := range fs {
		line, err := s.summary(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}

		b.WriteString(strings.Repeat("  ", depth) + "- `" + f.name + "`: " + line + "\n")

		if err := s.writeFields(b, f.schema, depth+1, seen); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}

	return nil
}

// renderDefinitionPage renders the factory definition reference from the schema's JSON: one
// section per top-level field, each with its fields nested below it.
func renderDefinitionPage(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	root, err := decodeNode(dec)
	if err != nil {
		return "", err
	}

	if root.get("properties") == nil {
		return "", fmt.Errorf("%w: no top-level properties", errSchema)
	}

	s := definitionSchema{defs: root.get("$defs")}

	var b strings.Builder

	b.WriteString(pageHead("Factory definition reference",
		"Every field of a factory definition, with its type, whether it is required, its default and what it means.", definitionSource))

	if d := root.str("description"); d != "" {
		b.WriteString(escapeProse(d) + "\n\n")
	}

	b.WriteString("Fields are listed in the schema's order, each nested under the field that holds it. " +
		"The fields under an array are those of each of its elements, and the fields under a map are those of each of its values.\n")

	top, err := s.fields(root, nil)
	if err != nil {
		return "", err
	}

	for _, f := range top {
		line, err := s.summary(f)
		if err != nil {
			return "", fmt.Errorf("%s: %w", f.name, err)
		}

		b.WriteString("\n## " + f.name + "\n\n" + line + "\n")

		var children strings.Builder
		if err := s.writeFields(&children, f.schema, 0, nil); err != nil {
			return "", fmt.Errorf("%s: %w", f.name, err)
		}

		if children.Len() > 0 {
			b.WriteString("\n" + children.String())
		}
	}

	return b.String(), nil
}
