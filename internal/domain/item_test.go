package domain

import "testing"

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
