package porttest

import (
	"context"
	"sync"

	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.DirectMessenger = (*DirectMessenger)(nil)

// DirectSend is one message a DirectMessenger sent, with the token it was sent with.
type DirectSend struct {
	Token   string
	Message ports.DirectMessage
}

// DirectMessenger records the messages it is given. Fail, when set, decides each send's error
// first: a non-nil error fails the send and records nothing.
type DirectMessenger struct {
	mu   sync.Mutex
	sent []DirectSend
	Fail func(token string, m ports.DirectMessage) error
}

// NewDirectMessenger returns a messenger that has sent nothing and fails nothing.
func NewDirectMessenger() *DirectMessenger { return &DirectMessenger{} }

// Send implements ports.DirectMessenger.
func (d *DirectMessenger) Send(_ context.Context, token string, m ports.DirectMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.Fail != nil {
		if err := d.Fail(token, m); err != nil {
			return err
		}
	}

	d.sent = append(d.sent, DirectSend{Token: token, Message: m})

	return nil
}

// Sent returns the messages sent so far, in order.
func (d *DirectMessenger) Sent() []DirectSend {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]DirectSend(nil), d.sent...)
}
