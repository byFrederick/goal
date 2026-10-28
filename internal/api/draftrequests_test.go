package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
)

const acmeDraftRequests = "/v1/orgs/acme/factories/acme/draft/requests"

// An editor's request is queued and reads back, alone and in the list, newest first; the next
// one waits for it to end.
func TestAPI_DraftRequests(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var first gen.DraftRequest
	if code := h.call(t, http.MethodPost, acmeDraftRequests, `{"text":"  give review a 4th reviewer  "}`, &first); code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}

	if first.Id == "" || first.Text != "give review a 4th reviewer" || first.Agent != "" || first.Status != gen.DraftRequestStatusQueued ||
		first.Requester.Id != domain.LocalUser.ID || !first.CreatedAt.Equal(t0) || first.StartedAt != nil || first.ChangedPaths == nil {
		t.Errorf("created %+v, want the trimmed text queued by the local user at t0", first)
	}

	var got gen.DraftRequest
	if code := h.call(t, http.MethodGet, acmeDraftRequests+"/"+first.Id, "", &got); code != http.StatusOK || got.Id != first.Id || got.Status != gen.DraftRequestStatusQueued {
		t.Errorf("get = %d %+v, want the queued request", code, got)
	}

	// The request ends, as the runner will finish it, so the factory takes the next one.
	h.finishDraftRequests(t, acmeID)

	var second gen.DraftRequest
	if code := h.call(t, http.MethodPost, acmeDraftRequests, `{"text":"fix the writer","agent":"writer"}`, &second); code != http.StatusCreated || second.Agent != "writer" {
		t.Fatalf("create for writer = %d %+v, want 201", code, second)
	}

	var list gen.DraftRequestList
	if code := h.call(t, http.MethodGet, acmeDraftRequests, "", &list); code != http.StatusOK || len(list.Requests) != 2 ||
		list.Requests[0].Id != second.Id || list.Requests[1].Id != first.Id || list.Requests[1].Status != gen.DraftRequestStatusDone {
		t.Errorf("list = %d %+v, want the two requests newest first", code, list)
	}

	if code := h.call(t, http.MethodGet, acmeDraftRequests+"?limit=1", "", &list); code != http.StatusOK || len(list.Requests) != 1 || list.Requests[0].Id != second.Id {
		t.Errorf("list limit=1 = %d %+v, want the newest", code, list)
	}

	// Another factory has neither request.
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/draft/requests/"+first.Id, "", nil); code != http.StatusNotFound {
		t.Errorf("get through line = %d, want 404", code)
	}
}

// An agent counts when the open draft has it, even before it is published.
func TestAPI_DraftRequestAgentOfTheDraft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putDraftYAML(t, strings.Replace(acme, "- { name: writer, prompt: ./agents/writer.md }",
		"- { name: writer, prompt: ./agents/writer.md }\n    - { name: editor, prompt: ./agents/writer.md }", 1))

	var r gen.DraftRequest
	if code := h.call(t, http.MethodPost, acmeDraftRequests, `{"text":"tighten the editor","agent":"editor"}`, &r); code != http.StatusCreated || r.Agent != "editor" {
		t.Errorf("create for the draft's editor = %d %+v, want 201", code, r)
	}
}

func TestAPI_DraftRequestRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, c := range []struct{ what, body, code string }{
		{"empty text", `{"text":"   "}`, "draft-request-text-invalid"},
		{"2001 characters", `{"text":"` + strings.Repeat("é", domain.MaxDraftRequestText+1) + `"}`, "draft-request-text-invalid"},
	} {
		code, p := evalCall(t, h, http.MethodPost, acmeDraftRequests, c.body, nil)
		wantEvalProblem(t, c.what, code, p, http.StatusUnprocessableEntity, c.code)
	}

	code, p := evalCall(t, h, http.MethodPost, acmeDraftRequests, `{"text":"fix it","agent":"nobody"}`, nil)
	wantEvalProblem(t, "unknown agent", code, p, http.StatusNotFound, "agent-not-found")

	if code := h.call(t, http.MethodPost, acmeDraftRequests, `{"text":"exactly 2000 `+strings.Repeat("x", domain.MaxDraftRequestText-13)+`"}`, nil); code != http.StatusCreated {
		t.Fatalf("create of 2000 characters = %d, want 201", code)
	}

	code, p = evalCall(t, h, http.MethodPost, acmeDraftRequests, `{"text":"and another"}`, nil)
	wantEvalProblem(t, "second while one is queued", code, p, http.StatusConflict, "draft-request-open")

	code, p = evalCall(t, h, http.MethodGet, acmeDraftRequests+"/01900000-0000-7000-8000-999999999999", "", nil)
	wantEvalProblem(t, "unknown request", code, p, http.StatusNotFound, "draft-request-not-found")

	code, p = evalCall(t, h, http.MethodGet, acmeDraftRequests+"?limit=101", "", nil)
	if code != http.StatusBadRequest {
		t.Errorf("list limit=101 = %d %+v, want 400", code, p)
	}
}

// A draft whose factory.yaml does not parse cannot say which agents it has.
func TestAPI_DraftRequestRefusesAnAgentOfABrokenDraft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putDraftYAML(t, "spec: [\n")

	code, p := evalCall(t, h, http.MethodPost, acmeDraftRequests, `{"text":"fix it","agent":"writer"}`, nil)
	wantEvalProblem(t, "agent of a broken draft", code, p, http.StatusConflict, "yaml-invalid")

	// Without an agent the request does not read the draft.
	if code := h.call(t, http.MethodPost, acmeDraftRequests, `{"text":"fix the YAML"}`, nil); code != http.StatusCreated {
		t.Errorf("create without an agent = %d, want 201", code)
	}
}

func TestAPI_DraftRequestViewerMayReadButNotAsk(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var r gen.DraftRequest
	if code := h.call(t, http.MethodPost, acmeDraftRequests, `{"text":"give review a 4th reviewer"}`, &r); code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}

	h.evalsAsOn(acmeID, domain.FactoryViewer)

	for _, path := range []string{acmeDraftRequests, acmeDraftRequests + "/" + r.Id} {
		if code := h.call(t, http.MethodGet, path, "", nil); code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", path, code)
		}
	}

	code, p := evalCall(t, h, http.MethodPost, acmeDraftRequests, `{"text":"another"}`, nil)
	wantEvalProblem(t, "viewer create", code, p, http.StatusForbidden, "forbidden")
}

// putDraftYAML opens acme's draft and writes doc as its factory.yaml.
func (h *harness) putDraftYAML(t *testing.T, doc string) {
	t.Helper()

	svc, ctx := h.authors(t)

	if _, _, err := svc.OpenDraft(ctx, acmeID, authoring.OpenDraftRequest{}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.PutFile(ctx, acmeID, authoring.FileWrite{Path: yamlFile, Content: []byte(doc)}); err != nil {
		t.Fatal(err)
	}
}

// finishDraftRequests ends factory's open request done, as the runner does.
func (h *harness) finishDraftRequests(t *testing.T, factory domain.FactoryID) {
	t.Helper()

	c, err := h.drafts.Claim(t.Context(), t0, t0.Add(time.Minute))
	if err != nil || c == nil || c.Request.Factory != factory {
		t.Fatalf("claim = %+v, %v; want %s's request", c, err, factory)
	}

	if err := h.drafts.Finish(domain.WithOrg(t.Context(), c.Org), c.Lease, domain.DraftRequestResult{
		Status: domain.DraftRequestDone, Summary: "added a reviewer", ChangedPaths: []string{yamlFile}, At: t0,
	}); err != nil {
		t.Fatal(err)
	}
}
