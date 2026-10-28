package api_test

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const (
	// retention is how long the harness keeps a deleted organization.
	retention = 7 * 24 * time.Hour
	acmeSlug  = "acme"
	labsSlug  = "example-labs"
	// acmeSettings is the path of acme's settings.
	acmeSettings = "/v1/orgs/acme/settings"
)

// deref reads an optional string of the contract.
func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// change is a changed field of org.updated as JSON reads it back: a cap is a float64, none nil.
func change(from, to any) map[string]any {
	asJSON := func(v any) any {
		if n, ok := v.(int); ok {
			return float64(n)
		}

		return v
	}

	return map[string]any{"from": asJSON(from), "to": asJSON(to)}
}

// settings reads acme's settings.
func (h *harness) settings(t *testing.T) gen.OrgSettings {
	t.Helper()

	var s gen.OrgSettings
	if code := h.call(t, http.MethodGet, acmeSettings, "", &s); code != http.StatusOK {
		t.Fatalf("GET settings = %d", code)
	}

	return s
}

// patch changes acme's settings and returns them.
func (h *harness) patch(t *testing.T, body string) gen.OrgSettings {
	t.Helper()

	var s gen.OrgSettings
	if code := h.call(t, http.MethodPatch, acmeSettings, body, &s); code != http.StatusOK {
		t.Fatalf("PATCH settings %s = %d", body, code)
	}

	return s
}

func (h *harness) audit(t *testing.T, org, query string) gen.AuditPage {
	t.Helper()

	var p gen.AuditPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/"+org+"/audit"+query, "", &p); code != http.StatusOK {
		t.Fatalf("GET audit%s of %s = %d", query, org, code)
	}

	return p
}

func TestSettings_ReadAndChangeGeneralAndSlots(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	want := gen.OrgSettings{Name: "Acme", Slug: acmeSlug, Mark: "Ac", Slots: gen.OrgSlots{Factories: []gen.FactorySlots{
		{FactoryId: acmeID, Name: acmeSlug}, {FactoryId: lineID, Name: "line"},
	}}}
	if got := h.settings(t); !reflect.DeepEqual(got, want) {
		t.Errorf("settings = %+v; want %+v, no caps", got, want)
	}

	// Per-factory caps may add up to more than the organization's.
	got := h.patch(t,
		`{"name":" Acme Co ","mark":"AC","slots":{"max":4,"factories":[{"factory_id":"acme","max":3},{"factory_id":"`+lineID+`","max":2}]}}`)

	want.Name, want.Mark, want.Slots.Max = "Acme Co", "AC", new(4)
	want.Slots.Factories[0].Max, want.Slots.Factories[1].Max = new(3), new(2)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("PATCH answered %+v; want %+v", got, want)
	}

	if again := h.settings(t); !reflect.DeepEqual(again, want) {
		t.Errorf("settings after PATCH = %+v; want %+v", again, want)
	}

	// One org.updated per PATCH, with each changed field's from and to.
	entries := h.audit(t, acmeSlug, "?action=org.updated").Entries
	wantData := map[string]any{
		"name": change("Acme", "Acme Co"), "mark": change("Ac", "AC"), "max_slots": change(nil, 4),
		"factory_slots": map[string]any{acmeID: change(nil, 3), lineID: change(nil, 2)},
	}

	if len(entries) != 1 || entries[0].Actor.Id != domain.LocalUser.ID || entries[0].Target.Kind != "org" ||
		entries[0].Target.Id != string(testOrg) || !reflect.DeepEqual(entries[0].Data, wantData) {
		t.Errorf("org.updated = %+v; want one by the local user on acme with %v", entries, wantData)
	}
}

func TestSettings_NullClearsACapAndUnlistedFactoriesKeepTheirs(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.patch(t, `{"slots":{"max":4,"factories":[{"factory_id":"acme","max":3},{"factory_id":"line","max":2}]}}`)

	got := h.patch(t, `{"slots":{"max":null,"factories":[{"factory_id":"line","max":null}]}}`)
	if got.Slots.Max != nil || *got.Slots.Factories[0].Max != 3 || got.Slots.Factories[1].Max != nil {
		t.Errorf("slots after clearing = %+v; want the org's and line's caps cleared, acme's kept", got.Slots)
	}

	// Unchanged settings append nothing.
	h.patch(t, `{"name":"Acme","slots":{"max":null}}`)

	if entries := h.audit(t, acmeSlug, "?action=org.updated").Entries; len(entries) != 2 {
		t.Errorf("org.updated entries = %+v; want 2, one per change", entries)
	}
}

func TestSettings_SlugRenameResolvesAtOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if got := h.patch(t, `{"slug":"acme-co"}`); got.Slug != "acme-co" {
		t.Fatalf("rename = %+v", got)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme-co", "", nil); code != http.StatusOK {
		t.Errorf("GET the new slug = %d; want 200", code)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme", "", nil); code != http.StatusNotFound {
		t.Errorf("GET the old slug = %d; want 404", code)
	}
}

func TestSettings_BrokenRulesChangeNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	before := h.settings(t)

	tests := []struct {
		body string
		want int
		code string
	}{
		{body: `{"slug":"example-labs"}`, want: http.StatusConflict, code: "slug-taken"},
		{body: `{"slug":"settings"}`, want: http.StatusConflict},
		{body: `{"name":"   "}`, want: http.StatusConflict},
		{body: `{"mark":"ACO"}`, want: http.StatusConflict},
		{body: `{"slots":{"max":0}}`, want: http.StatusConflict},
		{body: `{"slots":{"max":1001}}`, want: http.StatusConflict},
		{body: `{"slots":{"max":2,"factories":[{"factory_id":"acme","max":-1}]}}`, want: http.StatusConflict},
		{body: `{"slots":{"max":2,"factories":[{"factory_id":"nope","max":1}]}}`, want: http.StatusNotFound, code: "factory-not-found"},
		{body: `{"slots":`, want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		var p gen.Problem
		if code := h.call(t, http.MethodPatch, acmeSettings, tt.body, &p); code != tt.want || deref(p.Code) != tt.code {
			t.Errorf("PATCH %s = %d %q; want %d %q", tt.body, code, deref(p.Code), tt.want, tt.code)
		}
	}

	if after := h.settings(t); !reflect.DeepEqual(after, before) {
		t.Errorf("settings after refused changes = %+v; want %+v", after, before)
	}

	if entries := h.audit(t, acmeSlug, "?action=org.updated").Entries; len(entries) != 0 {
		t.Errorf("refused changes were audited: %+v", entries)
	}
}

func TestDeleteOrg_AConfirmationThatIsNotTheNameDeletesNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, body := range []string{`{"confirm":"acme"}`, `{"confirm":""}`} {
		var p gen.Problem
		if code := h.call(t, http.MethodDelete, "/v1/orgs/acme", body, &p); code != http.StatusConflict ||
			deref(p.Code) != "confirmation-mismatch" {
			t.Errorf("delete with %s = %d %+v; want 409 confirmation-mismatch", body, code, p)
		}
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme", "", nil); code != http.StatusOK {
		t.Errorf("after refused deletes GET = %d; want the org kept", code)
	}
}

func TestDeleteOrg_GoneAtOnceAndPurgedAfterTheRetention(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var deleted gen.OrgDeleted
	if code := h.call(t, http.MethodDelete, "/v1/orgs/acme", `{"confirm":"Acme"}`, &deleted); code != http.StatusAccepted ||
		!deleted.PurgeAfter.Equal(t0.Add(retention)) {
		t.Fatalf("delete = %d %+v; want 202 with purge_after %s", code, deleted, t0.Add(retention))
	}

	for _, path := range []string{"/v1/orgs/acme", acmeSettings, "/v1/orgs/acme/audit", "/v1/orgs/" + string(testOrg)} {
		var p gen.Problem
		if code := h.call(t, http.MethodGet, path, "", &p); code != http.StatusNotFound || deref(p.Code) != "org-not-found" {
			t.Errorf("GET %s after the delete = %d %+v; want 404 org-not-found", path, code, p)
		}
	}

	var list gen.OrgList
	if h.call(t, http.MethodGet, orgsPath, "", &list); len(list.Orgs) != 1 || list.Orgs[0].Slug != labsSlug {
		t.Errorf("orgs after the delete = %+v; want only example-labs", list.Orgs)
	}

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditOrgDeleted, Limit: 10})
	if err != nil || len(entries) != 1 || entries[0].Actor != domain.LocalUser {
		t.Errorf("org.deleted entries = %+v, %v; want one by the local user", entries, err)
	}
}

