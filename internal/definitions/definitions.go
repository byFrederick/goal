// Package definitions is goald's read side of factory definitions: a cache of the published
// version every factory of every live organization runs (its current version) and of the
// older versions runs started on, read from ports.DefinitionStore. It serves the readers that
// need a definition: the engine's lines (LoadFunc), the API (api.Factories), the MCP gateway
// (gateway.Definitions), contract validation (ports.Contracts) and the Docker runner's agent
// files (docker.AgentFiles). A run reads the version it started on (TaskSpec.
// DefinitionVersionID, the gateway token's v claim), whatever was published meanwhile.
//
// Published versions never change, so a loaded version is kept by id: current versions while
// they are current, older ones in a least-recently-used set bounded by Config.CacheBytes. A
// version is read once from the store, every file verified against its hash; files a factory's
// next version shares with the one it replaces are not read again. Listen reloads an
// organization's current versions on every notice of a publish, restore, pause or resume, and
// everything on a resync; Refresh does the same for every organization, as a safety net behind
// the notices.
//
// Every read is scoped to the organization in its context (domain.WithOrg): a factory or a
// version of another organization is not found, and a read without an organization fails with
// domain.ErrNoOrg.
//
// The cache is a convenience, not state: every goald instance rebuilds it from the store, and
// a current version that fails to load in this goald keeps the previous one in use.
package definitions

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Orgs lists the organizations whose factories the cache loads; ports.Tenancy satisfies it.
// Factories of an organization that does not exist or was deleted are not loaded.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// LoadFunc is told the lines of every current version after each reload, before anyone can
// look them up, so goald can hand them to the engine (engine.SetLines). When it fails the
// reload is dropped and the cache keeps what it had.
type LoadFunc func(ctx context.Context, lines []domain.Line) error

// DefaultCacheBytes bounds the files of the versions kept besides the current ones.
const DefaultCacheBytes = 64 << 20

// Config tunes the cache; the zero value takes the defaults.
type Config struct {
	// CacheBytes bounds the file content of the versions kept for runs that started on a
	// version that is no longer current (default DefaultCacheBytes). Current versions are
	// always kept.
	CacheBytes int64
}

// version is one published definition version, loaded and valid.
type version struct {
	id       domain.DefinitionVersionID
	org      domain.OrgID
	number   int
	manifest domain.Manifest
	factory  *config.Factory
	files    config.Files
	size     int64
}

// current is the version a factory runs and whether the factory is paused.
type current struct {
	*version

	paused bool
}

// line is the engine's line of the version, in its organization.
func (c current) line() domain.Line {
	l := c.factory.Line()
	l.OrgID, l.VersionID, l.Number, l.Paused = c.org, c.id, c.number, c.paused

	return l
}

// Cache holds the current version of every published factory and the versions runs pinned. It
// is safe for concurrent use.
type Cache struct {
	store  ports.DefinitionStore
	orgs   Orgs
	onLoad LoadFunc
	log    *slog.Logger

	reload sync.Mutex // one reload at a time, so onLoad sees them in order
	// misses folds the reloads of lookups that miss together, by organization.
	misses singleflight.Group

	mu      sync.RWMutex
	current map[domain.FactoryID]current
	byID    map[domain.DefinitionVersionID]*version // the current versions, by id
	live    map[domain.OrgID]bool                   // the organizations the last reload found live

	pinned *lru // versions no longer current
}

// New returns an empty cache over store for the organizations orgs lists; call Refresh to fill
// it. onLoad and log may be nil.
func New(store ports.DefinitionStore, orgs Orgs, onLoad LoadFunc, cfg Config, log *slog.Logger) *Cache {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	if cfg.CacheBytes <= 0 {
		cfg.CacheBytes = DefaultCacheBytes
	}

	return &Cache{
		store: store, orgs: orgs, onLoad: onLoad, log: log,
		current: map[domain.FactoryID]current{}, byID: map[domain.DefinitionVersionID]*version{}, live: map[domain.OrgID]bool{},
		pinned: newLRU(cfg.CacheBytes),
	}
}

// Refresh reloads the current versions of every live organization's factories and returns
// the lines now loaded. An organization whose factories cannot be listed keeps what it had and
// its error is returned with the lines; the factories of an organization that is gone are
// dropped.
func (c *Cache) Refresh(ctx context.Context) ([]domain.Line, error) {
	return c.reloadOrgs(ctx, nil)
}

