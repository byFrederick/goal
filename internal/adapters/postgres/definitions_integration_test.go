//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
	"github.com/byFrederick/goal/internal/ports/porttest/definitionstest"
)

// The suite runs on PostgreSQL with the in-memory bucket: the bucket's own rules (PutIfAbsent
// writes once) are the s3store integration tests'.
func TestDefinitions_Conformance(t *testing.T) {
	t.Parallel()

	definitionstest.Run(t, func(t *testing.T) definitionstest.Harness {
		t.Helper()

		pool := newTestPool(t)
		s := New(pool)
		objects := porttest.NewObjectStore()

		return definitionstest.Harness{
			Definitions: NewDefinitions(pool, objects, nil),
			Notices:     NewDefinitionNotices(pool.Config().ConnConfig, nil),
			Objects:     objects,
			Items:       s,
			Audit:       s,
			NewOrg:      func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, s) },
			LegacyFactory: func(t *testing.T, org domain.OrgID, id domain.FactoryID, name string) {
				t.Helper()

				legacyFactory(t, pool, org, id, name)
			},
		}
	})
}

// definitionsFixture is a definition store on a fresh schema with one published factory in org.
type definitionsFixture struct {
	pool    *pgxpool.Pool
	s       *Store
	defs    *Definitions
	org     domain.OrgID
	factory ports.FactoryRecord
	v1      domain.DefinitionVersion
	draft   domain.DefinitionVersion
}

func newDefinitionsFixture(t *testing.T) definitionsFixture {
	t.Helper()

	pool := newTestPool(t)
	s := New(pool)
	defs := NewDefinitions(pool, porttest.NewObjectStore(), nil)
	org := newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), org)

	m, err := defs.PutBlobs(ctx, map[string][]byte{"factory.yaml": []byte("metadata:\n  name: acme\n")})
	if err != nil {
		t.Fatal(err)
	}

	rec, d, err := defs.CreateFactory(ctx, ports.NewFactory{
		ID: domain.FactoryID(newUUID(t)), Name: "acme", Manifest: m, Source: domain.VersionSource{Kind: domain.SourceBlank}, By: domain.LocalUser,
	})
	if err != nil {
		t.Fatal(err)
	}

	v1, err := defs.Publish(ctx, ports.Publish{Factory: rec.ID, DraftRevision: d.Revision, Name: "acme", Start: true, By: domain.LocalUser})
	if err != nil {
		t.Fatal(err)
	}

	draft, _, err := defs.OpenDraft(ctx, ports.OpenDraft{Factory: rec.ID, By: domain.LocalUser})
	if err != nil {
		t.Fatal(err)
	}

	return definitionsFixture{pool: pool, s: s, defs: defs, org: org, factory: rec, v1: v1, draft: draft}
}

func newUUID(t *testing.T) string {
	t.Helper()

	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}

	return id.String()
}

