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
// with it and no connections. Model, when set, replaces the agent's model for the try: a bake-off
// arm's (ADR 0029 decision 2); empty runs the agent on its own model.
type EvalTask struct {
	Run   domain.EvalRun
	Case  domain.EvalCaseSpec
	Try   int
	Judge domain.Judge
	Patch []byte
	Kind  domain.TaskKind
	Model string
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
	// its proposals with the factory's next numbers, sets each proposal a stored one Supersedes
	// superseded by domain.JonahActor while it is still proposed, and stores its observations:
	// one that names an open observation of the factory updates it (its items joined, its count
	// added, its state set when proposed or not Jonah's), any other is inserted. A result of state
	// pending puts the review back in the queue with its error and run, without a lease or a
	// finish time, to be claimed again (ADR 0039 decision 3). claimed is what
	// ClaimJonahReview returned; ErrLeaseLost unless the review still runs under that lease.
	// r must pass Check (domain.ErrInvalid, and nothing is stored). It returns the review and
	// the stored proposals, in r's order.
	FinishJonahReview(ctx context.Context, claimed domain.JonahReview, r domain.JonahReviewResult) (domain.JonahReview, []domain.Proposal, error)
	// LastDoneJonahReview is the factory's newest review that ended done: where the next review's
	// window starts (ADR 0039 decision 3). ErrNotFound when none has.
	LastDoneJonahReview(ctx context.Context, factory domain.FactoryID) (domain.JonahReview, error)
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
	// UndoProposal stores n as a proposal of the factory with its next number, made against
	// version, that reverses the proposal undoes (Undo this change, ADR 0039 decision 15): it has
	// no review. n must pass Check (domain.ErrInvalid); ErrConflict when an undo of that proposal
	// is proposed, published or applied. It supersedes n.Supersedes as FinishJonahReview does.
	UndoProposal(ctx context.Context, factory domain.FactoryID, undoes domain.ProposalID, version domain.DefinitionVersionID, n domain.NewProposal, at time.Time) (domain.Proposal, error)
	// ProposeModel stores n as a proposal of the factory with its next number, made against
	// version, that changes agent's model (Try another model, ADR 0039 decision 13): it has no
	// review. In the same transaction it queues tries, each set to the stored proposal, and
	// supersedes n.Supersedes as FinishJonahReview does. n and every try must pass Check
	// (domain.ErrInvalid, and nothing is stored); ErrNotFound when the factory, the version, an
	// item or a run is not the organization's; ErrConflict for a try of an item and attempt
	// given twice. It returns the proposal and its tries in order.
	ProposeModel(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID, agent string, n domain.NewProposal,
		tries []domain.NewPastTry, at time.Time) (domain.Proposal, []domain.PastTry, error)
	// Observations is the factory's observations, the latest updated first, at most limit.
	Observations(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.Observation, error)
	// OpenObservations is the factory's watching and proposed observations, the latest updated
	// first, at most limit: those a review reads and may update (ADR 0039 decision 5).
	OpenObservations(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.Observation, error)
	// JonahSpendSince sums the cost of the factory's runs started at or after since: LineUSD of
	// every kind but jonah, JonahUSD of kind jonah. Jonah's budget is a share of the first, and
	// the second is what it spent (ADR 0039 decision 16).
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (domain.JonahSpend, error)
	// SentBackBetween is the factory's sent-back touches (a person's rejected or gate_rejected)
	// at or after since and before until, oldest first (by at, then event id), at most limit:
	// a review's events and the evidence it may cite. Each names its item's number and title,
	// the event's data.reason ("" when none) and the agent of the item's last lease span that
	// ended at or before the touch, at any station ("" when none did).
	SentBackBetween(ctx context.Context, factory domain.FactoryID, since, until time.Time, limit int) ([]domain.SentBack, error)
	// StationFlow measures station over [since, until), an estimate's `before` (decision 3).
	// Passed counts the distinct items with a span at the station (any state) that left in the
	// window; SentBack counts the sent-back touches given at the station in the window; its
	// sent-back rate is SentBack over Passed. LeadTimeP85 is the 85th percentile, interpolated
	// between ranks as FlowMetrics does, of the durations of those spans.
	StationFlow(ctx context.Context, factory domain.FactoryID, station string, since, until time.Time) (domain.StationFlow, error)
	// EvidenceItems is the factory's items finished in [since, until), the most recently finished
	// first, at most limit: what a review's evidence reads (ADR 0039 decision 2). A done item
	// finished at its done_at, a cancelled one at its latest cancelled event. Each has its runs,
	// oldest first, and its milestone events in append order: every kind but domain.PerTurnKinds.
	EvidenceItems(ctx context.Context, factory domain.FactoryID, since, until time.Time, limit int) ([]domain.EvidenceItem, error)
	// StationRunMedians is the median cost and turns, interpolated as percentile_cont(0.5) does, of
	// the factory's finished station runs started in [since, until), one per station that has
	// any, sorted by station: what makes a run an outlier (ADR 0039 decision 1).
	StationRunMedians(ctx context.Context, factory domain.FactoryID, since, until time.Time) ([]domain.StationMedian, error)
}

// ProposalQuery selects a factory's proposals: in State when set, numbered below Before when
// set (the keyset of the next page), at most Limit.
type ProposalQuery struct {
	Factory domain.FactoryID
	State   domain.ProposalState
	Before  int64
	Limit   int
}

