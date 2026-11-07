//go:build integration

package postgres

import (
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
			Intake:  func() ports.Intake { return NewIntake(d.store) },
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

// ADR 0026 decision 2: no column of any table holds a webhook's signing secret in the clear, as
// hex or as text, after a create and a delivery with signature headers; and no delivery keeps a
// signature.
func TestIntake_NoPlaintextInAnyColumn(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	in := NewIntake(d.store)
	org := newOrgID(t, d.store)
	ctx := domain.WithOrg(t.Context(), org)

	w := seedWebhook(t, d, org, "ci-hooks")

	const signature = "sha256=5f1c0ffee5ignature"

	_, err := in.RecordDelivery(ctx, domain.NewWebhookDelivery{
		EndpointID: w.ID, ReceivedAt: time.Now(), Outcome: domain.DeliveryRefused, Reason: "signature did not match",
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
			hex.EncodeToString([]byte(seedWebhookValue)), seedWebhookValue, signature).Scan(&hits)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}

		if hits != 0 {
			t.Errorf("table %s has %d rows holding a signing secret or a signature", table, hits)
		}
	}

	// The table refuses a signature header whoever writes it.
	err = inOrgTx(t, d.store.pool, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO webhook_deliveries (id, endpoint_id, received_at, headers, outcome)
			VALUES (gen_random_uuid(), $1, now(), '{"x-hub-signature-256": "sha256=00"}', 'refused')`, string(w.ID))

		return err
	})
	if err == nil || !strings.Contains(err.Error(), "check") {
		t.Errorf("a delivery with a signature header = %v; want a check violation", err)
	}
}

// As goal_app, another organization neither reads nor writes the intake tables' rows, goal_app
// cannot rewrite a webhook's id, organization or creation time (00115 grants its name and
// enabled, for renaming it), nor change or delete a delivery or a firing; only the definers reach
// across organizations.
func TestIntake_RLSAndGrants(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	in := NewIntake(d.store)
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)
	s := seedWebhook(t, d, acme, "ci-hooks")
	factory := s.Openings[0].FactoryID
	ctx := domain.WithOrg(t.Context(), acme)

	if _, err := in.RecordDelivery(ctx, domain.NewWebhookDelivery{EndpointID: s.ID, ReceivedAt: time.Now(), Outcome: domain.DeliveryAccepted}); err != nil {
		t.Fatal(err)
	}

	_, _, err := in.RecordFiring(ctx, domain.TriggerFiring{
		FactoryID: factory, Trigger: "nightly-audit", Source: domain.TriggerSchedule, EventKey: "schedule",
		IdempotencyKey: "2026-10-01T00:00:00Z", Outcome: domain.FiringSkipped, Reason: "limits: perDay", At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	labsSeesNoIntake(t, d.store.pool, labs)

	for _, stmt := range []string{
		`UPDATE webhook_endpoints SET id = gen_random_uuid()`,
		`UPDATE webhook_endpoints SET org_id = '` + string(labs) + `'`,
		`UPDATE webhook_endpoints SET created_at = now()`,
		`UPDATE webhook_deliveries SET outcome = 'created'`,
		`DELETE FROM webhook_deliveries`,
		`UPDATE trigger_firings SET outcome = 'created'`,
		// DELETE on trigger_firings is goal_app's since 00120, for pruning (ADR 0040 decision 13).
	} {
		err := inOrgTx(t, d.store.pool, acme, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), stmt); return err })
		if err == nil {
			t.Errorf("goal_app ran %s", stmt)
		}
	}

	firingsPrunedAndChecked(t, d, in, acme, labs, factory)
	stateClearedOnlyInOrg(t, d.store.pool, acme, labs, factory)

	// Without an organization, goal_app reaches an endpoint's organization and the prune only
	// through the definers.
	if org, err := in.EndpointOrg(t.Context(), s.ID); err != nil || org != acme {
		t.Errorf("EndpointOrg = %q, %v; want %q", org, err, acme)
	}

	if n, err := in.PruneDeliveries(t.Context(), time.Now().Add(domain.DeliveryRetention+time.Hour)); err != nil || n != 1 {
		t.Errorf("PruneDeliveries = %d, %v; want 1", n, err)
	}
}

// firingsPrunedAndChecked checks migration 00121 and the firings' prune (ADR 0040 decision 16):
// the database refuses a missed firing without instants and a count on any other outcome, labs
// prunes none of acme's firings, and acme's goal_app deletes its old ones through PruneFirings.
func firingsPrunedAndChecked(t *testing.T, d connectionsDB, in *Intake, acme, labs domain.OrgID, factory domain.FactoryID) {
	t.Helper()

	for _, row := range []string{`'missed', 0`, `'skipped', 2`, `'missed', -1`} {
		stmt := `INSERT INTO trigger_firings (id, factory_id, trigger, source, event_key, idempotency_key, outcome, missed, at)
			VALUES (gen_random_uuid(), '` + string(factory) + `', 'nightly-audit', 'schedule', 'schedule', 'bad', ` + row + `, now())`
		if err := inOrgTx(t, d.store.pool, acme, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), stmt); return err }); err == nil {
			t.Errorf("the database took a firing of outcome and count %s", row)
		}
	}

	later := time.Now().Add(domain.FiringRetention + time.Hour)

	if n, err := in.PruneFirings(domain.WithOrg(t.Context(), labs), factory, later); err != nil || n != 0 {
		t.Errorf("labs PruneFirings = %d, %v; want none of acme's", n, err)
	}

	if n, err := in.PruneFirings(domain.WithOrg(t.Context(), acme), factory, later); err != nil || n != 1 {
		t.Errorf("PruneFirings = %d, %v; want acme's one firing", n, err)
	}
}

// stateClearedOnlyInOrg checks migration 00124: goal_app deletes a trigger's saved state (Clear),
// and labs, scoped to its own organization, deletes no row of acme's.
func stateClearedOnlyInOrg(t *testing.T, pool *pgxpool.Pool, acme, labs domain.OrgID, factory domain.FactoryID) {
	t.Helper()

	insert := `INSERT INTO trigger_state (factory_id, trigger, state, updated_at) VALUES ('` + string(factory) + `', 'nightly-audit', '{"since": "a"}', now())`

	if err := inOrgTx(t, pool, acme, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), insert); return err }); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		org  domain.OrgID
		want int64
	}{{labs, 0}, {acme, 1}} {
		err := inOrgTx(t, pool, c.org, func(tx pgx.Tx) error {
			tag, err := tx.Exec(t.Context(), `DELETE FROM trigger_state WHERE true`)
			if err == nil && tag.RowsAffected() != c.want {
				t.Errorf("goal_app of %s deleted %d trigger_state rows; want %d", c.org, tag.RowsAffected(), c.want)
			}

			return err
		})
		if err != nil {
			t.Errorf("goal_app of %s DELETE FROM trigger_state: %v", c.org, err)
		}
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

// seedWebhookValue is the value of the secret seedWebhook signs a webhook with.
const seedWebhookValue = "whsec-integration"

// seedWebhook creates the organization webhook name of org, signed by a new secret, open to a
// new factory, and returns it.
func seedWebhook(t *testing.T, d connectionsDB, org domain.OrgID, name string) domain.WebhookEndpoint {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), org)
	secret := domain.WebhookSecretName(name)

	if _, err := NewSecrets(d.store, testKey(t, "k1")).Create(ctx, domain.NewSecret{Name: secret, Value: seedWebhookValue}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	in := NewIntake(d.store)

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

// Migration 00115: another organization sees no webhook opening; goal_app cannot change a
// webhook's token settings, nor an opening's webhook or factory, and its keys refuse a secret or a
// factory the organization does not have (00117 grants the conversion UPDATE on sender, secret_id
// and factory_id); the database refuses
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

	if got, err := NewIntake(d.store).Endpoint(ctx, w.ID); err != nil || got.Secret != renamed || len(got.Openings) != 1 {
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

// Migration 00118: another organization sees no ledger id or daily count; goal_app may change a
// delivery's duplicates and a count's count only, deletes ledger ids but no count; the database
// refuses a signature header, a refused delivery with a body and a second row for one delivery id;
// without an organization the ledger's prune goes through its definer.
func TestIntake_ReceiverRLSAndGrants(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)
	w := seedWebhook(t, d, acme, "github-acme")
	in := NewIntake(d.store)
	ctx := domain.WithOrg(t.Context(), acme)
	old := time.Now().Add(-domain.LedgerRetention - 48*time.Hour)

	for _, n := range []domain.NewReceipt{
		{WebhookID: w.ID, ReceivedAt: time.Now(), Receipt: domain.ReceiptAccepted, DeliveryID: "d-1", Event: "push", Named: true},
		{WebhookID: w.ID, ReceivedAt: old, Receipt: domain.ReceiptAccepted, DeliveryID: "d-2", Event: "push"},
	} {
		if _, err := in.Receive(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	if err := inOrgTx(t, d.store.pool, labs, func(tx pgx.Tx) error {
		for _, table := range []string{"webhook_delivery_ids", "webhook_delivery_counts", "webhook_deliveries"} {
			var n int
			if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("labs sees %d rows of %s, %v; want none", n, table, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		`UPDATE webhook_deliveries SET duplicates = duplicates + 1`,
		`UPDATE webhook_delivery_counts SET count = count + 1`,
		`DELETE FROM webhook_delivery_ids WHERE delivery_id = 'nothing'`,
	} {
		if err := inOrgTx(t, d.store.pool, acme, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), stmt); return err }); err != nil {
			t.Errorf("goal_app could not run %s: %v", stmt, err)
		}
	}

	appRefuses(t, d, acme, []string{
		`UPDATE webhook_deliveries SET delivery_id = 'other'`,
		`UPDATE webhook_deliveries SET receipt = 'duplicate'`,
		`UPDATE webhook_deliveries SET event = 'other'`,
		`UPDATE webhook_delivery_ids SET delivery_id = 'other'`,
		`UPDATE webhook_delivery_counts SET day = current_date`,
		`DELETE FROM webhook_delivery_counts`,
		`INSERT INTO webhook_delivery_ids (webhook_id, delivery_id, received_at) VALUES ('` + string(w.ID) + `', '', now())`,
		`INSERT INTO webhook_deliveries (id, endpoint_id, received_at, outcome, receipt, delivery_id) VALUES (gen_random_uuid(), '` +
			string(w.ID) + `', now(), 'accepted', 'accepted', 'd-1')`,
		`INSERT INTO webhook_deliveries (id, endpoint_id, received_at, outcome, receipt, body) VALUES (gen_random_uuid(), '` +
			string(w.ID) + `', now(), 'refused', 'refused', 'x')`,
		`INSERT INTO webhook_deliveries (id, endpoint_id, received_at, outcome, receipt) VALUES (gen_random_uuid(), '` +
			string(w.ID) + `', now(), 'accepted', 'accepted')`,
		`INSERT INTO webhook_deliveries (id, endpoint_id, received_at, outcome, headers) VALUES (gen_random_uuid(), '` +
			string(w.ID) + `', now(), 'refused', '{"webhook-signature": "v1,AA=="}')`,
		`INSERT INTO webhook_deliveries (id, endpoint_id, received_at, outcome, headers) VALUES (gen_random_uuid(), '` +
			string(w.ID) + `', now(), 'refused', '{"authorization": "Bearer x"}')`,
	})

	if n, err := in.PruneDeliveries(t.Context(), time.Now()); err != nil || n != 2 {
		t.Errorf("PruneDeliveries = %d, %v; want d-2's ledger id and its day's count", n, err)
	}
}

