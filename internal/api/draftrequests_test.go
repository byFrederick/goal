package api_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
)

const acmeDraftRequests = "/v1/orgs/acme/factories/acme/draft/requests"

// Asking is refused until a runner is wired: 501 not-built, and nothing is queued.
func TestAPI_DraftRequestNotBuilt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	code, p := callWithProblem(t, h, http.MethodPost, acmeDraftRequests, `{"text":"give review a 4th reviewer"}`, nil)
	wantProblem(t, "create", code, p, http.StatusNotImplemented, "not-built")

	if p.Detail == nil || !strings.Contains(*p.Detail, "not built yet") {
		t.Errorf("detail = %v, want that draft requests are not built yet", p.Detail)
	}

	wantDraftRequestList(t, h, "")
}

// A request seeded in the store reads back, alone and in the list, newest first.
func TestAPI_DraftRequests(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	firstID := h.seedDraftRequest(t, acmeID, "give review a 4th reviewer", "")

	var got gen.DraftRequest
	if code := h.call(t, http.MethodGet, acmeDraftRequests+"/"+firstID, "", &got); code != http.StatusOK || got.Id != firstID {
		t.Errorf("get = %d %+v, want the queued request", code, got)
	}

	wantQueuedByLocalUser(t, got, "give review a 4th reviewer")

	h.finishDraftRequests(t, acmeID)

	secondID := h.seedDraftRequest(t, acmeID, "fix the writer", "writer")

	list := wantDraftRequestList(t, h, "", secondID, firstID)
	if list[0].Agent != "writer" || list[1].Status != gen.DraftRequestStatusDone {
		t.Errorf("list = %+v, want the writer's request, then the first one done", list)
	}

	wantDraftRequestList(t, h, "?limit=1", secondID)
}

// wantDraftRequestList lists acme's requests with query and checks that they are ids, in order.
func wantDraftRequestList(t *testing.T, h *harness, query string, ids ...string) []gen.DraftRequest {
	t.Helper()

	var list gen.DraftRequestList
	if code := h.call(t, http.MethodGet, acmeDraftRequests+query, "", &list); code != http.StatusOK {
		t.Fatalf("list%s = %d, want 200", query, code)
	}

	got := make([]string, 0, len(list.Requests))
	for _, r := range list.Requests {
		got = append(got, r.Id)
	}

	if !slices.Equal(got, ids) {
		t.Fatalf("list%s = %v, want %v", query, got, ids)
	}

	return list.Requests
}

// Another factory of the organization has none of acme's requests.
func TestAPI_DraftRequestOfAnotherFactory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	id := h.seedDraftRequest(t, acmeID, "give review a 4th reviewer", "")

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/draft/requests/"+id, "", nil); code != http.StatusNotFound {
		t.Errorf("get through line = %d, want 404", code)
	}
}

// wantQueuedByLocalUser checks that r is a new request of text, queued by the local user at t0.
func wantQueuedByLocalUser(t *testing.T, r gen.DraftRequest, text string) {
	t.Helper()

	if r.Id == "" || r.Text != text || r.Agent != "" || r.Status != gen.DraftRequestStatusQueued ||
		r.Requester.Id != domain.LocalUser.ID || !r.CreatedAt.Equal(t0) || r.StartedAt != nil || r.ChangedPaths == nil {
		t.Errorf("created %+v, want %q queued by the local user at t0", r, text)
	}
}

// An agent counts when the open draft has it, even before it is published: the request passes
// the agent check and reaches the not-built refusal.
func TestAPI_DraftRequestAgentOfTheDraft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putDraftYAML(t, strings.Replace(acme, "- { name: writer, prompt: ./agents/writer.md }",
		"- { name: writer, prompt: ./agents/writer.md }\n    - { name: editor, prompt: ./agents/writer.md }", 1))

	code, p := callWithProblem(t, h, http.MethodPost, acmeDraftRequests, `{"text":"tighten the editor","agent":"editor"}`, nil)
	wantProblem(t, "create for the draft's editor", code, p, http.StatusNotImplemented, "not-built")
}

func TestAPI_DraftRequestRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, c := range []struct{ what, body, code string }{
		{"empty text", `{"text":"   "}`, "draft-request-text-invalid"},
		{"2001 characters", `{"text":"` + strings.Repeat("é", domain.MaxDraftRequestText+1) + `"}`, "draft-request-text-invalid"},
	} {
		code, p := callWithProblem(t, h, http.MethodPost, acmeDraftRequests, c.body, nil)
		wantProblem(t, c.what, code, p, http.StatusUnprocessableEntity, c.code)
	}

	code, p := callWithProblem(t, h, http.MethodPost, acmeDraftRequests, `{"text":"fix it","agent":"nobody"}`, nil)
	wantProblem(t, "unknown agent", code, p, http.StatusNotFound, "agent-not-found")

	code, p = callWithProblem(t, h, http.MethodPost, acmeDraftRequests, `{"text":"exactly 2000 `+strings.Repeat("x", domain.MaxDraftRequestText-13)+`"}`, nil)
	wantProblem(t, "2000 characters", code, p, http.StatusNotImplemented, "not-built")

	code, p = callWithProblem(t, h, http.MethodGet, acmeDraftRequests+"/01900000-0000-7000-8000-999999999999", "", nil)
	wantProblem(t, "unknown request", code, p, http.StatusNotFound, "draft-request-not-found")

	code, p = callWithProblem(t, h, http.MethodGet, acmeDraftRequests+"?limit=101", "", nil)
	if code != http.StatusBadRequest {
		t.Errorf("list limit=101 = %d %+v, want 400", code, p)
	}
}

// A draft whose factory.yaml does not parse cannot say which agents it has.
func TestAPI_DraftRequestRefusesAnAgentOfABrokenDraft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putDraftYAML(t, "spec: [\n")

	code, p := callWithProblem(t, h, http.MethodPost, acmeDraftRequests, `{"text":"fix it","agent":"writer"}`, nil)
	wantProblem(t, "agent of a broken draft", code, p, http.StatusConflict, "yaml-invalid")

	// Without an agent the request does not read the draft.
	code, p = callWithProblem(t, h, http.MethodPost, acmeDraftRequests, `{"text":"fix the YAML"}`, nil)
	wantProblem(t, "create without an agent", code, p, http.StatusNotImplemented, "not-built")
}

func TestAPI_DraftRequestViewerMayReadButNotAsk(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	id := h.seedDraftRequest(t, acmeID, "give review a 4th reviewer", "")

	h.as(acmeMember(domain.FactoryViewer))

	for _, path := range []string{acmeDraftRequests, acmeDraftRequests + "/" + id} {
		if code := h.call(t, http.MethodGet, path, "", nil); code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", path, code)
		}
	}

	code, p := callWithProblem(t, h, http.MethodPost, acmeDraftRequests, `{"text":"another"}`, nil)
	wantProblem(t, "viewer create", code, p, http.StatusForbidden, "forbidden")
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

// seedDraftRequest queues a request of the local user on factory straight in the store, as the
// route will once a runner is wired, and returns its id.
func (h *harness) seedDraftRequest(t *testing.T, factory domain.FactoryID, text, agent string) string {
	t.Helper()

	r, err := h.drafts.Create(domain.WithOrg(t.Context(), testOrg), domain.NewDraftRequest{
		Factory: factory, Requester: domain.LocalUser, Text: text, Agent: agent,
	})
	if err != nil {
		t.Fatal(err)
	}

	return string(r.ID)
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
