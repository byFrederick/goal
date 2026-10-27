package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var (
	_ ports.Judges       = (*Judges)(nil)
	_ ports.FinishedRuns = (*FinishedRuns)(nil)
	_ ports.JudgeRunner  = (*JudgeRunner)(nil)
)

// Judges keeps each definition version's judges in memory, by organization.
type Judges struct {
	mu       sync.Mutex
	versions map[judgesKey][]domain.Judge
}

type judgesKey struct {
	org     domain.OrgID
	factory domain.FactoryID
	version domain.DefinitionVersionID
}

// NewJudges returns no versions.
func NewJudges() *Judges { return &Judges{versions: map[judgesKey][]domain.Judge{}} }

// Set makes judges the judges of version of org's factory.
func (j *Judges) Set(org domain.OrgID, factory domain.FactoryID, version domain.DefinitionVersionID, judges ...domain.Judge) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.versions[judgesKey{org, factory, version}] = slices.Clone(judges)
}

// Judges implements ports.Judges.
func (j *Judges) Judges(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID) ([]domain.Judge, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	judges, ok := j.versions[judgesKey{org, factory, version}]
	if !ok {
		return nil, fmt.Errorf("judges of version %s: %w", version, ports.ErrNotFound)
	}

	return slices.Clone(judges), nil
}

// FinishedRuns reads the finished runs of a Store.
type FinishedRuns struct{ store *Store }

// NewFinishedRuns reads the finished runs of s.
func NewFinishedRuns(s *Store) *FinishedRuns { return &FinishedRuns{store: s} }

// finished is every finished run of org, oldest end first.
func (f *FinishedRuns) finished(org domain.OrgID) []domain.Run {
	s := f.store

	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.Run

	for _, r := range s.runs {
		if _, ok := s.visible(r.ItemID, org); ok && r.Result.Status != domain.RunRunning {
			out = append(out, *r)
		}
	}

	slices.SortFunc(out, func(a, b domain.Run) int { return cmp.Or(a.EndedAt.Compare(b.EndedAt), cmp.Compare(a.ID, b.ID)) })

	return out
}

// EndedBetween implements ports.FinishedRuns.
func (f *FinishedRuns) EndedBetween(ctx context.Context, since, until time.Time) ([]domain.Run, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	var out []domain.Run

	for _, r := range f.finished(org) {
		if !r.EndedAt.Before(since) && !r.EndedAt.After(until) {
			out = append(out, r)
		}
	}

	return out, nil
}

// Latest implements ports.FinishedRuns.
func (f *FinishedRuns) Latest(ctx context.Context, q ports.FinishedRunQuery) ([]domain.Run, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	all := f.finished(org)
	slices.Reverse(all)

	var out []domain.Run

	for _, r := range all {
		switch {
		case len(out) >= q.Limit:
			return out, nil
		case r.FactoryID != q.Factory,
			len(q.Agents) > 0 && !slices.Contains(q.Agents, r.Agent),
			q.Item != "" && r.ItemID != q.Item,
			!q.EndedBy.IsZero() && r.EndedAt.After(q.EndedBy):
			continue
		}

		out = append(out, r)
	}

	return out, nil
}

// JudgeRunner answers judge runs with a function a test sets, and counts its calls.
type JudgeRunner struct {
	mu    sync.Mutex
	run   func(ctx context.Context, call int, t ports.JudgeTask) (ports.JudgeRun, error)
	tasks []ports.JudgeTask
}

// NewJudgeRunner answers each judge run with run, given the call's number from 1.
func NewJudgeRunner(run func(ctx context.Context, call int, t ports.JudgeTask) (ports.JudgeRun, error)) *JudgeRunner {
	return &JudgeRunner{run: run}
}

// Tasks returns every task it was given, in order.
func (j *JudgeRunner) Tasks() []ports.JudgeTask {
	j.mu.Lock()
	defer j.mu.Unlock()

	return slices.Clone(j.tasks)
}

// RunJudge implements ports.JudgeRunner.
func (j *JudgeRunner) RunJudge(ctx context.Context, t ports.JudgeTask) (ports.JudgeRun, error) {
	j.mu.Lock()
	j.tasks = append(j.tasks, t)
	call := len(j.tasks)
	j.mu.Unlock()

	return j.run(ctx, call, t)
}
