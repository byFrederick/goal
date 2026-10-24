package factorymodel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"
)

func corpus(tb testing.TB) map[string][]byte {
	tb.Helper()

	dir := os.DirFS("testdata/roundtrip")

	paths, err := fs.Glob(dir, "*.yaml")
	if err != nil || len(paths) == 0 {
		tb.Fatalf("no round-trip corpus: %v", err)
	}

	out := make(map[string][]byte, len(paths))

	for _, p := range paths {
		b, err := fs.ReadFile(dir, p)
		if err != nil {
			tb.Fatal(err)
		}

		out[p] = b
	}

	return out
}

func sectionJSON(tb testing.TB, m Model, s Section) json.RawMessage {
	tb.Helper()

	b, err := json.Marshal(m.Section(s))
	if err != nil {
		tb.Fatal(err)
	}

	return b
}

// dig follows keys and indexes into a model value; a missing step gives nil.
func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			l, _ := v.([]any)
			if k >= len(l) {
				return nil
			}

			v = l[k]
		}
	}

	return v
}

// The model shows what the Visual editor needs in its own words: the purpose even under its old
// key, prompts with their file's hash and length, built-in run modes as objects, connections with
// their settings and default names, and custom tools as a flag.
func TestFromYAML_ReadsTheSections(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"agents/architect.md": {Data: []byte("Write the spec first.\n")}}

	m, r, err := FromYAML(corpus(t)["commented.yaml"], files)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := json.Marshal(map[string]any{
		"purpose":    m.Basics["purpose"],
		"repository": m.Basics["repository"],
		"architect":  dig(m.Agents, "agents", 0),
		"engineer":   dig(m.Agents, "agents", 1, "builtins"),
		"browser":    m.Connections[1],
		"tickets":    dig(m.Connections, 0, "legacy"),
		"gate":       dig(m.Line, "stations", 3, "gate"),
		"librarian":  m.Line["librarian"],
		"budget":     m.Review["budget"],
	})

	want := `{"architect":{"custom_tools":true,"description":"Writes the spec","model":"claude-opus-5-5","name":"architect",` +
		`"prompt":{"path":"./agents/architect.md","sha256":"` + sha("Write the spec first.\n") + `","words":4},"skills":["acme-conventions"]},` +
		`"browser":{"agents":["engineer"],"connector":"playwright@2","legacy":false,"name":"browser","settings":{"origins":["https://preview.example.com"]},` +
		`"tools":{"browser__click":{"agents":["engineer"],"scope":"preview"}}},` +
		`"budget":{"daily_usd":20,"per_item_usd":2.5},` +
		`"engineer":{"edit":"allowed","read":"allowed","run":{"commands":["pnpm","go"],"mode":"allowed"},"web":"ask"},` +
		`"gate":{"max_rework":2,"on_reject":"build"},"librarian":"reviewer",` +
		`"purpose":"Builds and reviews changes to the acme example shop.",` +
		`"repository":{"token":"env:ACME_REPO_TOKEN","url":"https://code.example.com/acme/shop.git"},"tickets":true}`

	if string(got) != want {
		t.Errorf("model =\n%s\nwant\n%s", got, want)
	}

	for pointer, lines := range map[string][2]int{
		"/basics/purpose":                 {11, 11},
		"/line/stations/3":                {97, 100},
		"/line/stations/3/gate":           {100, 100},
		"/agents/agents/1/builtins":       {64, 68},
		"/connections/1/settings/origins": {42, 42},
	} {
		if r[pointer] != lines {
			t.Errorf("ranges[%s] = %v, want %v", pointer, r[pointer], lines)
		}
	}

	if from, to := r.Section(Review); from != 23 || to != 25 {
		t.Errorf("review spans lines %d-%d, want 23-25", from, to)
	}
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

func TestFromYAML_DocumentThatDoesNotParse(t *testing.T) {
	t.Parallel()

	_, _, err := FromYAML([]byte("apiVersion: goal/v1\nspec:\n  board: { stations: [\n"), nil)

	var pe *ParseError
	if !errors.As(err, &pe) || pe.Line == 0 {
		t.Fatalf("err = %v, want a located *ParseError", err)
	}
}

