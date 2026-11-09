package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CIWaitID identifies one CI wait.
type CIWaitID string

// CIPhase is when a station waits for CI (ADR 0044 decisions 2 and 3).
type CIPhase string

const (
	// CIPhaseBefore waits for CI on the item's commits before a run starts.
	CIPhaseBefore CIPhase = "before"
	// CIPhaseAfter waits for CI on what a run pushed, with the outcome to apply held on the wait.
	CIPhaseAfter CIPhase = "after"
)

// Valid reports whether p is a known phase.
func (p CIPhase) Valid() bool { return p == CIPhaseBefore || p == CIPhaseAfter }

// CIOutcome is how a CI wait ended (ADR 0044 decisions 7 and 8).
type CIOutcome string

// The outcomes of a CI wait: every check passed, a check failed, max passed first, no check
// appeared within grace, the token cannot read CI, or the head moved.
const (
	CIPassed     CIOutcome = "passed"
	CIFailed     CIOutcome = "failed"
	CITimedOut   CIOutcome = "timed_out"
	CINoCI       CIOutcome = "no_ci"
	CIUnreadable CIOutcome = "unreadable"
	// CISuperseded ends a wait whose head moved; a wait on the new head replaces it.
	CISuperseded CIOutcome = "superseded"
)

// Valid reports whether o is a known outcome.
func (o CIOutcome) Valid() bool {
	switch o {
	case CIPassed, CIFailed, CITimedOut, CINoCI, CIUnreadable, CISuperseded:
		return true
	}

	return false
}

// The bounds of a CI poll's interval: every 15 s at first, backing off to every 2 min (ADR 0044
// decision 9).
const (
	CIPollMinInterval = 15 * time.Second
	CIPollMaxInterval = 2 * time.Minute
)

// CIPollKey names the commit a poll reads: the code host, the repository's key
// (RepositoryKey) and the commit's sha. Within an organization each key has one poll, which
// every wait on the commit shares.
type CIPollKey struct {
	Host string
	Repo string
	SHA  string
}

// CIWaitKey names an item's CI waits at one station, which Store.CIWaitsOf reads in one batch.
type CIWaitKey struct {
	Item    ItemID
	Station string
}

// CIPoll is one commit's poll: when it is next due, how often, what the last poll read and who
// holds it.
type CIPoll struct {
	CIPollKey

	NextPollAt time.Time
	Interval   time.Duration
	ETag       string
	// Checks is the last CommitChecks read, as JSON; nil until a poll reads any.
	Checks           json.RawMessage
	FirstCheckSeenAt time.Time
	// FirstPolledAt is when a poll of the commit first read its checks; zero until one does. Every
	// wait on the commit counts its grace from it (ADR 0044 decision 7).
	FirstPolledAt time.Time
	LastError     string
	// LeaseOwner and LeaseUntil are the lease LeaseDueCIPolls took; empty and zero when none.
	LeaseOwner string
	LeaseUntil time.Time
}

// CIPollResult is what one poll read, recorded on the poll with when it is next due.
type CIPollResult struct {
	// Checks replaces the poll's checks; nil keeps them (a 304, or an error).
	Checks json.RawMessage
	// ETag replaces the poll's ETag; empty keeps it.
	ETag string
	// Error replaces the poll's last error; empty clears it.
	Error string
	// FirstCheckSeenAt is set once: a poll that already has one keeps it, and zero sets none.
	FirstCheckSeenAt time.Time
	// FirstPolledAt is set once, like FirstCheckSeenAt.
	FirstPolledAt time.Time
	NextPollAt    time.Time
	// Interval is between CIPollMinInterval and CIPollMaxInterval.
	Interval time.Duration
}

// Check reports a result the store must refuse, wrapping ErrInvalid.
func (r CIPollResult) Check() error {
	if r.NextPollAt.IsZero() {
		return fmt.Errorf("ci poll result: a next poll time is required: %w", ErrInvalid)
	}

	if r.Interval < CIPollMinInterval || r.Interval > CIPollMaxInterval {
		return fmt.Errorf("ci poll interval %s is outside %s to %s: %w", r.Interval, CIPollMinInterval, CIPollMaxInterval, ErrInvalid)
	}

	return nil
}

// NewCIWait opens a CI wait of an item at one visit to a station, on one repository's commit.
type NewCIWait struct {
	Item    ItemID
	Station string
	// Visit identifies the item's visit to the station; the caller chooses the key.
	Visit string
	// Repo is the repository's key (RepositoryKey); its host is its first segment.
	Repo  string
	SHA   string
	PRURL string
	Phase CIPhase
	// Round counts the CI rounds of the visit from 1.
	Round int
	// Held is the outcome to apply when CI passes, as JSON; only CIPhaseAfter holds one.
	Held      json.RawMessage
	StartedAt time.Time
}

// Check reports a wait the store must refuse, wrapping ErrInvalid.
func (n NewCIWait) Check() error {
	key, ok := RepositoryKey(n.Repo)

	switch {
	case n.Item == "" || n.Station == "" || n.Visit == "":
		return fmt.Errorf("ci wait: an item, a station and a visit are required: %w", ErrInvalid)
	case !ok || key != n.Repo:
		return fmt.Errorf("ci wait repository %q is not a repository key: %w", n.Repo, ErrInvalid)
	case n.SHA == "":
		return fmt.Errorf("ci wait on %s: a sha is required: %w", n.Repo, ErrInvalid)
	case !n.Phase.Valid():
		return fmt.Errorf("ci wait phase %q: %w", n.Phase, ErrInvalid)
	case n.Round < 1:
		return fmt.Errorf("ci wait round %d: rounds count from 1: %w", n.Round, ErrInvalid)
	case len(n.Held) > 0 && n.Phase != CIPhaseAfter:
		return fmt.Errorf("ci wait: only the %s phase holds an outcome: %w", CIPhaseAfter, ErrInvalid)
	case n.StartedAt.IsZero():
		return fmt.Errorf("ci wait: a start time is required: %w", ErrInvalid)
	}

	return nil
}

// PollKey is the key of the poll the wait shares.
func (n NewCIWait) PollKey() CIPollKey {
	host, _, _ := strings.Cut(n.Repo, "/")

	return CIPollKey{Host: host, Repo: n.Repo, SHA: n.SHA}
}

// CIWaitRecord is one CI wait as stored (the station setting is CIWait, in line.go): open while EndedAt is zero, else ended with Outcome.
type CIWaitRecord struct {
	ID CIWaitID
	NewCIWait

	EndedAt time.Time
	Outcome CIOutcome
	// Poll is the shared poll's latest state, as OpenCIWaits reads it.
	Poll CIPoll
}

// Open reports whether the wait has not ended.
func (w CIWaitRecord) Open() bool { return w.EndedAt.IsZero() }
