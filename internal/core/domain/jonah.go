package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Refusals of a decision on a proposal and of a manual review (ADR 0034 decisions 2, 7 and 8).
var (
	// ErrProposalDraftOpen refuses Approve while the factory has an open draft.
	ErrProposalDraftOpen = errors.New("the factory has an open draft: publish or discard it first, or edit the proposal into it")
	// ErrProposalStale refuses Approve when a file the proposal edits changed since its review.
	ErrProposalStale = errors.New("a file the proposal edits changed since it was made")
	// ErrProposalDraftConflict refuses Edit when the draft's copy of a file is not the proposal's base.
	ErrProposalDraftConflict = errors.New("the draft's copy of a file the proposal edits is not the one it was made against")
	// ErrReviewInFlight refuses a manual review while one is pending or running.
	ErrReviewInFlight = errors.New("a review of this factory is pending or running")
	// ErrLearningBudgetExhausted refuses a review while the last 7 days' review runs cost
	// learning.jonah.weeklyUsd or more.
	ErrLearningBudgetExhausted = errors.New("this factory's review runs used their weekly budget")
)

// Audit actions of Jonah's decisions.
const (
	AuditProposalPublished = "learning_proposal.published"
	AuditProposalApplied   = "learning_proposal.applied"
	AuditProposalRejected  = "learning_proposal.rejected"
	AuditReviewQueued      = "jonah_review.queued"
)

// Audit target kinds of Jonah's entries.
const (
	AuditTargetProposal    = "learning_proposal"
	AuditTargetJonahReview = "jonah_review"
)

// RunKind is whose run an agent run is: a line's, at a station, or a Jonah review's (ADR 0034).
type RunKind string

// Run kinds. A run with no kind is a station run.
const (
	RunKindStation RunKind = "station"
	RunKindJonah   RunKind = "jonah"
)

// JonahReviewID identifies one Jonah review.
type JonahReviewID string

// ProposalID identifies one proposal; people name it by its number in the factory.
type ProposalID string

// ObservationID identifies one observation.
type ObservationID string

// JonahTrigger is why a review was queued: a person asked, or the factory's sent-back touches
// reached learning.jonah.proposeAfter.
type JonahTrigger string

// Review triggers.
const (
	JonahManual    JonahTrigger = "manual"
	JonahThreshold JonahTrigger = "threshold"
)

// JonahReviewState is where a review is: pending until claimed, running under a lease, then
// done or failed.
type JonahReviewState string

// Review states.
const (
	JonahPending JonahReviewState = "pending"
	JonahRunning JonahReviewState = "running"
	JonahDone    JonahReviewState = "done"
	JonahFailed  JonahReviewState = "failed"
)

// JonahReview is one review of a factory's sent-back touches from WindowStart to WindowEnd, on
// the definition version VersionID (ADR 0034 decision 1). LeaseUntil is set while it runs;
// Attempts counts its claims. CreatedBy is the Actor.Ref() that queued it.
type JonahReview struct {
	ID          JonahReviewID
	FactoryID   FactoryID
	VersionID   DefinitionVersionID
	Trigger     JonahTrigger
	State       JonahReviewState
	LeaseUntil  time.Time
	Attempts    int
	RunID       RunID
	WindowStart time.Time
	WindowEnd   time.Time
	Error       string
	CreatedBy   string
	CreatedAt   time.Time
	FinishedAt  time.Time
}

// NewJonahReview is a review to queue, by By at At.
type NewJonahReview struct {
	FactoryID   FactoryID
	VersionID   DefinitionVersionID
	Trigger     JonahTrigger
	WindowStart time.Time
	WindowEnd   time.Time
	By          Actor
	At          time.Time
}

