package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// RunID identifies one agent run (one container working one item at one station).
type RunID string

// SandboxSize is how much CPU and memory a run's sandbox gets; each sandbox backend maps it to
// its own limits (spec 4 §4.6). Empty is medium, the default.
type SandboxSize string

// Sandbox sizes.
const (
	SandboxSmall  SandboxSize = "small"
	SandboxMedium SandboxSize = "medium"
	SandboxLarge  SandboxSize = "large"
)

// SandboxSizes are the sandbox sizes, smallest first.
var SandboxSizes = []SandboxSize{SandboxSmall, SandboxMedium, SandboxLarge}

// BoxSize is what one sandbox backend gives a box of Size: its virtual CPUs and memory.
type BoxSize struct {
	Size        SandboxSize
	VCPUs       float64
	MemoryBytes int64
}

// Lease is an agent slot's hold on an item. It expires unless heartbeated, so work held by a
// crashed instance returns to the queue.
type Lease struct {
	ItemID    ItemID
	FactoryID FactoryID
	Station   string
	Agent     string
	// Slot is the station slot in [0, capacity); a slot holds at most one lease.
	Slot int
	// AcquiredAt is set once per claim and fences the lease: a transition or heartbeat that
	// names an older AcquiredAt for the same item no longer holds the lease.
	AcquiredAt  time.Time
	HeartbeatAt time.Time
	ExpiresAt   time.Time
}

// Expired reports whether the lease has lapsed at now.
func (l Lease) Expired(now time.Time) bool { return !now.Before(l.ExpiresAt) }

// RunStatus is an agent run's outcome. Values match agent_runs.status.
type RunStatus string

