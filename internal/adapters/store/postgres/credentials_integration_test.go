//go:build integration

package postgres

import (
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/credentialtest"
)

// seededFactory is the factory seedCredential's credentials allow; seededCredential the name the
// tests give the credential they seed first.
const (
	seededFactory    = domain.FactoryID("00000000-0000-7000-8000-0000000000f1")
	seededCredential = "sentry-token"
)

var credentialUse = domain.CredentialUse{Factory: seededFactory, Agent: "backend", Run: "00000000-0000-7000-8000-0000000000aa"}

func TestCredentials_Conformance(t *testing.T) {
	t.Parallel()

	credentialtest.Run(t, func(t *testing.T) credentialtest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return credentialtest.Harness{
			Credentials: func(key ports.MasterKey) ports.Credentials { return NewCredentials(d.store, key) },
			Key:         func(ids ...string) ports.MasterKey { return testKey(t, ids...) },
			Audit:       d.store,
			NewOrg:      func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
		}
	})
}

// seedCredential creates credential name of org with value, scoped to seededFactory.
func seedCredential(t *testing.T, c *Credentials, org domain.OrgID, name, value string) domain.Credential {
	t.Helper()

	cred, err := c.Create(domain.WithOrg(t.Context(), org), domain.NewCredential{
		Name: name, Type: domain.CredentialAPIKey, Scope: domain.CredentialScope{Factories: []domain.FactoryID{seededFactory}},
		Agents: map[domain.FactoryID][]string{seededFactory: {"backend"}}, Value: value,
	}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	return cred
}

// ADR 0017 decision 3: no column of the credentials table, nor of any other table, the audit log
// included, holds a value in the clear, as text or as hex, after a create, a replace and a use.
func TestCredentials_NoPlaintextInAnyColumn(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	c := NewCredentials(d.store, testKey(t, "k1"))
	org := newOrgID(t, d.store)
	ctx := domain.WithOrg(t.Context(), org)

	const first, second = "sk-credential-first-7d3a", "sk-credential-second-1e8c"

	seedCredential(t, c, org, seededCredential, first)

	v := second
	if _, err := c.Patch(ctx, seededCredential, domain.CredentialPatch{Replace: &v}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Resolve(ctx, []string{seededCredential}, credentialUse); err != nil {
		t.Fatal(err)
	}

	tables := queryStrings(t, d.admin, `SELECT c.relname::text FROM pg_class c
		WHERE c.relnamespace = current_schema()::text::regnamespace AND c.relkind IN ('r', 'p')`)
	if !slices.Contains(tables, "credentials") {
		t.Fatalf("no credentials table among %v", tables)
	}

	for _, table := range tables {
		var hits int
		// Every column as text, bytea included (its hex form), searched for either value.
		err := d.admin.QueryRow(t.Context(), `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()+` r
			WHERE row_to_json(r)::text LIKE '%' || $1 || '%' OR row_to_json(r)::text LIKE '%' || $2 || '%'
			   OR row_to_json(r)::text LIKE '%' || encode(convert_to($1, 'UTF8'), 'hex') || '%'
			   OR row_to_json(r)::text LIKE '%' || encode(convert_to($2, 'UTF8'), 'hex') || '%'`, first, second).Scan(&hits)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}

		if hits != 0 {
			t.Errorf("table %s has %d rows holding a credential's value", table, hits)
		}
	}

	var (
		key    string
		cipher []byte
	)

	if err := d.admin.QueryRow(t.Context(), `SELECT key_id, ciphertext FROM credentials`).Scan(&key, &cipher); err != nil {
		t.Fatal(err)
	}

	if key != "k1" || strings.Contains(string(cipher), second) {
		t.Errorf("sealed under %q, ciphertext %q; want k1 and nothing readable", key, cipher)
	}
}

// ADR 0017 decision 3: a ciphertext copied to another credential, of the same organization or
// another, does not open there, and Resolve's refusal holds no value.
func TestCredentials_AADMismatchRefused(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	c := NewCredentials(d.store, testKey(t, "k1"))
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)

	const acmeValue = "sk-acme-credential-5c2b"

	seedCredential(t, c, acme, seededCredential, acmeValue)
	seedCredential(t, c, acme, "deploy-key", "sk-acme-other-0a9f")
	seedCredential(t, c, labs, seededCredential, "sk-labs-credential-6d1e")

	copySealed := func(t *testing.T, toOrg domain.OrgID, to string) {
		t.Helper()

		tag, err := d.admin.Exec(t.Context(), `UPDATE credentials dst SET key_id = src.key_id, nonce = src.nonce, ciphertext = src.ciphertext
			FROM credentials src WHERE src.org_id = $1 AND src.name = 'sentry-token' AND dst.org_id = $2 AND dst.name = $3`,
			string(acme), string(toOrg), to)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("copy the sealed value: %d rows, %v", tag.RowsAffected(), err)
		}
	}

	for _, to := range []struct {
		what string
		org  domain.OrgID
		name string
	}{
		{"another credential of the organization", acme, "deploy-key"},
		{"a credential of another organization", labs, seededCredential},
	} {
		copySealed(t, to.org, to.name)

		got, err := c.Resolve(domain.WithOrg(t.Context(), to.org), []string{to.name}, credentialUse)
		switch {
		case err == nil:
			t.Errorf("%s opened a copied ciphertext: %v", to.what, got)
		case strings.Contains(err.Error(), acmeValue):
			t.Errorf("%s: Resolve's refusal %v holds the value", to.what, err)
		}
	}

	got, err := c.Resolve(domain.WithOrg(t.Context(), acme), []string{seededCredential}, credentialUse)
	if err != nil || len(got) != 1 || got[0].Value != acmeValue {
		t.Errorf("the original no longer opens: %v", err)
	}
}

// goald refuses to start without a key a credential is sealed under: the key ids it checks
// include one that only a credential uses.
func TestConnectionKeyIDs_IncludesCredentials(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	org := newOrgID(t, d.store)
	seedConnections(t, NewConnections(d.store, testKey(t, "k1")), org, "s3cret")
	seedCredential(t, NewCredentials(d.store, testKey(t, "k9")), org, seededCredential, "sk-only-k9")

	ids, err := ConnectionKeyIDs(t.Context(), d.store)
	if err != nil || !slices.Equal(ids, []string{"k1", "k9"}) {
		t.Errorf("ConnectionKeyIDs = %v, %v; want [k1 k9]", ids, err)
	}
}

// goal db rekey reseals credentials as it reseals connections: after it, a credential sealed
// under k1 opens under k2 alone, and running it again reseals nothing.
func TestRekey_ResealsCredentials(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	org := newOrgID(t, d.store)
	cred := seedCredential(t, NewCredentials(d.store, testKey(t, "k1")), org, seededCredential, "sk-rekeyed-3b7c")

	if _, err := Rekey(t.Context(), d.owner, testKey(t, "k3")); err == nil || !strings.Contains(err.Error(), string(cred.ID)) ||
		strings.Contains(err.Error(), "sk-rekeyed-3b7c") {
		t.Errorf("Rekey without k1 = %v; want an error naming the credential", err)
	}

	n, err := Rekey(t.Context(), d.owner, testKey(t, "k2", "k1"))
	if err != nil || n != 1 {
		t.Fatalf("Rekey = %d, %v; want the one credential", n, err)
	}

	got, err := NewCredentials(d.store, testKey(t, "k2")).Resolve(domain.WithOrg(t.Context(), org), []string{seededCredential}, credentialUse)
	if err != nil || len(got) != 1 || got[0].Value != "sk-rekeyed-3b7c" {
		t.Errorf("Resolve under k2 after Rekey: %v", err)
	}

	if n, err := Rekey(t.Context(), d.owner, testKey(t, "k2", "k1")); err != nil || n != 0 {
		t.Errorf("Rekey again = %d, %v; want nothing left", n, err)
	}

	if ids, err := ConnectionKeyIDs(t.Context(), d.store); err != nil || !slices.Equal(ids, []string{"k2"}) {
		t.Errorf("ConnectionKeyIDs = %v, %v; want [k2]", ids, err)
	}
}

// As goal_app, another organization neither reads nor writes a credential, and goal_app cannot
// rewrite a credential's id, organization or creation.
func TestCredentials_RLSAndGrants(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	c := NewCredentials(d.store, testKey(t, "k1"))
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)
	seedCredential(t, c, acme, seededCredential, "sk-rls-4e2d")

	err := inOrgTx(t, d.store.pool, labs, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM credentials`).Scan(&n); err != nil || n != 0 {
			t.Errorf("labs sees %d credentials, %v; want none", n, err)
		}

		for _, stmt := range []string{`UPDATE credentials SET description = 'taken' WHERE true`, `DELETE FROM credentials WHERE true`} {
			if tag, err := tx.Exec(t.Context(), stmt); err != nil || tag.RowsAffected() != 0 {
				t.Errorf("%s as labs: %d rows, %v; want none", stmt, tag.RowsAffected(), err)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		`UPDATE credentials SET id = gen_random_uuid()`,
		`UPDATE credentials SET org_id = '` + string(labs) + `'`,
		`UPDATE credentials SET created_by = 'user:x'`,
		`UPDATE credentials SET created_at = now()`,
	} {
		err := inOrgTx(t, d.store.pool, acme, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), stmt); return err })
		if err == nil {
			t.Errorf("goal_app ran %s", stmt)
		}
	}
}
