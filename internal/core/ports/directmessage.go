package ports

import (
	"context"
	"errors"
)

// DirectMessenger sends one person a direct message in a chat workspace (ADR 0023, decision 5):
// the Slack adapter, with the token of the organization's Slack connection.
type DirectMessenger interface {
	// Send delivers m with the workspace's bot token. An error that wraps ErrDMUserNotFound
	// means the workspace has no person with m.To; one that wraps ErrDMPermanent will fail the
	// same way again, such as a revoked token; any other error is temporary.
	Send(ctx context.Context, token string, m DirectMessage) error
}

// DirectMessage is one plain-text direct message to the person whose email is To.
type DirectMessage struct {
	To   string
	Text string
}

var (
	// ErrDMPermanent marks a send that will not succeed if retried.
	ErrDMPermanent = errors.New("direct message refused for good")
	// ErrDMUserNotFound is a recipient the workspace does not know; it is permanent too.
	ErrDMUserNotFound = errors.New("direct message recipient not found")
)

// DMError is a failed send with a short fixed code that names no person, quotes no reply and
// holds no token, such as slack-user-not-found, so it may be logged and stored.
type DMError struct {
	Code      string
	Permanent bool
	NotFound  bool
}

func (e *DMError) Error() string { return "direct message: " + e.Code }

// Is reports a permanent DMError as ErrDMPermanent and one whose recipient was not found as
// ErrDMUserNotFound (and ErrDMPermanent).
func (e *DMError) Is(target error) bool {
	switch target {
	case ErrDMUserNotFound:
		return e.NotFound
	case ErrDMPermanent:
		return e.Permanent || e.NotFound
	default:
		return false
	}
}
