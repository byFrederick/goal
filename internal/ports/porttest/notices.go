package porttest

import (
	"context"
	"sync"

	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.ItemNotices = (*ItemNotices)(nil)

// ItemNotices delivers the notices a test publishes to every Listen call in progress, after
// the Resync notice each Listen call starts with.
type ItemNotices struct {
	mu        sync.Mutex
	listeners map[int]func(ports.ItemNotice)
	next      int
	changes   changes
}

// NewItemNotices returns a feed nobody listens to yet.
func NewItemNotices() *ItemNotices {
	return &ItemNotices{listeners: map[int]func(ports.ItemNotice){}}
}

// Listen implements ports.ItemNotices.
func (n *ItemNotices) Listen(ctx context.Context, deliver func(ports.ItemNotice)) error {
	n.mu.Lock()
	id := n.next
	n.next++
	n.listeners[id] = deliver
	deliver(ports.ItemNotice{Resync: true}) // under the lock, so before anything published
	n.mu.Unlock()
	n.changes.signal()

	<-ctx.Done()

	n.mu.Lock()
	delete(n.listeners, id)
	n.mu.Unlock()
	n.changes.signal()

	return nil
}

// Publish delivers notices, in order, to every current listener before it returns.
func (n *ItemNotices) Publish(notices ...ports.ItemNotice) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for _, notice := range notices {
		for _, deliver := range n.listeners {
			deliver(notice)
		}
	}
}

// AwaitListeners blocks until count Listen calls are in progress or ctx ends.
func (n *ItemNotices) AwaitListeners(ctx context.Context, count int) error {
	return n.changes.await(ctx, func() bool {
		n.mu.Lock()
		defer n.mu.Unlock()

		return len(n.listeners) == count
	})
}
