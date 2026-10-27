package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// JudgeScoreID identifies one judge score.
type JudgeScoreID string

// JudgeScoreState is where a judge score is (ADR 0025 decision 2): queued until a goald claims
// it, running under that claim's lease while its judge run works, then scored or error. An error
// is an infrastructure failure, never a failing score.
type JudgeScoreState string

// Judge score states.
const (
	JudgeScoreQueued  JudgeScoreState = "queued"
	JudgeScoreRunning JudgeScoreState = "running"
	JudgeScoreScored  JudgeScoreState = "scored"
	JudgeScoreError   JudgeScoreState = "error"
)

// Valid reports whether s is one of the four states.
func (s JudgeScoreState) Valid() bool {
	switch s {
	case JudgeScoreQueued, JudgeScoreRunning, JudgeScoreScored, JudgeScoreError:
		return true
	default:
		return false
	}
}

// JudgeSummaryWindow is how far back a judge's summary reaches.
const JudgeSummaryWindow = 30 * 24 * time.Hour

// MaxJudgeReason and MaxJudgeOutcome bound a judge's one-sentence reason and an outcome label.
const (
	MaxJudgeReason  = 2000
	MaxJudgeOutcome = 100
)

// judgeSlug is the form of a judge's slug, its file name: evals.schema.json's slug.
var judgeSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// JudgeScore is one judge's score of one run (ADR 0025 decision 2): a finished production run,
// or with no RunID a try of an eval case. Outcome, Score, Passed and ScoredAt are set once it is
// scored; an error keeps only Reason, CostUSD, JudgeRunID and ScoredAt.
type JudgeScore struct {
	ID        JudgeScoreID
	OrgID     OrgID
	FactoryID FactoryID
	// VersionID is the definition version whose judge file scored the run.
	VersionID DefinitionVersionID
	// Judge is the judge's slug.
	Judge string
	// RunID is the scored production run; empty for a try.
	RunID RunID
	// ItemID is the run's item; ItemNumber and ItemTitle are read with it by Recent only.
	ItemID     ItemID
	ItemNumber int64
	ItemTitle  string
	State      JudgeScoreState
	// Outcome is the label of the judge's outcome the judge run returned.
	Outcome string
	// Score is the outcome's score, from 0 to 1.
	Score  float64
	Passed bool
	Reason string
	// CostUSD is what the judge run cost.
	CostUSD float64
	// JudgeRunID is the judge's own run, once it has one.
	JudgeRunID RunID
	// LeaseUntil is when the claim of a running score ends.
	LeaseUntil time.Time
	CreatedAt  time.Time
	ScoredAt   time.Time
}

// NewJudgeScore is a score to take: judge Judge of version VersionID scores run RunID of item
// ItemID, or with no RunID a try (with or without an item). At is when it is queued.
type NewJudgeScore struct {
	FactoryID FactoryID
	VersionID DefinitionVersionID
	Judge     string
	RunID     RunID
	ItemID    ItemID
	At        time.Time
}

// Check reports the first rule n breaks, wrapping ErrInvalid.
func (n NewJudgeScore) Check() error {
	switch {
	case n.FactoryID == "":
		return fmt.Errorf("judge score: no factory: %w", ErrInvalid)
	case n.VersionID == "":
		return fmt.Errorf("judge score: no definition version: %w", ErrInvalid)
	case !judgeSlug.MatchString(n.Judge):
		return fmt.Errorf("judge score: judge %q is not a slug: %w", n.Judge, ErrInvalid)
	case n.RunID != "" && n.ItemID == "":
		return fmt.Errorf("judge score: run %s has no item: %w", n.RunID, ErrInvalid)
	case n.At.IsZero():
		return fmt.Errorf("judge score: no time: %w", ErrInvalid)
	default:
		return nil
	}
}

// JudgeScoreLease is a claim's hold on a running score. Token fences it: a Record that names an
// older token changes nothing once another claim took the score.
type JudgeScoreLease struct {
	ID    JudgeScoreID
	Token string
	Until time.Time
}

// JudgeScoreClaim is a claimed score, running, with its lease.
type JudgeScoreClaim struct {
	Score JudgeScore
	Lease JudgeScoreLease
}

// JudgeResult is what a judge run came to at At: scored, with the outcome it returned, its score
// and whether that reaches the judge's pass mark, or error, with no outcome.
type JudgeResult struct {
	State      JudgeScoreState
	Outcome    string
	Score      float64
	Passed     bool
	Reason     string
	CostUSD    float64
	JudgeRunID RunID
	At         time.Time
}

// Check reports the first rule r breaks, wrapping ErrInvalid.
func (r JudgeResult) Check() error {
	var err error

	switch {
	case r.State == JudgeScoreScored && r.Outcome == "":
		err = errors.New("a score has an outcome")
	case r.State == JudgeScoreScored && (r.Score < 0 || r.Score > 1):
		err = fmt.Errorf("score %v is not from 0 to 1", r.Score)
	case r.State == JudgeScoreError && (r.Outcome != "" || r.Score != 0 || r.Passed):
		err = errors.New("an error has no outcome, score or pass")
	case r.State != JudgeScoreScored && r.State != JudgeScoreError:
		err = fmt.Errorf("state %q is not a result", r.State)
	case len(r.Outcome) > MaxJudgeOutcome:
		err = fmt.Errorf("outcome longer than %d bytes", MaxJudgeOutcome)
	case len(r.Reason) > MaxJudgeReason:
		err = fmt.Errorf("reason longer than %d bytes", MaxJudgeReason)
	case r.CostUSD < 0:
		err = fmt.Errorf("cost %v is negative", r.CostUSD)
	case r.At.IsZero():
		err = errors.New("no time")
	}

	if err != nil {
		return fmt.Errorf("judge result: %w: %w", err, ErrInvalid)
	}

	return nil
}

// JudgeSummary is one judge's production scores over JudgeSummaryWindow: how many runs it scored
// and passed, its pass rate, what its judge runs cost (errors included) and its daily pass rate.
type JudgeSummary struct {
	Judge    string
	Scored   int
	Passed   int
	PassRate float64
	CostUSD  float64
	// Days has one point per UTC day with a scored run, oldest first.
	Days []JudgeDay
}

// JudgeDay is a judge's scores of one UTC day, for the pass-rate sparkline.
type JudgeDay struct {
	// Day is the day's midnight, UTC.
	Day      time.Time
	Scored   int
	Passed   int
	PassRate float64
}

// PassRate is passed over scored, 0 when nothing was scored.
func PassRate(scored, passed int) float64 {
	if scored == 0 {
		return 0
	}

	return float64(passed) / float64(scored)
}

// JudgeDayOf is the UTC day of at, as its midnight: what a summary groups scores by.
func JudgeDayOf(at time.Time) time.Time {
	return at.UTC().Truncate(24 * time.Hour)
}
