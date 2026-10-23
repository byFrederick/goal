// Package factorystore keeps factory definitions in object storage. A factory is one prefix,
// factories/<metadata.id>/, holding factory.yaml, agents/, skills/, contracts/ and evals/.
// knowledge/ under the same prefix is reserved for the factory's knowledge base, which this
// package never reads or writes.
//
// It pushes a local working copy to that prefix, pulls a prefix into a folder, lists the
// factories in the bucket and reads one factory as an fs.FS that config.Load accepts. The
// bucket is versioned, so every push leaves the previous object versions readable for rollback.
package factorystore

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	// Root is the prefix under which every factory lives.
	Root = "factories/"
	// KnowledgeDir is the folder inside a factory reserved for its knowledge base. Definitions
	// never include it: push neither uploads nor deletes anything under it, and reads skip it.
	KnowledgeDir = "knowledge/"
)

// Prefix is a factory's folder in the bucket. It uses the id, so renames never move objects.
func Prefix(id string) string { return Root + id + "/" }

// Action says what a push did to one file.
type Action string

// Actions a push reports.
const (
	Added     Action = "added"
	Updated   Action = "updated"
	Unchanged Action = "unchanged"
	Deleted   Action = "deleted"
)

// Change is what a push did to one file. Path is relative to the factory's root. Version is
// the object version the file has now; for a deleted file, the last version it had, which is
// what a rollback restores.
type Change struct {
	Path    string
	Action  Action
	Version string
}

// Catalog reads and writes factory definitions in one bucket.
type Catalog struct {
	store ports.ObjectStore
}

// New returns a catalog over store.
func New(store ports.ObjectStore) *Catalog { return &Catalog{store: store} }

// Push validates the factory in dir and makes its prefix match it: new and changed files are
// uploaded (factory.yaml last, so readers never see it point at files not yet written) and
// files no longer in dir are deleted. knowledge/ is left alone on both sides. Unchanged files
// are not rewritten, so their versions stay put.
func (c *Catalog) Push(ctx context.Context, dir string) (*config.Factory, []Change, error) {
	f, err := config.LoadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("validate %s: %w", dir, err)
	}

	local, err := readTree(os.DirFS(dir))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	}

	prefix := Prefix(f.Metadata.ID)

	remote, err := c.store.List(ctx, prefix)
	if err != nil {
		return nil, nil, err
	}

	maps.DeleteFunc(local, func(p string, _ []byte) bool { return strings.HasPrefix(p, KnowledgeDir) })

	current := map[string]ports.ObjectInfo{}
	for _, o := range remote {
		current[strings.TrimPrefix(o.Key, prefix)] = o
	}

	changes, err := c.upload(ctx, prefix, local, current)
	if err != nil {
		return nil, changes, err
	}

	for _, o := range remote {
		rel := strings.TrimPrefix(o.Key, prefix)
		if _, keep := local[rel]; keep || strings.HasPrefix(rel, KnowledgeDir) {
			continue
		}

		if err := c.store.Delete(ctx, o.Key); err != nil {
			return nil, changes, err
		}

		changes = append(changes, Change{Path: rel, Action: Deleted, Version: o.Version})
	}

	slices.SortFunc(changes, func(a, b Change) int { return cmp.Compare(a.Path, b.Path) })

	return f, changes, nil
}

func (c *Catalog) upload(ctx context.Context, prefix string, local map[string][]byte,
	current map[string]ports.ObjectInfo,
) ([]Change, error) {
	paths := make([]string, 0, len(local))
	for p := range local {
		paths = append(paths, p)
	}

	isDef := func(p string) int {
		if p == config.FileName {
			return 1
		}

		return 0
	}

	slices.SortFunc(paths, func(a, b string) int { return cmp.Or(cmp.Compare(isDef(a), isDef(b)), cmp.Compare(a, b)) })

	changes := make([]Change, 0, len(paths))

	for _, p := range paths {
		action := Added

		if o, exists := current[p]; exists {
			old, err := c.store.Get(ctx, o.Key, o.Version)
			if err != nil {
				return changes, err
			}

			if bytes.Equal(old.Body, local[p]) {
				changes = append(changes, Change{Path: p, Action: Unchanged, Version: o.Version})

				continue
			}

			action = Updated
		}

		put, err := c.store.Put(ctx, prefix+p, local[p])
		if err != nil {
			return changes, err
		}

		changes = append(changes, Change{Path: p, Action: action, Version: put.Version})
	}

	return changes, nil
}

