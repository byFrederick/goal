package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The definitions routes (B §8) on the harness's authoring service: acme (inbox → approve →
// done) and line are published as v1.

// Names the definitions tests use.
const (
	ifMatchHeader  = "If-Match"
	acmeTitle      = "Acme"
	filesKey       = "files"
	expectedKey    = "expected_current_version_id"
	revisionKey    = "revision"
	stInbox        = "inbox"
	codeNoIfMatch  = "precondition-required"
	factoriesPath  = "/v1/orgs/acme/factories"
	unknownVersion = "0b6f1c9e-0000-7000-8000-000000000999"
)

// sections is the part of a FactoryModel the tests read.
type sections struct {
	Basics map[string]any `json:"basics"`
	Line   map[string]any `json:"line"`
}

// acmeRenamed is acme with another title; acmeBroken names an agent acme lacks.
var (
	acmeRenamed = strings.Replace(acme, "title: Acme }", "title: Acme two }", 1)
	acmeBroken  = strings.Replace(acme, "kind: human", "kind: agent, agent: nobody", 1)
)

// decode reads a response body as T, failing the test on a status other than want.
func decode[T any](t *testing.T, r response, want int) T {
	t.Helper()

	if r.status != want {
		t.Fatalf("status %d %s, want %d", r.status, r.body, want)
	}

	var out T
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}

	return out
}

// refused checks that r is a problem with status and code, and returns it.
func refused(t *testing.T, what string, r response, status int, code string) gen.Problem {
	t.Helper()

	var p gen.Problem
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatalf("%s: decode %s: %v", what, r.body, err)
	}

	if r.status != status || valueOf(p.Code) != code || r.header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("%s: %d %s, want %d %s", what, r.status, r.body, status, code)
	}

	return p
}

func valueOf[T any](p *T) T {
	var zero T

	if p != nil {
		return *p
	}

	return zero
}

// quoted is an If-Match of tag.
func quoted(tag string) http.Header { return http.Header{ifMatchHeader: {`"` + tag + `"`}} }

// textFiles is a FileMap body of text files.
func textFiles(files map[string]string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for p, b := range files {
		out[p] = map[string]string{"text": b}
	}

	return out
}

func body(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// openDraft opens acme's draft and returns it.
func (h *harness) openDraft(t *testing.T) gen.Draft {
	t.Helper()

	return decode[gen.Draft](t, h.request(t, http.MethodPost, acmeDraft, `{}`, nil), http.StatusCreated)
}

// model reads acme's draft model.
func (h *harness) model(t *testing.T) (gen.DraftModel, sections) {
	t.Helper()

	m := decode[gen.DraftModel](t, h.request(t, http.MethodGet, acmeDraft+"/model", "", nil), http.StatusOK)

	var s sections
	if err := json.Unmarshal(m.Model, &s); err != nil {
		t.Fatal(err)
	}

	return m, s
}

// saveYAML writes acme's draft factory.yaml as text, over whatever is there.
func (h *harness) saveYAML(t *testing.T, doc string) gen.SaveResult {
	t.Helper()

	file := h.request(t, http.MethodGet, acmeDraft+"/files/factory.yaml", "", nil)
	if file.status != http.StatusOK {
		t.Fatalf("read factory.yaml: %d %s", file.status, file.body)
	}

	return decode[gen.SaveResult](t, h.request(t, http.MethodPut, acmeDraft+"/files/factory.yaml", doc,
		http.Header{ifMatchHeader: {file.header.Get("ETag")}}), http.StatusOK)
}

// current is the id of the version acme runs.
func (h *harness) current(t *testing.T) string {
	t.Helper()

	return decode[gen.Factory](t, h.request(t, http.MethodGet, acmeFactory, "", nil), http.StatusOK).Current.Id
}

// publish publishes acme's open draft at its current revision against the version running.
func (h *harness) publish(t *testing.T, message string) gen.Version {
	t.Helper()

	current := h.current(t)
	d := decode[gen.Draft](t, h.request(t, http.MethodGet, acmeDraft, "", nil), http.StatusOK)

	return decode[gen.Version](t, h.request(t, http.MethodPost, acmeDraft+"/publish", body(t, map[string]any{
		revisionKey: d.Revision, expectedKey: current, "message": message,
	}), nil), http.StatusCreated)
}

// renamed publishes acme's v2, with another title, and returns it.
func (h *harness) renamed(t *testing.T) gen.Version {
	t.Helper()

	h.openDraft(t)
	h.saveYAML(t, acmeRenamed)

	return h.publish(t, "Rename")
}

func TestDefinitions_FactoriesShowVersionsAndDrafts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	list := decode[gen.FactoryList](t, h.request(t, http.MethodGet, factoriesPath, "", nil), http.StatusOK)
	if first := list.Factories[0]; len(list.Factories) != 2 || first.Name != acmeSlug || first.Current == nil || first.Current.Number != 1 ||
		first.Draft != nil || first.Paused {
		t.Fatalf("factories %s, want acme and line running v1 with no draft", body(t, list))
	}

	refused(t, "unknown factory", h.request(t, http.MethodGet, factoriesPath+"/nope", "", nil), http.StatusNotFound,
		authoring.CodeFactoryNotFound)
	refused(t, "another org's factory", h.request(t, http.MethodGet, "/v1/orgs/example-labs/factories/acme", "", nil),
		http.StatusNotFound, authoring.CodeFactoryNotFound)
}

