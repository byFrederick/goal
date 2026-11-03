package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/knowledge"
)

// The routes the knowledge tests call.
const (
	sourcesPath  = "/v1/orgs/acme/knowledge/sources"
	handbookPath = sourcesPath + "/handbook"
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

// upload posts files (name → content) to source's uploads as multipart/form-data.
func (h *harness) upload(t *testing.T, source string, files ...[2]string) response {
	t.Helper()

	return h.uploadTo(t, sourcesPath+"/"+source+"/uploads", files...)
}

// uploadTo posts files (path → content) to path as multipart/form-data, each file name as given,
// folders and all.
func (h *harness) uploadTo(t *testing.T, path string, files ...[2]string) response {
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

	return h.request(t, http.MethodPost, path, buf.String(), http.Header{contentType: {mw.FormDataContentType()}})
}

// syncAll runs one ingestion pass over the organizations' due sources, as goald's loop does,
// writing snapshots into the harness's objects: on a clock of its own, since the knowledge fake
// dates sources by the time of day.
func (h *harness) syncAll(t *testing.T) {
	t.Helper()

	in := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: h.kb, Connections: h.conns, Uploads: h.uploads, Clock: porttest.NewClock(time.Now().Add(time.Second)),
		Objects: h.kbObjects, TempDir: t.TempDir(),
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

	h.syncedOnASecret(t)
}

// syncedOnASecret creates, reads and patches a synced source that reads with a secret.
func (h *harness) syncedOnASecret(t *testing.T) {
	t.Helper()

	h.createRepoSecret(t)
	decode[gen.Secret](t, h.request(t, http.MethodPost, secretsPath, `{"name":"docs-token","value":"test-token","scope":{"all_factories":true}}`, nil),
		http.StatusCreated)

	synced := decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath, `{"name":"runbooks","title":"Runbooks","kind":"synced",
"secret":"`+repoSecret+`","config":{"reader":"gitdocs","repository":"https://github.com/acme/docs"}}`, nil), http.StatusCreated)
	if deref(synced.Secret) != repoSecret || synced.Connection != nil || synced.Config["reader"] != "gitdocs" {
		t.Errorf("created synced = %s; want it on secret %s with its reader", body(t, synced), repoSecret)
	}

	got := decode[gen.KnowledgeSource](t, h.request(t, http.MethodGet, sourcesPath+"/runbooks", "", nil), http.StatusOK)
	if deref(got.Secret) != repoSecret {
		t.Errorf("read synced = %s; want secret %s", body(t, got), repoSecret)
	}

	patched := decode[gen.KnowledgeSource](t, h.request(t, http.MethodPatch, sourcesPath+"/runbooks", `{"secret":"docs-token"}`, nil), http.StatusOK)
	if deref(patched.Secret) != "docs-token" || patched.Config["reader"] != "gitdocs" {
		t.Errorf("patched synced = %s; want secret docs-token, its config kept", body(t, patched))
	}
}

// Requests the source rules refuse answer their codes; a source reading a connection is listed
// in the connection's used_by.
func TestKnowledge_SourceRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createHandbook(t)
	h.createGitHub(t)
	h.createRepoSecret(t)

	for _, tc := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"name taken", `{"name":"handbook","title":"Again","kind":"upload"}`, http.StatusConflict, "knowledge-source-name-taken"},
		{"upload with a connection", `{"name":"notes","title":"Notes","kind":"upload","connection":"github"}`, http.StatusBadRequest, ""},
		{"synced on a connection", `{"name":"models","title":"Models","kind":"synced","connection":"github","config":{"reader":"gitdocs"}}`, http.StatusBadRequest, ""},
		{"synced on no secret", `{"name":"models","title":"Models","kind":"synced"}`, http.StatusBadRequest, ""},
		{"synced without a reader", `{"name":"models","title":"Models","kind":"synced","secret":"` + repoSecret + `"}`, http.StatusBadRequest, ""},
		{"unknown secret", `{"name":"wiki","title":"Wiki","kind":"synced","secret":"wiki","config":{"reader":"confluence"}}`, http.StatusNotFound, "secret-not-found"},
		{"live with a secret", `{"name":"wiki","title":"Wiki","kind":"live","connection":"github","secret":"` + repoSecret + `"}`, http.StatusBadRequest, ""},
		{"unknown connection", `{"name":"wiki","title":"Wiki","kind":"live","connection":"wiki"}`, http.StatusNotFound, "connection-not-found"},
		{"schedule too short", `{"name":"often","title":"Often","kind":"upload","sync_every_seconds":60}`, http.StatusBadRequest, ""},
	} {
		refused(t, tc.name, h.request(t, http.MethodPost, sourcesPath, tc.body, nil), tc.status, tc.code)
	}

	decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath,
		`{"name":"repo-search","title":"Repository search","kind":"live","connection":"github"}`, nil), http.StatusCreated)

	refused(t, "sync of a live source", h.request(t, http.MethodPost, sourcesPath+"/repo-search/sync", "", nil), http.StatusConflict, "knowledge-source-live")
	refused(t, "patch of no source", h.request(t, http.MethodPatch, sourcesPath+"/nothing", `{"title":"x"}`, nil), http.StatusNotFound, "knowledge-source-not-found")
	refused(t, "patch of an upload source's secret", h.request(t, http.MethodPatch, handbookPath, `{"secret":"`+repoSecret+`"}`, nil),
		http.StatusBadRequest, "")
	refused(t, "patch to an unknown secret", h.request(t, http.MethodPatch, handbookPath, `{"secret":"gone"}`, nil), http.StatusNotFound, "secret-not-found")

	conn := decode[gen.Connection](t, h.request(t, http.MethodGet, githubPath, "", nil), http.StatusOK)
	if !slices.Equal(conn.UsedBy.KnowledgeSources, []string{"repo-search"}) {
		t.Errorf("github's used_by = %+v; want the live source that reads it", conn.UsedBy)
	}

	refused(t, "delete of a connection a source reads", h.request(t, http.MethodDelete, githubPath, "", nil), http.StatusConflict, "connection-in-use")
}

