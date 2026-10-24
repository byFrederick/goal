package porttest_test

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const (
	idAcme  domain.OrgID     = "019a2000-0000-7000-8000-0000000000a1"
	idBeta  domain.OrgID     = "019a2000-0000-7000-8000-0000000000b1"
	idAda   domain.UserID    = "019a2000-0000-7000-8000-000000000001"
	idGrace domain.UserID    = "019a2000-0000-7000-8000-000000000002"
	idF     domain.FactoryID = "019a2000-0000-7000-8000-0000000000f1"
)

var idNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// newIdentity is two organizations (A before B by name) and Ada, an approver of idF in the
// first, signed in for an hour, whose org has one API key, acme-key.
func newIdentity(t *testing.T, clock *porttest.Clock) *porttest.Identity {
	t.Helper()

	id := porttest.NewIdentity(clock.Now)
	id.AddOrg(domain.Org{ID: idAcme, Name: "A org"})
	id.AddOrg(domain.Org{ID: idBeta, Name: "B org"})

	must := func(err error) {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}
	}

	must(id.AddUser(porttest.IdentityUser{ID: idAda, Name: "Ada", Email: "ada@example.com"}))
	id.AddSession("ada-session", idAda, domain.Session{ID: "s", CreatedAt: idNow, ExpiresAt: idNow.Add(time.Hour)})
	must(id.SetOrgRole(idAcme, idAda, domain.OrgMember))
	must(id.GrantFactoryRole(idAcme, idAda, idF, domain.FactoryApprover))
	id.AddKey("acme-key", porttest.IdentityKey{ID: "k1", Org: idAcme, Name: "ci", ExpiresAt: idNow.Add(48 * time.Hour)})

	return id
}

func TestIdentity_SessionCarriesTheRolesInTheOrgAsked(t *testing.T) {
	t.Parallel()

	id := newIdentity(t, porttest.NewClock(idNow))

	p, err := id.Session(t.Context(), "ada-session", idAcme)
	if err != nil || p.OrgRole != domain.OrgMember || p.FactoryRoles[idF] != domain.FactoryApprover || p.Actor().ID != string(idAda) {
		t.Errorf("member's session = %+v, %v; want the member with their factory role", p, err)
	}

	if p, err := id.Session(t.Context(), "ada-session", idBeta); err != nil || p.OrgRole != "" || len(p.FactoryRoles) != 0 {
		t.Errorf("session in an org Ada is not in = %+v, %v; want no role", p, err)
	}

	if _, err := id.Session(t.Context(), "nobody", idAcme); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("unknown token: %v, want ErrUnauthenticated", err)
	}
}

func TestIdentity_SessionsAndKeysExpire(t *testing.T) {
	t.Parallel()

	clock := porttest.NewClock(idNow)
	id := newIdentity(t, clock)
	id.AddKey("revoked", porttest.IdentityKey{ID: "k2", Org: idAcme, ExpiresAt: idNow.Add(time.Hour)})
	id.RevokeKey("k2")

	if _, err := id.APIKey(t.Context(), "revoked"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("revoked key: %v, want ErrUnauthenticated", err)
	}

	clock.Advance(48 * time.Hour)

	if _, err := id.Session(t.Context(), "ada-session", ""); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expired session: %v, want ErrUnauthenticated", err)
	}

	if _, err := id.APIKey(t.Context(), "acme-key"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expired key: %v, want ErrUnauthenticated", err)
	}
}

func TestIdentity_ADeletedOrgEndsRolesAndKeys(t *testing.T) {
	t.Parallel()

	id := newIdentity(t, porttest.NewClock(idNow))
	id.DeleteOrg(idAcme)

	if p, err := id.Session(t.Context(), "ada-session", idAcme); err != nil || p.OrgRole != "" {
		t.Errorf("session in a deleted org = %+v, %v; want no role", p, err)
	}

	if _, err := id.APIKey(t.Context(), "acme-key"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("key of a deleted org: %v, want ErrUnauthenticated", err)
	}

	if orgs, err := id.OrgsFor(t.Context(), idAda); err != nil || len(orgs) != 0 {
		t.Errorf("OrgsFor = %+v, %v; want none", orgs, err)
	}
}

func TestIdentity_KeyUseIsRecordedAtMostOnceAMinute(t *testing.T) {
	t.Parallel()

	clock := porttest.NewClock(idNow)
	id := newIdentity(t, clock)

	for _, after := range []time.Duration{0, 30 * time.Second} {
		clock.Advance(after)

		if p, err := id.APIKey(t.Context(), "acme-key"); err != nil || p.Org != idAcme || p.Kind != domain.PrincipalService {
			t.Fatalf("live key = %+v, %v; want its org's service principal", p, err)
		}
	}

	if k, _ := id.Key("acme-key"); !k.LastUsedAt.Equal(idNow) {
		t.Errorf("last used %s after a second use within the minute, want %s", k.LastUsedAt, idNow)
	}
}

func TestIdentity_KeepsTheLastOwner(t *testing.T) {
	t.Parallel()

	id := newIdentity(t, porttest.NewClock(idNow))
	if err := id.AddUser(porttest.IdentityUser{ID: idGrace}); err != nil {
		t.Fatal(err)
	}

	if err := id.SetOrgRole(idBeta, idAda, domain.OrgOwner); err != nil {
		t.Fatal(err)
	}

	for _, role := range []domain.OrgRole{domain.OrgAdmin, ""} {
		if err := id.SetOrgRole(idBeta, idAda, role); !errors.Is(err, ports.ErrConflict) {
			t.Errorf("making the last owner %q: %v, want ErrConflict", role, err)
		}
	}

	if err := id.SetOrgRole(idBeta, idGrace, domain.OrgOwner); err != nil {
		t.Fatal(err)
	}

	if err := id.SetOrgRole(idBeta, idAda, domain.OrgAdmin); err != nil {
		t.Errorf("demoting an owner who is not the last: %v", err)
	}

	orgs, err := id.OrgsFor(t.Context(), idAda)
	if err != nil || len(orgs) != 2 || orgs[0].Org.ID != idAcme || orgs[1].Role != domain.OrgAdmin {
		t.Errorf("OrgsFor = %+v, %v; want A as member, then B as admin", orgs, err)
	}
}

func TestIdentity_KeepsTheLastPlatformAdmin(t *testing.T) {
	t.Parallel()

	id := porttest.NewIdentity(time.Now)
	if err := id.AddUser(porttest.IdentityUser{ID: idAda, PlatformAdmin: true}); err != nil {
		t.Fatal(err)
	}

	if err := id.AddUser(porttest.IdentityUser{ID: idAda}); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("removing the last platform admin: %v, want ErrConflict", err)
	}
}

func TestIdentity_DeviceRequestsExpire(t *testing.T) {
	t.Parallel()

	clock := porttest.NewClock(idNow)
	id := porttest.NewIdentity(clock.Now)
	id.AddDeviceRequest("ABCD-EFGH", domain.DeviceRequest{ClientID: "goal-cli", RequestedAt: idNow, ExpiresAt: idNow.Add(10 * time.Minute)})

	if d, err := id.DeviceRequest(t.Context(), "ABCD-EFGH"); err != nil || d.ClientID != "goal-cli" {
		t.Errorf("pending request = %+v, %v", d, err)
	}

	clock.Advance(10 * time.Minute)

	if _, err := id.DeviceRequest(t.Context(), "ABCD-EFGH"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("expired request: %v, want ErrNotFound", err)
	}
}
