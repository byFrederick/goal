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

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return root
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

	if err := run([]string{"-C", dir}, &bytes.Buffer{}, testUsage); err != nil {
		t.Fatal(err)
	}

	for file, want := range map[string]string{changeMapFile: wantChangeMap, adrIndexFile: wantADRIndex} {
		got, err := openRoot(t, dir).ReadFile(file)
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

func TestValidate_StaleGeneratedFilesPass(t *testing.T) {
	t.Parallel()

	dir := copyRepo(t, "repo")

	if err := run([]string{"-C", dir}, &bytes.Buffer{}, testUsage); err != nil {
		t.Fatal(err)
	}

	root := openRoot(t, dir)

	for _, file := range generatedFiles {
		if err := root.WriteFile(file, []byte("stale, edited by hand\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := run([]string{"-C", dir, checkFlag}, &bytes.Buffer{}, testUsage); err != nil {
		t.Errorf("check with stale generated files: %v, want it to pass", err)
	}

	for _, file := range generatedFiles {
		got, err := root.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != "stale, edited by hand\n" {
			t.Errorf("check rewrote %s, want it to write nothing", file)
		}
	}
}

func TestValidate_MissingGeneratedFilesPass(t *testing.T) {
	t.Parallel()

	dir := copyRepo(t, "repo")

	if err := run([]string{"-C", dir, checkFlag}, &bytes.Buffer{}, testUsage); err != nil {
		t.Errorf("check with no generated files: %v, want it to pass", err)
	}

	for _, file := range generatedFiles {
		if _, err := openRoot(t, dir).Stat(file); err == nil {
			t.Errorf("check wrote %s, want it to write nothing", file)
		}
	}
}

func TestValidate_RejectsBadEntry(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		file, text, want string
	}{
		"a change-map entry without a section": {
			"docs/engineering/change-map/20-second.md", "## When you change\n\na\n\n## Also change\n\nx\n", "20-second.md",
		},
		"an ADR whose heading has another number": {
			"docs/adr/0003-gamma.md", "# ADR 0004 — Gamma\n\nStatus: accepted\n", "0003-gamma.md",
		},
		"an ADR without a title": {
			"docs/adr/0003-gamma.md", "# ADR 0003 — \n\nStatus: accepted\n", "0003-gamma.md",
		},
		"an ADR without a status": {
			"docs/adr/0003-gamma.md", "# ADR 0003 — Gamma\n\n## Context\n", "0003-gamma.md",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := copyRepo(t, "repo")

			if err := openRoot(t, dir).WriteFile(tt.file, []byte(tt.text), 0o600); err != nil {
				t.Fatal(err)
			}

			err := run([]string{"-C", dir, checkFlag}, &bytes.Buffer{}, testUsage)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("check = %v, want an error naming %s", err, tt.want)
			}
		})
	}
}
