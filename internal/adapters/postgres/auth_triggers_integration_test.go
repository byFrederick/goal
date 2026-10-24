//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Names, roles and data keys the identity tests share.
const (
	nameCruz          = "F. Cruz"
	nameDoe           = "J. Doe"
	roleMember        = "member"
	fieldEmail        = "email"
	fieldFrom         = "from"
	fieldKind         = "kind"
	fieldFactoryRoles = "factory_roles"
	fieldRole         = "role"
	fieldInvitation   = "invitation_id"
	fieldOrg          = "org_id"
	fieldUser         = "user_id"
)

// provision creates an organization owned by owner through goal_provision_org, as goald does from
// 3-9 on.
func (d identityDB) provision(slug, owner string) domain.OrgID {
	d.t.Helper()

	id := newUUID(d.t)

	err := inOrgTx(d.t, d.app, domain.OrgID(id), func(tx pgx.Tx) error {
		_, err := tx.Exec(d.t.Context(), `SELECT * FROM goal_provision_org($1, $2, 'Acme', 'Ac', $3, goal_uuidv7(), $4, NULL)`,
			id, slug, owner, "user:"+owner)

		return err
	})
	if err != nil {
		d.t.Fatal(err)
	}

	return domain.OrgID(id)
}

func (d identityDB) email(user string) string {
	d.t.Helper()

	var email string
	if err := d.admin.QueryRow(d.t.Context(), `SELECT email FROM auth."user" WHERE id = $1`, user).Scan(&email); err != nil {
		d.t.Fatal(err)
	}

	return email
}

// logged is an audit entry as these tests compare it: action, actor ref and name, target, data.
type logged struct {
	action, actor, name, target string
	data                        map[string]any
}

// logOf is org's audit log, oldest first, without org.created.
func (d identityDB) logOf(org domain.OrgID) []logged {
	d.t.Helper()

	entries, err := d.s.AuditLog(domain.WithOrg(d.t.Context(), org), ports.AuditQuery{Limit: 100})
	if err != nil {
		d.t.Fatal(err)
	}

	out := make([]logged, 0, len(entries))
	for _, e := range slices.Backward(entries) {
		if e.Action != domain.AuditOrgCreated {
			out = append(out, logged{e.Action, e.Actor.Ref(), e.Actor.Name, e.TargetKind + ":" + e.TargetID, e.Data})
		}
	}

	return out
}

