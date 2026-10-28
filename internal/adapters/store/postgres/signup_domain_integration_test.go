//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// signupDomain is acme's sign-up domain in these tests.
const signupDomain = "acme.example.com"

// userAt creates a Better Auth user with email, verified or not, and returns its id.
func (d identityDB) userAt(email string, verified bool) string {
	d.t.Helper()

	var id string
	if err := d.auth.QueryRow(d.t.Context(), `INSERT INTO auth."user" (name, email, "emailVerified") VALUES ('', $1, $2) RETURNING id::text`,
		email, verified).Scan(&id); err != nil {
		d.t.Fatal(err)
	}

	return id
}

// owner makes a user with email an owner of org and returns them as the actor they sign in as.
func (d identityDB) owner(org domain.OrgID, email string) domain.Actor {
	d.t.Helper()

	id := d.userAt(email, true)
	d.member(org, id, "owner")

	return domain.Actor{Kind: domain.ActorUser, ID: id, Name: email}
}

// An owner sets, reads, changes and clears the sign-up domain, each change audited and setting
// what is set auditing nothing.
func TestSignupDomain_SetReadAndClearRoundTrip(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme")
	ctx := domain.WithOrg(t.Context(), acme.ID)
	owner := d.owner(acme.ID, "f.cruz@"+signupDomain)

	if _, set, err := d.s.SignupDomain(ctx); err != nil || set {
		t.Fatalf("before = %v, %v; want none", set, err)
	}

	got, err := d.s.SetSignupDomain(ctx, owner, signupDomain, domain.FactoryViewer)
	if err != nil {
		t.Fatal(err)
	}

	if got.Domain != signupDomain || got.JoinRole != domain.FactoryViewer || got.SetBy.Ref() != owner.Ref() || got.SetAt.IsZero() {
		t.Errorf("set = %+v; want %s, viewer, by the owner", got, signupDomain)
	}

	if read, set, err := d.s.SignupDomain(ctx); err != nil || !set || read != got {
		t.Errorf("read = %+v, %v, %v; want %+v", read, set, err, got)
	}

	changeAndClear(ctx, t, d, owner)
	checkSignupAudit(ctx, t, d, owner)
}

// changeAndClear sets the round trip's domain again (no change), to approver, then clears it twice
// (the second clears nothing).
func changeAndClear(ctx context.Context, t *testing.T, d identityDB, owner domain.Actor) {
	t.Helper()

	if _, err := d.s.SetSignupDomain(ctx, owner, signupDomain, domain.FactoryViewer); err != nil {
		t.Fatal(err)
	}

	if got, err := d.s.SetSignupDomain(ctx, owner, signupDomain, domain.FactoryApprover); err != nil || got.JoinRole != domain.FactoryApprover {
		t.Fatalf("change = %+v, %v; want approver", got, err)
	}

	for range 2 {
		if err := d.s.ClearSignupDomain(ctx, owner); err != nil {
			t.Fatal(err)
		}
	}

	if _, set, err := d.s.SignupDomain(ctx); err != nil || set {
		t.Errorf("after clearing = %v, %v; want none", set, err)
	}
}

// checkSignupAudit checks the round trip's audit: two sets (the repeat audits nothing), the latest
// to approver by the owner, and one clear (clearing nothing audits nothing).
func checkSignupAudit(ctx context.Context, t *testing.T, d identityDB, owner domain.Actor) {
	t.Helper()

	set, err := d.s.AuditLog(ctx, ports.AuditQuery{Action: domain.AuditOrgSignupDomainSet, Limit: 10})
	if err != nil || len(set) != 2 || set[0].Data["join_role"] != string(domain.FactoryApprover) || set[0].Actor.ID != owner.ID {
		t.Errorf("%s entries = %+v, %v; want 2, the latest to approver by the owner", domain.AuditOrgSignupDomainSet, set, err)
	}

	cleared, err := d.s.AuditLog(ctx, ports.AuditQuery{Action: domain.AuditOrgSignupDomainCleared, Limit: 10})
	if err != nil || len(cleared) != 1 {
		t.Errorf("%s entries = %d, %v; want 1", domain.AuditOrgSignupDomainCleared, len(cleared), err)
	}
}

