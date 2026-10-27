package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// EvalRunID identifies one eval run (a UUIDv7).
type EvalRunID string

// EvalTrigger is why an eval run started. Values match eval_runs.trigger.
type EvalTrigger string

// Eval triggers (ADR 0025 decision 4): a person asked for it, or a publish runs the cases on the
// draft first.
const (
	EvalManual    EvalTrigger = "manual"
	EvalOnPublish EvalTrigger = "publish"
)

// EvalRunState is where an eval run is. Values match eval_runs.state; done, failed and cancelled
// are final, and only those have a FinishedAt.
type EvalRunState string

// Eval run states.
const (
	EvalRunQueued    EvalRunState = "queued"
	EvalRunRunning   EvalRunState = "running"
	EvalRunDone      EvalRunState = "done"
	EvalRunFailed    EvalRunState = "failed"
	EvalRunCancelled EvalRunState = "cancelled"
)

// Final reports whether s is a finished run's state.
func (s EvalRunState) Final() bool {
	return s == EvalRunDone || s == EvalRunFailed || s == EvalRunCancelled
}

// EvalResultState is where one try of a case is. Values match eval_results.state. A try that
// failed for an infrastructure reason is error, never fail (ADR 0025 decision 4).
type EvalResultState string

// Eval result states: pass, fail and error are finished.
const (
	EvalPending EvalResultState = "pending"
	EvalRunning EvalResultState = "running"
	EvalPass    EvalResultState = "pass"
	EvalFail    EvalResultState = "fail"
	EvalError   EvalResultState = "error"
)

// Finished reports whether s is a finished try's state.
func (s EvalResultState) Finished() bool { return s == EvalPass || s == EvalFail || s == EvalError }

// evalCaseName is the form of a case's name, its file name under evals/cases without .yaml, as
// internal/config checks it.
var evalCaseName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// MaxEvalDetail is the largest EvalOutcome.Detail, encoded, in bytes (256 KiB).
const MaxEvalDetail = 256 << 10

// EvalCase is one case an eval run runs: its name, the hash of its file and how many tries.
type EvalCase struct {
	Name   string
	SHA256 BlobHash
	Tries  int
}

// NewEvalRun is an eval run to queue: the cases of one definition, named by the hash of its
// content (ManifestSHA256) and, once published, its version. A draft's run has no VersionID.
type NewEvalRun struct {
	FactoryID     FactoryID
	VersionID     DefinitionVersionID
	ContentSHA256 BlobHash
	Trigger       EvalTrigger
	// Case is the one case a run was asked for; empty runs every case.
	Case        string
	Cases       []EvalCase
	EstimateUSD float64
}

// Check reports what makes n impossible to queue, wrapping ErrInvalid.
func (n NewEvalRun) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("eval run: "+format+": %w", append(args, ErrInvalid)...))
	}

	if n.FactoryID == "" {
		bad("no factory")
	}

	if !n.ContentSHA256.Valid() {
		bad("content hash %q is not 64 lowercase hex", n.ContentSHA256)
	}

	if n.Trigger != EvalManual && n.Trigger != EvalOnPublish {
		bad("trigger %q is not manual or publish", n.Trigger)
	}

	if n.EstimateUSD < 0 {
		bad("estimate %v is negative", n.EstimateUSD)
	}

	if n.Case != "" && (len(n.Cases) != 1 || n.Cases[0].Name != n.Case) {
		bad("case %q was asked for, so it is the only case to run", n.Case)
	}

	return errors.Join(append(errs, checkEvalCases(n.Cases))...)
}

// checkEvalCases reports what makes an eval run's case list impossible to run, wrapping
// ErrInvalid: none, a name that is not a case file's, one listed twice, a bad hash or no tries.
func checkEvalCases(cases []EvalCase) error {
	if len(cases) == 0 {
		return fmt.Errorf("eval run: no cases: %w", ErrInvalid)
	}

	var errs []error

	seen := make(map[string]bool, len(cases))

	for _, c := range cases {
		switch {
		case !evalCaseName.MatchString(c.Name):
			errs = append(errs, fmt.Errorf("eval run: case name %q is not a case file's name: %w", c.Name, ErrInvalid))
		case seen[c.Name]:
			errs = append(errs, fmt.Errorf("eval run: case %q is listed twice: %w", c.Name, ErrInvalid))
		}

		seen[c.Name] = true

		if !c.SHA256.Valid() {
			errs = append(errs, fmt.Errorf("eval run: case %q: hash %q is not 64 lowercase hex: %w", c.Name, c.SHA256, ErrInvalid))
		}

		if c.Tries < 1 {
			errs = append(errs, fmt.Errorf("eval run: case %q: %d tries, want at least 1: %w", c.Name, c.Tries, ErrInvalid))
		}
	}

	return errors.Join(errs...)
}

