package api_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// More people of acme for the members tests: an admin and a member who is an approver of line.
const (
	adminID domain.UserID = "019a4000-0000-7000-8000-000000000004"
	nairID  domain.UserID = "019a4000-0000-7000-8000-000000000005"

	freshOwnerToken = "FreshOwnerFreshOwnerFreshOwner04"
	freshAdminToken = "FreshAdminFreshAdminFreshAdmin05"

	membersPath = acmePath + "/members"
	viewerRole  = `{"role":"viewer"}`
	codeReauth  = "reauth-required"
)

// withPeople adds acme's admin (signed in just now) and P. Nair, an approver of line, and signs
// the owner in again just now: changes to people need a recent sign-in.
func withPeople(t *testing.T, h signedIn) {
	t.Helper()

	for _, u := range []porttest.IdentityUser{{ID: adminID, Name: "A. Admin", Email: "a.admin@example.com"}, {ID: nairID, Name: "P. Nair", Email: "p.nair@example.com"}} {
		if err := h.identity.AddUser(u); err != nil {
			t.Fatal(err)
		}
	}

	must(t, h.identity.SetOrgRole(testOrg, adminID, domain.OrgAdmin))
	must(t, h.identity.SetOrgRole(testOrg, nairID, domain.OrgMember))
	must(t, h.identity.GrantFactoryRole(testOrg, nairID, lineID, domain.FactoryApprover))

	fresh := domain.Session{CreatedAt: t0, ExpiresAt: t0.Add(time.Hour), Browser: true}
	fresh.ID = "fresh-owner"
	h.identity.AddSession(freshOwnerToken, ownerID, fresh)
	fresh.ID = "fresh-admin"
	h.identity.AddSession(freshAdminToken, adminID, fresh)
}

