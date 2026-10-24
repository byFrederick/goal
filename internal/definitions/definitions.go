// Package definitions is goald's view of the factory definitions in the bucket: a cache of the
// current, valid definition of every factory of every live organization, refreshed from
// internal/factorystore. It serves the readers that need a whole definition rather than the
// engine's domain.Line: the API (api.Factories), the MCP gateway (gateway.Definitions),
// contract validation (ports.Contracts) and the Docker runner's agent files (docker.AgentFiles).
//
// Every read is scoped to the organization in its context (domain.WithOrg): a factory of
// another organization is not found, and a read without an organization fails with
// domain.ErrNoOrg. A factory's organization is where its definition is stored in the bucket.
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

// Catalog lists an organization's factories in the bucket; *factorystore.Catalog satisfies it.
type Catalog interface {
	List(ctx context.Context, org domain.OrgID) ([]factorystore.Entry, error)
}

// Orgs lists the organizations whose factories the cache loads; ports.Tenancy satisfies it.
// Factories of an organization that does not exist or was deleted are not loaded.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// Loaded is a factory that is loaded, with the organization that owns it.
type Loaded struct {
	Org     domain.OrgID
	Factory *config.Factory
}

// Line is the factory's line for the engine, in its organization.
func (l Loaded) Line() domain.Line {
	line := l.Factory.Line()
	line.OrgID = l.Org

	return line
}

// LoadFunc is told the factories that are loaded after every refresh, before anyone can look
// them up, so goald can register them in the store and hand their lines to the engine.
type LoadFunc func(ctx context.Context, factories []Loaded) error

// loaded is one factory's definition with its organization and the files it was read from.
type loaded struct {
	org      domain.OrgID
	factory  *config.Factory
	snapshot *factorystore.Snapshot
}

// Cache holds the current definition of every valid factory in the bucket. It is safe for
// concurrent use.
type Cache struct {
	catalog Catalog
	orgs    Orgs
	onLoad  LoadFunc
	log     *slog.Logger

	refresh sync.Mutex // one refresh at a time, so onLoad sees refreshes in order

	mu        sync.RWMutex
	factories map[domain.FactoryID]loaded
}

// New returns an empty cache over catalog for the organizations orgs lists; call Refresh to
// fill it. onLoad may be nil.
func New(catalog Catalog, orgs Orgs, onLoad LoadFunc, log *slog.Logger) *Cache {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Cache{catalog: catalog, orgs: orgs, onLoad: onLoad, log: log, factories: map[domain.FactoryID]loaded{}}
}

// Refresh reads every factory of every live organization and makes the valid ones current. A
// factory whose definition does not load is logged and keeps its last valid definition, if it
// had one; a factory removed from the bucket, or whose organization is gone, is dropped. A
// factory id stored in two organizations stays with the first by id (or the one it was loaded
// in) and is refused in the other. It returns the factories now loaded.
func (c *Cache) Refresh(ctx context.Context) ([]Loaded, error) {
	c.refresh.Lock()
	defer c.refresh.Unlock()

	orgs, err := c.orgs.ActiveOrgs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}

	var entries []entry

	for _, org := range slices.Sorted(slices.Values(orgs)) {
		es, err := c.catalog.List(ctx, org)
		if err != nil {
			return nil, fmt.Errorf("list factories of organization %s: %w", org, err)
		}

		for _, e := range es {
			entries = append(entries, entry{org: org, Entry: e})
		}
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

// entry is a factory found in an organization's prefix.
type entry struct {
	factorystore.Entry

	org domain.OrgID
}

// merge is the next set of loaded factories: every valid entry, and the current definition of
// every entry that does not load.
func (c *Cache) merge(ctx context.Context, entries []entry) map[domain.FactoryID]loaded {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// The owner of an id is the organization it is loaded in when that one still stores it, and
	// otherwise the first that stores it (entries come by organization).
	owner := map[domain.FactoryID]domain.OrgID{}

	for _, e := range entries {
		id := domain.FactoryID(e.ID)
		if _, claimed := owner[id]; !claimed {
			owner[id] = e.org
		}

		if prev, ok := c.factories[id]; ok && prev.org == e.org {
			owner[id] = e.org
		}
	}

	next := make(map[domain.FactoryID]loaded, len(entries))

	for _, e := range entries {
		id := domain.FactoryID(e.ID)
		log := c.log.With(slog.String("org_id", string(e.org)), slog.String("factory", e.ID))

		if owner[id] != e.org {
			log.ErrorContext(ctx, "factory id is stored in two organizations; ignored here", slog.String("loaded_in", string(owner[id])))
			continue
		}

		if e.Err == nil {
			next[id] = loaded{org: e.org, factory: e.Factory, snapshot: e.Snapshot}
			continue
		}

		prev, ok := c.factories[id]
		log.WarnContext(ctx, "factory definition does not load", slog.Bool("keeps_previous", ok), slog.Any("error", e.Err))

		if ok {
			next[id] = prev
		}
	}

	return next
}

// Factories returns the loaded factories of the organization in ctx, ordered by name; none
// without one.
func (c *Cache) Factories(ctx context.Context) []*config.Factory {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []*config.Factory

	for _, l := range sorted(c.factories) {
		if l.Org == org {
			out = append(out, l.Factory)
		}
	}

	return out
}

// Factory returns the current definition of a factory of the organization in ctx, or an error
// wrapping ports.ErrNotFound. It implements gateway.Definitions.
func (c *Cache) Factory(ctx context.Context, id domain.FactoryID) (*config.Factory, error) {
	l, err := c.get(ctx, id)
	if err != nil {
		return nil, err
	}

	return l.factory, nil
}

// Lookup finds a loaded factory of the organization in ctx by id or metadata.name. When nothing
// matches it refreshes once, so a factory pushed a moment ago is found, then returns an error
// wrapping ports.ErrNotFound.
func (c *Cache) Lookup(ctx context.Context, ref string) (*config.Factory, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("factory %q: %w", ref, domain.ErrNoOrg)
	}

	if f := c.find(org, ref); f != nil {
		return f, nil
	}

	if _, err := c.Refresh(ctx); err != nil {
		return nil, err
	}

	if f := c.find(org, ref); f != nil {
		return f, nil
	}

	return nil, fmt.Errorf("factory %q: %w", ref, ports.ErrNotFound)
}

func (c *Cache) find(org domain.OrgID, ref string) *config.Factory {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if l, ok := c.factories[domain.FactoryID(ref)]; ok && l.org == org {
		return l.factory
	}

	for _, l := range c.factories {
		if l.org == org && l.factory.Metadata.Name == ref {
			return l.factory
		}
	}

	return nil
}

// get returns a loaded factory of the organization in ctx.
func (c *Cache) get(ctx context.Context, id domain.FactoryID) (loaded, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return loaded{}, fmt.Errorf("factory %s: %w", id, domain.ErrNoOrg)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	l, ok := c.factories[id]
	if !ok || l.org != org {
		return loaded{}, fmt.Errorf("factory %s is not loaded: %w", id, ports.ErrNotFound)
	}

	return l, nil
}

func sorted(m map[domain.FactoryID]loaded) []Loaded {
	out := make([]Loaded, 0, len(m))
	for _, l := range m {
		out = append(out, Loaded{Org: l.org, Factory: l.factory})
	}

	slices.SortFunc(out, func(a, b Loaded) int {
		return cmp.Or(cmp.Compare(a.Factory.Metadata.Name, b.Factory.Metadata.Name), cmp.Compare(a.Org, b.Org),
			cmp.Compare(a.Factory.Metadata.ID, b.Factory.Metadata.ID))
	})

	return out
}
