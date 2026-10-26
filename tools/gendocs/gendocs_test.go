package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wantChangeMap = generatedNote + `
# Change map

Intro.

| When you change | Also change | Run |
| --- | --- | --- |
| ` + "`a.go`" + ` | ` + "`a_test.go`" + ` | ` + "`task a`, `task lint`" + ` |
| ` + "`b.go`" + ` | The [docs](../b.md) | ` + "`task b`" + ` |

## Generated files

Hand-written.
`

const wantADRIndex = generatedNote + `
# Architecture decision records

One line per ADR: its number, its title and the first part of its status. To add one, write ` + "`docs/adr/NNNN-<slug>.md`" + ` with the next number and run ` + "`task docs:gen`" + `.

- [ADR 0001](0001-alpha.md): Alpha. Status: accepted.
- [ADR 0002](0002-beta.md): Beta: the second. Status: accepted as amended (2026-10-24; amendment).
`

// copyRepo copies a fixture repository to a fresh directory, so a test may write into it.
func copyRepo(t *testing.T, name string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", name))); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestGenerate_ChangeMapRowsInFileNameOrder(t *testing.T) {
	t.Parallel()

	files, err := render(os.DirFS("testdata/repo"))
	if err != nil {
		t.Fatal(err)
	}

	if got := files[changeMapFile]; got != wantChangeMap {
		t.Errorf("%s =\n%s\nwant\n%s", changeMapFile, got, wantChangeMap)
	}
}

func TestGenerate_ADRIndex(t *testing.T) {
	t.Parallel()

	files, err := render(os.DirFS("testdata/repo"))
	if err != nil {
		t.Fatal(err)
	}

	if got := files[adrIndexFile]; got != wantADRIndex {
		t.Errorf("%s =\n%s\nwant\n%s", adrIndexFile, got, wantADRIndex)
	}
}

func TestGenerate_WritesBothFiles(t *testing.T) {
	t.Parallel()

	dir := copyRepo(t, "repo")

	if err := run([]string{"-C", dir}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	for file, want := range map[string]string{changeMapFile: wantChangeMap, adrIndexFile: wantADRIndex} {
		got, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != want {
			t.Errorf("%s =\n%s\nwant\n%s", file, got, want)
		}
	}
}

func TestGenerate_EntryMissingASection(t *testing.T) {
	t.Parallel()

	_, err := render(os.DirFS("testdata/missing-run"))
	if err == nil || !strings.Contains(err.Error(), "docs/engineering/change-map/10-first.md") || !strings.Contains(err.Error(), "## Run") {
		t.Fatalf("err = %v, want one naming the entry and its missing section", err)
	}
}

func TestParseEntry_Rejects(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"a pipe":           "## When you change\n\na | b\n\n## Also change\n\nx\n\n## Run\n\ny\n",
		"a line break":     "## When you change\n\na\nb\n\n## Also change\n\nx\n\n## Run\n\ny\n",
		"an empty cell":    "## When you change\n\n## Also change\n\nx\n\n## Run\n\ny\n",
		"another section":  "## When you change\n\na\n\n## Also change\n\nx\n\n## Run\n\ny\n\n## Notes\n\nz\n",
		"a repeat":         "## When you change\n\na\n\n## When you change\n\na\n\n## Also change\n\nx\n\n## Run\n\ny\n",
		"text before":      "intro\n\n## When you change\n\na\n\n## Also change\n\nx\n\n## Run\n\ny\n",
		"sections reorder": "## Also change\n\nx\n\n## When you change\n\na\n\n## Run\n\ny\n",
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := parseEntry(text); err == nil {
				t.Errorf("parseEntry(%q) succeeded, want an error", text)
			}
		})
	}
}

func TestParseADR_Rejects(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"no heading":        "Status: accepted\n",
		"another number":    "# ADR 0002 — Alpha\n\nStatus: accepted\n",
		"no status":         "# ADR 0001 — Alpha\n\n## Context\n",
		"heading not ADR":   "# Alpha\n\nStatus: accepted\n",
		"empty title":       "# ADR 0001 — \n\nStatus: accepted\n",
		"empty status":      "# ADR 0001 — Alpha\n\nStatus: \n",
		"status too late":   "# ADR 0001 — Alpha\n\n## Context\n\nStatus: accepted\n",
		"heading not first": "\nintro\n# ADR 0001 — Alpha\n\nStatus: accepted\n",
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := parseADR("0001-alpha.md", text); err == nil {
				t.Errorf("parseADR(%q) succeeded, want an error", text)
			}
		})
	}
}

func TestCheck_CleanTreePasses(t *testing.T) {
	t.Parallel()

	dir := copyRepo(t, "repo")

	if err := run([]string{"-C", dir}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"-C", dir, "-check"}, &bytes.Buffer{}); err != nil {
		t.Errorf("check on a fresh render: %v", err)
	}
}

func TestCheck_HandEditFails(t *testing.T) {
	t.Parallel()

	for _, file := range []string{changeMapFile, adrIndexFile} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			dir := copyRepo(t, "repo")

			if err := run([]string{"-C", dir}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}

			p := filepath.Join(dir, file)

			text, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}

			edited := strings.Replace(string(text), "Alpha", "Alpha, edited", 1)
			edited = strings.Replace(edited, "`a.go`", "`c.go`", 1)

			if err := os.WriteFile(p, []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer

			err = run([]string{"-C", dir, "-check"}, &out)
			if err == nil {
				t.Fatal("check after a hand edit succeeded, want an error")
			}

			if !strings.Contains(out.String(), file) {
				t.Errorf("output %q does not name %s", out.String(), file)
			}
		})
	}
}

func TestCheck_MissingFileFails(t *testing.T) {
	t.Parallel()

	dir := copyRepo(t, "repo")

	var out bytes.Buffer
	if err := run([]string{"-C", dir, "-check"}, &out); err == nil {
		t.Fatal("check with no generated files succeeded, want an error")
	}

	if !strings.Contains(out.String(), adrIndexFile) || !strings.Contains(out.String(), changeMapFile) {
		t.Errorf("output %q does not name both files", out.String())
	}
}
