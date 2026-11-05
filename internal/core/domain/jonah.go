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
	// ErrProposalStale refuses Approve when an edit no longer applies to the current version: a
	// replacement's old text no longer occurs exactly once (ADR 0039 decision 6).
	ErrProposalStale = errors.New("a file the proposal edits changed so that its edits no longer apply")
	// ErrProposalDraftConflict refuses Edit when an edit no longer applies to the draft's copy of
	// its file.
	ErrProposalDraftConflict = errors.New("the draft's copy of a file the proposal edits changed so that its edits no longer apply")
	// ErrReviewInFlight refuses a manual review while one is pending or running.
	ErrReviewInFlight = errors.New("a review of this factory is pending or running")
	// ErrProposalNotPublished refuses Undo this change for a proposal that was never published.
	ErrProposalNotPublished = errors.New("the proposal was not published, so there is no change to undo")
	// ErrUndoConflict refuses Undo this change when a file changed since so that the change no
	// longer reverses: a reversed replacement's old text does not occur exactly once.
	ErrUndoConflict = errors.New("a file the change edited changed since, so the change does not reverse")
	// ErrLearningBudgetExhausted refuses a review while the last 7 days' review runs cost
	// learning.jonah.weeklyUsd or more.
	ErrLearningBudgetExhausted = errors.New("this factory's review runs used their weekly budget")
)

// Refusals of Try another model (ADR 0039 decision 13).
var (
	// ErrModelNotAllowed refuses a model the organization's policy does not allow, or the
	// agent's model already.
	ErrModelNotAllowed = errors.New("the model is not one the organization allows, or is the agent's model already")
	// ErrAgentNotFound refuses an agent the current version does not have.
	ErrAgentNotFound = errors.New("the current version has no such agent")
	// ErrAgentNotTriable refuses an agent that needs MCP servers or secrets to do its work, so
	// it cannot be tried on past items.
	ErrAgentNotTriable = errors.New("the agent needs MCP servers or secrets, so it can't be tried on past items")
	// ErrNoFinishedItems refuses an agent with no finished item whose first run of a claim can
	// be rerun.
	ErrNoFinishedItems = errors.New("the agent has no finished item to try the model on")
)

// Refusals of Try again (ADR 0039 decision 11).
var (
	// ErrProposalNotTriable refuses a proposal that reaches no station that can be tried, as a
	// memory-only or flow-only one.
	ErrProposalNotTriable = errors.New("the proposal changes no station that can be tried on past items")
	// ErrProposalNotTried refuses a proposal that can be tried but has no tries yet: its first
	// ones are queued by Jonah's sweep.
	ErrProposalNotTried = errors.New("the proposal has not been tried yet, so there is nothing to try again")
)

// Audit actions of Jonah's decisions.
const (
	AuditProposalPublished = "learning_proposal.published"
	AuditProposalApplied   = "learning_proposal.applied"
	AuditProposalRejected  = "learning_proposal.rejected"
	AuditReviewQueued      = "jonah_review.queued"
	AuditProposalUndo      = "learning_proposal.undo_proposed"
	AuditProposalModel     = "learning_proposal.model_proposed"
	AuditProposalTried     = "learning_proposal.tried_again"
)

// Audit target kinds of Jonah's entries.
const (
	AuditTargetProposal    = "learning_proposal"
	AuditTargetJonahReview = "jonah_review"
)

// RunKind is whose run an agent run is: a line's, at a station, a Jonah review's (ADR 0034), a
// try of a past item (ADR 0039 decision 9) or a trigger's check (ADR 0040 decision 19).
type RunKind string

// Run kinds. A run with no kind is a station run.
const (
	RunKindStation RunKind = "station"
	RunKindJonah   RunKind = "jonah"
	RunKindTry     RunKind = "try"
	RunKindCheck   RunKind = "check"
)

// Itemless reports whether a run of kind k works no item: Jonah's runs and checks.
func (k RunKind) Itemless() bool { return k.Jonahs() || k == RunKindCheck }

// Jonahs reports whether a run of kind k is Jonah's: a review or a try. Jonah's runs work no item
// and are paid from Jonah's share, never the line's spend (ADR 0039 decision 16).
func (k RunKind) Jonahs() bool { return k == RunKindJonah || k == RunKindTry }

// JonahActor is Jonah as the actor of what goald does for it: the daily reviews it queues and
// the proposals its new ones supersede.
var JonahActor = Actor{Kind: ActorSystem, ID: "jonah", Name: "Jonah"}

// JonahReviewID identifies one Jonah review.
type JonahReviewID string

// ProposalID identifies one proposal; people name it by its number in the factory.
type ProposalID string

// ObservationID identifies one observation.
type ObservationID string

// JonahBudgetWindow is how far back a factory's Jonah budget counts the line's spend and Jonah's
// (ADR 0039 decision 16).
const JonahBudgetWindow = 7 * 24 * time.Hour

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

