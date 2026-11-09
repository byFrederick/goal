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

// typeObject is the type of a schema with properties, and typeAny that of one the page cannot
// name.
const (
	typeObject = "object"
	typeAny    = "any"
)

// The keywords that combine subschemas.
const (
	keyAllOf = "allOf"
	keyOneOf = "oneOf"
	keyAnyOf = "anyOf"
)

// typeOf names the type of schema n in words: string, array of object, map of string, one of `"a"`, `"b"`.
func (s definitionSchema) typeOf(n *node, depth int) (string, error) {
	n, _, err := s.resolve(n)
	if err != nil || n == nil {
		return typeAny, err
	}

	if depth > 8 {
		return typeObject, nil
	}

	if literal := literalType(n); literal != "" {
		return literal, nil
	}

	switch t := n.get("type"); {
	case t != nil && t.array:
		names := make([]string, len(t.items))
		for i, item := range t.items {
			names[i] = item.json()
		}

		return strings.ReplaceAll(strings.Join(names, " or "), `"`, ""), nil
	case t != nil:
		return s.namedType(n, n.str("type"), depth)
	case n.get("properties") != nil:
		return typeObject, nil
	}

	return s.variantTypes(n, depth)
}

// literalType names a const or an enum's values, or is empty.
func literalType(n *node) string {
	switch {
	case n.get("const") != nil:
		return "`" + n.get("const").json() + "`"
	case n.get("enum") != nil:
		values := make([]string, len(n.get("enum").items))
		for i, v := range n.get("enum").items {
			values[i] = "`" + v.json() + "`"
		}

		return "one of " + strings.Join(values, ", ")
	}

	return ""
}

// variantTypes names the types of a schema without a type of its own: those of its oneOf, anyOf
// or if's then and else, joined by or. A then without a type of its own has the type its if tests
// for, as in {if: {type: string}, then: {minLength: 1}, else: {type: object}}.
func (s definitionSchema) variantTypes(n *node, depth int) (string, error) {
	var branches []*node

	for _, key := range []string{keyOneOf, keyAnyOf} {
		if n.get(key) != nil {
			branches = append(branches, n.get(key).items...)
		}
	}

	if n.get("if") != nil {
		branches = append(branches, thenOrItsTest(n), n.get("else"))
	}

	var names []string

	for _, b := range branches {
		if b == nil {
			continue
		}

		name, err := s.typeOf(b, depth+1)
		if err != nil {
			return "", err
		}

		if name != typeAny && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return typeAny, nil
	}

	return strings.Join(names, " or "), nil
}

// thenOrItsTest is n's then, or its if when that only tests the type and the then states none.
func thenOrItsTest(n *node) *node {
	then := n.get("then")
	if then != nil && typeTest(n.get("if")) && then.get("type") == nil && then.get("$ref") == nil && then.get("properties") == nil {
		return n.get("if")
	}

	return then
}

