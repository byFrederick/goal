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
	// Assemblies says how runs keep item branches fresh and what a merge conflict does.
	Assemblies AssemblyPolicy
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

// AssemblyPolicy is board.flow.assemblies, with its defaults applied.
type AssemblyPolicy struct {
	FreshBase  FreshBase
	OnConflict ConflictPolicy
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