// Check reports what makes n impossible to queue, wrapping ErrInvalid.
func (n NewJonahReview) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("jonah review: "+format+": %w", append(args, ErrInvalid)...))
	}

	if n.FactoryID == "" || n.VersionID == "" {
		bad("no factory or version")
	}

	if n.Trigger != JonahManual && n.Trigger != JonahThreshold {
		bad("trigger %q is not manual or threshold", n.Trigger)
	}

	if n.WindowEnd.Before(n.WindowStart) {
		bad("window ends before it starts")
	}

	if n.By.Kind == "" || n.By.ID == "" {
		bad("no actor")
	}

	if n.At.IsZero() {
		bad("no time")
	}

	return errors.Join(errs...)
}

// JonahReviewResult is how a claimed review ended: done with its proposals and observations, or
// failed with Error. RunID is the review's agent run, when it had one.
type JonahReviewResult struct {
	State        JonahReviewState
	Error        string
	RunID        RunID
	Proposals    []NewProposal
	Observations []NewObservation
	At           time.Time
}

// Check reports what makes r impossible to record, wrapping ErrInvalid.
func (r JonahReviewResult) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("jonah review result: "+format+": %w", append(args, ErrInvalid)...))
	}

	if r.State != JonahDone && r.State != JonahFailed {
		bad("state %q is not done or failed", r.State)
	}

	if utf8.RuneCountInString(r.Error) > maxJonahError {
		bad("error longer than %d characters", maxJonahError)
	}

	if r.At.IsZero() {
		bad("no time")
	}

	for i, p := range r.Proposals {
		if err := p.Check(); err != nil {
			errs = append(errs, fmt.Errorf("proposal %d: %w", i+1, err))
		}
	}

	for i, o := range r.Observations {
		if err := o.Check(); err != nil {
			errs = append(errs, fmt.Errorf("observation %d: %w", i+1, err))
		}

		if o.Proposal < 0 || o.Proposal > len(r.Proposals) {
			bad("observation %d names proposal %d of %d", i+1, o.Proposal, len(r.Proposals))
		}
	}

	return errors.Join(errs...)
}

// ProposalState is where a proposal is (ADR 0034 decision 6): proposed until a person approves
// it (published), edits it into the draft (applied) or rejects it, or a later change supersedes
// it.
type ProposalState string

// Proposal states.
const (
	ProposalProposed   ProposalState = "proposed"
	ProposalPublished  ProposalState = "published"
	ProposalApplied    ProposalState = "applied"
	ProposalRejected   ProposalState = "rejected"
	ProposalSuperseded ProposalState = "superseded"
)

// ProposalStates are every proposal state, proposed first.
var ProposalStates = []ProposalState{ProposalProposed, ProposalPublished, ProposalApplied, ProposalRejected, ProposalSuperseded}

// Valid reports whether s is a proposal state.
func (s ProposalState) Valid() bool { return slices.Contains(ProposalStates, s) }

// Limits of a proposal, as 00075's CHECKs hold them.
const (
	// MaxProposalFilesBytes is the most the contents of a proposal's files may total: 256 KiB.
	MaxProposalFilesBytes = 256 << 10
	maxProposalTitle      = 200
	maxProposalSummary    = 4000
	maxProposalEvidence   = 50
	maxEvidenceQuote      = 500
	maxObservationSummary = 500
	maxObservationDetail  = 2000
	maxObservationItems   = 1000
	maxJonahError         = 2000
	maxRejectReason       = 2000
)

// proposalPathPattern is the allowlist of the paths a proposal may edit (ADR 0034 decision 4):
// never evals/, which only a person changes.
var proposalPathPattern = regexp.MustCompile(`^(factory\.yaml|agents/.+|skills/.+|rules/.+|contracts/.+)$`)

// ProposalPathAllowed reports whether a proposal may edit path: one under the allowlist, without
// a . or .. segment or a control character. 00075's goal_jonah_paths_allowed checks the same.
func ProposalPathAllowed(path string) bool {
	if !proposalPathPattern.MatchString(path) || strings.ContainsFunc(path, unicode.IsControl) {
		return false
	}

	for seg := range strings.SplitSeq(path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}

	return true
}