func TestAudit_PagesNewestFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for i := range 3 {
		h.patch(t, fmt.Sprintf(`{"name":"Acme %d"}`, i))
	}

	page := h.audit(t, acmeSlug, "?limit=2")
	if len(page.Entries) != 2 || page.NextBefore == nil || page.Entries[0].Id <= page.Entries[1].Id ||
		*page.NextBefore != page.Entries[1].Id {
		t.Fatalf("first page = %+v; want the 2 newest entries and a cursor", page)
	}

	rest := h.audit(t, acmeSlug, fmt.Sprintf("?limit=2&before=%d", *page.NextBefore))
	if len(rest.Entries) != 2 || rest.NextBefore != nil || rest.Entries[1].Action != domain.AuditOrgCreated {
		t.Errorf("last page = %+v; want the 2 oldest, org.created last, and no cursor", rest)
	}
}

func TestAudit_FiltersAndOrgs(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.patch(t, `{"name":"Acme Co"}`)

	if got := h.audit(t, acmeSlug, "?action=org.created").Entries; len(got) != 1 {
		t.Errorf("action filter = %+v; want the one org.created", got)
	}

	if got := h.audit(t, acmeSlug, "?factory=acme").Entries; len(got) != 0 {
		t.Errorf("factory filter = %+v; want none, no entry concerns a factory", got)
	}

	if got := h.audit(t, labsSlug, "").Entries; len(got) != 1 || got[0].Target.Id != string(otherOrg) {
		t.Errorf("another org's log = %+v; want only its own org.created", got)
	}

	for _, q := range []string{"?limit=0", "?limit=501", "?factory=nope"} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/audit"+q, "", nil); code == http.StatusOK {
			t.Errorf("GET audit%s = %d; want refused", q, code)
		}
	}
}

// signupPath is acme's sign-up domain; ownDomain sets it to its owner's email's domain.
const (
	signupPath = acmeSettings + "/signup-domain"
	ownDomain  = `{"domain":"example.com"}`
)

// withSignupDomains is the signed-in harness with acme's people and the webmail list, the fake
// checking sign-up domains against the people's sign-in emails (the owner's is at example.com).
func withSignupDomains(t *testing.T) signedIn {
	t.Helper()

	h := newSignedIn(t)
	withPeople(t, h)
	h.tenancy.WithSignIn(h.identity.Email, "gmail.com")

	return h
}

// ownerSettings reads acme's settings as its owner.
func ownerSettings(t *testing.T, h signedIn) gen.OrgSettings {
	t.Helper()

	return decode[gen.OrgSettings](t, h.as(t, ownerToken, http.MethodGet, acmeSettings, ""), http.StatusOK)
}

// An owner sets acme's sign-up domain, case ignored and viewer by default, changes its role and
// clears it; GET settings shows each state and the audit log each change.
func TestSignupDomain_OwnerSetsReadsAndClears(t *testing.T) {
	t.Parallel()

	h := withSignupDomains(t)

	if s := ownerSettings(t, h); s.SignupDomain != nil {
		t.Fatalf("signup_domain before = %+v; want null", s.SignupDomain)
	}

	set := decode[gen.SignupDomain](t, h.as(t, freshOwnerToken, http.MethodPut, signupPath, `{"domain":" Example.COM "}`), http.StatusOK)
	if set.Domain != "example.com" || set.JoinRole != gen.Viewer || set.SetBy.Id != string(ownerID) || set.SetAt.IsZero() {
		t.Errorf("set = %+v; want example.com, viewer, by the owner, stamped", set)
	}

	if s := ownerSettings(t, h); s.SignupDomain == nil || *s.SignupDomain != set {
		t.Errorf("settings' signup_domain = %+v; want %+v", s.SignupDomain, set)
	}

	edit := decode[gen.SignupDomain](t, h.as(t, freshOwnerToken, http.MethodPut, signupPath, `{"domain":"example.com","join_role":"editor"}`), http.StatusOK)
	if edit.JoinRole != gen.Editor {
		t.Errorf("join role after the change = %q; want editor", edit.JoinRole)
	}

	if res := h.as(t, freshOwnerToken, http.MethodDelete, signupPath, ""); res.status != http.StatusNoContent {
		t.Fatalf("DELETE = %d; want 204", res.status)
	}

	if s := ownerSettings(t, h); s.SignupDomain != nil {
		t.Errorf("signup_domain after clearing = %+v; want null", s.SignupDomain)
	}

	var actions []string

	page := decode[gen.AuditPage](t, h.as(t, ownerToken, http.MethodGet, acmePath+"/audit?target_kind=org", ""), http.StatusOK)
	for _, e := range page.Entries {
		actions = append(actions, e.Action)
	}

	want := []string{domain.AuditOrgSignupDomainCleared, domain.AuditOrgSignupDomainSet, domain.AuditOrgSignupDomainSet, domain.AuditOrgCreated}
	if !reflect.DeepEqual(actions, want) {
		t.Errorf("audit = %v; want %v, newest first", actions, want)
	}
}