// Snapshot is one factory's files as read from the bucket in one pass, each at a known object
// version, so a caller can record or pin exactly what it loaded.
type Snapshot struct {
	ID string
	// Files are the objects read, with keys relative to the factory's root, ordered by key.
	Files []ports.ObjectInfo
	files memFS
}

// FS returns the snapshot as a read-only file tree rooted at the factory's folder.
func (s *Snapshot) FS() fs.FS { return s.files }

// Open reads the definition of the factory with this id (everything but knowledge/). It
// returns an error wrapping ports.ErrNotFound when the prefix has no factory.yaml.
func (c *Catalog) Open(ctx context.Context, id string) (*Snapshot, error) {
	objects, err := c.store.List(ctx, Prefix(id))
	if err != nil {
		return nil, err
	}

	return c.open(ctx, id, objects)
}

func (c *Catalog) open(ctx context.Context, id string, objects []ports.ObjectInfo) (*Snapshot, error) {
	prefix := Prefix(id)
	s := &Snapshot{ID: id, files: memFS{}}

	for _, o := range objects {
		rel := strings.TrimPrefix(o.Key, prefix)
		if strings.HasSuffix(rel, "/") || strings.HasPrefix(rel, KnowledgeDir) {
			continue // folder markers and the knowledge base are not part of the definition
		}

		if !fs.ValidPath(rel) {
			return nil, fmt.Errorf("factory %s: object key %q is not a valid file path", id, o.Key)
		}

		// Read the listed version, not the latest: a concurrent push cannot mix two definitions.
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

// List reads and validates every factory in the bucket, ordered by id.
func (c *Catalog) List(ctx context.Context) ([]Entry, error) {
	objects, err := c.store.List(ctx, Root)
	if err != nil {
		return nil, err
	}

	byID := map[string][]ports.ObjectInfo{}

	for _, o := range objects {
		id, _, ok := strings.Cut(strings.TrimPrefix(o.Key, Root), "/")
		if ok && id != "" {
			byID[id] = append(byID[id], o)
		}
	}

	entries := make([]Entry, 0, len(byID))

	for id, objs := range byID {
		e := Entry{ID: id}

		e.Snapshot, e.Err = c.open(ctx, id, objs)
		if e.Err == nil {
			e.Factory, e.Err = config.Load(e.Snapshot.FS())
		}

		if e.Err == nil && e.Factory.Metadata.ID != id {
			e.Factory, e.Err = nil, fmt.Errorf("metadata.id %s does not match its prefix %s", e.Factory.Metadata.ID, Prefix(id))
		}

		entries = append(entries, e)
	}

	slices.SortFunc(entries, func(a, b Entry) int { return cmp.Compare(a.ID, b.ID) })

	return entries, nil
}

// Resolve finds the id of the factory that ref names: its id, or the metadata.name of a factory
// that loads. It returns an error wrapping ports.ErrNotFound when nothing matches.
func (c *Catalog) Resolve(ctx context.Context, ref string) (string, error) {
	entries, err := c.List(ctx)
	if err != nil {
		return "", err
	}

	var ids []string

	for _, e := range entries {
		if e.ID == ref || (e.Factory != nil && e.Factory.Metadata.Name == ref) {
			ids = append(ids, e.ID)
		}
	}

	switch len(ids) {
	case 0:
		return "", fmt.Errorf("factory %q: %w", ref, ports.ErrNotFound)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("factory name %q is used by %s; pull by id", ref, strings.Join(ids, ", "))
	}
}

// Pull writes the definition of the factory that ref names into dir, which must be missing or
// empty so that no local edit is ever overwritten.
func (c *Catalog) Pull(ctx context.Context, ref, dir string) (*Snapshot, error) {
	if err := mustBeEmpty(dir); err != nil {
		return nil, err
	}

	id, err := c.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}

	s, err := c.Open(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := os.CopyFS(dir, s.FS()); err != nil {
		return nil, fmt.Errorf("write %s: %w", dir, err)
	}

	return s, nil
}

func mustBeEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("pull into %s: %w", dir, err)
	}

	if len(entries) > 0 {
		return fmt.Errorf("pull into %s: the folder is not empty; pull into a new folder", dir)
	}

	return nil
}

// readTree returns every file in files by slash path. Hidden files and folders (.git,
// .DS_Store) are not part of a factory and are skipped.
func readTree(files fs.FS) (map[string][]byte, error) {
	out := map[string][]byte{}

	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if p != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return nil
		}

		b, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}

		out[p] = b

		return nil
	})

	return out, err
}
