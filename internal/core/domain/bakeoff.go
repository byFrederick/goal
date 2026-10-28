package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Bake-off limits (ADR 0029 decisions 1 and 2): two to four arms, and 1 to 10 tries per case.
const (
	MinBakeOffArms      = 2
	MaxBakeOffArms      = 4
	MaxBakeOffTries     = 10
	DefaultBakeOffTries = 3
)

// modelID is the shape of a model id, as contracts/definitions/factory.schema.json checks an
// agent's model. Which models exist is a deployment setting, never Go code.
var modelID = regexp.MustCompile(`^claude-[a-z0-9-]+$`)

// definitionSlug is the shape of a station's or an agent's id in a definition.
var definitionSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// NewBakeOffArm is one model to try in a bake-off. Current marks the agent's model in the
// bake-off's version, the arm the screens label "Now".
type NewBakeOffArm struct {
	Model   string
	Current bool
}

// NewBakeOff is a bake-off to queue (ADR 0029): an eval run of kind bake-off that runs one agent's
// cases of a published version on every arm. Each case runs TriesPerCase times on every arm; a
// case's own Tries is not used. The arms are numbered from 1 in the order given.
type NewBakeOff struct {
	FactoryID     FactoryID
	VersionID     DefinitionVersionID
	ContentSHA256 BlobHash
	Station       string
	Agent         string
	Cases         []EvalCase
	TriesPerCase  int
	Arms          []NewBakeOffArm
	EstimateUSD   float64
}

// Check reports what makes n impossible to queue, wrapping ErrInvalid.
func (n NewBakeOff) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("bake-off: "+format+": %w", append(args, ErrInvalid)...))
	}

	if n.FactoryID == "" {
		bad("no factory")
	}

	if n.VersionID == "" {
		bad("no version: a bake-off runs a published version")
	}

	if !n.ContentSHA256.Valid() {
		bad("content hash %q is not 64 lowercase hex", n.ContentSHA256)
	}

	if !definitionSlug.MatchString(n.Station) {
		bad("station %q is not a station id", n.Station)
	}

	if !definitionSlug.MatchString(n.Agent) {
		bad("agent %q is not an agent id", n.Agent)
	}

	if n.TriesPerCase < 1 || n.TriesPerCase > MaxBakeOffTries {
		bad("%d tries per case, want 1 to %d", n.TriesPerCase, MaxBakeOffTries)
	}

	if n.EstimateUSD < 0 {
		bad("estimate %v is negative", n.EstimateUSD)
	}

	errs = append(errs, checkBakeOffArms(n.Arms))

	return errors.Join(append(errs, checkEvalCases(n.EvalCases()))...)
}

// EvalCases are n's cases, each with TriesPerCase tries: what every arm runs.
func (n NewBakeOff) EvalCases() []EvalCase {
	out := make([]EvalCase, 0, len(n.Cases))
	for _, c := range n.Cases {
		c.Tries = n.TriesPerCase
		out = append(out, c)
	}

	return out
}

// checkBakeOffArms reports what makes a bake-off's arms impossible to run, wrapping ErrInvalid:
// fewer than two or more than four, a model that is not a model id or is listed twice, and
// anything but exactly one current arm.
func checkBakeOffArms(arms []NewBakeOffArm) error {
	var errs []error

	if len(arms) < MinBakeOffArms || len(arms) > MaxBakeOffArms {
		errs = append(errs, fmt.Errorf("bake-off: %d arms, want %d to %d: %w", len(arms), MinBakeOffArms, MaxBakeOffArms, ErrInvalid))
	}

	seen := make(map[string]bool, len(arms))
	current := 0

	for _, a := range arms {
		switch {
		case !modelID.MatchString(a.Model):
			errs = append(errs, fmt.Errorf("bake-off: model %q is not a model id (claude-...): %w", a.Model, ErrInvalid))
		case seen[a.Model]:
			errs = append(errs, fmt.Errorf("bake-off: model %q is listed twice: %w", a.Model, ErrInvalid))
		}

		seen[a.Model] = true

		if a.Current {
			current++
		}
	}

	if current != 1 {
		errs = append(errs, fmt.Errorf("bake-off: %d current arms, want exactly one: %w", current, ErrInvalid))
	}

	return errors.Join(errs...)
}

// BakeOff is an eval run of kind bake-off with its agent and arms. Its EvalRun's Results, from
// GetBakeOff, are every arm's tries by arm, case and try.
type BakeOff struct {
	EvalRun

	Station      string
	Agent        string
	TriesPerCase int
	Arms         []BakeOffArm
}

