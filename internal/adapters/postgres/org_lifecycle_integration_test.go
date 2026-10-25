//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// identityDB is a migrated test database with a pool per role: the store as goal_app, Better
// Auth's writes as goal_auth (the identity service's role) and checks as the bootstrap superuser.
type identityDB struct {
	t                *testing.T
	admin, app, auth *pgxpool.Pool
	s                *Store
}

func newIdentityDB(t *testing.T) identityDB {
	t.Helper()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)
	app := openPool(t, db.app)

	return identityDB{t: t, admin: openPool(t, db.admin), app: app, auth: openPool(t, db.auth), s: New(app)}
}

// org creates an organization through the Tenancy port.
func (d identityDB) org(slug string) domain.Org {
	d.t.Helper()

	o, err := d.s.CreateOrg(d.t.Context(), domain.LocalUser, newOrg(slug, slug))
	if err != nil {
		d.t.Fatal(err)
	}

	return o
}

// user creates a Better Auth user as the identity service does and returns its id.
func (d identityDB) user(name string) string {
	d.t.Helper()

	var id string
	if err := d.auth.QueryRow(d.t.Context(), `INSERT INTO auth."user" (name, email, "emailVerified")
		VALUES ($1, $2, true) RETURNING id::text`, name, name+"-"+randomHex(d.t)+"@example.com").Scan(&id); err != nil {
		d.t.Fatal(err)
	}

	return id
}

// member makes user a member of org with role, as Better Auth's accepted invitation does.
func (d identityDB) member(org domain.OrgID, user, role string) {
	d.t.Helper()

	d.exec(d.auth, `INSERT INTO auth.member ("organizationId", "userId", role, "createdAt") VALUES ($1, $2, $3, now())`,
		string(org), user, role)
}

// invite creates an invitation of org from inviter, as Better Auth's invite-member does.
func (d identityDB) invite(org domain.OrgID, inviter, email, status string) string {
	d.t.Helper()

	var id string
	if err := d.auth.QueryRow(d.t.Context(), `INSERT INTO auth.invitation ("organizationId", email, role, status, "expiresAt", "inviterId")
		VALUES ($1, $2, 'member', $3, now() + interval '3 days', $4) RETURNING id::text`, string(org), email, status, inviter).Scan(&id); err != nil {
		d.t.Fatal(err)
	}

	return id
}

func (d identityDB) exec(pool *pgxpool.Pool, sql string, args ...any) {
	d.t.Helper()

	if _, err := pool.Exec(d.t.Context(), sql, args...); err != nil {
		d.t.Fatalf("%s: %v", sql, err)
	}
}

// count counts rows as the superuser.
func (d identityDB) count(sql string, args ...any) int {
	d.t.Helper()

	var n int
	if err := d.admin.QueryRow(d.t.Context(), sql, args...).Scan(&n); err != nil {
		d.t.Fatalf("%s: %v", sql, err)
	}

	return n
}

// roleOf is user's role in org ("" when not a member).
func (d identityDB) roleOf(org domain.OrgID, user string) string {
	d.t.Helper()

	var role string

	err := d.admin.QueryRow(d.t.Context(), `SELECT role FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`,
		string(org), user).Scan(&role)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		d.t.Fatal(err)
	}

	return role
}

// expectCheckViolation expects err to be a check_violation naming constraint.
func expectCheckViolation(t *testing.T, what string, err error, constraint string) {
	t.Helper()

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != checkViolation || pgErr.ConstraintName != constraint {
		t.Errorf("%s = %v; want check_violation %s", what, err, constraint)
	}
}