// checkNames checks the members' names, in the order listed.
func checkNames(t *testing.T, list gen.MemberList, want ...string) {
	t.Helper()

	names := []string{}
	for _, m := range list.Members {
		names = append(names, m.User.Name)
	}

	if !slices.Equal(names, want) {
		t.Fatalf("members = %v; want %v", names, want)
	}
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func memberNamed(t *testing.T, list gen.MemberList, name string) gen.Member {
	t.Helper()

	i := slices.IndexFunc(list.Members, func(m gen.Member) bool { return m.User.Name == name })
	if i < 0 {
		t.Fatalf("%s is not among %+v", name, list.Members)
	}

	return list.Members[i]
}

// Members lists everyone of the organization by name with their roles, and says what the caller
// may change: an admin anyone but an owner, an owner the last owner neither.
func TestAPI_Members_WhatOwnersAndAdminsMayChange(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	withPeople(t, h)

	byAdmin := decode[gen.MemberList](t, h.as(t, freshAdminToken, http.MethodGet, membersPath, ""), http.StatusOK)
	checkNames(t, byAdmin, "A. Admin", ownerName, "M. Okafor", "P. Nair")

	checkAdminsView(t, byAdmin)

	if o := memberNamed(t, decode[gen.MemberList](t, h.as(t, freshOwnerToken, http.MethodGet, membersPath, ""), http.StatusOK), ownerName); o.CanChangeRole || o.CanRemove || !o.You {
		t.Errorf("the only owner as they see themself = %+v; want a role nobody may change while acme has no other owner", o)
	}
}

// checkAdminsView checks what acme's admin may change: not the owner, the members, their own role
// but not remove themself (they leave).
func checkAdminsView(t *testing.T, byAdmin gen.MemberList) {
	t.Helper()

	owner, nair, admin := memberNamed(t, byAdmin, ownerName), memberNamed(t, byAdmin, "P. Nair"), memberNamed(t, byAdmin, "A. Admin")
	if owner.Role != gen.OrgRoleOwner || owner.CanChangeRole || owner.CanRemove || owner.You {
		t.Errorf("the owner as an admin sees them = %+v; want an owner they may not change", owner)
	}

	if !nair.CanChangeRole || !nair.CanRemove || !slices.Equal(nair.FactoryRoles, []gen.FactoryRoleGrant{{FactoryId: lineID, Role: gen.Approver}}) {
		t.Errorf("Nair as an admin sees them = %+v; want an approver of line they may change and remove", nair)
	}

	if !admin.You || !admin.CanChangeRole || admin.CanRemove || admin.LastActiveAt == nil {
		t.Errorf("the admin themself = %+v; want you, a role they may change, not removable (they leave), active", admin)
	}
}

// A member may change nobody, and sees factory roles only on the factories they see.
func TestAPI_Members_ReadOnlyForAMember(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	withPeople(t, h)

	byViewer := decode[gen.MemberList](t, h.as(t, viewerToken, http.MethodGet, membersPath, ""), http.StatusOK)
	for _, m := range byViewer.Members {
		if m.CanChangeRole || m.CanRemove {
			t.Errorf("a member sees %s as changeable: %+v", m.User.Name, m)
		}
	}

	if n := memberNamed(t, byViewer, "P. Nair"); len(n.FactoryRoles) != 0 {
		t.Errorf("a viewer of acme sees Nair's roles %+v; want none on line, which they do not see", n.FactoryRoles)
	}
}

// Invitations are for owners and admins: a member is refused.
func TestAPI_Members_InvitationsForOwnersAndAdmins(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	must(t, h.identity.Invite(testOrg, domain.Invitation{
		ID: "019a4000-0000-7000-8000-0000000000e1", Email: "d.ross@example.com", Role: domain.OrgMember, Inviter: domain.User{ID: ownerID},
		FactoryRoles: []domain.FactoryGrant{{Factory: acmeID, Role: domain.FactoryViewer}}, ExpiresAt: t0.Add(72 * time.Hour),
	}, "pending"))

	list := decode[gen.InvitationList](t, h.as(t, ownerToken, http.MethodGet, acmePath+"/invitations", ""), http.StatusOK)
	if len(list.Invitations) != 1 || list.Invitations[0].Email != "d.ross@example.com" || list.Invitations[0].Inviter.Name != ownerName ||
		!slices.Equal(list.Invitations[0].FactoryRoles, []gen.FactoryRoleGrant{{FactoryId: acmeID, Role: gen.Viewer}}) {
		t.Errorf("invitations = %+v; want d.ross's, by F. Cruz, viewer of acme", list.Invitations)
	}

	if p := problem(t, h.as(t, viewerToken, http.MethodGet, acmePath+"/invitations", "")); p.Status != http.StatusForbidden {
		t.Errorf("a member listing invitations = %+v; want 403", p)
	}
}

// A factory role is given, changed and taken away at once, by name or id, with a recent sign-in;
// owners and admins hold every role already, and a stranger or an unknown factory is not found.
func TestAPI_Members_FactoryRoles(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	withPeople(t, h)

	rolePath := membersPath + "/" + string(viewerID) + "/factory-roles/"

	if p := problem(t, h.as(t, ownerToken, http.MethodPut, rolePath+"line", `{"role":"approver"}`)); deref(p.Code) != codeReauth {
		t.Errorf("with a session from an hour ago = %+v; want 403 reauth-required", p)
	}

	if res := h.as(t, viewerToken, http.MethodGet, "/v1/orgs/acme/factories/line", ""); res.status != http.StatusNotFound {
		t.Fatalf("before: the viewer sees line: %d", res.status)
	}

	grant := decode[gen.FactoryRoleGrant](t, h.as(t, freshOwnerToken, http.MethodPut, rolePath+"line", `{"role":"approver"}`), http.StatusOK)
	if grant.FactoryId != lineID || grant.Role != gen.Approver {
		t.Errorf("grant = %+v; want approver of line by its id", grant)
	}

	if res := h.as(t, viewerToken, http.MethodGet, "/v1/orgs/acme/factories/line", ""); res.status != http.StatusOK {
		t.Errorf("after the grant the viewer gets line = %d; want 200 at once", res.status)
	}

	if res := h.as(t, freshAdminToken, http.MethodDelete, rolePath+lineID, ""); res.status != http.StatusNoContent {
		t.Errorf("removing = %d %s; want 204", res.status, res.body)
	}

	if res := h.as(t, viewerToken, http.MethodGet, "/v1/orgs/acme/factories/line", ""); res.status != http.StatusNotFound {
		t.Errorf("after the removal the viewer gets line = %d; want 404 at once", res.status)
	}

	for _, c := range []struct {
		name, token, path, body string
		status                  int
		code                    string
	}{
		{"an owner", freshOwnerToken, membersPath + "/" + string(adminID) + "/factory-roles/line", viewerRole, http.StatusConflict, "editor-everywhere"},
		{"an unknown factory", freshOwnerToken, rolePath + "nope", viewerRole, http.StatusNotFound, "factory-not-found"},
		{"someone of another org", freshOwnerToken, membersPath + "/" + string(strangerID) + "/factory-roles/line", viewerRole, http.StatusNotFound, ""},
		{"an unknown role", freshOwnerToken, rolePath + "line", `{"role":"owner"}`, http.StatusBadRequest, ""},
		{"a member", viewerToken, membersPath + "/" + string(nairID) + "/factory-roles/acme", viewerRole, http.StatusForbidden, wantForbidden},
	} {
		p := problem(t, h.as(t, c.token, http.MethodPut, c.path, c.body))
		if p.Status != c.status || deref(p.Code) != c.code {
			t.Errorf("%s: %+v; want %d %q", c.name, p, c.status, c.code)
		}
	}
}

// Ownership goes to an admin, the organization's name typed, in one change: the caller becomes an
// admin and the admin an owner.
func TestAPI_Members_TransferOwnership(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	withPeople(t, h)

	transfer := func(token, to, confirm string) response {
		return h.as(t, token, http.MethodPost, acmePath+"/transfer", `{"to":"`+to+`","confirm":"`+confirm+`"}`)
	}

	for _, c := range []struct {
		name, token, to, confirm string
		status                   int
		code                     string
	}{
		{"a stale session", ownerToken, string(adminID), acmeTitle, http.StatusForbidden, codeReauth},
		{"an admin", freshAdminToken, string(adminID), acmeTitle, http.StatusForbidden, wantForbidden},
		{"the wrong name", freshOwnerToken, string(adminID), "acme", http.StatusConflict, "confirmation-mismatch"},
		{"to a member", freshOwnerToken, string(viewerID), acmeTitle, http.StatusConflict, "not-an-admin"},
	} {
		if p := problem(t, transfer(c.token, c.to, c.confirm)); p.Status != c.status || deref(p.Code) != c.code {
			t.Errorf("%s: %+v; want %d %q", c.name, p, c.status, c.code)
		}
	}

	org := decode[gen.Org](t, transfer(freshOwnerToken, string(adminID), acmeTitle), http.StatusOK)
	if org.Role == nil || *org.Role != gen.OrgRoleAdmin || slices.Contains(org.Can, "org:transfer") || !slices.Contains(org.Can, "member:manage") {
		t.Errorf("acme after the transfer = role %v can %v; want an admin who may no longer transfer it", org.Role, org.Can)
	}

	if o := decode[gen.Org](t, h.as(t, freshAdminToken, http.MethodGet, acmePath, ""), http.StatusOK); o.Role == nil || *o.Role != gen.OrgRoleOwner {
		t.Errorf("the former admin's role = %v; want owner", o.Role)
	}
}

// Under GOAL_AUTH=none the local user sees the members read-only and owns nothing to transfer.
func TestAPI_Members_WithoutSignIn(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var list gen.MemberList
	if status := h.call(t, http.MethodGet, membersPath, "", &list); status != http.StatusOK || len(list.Members) != 0 {
		t.Errorf("members without sign-in = %d %+v; want 200, nobody", status, list)
	}

	if res := h.request(t, http.MethodPost, acmePath+"/transfer", `{"to":"x","confirm":"`+acmeTitle+`"}`, nil); res.status != http.StatusForbidden {
		t.Errorf("a transfer without sign-in = %d %s; want 403", res.status, res.body)
	}
}
