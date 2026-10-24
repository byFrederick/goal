package config

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// Files is a read-only file tree in memory: slash paths relative to its root → content, such
// as a definition read from the bucket for Load. Directories are implied by the paths of the
// files under them.
type Files map[string][]byte

// Open implements fs.FS.
func (m Files) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	if b, ok := m[name]; ok {
		return &memFile{info: entry{name: path.Base(name), size: int64(len(b))}, r: bytes.NewReader(b)}, nil
	}

	entries := m.readDir(name)
	if len(entries) == 0 && name != "." {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	return &memDir{info: entry{name: path.Base(name), dir: true}, entries: entries}, nil
}

// readDir returns the direct children of dir, sorted by name.
func (m Files) readDir(dir string) []fs.DirEntry {
	prefix := dir + "/"
	if dir == "." {
		prefix = ""
	}

	children := map[string]entry{}

	for p, b := range m {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}

		if name, _, isDir := strings.Cut(rest, "/"); isDir {
			children[name] = entry{name: name, dir: true}
		} else {
			children[name] = entry{name: name, size: int64(len(b))}
		}
	}

	out := make([]fs.DirEntry, 0, len(children))
	for _, e := range children {
		out = append(out, e)
	}

	slices.SortFunc(out, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })

	return out
}

// entry is both the fs.FileInfo and the fs.DirEntry of a file or directory.
type entry struct {
	name string
	size int64
	dir  bool
}

func (e entry) Name() string               { return e.name }
func (e entry) Size() int64                { return e.size }
func (e entry) ModTime() time.Time         { return time.Time{} }
func (e entry) IsDir() bool                { return e.dir }
func (e entry) Sys() any                   { return nil }
func (e entry) Type() fs.FileMode          { return e.Mode().Type() }
func (e entry) Info() (fs.FileInfo, error) { return e, nil }

func (e entry) Mode() fs.FileMode {
	if e.dir {
		return fs.ModeDir | 0o555
	}

	return 0o444
}

type memFile struct {
	info entry
	r    *bytes.Reader
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *memFile) Close() error               { return nil }

type memDir struct {
	info    entry
	entries []fs.DirEntry
	offset  int
}

func (d *memDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *memDir) Close() error               { return nil }

func (d *memDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: fs.ErrInvalid}
}

// ReadDir follows fs.ReadDirFile: n <= 0 returns everything left, n > 0 at most n entries and
// io.EOF once none are left.
func (d *memDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.entries[d.offset:]
	if n <= 0 {
		d.offset = len(d.entries)

		return slices.Clone(rest), nil
	}

	if len(rest) == 0 {
		return nil, io.EOF
	}

	n = min(n, len(rest))
	d.offset += n

	return slices.Clone(rest[:n]), nil
}
