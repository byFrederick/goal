package api_test

import (
	"bytes"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/knowledge"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// The routes the knowledge tests call.
const (
	sourcesPath   = "/v1/orgs/acme/knowledge/sources"
	handbookPath  = sourcesPath + "/handbook"
	documentsPath = "/v1/orgs/acme/knowledge/documents"
	searchPath    = "/v1/orgs/acme/knowledge/search"
)

// testUploadMax is the harness's GOAL_UPLOAD_MAX_BYTES.
const testUploadMax = 64 << 10

// The source and the file the tests upload.
const (
	srcHandbook = "handbook"
	leaveFile   = "leave.md"
	leaveMD     = "# Leave\n\nAsk your lead two weeks ahead."
)

func (h *harness) createHandbook(t *testing.T) gen.KnowledgeSource {
	t.Helper()

	return decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath,
		`{"name":"handbook","title":"Handbook","kind":"upload"}`, nil), http.StatusCreated)
}

// upload posts files (name → content) to source's documents as multipart/form-data.
func (h *harness) upload(t *testing.T, source string, files ...[2]string) response {
	t.Helper()

	var buf bytes.Buffer

	mw := multipart.NewWriter(&buf)

	for _, f := range files {
		part, err := mw.CreateFormFile("files", f[0])
		if err != nil {
			t.Fatal(err)
		}

		if _, err := part.Write([]byte(f[1])); err != nil {
			t.Fatal(err)
		}
	}

	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	return h.request(t, http.MethodPost, sourcesPath+"/"+source+"/documents", buf.String(),
		http.Header{"Content-Type": {mw.FormDataContentType()}})
}

// syncAll runs one ingestion pass over the organizations' due sources, as goald's loop does: on a
// clock of its own, since the knowledge fake dates sources by the time of day.
func (h *harness) syncAll(t *testing.T) {
	t.Helper()

	in := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: h.kb, Orgs: h.tenancy, Connections: h.conns, Uploads: h.uploads, Clock: porttest.NewClock(time.Now().Add(time.Second)),
	})

	if _, err := in.SyncDue(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// sourceShape is what the tests check of a source; Due says it has a next sync.
type sourceShape struct {
	Name, Title string
	Kind        gen.KnowledgeSourceKind
	Status      gen.KnowledgeSourceStatus
	Every       int
	Due         bool
}

func shapeOf(s gen.KnowledgeSource) sourceShape {
	return sourceShape{Name: s.Name, Title: s.Title, Kind: s.Kind, Status: s.Status, Every: s.SyncEverySeconds, Due: s.NextSyncAt != nil}
}

// docShapes are what the tests check of documents: source/title status.
func docShapes(docs []gen.KnowledgeDocument) []string {
	out := []string{}
	for _, d := range docs {
		out = append(out, d.Source+"/"+d.Title+" "+string(d.Status))
	}

	return out
}

// An admin adds an upload source, which members list and read; its title and schedule change, and
// a sync makes it due. Every change is audited.
func TestKnowledge_SourcesCreateChangeAndSync(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	created := h.createHandbook(t)
	if got, want := shapeOf(created), (sourceShape{srcHandbook, "Handbook", gen.KnowledgeSourceKindUpload, gen.KnowledgeSourceStatusIdle, 3600, true}); got != want {
		t.Errorf("created = %+v; want %+v: an idle upload source syncing hourly, due now", got, want)
	}

	changed := decode[gen.KnowledgeSource](t, h.request(t, http.MethodPatch, handbookPath,
		`{"title":"Acme handbook","sync_every_seconds":7200}`, nil), http.StatusOK)
	if got, want := shapeOf(changed), (sourceShape{srcHandbook, "Acme handbook", gen.KnowledgeSourceKindUpload, gen.KnowledgeSourceStatusIdle, 7200, true}); got != want {
		t.Errorf("patched = %+v; want %+v", got, want)
	}

	list := decode[gen.KnowledgeSourceList](t, h.request(t, http.MethodGet, sourcesPath, "", nil), http.StatusOK)
	if len(list.Sources) != 1 || list.Sources[0].Id != created.Id {
		t.Errorf("list = %+v; want handbook", list.Sources)
	}

	if r := h.request(t, http.MethodPost, handbookPath+"/sync", "", nil); r.status != http.StatusAccepted {
		t.Errorf("sync = %d %s; want 202", r.status, r.body)
	}

	audit := decode[gen.AuditPage](t, h.request(t, http.MethodGet, "/v1/orgs/acme/audit?target_kind=knowledge_source", "", nil), http.StatusOK)
	if want := []string{domain.AuditKnowledgeSourceUpdated, domain.AuditKnowledgeSourceCreated}; !slices.Equal(actions(audit.Entries), want) {
		t.Errorf("audit = %v; want %v", actions(audit.Entries), want)
	}

	if r := h.request(t, http.MethodDelete, handbookPath, "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s; want 204", r.status, r.body)
	}

	refused(t, "get after delete", h.request(t, http.MethodGet, handbookPath, "", nil), http.StatusNotFound, "knowledge-source-not-found")
}

