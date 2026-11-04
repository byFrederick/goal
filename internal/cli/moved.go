package cli

import "context"

// movedCommand is a command ADR 0039 decision 21 removed, kept for one release: with any
// arguments it calls no API and prints the one line saying what replaced it.
func movedCommand(line string) command {
	return func(_ context.Context, out *printer, _ func(string) string, _ []string) error {
		out.printf("%s\n", line)

		return out.err
	}
}

// The removed commands and what replaced each.
var (
	judgeMoved   = movedCommand("goal judge moved to goal jonah: Jonah learns from the line, so there are no judges")
	evalMoved    = movedCommand("goal eval moved to goal test: kept tests replace eval cases")
	bakeOffMoved = movedCommand("goal bake-off moved to goal agent try-model: Jonah tries another model on past items")
)