func TestDefinitions_CreateABlankFactory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	created := h.request(t, http.MethodPost, factoriesPath, `{"name":"support","title":"Support","start":{"kind":"blank"}}`, nil)
	f := decode[gen.Factory](t, created, http.StatusCreated)

	if created.header.Get("Location") != factoriesPath+"/"+f.Id || f.Current != nil || f.Draft == nil ||
		f.Draft.Revision != 1 || len(f.Stations) != 0 || f.Name != "support" {
		t.Errorf("created %s at %s, want a draft-only factory at its address", created.body, created.header.Get("Location"))
	}

	if got := decode[gen.Factory](t, h.request(t, http.MethodGet, factoriesPath+"/support", "", nil), http.StatusOK); got.Id != f.Id {
		t.Errorf("get by name = %+v, want the new factory", got)
	}

	if list := decode[gen.FactoryList](t, h.request(t, http.MethodGet, factoriesPath, "", nil), http.StatusOK); len(list.Factories) != 3 {
		t.Errorf("factories %s, want the new one listed", body(t, list))
	}
}

func TestDefinitions_PauseAndResume(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// goald loads the factories it runs (the harness's cache, when a route first names one).
	h.request(t, http.MethodGet, acmeFactory+"/board", "", nil)

	if paused := decode[gen.Factory](t, h.request(t, http.MethodPost, acmeFactory+"/pause", "", nil), http.StatusOK); !paused.Paused ||
		len(paused.Stations) != 3 {
		t.Errorf("pause = %+v, want acme paused with its stations", paused)
	}

	if resumed := decode[gen.Factory](t, h.request(t, http.MethodPost, acmeFactory+"/resume", "", nil), http.StatusOK); resumed.Paused {
		t.Errorf("resume = %+v, want acme running", resumed)
	}
}

func TestDefinitions_CreateFactoryStarts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putTemplate(t, "acme-review", 1, acmeFiles)

	tests := []struct {
		name, start string
		want        int
		code        string
	}{
		{name: "template", start: `{"kind":"template","template":"acme-review"}`, want: http.StatusCreated},
		{name: "clone", start: `{"kind":"clone","factory":"acme","version":"1"}`, want: http.StatusCreated},
		{name: "import", start: body(t, map[string]any{"kind": "import", filesKey: textFiles(acmeFiles)}), want: http.StatusCreated},
		{name: "unknown template", start: `{"kind":"template","template":"nope"}`, want: http.StatusNotFound, code: authoring.CodeTemplateNotFound},
		{name: "unknown clone source", start: `{"kind":"clone","factory":"nope"}`, want: http.StatusNotFound, code: authoring.CodeFactoryNotFound},
		{name: "bad clone version", start: `{"kind":"clone","factory":"acme","version":"v1"}`, want: http.StatusBadRequest, code: authoring.CodeInvalidRequest},
		{name: "import without factory.yaml", start: `{"kind":"import","files":{"README.md":{"text":"hi"}}}`, want: http.StatusUnprocessableEntity, code: authoring.CodeImportInvalid},
		{name: "import of a bad path", start: `{"kind":"import","files":{"knowledge/x.md":{"text":"hi"}}}`, want: http.StatusUnprocessableEntity, code: authoring.CodeBadPath},
		{
			name: "import keeping a taken id", start: body(t, map[string]any{"kind": "import", "keep_id": true, filesKey: textFiles(acmeFiles)}),
			want: http.StatusConflict, code: authoring.CodeFactoryExists,
		},
		{name: "a file with text and base64", start: `{"kind":"import","files":{"factory.yaml":{"text":"a","base64":"YQ=="}}}`, want: http.StatusBadRequest},
		{name: "unknown kind", start: `{"kind":"magic"}`, want: http.StatusBadRequest},
	}

	for i, tt := range tests {
		name := fmt.Sprintf("new-%d", i)
		r := h.request(t, http.MethodPost, factoriesPath, fmt.Sprintf(`{"name":%q,"start":%s}`, name, tt.start), nil)

		switch {
		case tt.want == http.StatusCreated:
			if f := decode[gen.Factory](t, r, http.StatusCreated); f.Id == acmeID || f.Name != name {
				t.Errorf("%s: created %+v, want a new id and the request's name", tt.name, f)
			}
		case tt.code == "":
			if r.status != tt.want {
				t.Errorf("%s: %d %s, want %d", tt.name, r.status, r.body, tt.want)
			}
		default:
			refused(t, tt.name, r, tt.want, tt.code)
		}
	}

	refused(t, "taken name", h.request(t, http.MethodPost, factoriesPath, `{"name":"acme","start":{"kind":"blank"}}`, nil),
		http.StatusConflict, authoring.CodeNameTaken)
	refused(t, "no name", h.request(t, http.MethodPost, factoriesPath, `{"start":{"kind":"blank"}}`, nil),
		http.StatusBadRequest, authoring.CodeInvalidRequest)
}

