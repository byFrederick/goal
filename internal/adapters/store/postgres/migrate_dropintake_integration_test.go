//go:build integration

package postgres

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/core/domain"
)

// intakeReleased is the release the refusal of 00138 names.
const intakeReleased = "run ADR 0040's release first, then this one"

// endpointKeyID is the master key id the seeded endpoints' own sealed secrets are under.
const endpointKeyID = "endpoint-k0"

// seedIntake is a database at 00135 with an organization webhook signed by a secret sealed under
// key id k1, open to a factory.
type seedIntake struct {
	seedConnections

	webhook domain.WebhookEndpoint
}

func newSeedIntake(t *testing.T) seedIntake {
	t.Helper()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 135)

	store := New(openPool(t, db.app))
	s := seedIntake{seedConnections: seedConnections{db: db, owner: owner, store: store, org: newOrgID(t, store)}}
	s.webhook = seedWebhook(t, connectionsDB{db: db, store: store, owner: New(owner), admin: openPool(t, db.admin)}, s.org, "github-acme")

	if err := owner.QueryRow(t.Context(), `SELECT factory_id FROM webhook_openings WHERE webhook_id = $1`, string(s.webhook.ID)).Scan(&s.factory); err != nil {
		t.Fatal(err)
	}

	return s
}

// factoryEndpoint inserts a webhook endpoint of the factory, as 00071 made them, signed by its
// own sealed secret under endpointKeyID.
func (s seedIntake) factoryEndpoint(t *testing.T) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	s.exec(t, `INSERT INTO webhook_endpoints (id, org_id, factory_id, name, kind, key_id, nonce, ciphertext)
		VALUES ($1, $2, $3, 'github', 'github', $4, $5, 'sealed')`, id, string(s.org), s.factory, endpointKeyID, bytes.Repeat([]byte{1}, 12))

	return id
}

// droppedColumns are the columns of work_items and webhook_endpoints 00138 drops that are still
// there.
func (s seedIntake) droppedColumns(t *testing.T) []string {
	t.Helper()

	rows, err := s.owner.Query(t.Context(), `SELECT table_name || '.' || column_name FROM information_schema.columns
		WHERE (table_name = 'work_items' AND column_name = 'source')
		   OR (table_name = 'webhook_endpoints' AND column_name IN ('key_id', 'nonce', 'ciphertext', 'kind', 'factory_id'))
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}

	columns, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}

	return columns
}

// Migration 00138 drops work_items.source and the factory endpoints' columns on a database that
// ran ADR 0040's release: the organization's webhook still reads, goal_connection_key_ids lists
// only the secrets' key ids, and a webhook's name is still unique in its organization and none of
// the reserved names.
func TestMigrate_DropsOldIntakeColumns(t *testing.T) {
	t.Parallel()

	s := newSeedIntake(t)
	// A webhook converted from a factory endpoint may still hold the sealed value it was made with.
	s.exec(t, `UPDATE webhook_endpoints SET key_id = $2, nonce = $3, ciphertext = 'sealed' WHERE id = $1`,
		string(s.webhook.ID), endpointKeyID, bytes.Repeat([]byte{1}, 12))

	if ids, err := ConnectionKeyIDs(t.Context(), s.store); err != nil || !slices.Equal(ids, []string{endpointKeyID, "k1"}) {
		t.Fatalf("ConnectionKeyIDs at 00135 = %v, %v; want the webhook's %s and the secret's k1", ids, err, endpointKeyID)
	}

	migrate(t, s.db.owner)

	if columns := s.droppedColumns(t); len(columns) != 0 {
		t.Errorf("columns after 00138 = %v; want none of them", columns)
	}

	var nullable int
	if err := s.owner.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'webhook_endpoints' AND column_name IN ('sender', 'secret_id') AND is_nullable = 'YES'`).Scan(&nullable); err != nil || nullable != 0 {
		t.Errorf("%d nullable sender and secret_id columns after 00138, %v; want none", nullable, err)
	}

	ctx := domain.WithOrg(t.Context(), s.org)
	if got, err := NewIntake(s.store).Endpoint(ctx, s.webhook.ID); err != nil || got.Name != "github-acme" || got.Sender != domain.SenderGitHub {
		t.Errorf("Endpoint after 00138 = %+v, %v; want the github-acme webhook", got, err)
	}

	if ids, err := ConnectionKeyIDs(t.Context(), s.store); err != nil || !slices.Equal(ids, []string{"k1"}) {
		t.Errorf("ConnectionKeyIDs after 00138 = %v, %v; want only the secret's k1", ids, err)
	}

	for _, name := range []string{"github-acme", "schedule"} {
		if _, err := s.owner.Exec(t.Context(), `INSERT INTO webhook_endpoints (id, org_id, name, sender, secret_id)
			SELECT $1, org_id, $2, sender, secret_id FROM webhook_endpoints WHERE id = $3`,
			uuid.Must(uuid.NewV7()), name, string(s.webhook.ID)); err == nil {
			t.Errorf("a second webhook named %s after 00138 was accepted; want it refused", name)
		}
	}
}

// Migration 00138 refuses a database ADR 0040's release has not run on, before it changes
// anything: it names the release to run first, and the columns are still there.
func TestMigrate_DropsOldIntakeColumnsRefusesUnreleased(t *testing.T) {
	t.Parallel()

	for name, seed := range map[string]func(*testing.T, seedIntake){
		"a factory endpoint with no secret": func(t *testing.T, s seedIntake) {
			t.Helper()
			s.factoryEndpoint(t)
		},
		"a converted endpoint that kept its factory": func(t *testing.T, s seedIntake) {
			t.Helper()
			id := s.factoryEndpoint(t)
			s.exec(t, `UPDATE webhook_endpoints SET sender = 'github', secret_id = $2, kind = NULL WHERE id = $1`,
				id, string(s.webhook.SecretID))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newSeedIntake(t)
			seed(t, s)

			err := Migrate(t.Context(), s.owner)
			if err == nil || !strings.Contains(err.Error(), intakeReleased) {
				t.Fatalf("Migrate = %v; want a refusal that says to %s", err, intakeReleased)
			}

			if columns := s.droppedColumns(t); len(columns) != 6 {
				t.Errorf("columns after a refused 00138 = %v; want all six kept", columns)
			}
		})
	}
}
