// Package factorystore reads the bucket layouts factory definitions had before versions
// (2.2-B): every object an organization owns lives under orgs/<org-id>/ (OrgPrefix), and a
// factory was one prefix in it, orgs/<org-id>/factories/<metadata.id>/ (Prefix), holding
// factory.yaml, agents/, skills/, contracts/ and evals/. goald imports each such factory once as
// version 1 of itself (definitions.ImportLegacy) and ignores the prefixes afterwards: definitions
// now change through goald's API (goal factory push, the builder). knowledge/ under the same
// prefix is reserved for the factory's knowledge base, which this package never reads. The
// organization id always comes from the database, never from a definition.
//
// List lists an organization's factories and Read reads one, as the import needs them: it reads
// the files only of those it has yet to import. MigrateLegacy moves the
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

// Listed is one factory of an organization's legacy prefix as listed, before any of its files
// is read.
type Listed struct {
	Org domain.OrgID
	ID  string
	// Files are the definition's objects (no folder markers, nothing under knowledge/), with
	// keys relative to the factory's root, ordered by key.
	Files []ports.ObjectInfo
}

// Versions are the object versions listed, by path.
func (l Listed) Versions() map[string]string {
	out := make(map[string]string, len(l.Files))
	for _, o := range l.Files {
		out[o.Key] = o.Version
	}

	return out
}

// List lists every factory of org's legacy prefix, ordered by id, reading no file.
func (c *Catalog) List(ctx context.Context, org domain.OrgID) ([]Listed, error) {
	root := OrgPrefix(org) + factoriesDir

	objects, err := c.store.List(ctx, root)
	if err != nil {
		return nil, err
	}

	byID := map[string]*Listed{}

	for _, o := range objects {
		id, _, ok := strings.Cut(strings.TrimPrefix(o.Key, root), "/")
		if !ok || id == "" {
			continue
		}

		l := byID[id]
		if l == nil {
			l = &Listed{Org: org, ID: id}
			byID[id] = l
		}

		rel := strings.TrimPrefix(o.Key, Prefix(org, id))

		// Folder markers and the knowledge base are not part of the definition.
		if !strings.HasSuffix(rel, "/") && !strings.HasPrefix(rel, KnowledgeDir) {
			l.Files = append(l.Files, ports.ObjectInfo{Key: rel, Version: o.Version, Size: o.Size})
		}
	}

	out := make([]Listed, 0, len(byID))
	for _, l := range byID {
		slices.SortFunc(l.Files, func(a, b ports.ObjectInfo) int { return cmp.Compare(a.Key, b.Key) })
		out = append(out, *l)
	}

	slices.SortFunc(out, func(a, b Listed) int { return cmp.Compare(a.ID, b.ID) })

	return out, nil
}

// Entry is one factory read from the bucket. Factory is nil when its definition does not load;
// Err then says why.
type Entry struct {
	ID       string
	Snapshot *Snapshot
	Factory  *config.Factory
	Err      error
}

// Read reads and validates a listed factory: each file at the version listed, so a concurrent
// write cannot mix two definitions. A factory that cannot be one (a bad key, no factory.yaml,
// a definition that does not load) is an Entry with Err; an error is the bucket's.
func (c *Catalog) Read(ctx context.Context, l Listed) (Entry, error) {
	e := Entry{ID: l.ID}
	s := &Snapshot{Org: l.Org, ID: l.ID, Files: l.Files, files: config.Files{}}

	for _, o := range l.Files {
		if !fs.ValidPath(o.Key) {
			e.Err = fmt.Errorf("factory %s: object key %q is not a valid file path", l.ID, Prefix(l.Org, l.ID)+o.Key)
			return e, nil
		}
	}

	for _, o := range l.Files {
		obj, err := c.store.Get(ctx, Prefix(l.Org, l.ID)+o.Key, o.Version)
		if err != nil {
			return Entry{}, fmt.Errorf("read factory %s: %w", l.ID, err)
		}

		s.files[o.Key] = obj.Body
	}

	if _, ok := s.files[config.FileName]; !ok {
		e.Err = fmt.Errorf("factory %s: no %s%s: %w", l.ID, Prefix(l.Org, l.ID), config.FileName, ports.ErrNotFound)
		return e, nil
	}

	e.Snapshot = s
	if e.Factory, e.Err = config.Load(s.FS()); e.Err == nil && e.Factory.Metadata.ID != l.ID {
		e.Factory, e.Err = nil, fmt.Errorf("metadata.id %s does not match its prefix %s", e.Factory.Metadata.ID, Prefix(l.Org, l.ID))
	}

	return e, nil
}
