//go:build integration

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"maps"
	"net/url"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// mxDatabase is a throwaway database at goose head in the PostgreSQL of GOAL_TEST_DATABASE_URL
// (compose's, as its superuser), with the roles and grants goal db init gives; its pools switch
// role with the startup parameter, so no role's password is needed or changed.
type mxDatabase struct {
	admin, app, auth *pgxpool.Pool
}

func newMXDatabase(t *testing.T) mxDatabase {
	t.Helper()

	base := os.Getenv("GOAL_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("GOAL_TEST_DATABASE_URL is not set; run task test-integration")
	}

	roles, err := os.ReadFile("../adapters/postgres/roles.sql")
	if err != nil {
		t.Fatal(err)
	}

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "goal_test_api_" + hex.EncodeToString(b)

	exec := func(url, sql string) {
		t.Helper()

		ctx := context.WithoutCancel(t.Context()) // also run by Cleanup, after the test's context ended

		conn, err := pgx.Connect(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close(ctx) }()

		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	exec(base, "BEGIN; SELECT pg_advisory_xact_lock(hashtext('goal.roles')); "+string(roles)+"; COMMIT")
	exec(base, "CREATE DATABASE "+name)
	t.Cleanup(func() { exec(base, "DROP DATABASE "+name+" WITH (FORCE)") })

	as := func(role string) string {
		u, err := url.Parse(base)
		if err != nil {
			t.Fatal(err)
		}

		u.Path = "/" + name
		if role != "" {
			q := u.Query()
			q.Set("role", role)
			u.RawQuery = q.Encode()
		}

		return u.String()
	}

	exec(as(""), "REVOKE TEMPORARY, CONNECT ON DATABASE "+name+" FROM PUBLIC; GRANT CONNECT, CREATE ON DATABASE "+name+" TO goal_owner; "+
		"GRANT CONNECT ON DATABASE "+name+" TO goal_app, goal_auth; GRANT USAGE, CREATE ON SCHEMA public TO goal_owner; "+
		"GRANT USAGE ON SCHEMA public TO goal_app, goal_auth; CREATE EXTENSION IF NOT EXISTS vector")

	pool := func(role string) *pgxpool.Pool {
		p, err := postgres.Open(t.Context(), as(role))
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(p.Close)

		return p
	}

	if err := postgres.Migrate(t.Context(), pool(postgres.OwnerRole)); err != nil {
		t.Fatal(err)
	}

	return mxDatabase{admin: pool(""), app: pool(postgres.AppRole), auth: pool(postgres.AuthRole)}
}

func (d mxDatabase) exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// inOrg runs sql as goal_app in a transaction bound to org.
func (d mxDatabase) inOrg(t *testing.T, org domain.OrgID, sql string, args ...any) {
	t.Helper()

	err := pgx.BeginFunc(t.Context(), d.app, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "SELECT set_config('goal.org_id', $1, true)", string(org)); err != nil {
			return err
		}

		_, err := tx.Exec(t.Context(), sql, args...)

		return err
	})
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// seed writes the matrix's organizations, factories and people as goald and the identity service
// would: organizations through goal_provision_org, Better Auth's users, sessions and members as
// goal_auth, factory roles under row-level security. Sessions are dated by the matrix's clock and
// expire a week from now on the wall clock, which the database compares with.
func (d mxDatabase) seed(t *testing.T) {
	t.Helper()

	for _, o := range []struct {
		id         domain.OrgID
		slug, name string
		factories  []domain.FactoryID
	}{{mxAcme, "acme", "Acme", []domain.FactoryID{mxF, mxG}}, {mxOther, "other", "Other", []domain.FactoryID{mxH}}} {
		d.inOrg(t, o.id, "SELECT * FROM goal_provision_org($1, $2, $3, 'Ac', $4, $5, 'system:test', $6)", string(o.id), o.slug, o.name,
			nil, nil, nil) // no owner, no request

		for _, f := range o.factories {
			d.inOrg(t, o.id, "INSERT INTO factories (id, org_id, name, definition, definition_sha256) VALUES ($1, goal_org(), $2, '{}', 'sha')",
				string(f), "f-"+string(f)[len(f)-4:])
		}
	}

	for _, p := range mxPeople() {
		s := p.session()
		d.exec(t, d.auth, `INSERT INTO auth."user" (id, name, email, "emailVerified") VALUES ($1, $2, $3, true)`, string(p.user), p.name, p.name+"@example.com")
		d.exec(t, d.auth, `INSERT INTO auth.session (token, "userId", "createdAt", "updatedAt", "expiresAt", "signInMethod")
			VALUES ($1, $2, $3, $3, $4, 'email_code')`, p.token, string(p.user), s.CreatedAt, time.Now().Add(7*24*time.Hour))

		if p.platformAdmin {
			d.exec(t, d.admin, `INSERT INTO platform_admins (user_id, added_by) VALUES ($1, 'system:test')`, string(p.user))
		}

		if p.org != "" {
			d.exec(t, d.auth, `INSERT INTO auth.member ("organizationId", "userId", role, "createdAt") VALUES ($1, $2, $3, now())`,
				string(p.org), string(p.user), string(p.role))
		}

		if p.factory != "" {
			d.inOrg(t, p.org, "INSERT INTO factory_roles (factory_id, user_id, role, granted_by) VALUES ($1, $2, $3, 'system:test')",
				string(mxF), string(p.user), string(p.factory))
		}
	}

	d.seedKeys(t)
	d.seedTokens(t)
}

