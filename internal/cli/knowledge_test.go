package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The command and the source the knowledge tests use.
const (
	cmdKnowledge = "knowledge"
	srcHandbook  = "handbook"
	kindUpload   = "upload"
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

// An admin adds an upload source, uploads files to it, and once a sync has indexed them searches
// them as agents do and removes one.
func TestKnowledge_AddUploadSearchAndRemove(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	expectOutput(t, "add", g.mustGoal(t, cmdKnowledge, "add", srcHandbook, "--kind", kindUpload, "--title", "Acme handbook"),
		[]string{"added knowledge source handbook"})

	paths := files(t, map[string]string{"leave.md": "# Leave\n\nAsk your lead two weeks ahead.", "expenses.txt": "Receipts within 30 days."})
	expectOutput(t, kindUpload, g.mustGoal(t, append([]string{cmdKnowledge, kindUpload, srcHandbook}, paths...)...),
		[]string{"leave.md", "expenses.txt", "pending"})
	expectOutput(t, "sync", g.mustGoal(t, cmdKnowledge, "sync", srcHandbook), []string{"handbook is due"})

	g.sync(t)

	expectOutput(t, "sources", g.mustGoal(t, cmdKnowledge, "sources"), []string{srcHandbook, kindUpload, "2 documents"})

	found := g.mustGoal(t, cmdKnowledge, "search", "receipts", "--source", srcHandbook)
	expectOutput(t, "search", found, []string{"expenses.txt"}, "leave.md")

	id := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f-]{27}`).FindString(found)
	if id == "" {
		t.Fatalf("search = %q; want the document's id", found)
	}

	expectOutput(t, "rm", g.mustGoal(t, cmdKnowledge, "rm", id), []string{"removed"})
	expectOutput(t, "documents after rm", g.mustGoal(t, cmdKnowledge, "documents", srcHandbook), []string{"leave.md"}, "expenses.txt")
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
		[]string{"one.md", "two.md", "pending"})
}

// What goald refuses reaches the user; a command line that is not one is a usage error.
func TestKnowledge_Refusals(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goal(t, cmdKnowledge, "add", srcHandbook, "--kind", "synced"); err == nil || !strings.Contains(err.Error(), "connection") {
		t.Errorf("add synced without a connection = %v; want goald's refusal", err)
	}

	if _, err := g.goal(t, cmdKnowledge, kindUpload, "nothing", filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Error("upload of a missing file succeeded")
	}

	for _, args := range [][]string{{"add", srcHandbook}, {kindUpload, srcHandbook}, {"search"}, {"rm"}, {"teach"}} {
		if _, err := g.goal(t, append([]string{cmdKnowledge}, args...)...); !errors.Is(err, errUsage) {
			t.Errorf("goal knowledge %s = %v; want the usage", strings.Join(args, " "), err)
		}
	}
}
