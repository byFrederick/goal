package domain

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
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
	// MaxFactorySources is how many sources a factory's knowledge.sources lists (decision 4).
	MaxFactorySources = 10
)

// Limits of a factory's memory, its memory.yaml (decision 6): an entry's text in characters, and
// the file's bytes and entries.
const (
	MaxMemoryText    = 300
	MaxMemoryBytes   = 8 << 10
	MaxMemoryEntries = 50
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

// KnowledgeSourceRoot is the path a search of a whole source counts at: a search names a folder,
// and the source's own folder has no path of its own.
const KnowledgeSourceRoot = "."

// KnowledgeTouch is what one Read, Grep or Glob call of a run did under KnowledgeRoot (ADR 0038
// decision 10): a Read of the file Path of Source, or with Search a Grep or Glob of the folder or
// file Path (KnowledgeSourceRoot for the whole source).
type KnowledgeTouch struct {
	Source string
	Path   string
	Search bool
}

// TouchOfKnowledge is what the call of tool on the path p, as the worker reports it, did under
// KnowledgeRoot: ok only for a Read of a file in a source's folder, or a Grep or Glob of a source's
// folder or anything in it. Every other tool, a relative path, a path outside the sources' folders
// or one that is no file's path is no touch.
func TouchOfKnowledge(tool, p string) (KnowledgeTouch, bool) {
	search := tool == "Grep" || tool == "Glob"
	if (!search && tool != "Read") || !path.IsAbs(p) {
		return KnowledgeTouch{}, false
	}

	rest, ok := strings.CutPrefix(path.Clean(p), KnowledgeRoot+"/")
	if !ok {
		return KnowledgeTouch{}, false
	}

	source, rel, _ := strings.Cut(rest, "/")

	switch {
	case rel == "" && search:
		rel = KnowledgeSourceRoot
	case rel == "" || CheckFilePath(rel) != nil:
		return KnowledgeTouch{}, false
	}

	return KnowledgeTouch{Source: source, Path: rel, Search: search}, true
}

// KnowledgeTally adds up one run's touches of the sources it pinned, to count them once the run
// ends (ports.Knowledge.AddReads).
type KnowledgeTally struct {
	pinned map[string]bool
	counts map[KnowledgeTouch]int
}

// NewKnowledgeTally counts the touches of the sources of pins only.
func NewKnowledgeTally(pins []KnowledgePin) *KnowledgeTally {
	t := &KnowledgeTally{pinned: make(map[string]bool, len(pins)), counts: map[KnowledgeTouch]int{}}
	for _, p := range pins {
		t.pinned[p.Source] = true
	}

	return t
}

// Add counts the call of tool on the path p when it touched a pinned source (TouchOfKnowledge).
func (t *KnowledgeTally) Add(tool, p string) {
	if k, ok := TouchOfKnowledge(tool, p); ok && t.pinned[k.Source] {
		t.counts[k]++
	}
}

// Sources are the names of the sources touched, sorted.
func (t *KnowledgeTally) Sources() []string {
	seen := map[string]bool{}

	var out []string

	for k := range t.counts {
		if !seen[k.Source] {
			seen[k.Source] = true
			out = append(out, k.Source)
		}
	}

	slices.Sort(out)

	return out
}

// Reads are the counts as factory's on the day of at, one per source and path, by source name then
// path; ids gives each source's id by name, and a source without one is left out.
func (t *KnowledgeTally) Reads(ids map[string]KnowledgeSourceID, factory FactoryID, at time.Time) []KnowledgeFileReads {
	type key struct{ source, path string }

	sums := map[key]*KnowledgeFileReads{}

	var keys []key

	for k, n := range t.counts {
		id, ok := ids[k.Source]
		if !ok {
			continue
		}

		kk := key{k.Source, k.Path}

		r := sums[kk]
		if r == nil {
			r = &KnowledgeFileReads{Source: id, Path: k.Path, Factory: factory, Day: KnowledgeDay(at)}
			sums[kk] = r
			keys = append(keys, kk)
		}

		if k.Search {
			r.Searches += n
		} else {
			r.Reads += n
		}
	}

	slices.SortFunc(keys, func(a, b key) int {
		return cmp.Or(strings.Compare(a.source, b.source), strings.Compare(a.path, b.path))
	})

	out := make([]KnowledgeFileReads, 0, len(keys))
	for _, k := range keys {
		out = append(out, *sums[k])
	}

	return out
}

// KnowledgeDay is the day t counts on: its UTC date, at midnight.
func KnowledgeDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Check enforces the rules of one count: a source, a file's path (or KnowledgeSourceRoot for
// searches only), a factory, a day and counts that are not negative and not both zero. Errors wrap
// ErrInvalid.
func (r KnowledgeFileReads) Check() error {
	var errs []error

	switch {
	case r.Path != KnowledgeSourceRoot:
		errs = append(errs, CheckFilePath(r.Path))
	case r.Reads != 0:
		errs = append(errs, fmt.Errorf("a source's own folder is searched, never read: %w", ErrInvalid))
	}

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

// SourceRepositoryKey is the config key of the repository a Repository source, or a synced source
// read by gitdocs, reads.
const SourceRepositoryKey = "repository"

// CopiesCheckout reports whether s reads the repository at checkout: a Repository source, or a
// synced source that gitdocs reads, until the cut-over converts it (ADR 0038 decision 16), whose
// repository has the same host and path, whatever their case, a trailing slash or .git.
func (s KnowledgeSource) CopiesCheckout(checkout string) bool {
	if s.Kind != SourceRepository && s.Kind != SourceSynced {
		return false
	}

	a, b := repositoryKey(s.Config[SourceRepositoryKey]), repositoryKey(checkout)

	return a != "" && a == b
}

// repositoryKey is a repository URL as host/owner/repo in lower case, "" for a value that is not
// an absolute URL.
func repositoryKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}

	p := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if p == "" {
		return ""
	}

	return strings.ToLower(u.Host + "/" + p)
}

// SourceCheck is what listing a source with settings not saved yet found (ADR 0038 decision 12's
// Check): Error says why it could not be listed ("" when it could), and Files how many items it
// lists, counting at most MaxSourceFiles+1, past which a sync fails.
type SourceCheck struct {
	Error string
	Files int
}

// KnowledgeEntry is one source of a factory's knowledge.sources (ADR 0038 decision 5): the source's
// name, the runs it applies to (a zero AppliesTo is every run, matched as rules are) and the line
// that replaces its read-when line in the factory's prompt ("" keeps the snapshot's).
type KnowledgeEntry struct {
	Name      string
	AppliesTo RuleScope
	ReadWhen  string
}

// MemoryFact is one fact of a factory's memory.yaml (decision 6): its text and the items it came
// from (<factory>#<number>).
type MemoryFact struct {
	Text string
	From []string
}

// KnowledgePin is one knowledge snapshot a claim's runs read (decision 7): its source's name and
// the snapshot's id, decided once at the claim's start.
type KnowledgePin struct {
	Source   string              `json:"n"`
	Snapshot KnowledgeSnapshotID `json:"s"`
}

// ApplyingKnowledge are the entries that apply to run, in the definition's order: those whose
// AppliesTo matches it as a rule's scope does (EffectiveRules).
func ApplyingKnowledge(entries []KnowledgeEntry, run RuleTarget) []KnowledgeEntry {
	var out []KnowledgeEntry

	for _, k := range entries {
		if k.AppliesTo.matches(run) {
			out = append(out, k)
		}
	}

	return out
}