// EvalRun is one eval run and, from GetRun, its results by case and try.
type EvalRun struct {
	ID            EvalRunID
	FactoryID     FactoryID
	VersionID     DefinitionVersionID
	ContentSHA256 BlobHash
	Trigger       EvalTrigger
	Case          string
	State         EvalRunState
	EstimateUSD   float64
	// CostUSD is the sum of its recorded tries' costs.
	CostUSD   float64
	CreatedBy Actor
	CreatedAt time.Time
	// FinishedAt is zero until the run is done, failed or cancelled.
	FinishedAt time.Time
	Results    []EvalResult
}

// EvalResult is one try of one case in an eval run.
type EvalResult struct {
	EvalRunID  EvalRunID
	Case       string
	CaseSHA256 BlobHash
	Try        int
	State      EvalResultState
	// LeaseUntil is when a running try's claim ends; zero otherwise. RecordResult fences on it.
	LeaseUntil time.Time
	// RunID is the agent run that ran the try, empty when none did (or it was purged).
	RunID     RunID
	CheckedBy string
	Detail    map[string]any
	CostUSD   float64
	Turns     int
}

// EvalOutcome is how a claimed try ended: pass, fail or error, with what checked it (the
// contract, a judge, a reference patch), what it found, and what the try cost.
type EvalOutcome struct {
	State     EvalResultState
	RunID     RunID
	CheckedBy string
	Detail    map[string]any
	CostUSD   float64
	Turns     int
}

// Check reports what makes o impossible to record, wrapping ErrInvalid. Detail's size is checked
// where it is encoded.
func (o EvalOutcome) Check() error {
	var errs []error

	if !o.State.Finished() {
		errs = append(errs, fmt.Errorf("eval outcome %q is not pass, fail or error: %w", o.State, ErrInvalid))
	}

	if o.CostUSD < 0 {
		errs = append(errs, fmt.Errorf("eval outcome cost %v is negative: %w", o.CostUSD, ErrInvalid))
	}

	if o.Turns < 0 {
		errs = append(errs, fmt.Errorf("eval outcome turns %d is negative: %w", o.Turns, ErrInvalid))
	}

	if len(o.CheckedBy) > 200 {
		errs = append(errs, fmt.Errorf("eval outcome checked_by is over 200 bytes: %w", ErrInvalid))
	}

	return errors.Join(errs...)
}

// EvalCaseHistory is one case's recent record in a factory: its last finished tries, newest
// first, and what the case cost in the latest run that ran it.
type EvalCaseHistory struct {
	Case  string
	Tries []EvalResult
	// LatestRunID is that latest run, and LatestCostUSD the sum of the case's tries in it.
	LatestRunID   EvalRunID
	LatestCostUSD float64
}

// EvalHistoryTries is how many finished tries EvalCaseHistory keeps per case.
const EvalHistoryTries = 3

// ManifestSHA256 is the hash of a definition's content: the SHA-256 of "path\x00hash\n" for each
// file, sorted by path. It does not depend on map order, and it is the one hash an eval run and
// the publish gate compare a draft's content by.
func ManifestSHA256(m Manifest) BlobHash {
	h := sha256.New()

	for _, p := range slices.Sorted(maps.Keys(m)) {
		_, _ = h.Write([]byte(p + "\x00" + string(m[p]) + "\n"))
	}

	return BlobHash(hex.EncodeToString(h.Sum(nil)))
}

// EvalInput is an eval case's item as it reaches the agent: its title, body, work type and lane,
// earlier stations' outputs by station id, and the commit of runtime.repository the try starts
// from (empty: the default branch).
type EvalInput struct {
	Title   string
	Body    string
	Type    string
	Lane    string
	Outputs map[string]any
	Ref     string
}

// EvalCaseSpec is an eval case as the eval loop reads it from evals/cases/<name>.yaml (the schema
// contracts/definitions/evals.schema.json describes): the agent it runs, its input, how many tries
// and exactly one check. Contract checks the output against the agent's contract, Judge names a
// judge of evals/judges by slug, and ReferencePatch is the path of a known-good patch in the
// definition.
type EvalCaseSpec struct {
	Name        string
	SHA256      BlobHash
	Agent       string
	Description string
	Input       EvalInput
	Tries       int

	Contract       bool
	Judge          string
	ReferencePatch string
}

