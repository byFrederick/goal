package factorymodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ErrUnsafe means an edit did not survive its own check: re-reading the written document gave
// a different model. Nothing should be saved; it is a bug, not a user error.
var ErrUnsafe = errors.New("factorymodel: the edited document does not read back as the edit")

// Apply writes v, the new value of section s, into doc, a factory.yaml. Only the YAML the section
// owns changes; within it, existing nodes are changed in place (keeping their comments and
// style), sequence items are matched by identity (stations by id, agents, skills and
// connections by name), so reordering moves nodes with their comments, and new keys and items
// take the canonical place and their siblings' style. A value equal to the current one returns
// doc unchanged, byte for byte; otherwise every line the edit did not touch is kept as it was.
//
// A doc that does not parse is a *ParseError; a value of the wrong shape is ErrModelInvalid.
func Apply(doc []byte, s Section, v json.RawMessage) ([]byte, error) {
	t, err := parse(doc)
	if err != nil {
		return nil, err
	}

	value, err := decode(v)
	if err != nil {
		return nil, err
	}

	if err := checkValue(s, value); err != nil {
		return nil, err
	}

	before := readModel(t.root, nil)
	if equal(normalizeSection(s, before.Section(s)), normalizeSection(s, value)) {
		return doc, nil
	}

	norm, err := render(t)
	if err != nil {
		return nil, err
	}

	applySection(t, s, value)
	keepAliasesValid(t.doc)

	edited, err := render(t)
	if err != nil {
		return nil, err
	}

	for _, out := range [][]byte{keepOriginalLines(doc, norm, edited), edited} {
		if readsBack(out, s, value, before) {
			return out, nil
		}
	}

	return nil, ErrUnsafe
}

// readsBack is Apply's safety net: out's model has the requested section value and every other
// section as before.
func readsBack(out []byte, s Section, want any, before Model) bool {
	t, err := parse(out)
	if err != nil {
		return false
	}

	after := readModel(t.root, nil)

	for _, sec := range Sections {
		w := before.Section(sec)
		if sec == s {
			w = want
		}

		if !equal(normalizeSection(sec, after.Section(sec)), normalizeSection(sec, w)) {
			return false
		}
	}

	return true
}

func decode(v json.RawMessage) (any, error) {
	d := json.NewDecoder(bytes.NewReader(v))
	d.UseNumber()

	var out any
	if err := d.Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModelInvalid, err)
	}

	return out, nil
}

// part is the value of binding b in a section's value, and whether it is there.
func part(v any, b binding) (any, bool) {
	if b.key == "" {
		return v, true
	}

	m, _ := v.(map[string]any)
	p, ok := m[b.key]

	return p, ok
}

func applySection(t *tree, s Section, v any) {
	for _, b := range sections[s] {
		val, present := part(v, b)
		p := b.path

		switch b.key {
		case keyPurpose:
			p = purposeTarget(t.root, val)
		case keyLibrarian:
			if empty(val) {
				removeLibrarian(t.root)

				continue
			}
		}

		set(t.root, p, b.shape, val, present && !empty(val))
	}
}

// purposeTarget writes an edited purpose under metadata.purpose: an only-description document
// has its key renamed, keeping the node and its comments.
func purposeTarget(root *yaml.Node, v any) []string {
	p := purposePath(root)
	if p[1] == keyDescription {
		if cur := lookup(root, p); cur != nil && !equal(scalar(cur), v) {
			renameKey(lookup(root, p[:1]), keyDescription, keyPurpose)

			return []string{keyMetadata, keyPurpose}
		}
	}

	return p
}

func renameKey(m *yaml.Node, from, to string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == from {
			m.Content[i].Value = to
		}
	}
}

// removeLibrarian drops learning.librarian, and learning when nothing else is left in it.
func removeLibrarian(root *yaml.Node) {
	learning := lookup(root, []string{keySpec, keyLearning})
	if learning == nil {
		return
	}

	deleteKey(learning, keyLibrarian)

	if len(learning.Content) == 0 {
		deleteKey(lookup(root, []string{keySpec}), keyLearning)
	}
}

// set writes v at path below root, creating missing parents, or deletes the key when !present.
func set(root *yaml.Node, path []string, s *shape, v any, present bool) {
	parent := root

	for i, k := range path[:len(path)-1] {
		next := mapValue(parent, k)
		if next == nil || next.Kind != yaml.MappingNode {
			if !present {
				return
			}

			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setKey(parent, k, next, containerOrder[strings.Join(path[:i], ".")])
		}

		parent = next
	}

	key := path[len(path)-1]

	if !present {
		deleteKey(parent, key)

		return
	}

	order := containerOrder[strings.Join(path[:len(path)-1], ".")]
	flow := parent.Style&yaml.FlowStyle != 0

	if cur := entryValue(parent, key); cur != nil {
		setKey(parent, key, mergeNode(cur, s, v, flow), order)

		return
	}

	setKey(parent, key, build(v, s, flow), order)
}

// entryValue is the value node of key in m as written (an alias stays an alias).
func entryValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	return nil
}

// setKey replaces key's value in m, or inserts the pair at its canonical place.
func setKey(m *yaml.Node, key string, val *yaml.Node, order []string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val

			return
		}
	}

	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: key}
	m.Content = slices.Insert(m.Content, insertAt(m, key, order), k, val)
}

// insertAt is where a new key goes: after the nearest key before it in the canonical order,
// else before the nearest key after it, else at the end.
func insertAt(m *yaml.Node, key string, order []string) int {
	pos := slices.Index(order, key)
	if pos < 0 {
		return len(m.Content)
	}

	after, before := -1, -1

	for i := 0; i+1 < len(m.Content); i += 2 {
		switch p := slices.Index(order, m.Content[i].Value); {
		case p < 0:
		case p < pos:
			after = i
		case p > pos && before < 0:
			before = i
		}
	}

	switch {
	case after >= 0:
		return after + 2
	case before >= 0:
		return before
	}

	return len(m.Content)
}

func deleteKey(m *yaml.Node, key string) {
	if m == nil {
		return
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = slices.Delete(m.Content, i, i+2)

			return
		}
	}
}

// keepAliasesValid replaces each alias that no longer comes after its anchor (the anchored node
// was removed, or moved below it) with a copy of the value it named.
func keepAliasesValid(doc *yaml.Node) {
	defined := map[*yaml.Node]bool{}

	var visit func(*yaml.Node)

	visit = func(n *yaml.Node) {
		for i, c := range n.Content {
			if c.Kind == yaml.AliasNode && !defined[c.Alias] {
				n.Content[i] = withComments(unanchored(c.Alias), c)
				c = n.Content[i]
			}

			if c.Anchor != "" {
				defined[c] = true
			}

			visit(c)
		}
	}

	visit(doc)
}

func unanchored(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.AliasNode {
		return unanchored(n.Alias)
	}

	cp := *n
	cp.Anchor = ""
	cp.Content = make([]*yaml.Node, len(n.Content))

	for i, c := range n.Content {
		cp.Content[i] = unanchored(c)
	}

	return &cp
}
