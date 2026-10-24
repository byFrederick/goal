package porttest_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
	"github.com/byFrederick/goal/internal/ports/porttest/identitytest"
)

// fakeSeed sets up porttest.Identity for identitytest.
type fakeSeed struct {
	id *porttest.Identity
	n  *int
}

func (s fakeSeed) next(prefix string) string {
	*s.n++

	return fmt.Sprintf("%s-0000-7000-8000-%012d", prefix, *s.n)
}

func (s fakeSeed) Org(_ *testing.T, name string) domain.OrgID {
	id := domain.OrgID(s.next("019a3000"))
	s.id.AddOrg(domain.Org{ID: id, Slug: strings.ToLower(name), Name: name})

	return id
}

func (s fakeSeed) Factory(_ *testing.T, org domain.OrgID) domain.FactoryID {
	f := domain.FactoryID(s.next("019a3001"))
	s.id.AddFactory(org, f)

	return f
}

func (s fakeSeed) User(t *testing.T, name, email string, platformAdmin bool) domain.UserID {
	t.Helper()

	u := domain.UserID(s.next("019a3002"))
	if err := s.id.AddUser(porttest.IdentityUser{ID: u, Name: name, Email: email, PlatformAdmin: platformAdmin}); err != nil {
		t.Fatal(err)
	}

	return u
}

func (s fakeSeed) Session(_ *testing.T, user domain.UserID, created, expires time.Time) string {
	id := s.next("019a3003")
	token := fmt.Sprintf("tok%029d", *s.n)
	s.id.AddSession(token, user, domain.Session{ID: id, CreatedAt: created, ExpiresAt: expires})

	return token
}

func (s fakeSeed) EndSession(_ *testing.T, token string) { s.id.EndSession(token) }

func (s fakeSeed) SetRole(t *testing.T, org domain.OrgID, user domain.UserID, role domain.OrgRole) {
	t.Helper()

	if err := s.id.SetOrgRole(org, user, role); err != nil {
		t.Fatal(err)
	}
}

func (s fakeSeed) SetFactoryRole(t *testing.T, org domain.OrgID, user domain.UserID, f domain.FactoryID, role domain.FactoryRole) {
	t.Helper()

	if err := s.id.SetFactoryRole(org, user, f, role); err != nil {
		t.Fatal(err)
	}
}

func (s fakeSeed) DeleteOrg(_ *testing.T, org domain.OrgID) { s.id.DeleteOrg(org) }

func (s fakeSeed) ExpireKey(_ *testing.T, _ domain.OrgID, id domain.APIKeyID) { s.id.ExpireKey(id) }

func TestIdentity_APIKeysConformance(t *testing.T) {
	t.Parallel()

	identitytest.RunAPIKeys(t, func(*testing.T) (ports.Identity, ports.Access, identitytest.KeySeed) {
		id := porttest.NewIdentity(time.Now)

		return id, id, fakeSeed{id: id, n: new(int)}
	})
}

func TestIdentity_Conformance(t *testing.T) {
	t.Parallel()

	identitytest.Run(t, func(*testing.T) (ports.Identity, identitytest.Seed) {
		id := porttest.NewIdentity(time.Now)

		return id, fakeSeed{id: id, n: new(int)}
	})
}

func TestPlatform_Conformance(t *testing.T) {
	t.Parallel()

	identitytest.RunPlatform(t, func(*testing.T) (ports.Platform, identitytest.Seed) {
		id := porttest.NewIdentity(time.Now)

		return porttest.NewPlatform(id), fakeSeed{id: id, n: new(int)}
	})
}
