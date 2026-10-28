package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The command and the source the knowledge tests use.
const (
	cmdKnowledge = "knowledge"
	srcHandbook  = "handbook"
	kindUpload   = "upload"
	subUsage     = "usage"
	fileLeave    = "leave.md"
	runOne       = "run-1"
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

	paths := files(t, map[string]string{fileLeave: "# Leave\n\nAsk your lead two weeks ahead.", "expenses.txt": "Receipts within 30 days."})
	expectOutput(t, kindUpload, g.mustGoal(t, append([]string{cmdKnowledge, kindUpload, srcHandbook}, paths...)...),
		[]string{fileLeave, "expenses.txt", "pending"})
	expectOutput(t, "sync", g.mustGoal(t, cmdKnowledge, "sync", srcHandbook), []string{"handbook is due"})

	g.sync(t)

	expectOutput(t, "sources", g.mustGoal(t, cmdKnowledge, "sources"), []string{srcHandbook, kindUpload, "2 documents"})

	found := g.mustGoal(t, cmdKnowledge, "search", "receipts", "--source", srcHandbook)
	expectOutput(t, "search", found, []string{"expenses.txt"}, fileLeave)

	id := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f-]{27}`).FindString(found)
	if id == "" {
		t.Fatalf("search = %q; want the document's id", found)
	}

	expectOutput(t, "rm", g.mustGoal(t, cmdKnowledge, "rm", id), []string{"removed"})
	expectOutput(t, "documents after rm", g.mustGoal(t, cmdKnowledge, "documents", srcHandbook), []string{fileLeave}, "expenses.txt")
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

	for _, args := range [][]string{
		{"add", srcHandbook},
		{kindUpload, srcHandbook},
		{"search"},
		{"rm"},
		{"teach"},
		{"uses"},
		{"uses", "a", "b"},
		{"verdict"},
		{"verdict", "d"},
		{subUsage, argExtra},
		{subUsage, "--days", "-1"},
	} {
		if _, err := g.goal(t, append([]string{cmdKnowledge}, args...)...); !errors.Is(err, errUsage) {
			t.Errorf("goal knowledge %s = %v; want the usage", strings.Join(args, " "), err)
		}
	}
}

// documentID is the one document id in out.
func documentID(t *testing.T, out string) string {
	t.Helper()

	id := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f-]{27}`).FindString(out)
	if id == "" {
		t.Fatalf("output = %q; want a document id", out)
	}

	return id
}

