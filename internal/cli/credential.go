package cli

import (
	"context"
)

// errCredentialMoved is every goal credential command line: the organization's credentials are
// secrets now (ADR 0037 decision 17), and goal credential says so for one release, then goes away.
var errCredentialMoved = &movedError{msg: "goal credential moved to goal secret: use goal secret list|show|create|replace|delete|import"}

// credentialCommand runs goal credential …, which moved to goal secret.
func credentialCommand(context.Context, *printer, func(string) string, []string) error {
	return errCredentialMoved
}