// Spec 3 §11.1 row 17: an organization and its auth.organization row are written together or not
// at all, and only goal_provision_org writes them.
func TestProvisionOrg_Atomic(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme")

	var name, slug string

	var created time.Time
	if err := d.admin.QueryRow(t.Context(), `SELECT name, slug, "createdAt" FROM auth.organization WHERE id = $1`, string(acme.ID)).
		Scan(&name, &slug, &created); err != nil || name != acme.Name || slug != acme.Slug || !created.Equal(acme.CreatedAt) {
		t.Errorf("auth.organization of acme = %q %q %v, %v; want %q %q %v", name, slug, created, err, acme.Name, acme.Slug, acme.CreatedAt)
	}

	// A slug Better Auth already holds (which nothing but drift could cause) refuses the whole
	// create: no organizations row, no audit entry.
	d.exec(d.admin, `INSERT INTO auth.organization (id, name, slug, "createdAt") VALUES (goal_uuidv7(), 'Labs', 'labs', now())`)

	if _, err := d.s.CreateOrg(t.Context(), domain.LocalUser, newOrg("labs", "Labs")); !errors.Is(err, domain.ErrSlugTaken) {
		t.Errorf("CreateOrg with a slug auth.organization holds = %v; want ErrSlugTaken", err)
	}

	if n := d.count(`SELECT count(*) FROM organizations WHERE slug = 'labs'`); n != 0 {
		t.Errorf("organizations named labs after the refusal = %d; want 0", n)
	}

	if n := d.count(`SELECT count(*) FROM audit_log a WHERE NOT EXISTS (SELECT 1 FROM organizations o WHERE o.id = a.org_id)`); n != 0 {
		t.Errorf("audit entries without an organization = %d; want 0", n)
	}

	// goal_app cannot insert an organization without its Better Auth row.
	if _, err := d.app.Exec(t.Context(), `INSERT INTO organizations (id, slug, name, mark) VALUES (goal_uuidv7(), 'rogue', 'Rogue', 'Ro')`); err == nil {
		t.Error("goal_app inserted an organization directly; want permission denied")
	}
}

// With an owner (3-9 passes one), the owner's membership is created with the organization,
// stamped with the creator, and org.created names the creator by the user's name.
func TestProvisionOrg_WithAnOwner(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner := d.user(nameCruz)
	member, id := newUUID(t), newUUID(t)
	by := "user:" + owner

	err := inOrgTx(t, d.app, domain.OrgID(id), func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `SELECT * FROM goal_provision_org($1, 'owned', 'Owned', 'Ow', $2, $3, $4, NULL)`, id, owner, member, by)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	var memberID, changedBy string
	if err := d.admin.QueryRow(t.Context(), `SELECT id::text, "changedBy" FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2 AND role = 'owner'`,
		id, owner).Scan(&memberID, &changedBy); err != nil || memberID != member || changedBy != by {
		t.Errorf("owner of the provisioned org = %s by %s, %v; want member %s by %s", memberID, changedBy, err, member, by)
	}

	entries, err := d.s.AuditLog(domain.WithOrg(t.Context(), domain.OrgID(id)), ports.AuditQuery{Action: domain.AuditOrgCreated, Limit: 10})
	if want := (domain.Actor{Kind: domain.ActorUser, ID: owner, Name: nameCruz}); err != nil || len(entries) != 1 || entries[0].Actor != want {
		t.Errorf("org.created of the provisioned org = %+v, %v; want one by %+v (the user's name)", entries, err, want)
	}
}

// Renaming an organization renames it in Better Auth in the same transaction.
func TestUpdateOrg_RenameSyncsBetterAuth(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme")
	labs := d.org("labs")

	if _, err := d.s.UpdateOrg(domain.WithOrg(t.Context(), acme.ID), domain.LocalUser,
		ports.OrgUpdate{Slug: new("acme-co"), Name: new("Acme Co")}); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[domain.OrgID]string{acme.ID: "acme-co|Acme Co", labs.ID: "labs|labs"} {
		var got string
		if err := d.admin.QueryRow(t.Context(), `SELECT slug || '|' || name FROM auth.organization WHERE id = $1`, string(id)).
			Scan(&got); err != nil || got != want {
			t.Errorf("auth.organization %s = %q, %v; want %q", id, got, err, want)
		}
	}
}

