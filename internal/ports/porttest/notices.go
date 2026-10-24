package porttest

import (
	"context"
	"sync"

	"github.com/byFrederick/goal/internal/ports"
)

var (
	_ ports.ItemNotices       = (*ItemNotices)(nil)
	_ ports.DefinitionNotices = (*DefinitionNotices)(nil)
)

// ItemNotices delivers the notices a test publishes to every Listen call in progress, after
// the Resync notice each Listen call starts with.
type ItemNotices struct {
	feed feed[ports.ItemNotice]
}

// NewItemNotices returns a feed nobody listens to yet.
func NewItemNotices() *ItemNotices { return &ItemNotices{} }

// Listen implements ports.ItemNotices.
func (n *ItemNotices) Listen(ctx context.Context, deliver func(ports.ItemNotice)) error {
	return n.feed.listen(ctx, deliver, ports.ItemNotice{Resync: true})
}

// Publish delivers notices, in order, to every current listener before it returns.
func (n *ItemNotices) Publish(notices ...ports.ItemNotice) { n.feed.publish(notices...) }

// AwaitListeners blocks until count Listen calls are in progress or ctx ends.
func (n *ItemNotices) AwaitListeners(ctx context.Context, count int) error {
	return n.feed.awaitListeners(ctx, count)
}

// DefinitionNotices delivers definition notices to every Listen call in progress, after the
// Resync notice each Listen call starts with. Definitions publishes one for every change it
// commits; tests may publish their own.
type DefinitionNotices struct {
	feed feed[ports.DefinitionNotice]
}

// NewDefinitionNotices returns a feed nobody listens to yet.
func NewDefinitionNotices() *DefinitionNotices { return &DefinitionNotices{} }

// Listen implements ports.DefinitionNotices.
func (n *DefinitionNotices) Listen(ctx context.Context, deliver func(ports.DefinitionNotice)) error {
	return n.feed.listen(ctx, deliver, ports.DefinitionNotice{Resync: true})
}

// Publish delivers notices, in order, to every current listener before it returns.
func (n *DefinitionNotices) Publish(notices ...ports.DefinitionNotice) { n.feed.publish(notices...) }

// AwaitListeners blocks until count Listen calls are in progress or ctx ends.
func (n *DefinitionNotices) AwaitListeners(ctx context.Context, count int) error {
	return n.feed.awaitListeners(ctx, count)
}

// feed fans notices out to the listeners in progress.
type feed[N any] struct {
	mu        sync.Mutex
	listeners map[int]func(N)
	next      int
	changes   changes
}

func (f *feed[N]) listen(ctx context.Context, deliver func(N), resync N) error {
	f.mu.Lock()

	if f.listeners == nil {
		f.listeners = map[int]func(N){}
	}

	id := f.next
	f.next++
	f.listeners[id] = deliver
	deliver(resync) // under the lock, so before anything published
	f.mu.Unlock()
	f.changes.signal()

	<-ctx.Done()

	f.mu.Lock()
	delete(f.listeners, id)
	f.mu.Unlock()
	f.changes.signal()

	return nil
}

func (f *feed[N]) publish(notices ...N) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, notice := range notices {
		for _, deliver := range f.listeners {
			deliver(notice)
		}
	}
}

func (f *feed[N]) awaitListeners(ctx context.Context, count int) error {
	return f.changes.await(ctx, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()

		return len(f.listeners) == count
	})
}