// PastTries keeps the tries of past items (ADR 0039 decisions 9 and 11), for the organization in
// ctx; *postgres.Store and *porttest.Store implement it next to ports.Store. A try is claimed
// under a lease, so any instance may run it and a dead one's try is claimed again.
type PastTries interface {
	// QueuePastTries queues tries of the factory's proposal, all of them or none: ErrConflict when
	// the proposal has tries already or an item and attempt is given twice, ErrNotFound when the proposal, an item or a run is not the
	// organization's, and domain.ErrInvalid when a try fails Check. It returns them in order.
	QueuePastTries(ctx context.Context, factory domain.FactoryID, proposal domain.ProposalID, tries []domain.NewPastTry, at time.Time) ([]domain.PastTry, error)
	// AddPastTries adds tries to the factory's proposal for Try again (ADR 0039 decision 11), all
	// of them or none, under the proposal's lock: for each add, Tries copies of the item's try with
	// the highest attempt (its station, original run and model), at the attempts after it, so a
	// second call adds the next attempts. It returns the new tries in order. ErrNotFound when the
	// proposal is not the organization's or has no try of an item, ErrProposalDecided when it is
	// not proposed, and domain.ErrInvalid for an add of no tries or an item given twice.
	AddPastTries(ctx context.Context, factory domain.FactoryID, proposal domain.ProposalID, adds []domain.PastTryAdd, at time.Time) ([]domain.PastTry, error)
	// ProposalPastTries is the proposal's tries, oldest first.
	ProposalPastTries(ctx context.Context, proposal domain.ProposalID) ([]domain.PastTry, error)
	// PastTry is one try with its proposal's number. ErrNotFound when there is none.
	PastTry(ctx context.Context, id domain.PastTryID) (domain.PastTry, error)
	// ClaimPastTry takes the factory's oldest queued try, or running one whose lease ended by now,
	// skipping tries another transaction holds, while fewer than limit of the factory's tries run
	// under a lease that has not ended: running until now+lease, with one more attempt.
	// ErrNotFound when nothing is claimable.
	ClaimPastTry(ctx context.Context, factory domain.FactoryID, now time.Time, lease time.Duration, limit int) (domain.PastTry, error)
	// FinishPastTry ends the claimed try done as r says. claimed is what ClaimPastTry returned;
	// ErrLeaseLost unless the try still runs under that lease. r must pass Check.
	FinishPastTry(ctx context.Context, claimed domain.PastTry, r domain.PastTryResult) (domain.PastTry, error)
	// RecordPastTryRun stores the claimed try's run, diff and cost while it still runs, before it
	// is graded (ADR 0039 decision 10): a later claim then grades it without running it again.
	// claimed is what ClaimPastTry returned; ErrLeaseLost unless the try still runs under that
	// lease. domain.ErrInvalid for no run, a negative cost or a diff past domain.MaxPastTryDiff.
	RecordPastTryRun(ctx context.Context, claimed domain.PastTry, run domain.RunID, diff string, cost float64) (domain.PastTry, error)
	// PastTryTarget is the run a try of item at station reruns: the item's newest finished run of
	// kind station there that was a claim's first run (its branch head recorded). ErrNotFound when
	// it has none.
	PastTryTarget(ctx context.Context, item domain.ItemID, station string) (domain.Run, error)
	// AgentDoneItems are the factory's done items that agent has a finished run of kind station
	// of that was a claim's first (its branch head recorded), the most recently done first, at
	// most limit: what Try another model tries (ADR 0039 decision 13). Each has that newest run,
	// the stations of its rejected and gate_rejected events, oldest first, and the item's cost.
	AgentDoneItems(ctx context.Context, factory domain.FactoryID, agent string, limit int) ([]domain.AgentItem, error)
	// PastTryInput is what run recorded, with its input and output, and its item's latest output
	// of each station among the runs that ended before run started. ErrNotFound when there is no
	// such run.
	PastTryInput(ctx context.Context, run domain.RunID) (domain.PastTryInput, error)
	// RecordGraderCheck stores one check of the grader on a tried item's result (ADR 0039 decision
	// 10), of the proposal's factory. n must pass Check (domain.ErrInvalid); ErrNotFound when the
	// proposal is not the organization's or the item not its factory's, ErrConflict when the
	// proposal has a check of that item and result already.
	RecordGraderCheck(ctx context.Context, n domain.NewGraderCheck) (domain.GraderCheck, error)
	// ProposalGraderChecks is the proposal's checks, oldest first.
	ProposalGraderChecks(ctx context.Context, proposal domain.ProposalID) ([]domain.GraderCheck, error)
	// GraderHitRates is the proposal's two hit rates (domain.HitRatesOf of its checks); zero when
	// it has none.
	GraderHitRates(ctx context.Context, proposal domain.ProposalID) (domain.GraderHitRates, error)
}

// KeptTests keeps the past items people kept as tests (ADR 0039 decision 12), for the organization
// in ctx; *postgres.Store and *porttest.Store implement it next to ports.Store. A test goes with
// its item and its factory: both purges remove it.
type KeptTests interface {
	// KeepTest stores n with its item's number and title. n must pass Check (domain.ErrInvalid);
	// ErrNotFound when the item is not the organization's or not the factory's, ErrConflict when
	// the item is kept at that station already.
	KeepTest(ctx context.Context, n domain.NewKeptTest) (domain.KeptTest, error)
	// KeptTests is the factory's kept tests, only station's when station is set, newest first.
	KeptTests(ctx context.Context, factory domain.FactoryID, station string) ([]domain.KeptTest, error)
	// RemoveKeptTest removes the factory's kept test id. ErrNotFound when the factory has none.
	RemoveKeptTest(ctx context.Context, factory domain.FactoryID, id domain.KeptTestID) error
}
