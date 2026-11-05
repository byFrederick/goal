package api_test

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The knowledge routes of ADR 0038 the tests call, and the runbook they upload.
const (
	knowledgePath = "/v1/orgs/acme/knowledge"
	diskFile      = "db/disk.md"
	diskMD        = "# Disk alerts\n\nBelow 85% they are noise: the nightly vacuum clears them."
)

// rangeCounter is the API's knowledge objects, recording the length of every ranged read.
type rangeCounter struct {
	*porttest.ObjectStore

	mu      sync.Mutex
	lengths []int64
}

func (r *rangeCounter) GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	r.mu.Lock()
	r.lengths = append(r.lengths, length)
	r.mu.Unlock()

	return r.ObjectStore.GetRange(ctx, key, offset, length)
}

// reads are the lengths of the ranged reads so far.
func (r *rangeCounter) reads() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.lengths)
}

// docsReader lists a repository source's files for a Check: their paths, never their content.
type docsReader struct{ paths []string }

func (d *docsReader) List(context.Context, domain.SyncSpec) iter.Seq2[domain.SourceItem, error] {
	return func(yield func(domain.SourceItem, error) bool) {
		for _, p := range d.paths {
			if !yield(domain.SourceItem{ID: p, Version: p, Title: p}, nil) {
				return
			}
		}
	}
}

func (d *docsReader) Fetch(context.Context, domain.SyncSpec, string) (domain.SourceDoc, error) {
	return domain.SourceDoc{}, io.ErrUnexpectedEOF
}

// uploadedHandbook is a harness whose handbook holds leave.md and db/disk.md, uploaded through
// the uploads route and synced into a snapshot.
func uploadedHandbook(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.createHandbook(t)

	up := decode[gen.KnowledgeUploadList](t, h.uploadTo(t, handbookPath+"/uploads", [2]string{leaveFile, leaveMD}, [2]string{diskFile, diskMD}), http.StatusAccepted)
	if len(up.Files) != 2 || up.Files[1].Path != diskFile || up.Files[1].Bytes != int64(len(diskMD)) {
		t.Fatalf("uploaded = %+v; want both files at their paths, folders and all", up.Files)
	}

	h.syncAll(t)

	return h
}

// A source's files are its current snapshot's, filtered by path or title; one file's Markdown
// comes from the snapshot's zip by range, never the whole zip; a path the snapshot lacks is 404.
func TestKnowledge_FileReadByRange(t *testing.T) {
	t.Parallel()

	h := uploadedHandbook(t)

	files := decode[gen.KnowledgeFilePage](t, h.request(t, http.MethodGet, handbookPath+"/files", "", nil), http.StatusOK)
	if len(files.Files) != 2 || files.Files[0].Path != diskFile || files.Files[1].Path != leaveFile || files.NextAfter != nil {
		t.Fatalf("files = %+v; want db/disk.md and leave.md, in path order", files)
	}

	found := decode[gen.KnowledgeFilePage](t, h.request(t, http.MethodGet, handbookPath+"/files?find=db/", "", nil), http.StatusOK)
	if len(found.Files) != 1 || found.Files[0].Path != diskFile {
		t.Errorf("files under db/ = %+v; want db/disk.md", found.Files)
	}

	before := len(h.kbRanges.reads())

	r := h.request(t, http.MethodGet, handbookPath+"/files/"+url.PathEscape(diskFile), "", nil)
	if r.status != http.StatusOK || !strings.Contains(string(r.body), diskMD) || !strings.HasPrefix(r.header.Get("Content-Type"), "text/markdown") {
		t.Fatalf("file = %d %s %q; want its Markdown", r.status, r.header.Get("Content-Type"), r.body)
	}

	ranges := h.kbRanges.reads()[before:]
	if len(ranges) != 4 || slices.Contains(ranges, -1) {
		t.Errorf("ranged reads = %v; want four (end record, directory, header, entry), none of the whole zip", ranges)
	}
}

// A file the current snapshot lacks, a source with no snapshot and an unknown source are refused;
// a file is found across every source by path or title.
func TestKnowledge_FileRefusalsAndFind(t *testing.T) {
	t.Parallel()

	h := uploadedHandbook(t)

	refused(t, "a path the snapshot lacks", h.request(t, http.MethodGet, handbookPath+"/files/"+url.PathEscape("db/none.md"), "", nil),
		http.StatusNotFound, "knowledge-file-not-found")
	refused(t, "a path that is no file's", h.request(t, http.MethodGet, handbookPath+"/files/"+url.PathEscape("../x.md"), "", nil),
		http.StatusNotFound, "knowledge-file-not-found")

	decode[gen.KnowledgeSource](t, h.request(t, http.MethodPost, sourcesPath, `{"name":"drops","title":"Drops","kind":"upload"}`, nil), http.StatusCreated)
	refused(t, "a source with no snapshot", h.request(t, http.MethodGet, sourcesPath+"/drops/files/"+url.PathEscape(leaveFile), "", nil),
		http.StatusNotFound, "knowledge-file-not-found")
	refused(t, "an unknown source", h.request(t, http.MethodGet, sourcesPath+"/wiki/files", "", nil), http.StatusNotFound, "knowledge-source-not-found")

	all := decode[gen.KnowledgeFileList](t, h.request(t, http.MethodGet, knowledgePath+"/files?find=leave", "", nil), http.StatusOK)
	if len(all.Files) != 1 || all.Files[0].Source != srcHandbook || all.Files[0].Path != leaveFile {
		t.Errorf("found across sources = %+v; want handbook's leave.md", all.Files)
	}
}

