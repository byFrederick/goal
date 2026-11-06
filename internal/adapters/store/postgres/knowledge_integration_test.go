//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/knowledgetest"
)

func TestKnowledge_Conformance(t *testing.T) {
	t.Parallel()

	knowledgetest.Run(t, func(t *testing.T) knowledgetest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return knowledgetest.Harness{
			Knowledge: NewKnowledge(d.store),
			Secrets:   NewSecrets(d.store, testKey(t, "k1")), Audit: d.store,
			NewOrg: func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
			Pin: func(t *testing.T, org domain.OrgID, snap domain.KnowledgeSnapshotID) {
				t.Helper()
				pinSnapshot(t, d, org, snap)
			},
		}
	})
}

// pinSnapshot gives org a running agent run whose knowledge_pinned event names snap, as the
// bootstrap superuser with key checks off: the run needs no factory or item for retention.
func pinSnapshot(t *testing.T, d connectionsDB, org domain.OrgID, snap domain.KnowledgeSnapshotID) {
	t.Helper()

	data, err := json.Marshal(map[string]any{"snapshots": []map[string]string{{"source": "pinned", "snapshot": string(snap)}}})
	if err != nil {
		t.Fatal(err)
	}

	err = pgx.BeginFunc(t.Context(), d.admin, func(tx pgx.Tx) error {
		for _, stmt := range []string{
			"SET LOCAL session_replication_role = replica",
			`WITH r AS (
			   INSERT INTO agent_runs (org_id, factory_id, item_id, station, agent, model, status)
			   VALUES ($1::uuid, uuidv7(), gen_random_uuid(), 'work', 'worker', 'model-a', 'running')
			   RETURNING id, factory_id, item_id)
			 INSERT INTO item_events (org_id, factory_id, item_id, run_id, kind, station, actor, data)
			 SELECT $1::uuid, r.factory_id, r.item_id, r.id, 'knowledge_pinned', 'work', 'worker', $2::jsonb FROM r`,
		} {
			args := []any{string(org), data}
			if !strings.Contains(stmt, "$1") {
				args = nil
			}

			if _, err := tx.Exec(t.Context(), stmt, args...); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("pin snapshot %s: %v", snap, err)
	}
}

// knowledgeDoc gives org an upload source "runbooks" with one file whose name is text.
func knowledgeDoc(t *testing.T, d connectionsDB, org domain.OrgID, text string) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), org)
	k := NewKnowledge(d.store)

	if _, err := k.CreateSource(ctx, domain.NewKnowledgeSource{Name: "runbooks", Title: "Runbooks", Kind: domain.SourceUpload}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if _, err := k.AddUpload(ctx, "runbooks", domain.KnowledgeUpload{Name: text, MIME: "text/markdown", Bytes: 3, Blob: domain.HashBlob([]byte(text))}, domain.LocalUser, nil); err != nil {
		t.Fatal(err)
	}
}

func syncingKind(t *testing.T, d connectionsDB) {
	t.Helper()

	if err := NewConnections(d.store, testKey(t, "k1")).SyncPlatformKinds(t.Context(), []domain.ConnectionKind{{
		Name: "kb-docs", Title: "Docs", Platform: true, Spec: domain.KindSpec{Sync: "gitdocs"},
	}}); err != nil {
		t.Fatal(err)
	}
}

