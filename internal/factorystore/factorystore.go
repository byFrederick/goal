// Package factorystore reads the bucket layouts factory definitions had before versions
// (2.2-B): every object an organization owns lives under orgs/<org-id>/ (OrgPrefix), and a
// factory was one prefix in it, orgs/<org-id>/factories/<metadata.id>/ (Prefix), holding
// factory.yaml, agents/, skills/, contracts/ and evals/. goald imports each such factory once as
// version 1 of itself (definitions.ImportLegacy) and ignores the prefixes afterwards: definitions
// now change through goald's API (goal factory push, the builder). knowledge/ under the same
// prefix is reserved for the factory's knowledge base, which this package never reads. The
// organization id always comes from the database, never from a definition.
//
// List reads an organization's factories as the import needs them. MigrateLegacy moves the
// factories of the layout before organizations (factories/<id>/) into one organization, and
// DeleteOrg empties a deleted organization's prefix.
package factorystore

import (
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	// OrgsRoot is the prefix under which every organization's objects live.
	OrgsRoot = "orgs/"
	// factoriesDir is the folder of an organization's prefix that holds its factories.
	factoriesDir = "factories/"
	// KnowledgeDir is the folder inside a factory reserved for its knowledge base. Definitions
	// never include it: reads skip it.
	KnowledgeDir = "knowledge/"
)

// OrgPrefix is an organization's folder in the bucket; every object the organization owns is
// under it. It is the only place that prefix is built.
func OrgPrefix(org domain.OrgID) string { return OrgsRoot + string(org) + "/" }

// Prefix is a factory's folder in the bucket. It uses the ids, so renames never move objects.
func Prefix(org domain.OrgID, id string) string { return OrgPrefix(org) + factoriesDir + id + "/" }

// Catalog reads factory definitions in one bucket.
type Catalog struct {
	store ports.ObjectStore
}

// New returns a catalog over store.
func New(store ports.ObjectStore) *Catalog { return &Catalog{store: store} }

// Snapshot is one factory's files as read from the bucket in one pass, each at a known object
// version, so a caller can record or pin exactly what it loaded.
type Snapshot struct {
	Org domain.OrgID
	ID  string
	// Files are the objects read, with keys relative to the factory's root, ordered by key.
	Files []ports.ObjectInfo
	files config.Files
}

// FS returns the snapshot as a read-only file tree rooted at the factory's folder.
func (s *Snapshot) FS() fs.FS { return s.files }

// open reads the definition of org's factory with this id from its listed objects (everything
// but knowledge/). It returns an error wrapping ports.ErrNotFound when they hold no factory.yaml.
func (c *Catalog) open(ctx context.Context, org domain.OrgID, id string, objects []ports.ObjectInfo) (*Snapshot, error) {
	prefix := Prefix(org, id)
	s := &Snapshot{Org: org, ID: id, files: config.Files{}}

	for _, o := range objects {
		rel := strings.TrimPrefix(o.Key, prefix)
		if strings.HasSuffix(rel, "/") || strings.HasPrefix(rel, KnowledgeDir) {
			continue // folder markers and the knowledge base are not part of the definition
		}

		if !fs.ValidPath(rel) {
			return nil, fmt.Errorf("factory %s: object key %q is not a valid file path", id, o.Key)
		}

		// Read the listed version, not the latest: a concurrent write cannot mix two definitions.
		obj, err := c.store.Get(ctx, o.Key, o.Version)
		if err != nil {
			return nil, err
		}

		s.files[rel] = obj.Body
		s.Files = append(s.Files, ports.ObjectInfo{Key: rel, Version: obj.Version, Size: obj.Size})
	}

	if _, ok := s.files[config.FileName]; !ok {
		return nil, fmt.Errorf("factory %s: no %s%s: %w", id, prefix, config.FileName, ports.ErrNotFound)
	}

	return s, nil
}

// Entry is one factory found in the bucket. Factory is nil when its definition does not load;
// Err then says why.
type Entry struct {
	ID       string
	Snapshot *Snapshot
	Factory  *config.Factory
	Err      error
}

// List reads and validates every factory of org, ordered by id.
func (c *Catalog) List(ctx context.Context, org domain.OrgID) ([]Entry, error) {
	root := OrgPrefix(org) + factoriesDir

	objects, err := c.store.List(ctx, root)
	if err != nil {
		return nil, err
	}

	byID := map[string][]ports.ObjectInfo{}

	for _, o := range objects {
		id, _, ok := strings.Cut(strings.TrimPrefix(o.Key, root), "/")
		if ok && id != "" {
			byID[id] = append(byID[id], o)
		}
	}

	entries := make([]Entry, 0, len(byID))

	for id, objs := range byID {
		e := Entry{ID: id}

		e.Snapshot, e.Err = c.open(ctx, org, id, objs)
		if e.Err == nil {
			e.Factory, e.Err = config.Load(e.Snapshot.FS())
		}

		if e.Err == nil && e.Factory.Metadata.ID != id {
			e.Factory, e.Err = nil, fmt.Errorf("metadata.id %s does not match its prefix %s", e.Factory.Metadata.ID, Prefix(org, id))
		}

		entries = append(entries, e)
	}

	slices.SortFunc(entries, func(a, b Entry) int { return cmp.Compare(a.ID, b.ID) })

	return entries, nil
}