// BakeOffArm is one model of a bake-off, numbered from 1, and its summary once the engine has
// recorded it.
type BakeOffArm struct {
	Arm     int
	Model   string
	Current bool
	// Summary is nil until RecordArmSummary writes it.
	Summary *ArmSummary
}

// ArmSummary is one arm's row of a bake-off's report (ADR 0029 decision 3). A try recorded as
// error counts as not passed and stays in the means of what it spent.
type ArmSummary struct {
	Tries  int
	Passed int
	Errors int
	// CostPerItemUSD, TimePerItem and MeanTurns are the means over the arm's tries.
	CostPerItemUSD float64
	TimePerItem    time.Duration
	MeanTurns      float64
	// Judge is the advisory judge's slug and JudgedShare the share of the tries it passed; both
	// are empty when no judge scores the agent.
	Judge       string
	JudgedShare *float64
	// PerCase is the tries passed per case, by case name, out of the bake-off's tries per case.
	PerCase map[string]int
}

// PassRate is the share of the arm's tries that passed, 0 when it has none.
func (s ArmSummary) PassRate() float64 {
	if s.Tries == 0 {
		return 0
	}

	return float64(s.Passed) / float64(s.Tries)
}

// Check reports what makes s impossible to record, wrapping ErrInvalid.
func (s ArmSummary) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("arm summary: "+format+": %w", append(args, ErrInvalid)...))
	}

	if s.Tries < 0 || s.Passed < 0 || s.Errors < 0 || s.Passed+s.Errors > s.Tries {
		bad("%d passed and %d errors out of %d tries", s.Passed, s.Errors, s.Tries)
	}

	if s.CostPerItemUSD < 0 || s.TimePerItem < 0 || s.MeanTurns < 0 {
		bad("a negative mean: cost %v, time %v, turns %v", s.CostPerItemUSD, s.TimePerItem, s.MeanTurns)
	}

	return errors.Join(append(errs, s.checkJudged(), s.checkPerCase())...)
}

// checkJudged reports a judge without its share, a share without its judge, a judge that is not
// a slug and a share outside 0 to 1, wrapping ErrInvalid.
func (s ArmSummary) checkJudged() error {
	switch {
	case (s.Judge == "") != (s.JudgedShare == nil):
		return fmt.Errorf("arm summary: judge %q and its share go together: %w", s.Judge, ErrInvalid)
	case s.Judge == "":
		return nil
	case !judgeSlug.MatchString(s.Judge):
		return fmt.Errorf("arm summary: judge %q is not a judge's slug: %w", s.Judge, ErrInvalid)
	case *s.JudgedShare < 0 || *s.JudgedShare > 1:
		return fmt.Errorf("arm summary: judged share %v is not between 0 and 1: %w", *s.JudgedShare, ErrInvalid)
	default:
		return nil
	}
}

// checkPerCase reports a name that is not a case file's and a count outside 0 to Tries, wrapping
// ErrInvalid.
func (s ArmSummary) checkPerCase() error {
	var errs []error

	for name, passed := range s.PerCase {
		if !evalCaseName.MatchString(name) {
			errs = append(errs, fmt.Errorf("arm summary: case name %q is not a case file's name: %w", name, ErrInvalid))
		}

		if passed < 0 || passed > s.Tries {
			errs = append(errs, fmt.Errorf("arm summary: case %q: %d tries passed out of %d: %w", name, passed, s.Tries, ErrInvalid))
		}
	}

	return errors.Join(errs...)
}

// RecommendBakeOffArm is the arm a done bake-off recommends (ADR 0029 decision 4): the highest
// pass rate, then the lowest cost per item, then the lowest arm number. There is none (false)
// while the run is not done, when an arm has no summary or no tries, or when any arm errored on
// every try. The judged share never changes it.
func RecommendBakeOffArm(b BakeOff) (int, bool) {
	if b.State != EvalRunDone || len(b.Arms) == 0 {
		return 0, false
	}

	var best *BakeOffArm

	for i := range b.Arms {
		a := &b.Arms[i]
		if a.Summary == nil || a.Summary.Tries == 0 || a.Summary.Errors >= a.Summary.Tries {
			return 0, false
		}

		if best == nil || betterArm(*a, *best) {
			best = a
		}
	}

	return best.Arm, true
}

// betterArm reports whether a beats b: a higher pass rate, compared without rounding, then a
// lower cost per item, then a lower arm number.
func betterArm(a, b BakeOffArm) bool {
	sa, sb := a.Summary, b.Summary

	if l, r := sa.Passed*sb.Tries, sb.Passed*sa.Tries; l != r {
		return l > r
	}

	if sa.CostPerItemUSD != sb.CostPerItemUSD {
		return sa.CostPerItemUSD < sb.CostPerItemUSD
	}

	return a.Arm < b.Arm
}