// putTemplate adds a template version to the catalog.
func (h *harness) putTemplate(t *testing.T, name string, version int, files map[string]string) {
	t.Helper()

	base := fmt.Sprintf("%s%s/%d/", authoring.TemplatesPrefix, name, version)
	meta := fmt.Sprintf("title: Acme review v%d\nsummary: Build and review\nchanged: \"2026-10-09\"\n", version)

	if _, err := h.objects.Put(t.Context(), base+"template.yaml", []byte(meta)); err != nil {
		t.Fatal(err)
	}

	for p, b := range files {
		if _, err := h.objects.Put(t.Context(), base+"factory/"+p, []byte(b)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDefinitions_DeleteOnlyAFactoryNeverPublished(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	decode[gen.Factory](t, h.request(t, http.MethodPost, factoriesPath, `{"name":"support","start":{"kind":"blank"}}`, nil), http.StatusCreated)

	if r := h.request(t, http.MethodDelete, factoriesPath+"/support", "", nil); r.status != http.StatusNoContent {
		t.Errorf("delete a draft factory = %d %s, want 204", r.status, r.body)
	}

	refused(t, "deleted", h.request(t, http.MethodGet, factoriesPath+"/support", "", nil), http.StatusNotFound, authoring.CodeFactoryNotFound)
	refused(t, "published", h.request(t, http.MethodDelete, acmeFactory, "", nil), http.StatusConflict, authoring.CodeFactoryPublished)
}

func TestDefinitions_Templates(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putTemplate(t, "acme-review", 1, acmeFiles)
	h.putTemplate(t, "acme-review", 2, acmeFiles)

	list := decode[gen.TemplateList](t, h.request(t, http.MethodGet, "/v1/orgs/acme/templates", "", nil), http.StatusOK)
	if len(list.Templates) != 1 || list.Templates[0].Version != 2 || list.Templates[0].Stations != 3 || list.Templates[0].Connectors == nil {
		t.Errorf("templates %s, want acme-review v2 with 3 stations", body(t, list))
	}

	v1 := decode[gen.Template](t, h.request(t, http.MethodGet, "/v1/orgs/acme/templates/acme-review?version=1", "", nil), http.StatusOK)

	var model sections
	if err := json.Unmarshal(v1.Model, &model); err != nil || v1.Version != 1 || model.Line["stations"] == nil {
		t.Errorf("template v1 %s (%v), want its model's stations", body(t, v1), err)
	}

	refused(t, "unknown version", h.request(t, http.MethodGet, "/v1/orgs/acme/templates/acme-review?version=7", "", nil),
		http.StatusNotFound, authoring.CodeTemplateNotFound)
}

func TestDefinitions_ValidateFiles(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	validate := func(files map[string]string) response {
		return h.request(t, http.MethodPost, "/v1/orgs/acme/definitions/validate", body(t, map[string]any{filesKey: textFiles(files)}), nil)
	}

	// A new factory: another name than the organization's.
	fresh := map[string]string{yamlFile: strings.Replace(acme, "name: acme,", "name: acme-new,", 1), writerFile: writerPrompt}

	if ok := decode[gen.Validation](t, validate(fresh), http.StatusOK); !ok.Ok || ok.Counts.Stations != 3 || ok.Counts.Agents != 1 || len(ok.Checks) == 0 {
		t.Errorf("validation %s, want ok with 3 stations and its checks", body(t, ok))
	}

	bad := decode[gen.Validation](t, validate(map[string]string{yamlFile: acmeBroken, writerFile: writerPrompt}), http.StatusOK)
	if bad.Ok || !slices.ContainsFunc(bad.Problems, func(p gen.DefinitionProblem) bool {
		return p.Code == "unknown-agent" && p.Line > 0 && p.Severity == gen.DefinitionProblemSeverityError && valueOf(p.File) == "factory.yaml"
	}) {
		t.Errorf("validation %s, want a located unknown-agent error", body(t, bad))
	}

	refused(t, "a file over 1 MiB", validate(map[string]string{yamlFile: acme, writerFile: strings.Repeat("x", domain.MaxBlobSize+1)}),
		http.StatusRequestEntityTooLarge, authoring.CodeTooLarge)
}

func TestDefinitions_FormatYAML(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	formatted := h.request(t, http.MethodPost, "/v1/orgs/acme/definitions/format", "a:   b\nc: [1,  2]\n", yamlBody)
	if formatted.status != http.StatusOK || formatted.header.Get("Content-Type") != "text/yaml" || !strings.HasPrefix(string(formatted.body), "a: b\n") {
		t.Errorf("format = %d %s %q, want the formatted document", formatted.status, formatted.header.Get("Content-Type"), formatted.body)
	}

	p := refused(t, "format of YAML that does not parse", h.request(t, http.MethodPost, "/v1/orgs/acme/definitions/format",
		"a: [b\n", yamlBody), http.StatusUnprocessableEntity, authoring.CodeYAMLInvalid)
	if p.ParseError == nil || p.ParseError.Line == 0 {
		t.Errorf("problem %+v, want where it stops parsing", p)
	}
}

func TestDefinitions_DraftOpen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	refused(t, "no draft yet", h.request(t, http.MethodGet, acmeDraft, "", nil), http.StatusNotFound, authoring.CodeNoDraft)

	d := h.openDraft(t)
	if d.Base == nil || d.Base.Number != 1 || d.Current == nil || d.Current.Id != d.Base.Id || d.NextNumber != 2 ||
		d.Revision != 1 || d.Source.Kind != gen.VersionSourceKindEdit || len(d.Sections) != 5 || d.CreatedBy.Id != "local" {
		t.Errorf("draft %s, want draft of v2 based on v1, running", body(t, d))
	}

	if again := decode[gen.Draft](t, h.request(t, http.MethodPost, acmeDraft, `{}`, nil), http.StatusOK); again.Id != d.Id {
		t.Errorf("open again = %s, want the same draft", body(t, again))
	}

	if got := h.request(t, http.MethodGet, acmeDraft, "", nil); got.header.Get("ETag") != `"1"` {
		t.Errorf("get draft ETag %q, want the revision", got.header.Get("ETag"))
	}
}

func TestDefinitions_DiscardDraft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.openDraft(t)

	refused(t, "discard without If-Match", h.request(t, http.MethodDelete, acmeDraft, "", nil), http.StatusPreconditionRequired, codeNoIfMatch)
	refused(t, "discard of a stale revision", h.request(t, http.MethodDelete, acmeDraft, "", quoted("9")), http.StatusPreconditionFailed,
		authoring.CodeDraftConflict)

	if r := h.request(t, http.MethodDelete, acmeDraft, "", http.Header{ifMatchHeader: {"1"}}); r.status != http.StatusBadRequest {
		t.Errorf("unquoted If-Match = %d, want 400", r.status)
	}

	if r := h.request(t, http.MethodDelete, acmeDraft, "", quoted("1")); r.status != http.StatusNoContent {
		t.Fatalf("discard = %d %s, want 204", r.status, r.body)
	}

	refused(t, "discarded", h.request(t, http.MethodGet, acmeDraft, "", nil), http.StatusNotFound, authoring.CodeNoDraft)
}

func TestDefinitions_DraftOnAnotherBase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.renamed(t)
	h.openDraft(t)

	refused(t, "a draft on v1 while one on v2 is open", h.request(t, http.MethodPost, acmeDraft, `{"base":"1"}`, nil),
		http.StatusConflict, authoring.CodeDraftExists)

	if replaced := decode[gen.Draft](t, h.request(t, http.MethodPost, acmeDraft, `{"base":"1","replace":true}`, nil), http.StatusCreated); replaced.Base.Number != 1 {
		t.Errorf("replaced draft %s, want one based on v1", body(t, replaced))
	}

	refused(t, "a draft on a selector that is no version", h.request(t, http.MethodPost, acmeDraft, `{"base":"draft"}`, nil),
		http.StatusBadRequest, authoring.CodeInvalidRequest)
}