// Requests the source rules refuse answer their codes; a source reading a connection is listed
// in the connection's used_by.
func TestKnowledge_SourceRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createHandbook(t)
	h.createGitHub(t)
	decode[gen.Connection](t, h.request(t, http.MethodPost, connectionsPath, `{"name":"claude","kind":"anthropic","secrets":{"key":"k"}}`, nil),
		http.StatusCreated)

	for _, tc := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"name taken", `{"name":"handbook","title":"Again","kind":"upload"}`, http.StatusConflict, "knowledge-source-name-taken"},
		{"upload with a connection", `{"name":"notes","title":"Notes","kind":"upload","connection":"github"}`, http.StatusBadRequest, ""},
		{"synced on a kind that cannot sync", `{"name":"models","title":"Models","kind":"synced","connection":"claude"}`, http.StatusBadRequest, "connection-cannot-sync"},
		{"unknown connection", `{"name":"wiki","title":"Wiki","kind":"synced","connection":"wiki"}`, http.StatusNotFound, "connection-not-found"},
		{"schedule too short", `{"name":"often","title":"Often","kind":"upload","sync_every_seconds":60}`, http.StatusBadRequest, ""},
	} {
		refused(t, tc.name, h.request(t, http.MethodPost, sourcesPath, tc.body, nil), tc.status, tc.code)
	}

	decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath,
		`{"name":"repo-search","title":"Repository search","kind":"live","connection":"github"}`, nil), http.StatusCreated)

	refused(t, "sync of a live source", h.request(t, http.MethodPost, sourcesPath+"/repo-search/sync", "", nil), http.StatusConflict, "knowledge-source-live")
	refused(t, "patch of no source", h.request(t, http.MethodPatch, sourcesPath+"/nothing", `{"title":"x"}`, nil), http.StatusNotFound, "knowledge-source-not-found")

	conn := decode[gen.Connection](t, h.request(t, http.MethodGet, githubPath, "", nil), http.StatusOK)
	if !slices.Equal(conn.UsedBy.KnowledgeSources, []string{"repo-search"}) {
		t.Errorf("github's used_by = %+v; want the live source that reads it", conn.UsedBy)
	}

	refused(t, "delete of a connection a source reads", h.request(t, http.MethodDelete, githubPath, "", nil), http.StatusConflict, "connection-in-use")
}

// Files uploaded to an upload source are pending until a sync indexes them; then they are read,
// searched as agents search them, and deleted.
func TestKnowledge_UploadReadSearchAndDelete(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createHandbook(t)

	up := decode[gen.KnowledgeDocumentList](t, h.upload(t, srcHandbook, [2]string{leaveFile, leaveMD}, [2]string{"expenses.txt", "Receipts within 30 days."}),
		http.StatusAccepted)
	if got, want := docShapes(up.Documents), []string{"handbook/leave.md pending", "handbook/expenses.txt pending"}; !slices.Equal(got, want) {
		t.Fatalf("uploaded = %v; want %v", got, want)
	}

	h.syncAll(t)

	page := decode[gen.KnowledgeDocumentPage](t, h.request(t, http.MethodGet, handbookPath+"/documents?limit=1", "", nil), http.StatusOK)
	if len(page.Documents) != 1 || page.NextAfter == nil {
		t.Fatalf("first page = %+v; want one document and a next page", page)
	}

	rest := decode[gen.KnowledgeDocumentPage](t, h.request(t, http.MethodGet, handbookPath+"/documents?after="+*page.NextAfter, "", nil), http.StatusOK)
	if got := docShapes(append(page.Documents, rest.Documents...)); rest.NextAfter != nil || len(got) != 2 || !slices.Contains(got, "handbook/leave.md indexed") {
		t.Errorf("pages = %v, next %v; want both documents indexed, then no next page", got, rest.NextAfter)
	}

	leave := up.Documents[0].Id

	got := decode[gen.KnowledgeDocumentText](t, h.request(t, http.MethodGet, documentsPath+"/"+leave, "", nil), http.StatusOK)
	if got.Text != leaveMD || got.Document.Source != srcHandbook {
		t.Errorf("document = %+v; want leave.md's text", got)
	}

	hits := decode[gen.KnowledgeSearchResult](t, h.request(t, http.MethodGet, searchPath+"?q=receipts", "", nil), http.StatusOK)
	if len(hits.Hits) == 0 || hits.Hits[0].Source+"/"+hits.Hits[0].Title != "handbook/expenses.txt" || hits.Hits[0].Document == "" {
		t.Errorf("search = %+v; want expenses.txt first", hits.Hits)
	}

	if r := h.request(t, http.MethodDelete, documentsPath+"/"+leave, "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s; want 204", r.status, r.body)
	}

	refused(t, "get after delete", h.request(t, http.MethodGet, documentsPath+"/"+leave, "", nil), http.StatusNotFound, "knowledge-document-not-found")
}