// delivery_evaluations is scoped to its organization by row-level security, and goal_app may
// update it but not delete from it; it may delete trigger firings, for pruning (ADR 0040
// decision 13).
func TestIntake_DeliveryEvaluationsRLSAndGrants(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)
	admin := openPool(t, db.admin)

	var rls bool
	if err := admin.QueryRow(t.Context(), `SELECT relrowsecurity FROM pg_class WHERE oid = (current_schema() || '.' || $1)::regclass`, tableEvaluations).Scan(&rls); err != nil || !rls {
		t.Errorf("delivery_evaluations row-level security = %v, %v; want on", rls, err)
	}

	checkPolicy(t, admin, tableEvaluations)

	for _, g := range []struct {
		table, priv string
		want        bool
	}{
		{tableEvaluations, "select", true},
		{tableEvaluations, "insert", true},
		{tableEvaluations, "update", true},
		{tableEvaluations, "delete", false},
		{"trigger_firings", "delete", true},
	} {
		var has bool
		if err := admin.QueryRow(t.Context(), "SELECT has_table_privilege('goal_app', current_schema() || '.' || $1, $2)", g.table, g.priv).
			Scan(&has); err != nil || has != g.want {
			t.Errorf("goal_app %s on %s = %v, %v; want %v", g.priv, g.table, has, err, g.want)
		}
	}
}

// tableEvaluations is the table of ADR 0040 decision 13.
const tableEvaluations = "delivery_evaluations"
