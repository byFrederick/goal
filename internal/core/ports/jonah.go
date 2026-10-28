package ports

import (
	"context"
	"encoding/json"
	"errors"
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

// ErrProposalDecided is DecideProposal's answer for a proposal no longer in state proposed.
var ErrProposalDecided = errors.New("proposal already decided")

// JonahStore keeps a factory's Jonah reviews, proposals and observations (ADR 0034), for the
// organization in ctx; *postgres.Store and *porttest.Store implement it next to ports.Store. At
// most one review is pending or running per factory. A review is claimed under a lease, so any
// instance may run it and a dead one's review is claimed again.
type JonahStore interface {
	// QueueJonahReview queues a pending review. n must pass Check (domain.ErrInvalid);
	// ErrNotFound when the factory, or the version of that factory, does not exist. When the
	// factory has a review pending or running, it returns that review with ErrConflict.
	QueueJonahReview(ctx context.Context, n domain.NewJonahReview) (domain.JonahReview, error)
	// ClaimJonahReview takes the oldest pending review, or running one whose lease ended by now,
	// skipping reviews another transaction holds: running until now+lease, with one more
	// attempt. ErrNotFound when nothing is claimable.
	ClaimJonahReview(ctx context.Context, now time.Time, lease time.Duration) (domain.JonahReview, error)
	// FinishJonahReview ends the claimed review as r says and, in the same transaction, stores
	// its proposals with the factory's next numbers and its observations. claimed is what
	// ClaimJonahReview returned; ErrLeaseLost unless the review still runs under that lease.
	// r must pass Check (domain.ErrInvalid, and nothing is stored). It returns the review and
	// the stored proposals, in r's order.
	FinishJonahReview(ctx context.Context, claimed domain.JonahReview, r domain.JonahReviewResult) (domain.JonahReview, []domain.Proposal, error)
	// JonahReviews is the factory's latest reviews, newest first, at most limit.
	JonahReviews(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.JonahReview, error)
	// Proposals is the factory's proposals that q selects, newest (highest number) first.
	Proposals(ctx context.Context, q ProposalQuery) ([]domain.Proposal, error)
	// Proposal is the factory's proposal of number. ErrNotFound when there is none.
	Proposal(ctx context.Context, factory domain.FactoryID, number int64) (domain.Proposal, error)
	// DecideProposal records d on the factory's proposal of number while it is proposed. d
	// must pass Check; ErrNotFound when there is no such proposal, ErrProposalDecided when it
	// is no longer proposed.
	DecideProposal(ctx context.Context, factory domain.FactoryID, number int64, d domain.ProposalDecision) (domain.Proposal, error)
	// Observations is the factory's latest observations, newest first, at most limit.
	Observations(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.Observation, error)
	// JonahSpendSince sums the cost of the factory's runs of kind jonah started at or after
	// since: the weekly budget's spend (decision 2).
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error)
	// SentBackSince counts the factory's sent-back touches (a person's rejected or
	// gate_rejected) at or after since: the threshold's count (decision 2).
	SentBackSince(ctx context.Context, factory domain.FactoryID, since time.Time) (int, error)
}

// ProposalQuery selects a factory's proposals: in State when set, numbered below Before when
// set (the keyset of the next page), at most Limit.
type ProposalQuery struct {
	Factory domain.FactoryID
	State   domain.ProposalState
	Before  int64
	Limit   int
}