func TestDefinitions_SectionSaves(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.openDraft(t)

	m, model := h.model(t)
	if model.Basics["name"] != acmeSlug || m.Etags.Basics == "" || len(m.Ranges) == 0 || len(m.Edited) != 0 {
		t.Fatalf("model %s, want acme's, unedited, with etags and ranges", body(t, m))
	}

	model.Basics["title"] = "Acme two"
	save := body(t, map[string]any{"value": model.Basics})

	refused(t, "save without If-Match", h.request(t, http.MethodPut, acmeDraft+"/model/basics", save, nil), http.StatusPreconditionRequired, codeNoIfMatch)

	saved := decode[gen.SaveResult](t, h.request(t, http.MethodPut, acmeDraft+"/model/basics", save, quoted(m.Etags.Basics)), http.StatusOK)
	if saved.Revision != 2 || valueOf(saved.Etag) == "" || valueOf(saved.Etag) == m.Etags.Basics || saved.ChangeCount != 1 {
		t.Errorf("save %s, want revision 2, a new etag and one change", body(t, saved))
	}

	// Another tab saving basics on the etag it read gets the section as it is now.
	stale := body(t, map[string]any{"value": map[string]any{"name": acmeSlug, "title": "Acme three"}})
	p := refused(t, "stale save", h.request(t, http.MethodPut, acmeDraft+"/model/basics", stale, quoted(m.Etags.Basics)),
		http.StatusPreconditionFailed, authoring.CodeDraftConflict)

	if now, _ := p.Value.(map[string]any); p.Section == nil || *p.Section != gen.Basics || valueOf(p.Etag) != valueOf(saved.Etag) || now["title"] != "Acme two" {
		t.Errorf("conflict %+v, want basics as saved and its etag", p)
	}

	// Another section saves on its own etag, whatever happened to basics.
	decode[gen.SaveResult](t, h.request(t, http.MethodPut, acmeDraft+"/model/line", body(t, map[string]any{"value": model.Line}),
		quoted(m.Etags.Line)), http.StatusOK)

	refused(t, "a value of the wrong shape", h.request(t, http.MethodPut, acmeDraft+"/model/basics", `{"value":[1]}`, quoted(valueOf(saved.Etag))),
		http.StatusUnprocessableEntity, authoring.CodeModelInvalid)
	refused(t, "an unknown section", h.request(t, http.MethodPut, acmeDraft+"/model/nope", save, quoted("x")), http.StatusBadRequest,
		authoring.CodeInvalidRequest)
}

