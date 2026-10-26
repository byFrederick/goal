// Package identitytest is the conformance suite of ports.Identity's session lookups (spec 3 §13):
// the in-memory fake and the PostgreSQL adapter run the same cases, so the API's tests on the
// fake hold against the database. Each implementation provides a Seed to set up people,
// sessions, organizations and roles the way its own world does (the fake's setters, Better
// Auth's rows).
package identitytest

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Seed sets up what the cases need. Every method fails the test on error.
type Seed interface {
	// Org creates a live organization named name and returns its id.
	Org(t *testing.T, name string) domain.OrgID
	// Factory adds a factory to org and returns its id.
	Factory(t *testing.T, org domain.OrgID) domain.FactoryID
	// User adds a person, a platform admin or not.
	User(t *testing.T, name, email string, platformAdmin bool) domain.UserID
	// Session signs user in with a session created at created and expiring at expires, and
	// returns its token (32 letters and digits).
	Session(t *testing.T, user domain.UserID, created, expires time.Time) string
	// SignedInWith signs user in now, for an hour, as the auth service records how (method:
	// email_code, google, device, other, or "" for a session it did not tag), and returns its
	// token.
	SignedInWith(t *testing.T, user domain.UserID, method string) string
	// EndSession signs the session out.
	EndSession(t *testing.T, token string)
	// SetRole makes user a member of org with role; "" removes them.
	SetRole(t *testing.T, org domain.OrgID, user domain.UserID, role domain.OrgRole)
	// SetFactoryRole gives member user role on factory f of org; "" removes it.
	SetFactoryRole(t *testing.T, org domain.OrgID, user domain.UserID, f domain.FactoryID, role domain.FactoryRole)
	// DeleteOrg soft-deletes org.
	DeleteOrg(t *testing.T, org domain.OrgID)
}

// Run runs every case against a fresh identity and its seed from setup.
func Run(t *testing.T, setup func(t *testing.T) (ports.Identity, Seed)) {
	t.Helper()

	cases := []struct {
		name string
		run  func(t *testing.T, id ports.Identity, s Seed)
	}{
		{"SessionCarriesTheRolesOfTheOrgAsked", testSessionRoles},
		{"UnknownExpiredAndEndedSessionsDoNotResolve", testSessionRefused},
		{"OnlyABrowserSignInIsABrowserSession", testBrowserSession},
		{"ChangesHoldOnTheNextLookup", testChangesHoldAtOnce},
		{"ADeletedOrgGivesNoRole", testDeletedOrg},
		{"OrgsForListsTheLiveMembershipsByName", testOrgsFor},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			id, s := setup(t)
			c.run(t, id, s)
		})
	}
}

// now is the cases' clock: the database's sessions are dated by the wall clock.
func now() time.Time { return time.Now().UTC().Truncate(time.Second) }

func session(t *testing.T, id ports.Identity, token string, org domain.OrgID) domain.Principal {
	t.Helper()

	p, err := id.Session(t.Context(), token, org)
	if err != nil {
		t.Fatalf("Session(%s) = %v; want it resolved", org, err)
	}

	return p
}

func testSessionRoles(t *testing.T, id ports.Identity, s Seed) {
	t.Helper()

	acme, labs := s.Org(t, "Acme"), s.Org(t, "Labs")
	f, g := s.Factory(t, acme), s.Factory(t, acme)
	ada := s.User(t, "Ada", "ada@example.com", true)
	created, expires := now().Add(-time.Minute), now().Add(time.Hour)
	token := s.Session(t, ada, created, expires)
	s.SetRole(t, acme, ada, domain.OrgMember)
	s.SetFactoryRole(t, acme, ada, f, domain.FactoryApprover)

	p := session(t, id, token, acme)
	checkSessionPerson(t, p, ada, acme, created, expires)

	if p.OrgRole != domain.OrgMember || len(p.FactoryRoles) != 1 || p.FactoryRoles[f] != domain.FactoryApprover || p.RoleOn(g) != "" {
		t.Errorf("roles in acme = %s %v; want member, approver of %s only", p.OrgRole, p.FactoryRoles, f)
	}

	for _, org := range []domain.OrgID{labs, "", "00000000-0000-7000-8000-00000000dead", "not-an-id"} {
		if p := session(t, id, token, org); p.OrgRole != "" || len(p.FactoryRoles) != 0 || p.User != ada {
			t.Errorf("in %q: roles %s %v; want Ada without a role", org, p.OrgRole, p.FactoryRoles)
		}
	}
}

// checkSessionPerson checks the person and session a lookup resolved: Ada, a platform admin, in org.
func checkSessionPerson(t *testing.T, p domain.Principal, ada domain.UserID, org domain.OrgID, created, expires time.Time) {
	t.Helper()

	if p.Kind != domain.PrincipalUser || p.User != ada || p.Name != "Ada" || p.Email != "ada@example.com" || !p.PlatformAdmin || p.Org != org {
		t.Errorf("principal = %+v; want Ada, a platform admin, in %s", p, org)
	}

	if p.Session == nil || p.Session.ID == "" || !p.Session.CreatedAt.Equal(created) || !p.Session.ExpiresAt.Equal(expires) {
		t.Errorf("session = %+v; want created %v, expiring %v", p.Session, created, expires)
	}
}

