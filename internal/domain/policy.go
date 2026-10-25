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
	// Assemblies is how the line treats items that edit the same files.
	Assemblies AssembliesPolicy
}

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

// AssembliesPolicy is board.flow.assemblies: how the parts of assemblies, and any items of the
// line, are built together.
type AssembliesPolicy struct {
	Overlap OverlapPolicy
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