// Spec 4 §11.2 row 1: as goal_app, organization B never reads A's chunks; nor does it see A's
// sources or documents, or write into them.
func TestKnowledge_RLSAcrossOrgs(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	syncingKind(t, d)

	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)
	knowledgeDoc(t, d, acme, "acme's secret runbook")
	knowledgeDoc(t, d, labs, "labs' own runbook")

	if err := inOrgTx(t, d.store.pool, labs, func(tx pgx.Tx) error { return seesOnlyItsKnowledge(t, tx, acme) }); err != nil {
		t.Fatal(err)
	}

	// Nor writes into them: a source claiming acme's organization is refused by the policy.
	err := inOrgTx(t, d.store.pool, labs, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO knowledge_sources (id, org_id, name, title, kind, next_sync_at, created_by)
			VALUES (gen_random_uuid(), '`+string(acme)+`', 'forged', 'x', 'upload', now(), 'user:x')`)

		return err
	})
	if err == nil {
		t.Error("labs wrote a source into acme's knowledge base")
	}
}

// seesOnlyItsKnowledge checks, in a transaction of another organization than acme, that its
// uploads are that organization's only, and that no row of acme's knowledge base is visible.
func seesOnlyItsKnowledge(t *testing.T, tx pgx.Tx, acme domain.OrgID) error {
	t.Helper()

	rows, err := tx.Query(t.Context(), `SELECT name FROM knowledge_uploads`)
	if err != nil {
		return err
	}

	texts, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}

	if len(texts) != 1 || texts[0] != "labs' own runbook" {
		t.Errorf("labs' uploads = %q, want its own only", texts)
	}

	for _, table := range []string{"knowledge_sources", "knowledge_uploads"} {
		var n int
		if err := tx.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE org_id = $1", string(acme)).Scan(&n); err != nil || n != 0 {
			t.Errorf("labs sees %d rows of acme's %s, %v", n, table, err)
		}
	}

	return nil
}

// Review 4 SQL L1: a deletion holds the content it drops until it commits, so an upload of the
// same content waits for it and then writes its object again, instead of committing between the
// deletion's check and its drop and losing the object.
func TestKnowledge_AnUploadWaitsForTheDropOfItsContent(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	k := NewKnowledge(d.store)
	ctx := domain.WithOrg(t.Context(), newOrgID(t, d.store))
	h := domain.HashBlob([]byte("# Policy"))

	for _, name := range []string{"policies", "archive"} {
		if _, err := k.CreateSource(ctx, domain.NewKnowledgeSource{Name: name, Title: name, Kind: domain.SourceUpload}, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := k.AddUpload(ctx, "policies", domain.KnowledgeUpload{Name: "policy.md", MIME: domain.MediaMarkdown, Bytes: 8, Blob: h}, domain.LocalUser, nil); err != nil {
		t.Fatal(err)
	}

	dropping, added := make(chan struct{}), make(chan error, 1)

	go func() {
		<-dropping

		_, err := k.AddUpload(ctx, "archive", domain.KnowledgeUpload{Name: "copy.md", MIME: domain.MediaMarkdown, Bytes: 8, Blob: h}, domain.LocalUser, nil)
		added <- err
	}()

	early := false

	err := k.DeleteUpload(ctx, "policies", "policy.md", domain.LocalUser, func(context.Context, []domain.BlobHash) error {
		close(dropping)

		select {
		case err := <-added:
			early = true

			t.Errorf("the upload committed while the drop ran (%v)", err)
		case <-time.After(300 * time.Millisecond):
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if !early {
		if err := <-added; err != nil {
			t.Fatalf("the upload after the drop: %v", err)
		}
	}
}

// A claim reaches every organization but a deleted one, whose sources wait for its purge.
func TestKnowledge_ClaimSkipsDeletedOrganizations(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	k := NewKnowledge(d.store)
	gone, live := newOrgID(t, d.store), newOrgID(t, d.store)

	for _, org := range []domain.OrgID{gone, live} {
		if _, err := k.CreateSource(domain.WithOrg(t.Context(), org),
			domain.NewKnowledgeSource{Name: "archive", Title: "Archive", Kind: domain.SourceUpload}, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	if err := d.store.DeleteOrg(domain.WithOrg(t.Context(), gone), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Add(time.Minute)

	c, err := k.ClaimSource(t.Context(), now, now.Add(time.Minute))
	if err != nil || c == nil || c.Org != live {
		t.Fatalf("claim = %+v, %v; want the live organization's archive", c, err)
	}

	if c, err := k.ClaimSource(t.Context(), now, now.Add(time.Minute)); err != nil || c != nil {
		t.Errorf("second claim = %+v, %v; want none (the deleted organization's is left)", c, err)
	}
}

// An organization's purge removes its knowledge base with its connections, whichever the cascade
// reaches first (the sources' foreign key to connections is checked at the statement's end).
func TestPurgeOrg_RemovesKnowledge(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	syncingKind(t, d)

	deleted := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	org := newOrgID(t, d.store)
	knowledgeDoc(t, d, org, "a runbook")

	if err := d.store.DeleteOrg(domain.WithOrg(t.Context(), org), domain.LocalUser, deleted); err != nil {
		t.Fatal(err)
	}

	if ok, err := d.store.PurgeOrg(t.Context(), org, deleted.Add(time.Second), nil); !ok || err != nil {
		t.Fatalf("PurgeOrg = %v, %v", ok, err)
	}

	var n int
	if err := d.admin.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM knowledge_sources) + (SELECT count(*) FROM knowledge_uploads) + (SELECT count(*) FROM connections)`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d knowledge and connection rows left, %v; want none", n, err)
	}
}

