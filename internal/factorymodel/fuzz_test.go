package factorymodel

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// FuzzApply makes random edits of one section of the corpus and checks Apply's guarantees: it
// never fails its own safety net, the document it writes parses and reads back as the edit, the
// other sections do not change, and every comment outside the edited section survives.
func FuzzApply(f *testing.F) {
	docs := corpus(f)
	names := make([]string, 0, len(docs))

	for name := range docs {
		names = append(names, name)
	}

	slices.Sort(names)

	for i := range names {
		for s := range Sections {
			f.Add(uint8(i), uint8(s), []byte{0, 1, 2, 3, 4, 5, 6, 7})
			f.Add(uint8(i), uint8(s), []byte{3, 0, 9, 1, 2, 4, 4, 1})
		}
	}

	f.Fuzz(func(t *testing.T, docIndex, sectionIndex uint8, ops []byte) {
		doc := docs[names[int(docIndex)%len(names)]]
		s := Sections[int(sectionIndex)%len(Sections)]

		m, r, err := FromYAML(doc, nil)
		if err != nil {
			t.Fatal(err)
		}

		value := mutate(m.Section(s), ops)

		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}

		out, err := Apply(doc, s, b)

		switch {
		case errors.Is(err, ErrModelInvalid):
			return
		case err != nil:
			t.Fatalf("apply %s %s: %v\n%s", names[int(docIndex)%len(names)], s, err, b)
		}

		if !readsBack(out, s, value, m) {
			t.Fatalf("%s does not read back as the edit:\n%s", s, out)
		}

		missing := slices.Clone(outsideComments(doc, r, s))
		for _, c := range comments(out) {
			if i := slices.Index(missing, c); i >= 0 {
				missing = slices.Delete(missing, i, i+1)
			}
		}

		if len(missing) > 0 {
			t.Fatalf("comments lost outside %s: %q\n%s", s, missing, out)
		}
	})
}

// outsideComments are the comments of doc that lie outside the lines section s spans (with the
// comments and blank lines right above it, which belong to its first node).
func outsideComments(doc []byte, r Ranges, s Section) []string {
	lines := strings.Split(string(doc), "\n")
	from, to := r.Section(s)

	for from > 1 && (strings.TrimSpace(lines[from-2]) == "" || strings.HasPrefix(strings.TrimSpace(lines[from-2]), "#")) {
		from--
	}

	var out []string

	for n, l := range lines {
		if line := n + 1; from > 0 && line >= from && line <= to {
			continue
		}

		if i := commentStart(l); i >= 0 {
			out = append(out, strings.TrimSpace(l[i:]))
		}
	}

	return out
}

// mutate applies edits chosen by ops to a copy of a model value: reorders, removals, new
// scalars, new keys and copies of items under a new identity.
func mutate(v any, ops []byte) any {
	b, _ := json.Marshal(v)
	out, _ := decode(b)

	for i := 0; i+2 < len(ops); i += 3 {
		targets := containers(out)
		if len(targets) == 0 {
			break
		}

		edit(targets[int(ops[i+1])%len(targets)], ops[i], int(ops[i+2]))
	}

	return out
}

// container is a map or list in a model value, with a way to replace its value in its parent.
type container struct {
	value any
	set   func(any)
}

func containers(v any) []container {
	var out []container

	var visit func(v any, set func(any))

	visit = func(v any, set func(any)) {
		switch x := v.(type) {
		case map[string]any:
			out = append(out, container{x, set})

			for _, k := range sortedKeys(x) {
				visit(x[k], func(nv any) { x[k] = nv })
			}
		case []any:
			out = append(out, container{x, set})

			for i := range x {
				visit(x[i], func(nv any) { x[i] = nv })
			}
		}
	}

	visit(v, func(any) {})

	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

func edit(c container, op byte, n int) {
	switch x := c.value.(type) {
	case []any:
		editList(c, x, op, n)
	case map[string]any:
		editMap(x, op, n)
	}
}

func editList(c container, x []any, op byte, n int) {
	if len(x) == 0 {
		c.set([]any{"item" + strconv.Itoa(n)})

		return
	}

	i := n % len(x)

	switch op % 4 {
	case 0:
		slices.Reverse(x)
	case 1:
		c.set(slices.Delete(slices.Clone(x), i, i+1))
	case 2:
		x[i] = scalarFor(n)
	default:
		c.set(append(slices.Clone(x), renamed(x[i], n)))
	}
}

func editMap(x map[string]any, op byte, n int) {
	keys := sortedKeys(x)

	switch op % 3 {
	case 0:
		if len(keys) > 0 {
			delete(x, keys[n%len(keys)])
		}
	case 1:
		if len(keys) > 0 {
			x[keys[n%len(keys)]] = scalarFor(n)
		}
	default:
		x["description"] = "added " + strconv.Itoa(n)
	}
}

func scalarFor(n int) any {
	switch n % 3 {
	case 0:
		return json.Number(strconv.Itoa(n))
	case 1:
		return n%2 == 0
	}

	return "value " + strconv.Itoa(n)
}

// renamed copies an item, giving it a new identity when it has one.
func renamed(v any, n int) any {
	b, _ := json.Marshal(v)
	cp, _ := decode(b)

	if m, ok := cp.(map[string]any); ok {
		for _, k := range []string{"id", "name"} {
			if _, has := m[k]; has {
				m[k] = "copy-" + strconv.Itoa(n)
			}
		}
	}

	return cp
}