// A source's usage is one row per document: its runs, and its verdicts as agents plus people with
// the people's share; without --source every source's, and --json goald's answer.
func TestKnowledge_UsageTable(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.mustGoal(t, cmdKnowledge, "add", srcHandbook, "--kind", kindUpload)
	g.mustGoal(t, append([]string{cmdKnowledge, kindUpload, srcHandbook}, files(t, map[string]string{fileLeave: "# Leave\n\nAsk two weeks ahead."})...)...)
	g.sync(t)

	expectOutput(t, "usage before any use", g.mustGoal(t, cmdKnowledge, subUsage, "--source", srcHandbook),
		[]string{"no run used a document of handbook in the last 7 days"})

	id := documentID(t, g.mustGoal(t, cmdKnowledge, "documents", srcHandbook))

	doc, _, err := g.kb.Document(g.ctx(t), domain.KnowledgeDocumentID(id))
	if err != nil {
		t.Fatal(err)
	}

	for _, run := range []domain.RunID{runOne, "run-2"} {
		if err := g.kb.RecordUse(g.ctx(t), domain.KnowledgeUse{
			Factory: "factory-1", Item: "item-1", Run: run, Source: doc.Source,
			Document: doc.ID, Kind: domain.KnowledgeRead, At: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := g.kb.GiveVerdict(g.ctx(t), domain.KnowledgeVerdict{
		Document: doc.ID, Run: runOne, Item: "item-1", Verdict: domain.KnowledgeHelped,
		By: domain.Actor{Kind: domain.ActorAgent, ID: "writer", Name: "writer"},
	}); err != nil {
		t.Fatal(err)
	}

	expectOutput(t, "verdict", g.mustGoal(t, cmdKnowledge, "verdict", id, "helped"), []string{"✓ helped: knowledge document " + id + " of handbook"})

	row := regexp.MustCompile(id + `\s+leave\.md\s+handbook\s+2\s+2 \(1 people\)\s+0\n`)

	for _, args := range [][]string{{subUsage, "--source", srcHandbook, "--days", "30"}, {subUsage}} {
		out := g.mustGoal(t, append([]string{cmdKnowledge}, args...)...)
		expectOutput(t, strings.Join(args, " "), out, []string{"DOCUMENT", "TITLE", "SOURCE", "RUNS", "HELPED", "MISLED"})

		if !row.MatchString(out) {
			t.Errorf("goal knowledge %s = %q; want the row %s", strings.Join(args, " "), out, row)
		}
	}

	var usage gen.KnowledgeUsage
	if err := json.Unmarshal([]byte(g.mustGoal(t, cmdKnowledge, subUsage, "--source", srcHandbook, "--json")), &usage); err != nil {
		t.Fatal(err)
	}

	if len(usage.Documents) != 1 || usage.Documents[0].AgentsHelped != 1 || usage.Documents[0].PeopleHelped != 1 {
		t.Errorf("usage --json = %+v; want one document helped by one agent and one person", usage)
	}

	if _, err := g.goal(t, cmdKnowledge, subUsage, "--source", "wiki"); err == nil {
		t.Error("usage of an unknown source succeeded")
	}
}

// usesGoald answers a document's uses in two pages, the second after run-2.
func usesGoald(t *testing.T) *defGoald {
	t.Helper()

	at := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	use := func(run, ref string, kinds ...gen.KnowledgeUseKind) gen.KnowledgeDocumentUse {
		return gen.KnowledgeDocumentUse{
			RunId: run, Kinds: kinds, At: at, Factory: gen.FactoryHit{Id: "f1", Name: "writing", Title: "Writing"},
			Item: gen.RunItem{Id: "i-" + run, Ref: ref, Title: "Fix the build"},
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/knowledge/documents/doc-1/uses", func(w http.ResponseWriter, r *http.Request) {
		page := gen.KnowledgeDocumentUsePage{Uses: []gen.KnowledgeDocumentUse{use("run-3", "writing#3", "read", "search"), use("run-2", "writing#2", "read")}, NextAfter: new("run-2")}
		if r.URL.Query().Get("after") == "run-2" {
			page = gen.KnowledgeDocumentUsePage{Uses: []gen.KnowledgeDocumentUse{use(runOne, "writing#1", "search")}}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(page)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &defGoald{srv: srv}
}

// A document's uses are every page's runs in goald's order, newest first, with their item and
// factory; --json is the pages' uses in one object.
func TestKnowledge_Uses(t *testing.T) {
	t.Parallel()

	g := usesGoald(t)

	out := g.mustGoal(t, cmdKnowledge, "uses", "doc-1")
	lines := strings.Split(strings.TrimSpace(out), "\n")

	if len(lines) != 4 || !strings.HasPrefix(lines[0], "RUN") {
		t.Fatalf("uses = %q; want the header and three runs", out)
	}

	for i, want := range []string{`run-3\s+writing#3\s+Fix the build\s+writing\s+read,search\s+`, `run-2\s+writing#2`, `run-1\s+writing#1\s+.*search`} {
		if !regexp.MustCompile(want).MatchString(lines[i+1]) {
			t.Errorf("uses row %d = %q; want %s", i+1, lines[i+1], want)
		}
	}

	var page gen.KnowledgeDocumentUsePage
	if err := json.Unmarshal([]byte(g.mustGoal(t, cmdKnowledge, "uses", "doc-1", "--json")), &page); err != nil {
		t.Fatal(err)
	}

	if len(page.Uses) != 3 || page.Uses[2].RunId != runOne || page.NextAfter != nil {
		t.Errorf("uses --json = %+v; want the three runs of both pages", page)
	}
}

// verdictGoald answers PUT …/verdict with the verdict it was sent, or with goald's refusal for a run
// that never used the document, and records the bodies.
func verdictGoald(t *testing.T) (*defGoald, func() []string) {
	t.Helper()

	var (
		mu     sync.Mutex
		bodies []string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/orgs/acme/knowledge/documents/doc-1/verdict", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		mu.Lock()

		bodies = append(bodies, string(raw))
		mu.Unlock()

		var in gen.KnowledgeVerdictInput

		_ = json.Unmarshal(raw, &in)

		if deref(in.RunId) == "run-unused" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(gen.Problem{Title: "The run did not use this document", Status: http.StatusConflict, Code: new("knowledge-document-not-used")})

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gen.KnowledgeVerdict{
			Document: "doc-1", Source: srcHandbook, Verdict: gen.KnowledgeVerdictVerdict(in.Verdict), RunId: in.RunId, Note: in.Note,
			By: gen.Actor{Kind: "user", Id: "local", Name: "Local user"},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &defGoald{srv: srv}, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return bodies
	}
}

// A verdict sends {verdict, run_id, note}, leaving out what was not given, and goald's refusal
// reaches the user.
func TestKnowledge_VerdictBody(t *testing.T) {
	t.Parallel()

	g, bodies := verdictGoald(t)

	expectOutput(t, "verdict", g.mustGoal(t, cmdKnowledge, "verdict", "doc-1", "misled"), []string{"✓ misled: knowledge document doc-1 of handbook"}, "run")
	expectOutput(t, "verdict --run --note", g.mustGoal(t, cmdKnowledge, "verdict", "doc-1", "helped", "--run", runOne, "--note", "had the answer"),
		[]string{"✓ helped", "run run-1"})

	if out := g.mustGoal(t, cmdKnowledge, "verdict", "doc-1", "helped", "--json"); !strings.Contains(out, `"verdict":"helped"`) {
		t.Errorf("verdict --json = %q; want goald's answer", out)
	}

	_, err := g.goal(t, cmdKnowledge, "verdict", "doc-1", "helped", "--run", "run-unused")
	if err == nil || !strings.Contains(err.Error(), "did not use this document") {
		t.Errorf("verdict of a run that never used it = %v; want goald's refusal", err)
	}

	want := []string{
		`{"verdict":"misled"}`,
		`{"note":"had the answer","run_id":"run-1","verdict":"helped"}`,
		`{"verdict":"helped"}`,
		`{"run_id":"run-unused","verdict":"helped"}`,
	}

	got := bodies()
	if len(got) != len(want) {
		t.Fatalf("bodies = %q; want %q", got, want)
	}

	for i := range want {
		if strings.TrimSpace(got[i]) != want[i] {
			t.Errorf("body %d = %s; want %s", i, got[i], want[i])
		}
	}
}

// A verdict other than helped or misled is a usage error, and no request is made.
func TestKnowledge_VerdictRefusesValue(t *testing.T) {
	t.Parallel()

	g, bodies := verdictGoald(t)

	for _, v := range []string{"useful", "Helped", ""} {
		if _, err := g.goal(t, cmdKnowledge, "verdict", "doc-1", v); !errors.Is(err, errUsage) {
			t.Errorf("goal knowledge verdict doc-1 %q = %v; want the usage", v, err)
		}
	}

	if got := bodies(); len(got) != 0 {
		t.Errorf("requests = %q; want none", got)
	}
}
