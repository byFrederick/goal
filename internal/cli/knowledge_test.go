package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The command and the source the knowledge tests use.
const (
	cmdKnowledge = "knowledge"
	srcHandbook  = "handbook"
	kindUpload   = "upload"
	fileLeave    = "leave.md"
	diskRunbook  = "db/disk.md"
)

// expectOutput fails t unless out holds every one of want and none of absent.
func expectOutput(t *testing.T, what, out string, want []string, absent ...string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("%s = %q; want %q in it", what, out, w)
		}
	}

	for _, a := range absent {
		if strings.Contains(out, a) {
			t.Errorf("%s = %q; want no %q in it", what, out, a)
		}
	}
}

// files writes text files (name → text) to a new folder and returns their paths.
func files(t *testing.T, texts map[string]string) []string {
	t.Helper()

	dir := t.TempDir()
	paths := []string{}

	for name, text := range texts {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}

		paths = append(paths, p)
	}

	return paths
}

// draftFile is the factory's draft file at path, pulled as goal factory pull does.
func (g *defGoald) draftFile(t *testing.T, version, path string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "pulled")
	g.mustGoal(t, "factory", "pull", orgAcme, dir, "--version", version)

	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path))) //nolint:gosec // the test's own pulled folder
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}

	return string(raw)
}

// goal knowledge add --factories lists the new source in each factory's draft, keeping the
// file's comments, and publishes nothing: the current version still lists none.
func TestKnowledge_AddWritesEachFactoryDraft(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	commented := strings.Replace(factoryYAML, "spec:\n", "# The writer line: keep it small.\nspec:\n", 1)
	g.mustGoal(t, "factory", "push", newCopy(t, map[string]string{yamlPath: commented}), "--publish")

	expectOutput(t, "add", g.mustGoal(t, cmdKnowledge, "add", srcHandbook, "--kind", kindUpload, "--read-when", "Before you answer about leave.",
		"--factories", "acme"), []string{"added knowledge source handbook", "listed handbook in acme's draft"})
	expectOutput(t, "add another", g.mustGoal(t, cmdKnowledge, "add", "runbooks", "--kind", kindUpload, "--factories", "acme"),
		[]string{"listed runbooks in acme's draft"})

	draft := g.draftFile(t, "draft", yamlPath)
	expectOutput(t, "the draft", draft, []string{"# The writer line: keep it small.", "knowledge:", "- handbook", "- runbooks"})

	if strings.Count(draft, "handbook") != 1 {
		t.Errorf("the draft = %q; want handbook listed once", draft)
	}

	expectOutput(t, "the current version", g.draftFile(t, "current", yamlPath), nil, "handbook")

	src, err := g.kb.Source(g.ctx(t), srcHandbook)
	if err != nil || src.ReadWhen != "Before you answer about leave." {
		t.Errorf("handbook = %+v, %v; want its read-when line", src, err)
	}
}

// Files upload at their paths, folders and all; once synced, files lists and finds them and cat
// prints one; rm removes an upload, then the source.
func TestKnowledge_CatAndFiles(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.mustGoal(t, cmdKnowledge, "add", srcHandbook, "--kind", kindUpload)

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{diskRunbook: "# Disk\n\nBelow 85% is noise.", fileLeave: "# Leave\n\nAsk two weeks ahead.", ".git/HEAD": "x"})

	expectOutput(t, kindUpload, g.mustGoal(t, cmdKnowledge, kindUpload, srcHandbook, dir),
		[]string{"uploaded handbook/db/disk.md", "uploaded handbook/leave.md"}, ".git")

	g.sync(t)

	expectOutput(t, "files", g.mustGoal(t, cmdKnowledge, "files", srcHandbook), []string{"handbook/db/disk.md", "handbook/leave.md"})
	expectOutput(t, "files --find", g.mustGoal(t, cmdKnowledge, "files", "--find", "db/"), []string{"handbook/db/disk.md"}, fileLeave)
	expectOutput(t, "cat", g.mustGoal(t, cmdKnowledge, "cat", srcHandbook, diskRunbook), []string{"Below 85% is noise."})
	expectOutput(t, "reads", g.mustGoal(t, cmdKnowledge, "reads"), []string{"no reads"})

	expectOutput(t, "rm a file", g.mustGoal(t, cmdKnowledge, "rm", srcHandbook, fileLeave), []string{"removed handbook/leave.md"})
	g.sync(t)
	expectOutput(t, "files after rm", g.mustGoal(t, cmdKnowledge, "files", srcHandbook), []string{diskRunbook}, fileLeave)

	if _, err := g.goal(t, cmdKnowledge, "cat", srcHandbook, "db/none.md"); err == nil || !strings.Contains(err.Error(), "knowledge file not found") {
		t.Errorf("cat of a missing file = %v; want goald's 404", err)
	}

	expectOutput(t, "rm the source", g.mustGoal(t, cmdKnowledge, "rm", srcHandbook), []string{"removed knowledge source handbook"})
}

