package ports

import (
	"context"
	"encoding/json"
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

// EvalSuites reads the cases an eval run runs (ADR 0025 decision 4), for the organization in ctx:
// each evals/cases/<name>.yaml of the run's content, resolved with its judge or its reference
// patch. It reads the content the run names, a published version's or a draft's by its content
// hash, so a draft's run reads the same files the publish gate compares.
type EvalSuites interface {
	// EvalSuite is every case of run's content, by name. ErrNotFound when the content is gone.
	EvalSuite(ctx context.Context, run domain.EvalRun) ([]EvalSuiteCase, error)
}

// EvalSuiteCase is one case of an eval run's content and what its check needs: the judge it
// names, or the bytes of its reference patch.
type EvalSuiteCase struct {
	Case  domain.EvalCaseSpec
	Judge domain.Judge
	Patch []byte
}

// EvalRunner runs one try of an eval case for the organization in ctx: an ordinary agent run of
// the case's agent, with its prompt, skills, tools and contract, pinned to the eval run's content,
// that starts from the case's input in a box. The box clones runtime.repository on a throwaway
// branch at the input's ref; the agent gets the model connection only, and the task token is of
// Kind eval and names no connection, so the gateway serves it only the knowledge tools and the try
// never pushes, comments or calls an outside API. For a case checked by a judge, the runner also
// makes the judge run (goal-agent judge) on the try's output and returns its answer.
type EvalRunner interface {
	// RunEvalTry returns how the try's run ended. An error means only an infrastructure failure
	// (the box, the model provider, the gateway), never a failing try: an agent run that failed or
	// gave no valid output is an EvalTry with Failure set. The EvalTry returned with an error still
	// carries what the try cost and its run when it had one.
	RunEvalTry(ctx context.Context, t EvalTask) (EvalTry, error)
}

// EvalTask is one try to run: try Try (from 1) of Case in Run, with the judge or reference patch
// its check names. Kind is always domain.TaskKindEval, and the runner signs the try's task token
// with it and no connections.
type EvalTask struct {
	Run   domain.EvalRun
	Case  domain.EvalCaseSpec
	Try   int
	Judge domain.Judge
	Patch []byte
	Kind  domain.TaskKind
}

// EvalTry is how one try's run ended.
type EvalTry struct {
	// Failure is why the agent run ended without a valid output (it failed, ran out of turns or
	// answered nothing that parsed); empty when it gave one.
	Failure string
	// Output is the agent's final JSON output.
	Output json.RawMessage
	// ContractProblem is what makes Output break the agent's contract; empty means it satisfies it.
	ContractProblem string
	// ChangedFiles are the paths the try's change touches against the ref it started from.
	ChangedFiles []string
	// Checks are the factory's checks run on the try's change, with their exit codes.
	Checks []EvalCheck
	// Outcome and Reason are the judge's answer, for a case checked by a judge.
	Outcome string
	Reason  string
	CostUSD float64
	Turns   int
	RunID   domain.RunID
}

// EvalCheck is one check run on an eval try's change and its exit code.
type EvalCheck struct {
	Command  string
	ExitCode int
}
