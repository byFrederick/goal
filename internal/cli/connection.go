package cli

import (
	"context"
)

// errConnectionMoved is every goal connection command line: connections are secrets, environment
// files and the Access page now (ADR 0037 decision 17), and goal connection says so for one
// release, then goes away.
var errConnectionMoved = &movedError{msg: "goal connection moved: see Access (goal access), Secrets (goal secret) and the factory's environment files"}

// connectionCommand runs goal connection …, which moved to goal access, goal secret and the
// factory's environment files.
func connectionCommand(context.Context, *printer, func(string) string, []string) error {
	return errConnectionMoved
}

// The verbs commands share.
const (
	verbCreate = "create"
	verbDelete = "delete"
)
