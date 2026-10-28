package api_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The routes the saved views tests call and the views most of them save.
const (
	viewsPath    = "/v1/orgs/acme/factories/acme/views"
	sharedBugs   = `{"name":"Expedite bugs","shared":true,"filters":{"type":["bug"],"class":["expedite"]}}`
	ownerOwn     = `{"name":"Owner's","shared":false,"filters":{"station":["review"]}}`
	viewerOwn    = `{"name":"Mine","shared":false,"filters":{"status":["open","waiting"]}}`
	renamedView  = `{"name":"Renamed","filters":{"type":["fix"]}}`
	codeViewGone = "view-not-found"
	codeTaken    = "view-name-taken"
	codeInvalid  = "invalid-view"
	codeRefused  = "forbidden"
	codeNoPerson = "person-required"
	problemType  = "application/problem+json"
)

// saveView saves body as the person of session and returns the view.
func (h signedIn) saveView(t *testing.T, session, body string) gen.SavedView {
	t.Helper()

	return decode[gen.SavedView](t, h.as(t, session, http.MethodPost, viewsPath, body), http.StatusCreated)
}

// listViews lists acme's views as the caller of header.
func (h signedIn) listViews(t *testing.T, header http.Header) []gen.SavedView {
	t.Helper()

	return decode[gen.SavedViewList](t, h.request(t, http.MethodGet, viewsPath, "", header), http.StatusOK).Views
}

// viewNames are views' names, each with "*" after it when it is the caller's.
func viewNames(views []gen.SavedView) []string {
	out := make([]string, 0, len(views))

	for _, v := range views {
		name := v.Name
		if v.Mine {
			name += "*"
		}

		out = append(out, name)
	}

	return out
}

// wantStatus checks res's status and, when code is set, its problem's code.
func wantStatus(t *testing.T, what string, res response, status int, code string) {
	t.Helper()

	if res.status != status {
		t.Errorf("%s = %d %s, want %d %s", what, res.status, res.body, status, code)
		return
	}

	if code != "" {
		if got := deref(problem(t, res).Code); got != code || res.header.Get(contentType) != problemType {
			t.Errorf("%s = %q (%s), want problem %q", what, got, res.header.Get(contentType), code)
		}
	}
}

// List answers the factory's shared views and the caller's own personal ones, never another
// person's; mine marks only the caller's own, and an API key sees only the shared views.
func TestSavedViews_ListSharedAndOwn(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	shared := h.saveView(t, ownerToken, sharedBugs)
	h.saveView(t, ownerToken, ownerOwn)
	h.saveView(t, viewerToken, viewerOwn)

	if !shared.Shared || !shared.Mine || shared.Id == "" || !slices.Equal(shared.Filters["class"], []string{"expedite"}) ||
		!shared.CreatedAt.Equal(t0) || !shared.UpdatedAt.Equal(t0) {
		t.Errorf("created = %+v; want the shared view, the owner's, at t0", shared)
	}

	key, _ := h.create(t, viewsKey)

	for _, c := range []struct {
		who    string
		header http.Header
		want   []string
	}{
		{"viewer", cookie(viewerToken), []string{"Expedite bugs", "Mine*"}},
		{roleOwner, cookie(ownerToken), []string{"Expedite bugs*", "Owner's*"}},
		{"API key", bearer(key.Secret), []string{"Expedite bugs"}},
	} {
		views := h.listViews(t, c.header)
		if got := viewNames(views); !slices.Equal(got, c.want) {
			t.Errorf("%s lists %v, want %v", c.who, got, c.want)
		}
	}

	mine := h.listViews(t, cookie(viewerToken))[1]
	if !slices.Equal(mine.Filters["status"], []string{"open", "waiting"}) || mine.Shared {
		t.Errorf("viewer's view = %+v; want personal with its filters", mine)
	}
}

// A viewer manages their own personal views but no shared one: saving, renaming or deleting a
// shared view needs factory:manage.
func TestSavedViews_ViewerCannotWriteShared(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	wantStatus(t, "viewer saves a shared view", h.as(t, viewerToken, http.MethodPost, viewsPath, sharedBugs), http.StatusForbidden, codeRefused)

	shared := h.saveView(t, ownerToken, sharedBugs)
	wantStatus(t, "viewer renames the shared view", h.as(t, viewerToken, http.MethodPatch, viewsPath+"/"+shared.Id, renamedView),
		http.StatusForbidden, codeRefused)
	wantStatus(t, "viewer deletes the shared view", h.as(t, viewerToken, http.MethodDelete, viewsPath+"/"+shared.Id, ""),
		http.StatusForbidden, codeRefused)

	own := h.saveView(t, viewerToken, viewerOwn)
	h.clock.Advance(time.Minute)

	renamed := decode[gen.SavedView](t, h.as(t, viewerToken, http.MethodPatch, viewsPath+"/"+own.Id, renamedView), http.StatusOK)
	if renamed.Id != own.Id || renamed.Name != "Renamed" || !renamed.Mine || renamed.Shared || len(renamed.Filters) != 1 ||
		!slices.Equal(renamed.Filters["type"], []string{"fix"}) || !renamed.UpdatedAt.After(renamed.CreatedAt) {
		t.Errorf("renamed = %+v; want the viewer's view renamed with new filters", renamed)
	}

	wantStatus(t, "viewer deletes their own view", h.as(t, viewerToken, http.MethodDelete, viewsPath+"/"+own.Id, ""), http.StatusNoContent, "")

	if got := viewNames(h.listViews(t, cookie(viewerToken))); !slices.Equal(got, []string{"Expedite bugs"}) {
		t.Errorf("viewer lists %v after the refusals and the delete, want the shared view unchanged", got)
	}
}

