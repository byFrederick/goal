package domain

import (
	"reflect"
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