func testSessionRefused(t *testing.T, id ports.Identity, s Seed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada := s.User(t, "Ada", "ada@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	expired := s.Session(t, ada, now().Add(-2*time.Hour), now().Add(-time.Minute))
	ended := s.Session(t, ada, now().Add(-time.Minute), now().Add(time.Hour))
	s.EndSession(t, ended)

	for name, token := range map[string]string{
		"unknown": "Zz0000000000000000000000000000zZ", "expired": expired, "ended": ended, "empty": "",
	} {
		if p, err := id.Session(t.Context(), token, acme); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("%s session = %+v, %v; want ErrUnauthenticated", name, p, err)
		}
	}
}

// testBrowserSession: a session is the browser's only when the person signed in there, by an
// email code or Google; goal login's device flow, any other way and an untagged session are not,
// however their token is later sent (review M1).
func testBrowserSession(t *testing.T, id ports.Identity, s Seed) {
	t.Helper()

	ada := s.User(t, "Ada", "ada@example.com", false)

	for method, browser := range map[string]bool{"email_code": true, "google": true, "device": false, "other": false, "": false} {
		p := session(t, id, s.SignedInWith(t, ada, method), "")
		if p.Session == nil || p.Session.Browser != browser {
			t.Errorf("signed in with %q: session %+v; want Browser %v", method, p.Session, browser)
		}
	}
}

func testChangesHoldAtOnce(t *testing.T, id ports.Identity, s Seed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	f := s.Factory(t, acme)
	owner, ada := s.User(t, "Owner", "owner@example.com", false), s.User(t, "Ada", "ada@example.com", false)
	s.SetRole(t, acme, owner, domain.OrgOwner)
	s.SetRole(t, acme, ada, domain.OrgAdmin)
	token := s.Session(t, ada, now(), now().Add(time.Hour))

	if p := session(t, id, token, acme); p.OrgRole != domain.OrgAdmin {
		t.Fatalf("role = %q; want admin", p.OrgRole)
	}

	s.SetRole(t, acme, ada, domain.OrgMember)
	s.SetFactoryRole(t, acme, ada, f, domain.FactoryViewer)

	if p := session(t, id, token, acme); p.OrgRole != domain.OrgMember || p.FactoryRoles[f] != domain.FactoryViewer {
		t.Errorf("after the change: %s %v; want member, viewer of %s", p.OrgRole, p.FactoryRoles, f)
	}

	s.SetFactoryRole(t, acme, ada, f, "")

	if p := session(t, id, token, acme); len(p.FactoryRoles) != 0 {
		t.Errorf("after removing the factory role: %v; want none", p.FactoryRoles)
	}

	s.SetRole(t, acme, ada, "")

	if p := session(t, id, token, acme); p.OrgRole != "" {
		t.Errorf("after the removal: role %q; want none", p.OrgRole)
	}
}

func testDeletedOrg(t *testing.T, id ports.Identity, s Seed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada := s.User(t, "Ada", "ada@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	token := s.Session(t, ada, now(), now().Add(time.Hour))
	s.DeleteOrg(t, acme)

	if p := session(t, id, token, acme); p.OrgRole != "" {
		t.Errorf("role in a deleted org = %q; want none", p.OrgRole)
	}

	orgs, err := id.OrgsFor(t.Context(), ada)
	if err != nil || len(orgs) != 0 {
		t.Errorf("OrgsFor after the deletion = %+v, %v; want none", orgs, err)
	}
}

func testOrgsFor(t *testing.T, id ports.Identity, s Seed) {
	t.Helper()

	beta, alpha, other := s.Org(t, "Beta"), s.Org(t, "Alpha"), s.Org(t, "Gamma")
	f := s.Factory(t, beta)
	s.Factory(t, beta)
	s.Factory(t, alpha)
	s.Factory(t, alpha)
	s.Factory(t, alpha)
	ada, owner := s.User(t, "Ada", "ada@example.com", false), s.User(t, "Owner", "owner@example.com", false)

	for _, org := range []domain.OrgID{beta, alpha, other} {
		s.SetRole(t, org, owner, domain.OrgOwner)
	}

	s.SetRole(t, beta, ada, domain.OrgMember)
	s.SetFactoryRole(t, beta, ada, f, domain.FactoryEditor)
	s.SetRole(t, alpha, ada, domain.OrgAdmin)

	got, err := id.OrgsFor(t.Context(), ada)
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		org       domain.OrgID
		role      domain.OrgRole
		factories int
	}{{alpha, domain.OrgAdmin, 3}, {beta, domain.OrgMember, 1}}

	if len(got) != len(want) {
		t.Fatalf("OrgsFor = %+v; want %+v", got, want)
	}

	for i, w := range want {
		if g := got[i]; g.Org.ID != w.org || g.Role != w.role || g.FactoryCount != w.factories || g.Org.Name == "" {
			t.Errorf("OrgsFor[%d] = %+v; want %s as %s seeing %d factories", i, g, w.org, w.role, w.factories)
		}
	}

	if none, err := id.OrgsFor(t.Context(), domain.UserID(domain.LocalUser.ID)); err != nil || len(none) != 0 {
		t.Errorf("OrgsFor of the local user = %+v, %v; want none", none, err)
	}
}
