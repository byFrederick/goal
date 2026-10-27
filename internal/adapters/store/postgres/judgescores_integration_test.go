//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/judgescoretest"
)

func TestJudgeScores_Conformance(t *testing.T) {
	t.Parallel()

	judgescoretest.Run(t, func(t *testing.T) judgescoretest.Harness {
		t.Helper()

		s := New(newTestPool(t))

		return judgescoretest.Harness{
			JudgeScores: NewJudgeScores(s),
			Store:       s,
			NewOrg:      func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, s) },
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, s)
			},
			NewVersion: func(t *testing.T, org domain.OrgID, factory domain.FactoryID) domain.DefinitionVersionID {
				t.Helper()

				return newPublishedVersion(t, s.pool, org, factory)
			},
			Hold: func(t *testing.T, org domain.OrgID, id domain.JudgeScoreID) func() {
				t.Helper()

				return holdJudgeScore(t, s.pool, org, id)
			},
		}
	})
}

// holdJudgeScore locks score id in a transaction of org's, as another goald's claim does, until
// release rolls it back.
func holdJudgeScore(t *testing.T, pool *pgxpool.Pool, org domain.OrgID, id domain.JudgeScoreID) (release func()) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	rollback := func() { _ = tx.Rollback(t.Context()) }

	if _, err := tx.Exec(t.Context(), "SELECT set_config('goal.org_id', $1, true)", string(org)); err != nil {
		rollback()
		t.Fatal(err)
	}

	var n int
	if err := tx.QueryRow(t.Context(), "SELECT count(*) FROM (SELECT 1 FROM judge_scores WHERE id = $1 FOR UPDATE) l", string(id)).Scan(&n); err != nil || n != 1 {
		rollback()
		t.Fatalf("hold judge score %s: %d rows, %v; want it locked", id, n, err)
	}

	t.Cleanup(rollback)

	return rollback
}

// One organization never sees, claims or counts another's judge scores, in raw SQL as in the
// port; and the purge of a deleted organization removes its scores with its factory, items and
// runs, the cascades not blocking it.
func TestJudgeScores_RLS(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	j := NewJudgeScores(s)
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	acme, labs := newOrgID(t, s), newOrgID(t, s)
	acmeCtx, labsCtx := domain.WithOrg(t.Context(), acme), domain.WithOrg(t.Context(), labs)
	seedJudgeScore(acmeCtx, t, s, j, at)
	seedJudgeScore(acmeCtx, t, s, j, at)
	seedJudgeScore(labsCtx, t, s, j, at)

	checkJudgeScoreCounts(t, s.pool, "before the purge", map[domain.OrgID]int{acme: 2, labs: 1})

	if c, err := j.Claim(labsCtx, at, at.Add(time.Minute)); err != nil || c == nil || c.Score.OrgID != labs {
		t.Fatalf("labs Claim = %+v, %v; want its own score", c, err)
	}

	if c, err := j.Claim(labsCtx, at, at.Add(time.Minute)); err != nil || c != nil {
		t.Errorf("labs Claim with only acme's scores queued = %+v, %v; want nil", c, err)
	}

	deleted := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	if err := s.DeleteOrg(acmeCtx, domain.LocalUser, deleted); err != nil {
		t.Fatal(err)
	}

	if ok, err := s.PurgeOrg(t.Context(), acme, deleted.Add(time.Second), nil); !ok || err != nil {
		t.Fatalf("PurgeOrg of an org with judge scores = %v, %v; want purged", ok, err)
	}

	checkJudgeScoreCounts(t, s.pool, "after acme's purge", map[domain.OrgID]int{acme: 0, labs: 1})
}

// checkJudgeScoreCounts checks that each org sees its wanted number of judge scores in raw SQL,
// and none of another org's.
func checkJudgeScoreCounts(t *testing.T, pool *pgxpool.Pool, when string, want map[domain.OrgID]int) {
	t.Helper()

	for org, n := range want {
		var all, foreign int
		if err := asOrg(t, pool, org, "SELECT count(*), count(*) FILTER (WHERE org_id <> '"+string(org)+"') FROM judge_scores", &all, &foreign); err != nil {
			t.Fatal(err)
		}

		if all != n || foreign != 0 {
			t.Errorf("%s, org %s sees %d judge scores, %d of another org; want %d, none", when, org, all, foreign, n)
		}
	}
}

// seedJudgeScore queues a score of a new run of a new item of a new factory of ctx's organization.
func seedJudgeScore(ctx context.Context, t *testing.T, s *Store, j *JudgeScores, at time.Time) {
	t.Helper()

	org, _ := domain.OrgFrom(ctx)
	factory := saveFactory(ctx, t, s)
	version := newPublishedVersion(t, s.pool, org, factory)

	it, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: factory, Type: typeTask, Title: "a judged item", Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	run, err := s.StartRun(ctx, ports.NewRun{FactoryID: factory, DefinitionVersionID: version, ItemID: it.ID, Station: stWork, Agent: "backend", Model: "claude-test", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := j.Enqueue(ctx, domain.NewJudgeScore{FactoryID: factory, VersionID: version, Judge: "spec-fidelity", RunID: run, ItemID: it.ID, At: at}); err != nil {
		t.Fatal(err)
	}
}
