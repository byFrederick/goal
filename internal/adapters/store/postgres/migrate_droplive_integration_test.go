//go:build integration

package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestMigrate_DropsLiveKind checks 00142: the kind CHECK refuses 'live', the live check is gone,
// and the next-sync index no longer filters on kind (ADR 0038 decision 16).
func TestMigrate_DropsLiveKind(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 142)

	org := newOrgID(t, New(openPool(t, db.app)))

	_, err := owner.Exec(t.Context(), `INSERT INTO knowledge_sources (id, org_id, name, title, kind, next_sync_at, created_by)
		VALUES ($1, $2, 'tickets', 'Tickets', 'live', NULL, 'user:local')`, uuid.Must(uuid.NewV7()), string(org))

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "knowledge_sources_kind_check" {
		t.Fatalf("insert of a live source: err = %v, want a violation of knowledge_sources_kind_check", err)
	}

	var liveCheck bool
	if err := owner.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_constraint
		WHERE conrelid = 'knowledge_sources'::regclass AND conname = 'knowledge_sources_live_check')`).Scan(&liveCheck); err != nil {
		t.Fatal(err)
	}

	if liveCheck {
		t.Error("knowledge_sources_live_check still exists")
	}

	var predicate string
	if err := owner.QueryRow(t.Context(), `SELECT coalesce(pg_get_expr(indpred, indrelid), '') FROM pg_index
		WHERE indexrelid = 'knowledge_sources_next_sync_idx'::regclass`).Scan(&predicate); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(predicate, "kind") || !strings.Contains(predicate, "next_sync_at IS NOT NULL") {
		t.Errorf("knowledge_sources_next_sync_idx predicate = %q, want next_sync_at IS NOT NULL without kind", predicate)
	}
}