// seedTokens writes the matrix's personal tokens and their own people, as goald and the identity
// service would, then what happened afterwards: a demotion and a removal as goal_auth (the
// removal deletes the token by its foreign key), an expiry, a revocation, the organization's
// deletion. The owner's CLI session is a second session row of theirs.
func (d mxDatabase) seedTokens(t *testing.T) {
	t.Helper()

	owner := mxPeople()[0]
	d.exec(t, d.auth, `INSERT INTO auth.session (token, "userId", "createdAt", "updatedAt", "expiresAt", "signInMethod")
		VALUES ($1, $2, $3, $3, $4, 'device')`, mxBearerSession, string(owner.user), owner.session().CreatedAt, time.Now().Add(7*24*time.Hour))
	d.inOrg(t, mxGone, "SELECT * FROM goal_provision_org($1, 'gone', 'Gone', 'Go', $2, $3, 'system:test', $4)", string(mxGone),
		nil, nil, nil) // no owner, no request

	for _, k := range mxTokens() {
		p := k.person()
		if k.of == "" {
			d.exec(t, d.auth, `INSERT INTO auth."user" (id, name, email, "emailVerified") VALUES ($1, $2, $3, true)`, string(p.user), p.name, p.name+"@example.com")
			d.exec(t, d.auth, `INSERT INTO auth.member ("organizationId", "userId", role, "createdAt") VALUES ($1, $2, $3, now())`,
				string(p.org), string(p.user), string(p.role))

			if p.platformAdmin {
				d.exec(t, d.admin, `INSERT INTO platform_admins (user_id, added_by) VALUES ($1, 'system:test')`, string(p.user))
			}

			if p.factory != "" {
				d.inOrg(t, p.org, "INSERT INTO factory_roles (factory_id, user_id, role, granted_by) VALUES ($1, $2, $3, 'system:test')",
					string(mxF), string(p.user), string(p.factory))
			}
		}

		created, expires := "now()", "now() + interval '30 days'"
		if k.after == mxExpired {
			created, expires = "now() - interval '2 days'", "now() - interval '1 day'"
		}

		scopes := make([]string, 0, len(k.scopes))
		for _, s := range k.scopes {
			scopes = append(scopes, string(s))
		}

		d.inOrg(t, p.org, `INSERT INTO personal_tokens (id, user_id, name, hint, hash, scopes, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, `+created+`, `+expires+`)`, string(k.id), string(p.user), k.name, k.secret[:13], k.hash, scopes)

		switch k.after {
		case mxDemoted:
			d.exec(t, d.auth, `UPDATE auth.member SET role = 'member' WHERE "organizationId" = $1 AND "userId" = $2`, string(p.org), string(p.user))
		case mxRemoved:
			d.exec(t, d.auth, `DELETE FROM auth.member WHERE "organizationId" = $1 AND "userId" = $2`, string(p.org), string(p.user))
		case mxRevoked:
			d.inOrg(t, p.org, "UPDATE personal_tokens SET revoked_at = now(), revoked_by = 'system:test' WHERE id = $1", string(k.id))
		case mxOrgDeleted:
			d.exec(t, d.admin, "UPDATE organizations SET deleted_at = now() WHERE id = $1", string(p.org))
		}
	}
}