// Run statuses.
const (
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// RunLostError is the error of a run that was still running when its lease ended without it: the
// instance running it stopped before it could finish it, so its cost and turns are not known.
const RunLostError = "the instance running it stopped before finishing it; cost and turns unknown"

// Run is one agent run as recorded by the store.
type Run struct {
	ID        RunID
	FactoryID FactoryID
	// DefinitionVersionID is the definition version the run started and finished on; empty
	// for runs from before versions ("version unknown").
	DefinitionVersionID DefinitionVersionID
	ItemID              ItemID
	Station             string
	Agent               string
	Model               string
	Result              RunResult
	// Input is what the run was given, as RecordRunInput stored it; empty for runs from before
	// run records (00085).
	Input     RunInput
	StartedAt time.Time
	EndedAt   time.Time
}

// RunInput is what a run was given, for a later try to rerun it (ADR 0039, decision 8). Empty
// fields are unknown: a retry in its claim's box has no heads, and a run without a repository none.
type RunInput struct {
	// Brief is the brief the agent was given: the bundle's brief (the item's work) and the run's
	// feedback (its notes, answers and rejections), joined as the worker's prompt joins them.
	Brief string
	// Repositories are the repositories prepare checked out, in the order it reported them, each
	// with its BaseCommit and BranchHead (ADR 0041 decision 5). Read back from the store, each
	// row is whole: its PushedHead and Wrote too.
	Repositories []RunRepository
}

// TryRepository is the repository a try of the run starts from (ADR 0041 decision 17): the first
// of its repositories with a branch head, false when it recorded none.
func (in RunInput) TryRepository() (RunRepository, bool) {
	for _, r := range in.Repositories {
		if r.BranchHead != "" {
			return r, true
		}
	}

	return RunRepository{}, false
}

// RunRepository is one repository a run fetched or pushed, as agent_run_repositories keeps it
// (ADR 0041 decision 5). Empty commits are unknown.
type RunRepository struct {
	// Repo is the repository's RepositoryKey. It is empty only on a run recorded before 00132
	// whose item had no repository row: the repository entry the run's factory version named.
	Repo string
	// BaseCommit and BranchHead are the base's and the item branch's commits when prepare checked
	// the branch out, before it brought the branch up to date with the base.
	BaseCommit string
	BranchHead string
	// PushedHead is the item branch's commit on the remote when the agent ended.
	PushedHead string
	// Wrote reports whether the run pushed a change to the repository.
	Wrote bool
}

// ListedRun is a run with the number and title of the item it worked, as the organization's run
// list shows it.
type ListedRun struct {
	Run
	ItemNumber int64
	ItemTitle  string
}

// RunSummary counts the runs of a window, whatever their status, and what they cost.
type RunSummary struct {
	Total   int
	Running int
	Failed  int
	// Finished counts succeeded and cancelled runs.
	Finished int
	SpendUSD float64
	// ByFactory is each factory's spend, only factories with a run in the window, by factory id.
	ByFactory []FactorySpend
}

// RunResult is what a runner returns when an agent task ends.
type RunResult struct {
	// RunID is the run's record (agent_runs), set by engine.RunTask once it recorded one.
	RunID  RunID
	Status RunStatus
	// Output is the agent's final JSON output; it must satisfy the station's contract.
	Output       json.RawMessage
	CostUSD      float64
	Turns        int
	InputTokens  int64
	OutputTokens int64
	SessionID    string
	// Error explains a failed or cancelled run.
	Error string
	// Repositories are the repositories the worker reported at the end, each with its PushedHead
	// and whether it Wrote; none without a repository or when the worker could not read them.
	// Read back from the store, they are the run's rows with a pushed head or a write, whole.
	Repositories []RunRepository
	// Diff is a try's change against the commit it started from, as engine.RunTask read it from
	// the box once the agent ended; empty for every other run.
	Diff string
	// FailedCheck is the first of a try's checks (RunRequest.Checks) that failed in its box once
	// its agent succeeded; nil when they all passed, and for every other run.
	FailedCheck *FailedCheck
}

// FailedCheck is a command check that ran and exited non-zero: its command, its exit code (124
// when it ran out of time) and the end of its output, scrubbed of the run's secrets.
type FailedCheck struct {
	Command  string
	ExitCode int
	Output   string
}

// RunRequest is one agent run for engine.RunTask: what it records, which sandbox it opens, what
// the gateway serves it and what it is given. A line's run (Kind station) works an item at a
// station under the item's lease; a Jonah review's (Kind jonah, ADR 0034 decision 1) works no
// item, holds no lease, clones no repository and gets only the model connection (Check). A try of
// a past item (Kind try, ADR 0039 decision 9) reruns a station on no item and under no lease, from
// a recorded commit, with only the model's access. A grade (Kind try on a TaskKindGrade bundle,
// ADR 0039 decision 10) grades a try's result, or its item's original or final one, in one model
// call: no item, lease, repository, commit, check, environment or connection but the model's, so
// its cost is Jonah's as a try's is.
type RunRequest struct {
	OrgID     OrgID
	FactoryID FactoryID
	// Version is the published definition version the run starts on.
	Version DefinitionVersionID
	// Kind is whose run it is; empty is RunKindStation.
	Kind RunKind
	// Station and Item are the station and the item a line's run works at; both empty for a Jonah
	// run.
	Station string
	Item    ItemID
	// Agent names the run's agent and Model its model.
	Agent string
	Model string
	// Brief is what goald adds to the bundle's brief for this run (GOAL_FEEDBACK); empty adds
	// nothing.
	Brief string
	// Bundle is the kind of task token the gateway serves the run's bundle by: empty for the
	// version's agent at the station, as for every line's run.
	Bundle TaskKind
	// WindowStart and WindowEnd are a Jonah review's window and Manual whether a person asked for
	// it: a TaskKindJonah bundle's token carries them (TaskClaims), and no other bundle has them.
	WindowStart, WindowEnd time.Time
	Manual                 bool
	// Repository is what the sandbox's prepare clones on Branch; zero clones nothing.
	Repository Repository
	// Branch and Base are the run's branch and the branch it is cut from (TaskClaims).
	Branch, Base string
	// ModelConnection is the connection that gives a run on connections its model access.
	ModelConnection string
	// Connections are the organization connections the agent gets besides the model's.
	Connections []string
	// ModelKey is the {{secret}} that gives a run on secrets its model (ADR 0037 decision 7), and
	// Environment the agent's environment file it gets its env from (decision 10); a run on
	// secrets names no connection, and a Jonah run no environment.
	ModelKey    string
	Environment Environment
	// DeniedCommands are added to the agent's deny rules (TaskClaims.DeniedCommands).
	DeniedCommands []string
	// Knowledge are the knowledge snapshots the run reads (TaskClaims.Knowledge).
	Knowledge []KnowledgePin
	// NoEnvironment says the agent gets no env, files or MCP servers (TaskClaims.NoEnvironment).
	NoEnvironment bool
	// Size is the sandbox's size; empty is medium. Image is the sandbox's image; empty is the
	// engine's, Goal's own.
	Size  SandboxSize
	Image string
	// Timeout bounds the run; BoxLimit, when set, bounds how long its sandbox may live (the
	// organization policy's box time limit). Zero takes the engine's defaults.
	Timeout, BoxLimit time.Duration
	// Lease is the lease the run heartbeats while it runs, and loses the run with when it is lost;
	// nil holds none.
	Lease *Lease
	// TryOf is the past-item try a try run carries out, which its task token names so the gateway
	// serves the original run's brief, outputs and commit, and Commit the commit it starts from
	// (the original run's branch head); both empty for every other kind.
	TryOf  PastTryID
	Commit string
	// GradeOf is which result a grade run grades, which its task token names so the gateway
	// serves that result; empty for every other run.
	GradeOf GradeOf
	// Checks are the station's command checks a try runs in its box, in order, once its agent
	// succeeded (ADR 0039 decision 10); a line's run gets its station's checks from its claim, so
	// only a try names them.
	Checks []string
	// Trigger is the trigger a check run (Kind check, ADR 0040 decision 19) checks for, which its
	// task token names; empty for every other run.
	Trigger string
}

// ErrBadRunRequest refuses a RunRequest that Check does not accept.
var ErrBadRunRequest = fmt.Errorf("bad run request: %w", ErrInvalid)

// Check reports why r cannot run: a request without its organization, factory or agent, without
// a model connection or a model key or with both, on a model key with a connection, a station run
// without its item and station, a Jonah run with an item, a station, a lease, a repository, an
// environment or a connection besides the model's, a try or a grade with what it may not have
// (checkTry, checkGrade), a jonah bundle without a window that ends after it starts, or a window
// on another bundle.
func (r RunRequest) Check() error {
	if err := r.checkModel(); err != nil {
		return err
	}

	if err := r.checkWindow(); err != nil {
		return err
	}

	if (r.Kind == RunKindCheck) != (r.Trigger != "") {
		return fmt.Errorf("%w: a check run, and only it, names its trigger", ErrBadRunRequest)
	}

	switch r.Kind {
	case RunKindCheck:
		return r.checkCheck()
	case RunKindJonah:
		return r.checkJonah()
	case RunKindTry:
		if r.Bundle == TaskKindGrade {
			return r.checkGrade()
		}

		return r.checkTry()
	case "", RunKindStation:
		return r.checkStation()
	default:
		return fmt.Errorf("%w: unknown run kind %q", ErrBadRunRequest, r.Kind)
	}
}

// checkStation refuses a line's run without its item and station, or that names what only a try
// or a grade has.
func (r RunRequest) checkStation() error {
	switch {
	case r.TryOf != "" || r.Commit != "" || r.GradeOf != "" || r.Bundle == TaskKindTry || r.Bundle == TaskKindGrade || len(r.Checks) > 0:
		return fmt.Errorf("%w: only a try names a try, a commit, a result to grade and checks", ErrBadRunRequest)
	case r.Item == "" || r.Station == "":
		return fmt.Errorf("%w: a station run needs its item and station", ErrBadRunRequest)
	}

	return nil
}

// checkModel refuses a request without its organization, factory or agent, without a model
// connection or a model key or with both, and one on a model key with a connection.
func (r RunRequest) checkModel() error {
	if r.OrgID == "" || r.FactoryID == "" || r.Agent == "" || (r.ModelConnection == "") == (r.ModelKey == "") {
		return fmt.Errorf("%w: it needs an organization, a factory, an agent and either a model connection or a model key", ErrBadRunRequest)
	}

	if r.ModelKey != "" && (len(r.Connections) > 0 || r.Repository.Connection != "") {
		return fmt.Errorf("%w: a run on secrets opens no connection", ErrBadRunRequest)
	}

	return nil
}

// checkWindow refuses a jonah bundle without a window that ends after it starts, and a window or
// Manual on any other bundle.
func (r RunRequest) checkWindow() error {
	if r.Bundle == TaskKindJonah {
		if r.WindowStart.IsZero() || !r.WindowEnd.After(r.WindowStart) {
			return fmt.Errorf("%w: a jonah bundle needs a window that ends after it starts", ErrBadRunRequest)
		}

		return nil
	}

	if !r.WindowStart.IsZero() || !r.WindowEnd.IsZero() || r.Manual {
		return fmt.Errorf("%w: only a jonah bundle has a window", ErrBadRunRequest)
	}

	return nil
}

// checkJonah refuses a Jonah run with what ADR 0034 decision 1 leaves out of it.
func (r RunRequest) checkJonah() error {
	if r.Item != "" || r.Station != "" || r.Lease != nil || r.Repository != (Repository{}) || len(r.Connections) > 0 ||
		r.Environment.Name != "" || len(r.Environment.Env) > 0 || r.TryOf != "" || r.GradeOf != "" {
		return fmt.Errorf("%w: a Jonah run has no item, station, lease, repository, environment, try or connection but the model's", ErrBadRunRequest)
	}

	return nil
}

// checkTry refuses a try run with an item, a lease or a connection besides the model's, without its
// station, its try, its recorded commit or a repository to fetch it from, or on another bundle.
func (r RunRequest) checkTry() error {
	switch {
	case r.Item != "" || r.Lease != nil || len(r.Connections) > 0:
		return fmt.Errorf("%w: a try has no item, lease or connection but the model's", ErrBadRunRequest)
	case r.Station == "" || r.TryOf == "" || r.Bundle != TaskKindTry || r.GradeOf != "":
		return fmt.Errorf("%w: a try needs its station, its try and a try bundle, and grades no result", ErrBadRunRequest)
	case r.Commit == "" || r.Repository.URL == "":
		return fmt.Errorf("%w: a try needs a recorded commit and the repository it is in", ErrBadRunRequest)
	}

	return nil
}

// checkCheck refuses a check run with an item, a station, a lease, a repository, a commit, a try,
// a grade, checks or knowledge (ADR 0040 decision 19: it only reads, clones nothing and has no
// person to ask), or on another bundle than a check's.
func (r RunRequest) checkCheck() error {
	switch {
	case r.Item != "" || r.Station != "" || r.Lease != nil || r.Repository != (Repository{}) || r.Commit != "" ||
		r.TryOf != "" || r.GradeOf != "" || len(r.Checks) > 0 || len(r.Knowledge) > 0:
		return fmt.Errorf("%w: a check has no item, station, lease, repository, commit, try, grade, checks or knowledge", ErrBadRunRequest)
	case r.Bundle != TaskKindCheck:
		return fmt.Errorf("%w: a check runs on a check bundle", ErrBadRunRequest)
	}

	return nil
}

// checkGrade refuses a grade run with an item, a lease, a repository, a commit, checks, an
// environment or a connection besides the model's, or without its station, its try, the result it
// grades or its model.
func (r RunRequest) checkGrade() error {
	switch {
	case r.Item != "" || r.Lease != nil || len(r.Connections) > 0 || r.Repository != (Repository{}) || r.Commit != "" ||
		len(r.Checks) > 0 || r.Environment.Name != "" || len(r.Environment.Env) > 0:
		return fmt.Errorf("%w: a grade has no item, lease, repository, commit, check, environment or connection but the model's", ErrBadRunRequest)
	case r.Station == "" || r.TryOf == "" || !r.GradeOf.Valid() || r.Model == "":
		return fmt.Errorf("%w: a grade needs its station, its try, the result it grades and its model", ErrBadRunRequest)
	}

	return nil
}

// Decision is a gate's ruling on an item.
type Decision string

// Gate decisions.
const (
	Approve Decision = "approve"
	Reject  Decision = "reject"
)

// Verdict is a gate station's output: {"decision": "approve"|"reject", "reason": "..."}.
type Verdict struct {
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason,omitempty"`
	// Category groups rejections for the agent statistics ("Why review sent work back"); a
	// reviewer's contract may declare it, best as an enum. Nothing in Goal names a category.
	Category string `json:"category,omitempty"`
}

// ErrBadVerdict means a gate station's output is not a usable verdict.
var ErrBadVerdict = errors.New("bad verdict")

// ParseVerdict reads a gate verdict from a run's output. A rejection must carry a reason,
// because the reason is what the upstream agent acts on.
func ParseVerdict(output json.RawMessage) (Verdict, error) {
	var v Verdict
	if err := json.Unmarshal(output, &v); err != nil {
		return Verdict{}, fmt.Errorf("%w: %w", ErrBadVerdict, err)
	}

	switch v.Decision {
	case Approve:
		return v, nil
	case Reject:
		if v.Reason == "" {
			return Verdict{}, fmt.Errorf("%w: a rejection needs a reason", ErrBadVerdict)
		}

		return v, nil
	}

	return Verdict{}, fmt.Errorf("%w: decision %q is not approve or reject", ErrBadVerdict, v.Decision)
}

// LaneFrom returns the "lane" an output declares (for example a spec choosing its engineer),
// or "" when the output does not declare one.
func LaneFrom(output json.RawMessage) string {
	var v struct {
		Lane string `json:"lane"`
	}

	if json.Unmarshal(output, &v) != nil {
		return ""
	}

	return v.Lane
}

// PathsFrom returns the file globs an output lists in field, the output field a definition maps
// to the item's paths (flow.dependencies.paths), without blank entries. It returns nil when field
// is "", or the output has no such list of strings or it is empty.
func PathsFrom(output json.RawMessage, field string) []string {
	if field == "" {
		return nil
	}

	var v map[string]json.RawMessage
	if json.Unmarshal(output, &v) != nil {
		return nil
	}

	var paths []string
	if json.Unmarshal(v[field], &paths) != nil {
		return nil
	}

	paths = slices.DeleteFunc(paths, func(p string) bool { return p == "" })
	if len(paths) == 0 {
		return nil
	}

	return paths
}