func TestDefinitions_RevertAndUnparsableYAML(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.openDraft(t)
	h.saveYAML(t, acmeRenamed)

	edited, _ := h.model(t)
	if _, ok := edited.Edited["/basics/title"]; !ok {
		t.Fatalf("edited %v, want the title marked", edited.Edited)
	}

	revert := acmeDraft + "/model/basics/revert"

	refused(t, "revert without If-Match", h.request(t, http.MethodPost, revert, `{"pointers":["/basics/title"]}`, nil),
		http.StatusPreconditionRequired, codeNoIfMatch)
	decode[gen.SaveResult](t, h.request(t, http.MethodPost, revert, `{"pointers":["/basics/title"]}`, quoted(edited.Etags.Basics)), http.StatusOK)

	if reverted, _ := h.model(t); len(reverted.Edited) != 0 {
		t.Errorf("after the revert edited %v, want nothing", reverted.Edited)
	}

	// While factory.yaml does not parse, Visual is read-only.
	h.saveYAML(t, "apiVersion: goal/v1\nkind: [Factory\n")

	p := refused(t, "model of YAML that does not parse", h.request(t, http.MethodGet, acmeDraft+"/model", "", nil), http.StatusConflict,
		authoring.CodeYAMLInvalid)
	if p.ParseError == nil || p.ParseError.Line == 0 {
		t.Errorf("problem %+v, want where it stops parsing", p)
	}

	refused(t, "save on YAML that does not parse", h.request(t, http.MethodPut, acmeDraft+"/model/basics", `{"value":{}}`, quoted(edited.Etags.Basics)),
		http.StatusConflict, authoring.CodeYAMLInvalid)
}

func TestDefinitions_FileReads(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.openDraft(t)

	files := decode[gen.FileList](t, h.request(t, http.MethodGet, acmeDraft+"/files", "", nil), http.StatusOK)
	if len(files.Files) != 2 || files.Files[0].Path != writerFile || files.Files[0].Size != int64(len(writerPrompt)) {
		t.Errorf("files %s, want the two files with sizes", body(t, files))
	}

	// A path's slashes are percent-encoded: it is one segment.
	prompt := h.request(t, http.MethodGet, acmeDraft+"/files/agents%2Fwriter.md", "", nil)
	if prompt.status != http.StatusOK || string(prompt.body) != writerPrompt || prompt.header.Get("ETag") != `"`+files.Files[0].Sha256+`"` {
		t.Errorf("prompt = %d %q ETag %s, want the file and its hash", prompt.status, prompt.body, prompt.header.Get("ETag"))
	}

	refused(t, "unknown file", h.request(t, http.MethodGet, acmeDraft+"/files/agents%2Fnope.md", "", nil), http.StatusNotFound,
		authoring.CodeFileNotFound)
}

func TestDefinitions_FileWrites(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.openDraft(t)

	notes := acmeDraft + "/files/agents%2Fnotes.md"
	create := http.Header{"If-None-Match": {"*"}}
	writer := string(domain.HashBlob([]byte(writerPrompt)))

	refused(t, "write without a precondition", h.request(t, http.MethodPut, notes, "notes", nil), http.StatusPreconditionRequired, codeNoIfMatch)

	if r := h.request(t, http.MethodPut, notes, "notes", http.Header{"If-None-Match": {"*"}, ifMatchHeader: {`"x"`}}); r.status != http.StatusBadRequest {
		t.Errorf("both preconditions = %d, want 400", r.status)
	}

	created := decode[gen.SaveResult](t, h.request(t, http.MethodPut, notes, "notes", create), http.StatusOK)
	if valueOf(created.Sha256) != string(domain.HashBlob([]byte("notes"))) {
		t.Errorf("create %s, want the file's hash", body(t, created))
	}

	p := refused(t, "create of a file that exists", h.request(t, http.MethodPut, notes, "other", create), http.StatusPreconditionFailed,
		authoring.CodeDraftConflict)
	if valueOf(p.Path) != "agents/notes.md" || valueOf(p.Sha256) != valueOf(created.Sha256) {
		t.Errorf("conflict %+v, want the file's hash now", p)
	}

	refused(t, "stale write", h.request(t, http.MethodPut, notes, "other", quoted(writer)), http.StatusPreconditionFailed, authoring.CodeDraftConflict)
	refused(t, "bad path", h.request(t, http.MethodPut, acmeDraft+"/files/knowledge%2Fx.md", "x", create), http.StatusUnprocessableEntity,
		authoring.CodeBadPath)
	refused(t, "a file over 1 MiB", h.request(t, http.MethodPut, notes, strings.Repeat("x", domain.MaxBlobSize+1), quoted(valueOf(created.Sha256))),
		http.StatusRequestEntityTooLarge, authoring.CodeTooLarge)

	refused(t, "delete without If-Match", h.request(t, http.MethodDelete, notes, "", nil), http.StatusPreconditionRequired, codeNoIfMatch)
	refused(t, "stale delete", h.request(t, http.MethodDelete, notes, "", quoted(writer)), http.StatusPreconditionFailed, authoring.CodeDraftConflict)
	decode[gen.SaveResult](t, h.request(t, http.MethodDelete, notes, "", quoted(valueOf(created.Sha256))), http.StatusOK)

	refused(t, "deleted file", h.request(t, http.MethodGet, notes, "", nil), http.StatusNotFound, authoring.CodeFileNotFound)
}

