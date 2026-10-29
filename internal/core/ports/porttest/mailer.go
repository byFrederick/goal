package porttest

import (
	"context"
	"sync"

	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Mailer = (*Mailer)(nil)

// Mailer records the messages it is given. Fail, when set, decides each send's error first: a
// non-nil error fails the send and records nothing.
type Mailer struct {
	mu   sync.Mutex
	sent []ports.Message
	Fail func(m ports.Message) error
}

// NewMailer returns a mailer that has sent nothing and fails nothing.
func NewMailer() *Mailer { return &Mailer{} }

// Send implements ports.Mailer.
func (m *Mailer) Send(_ context.Context, msg ports.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Fail != nil {
		if err := m.Fail(msg); err != nil {
			return err
		}
	}

	m.sent = append(m.sent, msg)

	return nil
}

// Sent returns the messages sent so far, in order.
func (m *Mailer) Sent() []ports.Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]ports.Message(nil), m.sent...)
}
