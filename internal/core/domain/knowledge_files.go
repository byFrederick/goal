package domain

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

// Knowledge as files (ADR 0038 decisions 1, 3 and 15): a source is a folder of Markdown files
// kept in sync. Each sync that changes something writes a snapshot, one zip in the bucket at
// KnowledgeSnapshotKey, and the database keeps its row and the current snapshot's files. Runs
// count their reads and searches of those files per day (KnowledgeFileReads).

// KnowledgeSnapshotID is a knowledge snapshot's id (a UUID).
type KnowledgeSnapshotID string

// Limits of a source's files (decision 4, 00090's checks).
const (
	// MaxReadWhen is a read-when line's length in characters.
	MaxReadWhen = 160
	// MaxSourceFiles, MaxSourceBytes and MaxFileBytes bound a source: its files, their Markdown
	// together and each file's.
	MaxSourceFiles = 20000
	MaxSourceBytes = 50 << 20
	MaxFileBytes   = 5 << 20
	// MaxFilePath is a file's path in bytes, MaxFileTitle its title in characters and
	// MaxFileVersion its version in characters.
	MaxFilePath    = 1024
	MaxFileTitle   = 1024
	MaxFileVersion = 1024
	// MaxSnapshotFailures is how many failed files a snapshot lists; Failed counts them all.
	MaxSnapshotFailures = 100
	// MaxFilePage bounds one page of a source's files.
	MaxFilePage = 1000
)

// KnowledgeSnapshotKey is the bucket key of a source's snapshot zip:
// orgs/<org-id>/knowledge/<source-id>/<snapshot-id>.zip.
func KnowledgeSnapshotKey(org OrgID, source KnowledgeSourceID, snapshot KnowledgeSnapshotID) string {
	return OrgPrefix(org) + "knowledge/" + string(source) + "/" + string(snapshot) + ".zip"
}

// KnowledgeSnapshot is one zip a sync wrote: its files, the Markdown they hold and the zip's own
// size, what changed since the snapshot before, and the read-when line it was made with, so a
// run's prompt comes from the snapshot it pinned.
type KnowledgeSnapshot struct {
	ID     KnowledgeSnapshotID
	Source KnowledgeSourceID
	// Files is how many files it holds; Failed how many failed to fetch or convert, or looked
	// like they held a key, and kept their last good version or stayed out.
	Files  int
	Failed int
	// Bytes is its files' Markdown together, ZipBytes the zip's size in the bucket.
	Bytes    int64
	ZipBytes int64
	Changes  SnapshotChanges
	ReadWhen string
	// Key is its zip's bucket key, KnowledgeSnapshotKey; the store sets it.
	Key       string
	CreatedAt time.Time
}

// SnapshotChanges summarizes what a snapshot changed: files added, changed and removed since the
// one before, and the failed ones, at most MaxSnapshotFailures.
type SnapshotChanges struct {
	Added   int           `json:"added"`
	Changed int           `json:"changed"`
	Removed int           `json:"removed"`
	Failed  []FileFailure `json:"failed,omitempty"`
}

// FileFailure is a file a sync could not take: Error says why, Key when it looked like it held a
// key (ADR 0037's plain-key patterns) and stayed out.
type FileFailure struct {
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
	Key   bool   `json:"key,omitempty"`
}

// KnowledgeFile is one file of a source's current snapshot.
type KnowledgeFile struct {
	Source KnowledgeSourceID
	// Path is its path in the source's folder, /knowledge/<source>/<path> in a run.
	Path  string
	Title string
	Bytes int64
	// Version changes whenever its content does: a content hash, a blob id, a page version.
	Version string
}

// KnowledgeFileQuery pages a source's current files in path order: after the path After ("" from
// the first), at most Limit (up to MaxFilePage), and with Prefix only those whose path, or title
// in any case, starts with it.
type KnowledgeFileQuery struct {
	Source string
	Prefix string
	After  string
	Limit  int
}

// KnowledgeFileReads is what one factory's runs did with one file of a source on one day: Reads
// opened it, Searches matched it. Adding to a day's counts adds to its one row.
type KnowledgeFileReads struct {
	Source   KnowledgeSourceID
	Path     string
	Factory  FactoryID
	Day      time.Time
	Reads    int
	Searches int
}

// KnowledgeDay is the day t counts on: its UTC date, at midnight.
func KnowledgeDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Check enforces the rules of one count: a source, a file's path, a factory, a day and counts
// that are not negative and not both zero. Errors wrap ErrInvalid.
func (r KnowledgeFileReads) Check() error {
	errs := []error{CheckFilePath(r.Path)}

	if r.Source == "" || r.Factory == "" || r.Day.IsZero() {
		errs = append(errs, fmt.Errorf("a read count names its source, factory and day: %w", ErrInvalid))
	}

	if r.Reads < 0 || r.Searches < 0 || r.Reads+r.Searches == 0 {
		errs = append(errs, fmt.Errorf("a read count adds reads or searches, never fewer: %w", ErrInvalid))
	}

	return errors.Join(errs...)
}