// viewsKey is a key of acme that may manage the factory, and still cannot write a view.
const viewsKey = `{"name":"views","scopes":["org:read","factory:read","factory:manage"],"role_ceiling":"editor","factories":["acme"],"expires_in_days":30}`

// Only a signed-in person owns a view: every write from an API key or a personal token is
// refused, even with factory:manage, and changes nothing.
func TestSavedViews_KeysCannotWrite(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	shared := h.saveView(t, ownerToken, sharedBugs)
	key, _ := h.create(t, viewsKey)
	token, _ := h.createToken(t, freshToken, `{"name":"laptop","scopes":["org:read","factory:read"]}`)

	for who, header := range map[string]http.Header{"API key": bearer(key.Secret), "personal token": bearer(token.Secret)} {
		for _, body := range []string{sharedBugs, viewerOwn} {
			wantStatus(t, who+" saves "+body, h.request(t, http.MethodPost, viewsPath, body, header), http.StatusForbidden, codeNoPerson)
		}

		wantStatus(t, who+" renames", h.request(t, http.MethodPatch, viewsPath+"/"+shared.Id, renamedView, header), http.StatusForbidden, codeNoPerson)
		wantStatus(t, who+" deletes", h.request(t, http.MethodDelete, viewsPath+"/"+shared.Id, "", header), http.StatusForbidden, codeNoPerson)
	}

	if got := viewNames(h.listViews(t, cookie(ownerToken))); !slices.Equal(got, []string{"Expedite bugs*"}) {
		t.Errorf("owner lists %v, want only the shared view, unchanged", got)
	}
}

// Another person's personal view, and a view the factory does not have, answer as unknown, even
// to the organization's owner.
func TestSavedViews_OthersPersonalIsNotFound(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	viewers := h.saveView(t, viewerToken, viewerOwn)
	owners := h.saveView(t, ownerToken, ownerOwn)

	for _, c := range []struct{ who, session, id string }{
		{"viewer", viewerToken, owners.Id},
		{"owner", ownerToken, viewers.Id},
		{"owner", ownerToken, "00000000-0000-7000-8000-fffffffffffe"},
	} {
		path := viewsPath + "/" + c.id
		wantStatus(t, c.who+" renames "+c.id, h.as(t, c.session, http.MethodPatch, path, renamedView), http.StatusNotFound, codeViewGone)
		wantStatus(t, c.who+" deletes "+c.id, h.as(t, c.session, http.MethodDelete, path, ""), http.StatusNotFound, codeViewGone)
	}

	if got := viewNames(h.listViews(t, cookie(viewerToken))); !slices.Equal(got, []string{"Mine*"}) {
		t.Errorf("viewer lists %v, want their view unchanged", got)
	}
}

// A name the owner's personal views, or the factory's shared views, already use (case ignored)
// answers 409 view-name-taken, on save and on rename.
func TestSavedViews_NameTaken(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	h.saveView(t, viewerToken, viewerOwn)
	h.saveView(t, ownerToken, sharedBugs)
	other := h.saveView(t, viewerToken, `{"name":"Other","shared":false,"filters":{}}`)

	wantStatus(t, "a second personal Mine", h.as(t, viewerToken, http.MethodPost, viewsPath, strings.Replace(viewerOwn, "Mine", "MINE", 1)),
		http.StatusConflict, codeTaken)
	wantStatus(t, "a second shared view", h.as(t, ownerToken, http.MethodPost, viewsPath, sharedBugs), http.StatusConflict, codeTaken)
	wantStatus(t, "renamed to Mine", h.as(t, viewerToken, http.MethodPatch, viewsPath+"/"+other.Id, `{"name":"mine","filters":{}}`),
		http.StatusConflict, codeTaken)

	// Another person's personal view of the same name is no clash.
	h.saveView(t, ownerToken, viewerOwn)
}

// A view outside the limits answers 400 invalid-view, on save and on rename.
func TestSavedViews_Invalid(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	own := h.saveView(t, viewerToken, viewerOwn)

	manyKeys := make([]string, 0, 21)
	for i := range 21 {
		manyKeys = append(manyKeys, `"k`+string(rune('a'+i))+`":[]`)
	}

	for name, body := range map[string]string{
		"a blank name":     `{"name":"  ","shared":false,"filters":{}}`,
		"a 61-letter name": `{"name":"` + strings.Repeat("a", 61) + `","shared":false,"filters":{}}`,
		"21 filters":       `{"name":"wide","shared":false,"filters":{` + strings.Join(manyKeys, ",") + `}}`,
		"a long value":     `{"name":"long","shared":false,"filters":{"q":["` + strings.Repeat("a", 201) + `"]}}`,
	} {
		wantStatus(t, "save with "+name, h.as(t, viewerToken, http.MethodPost, viewsPath, body), http.StatusBadRequest, codeInvalid)
	}

	wantStatus(t, "rename to blank", h.as(t, viewerToken, http.MethodPatch, viewsPath+"/"+own.Id, `{"name":"","filters":{}}`),
		http.StatusBadRequest, codeInvalid)
}

// Under GOAL_AUTH=none nobody signs in, so nobody owns a view: the local user lists the shared
// views (none) and cannot save one.
func TestSavedViews_NoSignInCannotWrite(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var list gen.SavedViewList
	if code := h.call(t, http.MethodGet, viewsPath, "", &list); code != http.StatusOK || len(list.Views) != 0 {
		t.Errorf("list = %d %+v, want 200 and no views", code, list)
	}

	code, p := problemOfCall(t, h, http.MethodPost, viewsPath, viewerOwn)
	wantProblem(t, "save without a sign-in", code, p, http.StatusForbidden, codeNoPerson)
}