// Only an owner with a recent sign-in sets or clears it, and only to their own email's domain,
// never a webmail domain or one another live organization holds; a refusal changes nothing.
func TestSignupDomain_Refusals(t *testing.T) {
	t.Parallel()

	h := withSignupDomains(t)

	fresh := domain.Session{ID: "fresh-stranger", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour), Browser: true}
	h.identity.AddSession("FreshStrangerFreshStrangerFresh6", strangerID, fresh)
	decode[gen.SignupDomain](t, h.as(t, "FreshStrangerFreshStrangerFresh6", http.MethodPut, "/v1/orgs/"+labsSlug+"/settings/signup-domain",
		ownDomain), http.StatusOK)

	for _, c := range []struct {
		name, token, method, body string
		status                    int
		code                      string
	}{
		{"a stale session", ownerToken, http.MethodPut, ownDomain, http.StatusForbidden, codeReauth},
		{"an admin", freshAdminToken, http.MethodPut, ownDomain, http.StatusForbidden, wantForbidden},
		{"an admin clearing", freshAdminToken, http.MethodDelete, "", http.StatusForbidden, wantForbidden},
		{"a viewer", viewerToken, http.MethodPut, ownDomain, http.StatusForbidden, wantForbidden},
		{"another domain", freshOwnerToken, http.MethodPut, `{"domain":"example.org"}`, http.StatusBadRequest, "signup-domain-not-yours"},
		{"a subdomain", freshOwnerToken, http.MethodPut, `{"domain":"eu.example.com"}`, http.StatusBadRequest, "signup-domain-not-yours"},
		{"no domain", freshOwnerToken, http.MethodPut, `{"domain":"not a domain"}`, http.StatusBadRequest, ""},
		{"an unknown role", freshOwnerToken, http.MethodPut, `{"domain":"example.com","join_role":"owner"}`, http.StatusBadRequest, ""},
		{"another org's", freshOwnerToken, http.MethodPut, ownDomain, http.StatusConflict, "signup-domain-taken"},
	} {
		if p := problem(t, h.as(t, c.token, c.method, signupPath, c.body)); p.Status != c.status || deref(p.Code) != c.code {
			t.Errorf("%s: %+v; want %d %q", c.name, p, c.status, c.code)
		}
	}

	if err := h.identity.AddUser(porttest.IdentityUser{ID: ownerID, Name: ownerName, Email: "f.cruz@GMAIL.com", PlatformAdmin: true}); err != nil {
		t.Fatal(err)
	}

	if p := problem(t, h.as(t, freshOwnerToken, http.MethodPut, signupPath, `{"domain":"gmail.com"}`)); p.Status != http.StatusBadRequest ||
		deref(p.Code) != "signup-domain-public" {
		t.Errorf("a webmail domain: %+v; want 400 signup-domain-public", p)
	}

	if s := ownerSettings(t, h); s.SignupDomain != nil {
		t.Errorf("signup_domain after the refusals = %+v; want null", s.SignupDomain)
	}
}

// A deleted organization's domain no longer blocks a live one's claim.
func TestSignupDomain_ADeletedOrgsDomainIsTakenOver(t *testing.T) {
	t.Parallel()

	h := withSignupDomains(t)

	fresh := domain.Session{ID: "fresh-stranger", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour), Browser: true}
	h.identity.AddSession("FreshStrangerFreshStrangerFresh6", strangerID, fresh)

	labs := "/v1/orgs/" + labsSlug
	decode[gen.SignupDomain](t, h.as(t, "FreshStrangerFreshStrangerFresh6", http.MethodPut, labs+"/settings/signup-domain", ownDomain), http.StatusOK)

	if res := h.as(t, "FreshStrangerFreshStrangerFresh6", http.MethodDelete, labs, `{"confirm":"Example Labs"}`); res.status != http.StatusAccepted {
		t.Fatalf("deleting example-labs = %d", res.status)
	}

	decode[gen.SignupDomain](t, h.as(t, freshOwnerToken, http.MethodPut, signupPath, ownDomain), http.StatusOK)
}