// Soft-deleting an organization cancels its pending invitations, stamped system:org-deleted,
// and leaves answered ones and other organizations' alone.
func TestDeleteOrg_CancelsPendingInvitations(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme, labs := d.org("acme"), d.org("labs")
	inviter := d.user("M. Okafor")
	d.member(acme.ID, inviter, "owner")
	d.member(labs.ID, inviter, "owner")

	pending := d.invite(acme.ID, inviter, "a@example.com", "pending")
	accepted := d.invite(acme.ID, inviter, "b@example.com", "accepted")
	other := d.invite(labs.ID, inviter, "c@example.com", "pending")

	if err := d.s.DeleteOrg(domain.WithOrg(t.Context(), acme.ID), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]string{pending: "canceled system:org-deleted", accepted: "accepted -", other: "pending -"} {
		var got string
		if err := d.admin.QueryRow(t.Context(), `SELECT status || ' ' || coalesce("changedBy", '-') FROM auth.invitation WHERE id = $1`, id).
			Scan(&got); err != nil || got != want {
			t.Errorf("invitation %s = %q, %v; want %q", id, got, err, want)
		}
	}
}

// The purge deletes the organization's Better Auth rows with Goal's, owners included, and no
// other organization's.
func TestPurgeOrg_RemovesBetterAuthRows(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme, labs := d.org("acme"), d.org("labs")
	owner := d.user(nameCruz)

	for _, o := range []domain.Org{acme, labs} {
		d.member(o.ID, owner, "owner")
		d.invite(o.ID, owner, "a@example.com", "pending")
	}

	deleted := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	if err := d.s.DeleteOrg(domain.WithOrg(t.Context(), acme.ID), domain.LocalUser, deleted); err != nil {
		t.Fatal(err)
	}

	if ok, err := d.s.PurgeOrg(t.Context(), acme.ID, deleted.Add(time.Second), nil); !ok || err != nil {
		t.Fatalf("PurgeOrg = %v, %v; want purged", ok, err)
	}

	for _, table := range []string{`auth.member WHERE "organizationId"`, `auth.invitation WHERE "organizationId"`, `auth.organization WHERE id`} {
		for id, want := range map[domain.OrgID]int{acme.ID: 0, labs.ID: 1} {
			if n := d.count(`SELECT count(*) FROM `+table+` = $1`, string(id)); n != want {
				t.Errorf("%s = %s: %d rows after purging acme; want %d", table, id, n, want)
			}
		}
	}
}

// Spec 3 §11.1 row 16: an organization keeps an owner, also when two owners demote each other
// at the same moment (each transaction alone would leave the other owner).
func TestMember_OrgKeepsAnOwner(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme")
	a, b := d.user("A. Owner"), d.user("B. Owner")
	d.member(acme.ID, a, "owner")
	d.member(acme.ID, b, "owner")

	const demote = `UPDATE auth.member SET role = 'admin' WHERE "organizationId" = $1 AND "userId" = $2`

	for round := range 3 {
		d.exec(d.admin, `UPDATE auth.member SET role = 'owner' WHERE "organizationId" = $1`, string(acme.ID))

		errs := make(chan error, 2)
		txs := make([]pgx.Tx, 0, 2)

		for _, user := range []string{a, b} {
			tx, err := d.auth.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if _, err := tx.Exec(t.Context(), demote, string(acme.ID), user); err != nil {
				t.Fatal(err)
			}

			txs = append(txs, tx)
		}

		for _, tx := range txs {
			go func() { errs <- tx.Commit(context.WithoutCancel(t.Context())) }()
		}

		var failed []error

		for range txs {
			if err := <-errs; err != nil {
				failed = append(failed, err)
			}
		}

		if len(failed) != 1 {
			t.Fatalf("round %d: two concurrent demotions failed %v; want exactly one refused", round, failed)
		}

		expectCheckViolation(t, fmt.Sprintf("round %d: the second demotion", round), failed[0], "member_keeps_an_owner")

		if n := d.count(`SELECT count(*) FROM auth.member WHERE "organizationId" = $1 AND role = 'owner'`, string(acme.ID)); n != 1 {
			t.Errorf("round %d: owners after the demotions = %d; want 1", round, n)
		}
	}

	// The last owner can be neither demoted nor removed; another owner can.
	last := a
	if d.roleOf(acme.ID, a) != "owner" {
		last = b
	}

	_, err := d.auth.Exec(t.Context(), demote, string(acme.ID), last)
	expectCheckViolation(t, "demoting the last owner", err, "member_keeps_an_owner")

	_, err = d.auth.Exec(t.Context(), `DELETE FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`, string(acme.ID), last)
	expectCheckViolation(t, "removing the last owner", err, "member_keeps_an_owner")

	other := d.user("C. Owner")
	d.member(acme.ID, other, "owner")
	d.exec(d.auth, `DELETE FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`, string(acme.ID), other)
}

