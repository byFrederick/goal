package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.JudgeScores = (*JudgeScores)(nil)

// JudgeScores implements ports.JudgeScores over 00066's judge_scores, as goal_app under
// row-level security: every statement sees the organization in ctx. A claim takes the oldest due
// score with FOR UPDATE SKIP LOCKED under a new lease token, and a record names that token.
type JudgeScores struct{ s *Store }

// NewJudgeScores returns the judge scores on s's pool.
func NewJudgeScores(s *Store) *JudgeScores { return &JudgeScores{s: s} }

// Enqueue implements ports.JudgeScores.
func (j *JudgeScores) Enqueue(ctx context.Context, n domain.NewJudgeScore) (domain.JudgeScore, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.JudgeScore{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.JudgeScore{}, fmt.Errorf("enqueue judge score: %w", err)
	}

	p, err := insertJudgeScoreParams(n)
	if err != nil {
		return domain.JudgeScore{}, fmt.Errorf("enqueue judge score: %w", err)
	}

	var row db.JudgeScore

	err = j.s.inTx(ctx, func(q *db.Queries) error {
		row, err = q.InsertJudgeScore(ctx, p)
		if !notFound(err) {
			return err
		}

		// The judge has a score of the run: the one there, if the organization can see it.
		row, err = q.JudgeScoreOfRun(ctx, db.JudgeScoreOfRunParams{Judge: n.Judge, RunID: *p.RunID})

		return err
	})

	switch {
	case notFound(err), isForeignKeyViolation(err):
		return domain.JudgeScore{}, fmt.Errorf("enqueue judge score of %s: item or run: %w", n.Judge, ports.ErrNotFound)
	case err != nil:
		return domain.JudgeScore{}, fmt.Errorf("enqueue judge score of %s: %w", n.Judge, err)
	default:
		return judgeScoreFrom(row), nil
	}
}

func insertJudgeScoreParams(n domain.NewJudgeScore) (db.InsertJudgeScoreParams, error) {
	id, err := newID()
	if err != nil {
		return db.InsertJudgeScoreParams{}, err
	}

	p := db.InsertJudgeScoreParams{ID: id, Judge: n.Judge, CreatedAt: n.At}

	if p.FactoryID, err = parseID("factory", string(n.FactoryID)); err != nil {
		return p, err
	}

	if p.VersionID, err = parseID("definition version", string(n.VersionID)); err != nil {
		return p, err
	}

	if p.RunID, err = optionalID("run", string(n.RunID)); err != nil {
		return p, err
	}

	p.ItemID, err = optionalID("item", string(n.ItemID))

	return p, err
}

// Claim implements ports.JudgeScores.
func (j *JudgeScores) Claim(ctx context.Context, now, until time.Time) (*domain.JudgeScoreClaim, error) {
	var row db.JudgeScore

	err := j.s.inTx(ctx, func(q *db.Queries) error {
		var err error

		row, err = q.ClaimJudgeScore(ctx, db.ClaimJudgeScoreParams{Until: until, Now: now})

		return err
	})

	switch {
	case notFound(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("claim judge score: %w", err)
	default:
		s := judgeScoreFrom(row)

		return &domain.JudgeScoreClaim{Score: s, Lease: domain.JudgeScoreLease{ID: s.ID, Token: row.LeaseToken.String(), Until: s.LeaseUntil}}, nil
	}
}

// Record implements ports.JudgeScores.
func (j *JudgeScores) Record(ctx context.Context, l domain.JudgeScoreLease, r domain.JudgeResult) (domain.JudgeScore, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.JudgeScore{}, domain.ErrNoOrg
	}

	if err := r.Check(); err != nil {
		return domain.JudgeScore{}, fmt.Errorf("record judge score %s: %w", l.ID, err)
	}

	id, idErr := uuid.Parse(string(l.ID))
	token, tokenErr := uuid.Parse(l.Token)

	if idErr != nil || tokenErr != nil {
		return domain.JudgeScore{}, fmt.Errorf("record judge score %s: %w", l.ID, ports.ErrLeaseLost)
	}

	judgeRun, err := optionalID("judge run", string(r.JudgeRunID))
	if err != nil {
		return domain.JudgeScore{}, fmt.Errorf("record judge score %s: %w", l.ID, err)
	}

	p := db.RecordJudgeScoreParams{
		State: string(r.State), Reason: r.Reason, CostUsd: r.CostUSD, JudgeRunID: judgeRun, ScoredAt: r.At, ID: id, LeaseToken: token,
	}

	if r.State == domain.JudgeScoreScored {
		p.Outcome, p.Score, p.Passed = &r.Outcome, &r.Score, &r.Passed
	}

	row, err := fetch(ctx, j.s.inTx, func(q *db.Queries) (db.JudgeScore, error) { return q.RecordJudgeScore(ctx, p) })

	switch {
	case notFound(err):
		return domain.JudgeScore{}, fmt.Errorf("record judge score %s: %w", l.ID, ports.ErrLeaseLost)
	case err != nil:
		return domain.JudgeScore{}, fmt.Errorf("record judge score %s: %w", l.ID, err)
	default:
		return judgeScoreFrom(row), nil
	}
}

