package api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The routes the knowledge use tests call.
const (
	usagePath = "/v1/orgs/acme/knowledge/usage"
	runsPath  = "/v1/orgs/acme/runs/"
)

// usedHandbook is the harness with the handbook source and its two uploaded documents, leave and
// expenses, which the tests' runs use.
type usedHandbook struct {
	*harness

	source          gen.KnowledgeSource
	leave, expenses string
}

func newUsedHandbook(t *testing.T) usedHandbook {
	t.Helper()

	h := newHarness(t)
	src := h.createHandbook(t)
	up := decode[gen.KnowledgeDocumentList](t, h.upload(t, srcHandbook, [2]string{leaveFile, leaveMD}, [2]string{"expenses.txt", "Receipts within 30 days."}),
		http.StatusAccepted)

	return usedHandbook{harness: h, source: src, leave: up.Documents[0].Id, expenses: up.Documents[1].Id}
}

// use records run's use of document by kind, as the gateway does.
func (h usedHandbook) use(t *testing.T, it domain.Item, run domain.RunID, document string, kind domain.KnowledgeUseKind, chunks ...int64) {
	t.Helper()

	err := h.kb.RecordUse(inOrg(t), domain.KnowledgeUse{
		Factory: it.FactoryID, Item: it.ID, Run: run, Source: domain.KnowledgeSourceID(h.source.Id),
		Document: domain.KnowledgeDocumentID(document), Kind: kind, Chunks: chunks, At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// verdict puts body as the caller's verdict on document.
func (h usedHandbook) verdict(t *testing.T, document, body string) response {
	t.Helper()

	return h.request(t, http.MethodPut, documentsPath+"/"+document+"/verdict", body, nil)
}

// usageLine renders a source's usage as "title runs agents+/- people+/-" per document.
func usageLine(u gen.KnowledgeUsage) []string {
	out := []string{}
	for _, d := range u.Documents {
		out = append(out, fmt.Sprintf("%s/%s %d %d/%d %d/%d", d.Source, d.Title, d.Runs, d.AgentsHelped, d.AgentsMisled, d.PeopleHelped, d.PeopleMisled))
	}

	return out
}

// A member reads a source's usage per document, the most used first; the window is 1 to 90 days
// and the source must exist.
func TestKnowledgeUses_Usage(t *testing.T) {
	t.Parallel()

	h := newUsedHandbook(t)
	build := h.seedTitled(t, "Fix the backend build")
	first := h.orgRun(t, build, agBackend, t0, domain.RunResult{Status: domain.RunSucceeded})
	second := h.orgRun(t, build, agBackend, t0.Add(time.Minute), domain.RunResult{Status: domain.RunSucceeded})

	h.use(t, build, first, h.expenses, domain.KnowledgeSearched, 1)
	h.use(t, build, first, h.expenses, domain.KnowledgeRead)
	h.use(t, build, second, h.expenses, domain.KnowledgeRead)
	h.use(t, build, second, h.leave, domain.KnowledgeRead)

	if _, err := h.kb.GiveVerdict(inOrg(t), domain.KnowledgeVerdict{
		Document: domain.KnowledgeDocumentID(h.leave), Run: second, Item: build.ID, Verdict: domain.KnowledgeMisled,
		By: domain.Actor{Kind: domain.ActorAgent, ID: agBackend, Name: agBackend},
	}); err != nil {
		t.Fatal(err)
	}

	decode[gen.KnowledgeVerdict](t, h.verdict(t, h.leave, `{"verdict":"helped"}`), http.StatusOK)

	got := decode[gen.KnowledgeUsage](t, h.request(t, http.MethodGet, usagePath+"?source=handbook", "", nil), http.StatusOK)
	if want := []string{"handbook/expenses.txt 2 0/0 0/0", "handbook/" + leaveFile + " 1 0/1 1/0"}; !slices.Equal(usageLine(got), want) {
		t.Errorf("usage = %v; want %v: expenses by its two runs first", usageLine(got), want)
	}

	if got.Source != srcHandbook || got.Days != domain.DefaultUsageDays || got.Documents[0].Id != h.expenses {
		t.Errorf("usage = %s %d days, first %s; want handbook over 7 days, expenses first", got.Source, got.Days, got.Documents[0].Id)
	}

	month := decode[gen.KnowledgeUsage](t, h.request(t, http.MethodGet, usagePath+"?source=handbook&days=90", "", nil), http.StatusOK)
	if month.Days != domain.MaxUsageDays || len(month.Documents) != 2 {
		t.Errorf("usage over 90 days = %d days, %d documents; want 90 and 2", month.Days, len(month.Documents))
	}

	refused(t, "a window of 0 days", h.request(t, http.MethodGet, usagePath+"?source=handbook&days=0", "", nil), http.StatusBadRequest, "")
	refused(t, "a window of 91 days", h.request(t, http.MethodGet, usagePath+"?source=handbook&days=91", "", nil), http.StatusBadRequest, "")
	refused(t, "an unknown source", h.request(t, http.MethodGet, usagePath+"?source=wiki", "", nil), http.StatusNotFound, "knowledge-source-not-found")
}

// A document's uses name each run, its item and its factory, newest first, in pages; a run's
// knowledge lists its uses, by time, with the verdicts given for it.
func TestKnowledgeUses_DocumentAndRunUses(t *testing.T) {
	t.Parallel()

	h := newUsedHandbook(t)
	build := h.seedTitled(t, "Fix the backend build")
	guide := h.seedGuide(t, "Write the user guide")
	older := h.orgRun(t, build, agBackend, t0, domain.RunResult{Status: domain.RunSucceeded})
	newer := h.orgRun(t, guide, "writer", t0.Add(time.Minute), domain.RunResult{Status: domain.RunSucceeded})

	h.use(t, build, older, h.leave, domain.KnowledgeSearched, 3, 1)
	h.use(t, build, older, h.leave, domain.KnowledgeRead)
	h.use(t, guide, newer, h.leave, domain.KnowledgeRead)

	usesPath := documentsPath + "/" + h.leave + "/uses"

	page := decode[gen.KnowledgeDocumentUsePage](t, h.request(t, http.MethodGet, usesPath+"?limit=1", "", nil), http.StatusOK)
	if len(page.Uses) != 1 || page.NextAfter == nil {
		t.Fatalf("first page = %+v; want one use and a next page", page)
	}

	want := gen.KnowledgeDocumentUse{
		RunId: string(newer), Item: gen.RunItem{Id: string(guide.ID), Ref: "acme#1", Title: "Write the user guide"},
		Factory: gen.FactoryHit{Id: acmeID, Name: acmeSlug, Title: page.Uses[0].Factory.Title},
		Kinds:   []gen.KnowledgeUseKind{gen.Read}, At: page.Uses[0].At,
	}
	if got := asJSON(t, page.Uses[0]); got != asJSON(t, want) {
		t.Errorf("the newest use =\n  %s\nwant\n  %s", got, asJSON(t, want))
	}

	next := decode[gen.KnowledgeDocumentUsePage](t, h.request(t, http.MethodGet, usesPath+"?limit=1&after="+url.QueryEscape(*page.NextAfter), "", nil),
		http.StatusOK)
	if len(next.Uses) != 1 || next.NextAfter != nil {
		t.Fatalf("second page = %+v; want the last use", next)
	}

	if u := next.Uses[0]; u.RunId != string(older) || u.Item.Ref != "line#1" || u.Factory.Id != lineID || u.Factory.Name != lineSlug ||
		!slices.Equal(u.Kinds, []gen.KnowledgeUseKind{gen.Read, gen.Search}) {
		t.Errorf("the older use = %s; want line#1's run, read and searched", asJSON(t, u))
	}

	refused(t, "the uses of no document", h.request(t, http.MethodGet, documentsPath+"/nothing/uses", "", nil), http.StatusNotFound,
		"knowledge-document-not-found")
	refused(t, "a limit over 100", h.request(t, http.MethodGet, usesPath+"?limit=101", "", nil), http.StatusBadRequest, "")

	h.wantRunKnowledge(t, build, older, newer)
}

// wantRunKnowledge checks older's uses of leave and its one verdict, and newer's use without one.
func (h usedHandbook) wantRunKnowledge(t *testing.T, build domain.Item, older, newer domain.RunID) {
	t.Helper()

	decode[gen.KnowledgeVerdict](t, h.verdict(t, h.leave, `{"verdict":"misled","run_id":"`+string(older)+`","note":"old policy"}`), http.StatusOK)
	decode[gen.KnowledgeVerdict](t, h.verdict(t, h.leave, `{"verdict":"helped"}`), http.StatusOK) // of no run

	run := decode[gen.RunKnowledge](t, h.request(t, http.MethodGet, runsPath+string(older)+"/knowledge", "", nil), http.StatusOK)

	uses := []string{}
	for _, u := range run.Uses {
		uses = append(uses, fmt.Sprintf("%s/%s %s %v", u.Source, u.Document, u.Kind, u.Chunks))
	}

	if want := []string{"handbook/" + h.leave + " search [1 3]", "handbook/" + h.leave + " read []"}; !slices.Equal(uses, want) {
		t.Errorf("the run's uses = %v; want %v", uses, want)
	}

	if len(run.Verdicts) != 1 || run.Verdicts[0].Verdict != gen.KnowledgeVerdictVerdictMisled || valueOf(run.Verdicts[0].RunId) != string(older) ||
		valueOf(run.Verdicts[0].ItemId) != string(build.ID) || valueOf(run.Verdicts[0].Note) != "old policy" || run.Verdicts[0].Source != srcHandbook {
		t.Errorf("the run's verdicts = %s; want the one misled given for it", asJSON(t, run.Verdicts))
	}

	none := decode[gen.RunKnowledge](t, h.request(t, http.MethodGet, runsPath+string(newer)+"/knowledge", "", nil), http.StatusOK)
	if len(none.Uses) != 1 || len(none.Verdicts) != 0 {
		t.Errorf("the newer run's knowledge = %s; want its one use and no verdict", asJSON(t, none))
	}

	refused(t, "the knowledge of no run", h.request(t, http.MethodGet, runsPath+"01900000-0000-7000-8000-000000000009/knowledge", "", nil),
		http.StatusNotFound, "run-not-found")
	refused(t, "the knowledge of a run through another organization",
		h.request(t, http.MethodGet, "/v1/orgs/"+labsSlug+"/runs/"+string(older)+"/knowledge", "", nil), http.StatusNotFound, "run-not-found")
}

// A person's verdict is stored, replaced by their next one, audited without its note; it must name
// a known document and, with a run, one that used it.
func TestKnowledgeUses_Verdict(t *testing.T) {
	t.Parallel()

	h := newUsedHandbook(t)
	build := h.seedTitled(t, "Fix the backend build")
	used := h.orgRun(t, build, agBackend, t0, domain.RunResult{Status: domain.RunSucceeded})
	other := h.orgRun(t, build, agBackend, t0.Add(time.Minute), domain.RunResult{Status: domain.RunSucceeded})

	h.use(t, build, used, h.leave, domain.KnowledgeRead)

	first := decode[gen.KnowledgeVerdict](t, h.verdict(t, h.leave, `{"verdict":"helped","run_id":"`+string(used)+`","note":"clear steps"}`), http.StatusOK)
	wantVerdict(t, "the first verdict", first, verdictShape{h.leave, srcHandbook, "helped", string(used), string(build.ID), "clear steps", gen.ActorKindUser})

	second := decode[gen.KnowledgeVerdict](t, h.verdict(t, h.leave, `{"verdict":"misled","run_id":"`+string(used)+`"}`), http.StatusOK)
	wantVerdict(t, "the second verdict", second, verdictShape{h.leave, srcHandbook, "misled", string(used), string(build.ID), "", gen.ActorKindUser})

	run := decode[gen.RunKnowledge](t, h.request(t, http.MethodGet, runsPath+string(used)+"/knowledge", "", nil), http.StatusOK)
	if len(run.Verdicts) != 1 || run.Verdicts[0].Verdict != gen.KnowledgeVerdictVerdictMisled {
		t.Errorf("the run's verdicts = %s; want the second only, which replaced the first", asJSON(t, run.Verdicts))
	}

	audit := decode[gen.AuditPage](t, h.request(t, http.MethodGet, "/v1/orgs/acme/audit?target_kind=knowledge_document", "", nil), http.StatusOK)
	if got := actions(audit.Entries); len(got) < 2 || got[0] != domain.AuditKnowledgeVerdictGiven || got[1] != domain.AuditKnowledgeVerdictGiven {
		t.Errorf("audit = %v; want knowledge_verdict.given twice, newest first", got)
	}

	if e := audit.Entries[1]; e.Data["verdict"] != "helped" || e.Data["run"] != string(used) || strings.Contains(asJSON(t, e), "clear steps") {
		t.Errorf("the first verdict's entry = %s; want its kind and run, never its note", asJSON(t, e))
	}

	const noDoc, noRun = "01900000-0000-7000-8000-000000000009", "01900000-0000-7000-8000-000000000009"

	for _, c := range []struct {
		what, document, body string
		status               int
		code                 string
	}{
		{"a verdict on no document", noDoc, `{"verdict":"helped"}`, http.StatusNotFound, "knowledge-document-not-found"},
		{
			"a verdict for a run with no use of the document", h.leave, `{"verdict":"helped","run_id":"` + string(other) + `"}`,
			http.StatusUnprocessableEntity, "knowledge-document-not-used",
		},
		{
			"a verdict on no document for a run", noDoc, `{"verdict":"helped","run_id":"` + string(used) + `"}`, http.StatusNotFound,
			"knowledge-document-not-found",
		},
		{"a verdict for no run", h.leave, `{"verdict":"helped","run_id":"` + noRun + `"}`, http.StatusNotFound, "run-not-found"},
		{"a note over 500 characters", h.leave, `{"verdict":"helped","note":"` + strings.Repeat("x", 501) + `"}`, http.StatusBadRequest, ""},
		{"an unknown verdict", h.leave, `{"verdict":"meh"}`, http.StatusBadRequest, ""},
	} {
		refused(t, c.what, h.verdict(t, c.document, c.body), c.status, c.code)
	}
}

// verdictShape is what the tests check of a verdict.
type verdictShape struct {
	Document, Source, Verdict, Run, Item, Note string
	By                                         gen.ActorKind
}

func wantVerdict(t *testing.T, what string, v gen.KnowledgeVerdict, want verdictShape) {
	t.Helper()

	got := verdictShape{v.Document, v.Source, string(v.Verdict), valueOf(v.RunId), valueOf(v.ItemId), valueOf(v.Note), v.By.Kind}
	if got != want || v.At.IsZero() {
		t.Errorf("%s = %+v at %v; want %+v at a time", what, got, v.At, want)
	}
}

// A key without knowledge:read or knowledge:verdict reads no usage and gives no verdict.
func TestKnowledgeUses_Refused(t *testing.T) {
	t.Parallel()

	h := keyed(t)
	created, _ := h.create(t, ciDeploy) // org:read, factory:read and item:create

	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, usagePath + "?source=handbook", ""},
		{http.MethodGet, documentsPath + "/d/uses", ""},
		{http.MethodPut, documentsPath + "/d/verdict", `{"verdict":"helped"}`},
	} {
		refused(t, "the key's "+c.method+" "+c.path, h.request(t, c.method, c.path, c.body, bearer(created.Secret)), http.StatusForbidden, "forbidden")
	}
}
