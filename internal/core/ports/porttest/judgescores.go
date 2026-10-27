package porttest

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.JudgeScores = (*JudgeScores)(nil)

// JudgeItems is what JudgeScores reads of the items and runs its scores name: a Store.
type JudgeItems interface {
	Item(ctx context.Context, id domain.ItemID) (domain.Item, error)
	Run(ctx context.Context, id domain.RunID) (domain.Run, error)
}

// JudgeScores keeps judge scores in memory with the database's rules: one score per judge and
// run, claims of the oldest due score that skip held ones and take back ended leases, records
// fenced by the lease's token, costs kept to four decimals, and organizations kept apart. Items
// and runs are looked up in items, under the organization in ctx.
type JudgeScores struct {
	items JudgeItems

	mu   sync.Mutex
	rows []*judgeScoreRow
}

type judgeScoreRow struct {
	score domain.JudgeScore
	token string
	held  bool
}

// NewJudgeScores returns no scores, naming the items and runs of items.
func NewJudgeScores(items JudgeItems) *JudgeScores { return &JudgeScores{items: items} }

// Hold holds score id of org, as another transaction's lock would, until release is called:
// Claim skips it meanwhile.
func (j *JudgeScores) Hold(org domain.OrgID, id domain.JudgeScoreID) (release func()) {
	j.mu.Lock()
	defer j.mu.Unlock()

	for _, r := range j.rows {
		if r.score.OrgID == org && r.score.ID == id {
			r.held = true

			return func() {
				j.mu.Lock()
				defer j.mu.Unlock()

				r.held = false
			}
		}
	}

	return func() {}
}

