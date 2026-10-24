package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

const (
	adminsPath = "/v1/platform/admins"
	eventsPath = "/v1/platform/auth-events"
	// freshToken is F. Cruz (acme's owner, a platform admin) signed in just now.
	freshToken = "FreshFreshFreshFreshFreshFresh04"
)

// newPlatformAdmins is the signed-in harness with F. Cruz also signed in just now, as managing
// platform admins needs.
func newPlatformAdmins(t *testing.T) signedIn {
	t.Helper()

	h := newSignedIn(t)
	h.identity.AddSession(freshToken, ownerID, domain.Session{
		ID: "019a4000-0000-7000-8000-0000000000b9", CreatedAt: h.clock.Now(), ExpiresAt: h.clock.Now().Add(time.Hour),
	})

	return h
}

func decodeAs[T any](t *testing.T, res response) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(res.body, &out); err != nil {
		t.Fatalf("decode %s: %v", res.body, err)
	}

	return out
}

// admins lists the platform admins as F. Cruz sees them.
func (h signedIn) admins(t *testing.T) []gen.PlatformAdmin {
	t.Helper()

	res := h.as(t, ownerToken, http.MethodGet, adminsPath, "")
	if res.status != http.StatusOK {
		t.Fatalf("list = %d %s", res.status, res.body)
	}

	return decodeAs[gen.PlatformAdminList](t, res).Admins
}

// adminIDs are the ids of admins, in order.
func adminIDs(admins []gen.PlatformAdmin) []string {
	out := make([]string, 0, len(admins))
	for _, a := range admins {
		out = append(out, a.User.Id)
	}

	return out
}

// Spec 3 §8.9 and §15 row 3-15: a platform admin lists platform admins and adds one by the email
// they sign in with; someone who never signed in is 404.
func TestAPI_PlatformAdmins_Add(t *testing.T) {
	t.Parallel()

	h := newPlatformAdmins(t)

	if got := h.admins(t); len(got) != 1 || got[0].User != (gen.User{Id: string(ownerID), Name: "F. Cruz", Email: "1@example.com"}) {
		t.Errorf("admins = %+v; want F. Cruz alone", got)
	}

	res := h.as(t, freshToken, http.MethodPost, adminsPath, `{"email":" 2@EXAMPLE.com "}`)
	if res.status != http.StatusCreated {
		t.Fatalf("add = %d %s", res.status, res.body)
	}

	added := decodeAs[gen.PlatformAdmin](t, res)
	if added.User.Id != string(viewerID) || added.AddedBy != (gen.Actor{Kind: gen.ActorKindUser, Id: string(ownerID), Name: "F. Cruz"}) {
		t.Errorf("added = %+v; want M. Okafor, added by F. Cruz", added)
	}

	if got := adminIDs(h.admins(t)); !slices.Equal(got, []string{string(ownerID), string(viewerID)}) {
		t.Errorf("admins = %v; want F. Cruz, then M. Okafor", got)
	}

	if res := h.as(t, freshToken, http.MethodPost, adminsPath, `{"email":"nobody@example.com"}`); res.status != http.StatusNotFound {
		t.Errorf("adding an unknown email = %d %s; want 404", res.status, res.body)
	}
}

// Removing a platform admin is in the security log; a non-admin is 404 and the install keeps its
// last admin (409 last-platform-admin).
func TestAPI_PlatformAdmins_Remove(t *testing.T) {
	t.Parallel()

	h := newPlatformAdmins(t)
	h.as(t, freshToken, http.MethodPost, adminsPath, `{"email":"2@example.com"}`)

	for _, c := range []struct {
		user domain.UserID
		want int
		code string
	}{
		{user: viewerID, want: http.StatusNoContent},
		{user: viewerID, want: http.StatusNotFound},
		{user: ownerID, want: http.StatusConflict, code: "last-platform-admin"},
	} {
		res := h.as(t, freshToken, http.MethodDelete, adminsPath+"/"+string(c.user), "")
		if res.status != c.want || (c.code != "" && deref(problem(t, res).Code) != c.code) {
			t.Errorf("remove %s = %d %s; want %d %s", c.user, res.status, res.body, c.want, c.code)
		}
	}

	if got := adminIDs(h.admins(t)); !slices.Equal(got, []string{string(ownerID)}) {
		t.Errorf("admins = %v; want F. Cruz kept", got)
	}

	page := decodeAs[gen.AuthEventPage](t, h.as(t, ownerToken, http.MethodGet, eventsPath, ""))
	kinds := []string{}

	for _, e := range page.Events {
		if deref(e.UserId) == string(viewerID) && e.Data["by"] == "user:"+string(ownerID) {
			kinds = append(kinds, e.Kind)
		}
	}

	if !slices.Equal(kinds, []string{"platform_admin.removed", "platform_admin.added"}) || page.NextBefore != nil {
		t.Errorf("security log = %+v; want the removal, then the addition, by F. Cruz", page)
	}
}

