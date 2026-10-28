//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

var jonahAt = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

// ruleA is the path the tests' proposals edit.
const ruleA = "rules/a.md"

// seedJonah gives a new factory of ctx's organization a finished review with one proposal and
// one observation, and returns the factory and the review's row.
func seedJonah(ctx context.Context, t *testing.T, s *Store) (domain.FactoryID, db.JonahReview) {
	t.Helper()

	org, _ := domain.OrgFrom(ctx)
	factory := saveFactory(ctx, t, s)

	if _, err := s.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: factory, VersionID: newPublishedVersion(t, s.pool, org, factory), Trigger: domain.JonahManual,
		WindowStart: jonahAt.Add(-time.Hour), WindowEnd: jonahAt, By: domain.LocalUser, At: jonahAt,
	}); err != nil {
		t.Fatal(err)
	}

	claimed, err := s.ClaimJonahReview(ctx, jonahAt, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.FinishJonahReview(ctx, claimed, domain.JonahReviewResult{
		State: domain.JonahDone, At: jonahAt,
		Proposals:    []domain.NewProposal{{Title: "a", Files: []domain.ProposalFile{{Path: ruleA, Content: "x"}}}},
		Observations: []domain.NewObservation{{Summary: "seen", Count: 1, Proposal: 1}},
	}); err != nil {
		t.Fatal(err)
	}

	var row db.JonahReview

	err = s.read(ctx, func(q *db.Queries) error {
		rows, err := q.LatestJonahReviews(ctx, db.LatestJonahReviewsParams{FactoryID: uuid.MustParse(string(factory)), MaxReviews: 1})
		if err == nil && len(rows) == 1 {
			row = rows[0]
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return factory, row
}

// The CHECKs refuse, as goal_app, a proposal the domain check would have refused first: a path
// outside the allowlist or with a .. segment, files whose paths are not paths, too many bytes,
// and a rejection without a reason.
func TestJonah_ProposalChecks(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	ctx := domain.WithOrg(t.Context(), newOrgID(t, s))
	_, review := seedJonah(ctx, t, s)

	insert := func(paths []string, files []domain.ProposalFile) error {
		raw, err := json.Marshal(files)
		if err != nil {
			t.Fatal(err)
		}

		return s.inTx(ctx, func(q *db.Queries) error {
			_, err := q.InsertProposal(ctx, db.InsertProposalParams{
				ID: uuid.Must(uuid.NewV7()), FactoryID: review.FactoryID, Number: 99, ReviewID: review.ID, VersionID: review.VersionID,
				Title: "raw", Files: raw, Evidence: []byte("[]"), Estimate: []byte("{}"), Paths: paths, CreatedAt: jonahAt,
			})

			return err
		})
	}

	file := func(path, content string) domain.ProposalFile {
		return domain.ProposalFile{Path: path, Content: content}
	}

	for name, tc := range map[string]struct {
		paths []string
		files []domain.ProposalFile
	}{
		"evals":          {[]string{"evals/cases/a.yaml"}, []domain.ProposalFile{file("evals/cases/a.yaml", "x")}},
		"parent segment": {[]string{"agents/../evals/a.md"}, []domain.ProposalFile{file("agents/../evals/a.md", "x")}},
		"no files":       {[]string{}, []domain.ProposalFile{}},
		"paths differ":   {[]string{ruleA}, []domain.ProposalFile{file("rules/b.md", "x")}},
		"too big":        {[]string{ruleA}, []domain.ProposalFile{file(ruleA, strings.Repeat("x", domain.MaxProposalFilesBytes+1))}},
		"bad base hash":  {[]string{ruleA}, []domain.ProposalFile{{Path: ruleA, BaseSHA256: "abc", Content: "x"}}},
	} {
		if err := insert(tc.paths, tc.files); !isCheckViolationErr(err) {
			t.Errorf("%s: insert = %v, want a check violation", name, err)
		}
	}

	if err := insert([]string{"factory.yaml"}, []domain.ProposalFile{file("factory.yaml", "x")}); err != nil {
		t.Fatalf("an allowed proposal: %v", err)
	}

	err := s.inTx(ctx, func(q *db.Queries) error {
		by := domain.LocalUser.Ref()
		_, err := q.DecideProposal(ctx, db.DecideProposalParams{
			State: string(domain.ProposalRejected), DecidedBy: &by, DecidedAt: jonahAt, FactoryID: review.FactoryID, Number: 99,
		})

		return err
	})
	if !isCheckViolationErr(err) {
		t.Errorf("reject without a reason = %v, want a check violation", err)
	}
}

// The purge of a deleted organization and the delete of an archived factory remove the Jonah
// rows through their keys' cascades, and leave another organization's.
func TestJonah_PurgesRemoveRows(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	defs := NewDefinitions(pool, porttest.NewObjectStore(), nil)

	acme, labs := newOrgID(t, s), newOrgID(t, s)
	acmeCtx, labsCtx := domain.WithOrg(t.Context(), acme), domain.WithOrg(t.Context(), labs)
	seedJonah(acmeCtx, t, s)
	gone, _ := seedJonah(labsCtx, t, s)
	seedJonah(labsCtx, t, s)

	checkJonahCounts(t, s.pool, "before", map[domain.OrgID]int{acme: 1, labs: 2})

	if _, err := defs.SetPaused(labsCtx, gone, true, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if _, err := defs.ArchiveFactory(labsCtx, gone, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if err := defs.DeleteArchivedFactory(labsCtx, gone, domain.LocalUser); err != nil {
		t.Fatalf("DeleteArchivedFactory of a factory with Jonah rows: %v", err)
	}

	checkJonahCounts(t, s.pool, "after labs' factory delete", map[domain.OrgID]int{acme: 1, labs: 1})

	deleted := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if err := s.DeleteOrg(acmeCtx, domain.LocalUser, deleted); err != nil {
		t.Fatal(err)
	}

	if ok, err := s.PurgeOrg(t.Context(), acme, deleted.Add(time.Second), nil); !ok || err != nil {
		t.Fatalf("PurgeOrg of an org with Jonah rows = %v, %v; want purged", ok, err)
	}

	checkJonahCounts(t, s.pool, "after acme's purge", map[domain.OrgID]int{acme: 0, labs: 1})
}

// checkJonahCounts checks that each org sees want reviews, proposals and observations each in
// raw SQL, and none of another org's.
func checkJonahCounts(t *testing.T, pool *pgxpool.Pool, when string, want map[domain.OrgID]int) {
	t.Helper()

	for org, n := range want {
		for _, table := range []string{"jonah_reviews", "jonah_proposals", "jonah_observations"} {
			var all, foreign int
			if err := asOrg(t, pool, org, "SELECT count(*), count(*) FILTER (WHERE org_id <> '"+string(org)+"') FROM "+table, &all, &foreign); err != nil {
				t.Fatal(err)
			}

			if all != n || foreign != 0 {
				t.Errorf("%s, org %s sees %d rows of %s, %d of another org; want %d, none", when, org, all, table, foreign, n)
			}
		}
	}
}
