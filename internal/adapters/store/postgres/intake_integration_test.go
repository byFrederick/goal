//go:build integration

package postgres

import (
	"bytes"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/intaketest"
)

func TestIntake_Conformance(t *testing.T) {
	t.Parallel()

	intaketest.Run(t, func(t *testing.T) intaketest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return intaketest.Harness{
			Intake:  func(key ports.MasterKey) ports.Intake { return NewIntake(d.store, key) },
			Secrets: func(key ports.MasterKey) ports.OrgSecrets { return NewSecrets(d.store, key) },
			Key:     func(ids ...string) ports.MasterKey { return testKey(t, ids...) },
			Store:   d.store,
			NewOrg:  func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, d.store)
			},
		}
	})
}

// seedEndpoint creates endpoint name on a new factory of org and returns it with its secret.
func seedEndpoint(t *testing.T, in *Intake, org domain.OrgID, name string) domain.WebhookSecret {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), org)

	s, err := in.CreateEndpoint(ctx, domain.NewWebhookEndpoint{FactoryID: saveFactory(ctx, t, in.s), Name: name, Kind: domain.WebhookGitHub})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// ADR 0026 decision 2: no column of any table holds a signing secret in the clear, as hex or as
// text, after a create, a rotation and a delivery with signature headers; and no delivery keeps a
// signature.
func TestIntake_NoPlaintextInAnyColumn(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	in := NewIntake(d.store, testKey(t, "k1"))
	org := newOrgID(t, d.store)
	ctx := domain.WithOrg(t.Context(), org)

	first := seedEndpoint(t, in, org, "ci-hooks")

	second, err := in.RotateSecret(ctx, first.Endpoint.ID)
	if err != nil {
		t.Fatal(err)
	}

	const signature = "sha256=5f1c0ffee5ignature"

	_, err = in.RecordDelivery(ctx, domain.NewWebhookDelivery{
		EndpointID: first.Endpoint.ID, ReceivedAt: time.Now(), Outcome: domain.DeliveryRefused, Reason: "signature did not match",
		Headers: map[string]string{"X-Hub-Signature-256": signature, "X-Goal-Signature": signature, "X-GitHub-Event": "push"},
	})
	if err != nil {
		t.Fatal(err)
	}

	tables := queryStrings(t, d.admin, `SELECT c.relname::text FROM pg_class c
		WHERE c.relnamespace = current_schema()::text::regnamespace AND c.relkind IN ('r', 'p')`)
	if !slices.Contains(tables, "webhook_endpoints") {
		t.Fatalf("no webhook_endpoints table among %v", tables)
	}

	for _, table := range tables {
		var hits int
		// Every column as text, bytea included (its hex form), searched for each secret and the
		// signature.
		err := d.admin.QueryRow(t.Context(), `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()+` r
			WHERE row_to_json(r)::text LIKE '%' || $1 || '%' OR row_to_json(r)::text LIKE '%' || $2 || '%'
			   OR row_to_json(r)::text LIKE '%' || $3 || '%'`,
			hex.EncodeToString(first.Secret), hex.EncodeToString(second.Secret), signature).Scan(&hits)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}

		if hits != 0 {
			t.Errorf("table %s has %d rows holding a signing secret or a signature", table, hits)
		}
	}

	var (
		key    string
		cipher []byte
	)

	if err := d.admin.QueryRow(t.Context(), `SELECT key_id, ciphertext FROM webhook_endpoints`).Scan(&key, &cipher); err != nil {
		t.Fatal(err)
	}

	if key != "k1" || bytes.Contains(cipher, second.Secret) {
		t.Errorf("sealed under %q, ciphertext %x; want k1 and no secret", key, cipher)
	}

	// The table refuses a signature header whoever writes it.
	err = inOrgTx(t, d.store.pool, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO webhook_deliveries (id, endpoint_id, received_at, headers, outcome)
			VALUES (gen_random_uuid(), $1, now(), '{"x-hub-signature-256": "sha256=00"}', 'refused')`, string(first.Endpoint.ID))

		return err
	})
	if err == nil || !strings.Contains(err.Error(), "check") {
		t.Errorf("a delivery with a signature header = %v; want a check violation", err)
	}
}

