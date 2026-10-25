package api_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// The people of the signed-in harness: acme's owner (a platform admin), a viewer of acme's
// factory acme only, and the owner of example-labs alone.
const (
	ownerID    domain.UserID = "019a4000-0000-7000-8000-000000000001"
	viewerID   domain.UserID = "019a4000-0000-7000-8000-000000000002"
	strangerID domain.UserID = "019a4000-0000-7000-8000-000000000003"
	ownerName                = "F. Cruz"

	ownerToken    = "OwnerOwnerOwnerOwnerOwnerOwner01"
	viewerToken   = "ViewerViewerViewerViewerViewer02"
	strangerToken = "StrangerStrangerStrangerStrang03"
	viewerSession = "019a4000-0000-7000-8000-0000000000b2"

	acmePath   = "/v1/orgs/acme"
	healthPath = "/v1/health"
)

// signedIn is the harness under GOAL_AUTH=session with acme's people signed in for a week.
type signedIn struct {
	*harness

	identity *porttest.Identity
	access   *porttest.AccessNotices
}

func newSignedIn(t *testing.T) signedIn {
	t.Helper()

	return newSignedInWith(t, nil)
}

// newSignedInWith is newSignedIn with goald's log written to logs when it is set.
func newSignedInWith(t *testing.T, logs io.Writer) signedIn {
	t.Helper()

	var h *harness

	id := porttest.NewIdentity(func() time.Time { return h.clock.Now() })
	in := &signIn{identity: id, access: porttest.NewAccessNotices(), google: true}
	h = newHarnessLogged(t, in, logs) // adds acme and example-labs, with acme's factories, to id

	people := []struct {
		user          domain.UserID
		name, token   string
		platformAdmin bool
		org           domain.OrgID
		role          domain.OrgRole
	}{
		{ownerID, ownerName, ownerToken, true, testOrg, domain.OrgOwner},
		{viewerID, "M. Okafor", viewerToken, false, testOrg, domain.OrgMember},
		{strangerID, "J. Doe", strangerToken, false, otherOrg, domain.OrgOwner},
	}

	for i, p := range people {
		if err := id.AddUser(porttest.IdentityUser{ID: p.user, Name: p.name, Email: string(p.user[len(p.user)-1:]) + "@example.com", PlatformAdmin: p.platformAdmin}); err != nil {
			t.Fatal(err)
		}

		session := domain.Session{ID: "019a4000-0000-7000-8000-0000000000b" + string(rune('1'+i)), CreatedAt: t0.Add(-time.Hour), ExpiresAt: t0.Add(7 * 24 * time.Hour), Browser: true}
		id.AddSession(p.token, p.user, session)

		if err := id.SetOrgRole(p.org, p.user, p.role); err != nil {
			t.Fatal(err)
		}
	}

	if err := id.GrantFactoryRole(testOrg, viewerID, acmeID, domain.FactoryViewer); err != nil {
		t.Fatal(err)
	}

	return signedIn{harness: h, identity: id, access: in.access}
}

// as sends a request with token's session cookie (Better Auth's signed value), or none.
func (h signedIn) as(t *testing.T, token, method, path, body string) response {
	t.Helper()

	var header http.Header
	if token != "" {
		header = cookie(token)
	}

	return h.request(t, method, path, body, header)
}

// cookie is token's session cookie as Better Auth signs it.
func cookie(token string) http.Header {
	return http.Header{"Cookie": {"goal.session_token=" + token + ".c2lnbmF0dXJl"}}
}

// problem decodes a problem response.
func problem(t *testing.T, res response) gen.Problem {
	t.Helper()

	var p gen.Problem
	if err := json.Unmarshal(res.body, &p); err != nil {
		t.Fatalf("decode %s: %v", res.body, err)
	}

	return p
}