// Uploads the knowledge base refuses answer their codes, and a refused file stores none of the
// request's files.
func TestKnowledge_UploadRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createHandbook(t)

	refused(t, "a type not indexed", h.upload(t, srcHandbook, [2]string{leaveFile, leaveMD}, [2]string{"deck.key", "PK"}),
		http.StatusUnsupportedMediaType, "unsupported-document-type")
	refused(t, "a file over the limit", h.upload(t, srcHandbook, [2]string{"big.md", strings.Repeat("a", testUploadMax+1)}),
		http.StatusRequestEntityTooLarge, "upload-too-large")
	refused(t, "a request over the limit", h.upload(t, srcHandbook, [2]string{"huge.md", strings.Repeat("a", 2<<20)}),
		http.StatusRequestEntityTooLarge, "upload-too-large")
	refused(t, "no files", h.upload(t, srcHandbook), http.StatusBadRequest, "")
	refused(t, "no source", h.upload(t, "nothing", [2]string{leaveFile, leaveMD}), http.StatusNotFound, "knowledge-source-not-found")

	files := decode[gen.KnowledgeFilePage](t, h.request(t, http.MethodGet, handbookPath+"/files", "", nil), http.StatusOK)
	if len(files.Files) != 0 {
		t.Errorf("files after the refusals = %+v; want none", files.Files)
	}

	if objects, err := h.kbObjects.List(t.Context(), ""); err != nil || len(objects) != 0 {
		t.Errorf("the bucket holds %v (%v) after the refusals; want nothing", objects, err)
	}

	h.createRepoSecret(t)
	decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath, `{"name":"wiki","title":"Wiki","kind":"synced","secret":"`+repoSecret+
		`","config":{"reader":"confluence","site":"acme.atlassian.net","email":"ada@example.com","spaces":"ENG"}}`, nil), http.StatusCreated)

	refused(t, "an upload to a synced source", h.upload(t, "wiki", [2]string{leaveFile, leaveMD}), http.StatusConflict, "not-an-upload-source")
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

// A source with no snapshot answers a null latest_snapshot; after a sync both the list and the get
// answer its newest snapshot.
func TestKnowledge_SourceLatestSnapshot(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	created := h.createHandbook(t)
	if created.LatestSnapshot != nil {
		t.Errorf("new source = %+v; want no snapshot", created.LatestSnapshot)
	}

	if r := h.upload(t, srcHandbook, [2]string{leaveFile, leaveMD}); r.status != http.StatusAccepted {
		t.Fatalf("upload = %d %s; want 202", r.status, r.body)
	}

	h.syncAll(t)

	got := decode[gen.KnowledgeSource](t, h.request(t, http.MethodGet, handbookPath, "", nil), http.StatusOK)
	list := decode[gen.KnowledgeSourceList](t, h.request(t, http.MethodGet, sourcesPath, "", nil), http.StatusOK)

	if len(list.Sources) != 1 {
		t.Fatalf("list = %+v; want handbook", list.Sources)
	}

	for name, s := range map[string]gen.KnowledgeSource{"get": got, "list": list.Sources[0]} {
		snap := s.LatestSnapshot
		if snap == nil || snap.Files != 1 || snap.Failed != 0 || snap.Bytes <= 0 || snap.SyncedAt.IsZero() {
			t.Errorf("%s: latest_snapshot = %+v; want 1 file, 0 failed, some bytes, a time", name, snap)
		}
	}
}