// seedKeys writes the matrix's API keys as goald does, under row-level security, with the fake's
// ids, hashes and grants: the expired one created two days ago and expired yesterday, the revoked
// one revoked now.
func (d mxDatabase) seedKeys(t *testing.T) {
	t.Helper()

	for _, k := range mxKeys() {
		created, expires := "now()", "now() + interval '90 days'"
		if k.expired {
			created, expires = "now() - interval '2 days'", "now() - interval '1 day'"
		}

		scopes := make([]string, 0, len(k.key.Scopes))
		for _, s := range k.key.Scopes {
			scopes = append(scopes, string(s))
		}

		d.inOrg(t, k.key.Org, `INSERT INTO api_keys (id, name, hint, hash, scopes, role_ceiling, all_factories, created_by, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'system:test', `+created+`, `+expires+`)`,
			string(k.key.ID), k.key.Name, k.secret[:12], k.hash, scopes, string(k.key.Ceiling), k.key.Factories == nil)

		for _, f := range k.key.Factories {
			d.inOrg(t, k.key.Org, "INSERT INTO api_key_factories (key_id, factory_id) VALUES ($1, $2)", string(k.key.ID), string(f))
		}

		if k.revoked {
			d.inOrg(t, k.key.Org, "UPDATE api_keys SET revoked_at = now(), revoked_by = 'system:test' WHERE id = $1", string(k.key.ID))
		}
	}
}

// Spec 3 §13: the two-org matrix over HTTP with the PostgreSQL Identity. Every person of the
// matrix is resolved from Better Auth's and Goal's rows, through goal_principal, and must be
// authorized exactly as the fake's same person is, then answered so over HTTP; so is every API key
// of the matrix, resolved through goal_api_key (00046).
func TestAPI_TwoOrgMatrixOverHTTPWithPostgres(t *testing.T) {
	t.Parallel()

	d := newMXDatabase(t)
	d.seed(t)

	identity := postgres.NewIdentity(postgres.New(d.app))
	_, fake := mxIdentity(t)
	fakeBy := map[string]mxPrincipal{}

	for _, p := range fake {
		fakeBy[p.name] = p
	}

	principals := []mxPrincipal{fakeBy["anonymous"]}

	for _, p := range mxPeople() {
		principals = append(principals, mxPrincipal{name: p.name, header: p.cookie(), resolve: func(org domain.OrgID) (domain.Principal, error) {
			return identity.Session(t.Context(), p.token, org)
		}})
	}

	for _, k := range mxKeys() {
		principals = append(principals, mxPrincipal{name: k.key.Name, header: k.header(), resolve: func(domain.OrgID) (domain.Principal, error) {
			return identity.APIKey(t.Context(), k.hash)
		}})
	}

	principals = append(principals, mxPrincipal{
		name: mxBearerName, header: fakeBy[mxBearerName].header, resolve: func(org domain.OrgID) (domain.Principal, error) {
			return identity.Session(t.Context(), mxBearerSession, org)
		},
	})

	for _, k := range mxTokens() {
		principals = append(principals, mxPrincipal{name: k.name, header: k.header(), resolve: func(domain.OrgID) (domain.Principal, error) {
			return identity.PersonalToken(t.Context(), k.hash)
		}})
	}

	authz := NewPermissions(porttest.NewClock(mxNow))
	contract := contractOps(t)

	for _, id := range slices.Sorted(maps.Keys(contract)) {
		op := contract[id]

		for _, p := range principals[1:] {
			for _, org := range []domain.OrgID{"", mxAcme, mxOther} {
				if got, want := mxAnswer(t, authz, id, op, p, org), mxAnswer(t, authz, id, op, fakeBy[p.name], org); !reflect.DeepEqual(got, want) {
					t.Errorf("%s by %s in %q: PostgreSQL's principal answers %+v, the fake's %+v", id, p.name, org, got, want)
				}
			}
		}
	}

	runHTTPMatrix(t, identity, principals)
}