// Raw SQL as goal_app: without an organization the definition tables refuse every read; bound
// to another organization they show and change none of these rows and accept none planted in
// the first.
func TestRLS_DefinitionTables(t *testing.T) {
	t.Parallel()

	f := newDefinitionsFixture(t)
	other := newOrgID(t, f.s)

	for _, table := range []string{"definition_versions", "definition_blobs", "factory_records"} {
		var n int
		if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err == nil {
			t.Errorf("SELECT from %s without an org = %d rows; want an error", table, n)
		}

		if err := asOrg(t, f.pool, other, "SELECT count(*) FROM "+table, &n); err != nil || n != 0 {
			t.Errorf("another org sees %d rows of %s, %v; want none", n, table, err)
		}

		if err := asOrg(t, f.pool, f.org, "SELECT count(*) FROM "+table, &n); err != nil || n == 0 {
			t.Errorf("the org sees %d rows of %s, %v; want its own", n, table, err)
		}
	}

	var updated int64

	err := inOrgTx(t, f.pool, other, func(tx pgx.Tx) error {
		tag, err := tx.Exec(t.Context(), "UPDATE definition_versions SET message = 'taken' WHERE id = $1", string(f.draft.ID))
		updated = tag.RowsAffected()

		return err
	})
	if err != nil || updated != 0 {
		t.Errorf("another org updated %d of the draft rows, %v; want none", updated, err)
	}

	err = inOrgTx(t, f.pool, other, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO definition_blobs (org_id, sha256, size) VALUES ($1, $2, 1)`,
			string(f.org), string(domain.HashBlob([]byte("planted"))))

		return err
	})
	if err == nil {
		t.Error("another org wrote a blob row into the org; want the policy to refuse it")
	}
}

// The schema keeps the version rules even against raw SQL: a published version is immutable and
// never deleted, the pointer names only a published version of its own factory, and a run can
// only pin a version of its own factory.
func TestSchema_DefinitionRules(t *testing.T) {
	t.Parallel()

	f := newDefinitionsFixture(t)

	for _, tc := range []struct {
		what string
		sql  string
		args []any
	}{
		{"edit a published version", "UPDATE definition_versions SET manifest = '{}' WHERE id = $1", []any{string(f.v1.ID)}},
		{"delete a published version", "DELETE FROM definition_versions WHERE id = $1", []any{string(f.v1.ID)}},
		{"point at a draft", "UPDATE factories SET current_version_id = $1 WHERE id = $2", []any{string(f.draft.ID), string(f.factory.ID)}},
		{"open a second draft", `INSERT INTO definition_versions (id, factory_id, state, manifest, created_by, updated_by)
			VALUES (uuidv7(), $1, 'draft', '{}', 'user:local', 'user:local')`, []any{string(f.factory.ID)}},
		{"publish without a number", "UPDATE definition_versions SET state = 'published' WHERE id = $1", []any{string(f.draft.ID)}},
	} {
		err := inOrgTx(t, f.pool, f.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), tc.sql, tc.args...)

			return err
		})
		if err == nil {
			t.Errorf("goal_app could %s; want the schema to refuse it", tc.what)
		}
	}

	other, _, err := f.defs.CreateFactory(domain.WithOrg(t.Context(), f.org), ports.NewFactory{
		ID: domain.FactoryID(newUUID(t)), Name: "zeta", Source: domain.VersionSource{Kind: domain.SourceBlank}, By: domain.LocalUser,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = inOrgTx(t, f.pool, f.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE factories SET current_version_id = $1 WHERE id = $2", string(f.v1.ID), string(other.ID))

		return err
	})
	if err == nil {
		t.Error("goal_app pointed a factory at another factory's version; want the foreign key to refuse it")
	}

	if got, err := f.defs.Version(domain.WithOrg(t.Context(), f.org), f.v1.ID); err != nil || got.State != domain.VersionPublished {
		t.Errorf("v1 after the refusals = %+v, %v; want it published as it was", got, err)
	}
}

// Purging a deleted organization removes its factories with their published versions and its
// blobs, which nothing else may delete.
func TestPurge_RemovesDefinitions(t *testing.T) {
	t.Parallel()

	f := newDefinitionsFixture(t)

	if err := f.s.DeleteOrg(domain.WithOrg(t.Context(), f.org), domain.LocalUser, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	if purged, err := f.s.PurgeOrg(t.Context(), f.org, time.Now(), nil); err != nil || !purged {
		t.Fatalf("PurgeOrg = %v, %v; want %s purged", purged, err, f.org)
	}

	for _, table := range []string{"definition_versions", "definition_blobs", "factories"} {
		var n int
		if err := asOrg(t, f.pool, f.org, "SELECT count(*) FROM "+table, &n); err != nil || n != 0 {
			t.Errorf("%d rows of %s left after the purge, %v; want none", n, table, err)
		}
	}
}

// A run records the definition version it starts on, and only a version of its own factory.
func TestStartRun_RecordsTheDefinitionVersion(t *testing.T) {
	t.Parallel()

	f := newDefinitionsFixture(t)
	ctx := domain.WithOrg(t.Context(), f.org)

	newItem := func(factory domain.FactoryID) domain.Item {
		t.Helper()

		it, err := f.s.CreateItem(ctx, domain.NewItem{
			FactoryID: factory, Type: "task", Title: "x", Station: "work", State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}

		return it
	}

	it := newItem(f.factory.ID)
	run := ports.NewRun{
		FactoryID: f.factory.ID, DefinitionVersionID: f.v1.ID, ItemID: it.ID, Station: "work", Agent: "worker", StartedAt: time.Now(),
	}

	if _, err := f.s.StartRun(ctx, run); err != nil {
		t.Fatal(err)
	}

	if runs, err := f.s.Runs(ctx, it.ID, 10); err != nil || len(runs) != 1 || runs[0].DefinitionVersionID != f.v1.ID {
		t.Fatalf("runs = %+v, want one on %s", runs, f.v1.ID)
	}

	other := saveFactory(ctx, t, f.s)
	run.FactoryID, run.ItemID = other, newItem(other).ID

	if _, err := f.s.StartRun(ctx, run); err == nil {
		t.Error("StartRun of another factory on this factory's version succeeded")
	}
}
