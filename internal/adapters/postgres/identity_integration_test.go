//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest/identitytest"
)

// auditMemberAdded is the audit action of a new membership (00043's trigger).
const auditMemberAdded = "member.added"

// pgSeed sets up Better Auth's and Goal's rows for identitytest, as the identity service and goald
// write them: users, sessions and members as goal_auth, factories and factory roles as goal_app
// under row-level security, platform admins as the superuser (the definers' tables).
type pgSeed struct{ d identityDB }

func (s pgSeed) Org(t *testing.T, name string) domain.OrgID {
	t.Helper()

	o, err := s.d.s.CreateOrg(t.Context(), domain.LocalUser, newOrg(strings.ToLower(name)+"-"+randomHex(t), name))
	if err != nil {
		t.Fatal(err)
	}

	return o.ID
}

func (s pgSeed) Factory(t *testing.T, org domain.OrgID) domain.FactoryID {
	t.Helper()

	id := newUUID(t)

	if err := inOrgTx(t, s.d.app, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO factories (id, org_id, name, definition, definition_sha256)
			VALUES ($1, goal_org(), $2, '{}', 'sha')`, id, "f-"+randomHex(t))

		return err
	}); err != nil {
		t.Fatal(err)
	}

	return domain.FactoryID(id)
}

func (s pgSeed) User(t *testing.T, name, email string, platformAdmin bool) domain.UserID {
	t.Helper()

	var id string
	if err := s.d.auth.QueryRow(t.Context(), `INSERT INTO auth."user" (name, email, "emailVerified") VALUES ($1, $2, true) RETURNING id::text`,
		name, email).Scan(&id); err != nil {
		t.Fatal(err)
	}

	if platformAdmin {
		s.d.exec(s.d.admin, `INSERT INTO platform_admins (user_id, added_by) VALUES ($1, 'system:test')`, id)
	}

	return domain.UserID(id)
}

func (s pgSeed) Session(t *testing.T, user domain.UserID, created, expires time.Time) string {
	t.Helper()

	token := sessionToken(t)
	s.d.exec(s.d.auth, `INSERT INTO auth.session (token, "userId", "createdAt", "updatedAt", "expiresAt") VALUES ($1, $2, $3, $3, $4)`,
		token, string(user), created, expires)

	return token
}

func (s pgSeed) EndSession(t *testing.T, token string) {
	t.Helper()

	s.d.exec(s.d.auth, `DELETE FROM auth.session WHERE token = $1`, token)
}

func (s pgSeed) SetRole(t *testing.T, org domain.OrgID, user domain.UserID, role domain.OrgRole) {
	t.Helper()

	if role == "" {
		s.d.exec(s.d.auth, `DELETE FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`, string(org), string(user))

		return
	}

	s.d.exec(s.d.auth, `INSERT INTO auth.member ("organizationId", "userId", role, "createdAt") VALUES ($1, $2, $3, now())
		ON CONFLICT ("organizationId", "userId") DO UPDATE SET role = EXCLUDED.role`, string(org), string(user), string(role))
}

func (s pgSeed) SetFactoryRole(t *testing.T, org domain.OrgID, user domain.UserID, f domain.FactoryID, role domain.FactoryRole) {
	t.Helper()

	if err := inOrgTx(t, s.d.app, org, func(tx pgx.Tx) error {
		if role == "" {
			_, err := tx.Exec(t.Context(), `DELETE FROM factory_roles WHERE factory_id = $1 AND user_id = $2`, string(f), string(user))

			return err
		}

		_, err := tx.Exec(t.Context(), `INSERT INTO factory_roles (factory_id, user_id, role, granted_by) VALUES ($1, $2, $3, 'system:test')
			ON CONFLICT (org_id, factory_id, user_id) DO UPDATE SET role = EXCLUDED.role`, string(f), string(user), string(role))

		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func (s pgSeed) DeleteOrg(t *testing.T, org domain.OrgID) {
	t.Helper()

	if err := s.d.s.DeleteOrg(domain.WithOrg(t.Context(), org), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// sessionToken is a token of Better Auth's shape: 32 letters and digits.
func sessionToken(t *testing.T) string {
	t.Helper()

	return strings.ToUpper(randomHex(t)) + randomHex(t) + strings.ToUpper(randomHex(t))[:8]
}

func TestIdentity_Conformance(t *testing.T) {
	t.Parallel()

	identitytest.Run(t, func(t *testing.T) (ports.Identity, identitytest.Seed) {
		t.Helper()

		d := newIdentityDB(t)

		return NewIdentity(d.s), pgSeed{d: d}
	})
}

// A new organization is owned by NewOrg.Owner, else by the install's earliest platform admin,
// else (before anyone signed in) by nobody; each owner's membership is audited as org_created.
func TestCreateOrg_Owner(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	s := pgSeed{d: d}

	nobody := d.org("nobody-" + randomHex(t))
	if n := d.count(`SELECT count(*) FROM auth.member WHERE "organizationId" = $1`, string(nobody.ID)); n != 0 {
		t.Errorf("an org created before anyone signed in has %d members; want none", n)
	}

	first := s.User(t, "First", "first@example.com", true)
	s.User(t, "Second", "second@example.com", true)
	creator := s.User(t, "Creator", "creator@example.com", false)

	local := d.org("local-" + randomHex(t))
	if role := d.roleOf(local.ID, string(first)); role != string(domain.OrgOwner) {
		t.Errorf("an org created under GOAL_AUTH=none is owned by the earliest platform admin as %q; want owner", role)
	}

	by := domain.Actor{Kind: domain.ActorUser, ID: string(creator), Name: "Creator"}

	o, err := d.s.CreateOrg(t.Context(), by, ports.NewOrg{Slug: "made-" + randomHex(t), Name: "Made", Owner: creator})
	if err != nil {
		t.Fatal(err)
	}

	if role := d.roleOf(o.ID, string(creator)); role != string(domain.OrgOwner) || d.roleOf(o.ID, string(first)) != "" {
		t.Errorf("the creator's role = %q, the admin's %q; want the creator alone as owner", role, d.roleOf(o.ID, string(first)))
	}

	if log := d.logOf(o.ID); len(log) != 1 || log[0].action != auditMemberAdded || log[0].actor != by.Ref() || log[0].data["via"] != "org_created" {
		t.Errorf("audit = %+v; want member.added by the creator, via org_created", log)
	}
}

// goal_members and goal_invitations show only the transaction's organization: its members with
// their roles, factory roles and last activity, and its pending, unexpired invitations.
func TestIdentityDefiners_MembersAndInvitations(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	s := pgSeed{d: d}
	acme, other := s.Org(t, "Acme"), s.Org(t, "Other")
	f := s.Factory(t, acme)
	ada, bob := s.User(t, "Ada", "ada@example.com", false), s.User(t, "Bob", "bob@example.com", false)
	s.SetRole(t, acme, ada, domain.OrgOwner)
	s.SetRole(t, acme, bob, domain.OrgMember)
	s.SetRole(t, other, bob, domain.OrgOwner)
	s.SetFactoryRole(t, acme, bob, f, domain.FactoryViewer)

	active := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	s.Session(t, bob, active, active.Add(time.Hour))

	pending := d.invite(acme, string(ada), "new@example.com", "pending")
	d.invite(acme, string(ada), "gone@example.com", "canceled")
	d.exec(d.auth, `UPDATE auth.invitation SET "expiresAt" = now() - interval '1 minute' WHERE id = $1`,
		d.invite(acme, string(ada), "late@example.com", "pending"))
	d.invite(other, string(bob), "elsewhere@example.com", "pending")

	var members []struct {
		UserID       string     `json:"user_id"`
		Role         string     `json:"role"`
		LastActiveAt *time.Time `json:"last_active_at"`
		FactoryRoles []struct {
			FactoryID string `json:"factory_id"`
			Role      string `json:"role"`
		} `json:"factory_roles"`
	}

	var invitations []struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Inviter struct {
			ID string `json:"id"`
		} `json:"inviter"`
	}

	d.definer(acme, `SELECT goal_members()`, &members)
	d.definer(acme, `SELECT goal_invitations()`, &invitations)

	if len(members) != 2 || members[0].UserID != string(ada) || members[0].Role != string(domain.OrgOwner) || members[0].LastActiveAt != nil ||
		members[1].UserID != string(bob) || members[1].LastActiveAt == nil || !members[1].LastActiveAt.Equal(active) ||
		len(members[1].FactoryRoles) != 1 || members[1].FactoryRoles[0].FactoryID != string(f) {
		t.Errorf("goal_members = %+v; want Ada (owner, never active) and Bob (member, viewer of %s, active at %v)", members, f, active)
	}

	if len(invitations) != 1 || invitations[0].ID != pending || invitations[0].Inviter.ID != string(ada) {
		t.Errorf("goal_invitations = %+v; want the one pending invitation %s", invitations, pending)
	}
}

// definer runs a jsonb definer of org's transaction as goal_app and decodes its result into out.
func (d identityDB) definer(org domain.OrgID, sql string, out any) {
	d.t.Helper()

	if err := inOrgTx(d.t, d.app, org, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(d.t.Context(), sql).Scan(&raw); err != nil {
			return err
		}

		return json.Unmarshal(raw, out)
	}); err != nil {
		d.t.Fatalf("%s: %v", sql, err)
	}
}

// A pending CLI sign-in is found by the code the person types, dashes and case aside, with the
// address of its device.requested event; an expired, approved or unknown one is not.
func TestIdentity_DeviceRequest(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	id := NewIdentity(d.s)

	device := func(code, status string, ttl time.Duration) {
		d.exec(d.auth, `INSERT INTO auth."deviceCode" ("deviceCode", "userCode", "expiresAt", status, "clientId")
			VALUES ($1, $2, $3, $4, 'goal-cli')`, randomHex(t), code, time.Now().Add(ttl), status)
	}

	device("ABCDEFGH", "pending", 10*time.Minute)
	device("EXPIRED1", "pending", -time.Minute)
	device("APPROVED", "approved", 10*time.Minute)

	sum := sha256.Sum256([]byte("ABCDEFGH"))
	d.exec(d.auth, `SELECT goal_auth_event('device.requested', NULL, $1, '203.0.113.7', 'goal/1.4 (darwin)', '{"client_id": "goal-cli"}')`,
		base64.RawURLEncoding.EncodeToString(sum[:]))

	r, err := id.DeviceRequest(t.Context(), "abcd-efgh")
	if err != nil || r.ClientID != "goal-cli" || r.ClientName != "Goal CLI" || r.IP != "203.0.113.7" || r.UserAgent != "goal/1.4 (darwin)" ||
		r.RequestedAt.IsZero() || !r.ExpiresAt.After(time.Now()) {
		t.Errorf("DeviceRequest = %+v, %v; want the pending CLI request from 203.0.113.7", r, err)
	}

	for _, code := range []string{"EXPIRED1", "APPROVED", "ZZZZZZZZ", ""} {
		if r, err := id.DeviceRequest(t.Context(), code); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("DeviceRequest(%q) = %+v, %v; want ErrNotFound", code, r, err)
		}
	}
}

// Platform admins are added by email and removed by id, each change in the security log; the
// last one stays, and an unknown email adds nobody.
func TestIdentityDefiners_PlatformAdmins(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	s := pgSeed{d: d}
	ada := s.User(t, "Ada", "ada@example.com", true)
	bob := s.User(t, "Bob", "bob@example.com", false)

	var added json.RawMessage
	if err := d.app.QueryRow(t.Context(), `SELECT goal_add_platform_admin($1, 'user:'||$2::text)`, strings.ToUpper(d.email(string(bob))), string(ada)).
		Scan(&added); err != nil || !strings.Contains(string(added), string(bob)) {
		t.Fatalf("add Bob = %s, %v; want Bob", added, err)
	}

	var none []byte
	if err := d.app.QueryRow(t.Context(), `SELECT goal_add_platform_admin('nobody@example.com', 'user:x')`).Scan(&none); err != nil || none != nil {
		t.Errorf("add an unknown email = %s, %v; want NULL", none, err)
	}

	var admins []struct {
		UserID string `json:"user_id"`
	}
	d.definer(domain.OrgID(newUUID(t)), `SELECT goal_platform_admins()`, &admins)

	if len(admins) != 2 || admins[0].UserID != string(ada) || admins[1].UserID != string(bob) {
		t.Errorf("platform admins = %+v; want Ada then Bob", admins)
	}

	d.exec(d.app, `SELECT goal_remove_platform_admin($1::uuid, 'user:'||$1::text)`, string(ada))

	_, err := d.app.Exec(t.Context(), `SELECT goal_remove_platform_admin($1::uuid, 'user:'||$1::text)`, string(bob))
	expectCheckViolation(t, "removing the last platform admin", err, "platform_admins_keep_one")

	var kinds []string
	d.definer(domain.OrgID(newUUID(t)), `SELECT coalesce(jsonb_agg(e->>'kind'), '[]') FROM jsonb_array_elements(goal_auth_events(NULL, 10, NULL)) e`, &kinds)

	if !slices.Equal(kinds, []string{"platform_admin.removed", "platform_admin.added"}) {
		t.Errorf("security log = %v; want removed, then added (newest first)", kinds)
	}
}

// The clean-up deletes only what has expired long enough, and counts it.
func TestIdentityDefiners_AuthCleanup(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	s := pgSeed{d: d}
	ada := s.User(t, "Ada", "ada@example.com", false)
	now := time.Now()
	s.Session(t, ada, now.Add(-9*24*time.Hour), now.Add(-2*24*time.Hour))
	s.Session(t, ada, now.Add(-2*time.Hour), now.Add(-time.Hour)) // expired, but less than a day ago
	d.exec(d.admin, `INSERT INTO auth_events (kind, at) VALUES ('otp.sent', now() - interval '91 days'), ('otp.sent', now())`)

	var counts map[string]int
	if err := d.app.QueryRow(t.Context(), `SELECT goal_auth_cleanup(now())`).Scan(&counts); err != nil {
		t.Fatal(err)
	}

	if counts["sessions"] != 1 || counts["auth_events"] != 1 {
		t.Errorf("clean-up = %v; want 1 session and 1 event", counts)
	}

	if n := d.count(`SELECT count(*) FROM auth.session`); n != 1 {
		t.Errorf("%d sessions left; want the recently expired one", n)
	}
}

// goald's startup self-check passes at goose head and fails once goal_app could use schema auth.
func TestCheckIdentity(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)

	if err := CheckIdentity(t.Context(), d.s); err != nil {
		t.Fatalf("CheckIdentity at head = %v; want ready", err)
	}

	d.exec(d.admin, `GRANT USAGE ON SCHEMA auth TO goal_app`)

	if err := CheckIdentity(t.Context(), d.s); !errors.Is(err, ErrIdentityNotReady) {
		t.Errorf("CheckIdentity with USAGE on auth = %v; want ErrIdentityNotReady", err)
	}

	d.exec(d.admin, `REVOKE USAGE ON SCHEMA auth FROM goal_app`)
}

// goal up asks whether anyone has a user before it starts sign-in: nobody in a database goald has
// not migrated yet, nor at head before the first sign-in; somebody once a user exists.
func TestUsersExist(t *testing.T) {
	t.Parallel()

	empty := newTestDB(t, OwnerRole)
	if exist, err := UsersExist(t.Context(), openPool(t, empty.admin)); err != nil || exist {
		t.Errorf("UsersExist before migrating = %v, %v; want false", exist, err)
	}

	d := newIdentityDB(t)
	if exist, err := UsersExist(t.Context(), d.admin); err != nil || exist {
		t.Errorf("UsersExist at head = %v, %v; want false", exist, err)
	}

	pgSeed{d: d}.User(t, "Ada", "ada@example.com", false)

	if exist, err := UsersExist(t.Context(), d.admin); err != nil || !exist {
		t.Errorf("UsersExist with a user = %v, %v; want true", exist, err)
	}
}

// The identity definers are goal_app's alone: the identity service may not run them.
func TestIdentityDefiners_GrantedToTheAppOnly(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)

	for _, fn := range []string{
		"goal_principal(text, uuid)", "goal_orgs_for(uuid)", "goal_members()", "goal_invitations()", "goal_device_request(text)",
		"goal_platform_admins()", "goal_add_platform_admin(text, text)", "goal_remove_platform_admin(uuid, text)",
		"goal_auth_events(bigint, integer, text)", "goal_auth_cleanup(timestamptz)", "goal_identity_ready()",
	} {
		for role, want := range map[string]bool{AppRole: true, AuthRole: false, publicRole: false} {
			var has bool
			if err := d.admin.QueryRow(t.Context(), `SELECT has_function_privilege($1, $2, 'EXECUTE')`, role, fn).Scan(&has); err != nil || has != want {
				t.Errorf("%s EXECUTE on %s = %v, %v; want %v", role, fn, has, err, want)
			}
		}
	}
}

// Access changes reach a listener as their notices claim them, after the first resync.
func TestAccessNotices_Delivered(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	s := pgSeed{d: d}
	acme := s.Org(t, "Acme")
	owner, ada := s.User(t, "Owner", "owner@example.com", false), s.User(t, "Ada", "ada@example.com", false)
	s.SetRole(t, acme, owner, domain.OrgOwner)
	s.SetRole(t, acme, ada, domain.OrgMember)
	token := s.Session(t, ada, time.Now(), time.Now().Add(time.Hour))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	got := make(chan ports.AccessNotice, 8)

	go func() { _ = NewAccessNotices(d.s, nil).Listen(ctx, func(n ports.AccessNotice) { got <- n }) }()

	if n := <-got; !n.Resync {
		t.Fatalf("first notice = %+v; want a resync", n)
	}

	s.SetRole(t, acme, ada, "")
	s.EndSession(t, token)

	want := []ports.AccessNotice{
		{Kind: ports.AccessMember, OrgID: acme, UserID: ada},
		{Kind: ports.AccessSession, UserID: ada},
	}

	for _, w := range want {
		select {
		case n := <-got:
			if n.Kind != w.Kind || n.OrgID != w.OrgID || n.UserID != w.UserID || (w.Kind == ports.AccessSession && n.SessionID == "") {
				t.Errorf("notice = %+v; want %+v", n, w)
			}
		case <-ctx.Done():
			t.Fatalf("no %s notice", w.Kind)
		}
	}
}