// ADR 0026 decision 2: a ciphertext copied to another endpoint, of the same organization or
// another, does not open there, and the refusal holds no secret.
func TestIntake_AADMismatchRefused(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	in := NewIntake(d.store, testKey(t, "k1"))
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)

	src := seedEndpoint(t, in, acme, "ci-hooks")
	sameOrg := seedEndpoint(t, in, acme, "deploys")
	otherOrg := seedEndpoint(t, in, labs, "ci-hooks")

	for _, to := range []struct {
		what string
		org  domain.OrgID
		id   domain.WebhookEndpointID
	}{
		{"another endpoint of the organization", acme, sameOrg.Endpoint.ID},
		{"an endpoint of another organization", labs, otherOrg.Endpoint.ID},
	} {
		tag, err := d.admin.Exec(t.Context(), `UPDATE webhook_endpoints dst SET key_id = src.key_id, nonce = src.nonce, ciphertext = src.ciphertext
			FROM webhook_endpoints src WHERE src.id = $1 AND dst.id = $2`, string(src.Endpoint.ID), string(to.id))
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("copy the sealed secret: %d rows, %v", tag.RowsAffected(), err)
		}

		got, err := in.OpenSecret(domain.WithOrg(t.Context(), to.org), to.id)

		switch {
		case err == nil:
			t.Errorf("%s opened a copied ciphertext: %x", to.what, got)
		case strings.Contains(err.Error(), hex.EncodeToString(src.Secret)) || strings.Contains(err.Error(), string(src.Secret)):
			t.Errorf("%s: the refusal %v holds the secret", to.what, err)
		}
	}

	got, err := in.OpenSecret(domain.WithOrg(t.Context(), acme), src.Endpoint.ID)
	if err != nil || !bytes.Equal(got, src.Secret) {
		t.Errorf("the original no longer opens: %v", err)
	}
}

// goal db rekey reseals endpoints' secrets as it reseals connections and credentials, and goald
// refuses to start without a key only an endpoint is sealed under.
func TestRekey_ResealsWebhookSecrets(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	org := newOrgID(t, d.store)
	s := seedEndpoint(t, NewIntake(d.store, testKey(t, "k9")), org, "ci-hooks")

	if ids, err := ConnectionKeyIDs(t.Context(), d.store); err != nil || !slices.Equal(ids, []string{"k9"}) {
		t.Errorf("ConnectionKeyIDs = %v, %v; want [k9]", ids, err)
	}

	if _, err := Rekey(t.Context(), d.owner, testKey(t, "k3")); err == nil || !strings.Contains(err.Error(), string(s.Endpoint.ID)) {
		t.Errorf("Rekey without k9 = %v; want an error naming the endpoint", err)
	}

	n, err := Rekey(t.Context(), d.owner, testKey(t, "k2", "k9"))
	if err != nil || n != 1 {
		t.Fatalf("Rekey = %d, %v; want the one endpoint", n, err)
	}

	got, err := NewIntake(d.store, testKey(t, "k2")).OpenSecret(domain.WithOrg(t.Context(), org), s.Endpoint.ID)
	if err != nil || !bytes.Equal(got, s.Secret) {
		t.Errorf("OpenSecret under k2 after Rekey: %v", err)
	}

	if n, err := Rekey(t.Context(), d.owner, testKey(t, "k2", "k9")); err != nil || n != 0 {
		t.Errorf("Rekey again = %d, %v; want nothing left", n, err)
	}

	if ids, err := ConnectionKeyIDs(t.Context(), d.store); err != nil || !slices.Equal(ids, []string{"k2"}) {
		t.Errorf("ConnectionKeyIDs = %v, %v; want [k2]", ids, err)
	}
}

