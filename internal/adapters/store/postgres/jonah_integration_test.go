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
		Proposals:    []domain.NewProposal{{Title: "a", Edits: []domain.ProposalEdit{{Path: ruleA, Content: "x"}}}},
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
// outside the allowlist (a script, or a memory.yaml not at the root, among them) or with a ..
// segment, edits whose paths are not paths, a malformed edit, too many bytes, and a rejection
// without a reason. memory.yaml at the root is allowed (ADR 0039 decision 7).
func TestJonah_ProposalChecks(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	ctx := domain.WithOrg(t.Context(), newOrgID(t, s))
	_, review := seedJonah(ctx, t, s)

	insert := func(paths []string, edits any) error {
		raw, err := json.Marshal(edits)
		if err != nil {
			t.Fatal(err)
		}

		return s.inTx(ctx, func(q *db.Queries) error {
			_, err := q.InsertProposal(ctx, db.InsertProposalParams{
				ID: uuid.Must(uuid.NewV7()), FactoryID: review.FactoryID, Number: 99, ReviewID: &review.ID, VersionID: review.VersionID,
				Title: "raw", Edits: raw, Evidence: []byte("[]"), Estimate: []byte("{}"), Paths: paths, CreatedAt: jonahAt,
			})

			return err
		})
	}

	whole := func(path, content string) []domain.ProposalEdit {
		return []domain.ProposalEdit{{Path: path, Content: content}}
	}
	replace := func(path, old, nw string) []domain.ProposalEdit {
		return []domain.ProposalEdit{{Path: path, Replacements: []domain.Replacement{{Old: old, New: nw}}}}
	}

	const editPath = "path"

	for name, tc := range map[string]struct {
		paths []string
		edits any
	}{
		"evals":             {[]string{"evals/cases/a.yaml"}, whole("evals/cases/a.yaml", "x")},
		"script":            {[]string{"skills/x/run.sh"}, replace("skills/x/run.sh", "a", "b")},
		"other file":        {[]string{"agents/a.txt"}, whole("agents/a.txt", "x")},
		"memory elsewhere":  {[]string{"x/memory.yaml"}, whole("x/memory.yaml", "x")},
		"memory backup":     {[]string{"memory.yaml.bak"}, whole("memory.yaml.bak", "x")},
		"parent segment":    {[]string{"agents/../evals/a.md"}, whole("agents/../evals/a.md", "x")},
		"no edits":          {[]string{}, []domain.ProposalEdit{}},
		"paths differ":      {[]string{ruleA}, whole("rules/b.md", "x")},
		"too big":           {[]string{ruleA}, replace(ruleA, "a", strings.Repeat("x", domain.MaxProposalFilesBytes))},
		"bad base hash":     {[]string{ruleA}, []domain.ProposalEdit{{Path: ruleA, BaseSHA256: "abc", Content: "x"}}},
		"empty old":         {[]string{ruleA}, replace(ruleA, "", "b")},
		"no replacements":   {[]string{ruleA}, []map[string]any{{editPath: ruleA, "replacements": []any{}}}},
		"both":              {[]string{ruleA}, []map[string]any{{editPath: ruleA, "content": "x", "replacements": []any{map[string]any{"old": "a", "new": "b"}}}}},
		"neither":           {[]string{ruleA}, []map[string]any{{editPath: ruleA}}},
		"replacement shape": {[]string{ruleA}, []map[string]any{{editPath: ruleA, "replacements": []any{map[string]any{"old": "a"}}}}},
	} {
		if err := insert(tc.paths, tc.edits); !isCheckViolationErr(err) {
			t.Errorf("%s: insert = %v, want a check violation", name, err)
		}
	}

	allowed := append(replace("factory.yaml", "wip: 2", "wip: 3"), whole(ruleA, "x")...)
	if err := insert([]string{"factory.yaml", ruleA, "memory.yaml"}, append(allowed, whole("memory.yaml", "[]\n")...)); err != nil {
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