// Spec 3 §8.3 and §11.1 rows 1 and 3: the credential is checked first, whatever the path, so a
// stranger learns nothing about organizations; then the organization, a non-member answering as
// an unknown one, exactly. Public operations read no credential at all.
func TestAPI_SignIn_CredentialFirstThenOrganization(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	notFound := problem(t, h.as(t, strangerToken, http.MethodGet, "/v1/orgs/nope", ""))

	for _, c := range []struct {
		name, token, path string
		want              int
	}{
		{name: "anonymous, a real org", path: acmePath, want: http.StatusUnauthorized},
		{name: "anonymous, an unknown org", path: "/v1/orgs/nope/factories", want: http.StatusUnauthorized},
		{name: "unknown session", token: "UnknownUnknownUnknownUnknownUnkn", path: acmePath, want: http.StatusUnauthorized},
		{name: "anonymous, the caller", path: "/v1/me", want: http.StatusUnauthorized},
		{name: "a member of another org", token: strangerToken, path: acmePath, want: http.StatusNotFound},
		{name: "a member", token: viewerToken, path: acmePath, want: http.StatusOK},
	} {
		res := h.as(t, c.token, http.MethodGet, c.path, "")
		if res.status != c.want {
			t.Errorf("%s: %s = %d %s, want %d", c.name, c.path, res.status, res.body, c.want)
		}

		if c.want == http.StatusUnauthorized && !unauthenticated(res) {
			t.Errorf("%s: %s, want the one 401 body", c.name, res.body)
		}

		if c.want == http.StatusNotFound && !reflect.DeepEqual(problem(t, res), notFound) {
			t.Errorf("%s: %s, want exactly the unknown organization's body %+v", c.name, res.body, notFound)
		}
	}

	broken := http.Header{"Cookie": {"goal.session_token=not-a-session"}}
	for _, path := range []string{healthPath, "/v1/auth/config"} {
		if res := h.request(t, http.MethodGet, path, "", broken); res.status != http.StatusOK {
			t.Errorf("%s with a broken cookie = %d %s, want it served (public operations read no credential)", path, res.status, res.body)
		}
	}

	if res := h.as(t, "", http.MethodGet, acmePath, ""); res.status != http.StatusUnauthorized {
		t.Errorf("a broken cookie elsewhere = %d, want 401", res.status)
	}

	config := decode[gen.AuthConfig](t, h.as(t, "", http.MethodGet, "/v1/auth/config", ""), http.StatusOK)
	if config.Mode != gen.Session || !slices.Equal(config.Methods, []gen.SignInMethod{gen.EmailCode, gen.Google}) {
		t.Errorf("auth config = %+v, want session by email code and Google", config)
	}
}

// A member sees their organizations with their role, what they may do there and the factories
// they see, and who they are.
func TestAPI_SignIn_OrgsByMembership(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	orgs := decode[gen.OrgList](t, h.as(t, viewerToken, http.MethodGet, "/v1/orgs", ""), http.StatusOK)
	if len(orgs.Orgs) != 1 || orgs.Orgs[0].Slug != acmeSlug || orgs.Orgs[0].Role == nil || *orgs.Orgs[0].Role != gen.OrgRoleMember || orgs.Orgs[0].FactoryCount != 1 {
		t.Errorf("viewer's orgs = %+v, want acme alone, as a member seeing 1 factory", orgs.Orgs)
	}

	if owners := decode[gen.OrgList](t, h.as(t, ownerToken, http.MethodGet, "/v1/orgs", ""), http.StatusOK); len(owners.Orgs) != 1 || owners.Orgs[0].FactoryCount != 2 {
		t.Errorf("owner's orgs = %+v, want acme with both factories", owners.Orgs)
	}

	org := decode[gen.Org](t, h.as(t, viewerToken, http.MethodGet, acmePath, ""), http.StatusOK)

	wantCan := []string{"connection:read", "knowledge:read", "member:read", "org:read", "personal_token:manage", "personal_token:read"}
	if org.Role == nil || *org.Role != gen.OrgRoleMember || !slices.Equal(org.Can, wantCan) || org.FactoryCount != 1 {
		t.Errorf("acme for its viewer = role %v can %v count %d, want member, %v, 1", org.Role, org.Can, org.FactoryCount, wantCan)
	}
}