// ProposalFile is one file a proposal writes: its path, the SHA-256 of the file in the version
// the review read (empty for a new file) and its new content.
type ProposalFile struct {
	Path       string `json:"path"`
	BaseSHA256 string `json:"base_sha256"`
	Content    string `json:"content"`
}

// ProposalEvidence is one sent-back event a proposal rests on, with its quoted reason.
type ProposalEvidence struct {
	ItemID  ItemID `json:"item_id"`
	EventID int64  `json:"event_id,omitempty"`
	Station string `json:"station,omitempty"`
	Quote   string `json:"quote"`
}

// ProposalMetric is what a proposal should move.
type ProposalMetric string

// Proposal metrics.
const (
	MetricSentBackRate ProposalMetric = "sent_back_rate"
	MetricLeadTimeP85  ProposalMetric = "lead_time_p85"
)

// ProposalEstimate is the metric a proposal should move at Station: Before is what goald
// computed from the recorded events, After the model's estimate, and CostPerItemUSD the change in
// cost per item. A proposal without an estimate has no Metric.
type ProposalEstimate struct {
	Metric         ProposalMetric `json:"metric,omitempty"`
	Station        string         `json:"station,omitempty"`
	Before         float64        `json:"before"`
	After          float64        `json:"after"`
	CostPerItemUSD float64        `json:"cost_per_item_usd"`
}

// Proposal is one hypothesis of a review: a set of file edits to the version it was made
// against, numbered per factory from 1.
type Proposal struct {
	ID        ProposalID
	FactoryID FactoryID
	Number    int64
	ReviewID  JonahReviewID
	VersionID DefinitionVersionID
	State     ProposalState
	Title     string
	Summary   string
	Files     []ProposalFile
	Evidence  []ProposalEvidence
	Estimate  ProposalEstimate
	// PublishedVersionID is the version Approve published.
	PublishedVersionID DefinitionVersionID
	RejectReason       string
	DecidedBy          string
	DecidedAt          time.Time
	CreatedAt          time.Time
}

// Paths are the paths of p's files, in order.
func (p Proposal) Paths() []string { return filePaths(p.Files) }

// NewProposal is a proposal a review outputs.
type NewProposal struct {
	Title    string
	Summary  string
	Files    []ProposalFile
	Evidence []ProposalEvidence
	Estimate ProposalEstimate
}

// Paths are the paths of n's files, in order.
func (n NewProposal) Paths() []string { return filePaths(n.Files) }

func filePaths(files []ProposalFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}

	return out
}

// Check reports what makes n impossible to store, wrapping ErrInvalid: a path outside the
// allowlist among them.
func (n NewProposal) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("proposal: "+format+": %w", append(args, ErrInvalid)...))
	}

	if l := utf8.RuneCountInString(n.Title); l == 0 || l > maxProposalTitle || strings.TrimSpace(n.Title) == "" {
		bad("title must have 1 to %d characters", maxProposalTitle)
	}

	if utf8.RuneCountInString(n.Summary) > maxProposalSummary {
		bad("summary longer than %d characters", maxProposalSummary)
	}

	checkProposalFiles(n.Files, bad)

	if len(n.Evidence) > maxProposalEvidence {
		bad("more than %d pieces of evidence", maxProposalEvidence)
	}

	for _, e := range n.Evidence {
		if utf8.RuneCountInString(e.Quote) > maxEvidenceQuote || utf8.RuneCountInString(e.Station) > maxEvidenceQuote {
			bad("evidence of %s longer than %d characters", e.ItemID, maxEvidenceQuote)
		}
	}

	switch n.Estimate.Metric {
	case "", MetricSentBackRate, MetricLeadTimeP85:
	default:
		bad("estimate metric %q is not sent_back_rate or lead_time_p85", n.Estimate.Metric)
	}

	if utf8.RuneCountInString(n.Estimate.Station) > 100 {
		bad("estimate station longer than 100 characters")
	}

	return errors.Join(errs...)
}