func TestDefinitions_TreeAndValidation(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	d := h.openDraft(t)

	tree := func(files map[string]string, expected int64) string {
		return body(t, map[string]any{filesKey: textFiles(files), "expected_revision": expected})
	}

	refused(t, "stale tree", h.request(t, http.MethodPut, acmeDraft+"/tree", tree(acmeFiles, d.Revision+5), nil), http.StatusPreconditionFailed,
		authoring.CodeDraftConflict)

	// A tree may be larger than other routes' bodies.
	big := map[string]string{
		yamlFile: acmeRenamed, writerFile: writerPrompt,
		"agents/a.md": strings.Repeat("a", 700<<10), "agents/b.md": strings.Repeat("b", 700<<10),
	}
	if saved := decode[gen.SaveResult](t, h.request(t, http.MethodPut, acmeDraft+"/tree", tree(big, d.Revision), nil), http.StatusOK); saved.Revision != 2 {
		t.Errorf("tree %s, want revision 2", body(t, saved))
	}

	refused(t, "a 1.4 MiB item", h.request(t, http.MethodPost, acmeItems, body(t, map[string]string{
		"title": "t", "type": "fix", "body": strings.Repeat("x", 1400<<10),
	}), nil), http.StatusRequestEntityTooLarge, authoring.CodeTooLarge)

	if v := decode[gen.Validation](t, h.request(t, http.MethodPost, acmeDraft+"/validate", "", nil), http.StatusOK); !v.Ok || v.Revision != 2 ||
		v.Counts.Changes == 0 {
		t.Errorf("validation %s, want the saved draft ok with its changes", body(t, v))
	}

	overlay := body(t, map[string]any{"overlay": textFiles(map[string]string{yamlFile: "kind: [\n"})})
	if v := decode[gen.Validation](t, h.request(t, http.MethodPost, acmeDraft+"/validate", overlay, nil), http.StatusOK); v.Ok ||
		v.Problems[0].Code != "yaml-invalid" {
		t.Errorf("validation of unsaved YAML %s, want yaml-invalid", body(t, v))
	}
}

func TestDefinitions_Publish(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	current := h.current(t)
	d := h.openDraft(t)
	saved := h.saveYAML(t, acmeRenamed)

	publish := func(revision int64, expected string) string {
		return body(t, map[string]any{revisionKey: revision, expectedKey: expected, "message": "Rename"})
	}

	refused(t, "stale revision", h.request(t, http.MethodPost, acmeDraft+"/publish", publish(d.Revision, current), nil),
		http.StatusPreconditionFailed, authoring.CodeDraftConflict)
	refused(t, "stale current", h.request(t, http.MethodPost, acmeDraft+"/publish", publish(saved.Revision, unknownVersion), nil),
		http.StatusConflict, authoring.CodeBaseMoved)

	v := decode[gen.Version](t, h.request(t, http.MethodPost, acmeDraft+"/publish", publish(saved.Revision, current), nil), http.StatusCreated)
	if valueOf(v.Number) != 2 || !v.Current || v.State != gen.VersionStatePublished || v.Message != "Rename" ||
		valueOf(v.PublishedBy).Id != "local" || v.Counts.Stations != 3 {
		t.Errorf("published %s, want v2 running, by the local user", body(t, v))
	}

	if f := decode[gen.Factory](t, h.request(t, http.MethodGet, acmeFactory, "", nil), http.StatusOK); f.Current.Number != 2 || f.Draft != nil {
		t.Errorf("factory %s, want v2 and no draft", body(t, f))
	}
}

func TestDefinitions_PublishRefusesErrors(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	current := h.current(t)

	h.openDraft(t)
	saved := h.saveYAML(t, acmeBroken)

	p := refused(t, "invalid", h.request(t, http.MethodPost, acmeDraft+"/publish", body(t, map[string]any{
		revisionKey: saved.Revision, expectedKey: current,
	}), nil), http.StatusConflict, authoring.CodeDefinitionInvalid)
	if p.Problems == nil || !slices.ContainsFunc(*p.Problems, func(p gen.DefinitionProblem) bool { return p.Code == "unknown-agent" }) {
		t.Errorf("problem %+v, want the unknown-agent problem", p)
	}
}

func TestDefinitions_PublishMovesItemsOffRemovedStations(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.submit(t)

	if err := h.engine.Tick(inOrg(t)); err != nil { // inbox → approve, waiting for a human
		t.Fatal(err)
	}

	current := h.current(t)

	h.openDraft(t)
	saved := h.saveYAML(t, strings.Replace(acme, "      - { id: approve, kind: human }\n", "", 1))

	publish := func(moves map[string]string) string {
		return body(t, map[string]any{revisionKey: saved.Revision, expectedKey: current, "station_moves": moves})
	}

	p := refused(t, "unmapped items", h.request(t, http.MethodPost, acmeDraft+"/publish", publish(nil), nil), http.StatusConflict,
		authoring.CodeItemsAtRemoved)
	if p.Items == nil || len(p.Items.Unmapped) != 1 || p.Items.Unmapped[0].Id != it.Id || p.Items.Unmapped[0].Station != "approve" {
		t.Fatalf("problem %+v, want the item waiting at approve", p)
	}

	decode[gen.Version](t, h.request(t, http.MethodPost, acmeDraft+"/publish", publish(map[string]string{"approve": stInbox}), nil), http.StatusCreated)

	if moved := decode[gen.Item](t, h.request(t, http.MethodGet, "/v1/orgs/acme/items/"+it.Id, "", nil), http.StatusOK); moved.Station != stInbox {
		t.Errorf("item at %s, want it moved to inbox", moved.Station)
	}
}