// goal_set_signup_domain's refusals reach the store as the sentinels the API answers, and change
// nothing.
func TestSignupDomain_RefusalsMapToTheDomainErrors(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme, labs := d.org("acme"), d.org("example-labs")
	ctx := domain.WithOrg(t.Context(), acme.ID)
	owner := d.owner(acme.ID, "f.cruz@"+signupDomain)
	webmail := d.owner(acme.ID, "j.doe@gmail.com")
	admin := domain.Actor{Kind: domain.ActorUser, ID: d.userAt("a.admin@"+signupDomain, true)}
	d.member(acme.ID, admin.ID, "admin")

	labsOwner := d.owner(labs.ID, "p.nair@"+signupDomain)
	if _, err := d.s.SetSignupDomain(domain.WithOrg(t.Context(), labs.ID), labsOwner, signupDomain, domain.FactoryViewer); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name   string
		by     domain.Actor
		domain string
		want   error
	}{
		{"another domain", owner, "example.org", domain.ErrSignupDomainNotYours},
		{"a subdomain of theirs", owner, "eu." + signupDomain, domain.ErrSignupDomainNotYours},
		{"the local user", domain.LocalUser, signupDomain, domain.ErrSignupDomainNotYours},
		{"a webmail domain", webmail, "gmail.com", domain.ErrSignupDomainPublic},
		{"an admin", admin, signupDomain, domain.ErrNotAnOwner},
		{"another live org's", owner, signupDomain, domain.ErrSignupDomainTaken},
	} {
		if _, err := d.s.SetSignupDomain(ctx, c.by, c.domain, domain.FactoryViewer); !errors.Is(err, c.want) {
			t.Errorf("%s: %v; want %v", c.name, err, c.want)
		}
	}

	if _, set, err := d.s.SignupDomain(ctx); err != nil || set {
		t.Errorf("acme's after the refusals = %v, %v; want none", set, err)
	}

	if err := d.s.ClearSignupDomain(ctx, admin); !errors.Is(err, domain.ErrNotAnOwner) {
		t.Errorf("an admin clearing: %v; want %v", err, domain.ErrNotAnOwner)
	}
}

// A deleted organization's claim neither admits anyone nor blocks a live one's, which takes it
// over.
func TestSignupDomain_ADeletedOrgsClaimIsTakenOver(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme, gone := d.org("acme"), d.org("gone")
	goneCtx := domain.WithOrg(t.Context(), gone.ID)

	if _, err := d.s.SetSignupDomain(goneCtx, d.owner(gone.ID, "p.nair@"+signupDomain), signupDomain, domain.FactoryViewer); err != nil {
		t.Fatal(err)
	}

	if err := d.s.DeleteOrg(goneCtx, domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	if orgs := d.signupOrgs("someone@" + signupDomain); len(orgs) != 0 {
		t.Errorf("a deleted organization's domain admits into %v; want nothing", orgs)
	}

	ctx := domain.WithOrg(t.Context(), acme.ID)
	if _, err := d.s.SetSignupDomain(ctx, d.owner(acme.ID, "f.cruz@"+signupDomain), signupDomain, domain.FactoryEditor); err != nil {
		t.Fatalf("taking over a deleted organization's domain: %v", err)
	}

	if orgs := d.signupOrgs("someone@" + signupDomain); len(orgs) != 1 || orgs[0] != string(acme.ID)+" editor" {
		t.Errorf("the domain admits into %v; want acme as editor", orgs)
	}
}

// Row-level security keeps each organization's row to itself: another organization's goald
// reads none.
func TestSignupDomain_RLSHidesAnotherOrgsRow(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme, labs := d.org("acme"), d.org("example-labs")

	if _, err := d.s.SetSignupDomain(domain.WithOrg(t.Context(), acme.ID), d.owner(acme.ID, "f.cruz@"+signupDomain), signupDomain,
		domain.FactoryViewer); err != nil {
		t.Fatal(err)
	}

	labsCtx := domain.WithOrg(t.Context(), labs.ID)
	if _, set, err := d.s.SignupDomain(labsCtx); err != nil || set {
		t.Errorf("example-labs' = %v, %v; want none", set, err)
	}

	var n int
	if err := pgx.BeginFunc(t.Context(), d.app, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `SELECT set_config('goal.org_id', $1, true)`, string(labs.ID)); err != nil {
			return err
		}

		return tx.QueryRow(t.Context(), `SELECT count(*) FROM org_signup_domains`).Scan(&n)
	}); err != nil || n != 0 {
		t.Errorf("rows example-labs' goald sees = %d, %v; want 0", n, err)
	}
}