// An upload is deleted by its path, and the next snapshot leaves it out; a path with no upload
// is 404.
func TestKnowledge_DeleteAnUpload(t *testing.T) {
	t.Parallel()

	h := uploadedHandbook(t)

	if r := h.request(t, http.MethodDelete, handbookPath+"/uploads?path="+url.QueryEscape(diskFile), "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s; want 204", r.status, r.body)
	}

	h.syncAll(t)

	files := decode[gen.KnowledgeFilePage](t, h.request(t, http.MethodGet, handbookPath+"/files", "", nil), http.StatusOK)
	if len(files.Files) != 1 || files.Files[0].Path != leaveFile {
		t.Errorf("files after the delete = %+v; want leave.md only", files.Files)
	}

	refused(t, "a path with no upload", h.request(t, http.MethodDelete, handbookPath+"/uploads?path="+url.QueryEscape(diskFile), "", nil),
		http.StatusNotFound, "knowledge-file-not-found")
	refused(t, "an upload path that is not clean", h.uploadTo(t, handbookPath+"/uploads", [2]string{"../x.md", "x"}), http.StatusBadRequest, "")
}

// The reads rollup is answered by source and window, with the factory's name.
func TestKnowledge_Reads(t *testing.T) {
	t.Parallel()

	h := uploadedHandbook(t)
	ctx := inOrg(t)

	src, err := h.kb.Source(ctx, srcHandbook)
	if err != nil {
		t.Fatal(err)
	}

	today := domain.KnowledgeDay(h.clock.Now())

	if err := h.kb.AddReads(ctx, []domain.KnowledgeFileReads{
		{Source: src.ID, Path: diskFile, Factory: acmeID, Day: today, Reads: 3},
		{Source: src.ID, Path: domain.KnowledgeSourceRoot, Factory: acmeID, Day: today, Searches: 1},
		{Source: src.ID, Path: leaveFile, Factory: acmeID, Day: today.AddDate(0, 0, -10), Reads: 1},
	}); err != nil {
		t.Fatal(err)
	}

	got := decode[gen.KnowledgeReadList](t, h.request(t, http.MethodGet, knowledgePath+"/reads?source=handbook", "", nil), http.StatusOK)

	lines := []string{}
	for _, r := range got.Reads {
		lines = append(lines, r.Source+" "+r.Path+" "+r.Factory+" "+r.Day.String())
	}

	day := " " + acmeSlug + " " + today.Format(time.DateOnly)
	if want := []string{"handbook ." + day, "handbook db/disk.md" + day}; !slices.Equal(lines, want) {
		t.Errorf("reads over 7 days = %v; want %v", lines, want)
	}

	month := decode[gen.KnowledgeReadList](t, h.request(t, http.MethodGet, knowledgePath+"/reads?days=30", "", nil), http.StatusOK)
	if len(month.Reads) != 3 {
		t.Errorf("reads of every source over 30 days = %+v; want all three", month.Reads)
	}

	refused(t, "a window of 91 days", h.request(t, http.MethodGet, knowledgePath+"/reads?days=91", "", nil), http.StatusBadRequest, "")
	refused(t, "an unknown source", h.request(t, http.MethodGet, knowledgePath+"/reads?source=wiki", "", nil), http.StatusNotFound, "knowledge-source-not-found")
}