// Check enforces a snapshot's rules with files, its files: an id, a source and a time; its counts
// and size are those of files, within a source's limits, each file's rules, every path once, and
// its read-when line's. Errors wrap ErrInvalid.
func (s KnowledgeSnapshot) Check(files []KnowledgeFile) error {
	errs := []error{CheckReadWhen(s.ReadWhen), s.Changes.check(s.Failed)}

	if s.ID == "" || s.Source == "" || s.CreatedAt.IsZero() {
		errs = append(errs, fmt.Errorf("a snapshot has an id, a source and a time: %w", ErrInvalid))
	}

	if s.Files > MaxSourceFiles || s.Files != len(files) {
		errs = append(errs, fmt.Errorf("a snapshot counts its %d files (at most %d), not %d: %w", len(files), MaxSourceFiles, s.Files, ErrInvalid))
	}

	if s.ZipBytes < 0 {
		errs = append(errs, fmt.Errorf("a zip's size is not negative: %w", ErrInvalid))
	}

	var total int64

	seen := make(map[string]bool, len(files))

	for _, f := range files {
		if err := f.Check(); err != nil {
			errs = append(errs, err)

			continue
		}

		if seen[f.Path] {
			errs = append(errs, fmt.Errorf("file %s is in the snapshot twice: %w", f.Path, ErrInvalid))
		}

		seen[f.Path] = true
		total += f.Bytes
	}

	if total > MaxSourceBytes || s.Bytes != total {
		errs = append(errs, fmt.Errorf("a snapshot's size is its files' %d bytes (at most %d), not %d: %w", total, MaxSourceBytes, s.Bytes, ErrInvalid))
	}

	return errors.Join(errs...)
}

func (c SnapshotChanges) check(failed int) error {
	if c.Added < 0 || c.Changed < 0 || c.Removed < 0 || failed < len(c.Failed) || len(c.Failed) > MaxSnapshotFailures {
		return fmt.Errorf("a snapshot's changes count up, and it lists at most %d of its %d failed files: %w", MaxSnapshotFailures, failed, ErrInvalid)
	}

	for _, f := range c.Failed {
		if err := CheckFilePath(f.Path); err != nil {
			return err
		}

		if utf8.RuneCountInString(f.Error) > MaxKnowledgeError {
			return fmt.Errorf("a failed file's error has at most %d characters: %w", MaxKnowledgeError, ErrInvalid)
		}
	}

	return nil
}

// Check enforces a file's rules: its path's, a title and version within their limits, and a size
// within MaxFileBytes. Errors wrap ErrInvalid.
func (f KnowledgeFile) Check() error {
	if err := CheckFilePath(f.Path); err != nil {
		return err
	}

	if utf8.RuneCountInString(f.Title) > MaxFileTitle {
		return fmt.Errorf("file %s: a title has at most %d characters: %w", f.Path, MaxFileTitle, ErrInvalid)
	}

	if n := utf8.RuneCountInString(f.Version); n == 0 || n > MaxFileVersion {
		return fmt.Errorf("file %s: a version has 1 to %d characters: %w", f.Path, MaxFileVersion, ErrInvalid)
	}

	if f.Bytes < 0 || f.Bytes > MaxFileBytes {
		return fmt.Errorf("file %s: %d bytes, at most %d: %w", f.Path, f.Bytes, MaxFileBytes, ErrInvalid)
	}

	return nil
}

// CheckFilePath refuses a path that cannot be a file in a source's folder: empty, longer than
// MaxFilePath bytes, not UTF-8, absolute, not clean (., .., doubled or trailing slashes), or with
// a backslash or a NUL. Errors wrap ErrInvalid.
func CheckFilePath(p string) error {
	switch {
	case p == "" || len(p) > MaxFilePath:
		return fmt.Errorf("a file's path has 1 to %d bytes: %w", MaxFilePath, ErrInvalid)
	case !utf8.ValidString(p) || strings.ContainsAny(p, "\\\x00"):
		return fmt.Errorf("file %q: a path is UTF-8 without a backslash or NUL: %w", p, ErrInvalid)
	case path.IsAbs(p) || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../"):
		return fmt.Errorf("file %q: a path is relative and clean: %w", p, ErrInvalid)
	}

	return nil
}

// CheckReadWhen refuses a read-when line longer than MaxReadWhen characters or of more than one
// line. Errors wrap ErrInvalid.
func CheckReadWhen(s string) error {
	if utf8.RuneCountInString(s) > MaxReadWhen || strings.ContainsAny(s, "\r\n") {
		return fmt.Errorf("a read-when line is one line of at most %d characters: %w", MaxReadWhen, ErrInvalid)
	}

	return nil
}
