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
// first: a non-nil error fails the send and records nothing. CheckWith, when set, answers each
// check; unset, every token works in workspace "acme".
type DirectMessenger struct {
	mu        sync.Mutex
	sent      []DirectSend
	checked   []string
	Fail      func(token string, m ports.DirectMessage) error
	CheckWith func(token string) (string, error)
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

// Check implements ports.DirectMessenger and records the token checked.
func (d *DirectMessenger) Check(_ context.Context, token string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.checked = append(d.checked, token)

	if d.CheckWith != nil {
		return d.CheckWith(token)
	}

	return "acme", nil
}

// Checked returns the tokens checked so far, in order.
func (d *DirectMessenger) Checked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]string(nil), d.checked...)
}
