//go:build integration

package postgres

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// notBuiltAt is when the seeded requests started and ended, which 00144 must keep for the ended.
var notBuiltAt = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

// notBuiltSeeds are a request in each status, inserted as 00143 left the table.
var notBuiltSeeds = []struct{ status, insert string }{
	{"queued", `INSERT INTO draft_requests (id, org_id, factory_id, requester, text, status)
		VALUES ($1, $2, $3, 'user:local', 'give review a 4th reviewer', 'queued')`},
	{"running", `INSERT INTO draft_requests (id, org_id, factory_id, requester, text, status, started_at, lease_holder, lease_expires_at)
		VALUES ($1, $2, $3, 'user:local', 'fix the writer', 'running', $4, gen_random_uuid(), $4)`},
	{"done", `INSERT INTO draft_requests (id, org_id, factory_id, requester, text, status, started_at, finished_at)
		VALUES ($1, $2, $3, 'user:local', 'add a station', 'done', $4, $4)`},
	{"failed", `INSERT INTO draft_requests (id, org_id, factory_id, requester, text, status, started_at, finished_at, error)
		VALUES ($1, $2, $3, 'user:local', 'rename it', 'failed', $4, $4, 'the draft changed')`},
}

// TestMigrate_FailsOpenDraftRequests checks 00144: every queued or running draft request, in
// every organization, ends failed with its lease cleared and the not-built reason, and a done or
// failed one keeps its result.
func TestMigrate_FailsOpenDraftRequests(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 143)

	store := New(openPool(t, db.app))
	seeded := map[uuid.UUID]string{}

	for _, org := range []string{string(newOrgID(t, store)), string(newOrgID(t, store))} {
		maps.Copy(seeded, seedDraftRequests(t, owner, org))
	}

	migrateTo(t, owner, 144)

	for id, was := range seeded {
		wantAfterNotBuilt(t, owner, id, was)
	}
}

// wantAfterNotBuilt checks request id, which was in status was before 00144: an open one is now
// failed as not built yet with no lease, an ended one is untouched.
func wantAfterNotBuilt(t *testing.T, owner *pgxpool.Pool, id uuid.UUID, was string) {
	t.Helper()

	var (
		status, errText string
		finished        *time.Time
		leased          bool
	)

	if err := owner.QueryRow(t.Context(), `SELECT status, error, finished_at, lease_holder IS NOT NULL OR lease_expires_at IS NOT NULL
		FROM draft_requests WHERE id = $1`, id).Scan(&status, &errText, &finished, &leased); err != nil {
		t.Fatal(err)
	}

	open := was == "queued" || was == "running"
	failed := status == "failed" && strings.Contains(errText, "not built yet") && finished != nil && !leased
	untouched := status == was && finished != nil && finished.Equal(notBuiltAt) && !strings.Contains(errText, "not built yet")

	if open && !failed || !open && !untouched {
		t.Errorf("%s request after 00144 = %s %q finished %v leased %v; want an open one failed as not built yet, an ended one untouched",
			was, status, errText, finished, leased)
	}
}

// seedDraftRequests inserts one request of each status in org, each on a factory of its own
// because a factory holds one open request, and returns their statuses by id.
func seedDraftRequests(t *testing.T, owner *pgxpool.Pool, org string) map[uuid.UUID]string {
	t.Helper()

	seeded := map[uuid.UUID]string{}

	for _, r := range notBuiltSeeds {
		id, factory := uuid.New(), uuid.New()
		if _, err := owner.Exec(t.Context(), `INSERT INTO factories (id, org_id, name, definition, definition_sha256)
			VALUES ($1, $2, $3, '{}', 'sha')`, factory, org, "acme-"+r.status); err != nil {
			t.Fatal(err)
		}

		args := []any{id, org, factory}
		if r.status != "queued" {
			args = append(args, notBuiltAt)
		}

		if _, err := owner.Exec(t.Context(), r.insert, args...); err != nil {
			t.Fatalf("insert %s request: %v", r.status, err)
		}

		seeded[id] = r.status
	}

	return seeded
}