// Enqueue implements ports.JudgeScores.
func (j *JudgeScores) Enqueue(ctx context.Context, n domain.NewJudgeScore) (domain.JudgeScore, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.JudgeScore{}, err
	}

	if err := n.Check(); err != nil {
		return domain.JudgeScore{}, fmt.Errorf("enqueue judge score: %w", err)
	}

	if n.ItemID != "" {
		if _, err := j.items.Item(ctx, n.ItemID); err != nil {
			return domain.JudgeScore{}, fmt.Errorf("enqueue judge score: %w", err)
		}
	}

	if n.RunID != "" {
		if _, err := j.items.Run(ctx, n.RunID); err != nil {
			return domain.JudgeScore{}, fmt.Errorf("enqueue judge score: %w", err)
		}
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	if n.RunID != "" {
		for _, r := range j.rows {
			if r.score.Judge == n.Judge && r.score.RunID == n.RunID {
				return r.score, nil
			}
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.JudgeScore{}, fmt.Errorf("enqueue judge score: %w", err)
	}

	s := domain.JudgeScore{
		ID: domain.JudgeScoreID(id.String()), OrgID: org, FactoryID: n.FactoryID, VersionID: n.VersionID, Judge: n.Judge,
		RunID: n.RunID, ItemID: n.ItemID, State: domain.JudgeScoreQueued, CreatedAt: n.At.UTC(),
	}
	j.rows = append(j.rows, &judgeScoreRow{score: s})

	return s, nil
}

// Claim implements ports.JudgeScores.
func (j *JudgeScores) Claim(ctx context.Context, now, until time.Time) (*domain.JudgeScoreClaim, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	var next *judgeScoreRow

	for _, r := range j.rows {
		s := r.score
		due := s.State == domain.JudgeScoreQueued || (s.State == domain.JudgeScoreRunning && !s.LeaseUntil.After(now))

		if s.OrgID != org || r.held || !due {
			continue
		}

		if next == nil || s.CreatedAt.Before(next.score.CreatedAt) || (s.CreatedAt.Equal(next.score.CreatedAt) && s.ID < next.score.ID) {
			next = r
		}
	}

	if next == nil {
		return nil, nil
	}

	next.token = uuid.NewString()
	next.score.State, next.score.LeaseUntil = domain.JudgeScoreRunning, until.UTC()

	return &domain.JudgeScoreClaim{
		Score: next.score,
		Lease: domain.JudgeScoreLease{ID: next.score.ID, Token: next.token, Until: next.score.LeaseUntil},
	}, nil
}

// Record implements ports.JudgeScores.
func (j *JudgeScores) Record(ctx context.Context, l domain.JudgeScoreLease, res domain.JudgeResult) (domain.JudgeScore, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.JudgeScore{}, err
	}

	if err := res.Check(); err != nil {
		return domain.JudgeScore{}, fmt.Errorf("record judge score %s: %w", l.ID, err)
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	for _, r := range j.rows {
		s := &r.score
		if s.OrgID != org || s.ID != l.ID || s.State != domain.JudgeScoreRunning || r.token != l.Token || !s.LeaseUntil.After(res.At) {
			continue
		}

		s.State, s.Outcome, s.Score, s.Passed, s.Reason = res.State, res.Outcome, res.Score, res.Passed, res.Reason
		s.CostUSD, s.JudgeRunID, s.ScoredAt, s.LeaseUntil = math.Round(res.CostUSD*1e4)/1e4, res.JudgeRunID, res.At.UTC(), time.Time{}
		r.token = ""

		return *s, nil
	}

	return domain.JudgeScore{}, fmt.Errorf("record judge score %s: %w", l.ID, ports.ErrLeaseLost)
}

// Recent implements ports.JudgeScores.
func (j *JudgeScores) Recent(ctx context.Context, factory domain.FactoryID, judge string, limit int) ([]domain.JudgeScore, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	j.mu.Lock()

	var out []domain.JudgeScore

	for _, r := range j.rows {
		if s := r.score; s.OrgID == org && s.FactoryID == factory && s.Judge == judge && s.RunID != "" {
			out = append(out, s)
		}
	}

	j.mu.Unlock()

	slices.SortFunc(out, func(a, b domain.JudgeScore) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
	out = out[:min(len(out), max(limit, 0))]

	for i := range out {
		it, err := j.items.Item(ctx, out[i].ItemID)
		if err != nil {
			return nil, fmt.Errorf("recent judge scores: %w", err)
		}

		out[i].ItemNumber, out[i].ItemTitle = it.Number, it.Title
	}

	return out, nil
}

// Summary implements ports.JudgeScores.
func (j *JudgeScores) Summary(ctx context.Context, factory domain.FactoryID, now time.Time) ([]domain.JudgeSummary, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	since := now.Add(-domain.JudgeSummaryWindow)

	j.mu.Lock()
	defer j.mu.Unlock()

	byJudge := map[string]*domain.JudgeSummary{}
	days := map[string]map[time.Time]*domain.JudgeDay{}

	for _, r := range j.rows {
		s := r.score
		ended := s.State == domain.JudgeScoreScored || s.State == domain.JudgeScoreError

		if s.OrgID != org || s.FactoryID != factory || s.RunID == "" || !ended || s.CreatedAt.Before(since) || s.CreatedAt.After(now) {
			continue
		}

		sum, ok := byJudge[s.Judge]
		if !ok {
			sum = &domain.JudgeSummary{Judge: s.Judge}
			byJudge[s.Judge], days[s.Judge] = sum, map[time.Time]*domain.JudgeDay{}
		}

		sum.CostUSD += s.CostUSD

		if s.State != domain.JudgeScoreScored {
			continue
		}

		day := domain.JudgeDayOf(s.CreatedAt)

		d, ok := days[s.Judge][day]
		if !ok {
			d = &domain.JudgeDay{Day: day}
			days[s.Judge][day] = d
		}

		sum.Scored++
		d.Scored++

		if s.Passed {
			sum.Passed++
			d.Passed++
		}
	}

	out := make([]domain.JudgeSummary, 0, len(byJudge))

	for judge, sum := range byJudge {
		sum.PassRate = domain.PassRate(sum.Scored, sum.Passed)
		sum.CostUSD = math.Round(sum.CostUSD*1e4) / 1e4

		for _, d := range days[judge] {
			d.PassRate = domain.PassRate(d.Scored, d.Passed)
			sum.Days = append(sum.Days, *d)
		}

		slices.SortFunc(sum.Days, func(a, b domain.JudgeDay) int { return a.Day.Compare(b.Day) })
		out = append(out, *sum)
	}

	slices.SortFunc(out, func(a, b domain.JudgeSummary) int { return cmp.Compare(a.Judge, b.Judge) })

	return out, nil
}
