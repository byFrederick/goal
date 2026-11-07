//go:build integration

package postgres

import (
	"crypto/sha256"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/masterkey/masterkey"
	"github.com/byFrederick/goal/internal/core/ports"
)

// testKey is master keys ids with secrets derived from each id, so an id is the same key in every
// call.
func testKey(t *testing.T, ids ...string) ports.MasterKey {
	t.Helper()

	keys := make([]masterkey.Key, 0, len(ids))
	for _, id := range ids {
		sum := sha256.Sum256([]byte("goal-test-key/" + id))
		keys = append(keys, masterkey.Key{ID: id, Secret: sum[:]})
	}

	k, err := masterkey.New(keys...)
	if err != nil {
		t.Fatal(err)
	}

	return k
}

// connectionsDB is a migrated database with goald's store (goal_app) and goal db rekey's
// (goal_owner), for the stores that seal under the master keys and the tests that share them.
type connectionsDB struct {
	db    testDB
	store *Store
	owner *Store
	admin *pgxpool.Pool
}

func newConnectionsDB(t *testing.T) connectionsDB {
	t.Helper()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	return connectionsDB{db: db, store: New(openPool(t, db.app)), owner: New(openPool(t, db.owner)), admin: openPool(t, db.admin)}
}