func TestDefinitions_VersionHistory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.renamed(t)

	first := decode[gen.VersionPage](t, h.request(t, http.MethodGet, acmeFactory+"/versions?limit=1", "", nil), http.StatusOK)
	if len(first.Versions) != 1 || valueOf(first.Versions[0].Number) != 2 || !first.Versions[0].Current || valueOf(first.NextBefore) != 2 {
		t.Fatalf("first page %s, want v2 and a next page", body(t, first))
	}

	rest := decode[gen.VersionPage](t, h.request(t, http.MethodGet, acmeFactory+"/versions?before=2", "", nil), http.StatusOK)
	if len(rest.Versions) != 1 || valueOf(rest.Versions[0].Number) != 1 || rest.Versions[0].Current || rest.NextBefore != nil {
		t.Errorf("next page %s, want v1 and no more", body(t, rest))
	}

	for _, q := range []string{"?limit=51", "?limit=0", "?before=0"} {
		if r := h.request(t, http.MethodGet, acmeFactory+"/versions"+q, "", nil); r.status != http.StatusBadRequest {
			t.Errorf("versions%s = %d, want 400", q, r.status)
		}
	}
}

func TestDefinitions_VersionReads(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	v1 := decode[gen.VersionDetail](t, h.request(t, http.MethodGet, acmeFactory+"/versions/1", "", nil), http.StatusOK)
	if valueOf(v1.Number) != 1 || len(v1.Files) != 2 || v1.Source.Kind != gen.VersionSourceKindCli || !v1.Current {
		t.Errorf("v1 %s, want the running version with its two files", body(t, v1))
	}

	refused(t, "unknown version", h.request(t, http.MethodGet, acmeFactory+"/versions/9", "", nil), http.StatusNotFound, authoring.CodeVersionNotFound)
	refused(t, "bad selector", h.request(t, http.MethodGet, acmeFactory+"/versions/v1", "", nil), http.StatusBadRequest, authoring.CodeInvalidRequest)

	vm := decode[gen.VersionModel](t, h.request(t, http.MethodGet, acmeFactory+"/versions/1/model", "", nil), http.StatusOK)

	var model sections
	if err := json.Unmarshal(vm.Model, &model); err != nil || model.Basics["title"] != acmeTitle || len(vm.Ranges) == 0 {
		t.Errorf("v1 model %s (%v), want v1's title", body(t, vm), err)
	}

	file := h.request(t, http.MethodGet, acmeFactory+"/versions/1/files/factory.yaml", "", nil)
	if file.status != http.StatusOK || string(file.body) != acme || file.header.Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Errorf("v1 factory.yaml = %d %s, want its bytes, cached forever", file.status, file.header.Get("Cache-Control"))
	}

	refused(t, "unknown file", h.request(t, http.MethodGet, acmeFactory+"/versions/1/files/nope.md", "", nil), http.StatusNotFound,
		authoring.CodeFileNotFound)

	h.openDraft(t)

	if draft := h.request(t, http.MethodGet, acmeFactory+"/versions/draft/files/factory.yaml", "", nil); draft.header.Get("Cache-Control") != "no-cache" {
		t.Errorf("draft file Cache-Control %q, want no-cache", draft.header.Get("Cache-Control"))
	}
}

func TestDefinitions_Compare(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.renamed(t)
	h.openDraft(t)

	c := decode[gen.Comparison](t, h.request(t, http.MethodGet, acmeFactory+"/compare?from=1&to=2", "", nil), http.StatusOK)
	if c.From.Selector != "1" || valueOf(c.From.Number) != 1 || valueOf(c.To.Number) != 2 || c.Counts.Changes != 1 ||
		len(c.Changes) != 1 || c.Changes[0].Section != "basics" {
		t.Errorf("compare %s, want v1 → v2's one change to basics", body(t, c))
	}

	if c := decode[gen.Comparison](t, h.request(t, http.MethodGet, acmeFactory+"/compare", "", nil), http.StatusOK); c.From.Selector != "base" ||
		c.To.Selector != "draft" || valueOf(c.To.State) != gen.VersionStateDraft || c.Counts.Changes != 0 {
		t.Errorf("default compare %s, want the unchanged draft against its base", body(t, c))
	}

	refused(t, "compare with an unknown version", h.request(t, http.MethodGet, acmeFactory+"/compare?from=7", "", nil), http.StatusNotFound,
		authoring.CodeVersionNotFound)
}

func TestDefinitions_Restore(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v2 := h.renamed(t)
	restore := func(version, expected string) response {
		return h.request(t, http.MethodPost, acmeFactory+"/versions/"+version+"/restore", body(t, map[string]string{expectedKey: expected}), nil)
	}

	if restored := decode[gen.Factory](t, restore("1", v2.Id), http.StatusOK); restored.Current.Number != 1 || valueOf(restored.Title) != acmeTitle {
		t.Errorf("restored %s, want v1 running", body(t, restored))
	}

	refused(t, "restore on a stale current", restore("2", v2.Id), http.StatusConflict, authoring.CodeBaseMoved)
	refused(t, "restore of an unknown version", restore("9", h.current(t)), http.StatusNotFound, authoring.CodeVersionNotFound)
}