// Spec 3 §11.1 row 22: each change Better Auth makes to a membership or an invitation writes one
// entry in its organization's log, in the same transaction, by the stamped actor when the stamp
// is fresh, else by the member themself (a new member), the inviter (a new invitation) or
// system:auth.
func TestAuthTriggers_AuditEachPathOnce(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner, invitee := d.user(nameCruz), d.user(nameDoe)
	acme := d.provision("acme", owner)
	email := d.email(invitee)
	factory := newUUID(t)

	var inv string
	if err := d.auth.QueryRow(t.Context(), `INSERT INTO auth.invitation ("organizationId", email, role, status, "expiresAt", "inviterId", "factoryRoles")
		VALUES ($1, $2, 'member', 'pending', now() + interval '3 days', $3, $4) RETURNING id::text`,
		string(acme), email, owner, `[{"factoryId":"`+factory+`","role":"viewer"}]`).Scan(&inv); err != nil {
		t.Fatal(err)
	}

	stamp := `, "changedBy" = $3, "changedAt" = now()`
	d.exec(d.auth, `UPDATE auth.invitation SET status = 'accepted'`+stamp+` WHERE "organizationId" = $1 AND id = $2`,
		string(acme), inv, "user:"+invitee)
	d.member(acme, invitee, roleMember)
	d.exec(d.auth, `UPDATE auth.member SET role = 'admin'`+stamp+` WHERE "organizationId" = $1 AND "userId" = $2`,
		string(acme), invitee, "user:"+owner)
	d.exec(d.auth, `UPDATE auth.member SET role = 'member', "changedBy" = $3, "changedAt" = now() - interval '2 minutes'
		WHERE "organizationId" = $1 AND "userId" = $2`, string(acme), invitee, "user:"+owner)
	d.exec(d.auth, `UPDATE auth.member SET "changedBy" = $3, "changedAt" = now() WHERE "organizationId" = $1 AND "userId" = $2`,
		string(acme), invitee, "user:"+invitee) // the stamp alone writes nothing
	d.exec(d.auth, `DELETE FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`, string(acme), invitee)
	rejected := d.invite(acme, owner, "x@example.com", "pending")
	d.exec(d.auth, `UPDATE auth.invitation SET status = 'rejected' WHERE id = $1`, rejected)
	pending := d.invite(acme, owner, "y@example.com", "pending")

	if err := d.s.DeleteOrg(domain.WithOrg(t.Context(), acme), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	o, i, s := "user:"+owner, "user:"+invitee, "system:auth"
	want := []logged{
		{auditMemberAdded, o, nameCruz, "member:" + owner, map[string]any{fieldUser: owner, fieldRole: "owner", "via": "org_created"}},
		{"invitation.created", o, nameCruz, "invitation:" + inv, map[string]any{
			fieldInvitation: inv, fieldEmail: email, fieldRole: roleMember,
			fieldFactoryRoles: []any{map[string]any{"factoryId": factory, fieldRole: "viewer"}},
		}},
		{"invitation.accepted", i, nameDoe, "invitation:" + inv, map[string]any{fieldInvitation: inv, fieldEmail: email}},
		{auditMemberAdded, i, nameDoe, "member:" + invitee, map[string]any{fieldUser: invitee, fieldRole: roleMember, "via": "invitation"}},
		{"member.role_changed", o, nameCruz, "member:" + invitee, map[string]any{fieldUser: invitee, fieldFrom: roleMember, "to": "admin"}},
		{"member.role_changed", s, "auth", "member:" + invitee, map[string]any{fieldUser: invitee, fieldFrom: "admin", "to": roleMember}},
		{"member.removed", i, nameDoe, "member:" + invitee, map[string]any{fieldUser: invitee, fieldRole: roleMember, "left": true}},
		{"invitation.created", o, nameCruz, "invitation:" + rejected, map[string]any{
			fieldInvitation: rejected, fieldEmail: "x@example.com", fieldRole: roleMember, fieldFactoryRoles: []any{},
		}},
		{"invitation.rejected", s, "auth", "invitation:" + rejected, map[string]any{fieldInvitation: rejected, fieldEmail: "x@example.com"}},
		{"invitation.created", o, nameCruz, "invitation:" + pending, map[string]any{
			fieldInvitation: pending, fieldEmail: "y@example.com", fieldRole: roleMember, fieldFactoryRoles: []any{},
		}},
		{"invitation.canceled", "system:org-deleted", "org-deleted", "invitation:" + pending, map[string]any{
			fieldInvitation: pending, fieldEmail: "y@example.com",
		}},
		{"org.deleted", domain.LocalUser.Ref(), domain.LocalUser.Name, "org:" + string(acme), map[string]any{"slug": "acme", "name": "Acme"}},
	}

	expectLogged(t, d.logOf(acme), want)

	// The bootstrap claim and recovery name themselves (00044, goal db recover-owner).
	labs := d.org("labs")
	d.exec(d.auth, `INSERT INTO auth.member ("organizationId", "userId", role, "createdAt", "changedBy", "changedAt")
		VALUES ($1, $2, 'owner', now(), 'system:bootstrap', now())`, string(labs.ID), owner)
	expectLogged(t, d.logOf(labs.ID), []logged{
		{auditMemberAdded, "system:bootstrap", "bootstrap", "member:" + owner, map[string]any{fieldUser: owner, fieldRole: "owner", "via": "bootstrap"}},
	})
}

// Review L1: an invitation belongs to a live organization. Once one is deleted, its lingering
// members' invitations are refused in the database, whatever the auth service's hooks say, and
// none of its invitations makes an invitee.
func TestAuthTriggers_InvitationsOnlyToALiveOrg(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner := d.user(nameCruz)
	acme := d.provision("acme", owner)

	var live bool
	if err := d.auth.QueryRow(t.Context(), `SELECT goal_org_live($1)`, string(acme)).Scan(&live); err != nil || !live {
		t.Fatalf("goal_org_live(acme) = %v, %v; want true", live, err)
	}

	if err := d.s.DeleteOrg(domain.WithOrg(t.Context(), acme), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	_, err := d.auth.Exec(t.Context(), `INSERT INTO auth.invitation ("organizationId", email, role, status, "expiresAt", "inviterId")
		VALUES ($1, 'late@example.com', 'member', 'pending', now() + interval '3 days', $2)`, string(acme), owner)
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23514" || pgErr.ConstraintName != "goal_invitation_live_org" {
		t.Fatalf("inviting to a deleted organization = %v; want check_violation goal_invitation_live_org", err)
	}

	var invitee bool
	if err := d.auth.QueryRow(t.Context(), `SELECT goal_org_live($1) OR goal_is_invitee('late@example.com', now())`, string(acme)).Scan(&invitee); err != nil || invitee {
		t.Errorf("after the deletion: live or invitee = %v, %v; want neither", invitee, err)
	}
}

// Review L2: a pending invitation never outlives its inviter's right to send it. In the same
// transaction as the change, removing an inviter cancels their pending invitations of the
// organization, demoting one to member cancels them all, and an owner made admin loses the
// invitations only an owner could send (owners); others' invitations and other organizations'
// stay pending. Each cancellation is audited by its system actor.
func TestAuthTriggers_InvitationsEndWithTheirInviter(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner, second, admin, leaver := d.user(nameCruz), d.user("A. Second"), d.user(nameDoe), d.user("L. Eaver")
	acme := d.provision("acme", owner)
	labs := d.provision("labs", admin)
	d.member(acme, second, "owner")
	d.member(acme, admin, "admin")
	d.member(acme, leaver, "admin")

	invite := func(org domain.OrgID, inviter, role string) string {
		t.Helper()

		var id string
		if err := d.auth.QueryRow(t.Context(), `INSERT INTO auth.invitation ("organizationId", email, role, status, "expiresAt", "inviterId")
			VALUES ($1, $2, $3, 'pending', now() + interval '3 days', $4) RETURNING id::text`,
			string(org), role+"-"+randomHex(t)+"@example.com", role, inviter).Scan(&id); err != nil {
			t.Fatal(err)
		}

		return id
	}

	adminsMember, adminsAdmin := invite(acme, admin, roleMember), invite(acme, admin, "admin")
	adminsInLabs := invite(labs, admin, roleMember)
	ownersOwner, ownersMember := invite(acme, owner, "owner"), invite(acme, owner, roleMember)
	leaversMember := invite(acme, leaver, roleMember)
	seconds := invite(acme, second, "owner")

	d.exec(d.auth, `UPDATE auth.member SET role = 'member' WHERE "organizationId" = $1 AND "userId" = $2`, string(acme), admin)
	d.exec(d.auth, `UPDATE auth.member SET role = 'admin' WHERE "organizationId" = $1 AND "userId" = $2`, string(acme), owner)
	d.exec(d.auth, `DELETE FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`, string(acme), leaver)

	want := map[string]string{
		adminsMember: "canceled system:inviter-demoted", adminsAdmin: "canceled system:inviter-demoted",
		adminsInLabs: "pending ", ownersOwner: "canceled system:inviter-demoted", ownersMember: "pending ",
		leaversMember: "canceled system:inviter-removed", seconds: "pending ",
	}

	for id, w := range want {
		var got string
		if err := d.admin.QueryRow(t.Context(), `SELECT status || ' ' || coalesce("changedBy", '') FROM auth.invitation WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}

		if got != w {
			t.Errorf("invitation %s = %q; want %q", id, got, w)
		}
	}

	var audited int
	if err := d.admin.QueryRow(t.Context(), `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'invitation.canceled'
		AND actor_kind = 'system' AND actor_id IN ('inviter-demoted', 'inviter-removed')`, string(acme)).Scan(&audited); err != nil || audited != 4 {
		t.Errorf("audited cancellations = %d, %v; want 4", audited, err)
	}
}

func expectLogged(t *testing.T, got, want []logged) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("audit log has %d entries; want %d:\n got %+v\nwant %+v", len(got), len(want), got, want)
	}

	for n := range want {
		g, w := got[n], want[n]
		if g.action != w.action || g.actor != w.actor || g.name != w.name || g.target != w.target || !jsonEqual(g.data, w.data) {
			t.Errorf("entry %d = %+v; want %+v", n, g, w)
		}
	}
}

// jsonEqual compares decoded JSON values.
func jsonEqual(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)

	return errA == nil && errB == nil && string(x) == string(y)
}

// Access changes are announced on goal_access at commit: role changes and removals, sign-outs,
// factory roles and soft deletes.
func TestAuthTriggers_AccessNotices(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner, member := d.user(nameCruz), d.user(nameDoe)
	acme := d.provision("acme", owner)
	d.member(acme, member, roleMember)
	ctx := domain.WithOrg(t.Context(), acme)
	factory := saveFactory(ctx, t, d.s)

	conn, err := d.admin.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(conn.Release)

	if _, err := conn.Exec(t.Context(), "LISTEN goal_access"); err != nil {
		t.Fatal(err)
	}

	var session string
	if err := d.auth.QueryRow(t.Context(), `INSERT INTO auth.session ("expiresAt", token, "updatedAt", "userId")
		VALUES (now() + interval '7 days', $1, now(), $2) RETURNING id::text`, randomHex(t)+randomHex(t), member).Scan(&session); err != nil {
		t.Fatal(err)
	}

	for _, sql := range []string{
		`INSERT INTO factory_roles (factory_id, user_id, role, granted_by) VALUES ($1, $2, 'viewer', 'user:local')`,
		`UPDATE factory_roles SET role = 'editor' WHERE factory_id = $1 AND user_id = $2`,
		`DELETE FROM factory_roles WHERE factory_id = $1 AND user_id = $2`,
	} {
		if err := inOrgTx(t, d.app, acme, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), sql, string(factory), member); return err }); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	d.exec(d.auth, `UPDATE auth.member SET role = 'admin' WHERE "organizationId" = $1 AND "userId" = $2`, string(acme), member)
	d.exec(d.auth, `DELETE FROM auth.session WHERE id = $1`, session)
	d.exec(d.auth, `DELETE FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`, string(acme), member)

	if err := d.s.DeleteOrg(ctx, domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	org, f := string(acme), string(factory)
	role := map[string]string{fieldKind: "factory_role", fieldOrg: org, fieldUser: member, "factory_id": f}
	want := []map[string]string{
		role, role, role,
		{fieldKind: roleMember, fieldOrg: org, fieldUser: member},
		{fieldKind: "session", fieldUser: member, "session_id": session},
		{fieldKind: roleMember, fieldOrg: org, fieldUser: member},
		{fieldKind: "org", fieldOrg: org},
	}

	for n, w := range want {
		wait, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		got, err := conn.Conn().WaitForNotification(wait)

		cancel()

		if err != nil {
			t.Fatalf("notice %d: %v; want %v", n, err, w)
		}

		var payload map[string]string
		if err := json.Unmarshal([]byte(got.Payload), &payload); err != nil || got.Channel != "goal_access" || !maps.Equal(payload, w) {
			t.Errorf("notice %d on %s = %s (%v); want %v", n, got.Channel, got.Payload, err, w)
		}
	}
}

// A factory role is an org row under row-level security and exists only for a member of the
// organization and a factory of it; it goes with the membership.
func TestFactoryRoles_MembersOfTheOrgOnly(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	owner, member, outsider := d.user(nameCruz), d.user(nameDoe), d.user("A. Outsider")
	acme, labs := d.provision("acme", owner), d.provision("labs", outsider)
	d.member(acme, member, roleMember)
	factory := saveFactory(domain.WithOrg(t.Context(), acme), t, d.s)
	other := saveFactory(domain.WithOrg(t.Context(), labs), t, d.s)

	grant := func(org domain.OrgID, f domain.FactoryID, user, role string) error {
		return inOrgTx(t, d.app, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `INSERT INTO factory_roles (factory_id, user_id, role, granted_by) VALUES ($1, $2, $3, 'user:local')`,
				string(f), user, role)

			return err
		})
	}

	if err := grant(acme, factory, member, "approver"); err != nil {
		t.Fatal(err)
	}

	for what, err := range map[string]error{
		"a non-member":            grant(acme, factory, outsider, "viewer"),
		"another org's factory":   grant(acme, other, member, "viewer"),
		"an unknown role":         grant(acme, factory, owner, "boss"),
		"another org's member":    grant(labs, other, member, "viewer"),
		"a row of another org id": insertRoleFor(t, d, acme, labs, other, outsider),
	} {
		if _, ok := errors.AsType[*pgconn.PgError](err); !ok {
			t.Errorf("factory role for %s = %v; want refused by the database", what, err)
		}
	}

	var seen int
	if err := asOrg(t, d.app, labs, `SELECT count(*) FROM factory_roles`, &seen); err != nil || seen != 0 {
		t.Errorf("labs sees %d factory roles, %v; want none", seen, err)
	}

	d.exec(d.auth, `DELETE FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`, string(acme), member)

	if n := d.count(`SELECT count(*) FROM factory_roles`); n != 0 {
		t.Errorf("factory roles after the member left = %d; want 0", n)
	}
}

// insertRoleFor writes, in a transaction of org, a factory role naming another organization's id.
func insertRoleFor(t *testing.T, d identityDB, org, other domain.OrgID, f domain.FactoryID, user string) error {
	t.Helper()

	return inOrgTx(t, d.app, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO factory_roles (org_id, factory_id, user_id, role, granted_by) VALUES ($1, $2, $3, 'viewer', 'user:local')`,
			string(other), string(f), user)

		return err
	})
}

