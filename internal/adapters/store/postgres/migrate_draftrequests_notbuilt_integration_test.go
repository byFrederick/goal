//go:build integration

package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMigrate_FailsOpenDraftRequests checks 00144: every queued or running draft request, in
// every organization, ends failed with its lease cleared and the not-built reason, and a done or
// failed one keeps its result.
func TestMigrate_FailsOpenDraftRequests(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 143)

	store := New(openPool(t, db.app))
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	want := map[uuid.UUID]string{}

	for _, org := range []string{string(newOrgID(t, store)), string(newOrgID(t, store))} {
		for _, r := range []struct{ status, insert, wantError string }{
			{"queued", `INSERT INTO draft_requests (id, org_id, factory_id, requester, text, status)
				VALUES ($1, $2, $3, 'user:local', 'give review a 4th reviewer', 'queued')`, "not built yet"},
			{"running", `INSERT INTO draft_requests (id, org_id, factory_id, requester, text, status, started_at, lease_holder, lease_expires_at)
				VALUES ($1, $2, $3, 'user:local', 'fix the writer', 'running', $4, gen_random_uuid(), $4)`, "not built yet"},
			{"done", `INSERT INTO draft_requests (id, org_id, factory_id, requester, text, status, started_at, finished_at)
				VALUES ($1, $2, $3, 'user:local', 'add a station', 'done', $4, $4)`, ""},
			{"failed", `INSERT INTO draft_requests (id, org_id, factory_id, requester, text, status, started_at, finished_at, error)
				VALUES ($1, $2, $3, 'user:local', 'rename it', 'failed', $4, $4, 'the draft changed')`, "the draft changed"},
		} {
			// A factory holds one open request, so each request has its own.
			id, factory := uuid.New(), uuid.New()
			if _, err := owner.Exec(t.Context(), `INSERT INTO factories (id, org_id, name, definition, definition_sha256)
				VALUES ($1, $2, $3, '{}', 'sha')`, factory, org, "acme-"+r.status); err != nil {
				t.Fatal(err)
			}

			args := []any{id, org, factory}
			if r.status != "queued" {
				args = append(args, at)
			}

			if _, err := owner.Exec(t.Context(), r.insert, args...); err != nil {
				t.Fatalf("insert %s request: %v", r.status, err)
			}

			want[id] = r.status + "|" + r.wantError
		}
	}

	migrateTo(t, owner, 144)

	for id, w := range want {
		wantStatus, wantError, _ := strings.Cut(w, "|")

		var (
			status, errText string
			finished        *time.Time
			leased          bool
		)

		if err := owner.QueryRow(t.Context(), `SELECT status, error, finished_at, lease_holder IS NOT NULL OR lease_expires_at IS NOT NULL
			FROM draft_requests WHERE id = $1`, id).Scan(&status, &errText, &finished, &leased); err != nil {
			t.Fatal(err)
		}

		if wantStatus == "queued" || wantStatus == "running" {
			if status != "failed" || !strings.Contains(errText, wantError) || finished == nil || leased {
				t.Errorf("%s request after 00144 = %s %q finished %v leased %v, want failed, not built yet, finished, no lease",
					wantStatus, status, errText, finished, leased)
			}

			continue
		}

		if status != wantStatus || errText != wantError || finished == nil || !finished.Equal(at) {
			t.Errorf("%s request after 00144 = %s %q finished %v, want it untouched", wantStatus, status, errText, finished)
		}
	}
}