// signupOrgs is what goal_signup_domain_org answers the auth service for email: "<org> <role>".
func (d identityDB) signupOrgs(email string) []string {
	d.t.Helper()

	rows, err := d.auth.Query(d.t.Context(), `SELECT org_id::text || ' ' || join_role FROM goal_signup_domain_org($1)`, email)
	if err != nil {
		d.t.Fatal(err)
	}

	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		d.t.Fatal(err)
	}

	return out
}

// goal_signup_domain_org admits exactly the domain of the part after the last @, case and
// surrounding spaces ignored: no subdomain, no longer name ending like it, nothing without an @.
func TestSignupDomain_TheAuthServiceMatchesTheDomainExactly(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme")

	if _, err := d.s.SetSignupDomain(domain.WithOrg(t.Context(), acme.ID), d.owner(acme.ID, "f.cruz@"+signupDomain), signupDomain,
		domain.FactoryViewer); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		email string
		want  int
	}{
		{"\tAda@ACME.example.com ", 1},
		{"ada@x@" + signupDomain, 1},
		{"ada@eu." + signupDomain, 0},
		{"ada@example.com", 0},
		{"ada@" + signupDomain + ".evil", 0},
		{"no-at-sign." + signupDomain, 0},
		{"ada@notacme.example.com", 0},
		{"ada@acme.example.com@gmail.com", 0},
	} {
		if got := d.signupOrgs(c.email); len(got) != c.want {
			t.Errorf("goal_signup_domain_org(%q) = %v; want %d rows", c.email, got, c.want)
		}
	}
}

// The auth service's side: goal_apply_signup_domain joins a verified user once, as a member with
// the join role on every factory, audited via signup-domain; it leaves an unverified user, an
// invitee of the organization and anyone elsewhere alone.
func TestSignupDomain_TheAuthServiceJoinsAtTheDomain(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme")
	ctx := domain.WithOrg(t.Context(), acme.ID)
	factory := saveFactory(ctx, t, d.s)
	owner := d.owner(acme.ID, "f.cruz@"+signupDomain)

	if _, err := d.s.SetSignupDomain(ctx, owner, signupDomain, domain.FactoryApprover); err != nil {
		t.Fatal(err)
	}

	apply := func(user string) *string {
		var org *string
		if err := d.auth.QueryRow(t.Context(), `SELECT goal_apply_signup_domain($1)::text`, user).Scan(&org); err != nil {
			t.Fatal(err)
		}

		return org
	}

	ada := d.userAt("ada@"+signupDomain, true)
	if org := apply(ada); org == nil || *org != string(acme.ID) {
		t.Fatalf("joining ada = %v; want acme", org)
	}

	if org := apply(ada); org != nil {
		t.Errorf("joining ada again = %v; want nothing", *org)
	}

	if role := d.roleOf(acme.ID, ada); role != "member" {
		t.Errorf("ada's role = %q; want member", role)
	}

	if n := d.count(`SELECT count(*) FROM factory_roles WHERE user_id = $1 AND factory_id = $2 AND role = 'approver'
		AND granted_by = 'system:signup-domain'`, ada, string(factory)); n != 1 {
		t.Errorf("ada's approver roles = %d; want 1", n)
	}

	if n := d.count(`SELECT count(*) FROM audit_log WHERE org_id = $1 AND target_id = $2 AND data->>'via' = 'signup-domain'
		AND action IN ('member.added', 'factory_role.granted')`, string(acme.ID), ada); n != 2 {
		t.Errorf("ada's audit entries via signup-domain = %d; want member.added and factory_role.granted", n)
	}

	unverified := d.userAt("bo@"+signupDomain, false)
	invitee := d.userAt("cy@"+signupDomain, true)
	d.invite(acme.ID, owner.ID, "cy@"+signupDomain, "pending")
	stranger := d.userAt("di@example.com", true)

	for name, user := range map[string]string{"an unverified user": unverified, "an invitee": invitee, "a stranger": stranger} {
		if org := apply(user); org != nil || d.roleOf(acme.ID, user) != "" {
			t.Errorf("%s joined %v; want nothing", name, org)
		}
	}

	if org := apply(uuid.NewString()); org != nil {
		t.Errorf("an unknown user joined %v; want nothing", *org)
	}
}
