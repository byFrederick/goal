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
// is listed after it unchanged, with no server key, and a row with both keys, or with part of the
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

	migrate(t, db.owner)

	got, err := NewStandingRules(store).StandingRules(domain.WithOrg(t.Context(), org), false)
	if err != nil || len(got) != 1 {
		t.Fatalf("StandingRules after 00091 = %+v, %v; want the old rule", got, err)
	}

	if r := got[0]; r.ID != domain.StandingRuleID(rule.String()) || r.ConnectionID != domain.ConnectionID(conn.String()) ||
		r.Factory != "" || r.Environment != "" || r.Server != "" || !r.Active() {
		t.Errorf("the old rule after 00091 = %+v; want it keyed by its connection, active", r)
	}

	factory := uuid.Must(uuid.NewV7())

	for name, cols := range map[string][]any{
		"both keys":                      {conn, factory, "default", "acme-git"},
		"a server key without a factory": {nil, nil, "default", "acme-git"},
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