// As goal_app, another organization neither reads nor writes the intake tables' rows, goal_app
// cannot rewrite an endpoint's id, organization, factory or kind (00113 grants its name and
// enabled, for renaming the organization's webhooks), nor change or delete a
// delivery or a firing; only the definers reach across organizations.
func TestIntake_RLSAndGrants(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	in := NewIntake(d.store, testKey(t, "k1"))
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)
	s := seedEndpoint(t, in, acme, "ci-hooks")
	ctx := domain.WithOrg(t.Context(), acme)

	if _, err := in.RecordDelivery(ctx, domain.NewWebhookDelivery{EndpointID: s.Endpoint.ID, ReceivedAt: time.Now(), Outcome: domain.DeliveryAccepted}); err != nil {
		t.Fatal(err)
	}

	_, _, err := in.RecordFiring(ctx, domain.TriggerFiring{
		FactoryID: s.Endpoint.FactoryID, Trigger: "nightly-audit", Source: domain.TriggerSchedule, EventKey: "schedule",
		IdempotencyKey: "2026-10-01T00:00:00Z", Outcome: domain.FiringSkipped, Reason: "limits: perDay", At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	labsSeesNoIntake(t, d.store.pool, labs)

	for _, stmt := range []string{
		`UPDATE webhook_endpoints SET id = gen_random_uuid()`,
		`UPDATE webhook_endpoints SET org_id = '` + string(labs) + `'`,
		`UPDATE webhook_endpoints SET factory_id = gen_random_uuid()`,
		`UPDATE webhook_endpoints SET kind = 'generic'`,
		`UPDATE webhook_endpoints SET created_at = now()`,
		`UPDATE webhook_deliveries SET outcome = 'created'`,
		`DELETE FROM webhook_deliveries`,
		`UPDATE trigger_firings SET outcome = 'created'`,
		`DELETE FROM trigger_firings`,
	} {
		err := inOrgTx(t, d.store.pool, acme, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), stmt); return err })
		if err == nil {
			t.Errorf("goal_app ran %s", stmt)
		}
	}

	// Without an organization, goal_app reaches an endpoint's organization and the prune only
	// through the definers.
	if org, err := in.EndpointOrg(t.Context(), s.Endpoint.ID); err != nil || org != acme {
		t.Errorf("EndpointOrg = %q, %v; want %q", org, err, acme)
	}

	if n, err := in.PruneDeliveries(t.Context(), time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Errorf("PruneDeliveries = %d, %v; want 1", n, err)
	}
}

// labsSeesNoIntake checks that org, as goal_app, sees no row of the intake tables and deletes no
// endpoint.
func labsSeesNoIntake(t *testing.T, pool *pgxpool.Pool, org domain.OrgID) {
	t.Helper()

	err := inOrgTx(t, pool, org, func(tx pgx.Tx) error {
		for _, table := range []string{"webhook_endpoints", "webhook_deliveries", "trigger_firings"} {
			var n int
			if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("labs sees %d rows of %s, %v; want none", n, table, err)
			}
		}

		tag, err := tx.Exec(t.Context(), `DELETE FROM webhook_endpoints WHERE true`)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("labs deletes %d endpoints, %v; want none", tag.RowsAffected(), err)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// seedWebhook creates the organization webhook name of org, signed by a new secret, open to a
// new factory, and returns it.
func seedWebhook(t *testing.T, d connectionsDB, org domain.OrgID, name string) domain.WebhookEndpoint {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), org)
	secret := domain.WebhookSecretName(name)

	if _, err := NewSecrets(d.store, testKey(t, "k1")).Create(ctx, domain.NewSecret{Name: secret, Value: "whsec-integration"}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	in := NewIntake(d.store, testKey(t, "k1"))

	w, err := in.CreateWebhook(ctx, domain.NewWebhook{Name: name, Sender: domain.SenderGitHub, Secret: secret})
	if err != nil {
		t.Fatal(err)
	}

	w, err = in.PatchWebhook(ctx, w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: saveFactory(ctx, t, d.store), Repositories: []string{"acme/api"}}}})
	if err != nil {
		t.Fatal(err)
	}

	return w
}

