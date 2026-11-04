package ports

import (
	"context"
	"errors"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

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
	// QueuePastTries queues tries of the factory's proposal, all of them or none, each set to the
	// proposal: of items it cites, and of kept tests with the content key of what they run on.
	// ErrConflict when the proposal has tries already or an item, kept test and attempt is given
	// twice, ErrNotFound when the proposal, an item, a run or a kept test is not the organization's,
	// and domain.ErrInvalid when a try fails Check. It returns them in order.
	QueuePastTries(ctx context.Context, factory domain.FactoryID, proposal domain.ProposalID, tries []domain.NewPastTry, at time.Time) ([]domain.PastTry, error)
	// QueuePublishTries queues a publish's tries of the factory's kept tests (ADR 0039 decision
	// 12): with no proposal, each running on version with edits, the open draft as whole files.
	// Every try names its kept test and content key. A try whose kept test, content key and attempt
	// a publish's try has already is skipped, so queuing is idempotent; the rest are queued all or
	// none. ErrNotFound when an item, a run or a kept test is not the organization's,
	// domain.ErrInvalid when a try fails Check or names a proposal. It returns those it queued, in
	// order.
	QueuePublishTries(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID, edits []domain.ProposalEdit,
		tries []domain.NewPastTry, at time.Time) ([]domain.PastTry, error)
	// KeptTestTries is the factory's kept-test tries whose content key is key, from any proposal
	// or publish, oldest first: what a publish of that content reuses.
	KeptTestTries(ctx context.Context, factory domain.FactoryID, key string) ([]domain.PastTry, error)
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
	// a publish's (with no proposal) before any proposal's, skipping tries another transaction holds, while fewer than limit of the factory's tries run
	// under a lease that has not ended: running until now+lease, with one more attempt.
	// ErrNotFound when nothing is claimable.
	ClaimPastTry(ctx context.Context, factory domain.FactoryID, now time.Time, lease time.Duration, limit int) (domain.PastTry, error)
	// FinishPastTry ends the claimed try done as r says. claimed is what ClaimPastTry returned;
	// ErrLeaseLost unless the try still runs under that lease. r must pass Check.
	FinishPastTry(ctx context.Context, claimed domain.PastTry, r domain.PastTryResult) (domain.PastTry, error)
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
	// KeptTests is the factory's kept tests, only station's when station is set, newest first,
	// each with its LastResult: its newest finished try of any source, nil when it has none.
	KeptTests(ctx context.Context, factory domain.FactoryID, station string) ([]domain.KeptTest, error)
	// RemoveKeptTest removes the factory's kept test id. ErrNotFound when the factory has none.
	RemoveKeptTest(ctx context.Context, factory domain.FactoryID, id domain.KeptTestID) error
}