// reloadOrgs reloads the organizations in only, or every live one when only is nil.
func (c *Cache) reloadOrgs(ctx context.Context, only []domain.OrgID) ([]domain.Line, error) {
	c.reload.Lock()
	defer c.reload.Unlock()

	active, err := c.orgs.ActiveOrgs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}

	live := make(map[domain.OrgID]bool, len(active))
	for _, o := range active {
		live[o] = true
	}

	c.mu.RLock()
	prev := maps.Clone(c.current)
	c.mu.RUnlock()

	next := maps.Clone(prev)
	maps.DeleteFunc(next, func(_ domain.FactoryID, cur current) bool { return !live[cur.org] })

	targets := active
	if only != nil {
		targets = slices.DeleteFunc(slices.Clone(only), func(o domain.OrgID) bool { return !live[o] })
	}

	var errs []error

	for _, org := range slices.Compact(slices.Sorted(slices.Values(targets))) {
		if err := c.reloadOrg(ctx, org, next); err != nil {
			errs = append(errs, err)
		}
	}

	lines := linesOf(next)

	if c.onLoad != nil {
		if err := c.onLoad(ctx, lines); err != nil {
			return nil, fmt.Errorf("load lines: %w", err)
		}
	}

	byID := make(map[domain.DefinitionVersionID]*version, len(next))
	for _, cur := range next {
		byID[cur.id] = cur.version
	}

	c.mu.Lock()
	c.current, c.byID, c.live = next, byID, live
	c.mu.Unlock()

	// A version that stopped being current may still have runs on it.
	for id, cur := range prev {
		if now, ok := next[id]; !ok || now.version != cur.version {
			c.pinned.add(cur.version)
		}
	}

	return lines, errors.Join(errs...)
}

// reloadOrg sets the current versions of org's factories in next: a version already loaded is
// kept, a new one is loaded, and a factory that has no current version any more (deleted, or
// never published) is dropped. A version that fails to load leaves the factory on the one it
// had, if any.
func (c *Cache) reloadOrg(ctx context.Context, org domain.OrgID, next map[domain.FactoryID]current) error {
	octx := domain.WithOrg(ctx, org)

	recs, err := c.store.FactoryRecords(octx)
	if err != nil {
		return fmt.Errorf("list factories of organization %s: %w", org, err)
	}

	running := map[domain.FactoryID]bool{}

	for _, r := range recs {
		if r.Current == nil {
			continue
		}

		running[r.ID] = true
		prev, had := next[r.ID]

		if had && prev.id == r.Current.ID {
			next[r.ID] = current{version: prev.version, paused: r.Paused()}
			continue
		}

		log := c.log.With(slog.String("org_id", string(org)), slog.String("factory", string(r.ID)), slog.Int("number", r.Current.Number))
		start := time.Now()

		v, err := c.load(octx, r.Current.ID, prev.version)
		if err != nil {
			log.WarnContext(ctx, "current definition version does not load in this goald", slog.Bool("keeps_previous", had),
				slog.Any("error", err))

			if had {
				next[r.ID] = current{version: prev.version, paused: r.Paused()}
			} else {
				delete(next, r.ID)
			}

			continue
		}

		log.InfoContext(ctx, "definitions reloaded", slog.Int("files", len(v.files)), slog.Int64("ms", time.Since(start).Milliseconds()))

		next[r.ID] = current{version: v, paused: r.Paused()}
	}

	maps.DeleteFunc(next, func(id domain.FactoryID, cur current) bool { return cur.org == org && !running[id] })

	return nil
}

// load reads a published version of the organization in ctx and validates it. Files with the
// hash of one of reuse's files are taken from reuse (which may be nil) instead of the store.
func (c *Cache) load(ctx context.Context, id domain.DefinitionVersionID, reuse *version) (*version, error) {
	dv, err := c.store.Version(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("definition version %s: %w", id, err)
	}

	if dv.State != domain.VersionPublished {
		return nil, fmt.Errorf("definition version %s is %s, not published: %w", id, dv.State, ports.ErrNotFound)
	}

	known := map[domain.BlobHash][]byte{}

	if reuse != nil {
		for p, h := range reuse.manifest {
			known[h] = reuse.files[p]
		}
	}

	v := &version{id: dv.ID, org: dv.OrgID, number: dv.Number, manifest: dv.Manifest, files: make(config.Files, len(dv.Manifest))}

	for p, h := range dv.Manifest {
		b, ok := known[h]
		if !ok {
			if b, err = c.store.Blob(ctx, h); err != nil {
				return nil, fmt.Errorf("definition version %s: file %s: %w", id, p, err)
			}

			// The store verifies too; a cache must never serve content under the wrong hash.
			if domain.HashBlob(b) != h {
				return nil, fmt.Errorf("definition version %s: file %s: %w", id, p, ports.ErrBlobCorrupt)
			}

			known[h] = b
		}

		v.files[p] = b
		v.size += int64(len(b))
	}

	if v.factory, err = config.Load(v.files); err != nil {
		return nil, fmt.Errorf("definition version %s: %w", id, err)
	}

	if v.factory.Metadata.ID != string(dv.FactoryID) {
		return nil, fmt.Errorf("definition version %s: metadata.id %s is not its factory %s", id, v.factory.Metadata.ID, dv.FactoryID)
	}

	return v, nil
}