// The caller is the signed-in person, with their session and when it stops being fresh.
func TestAPI_SignIn_Me(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	me := decode[gen.Me](t, h.as(t, viewerToken, http.MethodGet, "/v1/me", ""), http.StatusOK)
	if me.Mode != gen.Session || me.User == nil || me.User.Id != string(viewerID) || me.Session == nil || me.PlatformAdmin ||
		!me.Session.FreshUntil.Equal(t0.Add(-time.Hour).Add(domain.FreshFor)) {
		t.Errorf("me = %+v, want the viewer with their session", me)
	}
}

// A member sees only the factories they hold a role on, with that role and what it allows;
// anything else answers as unknown, and what the role does not allow is 403.
func TestAPI_SignIn_FactoriesByRole(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	list := decode[gen.FactoryList](t, h.as(t, viewerToken, http.MethodGet, "/v1/orgs/acme/factories", ""), http.StatusOK)
	if len(list.Factories) != 1 || list.Factories[0].Name != acmeSlug || list.Factories[0].Role == nil || *list.Factories[0].Role != gen.Viewer ||
		!slices.Equal(list.Factories[0].Can, []string{"factory:read"}) {
		t.Errorf("viewer's factories = %+v, want acme alone, as a viewer who may read it", list.Factories)
	}

	hidden := h.as(t, viewerToken, http.MethodGet, "/v1/orgs/acme/factories/line", "")
	unknown := h.as(t, viewerToken, http.MethodGet, "/v1/orgs/acme/factories/nope", "")

	if hidden.status != http.StatusNotFound || string(hidden.body) != string(unknown.body) {
		t.Errorf("a factory the viewer has no role on = %d %s, want exactly an unknown one's %s", hidden.status, hidden.body, unknown.body)
	}

	before := h.items(t)
	if res := h.as(t, viewerToken, http.MethodPost, acmeItems, submitFix); res.status != http.StatusForbidden || h.items(t) != before {
		t.Errorf("a viewer submitting = %d %s, want 403 and nothing written", res.status, res.body)
	}

	if res := h.as(t, ownerToken, http.MethodPost, acmeItems, submitFix); res.status != http.StatusCreated {
		t.Errorf("the owner submitting = %d %s, want 201", res.status, res.body)
	}
}

// Spec 3 §11.1 row 2: a removal and a sign-out hold on the next request; nothing is cached.
func TestAPI_SignIn_RemovalHoldsAtOnce(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	if res := h.as(t, viewerToken, http.MethodGet, "/v1/orgs/acme/factories/acme", ""); res.status != http.StatusOK {
		t.Fatalf("before = %d", res.status)
	}

	if err := h.identity.GrantFactoryRole(testOrg, viewerID, acmeID, ""); err != nil {
		t.Fatal(err)
	}

	if res := h.as(t, viewerToken, http.MethodGet, "/v1/orgs/acme/factories/acme", ""); res.status != http.StatusNotFound {
		t.Errorf("after the factory role went = %d, want 404", res.status)
	}

	if err := h.identity.SetOrgRole(testOrg, viewerID, ""); err != nil {
		t.Fatal(err)
	}

	if res := h.as(t, viewerToken, http.MethodGet, acmePath, ""); res.status != http.StatusNotFound {
		t.Errorf("after the removal = %d, want 404", res.status)
	}

	h.identity.EndSession(viewerToken)

	if res := h.as(t, viewerToken, http.MethodGet, "/v1/orgs", ""); !unauthenticated(res) {
		t.Errorf("after signing out = %d %s, want 401", res.status, res.body)
	}
}

// A platform admin's new organization is theirs (NewOrg.Owner); nobody else may create one.
func TestAPI_SignIn_CreateOrgOwnedByTheCaller(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	created := h.as(t, ownerToken, http.MethodPost, "/v1/orgs", `{"slug":"beta-labs","name":"Beta Labs"}`)
	o := decode[gen.Org](t, created, http.StatusCreated)

	if h.tenancy.Owner(domain.OrgID(o.Id)) != ownerID || o.Role == nil || *o.Role != gen.OrgRoleOwner || !slices.Contains(o.Can, "org:delete") {
		t.Errorf("created org = %+v (owner %s), want it owned by the caller", o, h.tenancy.Owner(domain.OrgID(o.Id)))
	}

	if res := h.as(t, viewerToken, http.MethodPost, "/v1/orgs", `{"slug":"gamma-labs","name":"Gamma Labs"}`); res.status != http.StatusForbidden {
		t.Errorf("a member creating an org = %d, want 403", res.status)
	}
}