// Review 4 Go L18: files each within goald's limit upload together however large they are in
// all: one request each, since goald bounds a request by one file's limit.
func TestKnowledge_UploadsLargeFilesOneByOne(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.mustGoal(t, cmdKnowledge, "add", srcHandbook, "--kind", kindUpload, "--title", "Acme handbook")

	big := strings.Repeat("a", 13<<20)
	paths := files(t, map[string]string{"one.md": "# One\n\n" + big, "two.md": "# Two\n\n" + big})
	expectOutput(t, kindUpload, g.mustGoal(t, append([]string{cmdKnowledge, kindUpload, srcHandbook}, paths...)...),
		[]string{"handbook/one.md", "handbook/two.md"})
}

// goal knowledge check answers the Check of settings that are not saved, and saves nothing.
func TestKnowledge_Check(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	expectOutput(t, "check", g.mustGoal(t, cmdKnowledge, "check", srcHandbook, "--kind", kindUpload, "--read-when", "Before you answer about leave."),
		[]string{"access", "agents will see:", "/knowledge/handbook/`: Before you answer about leave. 0 files."})

	if _, err := g.kb.Source(g.ctx(t), srcHandbook); err == nil {
		t.Error("check saved the source")
	}
}

// What goald refuses reaches the user; a command line that is not one is a usage error.
func TestKnowledge_Refusals(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goal(t, cmdKnowledge, "add", srcHandbook, "--kind", "synced"); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Errorf("add synced without a secret = %v; want goald's refusal", err)
	}

	if _, err := g.goal(t, cmdKnowledge, kindUpload, "nothing", filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Error("upload of a missing file succeeded")
	}

	for _, args := range [][]string{
		{subAdd, srcHandbook},
		{"check", srcHandbook},
		{kindUpload, srcHandbook},
		{"files"},
		{"cat", srcHandbook},
		{"rm"},
		{"rm", "a", "b", "c"},
		{"reads", argExtra},
		{"search", "q"},
		{"usage"},
		{"uses", "d"},
		{"documents", srcHandbook},
		{"verdict", "d", "helped"},
	} {
		if _, err := g.goal(t, append([]string{cmdKnowledge}, args...)...); !errors.Is(err, errUsage) {
			t.Errorf("goal knowledge %s = %v; want the usage", strings.Join(args, " "), err)
		}
	}
}

// goal knowledge add --secret adds a synced source that reads with the secret, and goal knowledge
// sources shows it.
func TestKnowledge_AddWithASecret(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if out, err := g.goalIn(t, "test-token\n", cmdSecret, verbCreate, "docs-token", "--all-factories"); err != nil {
		t.Fatalf("create = %q, %v", out, err)
	}

	expectOutput(t, "add", g.mustGoal(t, cmdKnowledge, "add", "runbooks", "--kind", "synced", "--secret", "docs-token",
		"--config", "reader=gitdocs", "--config", "repository=https://github.com/acme/docs"), []string{"added knowledge source runbooks (synced)"})

	src, err := g.kb.Source(g.ctx(t), "runbooks")
	if err != nil || src.Secret != "docs-token" || src.Config["reader"] != "gitdocs" {
		t.Errorf("runbooks = %+v, %v; want it on secret docs-token", src, err)
	}

	expectOutput(t, "sources", g.mustGoal(t, cmdKnowledge, "sources"), []string{"synced (secret docs-token)"})
}