// A Check lists settings not saved yet and answers the count, the limits, each picked factory's
// warnings and the line agents will see; it stores nothing and never returns the secret.
func TestKnowledge_Check(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := inOrg(t)
	h.kbDocs.paths = []string{"runbooks/disk.md", "runbooks/cpu.md"}
	h.listHosts(t, domain.CodeHost{Host: enterpriseHost, Kind: domain.CodeHostGitHub})

	if _, err := h.secrets.Create(ctx, domain.NewSecret{Name: "docs-token", Value: "test-token", Scope: domain.SecretScope{All: true}}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	body := `{"name":"guides","kind":"repository","secret":"docs-token","config":{"repository":"https://git.example.com/acme/guides"},` +
		`"read_when":"Before you touch production.","factories":["` + acmeSlug + `"]}`

	got := decode[gen.KnowledgeCheck](t, h.request(t, http.MethodPost, knowledgePath+"/check", body, nil), http.StatusOK)
	if !got.Access.Ok || got.Files != 2 || got.Bytes != nil || len(got.OverLimit) != 0 || len(got.Warnings) != 0 ||
		got.Limits.Files != domain.MaxSourceFiles || got.Line != "- `/knowledge/guides/`: Before you touch production. 2 files." {
		t.Errorf("check = %s; want access, 2 files, no warning and the line", asJSON(t, got))
	}

	if want := []string{"runbooks/disk.md", "runbooks/cpu.md"}; !slices.Equal(got.FirstFiles, want) {
		t.Errorf("check's first files = %v; want %v", got.FirstFiles, want)
	}

	// An upload source not saved lists nothing, answered as an empty list and never null.
	upload := h.request(t, http.MethodPost, knowledgePath+"/check", `{"name":"drops","kind":"upload"}`, nil)
	if !strings.Contains(string(upload.body), `"first_files":[]`) {
		t.Errorf("an unsaved upload source's check = %s; want first_files []", upload.body)
	}

	if _, err := h.kb.Source(ctx, "guides"); err == nil {
		t.Error("the checked source was stored")
	}

	refused(t, "settings a source may not have", h.request(t, http.MethodPost, knowledgePath+"/check", `{"name":"guides","kind":"upload","secret":"docs-token"}`, nil),
		http.StatusBadRequest, "")
	refused(t, "an unknown factory", h.request(t, http.MethodPost, knowledgePath+"/check", `{"name":"guides","kind":"upload","factories":["nope"]}`, nil),
		http.StatusNotFound, "factory-not-found")
}

// A Check with a secret a picked factory is outside of warns for that factory, and never answers
// the secret's value.
func TestKnowledge_CheckWarnsOfASecretsScope(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if _, err := h.secrets.Create(inOrg(t), domain.NewSecret{
		Name: "line-token", Value: "test-line", Scope: domain.SecretScope{Factories: []domain.FactoryID{lineID}},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	narrow := `{"name":"guides","kind":"repository","secret":"line-token","config":{"repository":"https://git.example.com/acme/guides"},` +
		`"factories":["` + acmeSlug + `"]}`

	got := decode[gen.KnowledgeCheck](t, h.request(t, http.MethodPost, knowledgePath+"/check", narrow, nil), http.StatusOK)
	if got.Access.Ok || len(got.Warnings) != 1 || got.Warnings[0].Code != "knowledge-secret-not-in-scope" || valueOf(got.Warnings[0].Factory) != acmeSlug ||
		strings.Contains(asJSON(t, got), "test-line") {
		t.Errorf("check with a secret open to line only = %s; want no access and acme out of its scope", asJSON(t, got))
	}
}

// knowledge:read lets a member read files and reads; check, uploads and deletion need
// knowledge:manage, which owners and admins have and members do not.
func TestKnowledge_PermissionMatrix(t *testing.T) {
	t.Parallel()

	h := uploadedHandbook(t)
	admin := acmeMember(domain.FactoryViewer)
	admin.OrgRole = domain.OrgAdmin

	reads := []struct{ method, path, body string }{
		{http.MethodGet, handbookPath + "/files", ""},
		{http.MethodGet, handbookPath + "/files/" + url.PathEscape(diskFile), ""},
		{http.MethodGet, knowledgePath + "/files?find=db", ""},
		{http.MethodGet, knowledgePath + "/reads", ""},
	}
	manages := []struct{ method, path, body string }{
		{http.MethodPost, knowledgePath + "/check", `{"name":"guides","kind":"upload"}`},
		{http.MethodDelete, handbookPath + "/uploads?path=" + url.QueryEscape(leaveFile), ""},
		{http.MethodDelete, handbookPath, ""},
	}

	h.as(acmeMember(domain.FactoryViewer))

	for _, c := range reads {
		if r := h.request(t, c.method, c.path, c.body, nil); r.status != http.StatusOK {
			t.Errorf("a member's %s %s = %d %s; want 200", c.method, c.path, r.status, r.body)
		}
	}

	for _, c := range manages {
		refused(t, "a member's "+c.method+" "+c.path, h.request(t, c.method, c.path, c.body, nil), http.StatusForbidden, "forbidden")
	}

	refused(t, "a member's upload", h.uploadTo(t, handbookPath+"/uploads", [2]string{"x.md", "x"}), http.StatusForbidden, "forbidden")

	h.as(admin)

	for _, c := range manages {
		if r := h.request(t, c.method, c.path, c.body, nil); r.status >= http.StatusBadRequest {
			t.Errorf("an admin's %s %s = %d %s; want it allowed", c.method, c.path, r.status, r.body)
		}
	}
}
