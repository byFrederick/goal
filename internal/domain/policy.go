package domain

import "time"

// ReleasePolicy says how queue stations release work into the line.
type ReleasePolicy string

// Release policies, as written in factory.yaml's flow.release.
const (
	// ReleasePush is the default: queue stations release everything they hold.
	ReleasePush ReleasePolicy = "push"
	// ReleaseDBR releases work at the pace of the constraint (drum-buffer-rope; enforcement is
	// later work).
	ReleaseDBR ReleasePolicy = "drum-buffer-rope"
)

// FlowPolicy is the line's flow policy and targets from factory.yaml's board.flow.
type FlowPolicy struct {
	Release ReleasePolicy
	// Constraint is "" or "auto" when the constraint is detected; otherwise a station id.
	Constraint string
	// ExpediteWIP caps expedite items in progress; 0 means unlimited.
	ExpediteWIP int
	// Andon, when set, pulls the cord automatically on a high rework rate (later work).
	Andon   *AndonPolicy
	Targets Targets
	// Assemblies says how runs keep item branches fresh, what a merge conflict does and how
	// the line treats items that edit the same files.
	Assemblies AssemblyPolicy
	// Retry caps the runs again of a claim whose sandbox is lost.
	Retry RetryPolicy
	// Dependencies maps the output fields that set an item's dependencies.
	Dependencies DependencyPolicy
}

// DependencyPolicy is board.flow.dependencies: which output fields set an item's dependencies.
// The platform names no field; a definition maps them (AGENTS.md rule 1).
type DependencyPolicy struct {
	// After is the output field listing the open items (<factory>#<number>, AfterFrom) the
	// work must follow: each becomes a linked edge from the item, or from its plan's root parts;
	// "" when no output declares any.
	After string
	// Paths is the output field whose list of file globs replaces the item's paths when a
	// station finishes with it (PathsFrom); "" when no output sets paths.
	Paths string
}

// DefaultTransientAttempts is board.flow.retry.transient.attempts when a definition leaves it out.
const DefaultTransientAttempts = 3

// RetryPolicy is board.flow.retry, with its defaults applied.
type RetryPolicy struct {
	// TransientAttempts is the most runs one claim makes when its sandbox is lost (a redeploy,
	// a box restart, a network blip); the loss that reaches it blocks the item. Below 1, as in
	// a line built without config, it is DefaultTransientAttempts.
	TransientAttempts int
}

// Transient is the cap on a claim's runs whose sandbox is lost: TransientAttempts, or
// DefaultTransientAttempts when that is unset.
func (p RetryPolicy) Transient() int {
	if p.TransientAttempts < 1 {
		return DefaultTransientAttempts
	}

	return p.TransientAttempts
}

// FreshBase says how a run brings an existing item branch up to date with the default branch.
type FreshBase string

const (
	// FreshBaseRebase is the default: the branch is rebased on the default branch.
	FreshBaseRebase FreshBase = "rebase"
	// FreshBaseMerge merges the default branch into the branch.
	FreshBaseMerge FreshBase = "merge"
)

// ConflictPolicy says what a merge conflict with the default branch does to an item.
type ConflictPolicy string

const (
	// ConflictRework is the default: the item goes back to be built again with the conflict.
	ConflictRework ConflictPolicy = "rework"
	// ConflictHuman leaves the conflict to a person.
	ConflictHuman ConflictPolicy = "human"
)

// OverlapPolicy says whether items whose file globs overlap may be built at the same time.
type OverlapPolicy string

// Overlap policies, as written in factory.yaml's flow.assemblies.overlap.
const (
	// OverlapSerialize is the default: an item whose files overlap those of an item in flight
	// waits for it.
	OverlapSerialize OverlapPolicy = "serialize"
	// OverlapAllow builds items in parallel whatever files they share.
	OverlapAllow OverlapPolicy = "allow"
)

// Holds reports whether items whose paths overlap wait for each other: every policy but
// OverlapAllow serializes them, as does the zero policy of a line built without config.
func (p OverlapPolicy) Holds() bool { return p != OverlapAllow }

// AssemblyPolicy is board.flow.assemblies, with its defaults applied.
type AssemblyPolicy struct {
	FreshBase  FreshBase
	OnConflict ConflictPolicy
	Overlap    OverlapPolicy
	// Materialize makes a station's plan (PlanFrom) an assembly with its parts when the station
	// finishes; off, the plan is ignored and the item moves on. On by default.
	Materialize bool
}

// AndonPolicy pulls the andon cord when too many gate verdicts reject.
type AndonPolicy struct {
	// ReworkRate is the share of gate verdicts that reject, 0..1.
	ReworkRate float64
	// Window is how many of the last verdicts at one gate station count.
	Window int
}

// Targets are what the line aims for; zero means no target.
type Targets struct {
	ThroughputPerDay float64
	// LeadTime is the target p85 lead time, created to done.
	LeadTime time.Duration
	// ExpediteLeadTime is the target lead time of each expedite item.
	ExpediteLeadTime time.Duration
}
