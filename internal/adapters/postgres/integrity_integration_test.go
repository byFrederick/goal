//go:build integration

package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/domain"
)

// Every table with org_id has a full index that starts with it, so the purge's deletes by org and
// the cascades from organizations never scan a whole table (review 2.2 M2). A table added later
// without one fails here.
func TestSchema_EveryOrgTableHasAnIndexOnOrgID(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	for _, table := range queryStrings(t, openPool(t, db.admin), `
		SELECT c.relname::text FROM pg_class c
		WHERE c.relnamespace = current_schema()::text::regnamespace AND c.relkind IN ('r', 'p')
		  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'org_id' AND NOT a.attisdropped)
		  AND NOT EXISTS (SELECT 1 FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
		                  WHERE i.indrelid = c.oid AND a.attname = 'org_id' AND i.indpred IS NULL)
		ORDER BY 1`) {
		t.Errorf("table %s has no index starting with org_id", table)
	}
}

// A work item's parent belongs to its own organization (review 2.2 M3): foreign keys ignore
// row-level security, so a plain parent_id key let an item point at another organization's item,
// told the caller that item existed, and then made the other organization's purge fail.
func TestWorkItems_ParentFromAnotherOrgIsRefused(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	acme, labs := newOrgID(t, s), newOrgID(t, s)
	acmeFactory := saveFactory(domain.WithOrg(t.Context(), acme), t, s)
	labsFactory := saveFactory(domain.WithOrg(t.Context(), labs), t, s)

	parent, err := s.CreateItem(domain.WithOrg(t.Context(), labs), domain.NewItem{
		FactoryID: labsFactory, Type: typeTask, Title: "labs parent", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	err = inOrgTx(t, pool, acme, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO work_items (id, factory_id, number, parent_id, type, title, station, state, created_at)
			VALUES ($1, $2, 1, $3, 'task', 'child', 'work', 'queued', now())`, uuid.NewString(), string(acmeFactory), string(parent.ID))

		return err
	})
	if err == nil || !strings.Contains(err.Error(), "foreign key") {
		t.Errorf("an acme item whose parent is a labs item = %v; want a foreign key violation", err)
	}
}

// A touch label is its event's, of the event's item and organization (review 4 SQL L2): the
// event_id key ignores row-level security, so an organization could take another's event id for
// a label of its own, and that organization's own label of the event then failed.
func TestTouchLabels_AnotherOrgsEventIsRefused(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	acme, labs := newOrgID(t, s), newOrgID(t, s)
	item := func(org domain.OrgID) domain.ItemID {
		ctx := domain.WithOrg(t.Context(), org)

		it, err := s.CreateItem(ctx, domain.NewItem{
			FactoryID: saveFactory(ctx, t, s), Type: typeTask, Title: "an item", Station: stWork, State: domain.StateQueued,
			Class: domain.ClassStandard, CreatedAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}

		return it.ID
	}
	acmeItem, labsItem := item(acme), item(labs)

	var event int64

	if err := inOrgTx(t, pool, acme, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT id FROM item_events WHERE item_id = $1 ORDER BY id LIMIT 1", string(acmeItem)).Scan(&event)
	}); err != nil {
		t.Fatal(err)
	}

	err := inOrgTx(t, pool, labs, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "INSERT INTO item_touch_labels (event_id, org_id, item_id) VALUES ($1, $2, $3)",
			event, string(labs), string(labsItem))

		return err
	})
	if pgErr := (*pgconn.PgError)(nil); !errors.As(err, &pgErr) || pgErr.ConstraintName != "item_touch_labels_event_of_item" {
		t.Errorf("a labs label of an acme event = %v; want refused", err)
	}

	// acme labels its own event.
	if err := inOrgTx(t, pool, acme, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "INSERT INTO item_touch_labels (event_id, org_id, item_id) VALUES ($1, $2, $3)",
			event, string(acme), string(acmeItem))

		return err
	}); err != nil {
		t.Errorf("acme's label of its own event = %v", err)
	}
}

// On a factory with a year of history, the flow reads come from an index that holds their order
// or window (review 2.2 L2): the factory's first lease span without sorting all of them, and the
// gate verdicts of the last week without reading every verdict the factory ever had.
func TestFlowReads_UseTheirIndexes(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)
	admin := openPool(t, db.admin)
	factory := uuid.NewString()

	// A year of hourly verdicts and lease spans, written as the bootstrap superuser with triggers
	// and key checks off (session_replication_role), so no notices or spans come of them.
	err := pgx.BeginFunc(t.Context(), admin, func(tx pgx.Tx) error {
		for _, stmt := range []string{
			"SET LOCAL session_replication_role = replica",
			`INSERT INTO item_events (org_id, factory_id, item_id, at, kind, station, actor)
			 SELECT gen_random_uuid(), $1::uuid, gen_random_uuid(), now() - h * interval '1 hour',
			        CASE WHEN h % 3 = 0 THEN 'gate_rejected' ELSE 'gate_approved' END, 'review', 'reviewer'
			 FROM generate_series(1, 8760) h`,
			`INSERT INTO lease_spans (org_id, factory_id, item_id, station, agent, slot, rework_round, acquired_at, ended_at, outcome)
			 SELECT gen_random_uuid(), $1::uuid, gen_random_uuid(), 'work', 'worker', 0, 0,
			        now() - h * interval '1 hour', now() - h * interval '1 hour' + interval '5 minutes', 'advanced'
			 FROM generate_series(1, 8760) h`,
		} {
			args := []any{factory}
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
		t.Fatal(err)
	}

	if _, err := admin.Exec(t.Context(), "ANALYZE item_events, lease_spans"); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ index, query string }{
		{"lease_spans_acquired_idx", `SELECT acquired_at FROM lease_spans WHERE factory_id = $1::uuid ORDER BY acquired_at LIMIT 1`},
		{"item_events_verdicts_at_idx", `SELECT station, count(*) FROM item_events
			WHERE factory_id = $1::uuid AND kind IN ('gate_approved', 'gate_rejected') AND at >= now() - interval '7 days' AND at < now()
			GROUP BY station`},
	} {
		plan := explain(t, admin, c.query, factory)
		if !strings.Contains(plan, c.index) {
			t.Errorf("plan of %s does not use %s:\n%s", strings.Join(strings.Fields(c.query), " "), c.index, plan)
		}
	}
}

// explain returns the plan of query with sequential scans disabled.
func explain(t *testing.T, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()

	var lines []string

	err := pgx.BeginFunc(t.Context(), pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "SET LOCAL enable_seqscan = off"); err != nil {
			return err
		}

		rows, err := tx.Query(t.Context(), "EXPLAIN "+query, args...)
		if err != nil {
			return err
		}

		lines, err = pgx.CollectRows(rows, pgx.RowTo[string])

		return err
	})
	if err != nil {
		t.Fatalf("EXPLAIN %s: %v", query, err)
	}

	return strings.Join(lines, "\n")
}

// A reopened item that was in a rework round keeps the station that started the round (review
// 2.2 L4): with no span open, the next one used to lose its rework_by.
func TestSpans_AReopenedItemKeepsItsReworkBy(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	org := newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), org)
	f := saveFactory(ctx, t, s)
	t0 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	it, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: f, Type: typeTask, Title: "reworked", Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = inOrgTx(t, pool, org, func(tx pgx.Tx) error {
		for i, set := range []string{
			"station = 'review'",                 // to review
			"station = 'work', rework_count = 1", // sent back by review: round 1
			"state = 'done'",                     // done: no span open
			"state = 'queued'",                   // reopened in round 1
		} {
			if _, err := tx.Exec(t.Context(), "UPDATE work_items SET "+set+", updated_at = $2 WHERE id = $1",
				string(it.ID), t0.Add(time.Duration(i+1)*time.Minute)); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	spans, err := s.ItemSpans(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}

	last := spans[len(spans)-1]
	if !last.LeftAt.IsZero() || last.ReworkRound != 1 || last.ReworkBy != "review" {
		t.Errorf("the reopened item's open span = %+v; want round 1 by review", last)
	}
}