// Threat row 11: an uploaded original is served as an attachment browsers never render, whatever
// it holds.
func TestAPI_DocumentContentIsAttachment(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createHandbook(t)

	page := `<html><script>alert(document.cookie)</script></html>`
	up := decode[gen.KnowledgeDocumentList](t, h.upload(t, srcHandbook, [2]string{"page.html", page}), http.StatusAccepted)

	r := h.request(t, http.MethodGet, documentsPath+"/"+up.Documents[0].Id+"/content", "", nil)
	if r.status != http.StatusOK || string(r.body) != page {
		t.Fatalf("content = %d %q; want the file as uploaded", r.status, r.body)
	}

	if d, params, err := mime.ParseMediaType(r.header.Get("Content-Disposition")); err != nil || d != "attachment" || params["filename"] != "page.html" {
		t.Errorf("Content-Disposition = %q; want an attachment named page.html", r.header.Get("Content-Disposition"))
	}

	if r.header.Get("X-Content-Type-Options") != "nosniff" || r.header.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("headers = %v; want nosniff and application/octet-stream", r.header)
	}
}

// Uploads the knowledge base refuses answer their codes, and a refused file stores none of the
// request's files.
func TestKnowledge_UploadRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createHandbook(t)

	refused(t, "a type not indexed", h.upload(t, srcHandbook, [2]string{leaveFile, leaveMD}, [2]string{"deck.pptx", "PK"}),
		http.StatusUnsupportedMediaType, "unsupported-document-type")
	refused(t, "a file over the limit", h.upload(t, srcHandbook, [2]string{"big.md", strings.Repeat("a", testUploadMax+1)}),
		http.StatusRequestEntityTooLarge, "upload-too-large")
	refused(t, "a request over the limit", h.upload(t, srcHandbook, [2]string{"huge.md", strings.Repeat("a", 2<<20)}),
		http.StatusRequestEntityTooLarge, "upload-too-large")
	refused(t, "no files", h.upload(t, srcHandbook), http.StatusBadRequest, "")
	refused(t, "no source", h.upload(t, "nothing", [2]string{leaveFile, leaveMD}), http.StatusNotFound, "knowledge-source-not-found")

	page := decode[gen.KnowledgeDocumentPage](t, h.request(t, http.MethodGet, handbookPath+"/documents", "", nil), http.StatusOK)
	if len(page.Documents) != 0 {
		t.Errorf("documents after the refusals = %+v; want none", page.Documents)
	}

	if objects, err := h.kbObjects.List(t.Context(), ""); err != nil || len(objects) != 0 {
		t.Errorf("the bucket holds %v (%v) after the refusals; want nothing", objects, err)
	}

	h.request(t, http.MethodPost, connectionsPath, `{"name":"wiki","kind":"confluence","fields":{"site":"acme.atlassian.net","email":"ada@example.com"},`+
		`"secrets":{"token":"t"}}`, nil)
	decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath,
		`{"name":"wiki","title":"Wiki","kind":"synced","connection":"wiki","config":{"space":"ENG"}}`, nil), http.StatusCreated)

	refused(t, "an upload to a synced source", h.upload(t, "wiki", [2]string{leaveFile, leaveMD}), http.StatusConflict, "not-an-upload-source")
}

// A search answers what the gateway's knowledge__search does, over every source or the named
// ones; it refuses an unknown source and a malformed query.
func TestKnowledge_SearchRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createHandbook(t)
	h.upload(t, srcHandbook, [2]string{leaveFile, leaveMD})
	h.syncAll(t)

	q := url.Values{"q": {"lead"}, "source": {srcHandbook}}
	if hits := decode[gen.KnowledgeSearchResult](t, h.request(t, http.MethodGet, searchPath+"?"+q.Encode(), "", nil), http.StatusOK); len(hits.Hits) != 1 {
		t.Errorf("search of handbook = %+v; want leave.md", hits.Hits)
	}

	refused(t, "an unknown source", h.request(t, http.MethodGet, searchPath+"?q=lead&source=wiki", "", nil), http.StatusNotFound, "knowledge-source-not-found")
	refused(t, "an empty query", h.request(t, http.MethodGet, searchPath+"?q=+", "", nil), http.StatusBadRequest, "")
	refused(t, "a limit over 20", h.request(t, http.MethodGet, searchPath+"?q=lead&limit=21", "", nil), http.StatusBadRequest, "")
}

// The stream relays a knowledge event under its name.
func TestAPI_StreamSendsKnowledgeEvents(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, _, events := h.stream(t, "")
	events.next(t) // retry

	data := `{"source_id":"0b6f1c9e-0000-7000-8000-000000000001","name":"handbook","status":"syncing"}`
	h.notices.Publish(ports.ItemNotice{OrgID: testOrg, Org: &ports.OrgEvent{Name: "knowledge", Data: json.RawMessage(data)}})

	got := events.next(t)
	if len(got) != 2 || got[0] != "event: knowledge" || got[1] != "data: "+data {
		t.Fatalf("event %q; want the knowledge event", got)
	}
}