// goal_transfer_ownership hands the transaction's organization from an owner to an admin and
// audits it; anyone else is refused, naming the rule.
func TestTransferOwnership_OwnerToAdmin(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme")
	owner, admin, member := d.user(nameCruz), d.user("M. Okafor"), d.user("J. Lindqvist")
	d.member(acme.ID, owner, "owner")
	d.member(acme.ID, admin, "admin")
	d.member(acme.ID, member, roleMember)

	transfer := func(org domain.OrgID, from, to string) error {
		return inOrgTx(t, d.app, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `SELECT goal_transfer_ownership($1, $2, $3, 'req-transfer-1')`, from, to, "user:"+from)

			return err
		})
	}

	expectCheckViolation(t, "transfer from a non-owner", transfer(acme.ID, admin, owner), "goal_transfer_from_owner")
	expectCheckViolation(t, "transfer to a member", transfer(acme.ID, owner, member), "goal_transfer_to_admin")

	if err := transfer(acme.ID, owner, admin); err != nil {
		t.Fatal(err)
	}

	if got := d.roleOf(acme.ID, owner) + " " + d.roleOf(acme.ID, admin); got != "admin owner" {
		t.Errorf("roles after the transfer = %q; want the owner admin and the admin owner", got)
	}

	entries, err := d.s.AuditLog(domain.WithOrg(t.Context(), acme.ID), ports.AuditQuery{Action: "org.ownership_transferred", Limit: 10})
	want := domain.Actor{Kind: domain.ActorUser, ID: owner, Name: nameCruz}

	if err != nil || len(entries) != 1 || entries[0].Actor != want || entries[0].Data["from"] != owner || entries[0].Data["to"] != admin {
		t.Errorf("org.ownership_transferred = %+v, %v; want one by %+v from %s to %s", entries, err, want, owner, admin)
	}

	if n := d.count(`SELECT count(*) FROM audit_log WHERE action = 'org.ownership_transferred' AND request_id = 'req-transfer-1'`); n != 1 {
		t.Errorf("transfer entries with the request id = %d; want 1", n)
	}

	if err := d.s.DeleteOrg(domain.WithOrg(t.Context(), acme.ID), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	var pgErr *pgconn.PgError
	if err := transfer(acme.ID, admin, owner); !errors.As(err, &pgErr) || pgErr.Code != "P0002" {
		t.Errorf("transfer in a deleted org = %v; want no_data_found", err)
	}
}

// Every existing organization, deleted ones and default included, gets its auth.organization
// row; none gets a member.
func TestMigrate_BackfillsAuthOrganizations(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 41)

	for _, stmt := range []string{
		`INSERT INTO organizations (id, slug, name, mark) VALUES (goal_uuidv7(), 'acme', 'Acme', 'Ac')`,
		`INSERT INTO organizations (id, slug, name, mark, deleted_at) VALUES (goal_uuidv7(), 'gone', 'Gone', 'Go', now())`,
	} {
		if _, err := owner.Exec(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}

	upgrade(t, db.owner)

	missing := queryStrings(t, owner, `SELECT o.slug FROM organizations o
		LEFT JOIN auth.organization a ON a.id = o.id AND a.slug = o.slug AND a.name = o.name AND a."createdAt" = o.created_at
		WHERE a.id IS NULL`)
	if total := queryStrings(t, owner, `SELECT slug FROM auth.organization ORDER BY slug`); len(missing) != 0 || len(total) != 3 {
		t.Errorf("auth.organization = %v, missing %v; want default, acme and gone", total, missing)
	}

	if members := queryStrings(t, owner, `SELECT id::text FROM auth.member`); len(members) != 0 {
		t.Errorf("members after the backfill = %v; want none", members)
	}
}