func (s definitionSchema) namedType(n *node, t string, depth int) (string, error) {
	switch t {
	case "array":
		if n.get("items") == nil {
			return "array", nil
		}

		of, err := s.typeOf(n.get("items"), depth+1)

		return "array of " + of, err
	case typeObject:
		if ap := n.get("additionalProperties"); ap != nil && ap.object && n.get("properties") == nil {
			of, err := s.typeOf(ap, depth+1)

			return "map of " + of, err
		}

		return typeObject, nil
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
	out := ownFields(n)

	// The fields of then apply when its if holds; else's and the others' always may.
	branches := []struct {
		schema    *node
		condition string
	}{{n.get("then"), condition(n.get("if"))}}

	for _, v := range variants(n) {
		branches = append(branches, struct {
			schema    *node
			condition string
		}{v, ""})
	}

	for _, b := range branches {
		if b.schema == nil {
			continue
		}

		fs, err := s.fields(b.schema, seen)
		if err != nil {
			return nil, err
		}

		out = addFields(out, fs, b.condition)
	}

	return out, nil
}

// ownFields are the properties of n, in order.
func ownFields(n *node) []field {
	props := n.get("properties")
	if props == nil {
		return nil
	}

	required := stringList(n.get("required"))
	out := make([]field, len(props.keys))

	for i, name := range props.keys {
		out[i] = field{name: name, schema: props.fields[name], required: slices.Contains(required, name)}
	}

	return out
}

// addFields adds to out each of fs whose name it lacks, applying when condition holds.
func addFields(out, fs []field, condition string) []field {
	for _, f := range fs {
		if slices.ContainsFunc(out, func(o field) bool { return o.name == f.name }) {
			continue
		}

		if f.condition == "" {
			f.condition = condition
		}

		out = append(out, f)
	}

	return out
}

// variants are the schemas whose fields a value of n may also have, besides its then: its oneOf,
// anyOf and allOf entries, its else, its array's elements and its map's values.
func variants(n *node) []*node {
	var out []*node

	for _, key := range []string{keyOneOf, keyAnyOf, keyAllOf} {
		if n.get(key) != nil {
			out = append(out, n.get(key).items...)
		}
	}

	if ap := n.get("additionalProperties"); ap != nil && ap.object {
		out = append(out, ap)
	}

	for _, v := range []*node{n.get("else"), n.get("items")} {
		if v != nil {
			out = append(out, v)
		}
	}

	return out
}

// condition says in words when an if of the form {properties: {k: {const: v}}} holds, or is empty.
func condition(ifSchema *node) string {
	if clause := conditionClause(ifSchema); clause != "" {
		return "only when " + clause
	}

	return ""
}

// conditionClause is "`k` is `v`" for an if of the form {properties: {k: {const: v}}}, or empty.
func conditionClause(ifSchema *node) string {
	props := ifSchema.get("properties")
	if props == nil || len(props.keys) != 1 || props.fields[props.keys[0]].get("const") == nil {
		return ""
	}

	key := props.keys[0]

	return "`" + key + "` is `" + props.fields[key].get("const").json() + "`"
}

// typeTest reports whether an if only tests the value's type, as {type: string} does: its then
// and else are then the value's variants, not conditions on an object.
func typeTest(ifSchema *node) bool {
	return ifSchema != nil && len(ifSchema.keys) == 1 && ifSchema.get("type") != nil
}

// A ruleScope is when the rules found under it apply: when holds the conditions of the ifs around
// them, and derivable is false under an if the generator cannot put in words, where only a rule
// with its own description can be rendered.
type ruleScope struct {
	when      string
	derivable bool
}

// within is the scope of the then (or, holds false, the else) of if.
func (r ruleScope) within(ifSchema *node, holds bool) ruleScope {
	if typeTest(ifSchema) {
		return r
	}

	clause := conditionClause(ifSchema)
	if !holds || clause == "" {
		return ruleScope{when: r.when}
	}

	if r.when != "" {
		clause = r.when + " and " + clause
	}

	return ruleScope{when: clause, derivable: r.derivable}
}

// constrainsOnly reports whether schema n only constrains the object it applies to: it names no
// type or values of its own and adds no field, save fields it pins to a const or forbids.
func constrainsOnly(n *node) bool {
	if n == nil || !n.object {
		return false
	}

	for _, k := range []string{"type", "$ref", "const", "enum", "items", "additionalProperties"} {
		if n.get(k) != nil {
			return false
		}
	}

	return pinsOnly(n.get("properties")) && subschemasConstrainOnly(n)
}

// pinsOnly reports whether each of properties is pinned to a const or forbidden.
func pinsOnly(properties *node) bool {
	if properties == nil {
		return true
	}

	for _, k := range properties.keys {
		p := properties.fields[k]
		if p.scalar != false && (len(p.keys) != 1 || p.get("const") == nil) {
			return false
		}
	}

	return true
}

// subschemasConstrainOnly reports whether n's then, else, not and combined subschemas all only
// constrain.
func subschemasConstrainOnly(n *node) bool {
	for _, k := range []string{"then", "else", "not"} {
		if v := n.get(k); v != nil && !constrainsOnly(v) {
			return false
		}
	}

	for _, k := range []string{keyAllOf, keyOneOf, keyAnyOf} {
		if v := n.get(k); v != nil && !allConstrainOnly(v) {
			return false
		}
	}

	return true
}

// allConstrainOnly reports whether a oneOf or anyOf is a rule on its object rather than a choice of
// the value's shapes: every branch only constrains.
func allConstrainOnly(branches *node) bool {
	if len(branches.items) == 0 {
		return false
	}

	for _, b := range branches.items {
		if !constrainsOnly(b) {
			return false
		}
	}

	return true
}

// A rulePart is a subschema of an object's schema that is a rule, or that may hold rules.
type rulePart struct {
	schema *node
	path   string
	scope  ruleScope
	rule   bool
}

// ruleParts are the subschemas of n, in schema order, along the paths fields takes: its allOf
// entries, its oneOf and anyOf (one rule when every branch only constrains, else each branch), its
// then and else, its array's elements and its map's values.
func ruleParts(n *node, scope ruleScope, path string) []rulePart {
	var out []rulePart

	if all := n.get(keyAllOf); all != nil {
		for i, e := range all.items {
			at := fmt.Sprintf("%s%s[%d]", path, keyAllOf, i)
			out = append(out, rulePart{schema: e, path: at, scope: scope, rule: constrainsOnly(e)})
		}
	}

	for _, key := range []string{keyOneOf, keyAnyOf} {
		branches := n.get(key)

		switch {
		case branches == nil:
		case allConstrainOnly(branches):
			choice := &node{object: true, keys: []string{key}, fields: map[string]*node{key: branches}}
			out = append(out, rulePart{schema: choice, path: path + key, scope: scope, rule: true})
		default:
			for i, b := range branches.items {
				out = append(out, rulePart{schema: b, path: fmt.Sprintf("%s%s[%d]", path, key, i), scope: scope})
			}
		}
	}

	nested := []struct {
		key   string
		scope ruleScope
	}{
		{"then", scope.within(n.get("if"), true)},
		{"else", scope.within(n.get("if"), false)},
		{"items", scope},
		{"additionalProperties", scope},
	}

	for _, v := range nested {
		if sub := n.get(v.key); sub != nil && sub.object {
			out = append(out, rulePart{schema: sub, path: path + v.key, scope: v.scope})
		}
	}

	return out
}

// rules lists, in schema order and each once, the rules schema n states on its object: its allOf
// entries and its oneOf or anyOf whose branches only constrain, and those inside the subschemas
// ruleParts names. path names where n is in the schema, for errors.
func (s definitionSchema) rules(n *node, scope ruleScope, path string, seen []string) ([]string, error) {
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

	var out []string

	for _, part := range ruleParts(n, scope, path) {
		var found []string

		if part.rule {
			r, err := ruleText(part.schema, part.scope, part.path)
			if err != nil {
				return nil, err
			}

			found = []string{r}
		} else if found, err = s.rules(part.schema, part.scope, part.path+".", seen); err != nil {
			return nil, err
		}

		for _, r := range found {
			if !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}

	return out, nil
}

// ruleText is a rule in words: its description, else what the generator derives from an if
// {properties: {k: {const: v}}} with a then, and a oneOf or anyOf of branches that each require one
// field. Any other rule needs a description.
func ruleText(e *node, scope ruleScope, path string) (string, error) {
	if d := e.str("description"); d != "" {
		return escapeProse(strings.Join(strings.Fields(d), " ")), nil
	}

	underivable := fmt.Errorf("%s: %w: a rule with no description and a shape the generator cannot put in words; "+
		"give it a description", path, errSchema)

	if !scope.derivable {
		return "", underivable
	}

	when, body := scope.when, e

	if e.get("if") != nil {
		clause := conditionClause(e.get("if"))
		if clause == "" || len(e.keys) != 2 || e.get("then") == nil {
			return "", underivable
		}

		if when != "" {
			clause = when + " and " + clause
		}

		when, body = clause, e.get("then")
	}

	text := choiceText(body)
	if text == "" {
		return "", underivable
	}

	if when != "" {
		return "When " + when + ", " + text + ".", nil
	}

	return strings.ToUpper(text[:1]) + text[1:] + ".", nil
}

// choiceText is "exactly one of `a` or `b` is set" for {oneOf: [{required: [a]}, {required: [b]}]},
// "at least one of…" for the same anyOf, or empty for any other schema.
func choiceText(n *node) string {
	if len(n.keys) != 1 {
		return ""
	}

	quantity := map[string]string{keyOneOf: "exactly one", keyAnyOf: "at least one"}[n.keys[0]]
	if quantity == "" {
		return ""
	}

	var names []string

	for _, b := range n.fields[n.keys[0]].items {
		required := stringList(b.get("required"))
		if len(b.keys) != 1 || len(required) != 1 {
			return ""
		}

		names = append(names, "`"+required[0]+"`")
	}

	if len(names) < 2 {
		return ""
	}

	return quantity + " of " + strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1] + " is set"
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

	if b := bounds(f.schema, resolved); b != "" {
		text += ", " + b
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

	// An array's elements may say what each is, such as the shorthand a string element stands for.
	if desc := resolved.get("items").str("description"); desc != "" {
		text += " " + escapeProse(strings.Join(strings.Fields(desc), " "))
	}

	return text, nil
}

// bounds says in words the bounds of a number, a string's length or an array's size: "1 to 20",
// "at least 1", "more than 0", "at most 40 characters", "at least 2 items". It is empty without any.
func bounds(referring, resolved *node) string {
	limit := func(key string) string {
		if v := firstOf(referring, resolved, key); v != nil {
			return v.json()
		}

		return ""
	}

	groups := []struct{ min, max, unit string }{
		{"minimum", "maximum", ""}, {"minLength", "maxLength", "character"}, {"minItems", "maxItems", "item"},
	}

	for _, g := range groups {
		lo, hi, above := limit(g.min), limit(g.max), false
		if g.unit == "" && lo == "" {
			lo, above = limit("exclusiveMinimum"), true
		}

		if text := rangeText(lo, hi, above); text != "" {
			return text + unitOf(g.unit, lo, hi)
		}
	}

	return ""
}

// rangeText is a range from lo (above it, when above holds) to hi, either of which may be empty.
func rangeText(lo, hi string, above bool) string {
	switch {
	case lo != "" && hi != "" && above:
		return "more than " + lo + " and at most " + hi
	case lo != "" && hi == lo:
		return lo
	case lo != "" && hi != "":
		return lo + " to " + hi
	case lo != "" && above:
		return "more than " + lo
	case lo != "":
		return "at least " + lo
	case hi != "":
		return "at most " + hi
	}

	return ""
}

// unitOf is " characters" or " items" after a range, singular when its one number is 1.
func unitOf(unit, lo, hi string) string {
	if unit == "" {
		return ""
	}

	if (lo == "1" || lo == "") && (hi == "1" || hi == "") {
		return " " + unit
	}

	return " " + unit + "s"
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

	rules, err := s.rules(n, ruleScope{derivable: true}, "", seen)
	if err != nil {
		return err
	}

	if len(rules) > 0 {
		b.WriteString(strings.Repeat("  ", depth) + "- Rules:\n")

		for _, r := range rules {
			b.WriteString(strings.Repeat("  ", depth+1) + "- " + r + "\n")
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
