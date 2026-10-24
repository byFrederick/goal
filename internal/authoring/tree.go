package authoring

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// Limits of a definition's files (B §11).
const (
	maxPathLen   = 255
	maxFiles     = 1000
	maxTreeBytes = 16 << 20
)

var pathPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

// CheckPath enforces the rules of a definition file's path: slash-separated segments of
// letters, digits, dots, dashes and underscores, at most 255 characters, no hidden segment
// (one starting with a dot, so no . or ..) and nothing under knowledge/, which is reserved for
// the knowledge base. The error wraps ErrBadPath.
func CheckPath(p string) error {
	if len(p) > maxPathLen || !pathPattern.MatchString(p) {
		return fmt.Errorf("path %q: %w", p, ErrBadPath)
	}

	for seg := range strings.SplitSeq(p, "/") {
		if strings.HasPrefix(seg, ".") {
			return fmt.Errorf("path %q has a hidden segment: %w", p, ErrBadPath)
		}
	}

	if p == "knowledge" || strings.HasPrefix(p, "knowledge/") {
		return fmt.Errorf("path %q: knowledge/ is reserved for the knowledge base: %w", p, ErrBadPath)
	}

	return nil
}

// checkFiles enforces the path rules and the size limits on files written in one request.
func checkFiles(files map[string][]byte) error {
	if len(files) > maxFiles {
		return fmt.Errorf("%d files, at most %d: %w", len(files), maxFiles, ErrTooLarge)
	}

	total := 0

	for p, b := range files {
		if err := CheckPath(p); err != nil {
			return err
		}

		if len(b) > domain.MaxBlobSize {
			return fmt.Errorf("file %s has %d bytes, at most %d: %w", p, len(b), domain.MaxBlobSize, ErrTooLarge)
		}

		total += len(b)
	}

	if total > maxTreeBytes {
		return fmt.Errorf("%d bytes of files, at most %d: %w", total, maxTreeBytes, ErrTooLarge)
	}

	return nil
}

// tree is a definition's file tree as an fs.FS whose files are read from the store the first
// time they are opened, so a section save fetches only factory.yaml and the prompts while a
// validation reads everything. Directories are implied by the paths under them. A tree serves
// one call: it is not safe for concurrent use.
type tree struct {
	manifest domain.Manifest
	data     map[string][]byte
	read     func(domain.BlobHash) ([]byte, error)
	// err is the first failure to read a blob; callers check it, since fs.FS users such as
	// config.Check turn a failed read into a "not found" problem.
	err error
}

// tree returns the files of m, read through the store with ctx.
func (s *Service) tree(ctx context.Context, m domain.Manifest) *tree {
	return &tree{
		manifest: maps.Clone(m),
		data:     map[string][]byte{},
		read:     func(h domain.BlobHash) ([]byte, error) { return s.store.Blob(ctx, h) },
	}
}

// with returns the tree with set written over it and remove deleted, sharing what was read.
func (t *tree) with(set map[string][]byte, remove []string) *tree {
	n := &tree{manifest: maps.Clone(t.manifest), data: maps.Clone(t.data), read: t.read}

	for _, p := range remove {
		delete(n.manifest, p)
		delete(n.data, p)
	}

	for p, b := range set {
		n.manifest[p] = domain.HashBlob(b)
		n.data[p] = b
	}

	return n
}

// file returns a file's content; ok is false when the tree has none at p.
func (t *tree) file(p string) (b []byte, ok bool, err error) {
	h, ok := t.manifest[p]
	if !ok {
		return nil, false, nil
	}

	if b, ok := t.data[p]; ok {
		return b, true, nil
	}

	b, err = t.read(h)
	if err != nil {
		if t.err == nil {
			t.err = fmt.Errorf("read %s: %w", p, err)
		}

		return nil, true, t.err
	}

	t.data[p] = b

	return b, true, nil
}

// Open implements fs.FS.
func (t *tree) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	b, ok, err := t.file(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}

	if ok {
		return &treeFile{info: info{name: path.Base(name), size: int64(len(b))}, r: bytes.NewReader(b)}, nil
	}

	entries := t.dir(name)
	if len(entries) == 0 && name != "." {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	return &treeDir{info: info{name: path.Base(name), dir: true}, entries: entries}, nil
}

// dir lists the direct children of a directory, by name.
func (t *tree) dir(name string) []fs.DirEntry {
	prefix := name + "/"
	if name == "." {
		prefix = ""
	}

	children := map[string]info{}

	for p := range t.manifest {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}

		child, _, isDir := strings.Cut(rest, "/")
		children[child] = info{name: child, dir: isDir, size: int64(len(t.data[p]))}
	}

	out := make([]fs.DirEntry, 0, len(children))
	for _, n := range slices.Sorted(maps.Keys(children)) {
		out = append(out, fs.FileInfoToDirEntry(children[n]))
	}

	return out
}

type info struct {
	name string
	size int64
	dir  bool
}

func (i info) Name() string { return i.name }
func (i info) Size() int64  { return i.size }
func (i info) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}

	return 0o444
}
func (i info) ModTime() time.Time { return time.Time{} }
func (i info) IsDir() bool        { return i.dir }
func (i info) Sys() any           { return nil }

type treeFile struct {
	info info
	r    *bytes.Reader
}

func (f *treeFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *treeFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *treeFile) Close() error               { return nil }

type treeDir struct {
	info    info
	entries []fs.DirEntry
	off     int
}

func (d *treeDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *treeDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: fs.ErrInvalid}
}
func (d *treeDir) Close() error { return nil }

// ReadDir implements fs.ReadDirFile.
func (d *treeDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.entries[d.off:]
	if n <= 0 {
		d.off = len(d.entries)

		return rest, nil
	}

	if len(rest) == 0 {
		return nil, io.EOF
	}

	rest = rest[:min(n, len(rest))]
	d.off += len(rest)

	return rest, nil
}

// putFiles stores files as blobs and returns their manifest.
func (s *Service) putFiles(ctx context.Context, files map[string][]byte) (domain.Manifest, error) {
	if len(files) == 0 {
		return domain.Manifest{}, nil
	}

	m, err := s.store.PutBlobs(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("store files: %w", err)
	}

	return m, nil
}

var _ fs.ReadDirFile = (*treeDir)(nil)