// version returns a published version of the organization in ctx: a current one, one kept for
// runs, or else one read from the store. An organization the last reload did not find live
// (deleted, or unknown) has none.
func (c *Cache) version(ctx context.Context, id domain.DefinitionVersionID) (*version, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("definition version %s: %w", id, domain.ErrNoOrg)
	}

	if id == "" {
		return nil, fmt.Errorf("no definition version: %w", ports.ErrNotFound)
	}

	c.mu.RLock()
	v, ok := c.byID[id]
	live := c.live[org]
	c.mu.RUnlock()

	if !live {
		return nil, fmt.Errorf("definition version %s: organization %s is not live: %w", id, org, ports.ErrNotFound)
	}

	if !ok {
		v, ok = c.pinned.get(id)
	}

	if ok {
		if v.org != org {
			return nil, fmt.Errorf("definition version %s: %w", id, ports.ErrNotFound)
		}

		return v, nil
	}

	v, err := c.load(ctx, id, nil)
	if err != nil {
		return nil, err
	}

	c.pinned.add(v)

	return v, nil
}

// Version returns a published definition version of the organization in ctx, for the run
// that started on it, or an error wrapping ports.ErrNotFound. It implements
// gateway.Definitions.
func (c *Cache) Version(ctx context.Context, id domain.DefinitionVersionID) (*config.Factory, error) {
	v, err := c.version(ctx, id)
	if err != nil {
		return nil, err
	}

	return v.factory, nil
}

// Factories returns the current definitions of the organization in ctx, ordered by name; none
// without one.
func (c *Cache) Factories(ctx context.Context) []*config.Factory {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []*config.Factory

	for _, cur := range sorted(c.current) {
		if cur.org == org {
			out = append(out, cur.factory)
		}
	}

	return out
}

// Lookup finds the current definition of a factory of the organization in ctx by id or by
// metadata.name, without case (as authoring does). On a miss it reloads the organization only
// when the store has a current version of that factory the cache lacks (a publish whose notice
// has not arrived yet), one reload for every lookup that misses meanwhile, so a typo, a stale ref
// or a factory with only a draft costs one read of the organization's factory records. Anything
// else, a store that fails on the way included, is an error wrapping ports.ErrNotFound.
func (c *Cache) Lookup(ctx context.Context, ref string) (*config.Factory, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("factory %q: %w", ref, domain.ErrNoOrg)
	}

	if f := c.find(org, ref); f != nil {
		return f, nil
	}

	if c.catchUp(ctx, org, ref) {
		if f := c.find(org, ref); f != nil {
			return f, nil
		}
	}

	return nil, fmt.Errorf("factory %q: %w", ref, ports.ErrNotFound)
}

// catchUp reloads org when the store names a current version of ref's factory that the cache
// lacks, and reports whether the cache has that version now (reloaded here, or by a concurrent
// lookup since this one missed). A failure is logged: the lookup stays a miss.
func (c *Cache) catchUp(ctx context.Context, org domain.OrgID, ref string) bool {
	log := c.log.With(slog.String("org_id", string(org)), slog.String("factory", ref))

	recs, err := c.store.FactoryRecords(ctx)
	if err != nil {
		log.WarnContext(ctx, "factory lookup cannot read the factory records; answered as not found", slog.Any("error", err))
		return false
	}

	i := slices.IndexFunc(recs, func(r ports.FactoryRecord) bool { return matches(string(r.ID), r.Name, ref) })
	if i < 0 || recs[i].Current == nil {
		return false
	}

	want := recs[i].Current.ID

	// Another lookup's reload brought it in since this one missed: find it again.
	if c.has(want) {
		return true
	}

	// The reload outlives a caller that gives up: others may be waiting on it.
	_, err, _ = c.misses.Do(string(org), func() (any, error) {
		if c.has(want) {
			return nil, nil
		}

		_, err := c.reloadOrgs(context.WithoutCancel(ctx), []domain.OrgID{org})

		return nil, err
	})
	if err != nil {
		log.WarnContext(ctx, "factory lookup reload failed; answered as not found", slog.Any("error", err))
		return false
	}

	return true
}

// has reports whether version id is current in the cache.
func (c *Cache) has(id domain.DefinitionVersionID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, ok := c.byID[id]

	return ok
}

// matches reports whether ref names the factory with this id or name, without case.
func matches(id, name, ref string) bool {
	return strings.EqualFold(id, ref) || strings.EqualFold(name, ref)
}

func (c *Cache) find(org domain.OrgID, ref string) *config.Factory {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cur, ok := c.current[domain.FactoryID(strings.ToLower(ref))]; ok && cur.org == org {
		return cur.factory
	}

	for _, cur := range c.current {
		if cur.org == org && strings.EqualFold(cur.factory.Metadata.Name, ref) {
			return cur.factory
		}
	}

	return nil
}

func sorted(m map[domain.FactoryID]current) []current {
	out := slices.Collect(maps.Values(m))

	slices.SortFunc(out, func(a, b current) int {
		return cmp.Or(cmp.Compare(a.factory.Metadata.Name, b.factory.Metadata.Name), cmp.Compare(a.org, b.org),
			cmp.Compare(a.factory.Metadata.ID, b.factory.Metadata.ID))
	})

	return out
}

func linesOf(m map[domain.FactoryID]current) []domain.Line {
	cur := sorted(m)
	lines := make([]domain.Line, 0, len(cur))

	for _, c := range cur {
		lines = append(lines, c.line())
	}

	return lines
}
