package factorymodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the edit goldens' want.yaml")

// editCase is one golden: testdata/edits/<case>/ holds in.yaml, section (its name) and
// edit.json, a JSON Patch on that section's model value; want.yaml is the expected file.
type editCase struct {
	name, dir string
	in, want  []byte
	section   Section
	ops       []patchOp
}

type patchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	From  string          `json:"from"`
	Value json.RawMessage `json:"value"`
}

func editCases(t *testing.T) []editCase {
	t.Helper()

	dirs, err := filepath.Glob("testdata/edits/*")
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no edit goldens: %v", err)
	}

	cases := make([]editCase, 0, len(dirs))

	for _, d := range dirs {
		c := editCase{name: filepath.Base(d), dir: d, in: readFile(t, d, "in.yaml"), want: readFile(t, d, "want.yaml")}
		c.section = Section(strings.TrimSpace(string(readFile(t, d, "section"))))

		if err := json.Unmarshal(readFile(t, d, "edit.json"), &c.ops); err != nil {
			t.Fatalf("%s: %v", d, err)
		}

		cases = append(cases, c)
	}

	return cases
}

func readFile(t *testing.T, dir, name string) []byte {
	t.Helper()

	b, err := fs.ReadFile(os.DirFS(dir), name)
	if err != nil && (!*update || name != "want.yaml") {
		t.Fatal(err)
	}

	return b
}

// Visual edits change only what they say: comments travel with moved nodes, removed nodes take
// theirs along, new keys and items take their canonical place and their siblings' style.
func TestApply_EditGoldens(t *testing.T) {
	t.Parallel()

	for _, c := range editCases(t) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			m, _, err := FromYAML(c.in, nil)
			if err != nil {
				t.Fatal(err)
			}

			v := patched(t, m.Section(c.section), c.ops)
			got := apply(t, c.in, c.section, v)

			if *update {
				if err := os.WriteFile(filepath.Join(c.dir, "want.yaml"), got, 0o600); err != nil {
					t.Fatal(err)
				}

				return
			}

			if !bytes.Equal(got, c.want) {
				t.Errorf("got\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// patched applies JSON Patch operations (add, remove, replace, move) to a copy of v.
func patched(t *testing.T, v any, ops []patchOp) any {
	t.Helper()

	b, _ := json.Marshal(v)

	doc, err := decode(b)
	if err != nil {
		t.Fatal(err)
	}

	for _, op := range ops {
		var val any
		if len(op.Value) > 0 {
			if val, err = decode(op.Value); err != nil {
				t.Fatal(err)
			}
		}

		switch op.Op {
		case "add":
			doc = patchAt(doc, tokens(op.Path), func(parent any, last string) any { return insert(t, parent, last, val) })
		case "replace":
			doc = patchAt(doc, tokens(op.Path), func(parent any, last string) any {
				return insert(t, remove(t, parent, last), last, val)
			})
		case "remove":
			doc = patchAt(doc, tokens(op.Path), func(parent any, last string) any { return remove(t, parent, last) })
		case "move":
			moved := get(doc, tokens(op.From))
			doc = patchAt(doc, tokens(op.From), func(parent any, last string) any { return remove(t, parent, last) })
			doc = patchAt(doc, tokens(op.Path), func(parent any, last string) any { return insert(t, parent, last, moved) })
		default:
			t.Fatalf("op %s", op.Op)
		}
	}

	return doc
}

func tokens(p string) []string { return strings.Split(strings.TrimPrefix(p, "/"), "/") }

func get(v any, toks []string) any {
	for _, k := range toks {
		switch x := v.(type) {
		case map[string]any:
			v = x[k]
		case []any:
			i, _ := strconv.Atoi(k)
			v = x[i]
		}
	}

	return v
}

// patchAt replaces the parent of toks' last token with fn(parent, last).
func patchAt(v any, toks []string, fn func(parent any, last string) any) any {
	if len(toks) == 1 {
		return fn(v, toks[0])
	}

	switch x := v.(type) {
	case map[string]any:
		x[toks[0]] = patchAt(x[toks[0]], toks[1:], fn)
	case []any:
		i, _ := strconv.Atoi(toks[0])
		x[i] = patchAt(x[i], toks[1:], fn)
	}

	return v
}

func insert(t *testing.T, parent any, k string, val any) any {
	t.Helper()

	switch x := parent.(type) {
	case map[string]any:
		x[k] = val

		return x
	case []any:
		if k == "-" {
			return append(x, val)
		}

		i, _ := strconv.Atoi(k)

		return slices.Insert(x, i, val)
	}

	t.Fatalf("insert into %T", parent)

	return nil
}

func remove(t *testing.T, parent any, k string) any {
	t.Helper()

	switch x := parent.(type) {
	case map[string]any:
		delete(x, k)

		return x
	case []any:
		i, _ := strconv.Atoi(k)

		return slices.Delete(x, i, i+1)
	}

	t.Fatalf("remove from %T", parent)

	return nil
}

func TestApply_RefusesValuesOfTheWrongShape(t *testing.T) {
	t.Parallel()

	doc := corpus(t)["line.yaml"]

	for name, tt := range map[string]struct {
		section Section
		value   string
	}{
		"connections is a list":  {Connections, `{}`},
		"line is an object":      {Line, `[]`},
		"camelCase model key":    {Review, `{"budget": {"perItemUsd": 2}}`},
		"unknown section":        {Section("files"), `{}`},
		"not JSON":               {Line, `{`},
		"camelCase in a station": {Line, `{"stations": [{"id": "inbox", "kind": "queue", "maxRework": 1}]}`},
		"camelCase in a gate":    {Line, `{"stations": [{"id": "x", "kind": "agent", "gate": {"onReject": "x"}}]}`},
		"camelCase in the flow":  {Line, `{"flow": {"andon": {"reworkRate": 1}}}`},
	} {
		if _, err := Apply(doc, tt.section, json.RawMessage(tt.value)); !errors.Is(err, ErrModelInvalid) {
			t.Errorf("%s: err = %v, want ErrModelInvalid", name, err)
		}
	}
}

// A blank factory starts without a line: the first station creates spec.board where the
// canonical order puts it.
func TestApply_CreatesMissingParents(t *testing.T) {
	t.Parallel()

	doc := []byte("apiVersion: goal/v1\nkind: Factory\nmetadata:\n  id: 00000000-0000-4000-8000-000000000000\n  name: acme-blank\n" +
		"spec:\n  runtime: {runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: bedrock}\n")

	got := apply(t, doc, Line, map[string]any{"stations": []any{map[string]any{"id": "inbox", "kind": "queue"}}})
	want := string(doc) + "  board:\n    stations:\n      - id: inbox\n        kind: queue\n"

	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestApply_DocumentThatDoesNotParse(t *testing.T) {
	t.Parallel()

	_, err := Apply([]byte("spec:\n  board: [\n"), Line, json.RawMessage(`{}`))

	var pe *ParseError
	if !errors.As(err, &pe) || pe.Line == 0 {
		t.Fatalf("err = %v, want a located *ParseError", err)
	}
}