// Platform routes are platform admins' only, and changes need a recent sign-in (spec 3 §8.4):
// a member is 403, a platform admin's older session reads but is 403 reauth-required on writes.
func TestAPI_PlatformAdmins_OnlyFreshPlatformAdminsChange(t *testing.T) {
	t.Parallel()

	h := newPlatformAdmins(t)

	for _, path := range []string{adminsPath, eventsPath} {
		if res := h.as(t, viewerToken, http.MethodGet, path, ""); res.status != http.StatusForbidden {
			t.Errorf("a member reads %s = %d %s; want 403", path, res.status, res.body)
		}

		if res := h.as(t, ownerToken, http.MethodGet, path, ""); res.status != http.StatusOK {
			t.Errorf("a platform admin's older session reads %s = %d %s; want 200", path, res.status, res.body)
		}
	}

	for _, req := range []struct{ method, path, body string }{
		{http.MethodPost, adminsPath, `{"email":"2@example.com"}`},
		{http.MethodDelete, adminsPath + "/" + string(ownerID), ""},
	} {
		res := h.as(t, ownerToken, req.method, req.path, req.body)
		if p := problem(t, res); res.status != http.StatusForbidden || deref(p.Code) != "reauth-required" {
			t.Errorf("%s %s with an older session = %d %s; want 403 reauth-required", req.method, req.path, res.status, res.body)
		}

		if res := h.as(t, viewerToken, req.method, req.path, req.body); res.status != http.StatusForbidden {
			t.Errorf("%s %s by a member = %d %s; want 403", req.method, req.path, res.status, res.body)
		}
	}

	if got := decodeAs[gen.PlatformAdminList](t, h.as(t, ownerToken, http.MethodGet, adminsPath, "")); len(got.Admins) != 1 {
		t.Errorf("admins = %+v; want them unchanged", got.Admins)
	}
}

// The security log pages newest first by id, filters by kind and refuses a limit out of range.
func TestAPI_AuthEvents_Pages(t *testing.T) {
	t.Parallel()

	h := newPlatformAdmins(t)

	for range 3 {
		h.as(t, freshToken, http.MethodPost, adminsPath, `{"email":"2@example.com"}`)
		h.as(t, freshToken, http.MethodDelete, adminsPath+"/"+string(viewerID), "")
	}

	first := decodeAs[gen.AuthEventPage](t, h.as(t, ownerToken, http.MethodGet, eventsPath+"?limit=4", ""))
	if len(first.Events) != 4 || first.NextBefore == nil || *first.NextBefore != first.Events[3].Id {
		t.Fatalf("first page = %+v; want 4 events and the next page's cursor", first)
	}

	rest := decodeAs[gen.AuthEventPage](t, h.as(t, ownerToken, http.MethodGet,
		eventsPath+"?limit=4&before="+strconv.FormatInt(*first.NextBefore, 10), ""))
	if len(rest.Events) != 2 || rest.NextBefore != nil || rest.Events[0].Id >= first.Events[3].Id {
		t.Errorf("last page = %+v; want the 2 older events and no cursor", rest)
	}

	added := decodeAs[gen.AuthEventPage](t, h.as(t, ownerToken, http.MethodGet, eventsPath+"?kind=platform_admin.added", ""))
	if len(added.Events) != 3 {
		t.Errorf("additions = %+v; want 3", added.Events)
	}

	for _, limit := range []string{"0", "501"} {
		if res := h.as(t, ownerToken, http.MethodGet, eventsPath+"?limit="+limit, ""); res.status != http.StatusBadRequest {
			t.Errorf("limit %s = %d %s; want 400", limit, res.status, res.body)
		}
	}
}
