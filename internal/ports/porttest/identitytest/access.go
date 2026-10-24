package identitytest

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// AccessSeed sets up what the cases of ports.Access need beyond Seed.
type AccessSeed interface {
	Seed
	// Invite adds an invitation of org by inviter for email with role and factory roles, in
	// status ("pending", "canceled", …), expiring at expires, and returns its id.
	Invite(t *testing.T, org domain.OrgID, inviter domain.UserID, email string, role domain.OrgRole,
		grants []domain.FactoryGrant, status string, expires time.Time) string
	// Accept has user accept the invitation as the identity service does: the invitation is
	// accepted, user becomes a member with its role, then its factory roles are applied.
	Accept(t *testing.T, invitation string, user domain.UserID)
}

// RunAccess runs every ports.Access case against a fresh implementation, its identity lookups
// (the same people) and its seed from setup.
func RunAccess(t *testing.T, setup func(t *testing.T) (ports.Identity, ports.Access, AccessSeed)) {
	t.Helper()

	cases := []struct {
		name string
		run  func(t *testing.T, id ports.Identity, a ports.Access, s AccessSeed)
	}{
		{"MembersAreTheOrgsPeopleByName", testMembers},
		{"InvitationsArePendingAndUnexpired", testInvitations},
		{"AcceptingAnInvitationAppliesItsFactoryRoles", testAccept},
		{"FactoryRolesAreGrantedChangedAndRemoved", testSetFactoryRole},
		{"FactoryRolesRefuseWhatTheyCannotGive", testSetFactoryRoleRefused},
		{"OwnershipGoesToAnAdminAtOnce", testTransfer},
		{"OwnershipStaysWhenTheTransferIsRefused", testTransferRefused},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			id, a, s := setup(t)
			c.run(t, id, a, s)
		})
	}
}

func members(t *testing.T, a ports.Access, org domain.OrgID) []domain.Member {
	t.Helper()

	ms, err := a.Members(domain.WithOrg(t.Context(), org))
	if err != nil {
		t.Fatalf("Members(%s) = %v", org, err)
	}

	return ms
}

func memberOf(ms []domain.Member, user domain.UserID) (domain.Member, bool) {
	i := slices.IndexFunc(ms, func(m domain.Member) bool { return m.User.ID == user })
	if i < 0 {
		return domain.Member{}, false
	}

	return ms[i], true
}

func testMembers(t *testing.T, _ ports.Identity, a ports.Access, s AccessSeed) {
	t.Helper()

	acme, other := s.Org(t, "Acme"), s.Org(t, "Other")
	f := s.Factory(t, acme)
	bob, ada := s.User(t, "Bob", "bob@example.com", false), s.User(t, adaName, adaEmail, false)
	cy := s.User(t, "Cy", "cy@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, acme, bob, domain.OrgMember)
	s.SetRole(t, other, cy, domain.OrgOwner)
	s.SetFactoryRole(t, acme, bob, f, domain.FactoryViewer)

	active := now().Add(-time.Hour)
	s.Session(t, bob, active.Add(-time.Hour), now().Add(time.Hour))
	s.Session(t, bob, active, now().Add(time.Hour))

	ms := members(t, a, acme)
	if len(ms) != 2 || ms[0].User.ID != ada || ms[1].User.ID != bob {
		t.Fatalf("members of acme = %+v; want Ada then Bob", ms)
	}

	checkOwnerNeverActive(t, ms[0])

	if m := ms[1]; m.Role != domain.OrgMember || !m.LastActiveAt.Equal(active) ||
		!slices.Equal(m.FactoryRoles, []domain.FactoryGrant{{Factory: f, Role: domain.FactoryViewer}}) {
		t.Errorf("Bob = %+v; want a member, viewer of %s, last active at %v", m, f, active)
	}

	if ms := members(t, a, other); len(ms) != 1 || ms[0].User.ID != cy {
		t.Errorf("members of the other org = %+v; want Cy alone", ms)
	}
}

// checkOwnerNeverActive checks Ada as Members lists her: the owner, never signed in, no factory roles.
func checkOwnerNeverActive(t *testing.T, m domain.Member) {
	t.Helper()

	person := m.User.Name == adaName && m.User.Email == adaEmail
	if m.ID == "" || !person || m.Role != domain.OrgOwner || len(m.FactoryRoles) != 0 || m.JoinedAt.IsZero() || !m.LastActiveAt.IsZero() {
		t.Errorf("Ada = %+v; want the owner, never active, without factory roles", m)
	}
}

func testInvitations(t *testing.T, _ ports.Identity, a ports.Access, s AccessSeed) {
	t.Helper()

	acme, other := s.Org(t, "Acme"), s.Org(t, "Other")
	f := s.Factory(t, acme)
	ada := s.User(t, adaName, adaEmail, false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, other, ada, domain.OrgOwner)

	later := now().Add(72 * time.Hour)
	grants := []domain.FactoryGrant{{Factory: f, Role: domain.FactoryApprover}}
	pending := s.Invite(t, acme, ada, "new@example.com", domain.OrgMember, grants, "pending", later)
	s.Invite(t, acme, ada, "admin@example.com", domain.OrgAdmin, nil, "pending", later)
	s.Invite(t, acme, ada, "gone@example.com", domain.OrgMember, nil, "canceled", later)
	s.Invite(t, acme, ada, "late@example.com", domain.OrgMember, nil, "pending", now().Add(-time.Minute))
	s.Invite(t, other, ada, "elsewhere@example.com", domain.OrgMember, nil, "pending", later)

	got, err := a.Invitations(domain.WithOrg(t.Context(), acme))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("invitations of acme = %+v; want the two pending, unexpired ones", got)
	}

	want := domain.Invitation{
		ID: pending, Email: "new@example.com", Role: domain.OrgMember, FactoryRoles: grants,
		Inviter: domain.User{ID: ada, Name: adaName}, CreatedAt: got[0].CreatedAt, ExpiresAt: later,
	}
	if i := got[0]; !reflect.DeepEqual(i, want) || i.CreatedAt.IsZero() {
		t.Errorf("first invitation = %+v; want %+v", i, want)
	}

	if i := got[1]; i.Email != "admin@example.com" || i.Role != domain.OrgAdmin || len(i.FactoryRoles) != 0 {
		t.Errorf("second invitation = %+v; want admin@example.com as an admin", i)
	}
}

