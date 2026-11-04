package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// goal judge, goal eval and goal bake-off, with any arguments, call no API and print one line
// naming the command that replaced them, exiting 0 (ADR 0039 decision 21). No GOAL_URL is set,
// so a command that called goald would fail.
func TestMovedCommands(t *testing.T) {
	t.Parallel()

	for line, want := range map[string]string{
		"judge":                           "goal judge moved to goal jonah: Jonah learns from the line, so there are no judges\n",
		"judge list --factory acme":       "goal judge moved to goal jonah: Jonah learns from the line, so there are no judges\n",
		"judge try followed":              "goal judge moved to goal jonah: Jonah learns from the line, so there are no judges\n",
		"eval":                            "goal eval moved to goal test: kept tests replace eval cases\n",
		"eval run --draft":                "goal eval moved to goal test: kept tests replace eval cases\n",
		"eval show 019a --json":           "goal eval moved to goal test: kept tests replace eval cases\n",
		"bake-off":                        "goal bake-off moved to goal agent try-model: Jonah tries another model on past items\n",
		"bake-off run writer --model m":   "goal bake-off moved to goal agent try-model: Jonah tries another model on past items\n",
		"bake-off apply 019a --factory f": "goal bake-off moved to goal agent try-model: Jonah tries another model on past items\n",
	} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			getenv := func(string) string { return "" }
			code := Run(context.Background(), strings.Fields(line), strings.NewReader(""), &stdout, &stderr, getenv, fixedBuildInfo)

			if code != 0 || stdout.String() != want || stderr.Len() != 0 {
				t.Errorf("goal %s = exit %d, stdout %q, stderr %q; want exit 0, stdout %q and no stderr", line, code, stdout.String(), stderr.String(), want)
			}
		})
	}
}
