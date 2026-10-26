//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/identitytest"
)

func TestPlatform_Conformance(t *testing.T) {
	t.Parallel()

	identitytest.RunPlatform(t, func(t *testing.T) (ports.Platform, identitytest.Seed) {
		t.Helper()

		d := newIdentityDB(t)

		return NewPlatform(d.s), pgSeed{d: d}
	})
}

// The clean-up deletes what each of Better Auth's tables and the security log no longer need,
// and one goald cleans up at a time: another's finds the lock taken and does nothing.
func TestPlatform_CleanUpAuth(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	p := NewPlatform(d.s)
	now := time.Now().UTC()
	ada := d.user("Ada")

	d.exec(d.auth, `INSERT INTO auth.verification (identifier, value, "expiresAt") VALUES ('old', 'x', $1), ('new', 'y', $2)`,
		now.Add(-time.Minute), now.Add(time.Minute))
	d.exec(d.auth, `INSERT INTO auth."deviceCode" ("deviceCode", "userCode", "expiresAt", status) VALUES
		('d-old', 'OLDCODE1', $1, 'pending'), ('d-new', 'NEWCODE1', $2, 'pending')`, now.Add(-time.Minute), now.Add(time.Minute))
	d.exec(d.auth, `INSERT INTO auth."rateLimit" (key, count, "lastRequest") VALUES ('idle', 1, $1), ('busy', 1, $2)`,
		now.Add(-25*time.Hour).UnixMilli(), now.UnixMilli())
	d.exec(d.auth, `INSERT INTO auth.session (token, "userId", "createdAt", "updatedAt", "expiresAt") VALUES
		('OldOldOldOldOldOldOldOldOldOld01', $1, $2, $2, $3), ('NewNewNewNewNewNewNewNewNewNew01', $1, $2, $2, $4)`,
		ada, now.Add(-72*time.Hour), now.Add(-48*time.Hour), now.Add(time.Hour))
	d.exec(d.admin, `INSERT INTO auth_events (kind, at) VALUES ('otp.sent', $1), ('otp.sent', $2)`, now.Add(-91*24*time.Hour), now)

	// Another goald holds the clean-up's lock: this one skips.
	held, err := d.admin.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	tx, err := held.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(hashtext('goal.auth.cleanup'))`); err != nil {
		t.Fatal(err)
	}

	if got, err := p.CleanUpAuth(t.Context(), now); err != nil || !got.Skipped || got.Sessions != 0 {
		t.Errorf("CleanUpAuth while another holds the lock = %+v, %v; want skipped", got, err)
	}

	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}

	held.Release()

	got, err := p.CleanUpAuth(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}

	if want := (ports.AuthCleanUp{Sessions: 1, Verifications: 1, DeviceCodes: 1, RateLimits: 1, AuthEvents: 1}); got != want {
		t.Errorf("CleanUpAuth = %+v; want %+v", got, want)
	}

	left := d.count(`SELECT (SELECT count(*) FROM auth.verification) + (SELECT count(*) FROM auth."deviceCode") +
		(SELECT count(*) FROM auth."rateLimit") + (SELECT count(*) FROM auth.session) + (SELECT count(*) FROM auth_events)`)
	if left != 5 {
		t.Errorf("%d rows left; want the five still needed", left)
	}
}

// identityOf is the person a session of user resolves to in org: whom signing in by code makes.
func (d identityDB) identityOf(user domain.UserID, org domain.OrgID) domain.Principal {
	d.t.Helper()

	token, at := sessionToken(d.t), time.Now()
	d.exec(d.auth, `INSERT INTO auth.session (token, "userId", "createdAt", "updatedAt", "expiresAt") VALUES ($1, $2, $3, $3, $4)`,
		token, string(user), at, at.Add(24*time.Hour))

	p, err := NewIdentity(d.s).Session(d.t.Context(), token, org)
	if err != nil {
		d.t.Fatal(err)
	}

	return p
}

// authEvents are the security log's events of kind, newest first.
func (d identityDB) authEvents(kind string) []domain.AuthEvent {
	d.t.Helper()

	got, err := NewPlatform(d.s).AuthEvents(d.t.Context(), ports.AuthEventQuery{Kind: kind})
	if err != nil {
		d.t.Fatal(err)
	}

	return got
}

// checkOneEvent checks the security log has one event of kind, about user, whose data holds
// want.
func (d identityDB) checkOneEvent(kind string, user domain.UserID, want map[string]any) {
	d.t.Helper()

	got := d.authEvents(kind)
	if len(got) != 1 || got[0].User != user {
		d.t.Errorf("%s = %+v; want one about %s", kind, got, user)

		return
	}

	for k, v := range want {
		if got[0].Data[k] != v {
			d.t.Errorf("%s data %v; want %s = %v", kind, got[0].Data, k, v)
		}
	}
}

// recoveredAdmin checks r's person is the one platform admin, added by system:recovery, and
// signs in as one.
func (d identityDB) recoveredAdmin(r Recovery) {
	d.t.Helper()

	admins, err := NewPlatform(d.s).PlatformAdmins(d.t.Context())
	if err != nil || len(admins) != 1 || admins[0].User.ID != r.User.ID || admins[0].AddedBy.Ref() != recoveryActor {
		d.t.Errorf("platform admins = %+v, %v; want the recovered person, added by %s", admins, err, recoveryActor)
	}

	if p := d.identityOf(r.User.ID, ""); !p.PlatformAdmin {
		d.t.Errorf("the recovered person signs in as %+v; want a platform admin", p)
	}
}

// Spec 3 §9, §11.1 row 37: goal db recover-admin needs only the database. It creates a person
// nobody has the email of (verified, so they sign in by code), makes them a platform admin by
// system:recovery and records it; an existing person is found by any case of their email.
func TestRecoverAdmin(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	d.user("Ada")

	const email = "lost.owner@example.com"

	r, err := RecoverAdmin(t.Context(), d.admin, "  Lost.Owner@Example.com ")
	if err != nil {
		t.Fatal(err)
	}

	if !r.Created || r.User.Email != email || r.User.Name != email ||
		d.count(`SELECT count(*) FROM auth."user" WHERE id = $1 AND "emailVerified"`, string(r.User.ID)) != 1 {
		t.Errorf("recovery = %+v; want a new person named by their verified email", r)
	}

	d.recoveredAdmin(r)
	d.checkOneEvent("recovery.used", r.User.ID, map[string]any{"command": "recover-admin", "created_user": true})
	d.checkOneEvent("platform_admin.added", r.User.ID, map[string]any{"by": recoveryActor})
	d.checkOneEvent("user.created", r.User.ID, map[string]any{"method": "recovery"})

	if used := d.authEvents("recovery.used"); len(used) != 1 || used[0].Subject != emailSubject(email) {
		t.Errorf("recovery.used = %+v; want the email's subject, not the email", used)
	}

	again, err := RecoverAdmin(t.Context(), d.admin, "LOST.OWNER@example.com")
	if err != nil || again.Created || again.User.ID != r.User.ID || d.count(`SELECT count(*) FROM auth."user"`) != 2 {
		t.Errorf("recovering again = %+v, %v; want the same person, not created", again, err)
	}
}

// checkRecoveryLog checks acme's audit log ends with the two recoveries: the new owner's
// member.added {via: recovery} and the admin's promotion, both by system:recovery.
func (d identityDB) checkRecoveryLog(org domain.OrgID) {
	d.t.Helper()

	log := d.logOf(org)
	if len(log) < 2 {
		d.t.Fatalf("audit = %+v; want the two recoveries last", log)
	}

	added, changed := log[len(log)-2], log[len(log)-1]
	if added.action != auditMemberAdded || added.actor != recoveryActor || added.data["via"] != "recovery" ||
		added.data["role"] != string(domain.OrgOwner) {
		d.t.Errorf("first recovery's entry = %+v; want member.added by %s, via recovery", added, recoveryActor)
	}

	if changed.action != "member.role_changed" || changed.actor != recoveryActor || changed.data["to"] != string(domain.OrgOwner) {
		d.t.Errorf("second recovery's entry = %+v; want member.role_changed to owner by %s", changed, recoveryActor)
	}
}

// recoveredOwner checks r created its person, an owner of org who was no member before and signs
// in as its owner, not as a platform admin.
func (d identityDB) recoveredOwner(r Recovery, org domain.OrgID) {
	d.t.Helper()

	if !r.Created || r.Org.ID != org || r.Was != "" || d.roleOf(org, string(r.User.ID)) != string(domain.OrgOwner) {
		d.t.Errorf("recovery = %+v; want a new person, owner of %s", r, org)
	}

	if p := d.identityOf(r.User.ID, org); p.OrgRole != domain.OrgOwner || p.PlatformAdmin {
		d.t.Errorf("the recovered owner signs in as %+v; want the owner, not a platform admin", p)
	}
}

// Spec 3 §9: goal db recover-owner makes a person an owner of a live organization, stamped by
// system:recovery: a new member is audited as member.added {via: recovery}, a member's promotion
// as member.role_changed.
func TestRecoverOwner(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme-" + randomHex(t)) // created before anyone signed in: no owner
	admin := domain.UserID(d.user("Admin"))
	d.member(acme.ID, string(admin), string(domain.OrgAdmin))

	r, err := RecoverOwner(t.Context(), d.admin, acme.Slug, "new.owner@example.com")
	if err != nil {
		t.Fatal(err)
	}

	d.recoveredOwner(r, acme.ID)

	promoted, err := RecoverOwner(t.Context(), d.admin, string(acme.ID), d.email(string(admin)))
	if err != nil || promoted.Created || promoted.Was != domain.OrgAdmin || d.roleOf(acme.ID, string(admin)) != string(domain.OrgOwner) {
		t.Errorf("promoting the admin = %+v, %v; want them owner, found by id", promoted, err)
	}

	d.checkRecoveryLog(acme.ID)

	used := d.authEvents("recovery.used")
	if len(used) != 2 || used[0].Data["was"] != string(domain.OrgAdmin) || used[1].Data["org_id"] != string(acme.ID) {
		t.Errorf("recovery.used = %+v; want both recoveries, newest first", used)
	}
}

// An unknown or deleted organization, a malformed email or a database goald never migrated
// changes nothing.
func TestRecover_Refusals(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)

	gone := d.org("gone-" + randomHex(t))
	if err := d.s.DeleteOrg(domain.WithOrg(t.Context(), gone.ID), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{gone.Slug, "nowhere"} {
		if _, err := RecoverOwner(t.Context(), d.admin, ref, "stranger@example.com"); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("recover-owner of %s = %v; want ErrNotFound", ref, err)
		}
	}

	if n := d.count(`SELECT count(*) FROM auth."user"`); n != 0 {
		t.Errorf("a refused recovery left %d users; want none", n)
	}

	for _, email := range []string{"", "not an email", "Ada <ada@example.com>"} {
		if _, err := RecoverAdmin(t.Context(), d.admin, email); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("recover-admin %q = %v; want ErrInvalid", email, err)
		}
	}

	bare := newTestDB(t, OwnerRole)
	if _, err := RecoverAdmin(t.Context(), openPool(t, bare.admin), "ada@example.com"); !errors.Is(err, ErrNotMigrated) {
		t.Errorf("recover-admin before the migrations = %v; want ErrNotMigrated", err)
	}
}
