package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestItemBranch_IsAStableGitRefFromNumberAndTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		number int64
		title  string
		want   string
	}{
		{7, "Add a `goal version` subcommand", "goal/7-add-a-goal-version-subcommand"},
		{12, "  Fix: lease sweep (again)!  ", "goal/12-fix-lease-sweep-again"},
		{3, "Ünïcode only ✓", "goal/3-n-code-only"},
		{4, "???", "goal/4"},
		{5, "a very long title that keeps going well past the limit of a branch", "goal/5-a-very-long-title-that-keeps-going-well-p"},
	}

	for _, tc := range tests {
		if got := (Item{Number: tc.number, Title: tc.title}).Branch(); got != tc.want {
			t.Errorf("Branch(%d, %q) = %q, want %q", tc.number, tc.title, got, tc.want)
		}
	}
}

func TestAssemblyProgress_CountsMergedWorkingAndWaitingParts(t *testing.T) {
	t.Parallel()

	a := Assembly{ID: "a", Parts: []Part{
		{ID: "p1", Station: stationBuild, State: StateDone},
		{ID: "p2", Station: stationBuild, State: StateInProgress},
		{ID: "p3", Station: "check", State: StateWaitingHuman},
		{ID: "p4", Station: stationBuild, State: StateQueued},
		{ID: "p5", Station: "triage", State: StateCancelled},
	}}

	want := AssemblyProgress{Total: 5, Merged: 1, InProgress: 1, Waiting: 2, Stations: []string{stationBuild, "check"}}
	if got := a.Progress(); !reflect.DeepEqual(got, want) {
		t.Errorf("Progress = %+v, want %+v", got, want)
	}

	if got := a.PartNumber("p3"); got != 3 {
		t.Errorf("PartNumber(p3) = %d, want 3", got)
	}

	if got := a.PartNumber("other"); got != 0 {
		t.Errorf("PartNumber(other) = %d, want 0", got)
	}
}

// An assembly's integration branch is goal/a<number>-<slug>, with exactly the slug of Branch.
func TestIntegrationBranch_UsesTheItemBranchSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		number int64
		title  string
		want   string
	}{
		{330, "Integration branches for assemblies", "goal/a330-integration-branches-for-assemblies"},
		{12, "  Fix: lease sweep (again)!  ", "goal/a12-fix-lease-sweep-again"},
		{3, "Ünïcode only ✓", "goal/a3-n-code-only"},
		{4, "???", "goal/a4"},
		{5, "", "goal/a5"},
		{5, "a very long title that keeps going well past the limit of a branch", "goal/a5-a-very-long-title-that-keeps-going-well-p"},
	}

	for _, tc := range tests {
		root := Item{Number: tc.number, Title: tc.title}
		if got := IntegrationBranch(root); got != tc.want {
			t.Errorf("IntegrationBranch(%d, %q) = %q, want %q", tc.number, tc.title, got, tc.want)
		}

		if got, item := IntegrationBranch(root), root.Branch(); strings.TrimPrefix(got, "goal/a") != strings.TrimPrefix(item, "goal/") {
			t.Errorf("IntegrationBranch(%q) = %q does not share Branch's slug %q", tc.title, got, item)
		}
	}
}

// A landing root, an assembly with no parent and an integration branch, works on that branch
// with the default branch as its base, at whichever station it is; a root on trunk keeps its own
// branch.
func TestRunBase_LandingRootWorksOnItsIntegrationBranch(t *testing.T) {
	t.Parallel()

	const integration = "goal/a1-big-request"

	landing := Item{ID: "a1", Number: 1, Title: "Big request", IntegrationBranch: integration, IntegrationLandAt: "merge"}
	if !landing.Landing() {
		t.Error("Landing() = false for a root with an integration branch, want true")
	}

	if branch, base := RunBase(landing, landing, ""); branch != integration || base != "" {
		t.Errorf("RunBase = %q, %q; want %q and the default branch", branch, base, integration)
	}

	trunk := landing
	trunk.IntegrationBranch, trunk.IntegrationLandAt = "", ""

	if trunk.Landing() {
		t.Error("Landing() = true for a root with no integration branch, want false")
	}

	if branch, base := RunBase(trunk, trunk, ""); branch != trunk.Branch() || base != "" {
		t.Errorf("RunBase = %q, %q; want %q and the default branch", branch, base, trunk.Branch())
	}

	part := Item{Number: 2, Title: "Store", ParentID: "a1", IntegrationBranch: integration}
	if part.Landing() {
		t.Error("Landing() = true for a part, want false")
	}
}

func TestRunBase_PartsBuildOnTheirAssemblysIntegrationBranch(t *testing.T) {
	t.Parallel()

	const integration = "goal/a1-big-request"

	part := Item{Number: 2, Title: "Store", ParentID: "a1"}
	root := Item{ID: "a1", Number: 1, Title: "Big request", IntegrationBranch: integration}
	trunkRoot := Item{ID: "a1", Number: 1, Title: "Big request"}
	single := Item{Number: 3, Title: "Fix it"}

	tests := []struct {
		name   string
		item   Item
		root   Item
		prBase string
		want   string
	}{
		{name: "a part with no pull request yet", item: part, root: root, want: integration},
		{name: "a part whose pull request is on the integration branch", item: part, root: root, prBase: integration, want: integration},
		{name: "a part whose pull request was opened against main", item: part, root: root, prBase: "main"},
		{name: "a part of an assembly on trunk", item: part, root: trunkRoot},
		{name: "an item with no parent", item: single, root: root},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			branch, base := RunBase(tt.item, tt.root, tt.prBase)
			if branch != tt.item.Branch() || base != tt.want {
				t.Errorf("RunBase = %q, %q; want %q, %q", branch, base, tt.item.Branch(), tt.want)
			}
		})
	}
}
