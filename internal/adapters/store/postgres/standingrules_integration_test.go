//go:build integration

package postgres

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest/standingruletest"
)

// Migration 00091 keys standing rules by server too: a rule keyed by its connection before it
// keeps that key after it, with no server key, and a row with both keys, or with part of the
// server key, is refused.
func TestMigrate_StandingRulesServerKey(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrateTo(t, openPool(t, db.owner), 89)

	store := New(openPool(t, db.app))
	org := newOrgID(t, store)
	admin := openPool(t, db.admin)

	conn, rule, item := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())

	_, err := admin.Exec(t.Context(), `INSERT INTO standing_rules (id, org_id, connection_id, tool, agent, created_by, item_id)
		VALUES ($1, $2, $3, 'create_comment', 'architect', 'user:local', $4)`, rule, string(org), conn, item)
	if err != nil {
		t.Fatal(err)
	}

	migrateTo(t, openPool(t, db.owner), 91)

	// 00133 drops connection_id, so the row is read as 00091 left it.
	var connID, factoryID, environment, server *string
	if err := admin.QueryRow(t.Context(), `SELECT connection_id::text, factory_id::text, environment, server
		FROM standing_rules WHERE id = $1 AND revoked_at IS NULL`, rule).Scan(&connID, &factoryID, &environment, &server); err != nil {
		t.Fatalf("the old rule after 00091: %v; want it, active", err)
	}

	if connID == nil || *connID != conn.String() || factoryID != nil || environment != nil || server != nil {
		t.Errorf("the old rule after 00091 = connection %v, factory %v, environment %v, server %v; want it keyed by its connection",
			connID, factoryID, environment, server)
	}

	factory := uuid.Must(uuid.NewV7())

	for name, cols := range map[string][]any{
		"both keys":                      {conn, factory, "default", acmeGit},
		"a server key without a factory": {nil, nil, "default", acmeGit},
		"no key":                         {nil, nil, nil, nil},
	} {
		_, err := admin.Exec(t.Context(), `INSERT INTO standing_rules
			(id, org_id, connection_id, factory_id, environment, server, tool, agent, created_by, item_id)
			VALUES ($1, $2, $3, $4, $5, $6, 'get_issue', 'architect', 'user:local', $7)`,
			uuid.Must(uuid.NewV7()), string(org), cols[0], cols[1], cols[2], cols[3], item)

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.ConstraintName != "standing_rules_key_check" {
			t.Errorf("a row with %s: %v; want standing_rules_key_check to refuse it", name, err)
		}
	}
}

func TestStandingRules_Conformance(t *testing.T) {
	t.Parallel()

	standingruletest.Run(t, func(t *testing.T) standingruletest.Harness {
		t.Helper()

		db := newTestDB(t, OwnerRole)
		migrate(t, db.owner)
		store := New(openPool(t, db.app))

		return standingruletest.Harness{
			StandingRules: NewStandingRules(store),
			Audit:         store,
			NewOrg:        func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, store) },
		}
	})
}
