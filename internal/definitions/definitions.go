// Package definitions is goald's view of the factory definitions in the bucket: a cache of the
// current, valid definition of every factory, refreshed from internal/factorystore. It serves
// the readers that need a whole definition rather than the engine's domain.Line: the MCP
// gateway (gateway.Definitions), contract validation (ports.Contracts) and the Docker runner's
// agent files (docker.AgentFiles).
//
// The cache is a convenience, not state: every goald instance rebuilds it from the bucket, and
// a definition that fails to load keeps the last valid one in use until it is fixed.
package definitions

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
)

// Catalog lists the factories in the bucket; *factorystore.Catalog satisfies it.
type Catalog interface {
	List(ctx context.Context) ([]factorystore.Entry, error)
}

// LoadFunc is told the factories that are loaded after every refresh, before anyone can look
// them up, so goald can register them in the store and hand their lines to the engine.
type LoadFunc func(ctx context.Context, factories []*config.Factory) error

// loaded is one factory's definition with the files it was read from.
type loaded struct {
	factory  *config.Factory
	snapshot *factorystore.Snapshot
}

// Cache holds the current definition of every valid factory in the bucket. It is safe for
// concurrent use.
type Cache struct {
	catalog Catalog
	onLoad  LoadFunc
	log     *slog.Logger

	refresh sync.Mutex // one refresh at a time, so onLoad sees refreshes in order

	mu        sync.RWMutex
	factories map[domain.FactoryID]loaded
}

// New returns an empty cache over catalog; call Refresh to fill it. onLoad may be nil.
func New(catalog Catalog, onLoad LoadFunc, log *slog.Logger) *Cache {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Cache{catalog: catalog, onLoad: onLoad, log: log, factories: map[domain.FactoryID]loaded{}}
}

// Refresh reads every factory in the bucket and makes the valid ones current. A factory whose
// definition does not load is logged and keeps its last valid definition, if it had one; a
// factory removed from the bucket is dropped. It returns the factories now loaded.
func (c *Cache) Refresh(ctx context.Context) ([]*config.Factory, error) {
	c.refresh.Lock()
	defer c.refresh.Unlock()

	entries, err := c.catalog.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list factories: %w", err)
	}

	next := c.merge(ctx, entries)
	factories := sorted(next)

	if c.onLoad != nil {
		if err := c.onLoad(ctx, factories); err != nil {
			return nil, fmt.Errorf("load factories: %w", err)
		}
	}

	c.mu.Lock()
	c.factories = next
	c.mu.Unlock()

	return factories, nil
}

// merge is the next set of loaded factories: every valid entry, and the current definition of
// every entry that does not load.
func (c *Cache) merge(ctx context.Context, entries []factorystore.Entry) map[domain.FactoryID]loaded {
	c.mu.RLock()
	defer c.mu.RUnlock()

	next := make(map[domain.FactoryID]loaded, len(entries))

	for _, e := range entries {
		id := domain.FactoryID(e.ID)
		if e.Err == nil {
			next[id] = loaded{factory: e.Factory, snapshot: e.Snapshot}
			continue
		}

		prev, ok := c.factories[id]
		c.log.WarnContext(ctx, "factory definition does not load", slog.String("factory", e.ID),
			slog.Bool("keeps_previous", ok), slog.Any("error", e.Err))

		if ok {
			next[id] = prev
		}
	}

	return next
}

// Factories returns the loaded factories ordered by name.
func (c *Cache) Factories() []*config.Factory {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return sorted(c.factories)
}

// Factory returns the current definition of a factory, or an error wrapping ports.ErrNotFound.
// It implements gateway.Definitions.
func (c *Cache) Factory(_ context.Context, id domain.FactoryID) (*config.Factory, error) {
	l, err := c.get(id)
	if err != nil {
		return nil, err
	}

	return l.factory, nil
}

// Lookup finds a loaded factory by id or metadata.name. When nothing matches it refreshes once,
// so a factory pushed a moment ago is found, then returns an error wrapping ports.ErrNotFound.
func (c *Cache) Lookup(ctx context.Context, ref string) (*config.Factory, error) {
	if f := c.find(ref); f != nil {
		return f, nil
	}

	if _, err := c.Refresh(ctx); err != nil {
		return nil, err
	}

	if f := c.find(ref); f != nil {
		return f, nil
	}

	return nil, fmt.Errorf("factory %q: %w", ref, ports.ErrNotFound)
}

func (c *Cache) find(ref string) *config.Factory {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if l, ok := c.factories[domain.FactoryID(ref)]; ok {
		return l.factory
	}

	for _, l := range c.factories {
		if l.factory.Metadata.Name == ref {
			return l.factory
		}
	}

	return nil
}

func (c *Cache) get(id domain.FactoryID) (loaded, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	l, ok := c.factories[id]
	if !ok {
		return loaded{}, fmt.Errorf("factory %s is not loaded: %w", id, ports.ErrNotFound)
	}

	return l, nil
}

func sorted(m map[domain.FactoryID]loaded) []*config.Factory {
	out := make([]*config.Factory, 0, len(m))
	for _, l := range m {
		out = append(out, l.factory)
	}

	slices.SortFunc(out, func(a, b *config.Factory) int {
		return cmp.Or(cmp.Compare(a.Metadata.Name, b.Metadata.Name), cmp.Compare(a.Metadata.ID, b.Metadata.ID))
	})

	return out
}
