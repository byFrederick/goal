package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrDraftRequestOpen means the factory already has a queued or running draft request: a factory
// runs one at a time (ADR 0021, decision 4), and the API answers 409.
var ErrDraftRequestOpen = errors.New("the factory already has a queued or running draft request")

// Draft request limits (ADR 0021, decision 1). Lengths count characters, as char_length does.
const (
	MaxDraftRequestText = 2000
	// MaxDraftRequestSummary and MaxDraftRequestError bound what a finish stores; longer is cut.
	MaxDraftRequestSummary = 4000
	MaxDraftRequestError   = 2000
	// MaxDraftRequestList is the most requests one List returns.
	MaxDraftRequestList = 100
)

// DraftRequestID is a draft request's id (a UUID).
type DraftRequestID string

// DraftRequestStatus is where a draft request is: queued until a goald claims it, running under
// a lease, then done or failed.
type DraftRequestStatus string

const (
	DraftRequestQueued  DraftRequestStatus = "queued"
	DraftRequestRunning DraftRequestStatus = "running"
	DraftRequestDone    DraftRequestStatus = "done"
	DraftRequestFailed  DraftRequestStatus = "failed"
)

// Check is ErrInvalid for a status other than the four.
func (s DraftRequestStatus) Check() error {
	switch s {
	case DraftRequestQueued, DraftRequestRunning, DraftRequestDone, DraftRequestFailed:
		return nil
	default:
		return fmt.Errorf("draft request status %q: %w", s, ErrInvalid)
	}
}

// Open reports whether the request still holds its factory's one turn: queued or running.
func (s DraftRequestStatus) Open() bool { return s == DraftRequestQueued || s == DraftRequestRunning }

// DraftRequest is an asked change to a factory's definition (ADR 0021): what a person asked from
// the Definition screen's Ask bar or the Agent screen's Ask Jonah for a fix, run by goald in a box
// that writes its result into the factory's draft and never publishes.
type DraftRequest struct {
	ID      DraftRequestID
	Org     OrgID
	Factory FactoryID
	// Requester is the actor who asked.
	Requester Actor
	Text      string
	// Agent is the definition's agent the request is about (Ask Jonah for a fix), "" for the Ask bar.
	Agent  string
	Status DraftRequestStatus
	// Summary is the agent's account of what it changed, or why the request failed.
	Summary string
	// ChangedPaths are the definition paths the request wrote or deleted in the draft.
	ChangedPaths []string
	CostUSD      float64
	Turns        int
	// Error is what went wrong when the request failed, "" otherwise.
	Error      string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	// LeaseExpiresAt is when a running request's lease ends; nil when no goald holds it.
	LeaseExpiresAt *time.Time
}

// NewDraftRequest is what creating a draft request takes; the store gives it its id, queued
// status and time.
type NewDraftRequest struct {
	Factory   FactoryID
	Requester Actor
	Text      string
	Agent     string
}

// Check trims the text and checks the request, each failure wrapping ErrInvalid: a factory, a
// requester, text of 1 to MaxDraftRequestText characters, and an agent that is "" or an agent
// name.
func (n *NewDraftRequest) Check() error {
	n.Text = strings.TrimSpace(n.Text)

	if n.Factory == "" {
		return fmt.Errorf("draft request has no factory: %w", ErrInvalid)
	}

	if n.Requester.Kind == "" || n.Requester.ID == "" {
		return fmt.Errorf("draft request has no requester: %w", ErrInvalid)
	}

	if c := utf8.RuneCountInString(n.Text); c < 1 || c > MaxDraftRequestText {
		return fmt.Errorf("draft request text must be 1 to %d characters: %w", MaxDraftRequestText, ErrInvalid)
	}

	if n.Agent != "" && !definitionSlug.MatchString(n.Agent) {
		return fmt.Errorf("draft request agent %q is not an agent name: %w", n.Agent, ErrInvalid)
	}

	return nil
}

// DraftRequestLease is one goald's hold on a running request. Its token is drawn on every claim
// and fences the heartbeats and the finish: once another claim took the request over (the
// holder stalled past its lease), neither applies any more.
type DraftRequestLease struct {
	Request DraftRequestID
	Token   string
}

// DraftRequestClaim is a claimed request with its organization, which the run then works under,
// and its lease.
type DraftRequestClaim struct {
	Org     OrgID
	Request DraftRequest
	Lease   DraftRequestLease
}

// DraftRequestResult is how a request ended.
type DraftRequestResult struct {
	// Status is done or failed.
	Status       DraftRequestStatus
	Summary      string
	ChangedPaths []string
	CostUSD      float64
	Turns        int
	Error        string
	At           time.Time
}

// Check is ErrInvalid for a status other than done or failed, a negative cost or turn count, or
// an empty changed path.
func (r DraftRequestResult) Check() error {
	if err := r.Status.Check(); err != nil {
		return err
	}

	if r.Status.Open() {
		return fmt.Errorf("a draft request ends done or failed, not %s: %w", r.Status, ErrInvalid)
	}

	if r.CostUSD < 0 || r.Turns < 0 {
		return fmt.Errorf("draft request cost and turns must not be negative: %w", ErrInvalid)
	}

	for _, p := range r.ChangedPaths {
		if p == "" {
			return fmt.Errorf("draft request changed path is empty: %w", ErrInvalid)
		}
	}

	return nil
}