// JonahReviewResult is how a claimed review ended: done with its proposals and observations,
// failed with Error, or pending again with Error to be retried (ADR 0039 decision 3), storing
// nothing. RunID is the review's agent run, when it had one.
type JonahReviewResult struct {
	State        JonahReviewState
	Error        string
	RunID        RunID
	Proposals    []NewProposal
	Observations []NewObservation
	At           time.Time
}

// stateProblem is what makes r's state impossible: one that is not done, failed or pending, or a
// review to retry that stores something. "" when there is none.
func (r JonahReviewResult) stateProblem() string {
	switch {
	case r.State != JonahDone && r.State != JonahFailed && r.State != JonahPending:
		return fmt.Sprintf("state %q is not done, failed or pending", r.State)
	case r.State == JonahPending && (len(r.Proposals) > 0 || len(r.Observations) > 0):
		return "a review to retry stores no proposal or observation"
	default:
		return ""
	}
}

// Check reports what makes r impossible to record, wrapping ErrInvalid.
func (r JonahReviewResult) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("jonah review result: "+format+": %w", append(args, ErrInvalid)...))
	}

	if reason := r.stateProblem(); reason != "" {
		bad("%s", reason)
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

// Limits of a proposal, as 00078's and 00101's CHECKs hold them.
const (
	// MaxProposalFilesBytes is the most the text of a proposal's edits may total: 256 KiB.
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

// proposalPathPattern is the positive allowlist of the paths a proposal may edit (ADR 0039
// decisions 6 and 7): factory.yaml, whose paths goald checks apart, memory.yaml, which only goald
// writes from the facts it verified, and the .md, .yaml and .json files under agents/, skills/,
// rules/ and contracts/. Never evals/, a script or any other file.
var proposalPathPattern = regexp.MustCompile(`^(factory\.yaml|memory\.yaml|(agents|skills|rules|contracts)/.+\.(md|yaml|json))$`)

// ProposalPathAllowed reports whether a proposal may edit path: one under the allowlist, without
// a . or .. segment or a control character. 00103's goal_jonah_paths_allowed checks the same.
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

// MemoryProposalPath is the factory's memory (ADR 0038 decision 6). A proposal may edit it, but
// only as the memory proposal goald writes from the facts it checked against people's words (ADR
// 0039 decision 7), never as a review's definition edit.
const MemoryProposalPath = "memory.yaml"

// DefinitionEditAllowed reports whether a review's definition edit may change path: one
// ProposalPathAllowed accepts, but not memory.yaml.
func DefinitionEditAllowed(path string) bool {
	return path != MemoryProposalPath && ProposalPathAllowed(path)
}

// PersonWords is the text a person wrote in e that a memory fact may quote (ADR 0039 decision 7),
// and whether e has one: a note's text, an answer to a question, or the reason of a reject, a
// gate's reject or a move, when data.actor is a person. An agent's or a check's event has none.
func PersonWords(e Event) (string, bool) {
	key, ok := personWordKeys[e.Kind]
	if !ok || !byPerson(e) {
		return "", false
	}

	text := dataText(e, key)

	return text, text != ""
}

// personWordKeys are the event data keys of a person's words, by the kinds of event that hold them.
var personWordKeys = map[EventKind]string{
	EventNote: "text", EventQuestionAnswered: "answer", EventRejected: evKeyReason, EventGateRejected: evKeyReason, EventMoved: evKeyReason,
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

// SentBack is one sent-back touch a review reads: a person's rejected or gate_rejected on an
// item at Station, with the reason the event quoted ("" when none) and Agent, the agent of the
// item's last lease that ended by then ("" when none had).
type SentBack struct {
	EventID    int64
	ItemID     ItemID
	ItemNumber int64
	ItemTitle  string
	Station    string
	Agent      string
	Kind       EventKind
	Reason     string
	At         time.Time
}

// StationFlow is one station's flow over a window, the `before` of a proposal's estimate:
// Passed counts the items with a span at the station that left in it, SentBack the sent-back
// touches given there in it, and LeadTimeP85 is the p85 of those spans' durations.
type StationFlow struct {
	Passed      int
	SentBack    int
	LeadTimeP85 time.Duration
}

// SentBackRate is SentBack over Passed; false when no item passed.
func (f StationFlow) SentBackRate() (float64, bool) {
	if f.Passed == 0 {
		return 0, false
	}

	return float64(f.SentBack) / float64(f.Passed), true
}

// Proposal is one hypothesis of a review: a set of exact edits to the definition, made against
// a version and applied again to the version it is approved or edited into, numbered per factory
// from 1.
type Proposal struct {
	ID        ProposalID
	FactoryID FactoryID
	Number    int64
	ReviewID  JonahReviewID
	VersionID DefinitionVersionID
	State     ProposalState
	Title     string
	Summary   string
	Edits     []ProposalEdit
	Evidence  []ProposalEvidence
	Estimate  ProposalEstimate
	// Undoes is the proposal this one reverses (Undo this change, ADR 0039 decision 15); it has
	// no review then.
	Undoes ProposalID
	// Agent is the agent whose model a person's Try another model changes (ADR 0039 decision
	// 13); it has no review then.
	Agent string
	// PublishedVersionID is the version Approve published.
	PublishedVersionID DefinitionVersionID
	RejectReason       string
	DecidedBy          string
	DecidedAt          time.Time
	CreatedAt          time.Time
}

// Paths are the paths of p's edits, in order.
func (p Proposal) Paths() []string { return editPaths(p.Edits) }

// NewProposal is a proposal a review outputs. Supersedes are the open proposals whose edits
// overlap its own (EditsOverlap): storing it sets each that is still proposed superseded.
type NewProposal struct {
	Title      string
	Summary    string
	Edits      []ProposalEdit
	Evidence   []ProposalEvidence
	Estimate   ProposalEstimate
	Supersedes []ProposalID
}

// Paths are the paths of n's edits, in order.
func (n NewProposal) Paths() []string { return editPaths(n.Edits) }

func editPaths(edits []ProposalEdit) []string {
	out := make([]string, len(edits))
	for i, e := range edits {
		out[i] = e.Path
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

	checkProposalEdits(n.Edits, bad)

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
	case MetricCostPerItem:
		bad("estimate metric %q is a watch's, not a proposal's", n.Estimate.Metric)
	default:
		bad("estimate metric %q is not sent_back_rate or lead_time_p85", n.Estimate.Metric)
	}

	if utf8.RuneCountInString(n.Estimate.Station) > 100 {
		bad("estimate station longer than 100 characters")
	}

	return errors.Join(errs...)
}

// checkProposalEdits reports to bad what makes edits impossible to store: none, a path outside
// the allowlist or twice, a replacement with an empty old, a base_sha256 on replacements or a
// malformed one, text that is not UTF-8, or too many bytes.
func checkProposalEdits(edits []ProposalEdit, bad func(format string, args ...any)) {
	if len(edits) == 0 {
		bad("no edits")
	}

	seen := map[string]bool{}
	total := 0

	for _, e := range edits {
		checkProposalEdit(e, seen, bad)

		seen[e.Path] = true
		total += e.size()
	}

	if total > MaxProposalFilesBytes {
		bad("edits total %d bytes, more than %d", total, MaxProposalFilesBytes)
	}
}

// checkProposalEdit reports to bad what makes e impossible to store, seen holding the paths of
// the edits before it.
func checkProposalEdit(e ProposalEdit, seen map[string]bool, bad func(format string, args ...any)) {
	switch {
	case !ProposalPathAllowed(e.Path):
		bad("path %q is not one a proposal may edit", e.Path)
	case seen[e.Path]:
		bad("path %q twice", e.Path)
	}

	for _, r := range e.Replacements {
		if r.Old == "" {
			bad("a replacement of %s has no old text", e.Path)
		}
	}

	switch {
	case !e.Whole() && (e.Content != "" || e.BaseSHA256 != ""):
		bad("an edit of %s has replacements and whole content", e.Path)
	case e.BaseSHA256 != "" && !BlobHash(e.BaseSHA256).Valid():
		bad("base_sha256 of %s is not 64 lowercase hex", e.Path)
	}

	if slices.ContainsFunc(e.texts(), func(s string) bool { return !utf8.ValidString(s) || strings.ContainsRune(s, 0) }) {
		bad("an edit of %s is not UTF-8 text", e.Path)
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

// ObservationState is where an observation is (ADR 0039 decision 5): proposed, when a proposal
// addresses it; watching, until its count reaches learning.jonah.proposeAfter ("Watching, 3 of
// 5"); or not Jonah's, when its cause is not a definition problem.
type ObservationState string

// Observation states.
const (
	ObservationWatching  ObservationState = "watching"
	ObservationProposed  ObservationState = "proposed"
	ObservationNotJonahs ObservationState = "not_jonahs"
)

// Open reports whether a review still updates an observation in s: watching or proposed.
func (s ObservationState) Open() bool { return s == ObservationWatching || s == ObservationProposed }

// Observation is a cause reviews noticed: a summary, a detail line, the items it rests on and a
// count that grows across reviews, and the proposal it became once proposed. ReviewID is the
// review that first noticed it; UpdatedAt the last review that updated it.
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
	UpdatedAt  time.Time
}

// NewObservation is an observation a review outputs. ID, when set, is the open observation it
// updates: its items join that one's, Count adds to its count and its state is set again; without
// an ID it is new. Proposal is the proposal of the same result it became, counted from 1; 0 while
// it is watched. NotJonahs marks a cause that is not a definition problem.
type NewObservation struct {
	ID        ObservationID
	Summary   string
	Detail    string
	ItemIDs   []ItemID
	Count     int
	Proposal  int
	NotJonahs bool
}

// State is the state n gives its observation: proposed when it became a proposal, not Jonah's
// when it is marked so, else watching.
func (n NewObservation) State() ObservationState {
	switch {
	case n.Proposal > 0:
		return ObservationProposed
	case n.NotJonahs:
		return ObservationNotJonahs
	default:
		return ObservationWatching
	}
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