// Rows from before sign-in name the local user so once sign-in is on (spec 3 §14).
func TestAPI_SignIn_LocalUserBeforeSignIn(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	var page gen.AuditPage
	if err := json.Unmarshal(h.as(t, ownerToken, http.MethodGet, "/v1/orgs/acme/audit", "").body, &page); err != nil || len(page.Entries) == 0 {
		t.Fatalf("audit = %+v, %v", page, err)
	}

	created := page.Entries[len(page.Entries)-1]
	if created.Action != domain.AuditOrgCreated || created.Actor.Id != domain.LocalUser.ID || created.Actor.Name != "Local user (before sign-in)" {
		t.Errorf("org.created by %+v, want the local user, named before sign-in", created.Actor)
	}
}

// stream opens acme's stream with token's cookie and reads its opening line.
func (h signedIn) stream(t *testing.T, token string) sse {
	t.Helper()

	return h.streamWith(t, cookie(token))
}

// streamWith opens acme's stream with header's credential and reads its opening line.
func (h signedIn) streamWith(t *testing.T, header http.Header) sse {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.srv.URL+"/v1/orgs/acme/stream", nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header = header

	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("stream = %v, %v", res, err)
	}

	t.Cleanup(func() { _ = res.Body.Close() })

	events := sse{r: bufio.NewReader(res.Body)}
	events.next(t) // retry

	return events
}

// ended reports whether the stream ended (EOF) before another event.
func ended(events sse) bool {
	_, err := events.r.ReadString('\n')

	return errors.Is(err, io.EOF)
}

// Spec 3 §8.8: a stream carries only the factories its subscriber sees, and ends when an access
// notice names its session, its membership or its organization, on a resync, and when its
// session expires.
func TestAPI_SignIn_StreamFollowsAccess(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	at := t0.Add(time.Second)
	event := func(f domain.FactoryID, id int64) ports.ItemNotice {
		return ports.ItemNotice{OrgID: testOrg, EventID: id, FactoryID: f, ItemID: "i", Number: 1, Kind: domain.EventCreated, Station: stInbox, State: domain.StateQueued, At: at}
	}

	events := h.stream(t, viewerToken)
	h.notices.Publish(event(lineID, 1), event(acmeID, 2))

	if got := events.next(t); len(got) != 3 || got[1] != "id: 2" {
		t.Errorf("viewer's first event = %q, want acme's (id 2), never line's", got)
	}

	// Someone else's change ends nothing; the viewer's own does.
	h.access.Publish(ports.AccessNotice{Kind: ports.AccessMember, OrgID: testOrg, UserID: ownerID})
	h.notices.Publish(event(acmeID, 3))

	if got := events.next(t); len(got) != 3 || got[1] != "id: 3" {
		t.Errorf("after another person's change = %q, want the stream going on", got)
	}

	h.access.Publish(ports.AccessNotice{Kind: ports.AccessSession, UserID: viewerID, SessionID: viewerSession})

	if !ended(events) {
		t.Error("the stream went on after its session ended")
	}

	for name, notice := range map[string]ports.AccessNotice{
		"membership":   {Kind: ports.AccessMember, OrgID: testOrg, UserID: ownerID},
		"factory role": {Kind: ports.AccessFactoryRole, OrgID: testOrg, UserID: ownerID, FactoryID: acmeID},
		"organization": {Kind: ports.AccessOrg, OrgID: testOrg},
		"resync":       {Resync: true},
	} {
		owner := h.stream(t, ownerToken)
		h.access.Publish(notice)

		if !ended(owner) {
			t.Errorf("the owner's stream went on after a %s notice", name)
		}
	}

	owner := h.stream(t, ownerToken)
	h.clock.Advance(7 * 24 * time.Hour)

	if !ended(owner) {
		t.Error("the stream went on past its session's expiry")
	}
}