// checkProposalFiles reports to bad what makes files impossible to store: none, a path outside
// the allowlist or twice, a malformed base_sha256, content that is not text, or too many bytes.
func checkProposalFiles(files []ProposalFile, bad func(format string, args ...any)) {
	if len(files) == 0 {
		bad("no files")
	}

	seen := map[string]bool{}
	total := 0

	for _, f := range files {
		switch {
		case !ProposalPathAllowed(f.Path):
			bad("path %q is not one a proposal may edit", f.Path)
		case seen[f.Path]:
			bad("path %q twice", f.Path)
		}

		seen[f.Path] = true

		if f.BaseSHA256 != "" && !BlobHash(f.BaseSHA256).Valid() {
			bad("base_sha256 of %s is not 64 lowercase hex", f.Path)
		}

		if !utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0) {
			bad("content of %s is not UTF-8 text", f.Path)
		}

		total += len(f.Content)
	}

	if total > MaxProposalFilesBytes {
		bad("files total %d bytes, more than %d", total, MaxProposalFilesBytes)
	}
}

// ProposalDecision is a person's (or a later change's) decision on a proposed proposal: published
// as PublishedVersionID, applied to the draft, rejected with RejectReason, or superseded.
type ProposalDecision struct {
	State              ProposalState
	PublishedVersionID DefinitionVersionID
	RejectReason       string
	By                 Actor
	At                 time.Time
}

// Check reports what makes d impossible to record, wrapping ErrInvalid.
func (d ProposalDecision) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("proposal decision: "+format+": %w", append(args, ErrInvalid)...))
	}

	if !d.State.Valid() || d.State == ProposalProposed {
		bad("state %q is not a decision", d.State)
	}

	if d.State == ProposalRejected && strings.TrimSpace(d.RejectReason) == "" {
		bad("a rejection needs a reason")
	}

	if utf8.RuneCountInString(d.RejectReason) > maxRejectReason {
		bad("reason longer than %d characters", maxRejectReason)
	}

	if d.By.Kind == "" || d.By.ID == "" {
		bad("no actor")
	}

	if d.At.IsZero() {
		bad("no time")
	}

	return errors.Join(errs...)
}

// ObservationState is whether an observation became a proposal or is still watched until its
// count reaches learning.jonah.proposeAfter ("Watching, 3 of 5").
type ObservationState string

// Observation states.
const (
	ObservationWatching ObservationState = "watching"
	ObservationProposed ObservationState = "proposed"
)

// Observation is a pattern a review noticed: a summary, a detail line, the items it rests on and
// how often it was seen, and the proposal it became once proposed.
type Observation struct {
	ID         ObservationID
	FactoryID  FactoryID
	ReviewID   JonahReviewID
	Summary    string
	Detail     string
	ItemIDs    []ItemID
	Count      int
	State      ObservationState
	ProposalID ProposalID
	CreatedAt  time.Time
}

// NewObservation is an observation a review outputs. Proposal is the proposal of the same result
// it became, counted from 1; 0 while it is watched.
type NewObservation struct {
	Summary  string
	Detail   string
	ItemIDs  []ItemID
	Count    int
	Proposal int
}

// Check reports what makes n impossible to store, wrapping ErrInvalid.
func (n NewObservation) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("observation: "+format+": %w", append(args, ErrInvalid)...))
	}

	if l := utf8.RuneCountInString(n.Summary); l == 0 || l > maxObservationSummary {
		bad("summary must have 1 to %d characters", maxObservationSummary)
	}

	if utf8.RuneCountInString(n.Detail) > maxObservationDetail {
		bad("detail longer than %d characters", maxObservationDetail)
	}

	if len(n.ItemIDs) > maxObservationItems {
		bad("more than %d items", maxObservationItems)
	}

	if n.Count < 0 {
		bad("negative count")
	}

	return errors.Join(errs...)
}