// Writing back the model a document reads as changes nothing, byte for byte.
func TestApply_UnchangedModelKeepsTheFile(t *testing.T) {
	t.Parallel()

	for name, doc := range corpus(t) {
		m, _, err := FromYAML(doc, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		for _, s := range Sections {
			out, err := Apply(doc, s, sectionJSON(t, m, s))
			if err != nil {
				t.Fatalf("%s %s: %v", name, s, err)
			}

			if !bytes.Equal(out, doc) {
				t.Errorf("%s %s: unchanged model rewrote the file:\n%s", name, s, out)
			}
		}
	}
}

// An edit and its undo give back the original file: every line the edit did not change is kept
// as written, with its spacing, quotes and comment alignment.
func TestApply_UndoRestoresTheOriginalBytes(t *testing.T) {
	t.Parallel()

	for name, doc := range corpus(t) {
		m, _, err := FromYAML(doc, nil)
		if err != nil {
			t.Fatal(err)
		}

		stations, _ := m.Line["stations"].([]any)
		edited := map[string]any{}

		maps.Copy(edited, m.Line)

		edited["stations"] = slices.Concat(stations[:1], []any{map[string]any{"id": "triage", "kind": "queue"}}, stations[1:])

		changed := apply(t, doc, Line, edited)
		if bytes.Equal(changed, doc) {
			t.Fatalf("%s: the edit changed nothing", name)
		}

		if undone := apply(t, changed, Line, m.Line); !bytes.Equal(undone, doc) {
			t.Errorf("%s: undo =\n%s\nwant\n%s", name, undone, doc)
		}
	}
}

func apply(tb testing.TB, doc []byte, s Section, v any) []byte {
	tb.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		tb.Fatal(err)
	}

	out, err := Apply(doc, s, b)
	if err != nil {
		tb.Fatalf("apply %s: %v", s, err)
	}

	return out
}

// Format keeps the document's meaning and comments, and a formatted document is a fixed point.
func TestFormat_IsStableAndKeepsMeaningAndComments(t *testing.T) {
	t.Parallel()

	for name, doc := range corpus(t) {
		once, err := Format(doc)
		if err != nil {
			t.Fatal(err)
		}

		twice, err := Format(once)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(once, twice) {
			t.Errorf("%s: format is not stable:\n%s\nthen\n%s", name, once, twice)
		}

		if !sameDoc(t, doc, once) {
			t.Errorf("%s: format changed the document's value:\n%s", name, once)
		}

		if got, want := comments(once), comments(doc); !slices.Equal(got, want) {
			t.Errorf("%s: comments %q, want %q", name, got, want)
		}

		if got, want := strings.Count(string(once), "\n\n"), strings.Count(string(doc), "\n\n"); got != want {
			t.Errorf("%s: %d blank lines, want %d:\n%s", name, got, want, once)
		}
	}
}

func sameDoc(tb testing.TB, a, b []byte) bool {
	tb.Helper()

	var va, vb any
	if err := yaml.Unmarshal(a, &va); err != nil {
		tb.Fatal(err)
	}

	if err := yaml.Unmarshal(b, &vb); err != nil {
		return false
	}

	ja, _ := json.Marshal(va)
	jb, _ := json.Marshal(vb)

	return bytes.Equal(ja, jb)
}

// comments lists a document's comment texts in order.
func comments(doc []byte) []string {
	var out []string

	scanComments(string(doc), func(c string) { out = append(out, c) })

	return out
}

func scanComments(doc string, fn func(string)) {
	for l := range strings.SplitSeq(doc, "\n") {
		if i := commentStart(l); i >= 0 {
			fn(strings.TrimSpace(l[i:]))
		}
	}
}

// commentStart is where a line's comment starts, outside quotes; -1 when it has none.
func commentStart(l string) int {
	var quote byte

	for i := range len(l) {
		c := l[i]

		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case (c == '"' || c == '\'') && (i == 0 || strings.ContainsRune(" [{,:", rune(l[i-1]))):
			quote = c
		case c == '#' && (i == 0 || l[i-1] == ' '):
			return i
		}
	}

	return -1
}