// Migration 00113: another organization sees no webhook opening; goal_app cannot change a
// webhook's sender, secret or factory, nor an opening's webhook or factory; the database refuses
// a reserved name and the delete of a secret a webhook uses, and a rename of the secret follows;
// goal_connection_key_ids skips the webhook's null key id; the purge removes both.
func TestIntake_OrgWebhooksRLSAndGrants(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)
	w := seedWebhook(t, d, acme, "github-acme")

	labsSeesNoIntake(t, d.store.pool, labs)

	if err := inOrgTx(t, d.store.pool, labs, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM webhook_openings`).Scan(&n); err != nil || n != 0 {
			t.Errorf("labs sees %d openings, %v; want none", n, err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	appRefuses(t, d, acme, []string{
		`UPDATE webhook_endpoints SET sender = 'gitlab'`,
		`UPDATE webhook_endpoints SET secret_id = gen_random_uuid()`,
		`UPDATE webhook_endpoints SET factory_id = gen_random_uuid()`,
		`UPDATE webhook_endpoints SET token_header = 'X-Token'`,
		`UPDATE webhook_openings SET factory_id = gen_random_uuid()`,
		`UPDATE webhook_openings SET webhook_id = gen_random_uuid()`,
		`INSERT INTO webhook_endpoints (id, name, sender, secret_id) VALUES (gen_random_uuid(), 'schedule', 'github', '` + string(w.SecretID) + `')`,
		`INSERT INTO webhook_endpoints (id, name, sender, secret_id) VALUES (gen_random_uuid(), 'github-acme', 'github', '` + string(w.SecretID) + `')`,
		`INSERT INTO webhook_endpoints (id, name, sender) VALUES (gen_random_uuid(), 'no-secret', 'github')`,
		`DELETE FROM secrets WHERE id = '` + string(w.SecretID) + `'`,
	})

	ctx := domain.WithOrg(t.Context(), acme)
	renamed := "github-signing"

	if _, err := NewSecrets(d.store, testKey(t, "k1")).Patch(ctx, w.Secret, domain.SecretPatch{Name: &renamed}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if got, err := NewIntake(d.store, testKey(t, "k1")).Endpoint(ctx, w.ID); err != nil || got.Secret != renamed || len(got.Openings) != 1 {
		t.Errorf("Endpoint after the secret's rename = %+v, %v; want it signed by %s, still open", got, err, renamed)
	}

	if ids, err := ConnectionKeyIDs(t.Context(), d.store); err != nil || !slices.Equal(ids, []string{"k1"}) {
		t.Errorf("ConnectionKeyIDs = %v, %v; want [k1], the secret's, and no empty id", ids, err)
	}

	purgeRemovesWebhooks(t, d, acme)
}

// appRefuses checks that goal_app, in org, runs none of stmts.
func appRefuses(t *testing.T, d connectionsDB, org domain.OrgID, stmts []string) {
	t.Helper()

	for _, stmt := range stmts {
		err := inOrgTx(t, d.store.pool, org, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), stmt); return err })
		if err == nil {
			t.Errorf("goal_app ran %s", stmt)
		}
	}
}

// purgeRemovesWebhooks deletes and purges org, whose webhook uses one of its secrets, and checks
// no webhook row is left.
func purgeRemovesWebhooks(t *testing.T, d connectionsDB, org domain.OrgID) {
	t.Helper()

	deleted := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	if err := d.store.DeleteOrg(domain.WithOrg(t.Context(), org), domain.LocalUser, deleted); err != nil {
		t.Fatal(err)
	}

	if ok, err := d.store.PurgeOrg(t.Context(), org, deleted.Add(time.Second), nil); !ok || err != nil {
		t.Fatalf("PurgeOrg with a webhook and its secret = %v, %v", ok, err)
	}

	var n int
	if err := d.admin.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM webhook_endpoints) + (SELECT count(*) FROM webhook_openings)`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d webhook rows left after the purge, %v; want none", n, err)
	}
}