// The checked_by of an eval result: the contract, judge:<slug> or reference-patch:<path>.
const (
	EvalCheckedByContract       = "contract"
	EvalCheckedByJudge          = "judge:"
	EvalCheckedByReferencePatch = "reference-patch:"
)

// Check reports what makes c impossible to run, wrapping ErrInvalid: a name that is not a case
// file's, no agent, no tries, or not exactly one check.
func (c EvalCaseSpec) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("eval case %q: "+format+": %w", append(append([]any{c.Name}, args...), ErrInvalid)...))
	}

	if !evalCaseName.MatchString(c.Name) {
		bad("the name is not a case file's name")
	}

	if c.Agent == "" {
		bad("no agent")
	}

	if c.Tries < 1 {
		bad("%d tries, want at least 1", c.Tries)
	}

	checks := 0

	for _, set := range []bool{c.Contract, c.Judge != "", c.ReferencePatch != ""} {
		if set {
			checks++
		}
	}

	if checks != 1 {
		bad("%d checks, want exactly one of contract, judge and reference patch", checks)
	}

	return errors.Join(errs...)
}

// CheckedBy is what an eval result of c records as its checked_by.
func (c EvalCaseSpec) CheckedBy() string {
	switch {
	case c.Judge != "":
		return EvalCheckedByJudge + c.Judge
	case c.ReferencePatch != "":
		return EvalCheckedByReferencePatch + c.ReferencePatch
	default:
		return EvalCheckedByContract
	}
}

// PatchFiles lists the paths a unified diff touches, sorted and each once: the old and new names
// of its file headers (--- and +++, git's diff --git and rename lines), with a/ and b/ stripped
// and /dev/null skipped, so a rename counts under both names. Lines inside hunks are never read as
// headers.
func PatchFiles(diff []byte) []string {
	seen := map[string]bool{}

	var h hunk

	for line := range strings.SplitSeq(string(diff), "\n") {
		line = strings.TrimSuffix(line, "\r")

		if h.inside() {
			h.consume(line)

			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			h.old, h.new = hunkLengths(line)

			continue
		}

		for _, p := range headerPaths(line) {
			if p != "" && p != "/dev/null" {
				seen[p] = true
			}
		}
	}

	return slices.Sorted(maps.Keys(seen))
}

// hunk counts the old and new lines a hunk has left.
type hunk struct{ old, new int }

func (h hunk) inside() bool { return h.old > 0 || h.new > 0 }

// consume counts one hunk line: removed, added, a "\ No newline" note or context.
func (h *hunk) consume(line string) {
	switch {
	case strings.HasPrefix(line, "-"):
		h.old--
	case strings.HasPrefix(line, "+"):
		h.new--
	case strings.HasPrefix(line, `\`):
	default:
		h.old--
		h.new--
	}
}

// headerPaths are the paths a header line outside a hunk names.
func headerPaths(line string) []string {
	switch {
	case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
		return []string{patchPath(line[4:])}
	case strings.HasPrefix(line, "rename from "):
		return []string{line[len("rename from "):]}
	case strings.HasPrefix(line, "rename to "):
		return []string{line[len("rename to "):]}
	case strings.HasPrefix(line, "diff --git a/"):
		// A mode change or a binary file has no --- and +++ lines.
		if a, b, ok := strings.Cut(line[len("diff --git a/"):], " b/"); ok {
			return []string{a, b}
		}
	}

	return nil
}

// patchPath is the path of a --- or +++ header: without a timestamp after a tab and without the
// a/ or b/ prefix.
func patchPath(s string) string {
	s, _, _ = strings.Cut(s, "\t")
	s = strings.TrimSpace(s)

	if s == "/dev/null" {
		return s
	}

	for _, prefix := range []string{"a/", "b/"} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			return rest
		}
	}

	return s
}

// hunkLengths reads the old and new line counts of a hunk header, @@ -l[,s] +l[,s] @@; a count
// left out is 1.
func hunkLengths(header string) (int, int) {
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return 0, 0
	}

	length := func(r string) int {
		_, s, ok := strings.Cut(r[1:], ",")
		if !ok {
			return 1
		}

		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0
		}

		return n
	}

	return length(fields[1]), length(fields[2])
}