func TestDefinitions_StreamSendsDefinitionEvents(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, _, events := h.stream(t, "?factory=acme")
	events.next(t) // retry

	// line's draft is another factory's: the stream of acme leaves it out.
	if r := h.request(t, http.MethodPost, factoriesPath+"/line/draft", `{}`, nil); r.status != http.StatusCreated {
		t.Fatalf("open line's draft = %d", r.status)
	}

	d := h.openDraft(t)

	got := events.next(t)
	if len(got) != 2 || got[0] != "event: definition" || !strings.HasPrefix(got[1], "data: ") {
		t.Fatalf("event %q, want a definition event", got)
	}

	var e gen.DefinitionEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[1], "data: ")), &e); err != nil {
		t.Fatal(err)
	}

	if e.OrgId != string(testOrg) || e.FactoryId != acmeID || e.Kind != gen.DefinitionEventKindDraft || valueOf(e.VersionId) != d.Id ||
		valueOf(e.Revision) != 1 || valueOf(e.By) != "user:local" {
		t.Errorf("definition event %+v, want acme's draft at revision 1 by the local user", e)
	}
}

// linePath is the line factory's address; line (build has two agent slots) is the one archived.
const linePath = factoriesPath + "/line"

// pauseLine pauses the line factory, as archiving requires.
func (h *harness) pauseLine(t *testing.T) gen.Factory {
	t.Helper()

	return decode[gen.Factory](t, h.request(t, http.MethodPost, linePath+"/pause", "", nil), http.StatusOK)
}

func TestDefinitions_ArchiveAndRestore(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.pauseLine(t)

	archived := decode[gen.Factory](t, h.request(t, http.MethodPost, linePath+"/archive", "", nil), http.StatusOK)
	if archived.ArchivedAt == nil || archived.ArchivedBy == nil || archived.ArchivedBy.Id != "local" || !archived.Paused {
		t.Errorf("archive = %s, want line archived by the local user, still paused", body(t, archived))
	}

	restored := decode[gen.Factory](t, h.request(t, http.MethodPost, linePath+"/unarchive", "", nil), http.StatusOK)
	if restored.ArchivedAt != nil || restored.ArchivedBy != nil || !restored.Paused {
		t.Errorf("restore = %s, want line not archived and still paused", body(t, restored))
	}

	for _, op := range []string{"/archive", "/unarchive"} {
		refused(t, op+" of an unknown factory", h.request(t, http.MethodPost, factoriesPath+"/nope"+op, "", nil),
			http.StatusNotFound, authoring.CodeFactoryNotFound)
	}
}

func TestDefinitions_ArchiveIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	paused := h.pauseLine(t)

	if same := decode[gen.Factory](t, h.request(t, http.MethodPost, linePath+"/unarchive", "", nil), http.StatusOK); same.ArchivedAt != nil ||
		!same.Paused || same.Id != paused.Id {
		t.Errorf("restore of a live factory = %s, want it unchanged", body(t, same))
	}

	first := decode[gen.Factory](t, h.request(t, http.MethodPost, linePath+"/archive", "", nil), http.StatusOK)

	h.clock.Advance(time.Minute)

	again := decode[gen.Factory](t, h.request(t, http.MethodPost, linePath+"/archive", "", nil), http.StatusOK)
	if again.ArchivedAt == nil || !again.ArchivedAt.Equal(*first.ArchivedAt) {
		t.Errorf("archive again = %s, want it still archived since %v", body(t, again), first.ArchivedAt)
	}
}

func TestDefinitions_ArchiveRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	p := refused(t, "archive of a running factory", h.request(t, http.MethodPost, linePath+"/archive", "", nil),
		http.StatusConflict, authoring.CodeFactoryRunning)
	if valueOf(p.Detail) != "Pause the factory first" {
		t.Errorf("detail %q, want \"Pause the factory first\"", valueOf(p.Detail))
	}

	h.pauseLine(t)

	for slot := range 2 {
		h.seed(t, stBuild, domain.StateQueued, t0)

		if _, _, err := h.store.ClaimNext(inOrg(t), ports.Claim{
			FactoryID: lineID, Station: build, Slot: slot, Now: t0, ExpiresAt: t0.Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	p = refused(t, "archive while 2 items hold leases", h.request(t, http.MethodPost, linePath+"/archive", "", nil),
		http.StatusConflict, authoring.CodeFactoryBusy)
	if valueOf(p.Detail) != "2 items are being worked" {
		t.Errorf("detail %q, want \"2 items are being worked\"", valueOf(p.Detail))
	}

	if got := decode[gen.Factory](t, h.request(t, http.MethodGet, linePath, "", nil), http.StatusOK); got.ArchivedAt != nil {
		t.Errorf("line after refused archives = %s, want it not archived", body(t, got))
	}
}

func TestDefinitions_ListFactoriesHidesArchived(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.pauseLine(t)
	decode[gen.Factory](t, h.request(t, http.MethodPost, linePath+"/archive", "", nil), http.StatusOK)

	for query, want := range map[string][]string{
		"": {acmeSlug}, "?archived=false": {acmeSlug}, "?archived=true": {"line"},
	} {
		list := decode[gen.FactoryList](t, h.request(t, http.MethodGet, factoriesPath+query, "", nil), http.StatusOK)

		var got []string
		for _, f := range list.Factories {
			got = append(got, f.Name)
		}

		if !slices.Equal(got, want) {
			t.Errorf("factories%s = %v, want %v", query, got, want)
		}
	}
}