func testAccept(t *testing.T, id ports.Identity, a ports.Access, s AccessSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	f, g := s.Factory(t, acme), s.Factory(t, acme)
	ada, dee := s.User(t, adaName, adaEmail, false), s.User(t, "Dee", "dee@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)

	grants := []domain.FactoryGrant{{Factory: f, Role: domain.FactoryViewer}, {Factory: g, Role: domain.FactoryEditor}}
	invitation := s.Invite(t, acme, ada, "dee@example.com", domain.OrgMember, grants, "pending", now().Add(time.Hour))
	token := s.Session(t, dee, now(), now().Add(time.Hour))
	s.Accept(t, invitation, dee)

	m, ok := memberOf(members(t, a, acme), dee)
	if !ok || m.Role != domain.OrgMember || len(m.FactoryRoles) != 2 {
		t.Fatalf("Dee after accepting = %+v (member %v); want a member with the invitation's two factory roles", m, ok)
	}

	if p := session(t, id, token, acme); p.FactoryRoles[f] != domain.FactoryViewer || p.FactoryRoles[g] != domain.FactoryEditor {
		t.Errorf("Dee's session in acme = %v; want viewer of %s and editor of %s", p.FactoryRoles, f, g)
	}

	invitations, err := a.Invitations(domain.WithOrg(t.Context(), acme))
	if err != nil || len(invitations) != 0 {
		t.Errorf("invitations after the acceptance = %+v, %v; want none pending", invitations, err)
	}
}

func testSetFactoryRole(t *testing.T, id ports.Identity, a ports.Access, s AccessSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	f := s.Factory(t, acme)
	ada, bob := s.User(t, adaName, adaEmail, false), s.User(t, "Bob", "bob@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, acme, bob, domain.OrgMember)
	token := s.Session(t, bob, now(), now().Add(time.Hour))
	by := domain.Actor{Kind: domain.ActorUser, ID: string(ada), Name: adaName}
	ctx := domain.WithOrg(t.Context(), acme)

	for _, step := range []struct {
		role domain.FactoryRole
		want []domain.FactoryGrant
	}{
		{domain.FactoryViewer, []domain.FactoryGrant{{Factory: f, Role: domain.FactoryViewer}}},
		{domain.FactoryEditor, []domain.FactoryGrant{{Factory: f, Role: domain.FactoryEditor}}},
		{domain.FactoryEditor, []domain.FactoryGrant{{Factory: f, Role: domain.FactoryEditor}}},
		{"", []domain.FactoryGrant{}},
		{"", []domain.FactoryGrant{}},
	} {
		if err := a.SetFactoryRole(ctx, by, bob, f, step.role); err != nil {
			t.Fatalf("SetFactoryRole(%q) = %v", step.role, err)
		}

		m, _ := memberOf(members(t, a, acme), bob)
		if !slices.Equal(append([]domain.FactoryGrant{}, m.FactoryRoles...), step.want) {
			t.Errorf("after SetFactoryRole(%q): %+v; want %+v", step.role, m.FactoryRoles, step.want)
		}

		if p := session(t, id, token, acme); p.FactoryRoles[f] != step.role {
			t.Errorf("after SetFactoryRole(%q): the session's role on %s is %q", step.role, f, p.FactoryRoles[f])
		}
	}
}

func testSetFactoryRoleRefused(t *testing.T, _ ports.Identity, a ports.Access, s AccessSeed) {
	t.Helper()

	acme, other := s.Org(t, "Acme"), s.Org(t, "Other")
	f, elsewhere := s.Factory(t, acme), s.Factory(t, other)
	ada, adm := s.User(t, adaName, adaEmail, false), s.User(t, "Adm", "adm@example.com", false)
	bob, cy := s.User(t, "Bob", "bob@example.com", false), s.User(t, "Cy", "cy@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, acme, adm, domain.OrgAdmin)
	s.SetRole(t, acme, bob, domain.OrgMember)
	s.SetRole(t, other, cy, domain.OrgOwner)

	by := domain.Actor{Kind: domain.ActorUser, ID: string(ada), Name: adaName}
	ctx := domain.WithOrg(t.Context(), acme)

	for _, c := range []struct {
		name string
		user domain.UserID
		f    domain.FactoryID
		role domain.FactoryRole
		want error
	}{
		{"an owner", ada, f, domain.FactoryViewer, domain.ErrRoleFromOrg},
		{"an admin", adm, f, domain.FactoryViewer, domain.ErrRoleFromOrg},
		{"someone of another org", cy, f, domain.FactoryViewer, ports.ErrNotFound},
		{"a user who is not one", "00000000-0000-7000-8000-00000000dead", f, domain.FactoryViewer, ports.ErrNotFound},
		{"another org's factory", bob, elsewhere, domain.FactoryViewer, ports.ErrNotFound},
		{"an unknown factory", bob, "00000000-0000-7000-8000-00000000beef", domain.FactoryViewer, ports.ErrNotFound},
		{"an unknown role", bob, f, "owner", domain.ErrInvalid},
	} {
		if err := a.SetFactoryRole(ctx, by, c.user, c.f, c.role); !errors.Is(err, c.want) {
			t.Errorf("%s: SetFactoryRole = %v; want %v", c.name, err, c.want)
		}
	}

	for _, m := range members(t, a, acme) {
		if len(m.FactoryRoles) != 0 {
			t.Errorf("%s holds %+v after the refusals; want nothing", m.User.Name, m.FactoryRoles)
		}
	}
}

func testTransfer(t *testing.T, id ports.Identity, a ports.Access, s AccessSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada, bob := s.User(t, adaName, adaEmail, false), s.User(t, "Bob", "bob@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, acme, bob, domain.OrgAdmin)
	adaToken, bobToken := s.Session(t, ada, now(), now().Add(time.Hour)), s.Session(t, bob, now(), now().Add(time.Hour))
	by := domain.Actor{Kind: domain.ActorUser, ID: string(ada), Name: adaName}

	if err := a.TransferOwnership(domain.WithOrg(t.Context(), acme), by, ada, bob); err != nil {
		t.Fatalf("TransferOwnership = %v", err)
	}

	if p := session(t, id, adaToken, acme); p.OrgRole != domain.OrgAdmin {
		t.Errorf("Ada after the transfer: %q; want admin", p.OrgRole)
	}

	if p := session(t, id, bobToken, acme); p.OrgRole != domain.OrgOwner {
		t.Errorf("Bob after the transfer: %q; want owner", p.OrgRole)
	}
}

func testTransferRefused(t *testing.T, _ ports.Identity, a ports.Access, s AccessSeed) {
	t.Helper()

	acme := s.Org(t, "Acme")
	ada, adm := s.User(t, adaName, adaEmail, false), s.User(t, "Adm", "adm@example.com", false)
	bob, cy := s.User(t, "Bob", "bob@example.com", false), s.User(t, "Cy", "cy@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, acme, adm, domain.OrgAdmin)
	s.SetRole(t, acme, bob, domain.OrgMember)

	by := domain.Actor{Kind: domain.ActorUser, ID: string(ada), Name: adaName}
	ctx := domain.WithOrg(t.Context(), acme)
	before := members(t, a, acme)

	for _, c := range []struct {
		name     string
		from, to domain.UserID
		want     error
	}{
		{"to a member", ada, bob, domain.ErrNotAnAdmin},
		{"to someone of no org", ada, cy, domain.ErrNotAnAdmin},
		{"to oneself", ada, ada, domain.ErrNotAnAdmin},
		{"from an admin", adm, adm, domain.ErrNotAnOwner},
		{"from a member", bob, adm, domain.ErrNotAnOwner},
	} {
		if err := a.TransferOwnership(ctx, by, c.from, c.to); !errors.Is(err, c.want) {
			t.Errorf("%s: TransferOwnership = %v; want %v", c.name, err, c.want)
		}
	}

	after := members(t, a, acme)
	for i := range before {
		if before[i].User.ID != after[i].User.ID || before[i].Role != after[i].Role {
			t.Errorf("roles after the refusals = %+v; want unchanged %+v", after, before)
		}
	}
}
