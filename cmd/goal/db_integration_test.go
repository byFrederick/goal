//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// recoveryDB is a throwaway database migrated as goald would (never the install's own): adminURL
// connects as the bootstrap superuser, as GOAL_DATABASE_ADMIN_URL does, admin and app pools as
// it and as goal_app.
type recoveryDB struct {
	adminURL   string
	admin, app *pgxpool.Pool
}

func newRecoveryDB(t *testing.T) recoveryDB {
	t.Helper()

	base := os.Getenv("GOAL_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("GOAL_TEST_DATABASE_URL is not set; run `task test-integration`")
	}

	server := openTestPool(t, base)

	// The roles of roles.sql, created when missing and otherwise left as they are.
	for _, role := range []string{
		"goal_owner NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB",
		"goal_app NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOINHERIT",
		"goal_auth NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOINHERIT",
	} {
		execTest(t, server, "DO $$ BEGIN CREATE ROLE "+role+"; EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$")
	}

	name := "goal_test_3_15_" + strings.ToLower(rand.Text()[:12])
	execTest(t, server, "CREATE DATABASE "+name)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := server.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})

	adminURL := databaseURL(t, base, name, "")
	admin := openTestPool(t, adminURL)
	execTest(t, admin, "REVOKE TEMPORARY, CONNECT ON DATABASE "+name+" FROM PUBLIC; "+
		"GRANT CONNECT, CREATE ON DATABASE "+name+" TO goal_owner; GRANT CONNECT ON DATABASE "+name+" TO goal_app, goal_auth; "+
		"GRANT USAGE, CREATE ON SCHEMA public TO goal_owner; GRANT USAGE ON SCHEMA public TO goal_app, goal_auth")

	if err := postgres.Migrate(t.Context(), openTestPool(t, databaseURL(t, base, name, postgres.OwnerRole))); err != nil {
		t.Fatal(err)
	}

	return recoveryDB{adminURL: adminURL, admin: admin, app: openTestPool(t, databaseURL(t, base, name, postgres.AppRole))}
}

// databaseURL is base on database, switching to role unless it is "".
func databaseURL(t *testing.T, base, database, role string) string {
	t.Helper()

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}

	u.Path = "/" + database

	if role != "" {
		q := u.Query()
		q.Set("role", role)
		u.RawQuery = q.Encode()
	}

	return u.String()
}

func openTestPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()

	pool, err := postgres.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func execTest(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func (d recoveryDB) count(t *testing.T, sql string, args ...any) int {
	t.Helper()

	var n int
	if err := d.admin.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}

	return n
}

// signIn is the person with email signed in as the identity service signs them in by code (a
// session row), resolved by goald for org.
func (d recoveryDB) signIn(t *testing.T, email string, org domain.OrgID) domain.Principal {
	t.Helper()

	token, at := strings.ReplaceAll(rand.Text(), "=", "")[:26]+"Signed", time.Now()
	execTest(t, d.admin, `INSERT INTO auth.session (token, "userId", "createdAt", "updatedAt", "expiresAt")
		SELECT $1, id, $3, $3, $4 FROM auth."user" WHERE email = $2`, token, email, at, at.Add(24*time.Hour))

	p, err := postgres.NewIdentity(postgres.New(d.app)).Session(t.Context(), token, org)
	if err != nil {
		t.Fatalf("%s signs in: %v", email, err)
	}

	return p
}

// goalCLI runs goal commands with GOAL_DATABASE_ADMIN_URL on d and nothing listening at the API:
// goald and the identity service are stopped.
func (d recoveryDB) goalCLI(t *testing.T) func(args ...string) (string, error) {
	t.Helper()

	env := testEnv(t, "http://127.0.0.1:1", map[string]string{"GOAL_DATABASE_ADMIN_URL": d.adminURL})

	return func(args ...string) (string, error) {
		var buf bytes.Buffer

		err := run(t.Context(), args, &printer{w: &buf}, env, nil)

		return buf.String(), err
	}
}

// The organization of the recovery test.
const recoveredOrgName = "Acme"

// Spec 3 §9, §11.1 row 37: goal db recover-admin needs only the database (no goald answers here),
// creates the person when nobody has the email, and is audited; they then sign in as a platform
// admin.
func TestDB_RecoverAdmin(t *testing.T) {
	t.Parallel()

	d := newRecoveryDB(t)
	goal := d.goalCLI(t)

	out, err := goal("db", "recover-admin", "Lost.Owner@Example.com")
	if err != nil || !strings.Contains(out, "✓ lost.owner@example.com is a platform admin (a new user: they sign in with a code") {
		t.Fatalf("goal db recover-admin = %q, %v", out, err)
	}

	if p := d.signIn(t, "lost.owner@example.com", ""); !p.PlatformAdmin || p.Name != "lost.owner@example.com" {
		t.Errorf("the recovered admin signs in as %+v; want a platform admin", p)
	}

	if n := d.count(t, `SELECT count(*) FROM platform_admins WHERE added_by = 'system:recovery'`); n != 1 {
		t.Errorf("%d platform admins added by system:recovery; want 1", n)
	}

	if n := d.count(t, `SELECT count(*) FROM auth_events WHERE kind = 'recovery.used'`); n != 1 {
		t.Errorf("%d recovery.used events; want 1", n)
	}
}

// goal db recover-owner makes an existing person an owner of an organization nobody owns, audited
// in its log as member.added {via: recovery}; an unknown organization is refused and changes
// nothing.
func TestDB_RecoverOwner(t *testing.T) {
	t.Parallel()

	d := newRecoveryDB(t)
	goal := d.goalCLI(t)

	acme, err := postgres.New(d.app).CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: orgAcme, Name: recoveredOrgName})
	if err != nil {
		t.Fatal(err)
	}

	execTest(t, d.admin, `INSERT INTO auth."user" (name, email, "emailVerified") VALUES ('Ada', 'ada@example.com', true)`)

	out, err := goal("db", "recover-owner", orgAcme, "ADA@example.com")
	if err != nil || !strings.Contains(out, "✓ ada@example.com is an owner of Acme (acme); recorded") || strings.Contains(out, "new user") {
		t.Fatalf("goal db recover-owner = %q, %v", out, err)
	}

	if p := d.signIn(t, "ada@example.com", acme.ID); p.OrgRole != domain.OrgOwner {
		t.Errorf("Ada signs in to acme as %q; want owner", p.OrgRole)
	}

	if n := d.count(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'member.added'
		AND actor_kind = 'system' AND actor_id = 'recovery' AND data->>'via' = 'recovery' AND data->>'role' = 'owner'`, acme.ID); n != 1 {
		t.Errorf("%d member.added {via: recovery} entries in acme's audit log; want 1", n)
	}

	if _, err := goal("db", "recover-owner", "nowhere", "stranger@example.com"); err == nil || !strings.Contains(err.Error(), `no live organization "nowhere"`) {
		t.Errorf("goal db recover-owner of an unknown org = %v; want it refused", err)
	}

	if n := d.count(t, `SELECT count(*) FROM auth."user"`); n != 1 {
		t.Errorf("%d users after a refused recovery; want Ada alone", n)
	}
}
