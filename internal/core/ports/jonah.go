package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Judges reads the judges of a definition version (ADR 0025): one per evals/judges/<slug>.md,
// each with its front matter and rubric, for the organization in ctx. A version without judges
// has none; ErrNotFound for a version the organization does not have.
type Judges interface {
	Judges(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID) ([]domain.Judge, error)
}

// FinishedRuns reads the organization's finished production runs, those whose status is no
// longer running, for the organization in ctx. Eval runs are never among them.
type FinishedRuns interface {
	// EndedBetween is every finished run of every factory that ended from since to until, oldest
	// first.
	EndedBetween(ctx context.Context, since, until time.Time) ([]domain.Run, error)
	// Latest is q.Factory's newest finished runs that q matches, newest first, at most q.Limit.
	Latest(ctx context.Context, q FinishedRunQuery) ([]domain.Run, error)
}

// FinishedRunQuery selects a factory's finished runs: those of Agents (empty: every agent), of
// Item when set, that ended by EndedBy when set.
type FinishedRunQuery struct {
	Factory domain.FactoryID
	Agents  []string
	Item    domain.ItemID
	EndedBy time.Time
	Limit   int
}

// JudgeRunner runs one judge run (goal-agent judge) for the organization in ctx: a short run in
// a box that scores Run against Judge's rubric and returns one of its outcomes.
type JudgeRunner interface {
	// RunJudge returns what the judge run answered. An error is an infrastructure failure (the
	// box, the model provider, a reply that did not parse), never a failing score; the JudgeRun
	// returned with it still carries what the run cost and its id when it had one.
	RunJudge(ctx context.Context, t JudgeTask) (JudgeRun, error)
}

// JudgeTask is one judge run to make: Judge of Score's definition version scores Run.
type JudgeTask struct {
	Score domain.JudgeScore
	Judge domain.Judge
	Run   domain.Run
}

// JudgeRun is what a judge run answered: the label of one outcome, a one-sentence reason, what it
// cost and the judge's own run.
type JudgeRun struct {
	Outcome string
	Reason  string
	CostUSD float64
	RunID   domain.RunID
}