// Without pgvector's extension the knowledge migration stops, saying what to run.
func TestMigrate_KnowledgeNeedsTheVectorExtension(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	execSQL(t, db.admin, "DROP EXTENSION vector")

	err := Migrate(t.Context(), openPool(t, db.owner))
	if err == nil || !strings.Contains(err.Error(), "extension vector is missing: run goal db init") {
		t.Fatalf("Migrate = %v, want the missing extension named", err)
	}
}

// Review 4 SQL L7: a provider that put pgvector in a schema of its own (an extensions schema is
// common) failed 00054 with "type halfvec does not exist"; goal db init says what to do instead.
func TestInitRoles_PgvectorInAnotherSchemaIsNamed(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	execSQL(t, db.admin, "DROP EXTENSION vector; CREATE SCHEMA extensions; CREATE EXTENSION vector SCHEMA extensions")

	err := pgx.BeginFunc(t.Context(), openPool(t, db.admin), func(tx pgx.Tx) error { return createExtensions(t.Context(), tx) })
	if err == nil || !strings.Contains(err.Error(), `ALTER EXTENSION vector SET SCHEMA public`) {
		t.Fatalf("createExtensions = %v, want the move to Goal's schema named", err)
	}

	execSQL(t, db.admin, "ALTER EXTENSION vector SET SCHEMA public")

	if err := pgx.BeginFunc(t.Context(), openPool(t, db.admin), func(tx pgx.Tx) error { return createExtensions(t.Context(), tx) }); err != nil {
		t.Fatalf("createExtensions after the move = %v", err)
	}
}

// Spec 4 §8.3: a sync's start and end notify the organization's event knowledge, read back from
// the source's row, so a forged one, or one naming another organization's source, is dropped.
func TestNotices_SyncStartAndEndAreKnowledgeEvents(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	k := NewKnowledge(d.store)
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)
	ctx := domain.WithOrg(t.Context(), acme)
	notices := listening(t, NewNotices(d.store, nil), "") // the database is this test's: only its events

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync", n)
	}

	src, err := k.CreateSource(ctx, domain.NewKnowledgeSource{Name: "handbook", Title: "Handbook", Kind: domain.SourceUpload}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Add(time.Second)

	claimed, err := k.ClaimSource(ctx, now, now.Add(time.Minute))
	if err != nil || claimed == nil || claimed.Source.ID != src.ID {
		t.Fatalf("claim = %+v, %v; want handbook", claimed, err)
	}

	forged := func(org domain.OrgID, id domain.KnowledgeSourceID) string {
		return fmt.Sprintf(`{"org_id":%q,"event":"knowledge","data":{"source_id":%q,"name":"forged","status":"failed"}}`, org, id)
	}

	for _, payload := range []string{forged(labs, src.ID), forged(acme, "0b6f1c9e-0000-7000-8000-000000000999")} {
		if _, err := d.admin.Exec(t.Context(), "SELECT pg_notify('goal_org_events', $1)", payload); err != nil {
			t.Fatal(err)
		}
	}

	if err := k.FinishSync(ctx, claimed.Lease, domain.SyncResult{At: now, Due: *claimed.Source.NextSyncAt}); err != nil {
		t.Fatal(err)
	}

	// Read back from the row: by the time the start is read, the sync may have ended.
	if start := nextKnowledgeEvent(t, notices, acme); start.SourceID != string(src.ID) || start.Name != "handbook" {
		t.Errorf("the start's event = %+v; want handbook's", start)
	}

	if end := nextKnowledgeEvent(t, notices, acme); end != (knowledgeEvent{SourceID: string(src.ID), Name: "handbook", Status: "idle"}) {
		t.Errorf("the end's event = %+v; want handbook idle", end)
	}
}

// nextKnowledgeEvent is the next notice, which must be org's knowledge event.
func nextKnowledgeEvent(t *testing.T, notices <-chan ports.ItemNotice, org domain.OrgID) knowledgeEvent {
	t.Helper()

	n := next(t, notices)
	if n.OrgID != org || n.Org == nil || n.Org.Name != "knowledge" {
		t.Fatalf("notice = %+v (org event %+v); want the organization's knowledge event", n, n.Org)
	}

	var got knowledgeEvent
	if err := json.Unmarshal(n.Org.Data, &got); err != nil {
		t.Fatal(err)
	}

	return got
}
