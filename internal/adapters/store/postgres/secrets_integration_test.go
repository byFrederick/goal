//go:build integration

package postgres

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/secrettest"
)

func TestSecrets_Conformance(t *testing.T) {
	t.Parallel()

	secrettest.Run(t, func(t *testing.T) secrettest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return secrettest.Harness{
			Secrets: func(key ports.MasterKey) ports.OrgSecrets { return NewSecrets(d.store, key) },
			Key:     func(ids ...string) ports.MasterKey { return testKey(t, ids...) },
			CopySealed: func(t *testing.T, fromOrg domain.OrgID, from string, toOrg domain.OrgID, to string) {
				t.Helper()

				tag, err := d.admin.Exec(t.Context(), `UPDATE secrets dst SET key_id = src.key_id, nonce = src.nonce, ciphertext = src.ciphertext
					FROM secrets src WHERE src.org_id = $1 AND src.name = $2 AND dst.org_id = $3 AND dst.name = $4`,
					string(fromOrg), from, string(toOrg), to)
				if err != nil || tag.RowsAffected() != 1 {
					t.Fatalf("copy the sealed value: %d rows, %v", tag.RowsAffected(), err)
				}
			},
			Audit: d.store,
			SignWebhook: func(t *testing.T, org domain.OrgID, name string) {
				t.Helper()

				n := domain.NewWebhook{Name: "github-acme", Sender: domain.SenderGitHub, Secret: name}
				if _, err := NewIntake(d.store).CreateWebhook(domain.WithOrg(t.Context(), org), n); err != nil {
					t.Fatal(err)
				}
			},
			NewOrg: func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
		}
	})
}

// Migration 00086 renames credentials to secrets: a credential sealed before it, with a type and
// an agent list, is a secret after it with the same id, name, scope and value, of source value,
// and the credentials API reads it as an api_key with no agents. The migration cannot reseal, so
// this holds only while domain.SecretAAD is domain.CredentialAAD's bytes.
func TestMigrate_CredentialsBecomeSecrets(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrateTo(t, openPool(t, db.owner), 84)

	store := New(openPool(t, db.app))
	org := newOrgID(t, store)
	key := testKey(t, "k1")

	const value = "sk-migrated-credential-2c4f"

	id := seedOldCredential(t, db, org, key, value)

	migrate(t, db.owner)

	ctx := domain.WithOrg(t.Context(), org)

	got, err := NewSecrets(store, key).Resolve(ctx, []string{seededCredential}, domain.SecretUse(credentialUse))
	if err != nil || len(got) != 1 || got[0].Value != value {
		t.Fatalf("Resolve of the migrated secret: %v; want its value", err)
	}

	s := got[0].Secret
	if s.ID != domain.SecretID(id) || s.Source != domain.SecretSourceValue || s.Description != "error reports" ||
		!slices.Equal(s.Scope.Factories, []domain.FactoryID{seededFactory}) {
		t.Errorf("migrated secret = %+v; want the credential's id, description and scope, of source value", s)
	}

	cred, err := NewCredentials(store, key).Get(ctx, seededCredential)
	if err != nil || cred.Type != domain.CredentialAPIKey || len(cred.Agents) != 0 {
		t.Errorf("the credentials API reads %+v, %v; want an api_key with no agents", cred, err)
	}

	if ids, err := ConnectionKeyIDs(t.Context(), store); err != nil || !slices.Equal(ids, []string{"k1"}) {
		t.Errorf("ConnectionKeyIDs = %v, %v; want the migrated secret's [k1]", ids, err)
	}
}

// seedOldCredential inserts credential seededCredential of org into 00065's credentials, an
// oauth_token for seededFactory's backend agent with value sealed under domain.CredentialAAD, and
// returns its id.
func seedOldCredential(t *testing.T, db testDB, org domain.OrgID, key ports.MasterKey, value string) domain.CredentialID {
	t.Helper()

	id := domain.CredentialID(uuid.Must(uuid.NewV7()).String())

	keyID, nonce, ct, err := key.Seal([]byte(value), domain.CredentialAAD(org, id, key.Active()))
	if err != nil {
		t.Fatal(err)
	}

	_, err = openPool(t, db.admin).Exec(t.Context(), `INSERT INTO credentials
		(id, org_id, name, type, description, factory_ids, agents, key_id, nonce, ciphertext, created_by, updated_by)
		VALUES ($1, $2, $3, 'oauth_token', 'error reports', ARRAY[$4::uuid], $5, $6, $7, $8, 'user:local', 'user:local')`,
		string(id), string(org), seededCredential, string(seededFactory), `{"`+string(seededFactory)+`": ["backend"]}`, keyID, nonce, ct)
	if err != nil {
		t.Fatal(err)
	}

	return id
}

// A webhook secret's previous value is sealed in its own columns under its own key id:
// goal_connection_key_ids lists it, goal db rekey reseals it beside the current one, and a lapsed
// one goes at the next rekey.
func TestRekey_ResealsPreviousSecrets(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	org := newOrgID(t, d.store)
	ctx := domain.WithOrg(t.Context(), org)
	w := seedWebhook(t, d, org, "github-acme")

	v := "whsec-integration-replaced"
	if _, err := NewSecrets(d.store, testKey(t, "k2", "k1")).Patch(ctx, w.Secret, domain.SecretPatch{Replace: &v}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if ids, err := ConnectionKeyIDs(t.Context(), d.store); err != nil || !slices.Equal(ids, []string{"k1", "k2"}) {
		t.Errorf("ConnectionKeyIDs = %v, %v; want [k1 k2]: the previous value's and the current one's", ids, err)
	}

	n, err := Rekey(t.Context(), d.owner, testKey(t, "k3", "k1", "k2"))
	if err != nil || n != 2 {
		t.Fatalf("Rekey = %d, %v; want the value and the previous one", n, err)
	}

	got, err := NewSecrets(d.store, testKey(t, "k3")).OpenSigning(ctx, w.SecretID)
	if err != nil || got.Current != v || got.Previous != "whsec-integration" {
		t.Errorf("OpenSigning under k3 after Rekey = %+v, %v; want both values", got, err)
	}

	lapsedPreviousGoes(t, d, org, w.SecretID)
}

// lapsedPreviousGoes moves secret id's previous value past its 24 hours and checks it no longer
// opens and goes at the next rekey.
func lapsedPreviousGoes(t *testing.T, d connectionsDB, org domain.OrgID, id domain.SecretID) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), org)

	if _, err := d.admin.Exec(t.Context(), `UPDATE secrets SET previous_until = now() - interval '1 second' WHERE id = $1`, string(id)); err != nil {
		t.Fatal(err)
	}

	if got, err := NewSecrets(d.store, testKey(t, "k3")).OpenSigning(ctx, id); err != nil || got.Previous != "" || got.PreviousUntil != nil {
		t.Errorf("OpenSigning once the previous value lapsed = %+v, %v; want the value alone", got, err)
	}

	if _, err := Rekey(t.Context(), d.owner, testKey(t, "k4", "k3")); err != nil {
		t.Fatal(err)
	}

	var left int
	if err := d.admin.QueryRow(t.Context(), `SELECT count(*) FROM secrets WHERE previous_key_id IS NOT NULL`).Scan(&left); err != nil || left != 0 {
		t.Errorf("%d previous values left after a rekey past their 24 hours, %v; want none", left, err)
	}
}