// The install keeps a platform admin once anyone has signed in, also when two admins remove each
// other at the same moment.
func TestPlatformAdmins_KeepOne(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	a, b := d.user("A. Admin"), d.user("B. Admin")
	d.exec(d.admin, `INSERT INTO platform_admins (user_id, added_by) VALUES ($1, 'system:bootstrap'), ($2, 'system:bootstrap')`, a, b)

	errs := make(chan error, 2)
	txs := make([]pgx.Tx, 0, 2)

	for _, user := range []string{a, b} {
		tx, err := d.admin.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		if _, err := tx.Exec(t.Context(), `DELETE FROM platform_admins WHERE user_id = $1`, user); err != nil {
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
		t.Fatalf("two concurrent removals failed %v; want exactly one refused", failed)
	}

	expectCheckViolation(t, "the second removal", failed[0], "platform_admins_keep_one")

	_, err := d.admin.Exec(t.Context(), `DELETE FROM platform_admins`)
	expectCheckViolation(t, "removing the last platform admin", err, "platform_admins_keep_one")

	if n := d.count(`SELECT count(*) FROM platform_admins`); n != 1 {
		t.Errorf("platform admins = %d; want 1", n)
	}
}

// API keys are audited as actor kind service (00043 widens the check).
func TestAudit_RecordsServiceActors(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	acme := d.org("acme")
	key := domain.Actor{Kind: domain.ActorService, ID: newUUID(t), Name: "ci (API key)"}

	if err := d.s.Append(domain.WithOrg(t.Context(), acme.ID), domain.AuditEntry{
		Actor: key, Action: andonPulled, TargetKind: domain.AuditTargetFactory, TargetID: "f",
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := d.s.AuditLog(domain.WithOrg(t.Context(), acme.ID), ports.AuditQuery{Action: andonPulled, Limit: 10})
	if err != nil || len(entries) != 1 || entries[0].Actor != key {
		t.Errorf("entries by the key = %+v, %v; want one by %+v", entries, err, key)
	}
}
