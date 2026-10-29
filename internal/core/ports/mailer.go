package ports

import (
	"context"
	"errors"
)

// Mailer sends email (ADR 0023, decision 4). goald has one only while GOAL_SMTP_URL is set.
type Mailer interface {
	// Send delivers m. An error that wraps ErrMailPermanent will fail the same way again, such as
	// a recipient the server refuses; any other error is temporary, and a retry may succeed.
	Send(ctx context.Context, m Message) error
}

// Message is one email: a plain-text body and a minimal HTML copy of it.
type Message struct {
	To      string // one plain address
	Subject string
	Text    string
	HTML    string
}

// ErrMailPermanent marks a send that will not succeed if retried.
var ErrMailPermanent = errors.New("mail refused for good")

// MailError is a failed send with a short fixed code that names no address and quotes no server
// reply, such as smtp-rcpt-550, so it may be logged and stored. Permanent sends match
// ErrMailPermanent.
type MailError struct {
	Code      string
	Permanent bool
}

func (e *MailError) Error() string { return "mail: " + e.Code }

// Is reports a permanent MailError as ErrMailPermanent.
func (e *MailError) Is(target error) bool { return e.Permanent && target == ErrMailPermanent }