// Recent implements ports.JudgeScores.
func (j *JudgeScores) Recent(ctx context.Context, factory domain.FactoryID, judge string, limit int) ([]domain.JudgeScore, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, fmt.Errorf("recent scores of judge %s: %w", judge, err)
	}

	rows, err := fetch(ctx, j.s.read, func(q *db.Queries) ([]db.RecentJudgeScoresRow, error) {
		return q.RecentJudgeScores(ctx, db.RecentJudgeScoresParams{FactoryID: f, Judge: judge, MaxScores: int64(max(limit, 0))})
	})
	if err != nil {
		return nil, fmt.Errorf("recent scores of judge %s: %w", judge, err)
	}

	out := make([]domain.JudgeScore, 0, len(rows))

	for _, r := range rows {
		s := judgeScoreFrom(r.JudgeScore)
		s.ItemNumber, s.ItemTitle = r.ItemNumber, r.ItemTitle
		out = append(out, s)
	}

	return out, nil
}

// Summary implements ports.JudgeScores.
func (j *JudgeScores) Summary(ctx context.Context, factory domain.FactoryID, now time.Time) ([]domain.JudgeSummary, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, fmt.Errorf("judge summary: %w", err)
	}

	rows, err := fetch(ctx, j.s.read, func(q *db.Queries) ([]db.JudgeScoreDaysRow, error) {
		return q.JudgeScoreDays(ctx, db.JudgeScoreDaysParams{FactoryID: f, Since: now.Add(-domain.JudgeSummaryWindow), Until: now})
	})
	if err != nil {
		return nil, fmt.Errorf("judge summary: %w", err)
	}

	// Rows come by judge, then day.
	var out []domain.JudgeSummary

	for _, r := range rows {
		if len(out) == 0 || out[len(out)-1].Judge != r.Judge {
			out = append(out, domain.JudgeSummary{Judge: r.Judge})
		}

		sum := &out[len(out)-1]
		scored, passed := int(r.Scored), int(r.Passed)
		sum.Scored += scored
		sum.Passed += passed
		sum.CostUSD += r.CostUsd

		if scored > 0 {
			sum.Days = append(sum.Days, domain.JudgeDay{Day: r.Day.UTC(), Scored: scored, Passed: passed, PassRate: domain.PassRate(scored, passed)})
		}
	}

	for i := range out {
		out[i].PassRate = domain.PassRate(out[i].Scored, out[i].Passed)
		out[i].CostUSD = math.Round(out[i].CostUSD*1e4) / 1e4
	}

	return out, nil
}

// optionalID parses a domain id that may be empty; a non-empty one that is not a UUID is not found.
func optionalID(kind, id string) (*uuid.UUID, error) {
	if id == "" {
		return nil, nil
	}

	u, err := parseID(kind, id)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

func judgeScoreFrom(r db.JudgeScore) domain.JudgeScore {
	s := domain.JudgeScore{
		ID: domain.JudgeScoreID(r.ID.String()), OrgID: domain.OrgID(r.OrgID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()),
		VersionID: domain.DefinitionVersionID(r.VersionID.String()), Judge: r.Judge, State: domain.JudgeScoreState(r.State),
		Outcome: deref(r.Outcome), Reason: r.Reason, CostUSD: r.CostUsd, CreatedAt: r.CreatedAt.UTC(),
	}

	if r.RunID != nil {
		s.RunID = domain.RunID(r.RunID.String())
	}

	if r.ItemID != nil {
		s.ItemID = domain.ItemID(r.ItemID.String())
	}

	if r.JudgeRunID != nil {
		s.JudgeRunID = domain.RunID(r.JudgeRunID.String())
	}

	if r.Score != nil {
		s.Score = *r.Score
	}

	if r.Passed != nil {
		s.Passed = *r.Passed
	}

	if r.LeaseUntil != nil {
		s.LeaseUntil = r.LeaseUntil.UTC()
	}

	if r.ScoredAt != nil {
		s.ScoredAt = r.ScoredAt.UTC()
	}

	return s
}
