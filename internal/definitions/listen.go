package definitions

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Listen keeps the cache current until ctx ends: on every notice that a factory's current
// version moved, that it was paused or resumed, created or deleted, or that an organization was
// deleted (whose lines it then drops), it reloads that organization, and on a resync (the first
// notice, and after a lost connection) every organization. Draft notices change nothing that runs. Notices that arrive while a reload
// runs are folded into the next one. It returns what notices.Listen returns.
func (c *Cache) Listen(ctx context.Context, notices ports.DefinitionNotices) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	w := &wanted{signal: make(chan struct{}, 1), orgs: map[domain.OrgID]bool{}}

	var wg sync.WaitGroup

	wg.Go(func() { c.follow(ctx, w) })

	err := notices.Listen(ctx, w.add)

	cancel()
	wg.Wait()

	return err
}

// follow reloads what w asks for, one reload at a time, until ctx ends.
func (c *Cache) follow(ctx context.Context, w *wanted) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.signal:
		}

		all, orgs := w.take()

		var only []domain.OrgID
		if !all {
			only = orgs
		}

		if !all && len(orgs) == 0 {
			continue
		}

		if _, err := c.reloadOrgs(ctx, only); err != nil && ctx.Err() == nil {
			c.log.WarnContext(ctx, "reload definitions", slog.Any("error", err))
		}
	}
}

// wanted collects what the notices ask to reload until follow takes it.
type wanted struct {
	mu     sync.Mutex
	all    bool
	orgs   map[domain.OrgID]bool
	signal chan struct{} // one pending wake-up at most
}

// add records what n asks for and wakes follow; it never blocks.
func (w *wanted) add(n ports.DefinitionNotice) {
	w.mu.Lock()

	switch {
	case n.Resync:
		w.all = true
	case n.Kind == ports.NoticeCurrent || n.Kind == ports.NoticeFactory || n.Kind == ports.NoticeOrg:
		w.orgs[n.OrgID] = true
	default:
		w.mu.Unlock()
		return
	}

	w.mu.Unlock()

	select {
	case w.signal <- struct{}{}:
	default:
	}
}

// take returns and clears what was asked for.
func (w *wanted) take() (all bool, orgs []domain.OrgID) {
	w.mu.Lock()
	defer w.mu.Unlock()

	all, orgs = w.all, slices.Sorted(maps.Keys(w.orgs))
	w.all = false
	clear(w.orgs)

	return all, orgs
}
