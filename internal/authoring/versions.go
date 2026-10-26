package authoring

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/factorymodel"
)

// SelectorKind says which version a VersionSelector names.
type SelectorKind string

// Selector kinds.
const (
	SelectNumber  SelectorKind = "number"  // published version N
	SelectCurrent SelectorKind = "current" // the version that runs
	SelectDraft   SelectorKind = "draft"   // the open draft
	SelectBase    SelectorKind = "base"    // the published version the draft started from
	SelectSource  SelectorKind = "source"  // the draft's starting tree (template, import, clone)
)

// VersionSelector names a version in a {version} path segment: a number, current, draft, base
// or source.
type VersionSelector struct {
	Kind   SelectorKind
	Number int
}

// ParseVersion reads a {version} segment; ErrInvalidRequest when it is none of the forms.
func ParseVersion(s string) (VersionSelector, error) {
	switch k := SelectorKind(s); k {
	case SelectCurrent, SelectDraft, SelectBase, SelectSource:
		return VersionSelector{Kind: k}, nil
	case SelectNumber:
	}

	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return VersionSelector{}, fmt.Errorf("version %q is not a number, current, draft, base or source: %w", s, ErrInvalidRequest)
	}

	return VersionSelector{Kind: SelectNumber, Number: n}, nil
}

func (v VersionSelector) String() string {
	if v.Kind == SelectNumber {
		return "v" + strconv.Itoa(v.Number)
	}

	return string(v.Kind)
}

// snapshot is a version's files, and the version when one holds them (a template's starting
// tree has none).
type snapshot struct {
	version *domain.DefinitionVersion
	files   *tree
}

// snapshot reads the version sel names. A selector that names nothing (base of a first draft,
// current before the first publish) is ErrVersionNotFound.
func (s *Service) snapshot(ctx context.Context, f domain.FactoryID, sel VersionSelector) (snapshot, error) {
	v, err := s.selectVersion(ctx, f, sel)
	if err != nil {
		return snapshot{}, err
	}

	if v != nil {
		return snapshot{version: v, files: s.tree(ctx, v.Manifest)}, nil
	}

	// sel is source with a starting tree no version holds.
	d, err := s.draft(ctx, f)
	if err != nil {
		return snapshot{}, err
	}

	return snapshot{files: s.tree(ctx, d.Source.Manifest)}, nil
}

// selectVersion returns the version sel names; nil (and no error) for a source that is a
// starting tree only.
func (s *Service) selectVersion(ctx context.Context, f domain.FactoryID, sel VersionSelector) (*domain.DefinitionVersion, error) {
	switch sel.Kind {
	case SelectNumber:
		v, err := s.store.VersionByNumber(ctx, f, sel.Number)

		return s.found(ctx, f, v, err, sel)
	case SelectCurrent:
		rec, err := s.record(ctx, f)
		if err != nil {
			return nil, err
		}

		if rec.Current == nil {
			return nil, fmt.Errorf("factory %s was never published: %w", f, ErrVersionNotFound)
		}

		return s.byID(ctx, f, rec.Current.ID, sel)
	case SelectDraft:
		d, err := s.draft(ctx, f)
		if err != nil {
			return nil, err
		}

		return &d, nil
	case SelectBase, SelectSource:
		d, err := s.draft(ctx, f)
		if err != nil {
			return nil, err
		}

		id := d.BaseID
		if sel.Kind == SelectSource {
			if d.Source.Manifest != nil {
				return nil, nil
			}

			id = d.Source.Version
		}

		if id == "" {
			return nil, fmt.Errorf("the draft of %s has no %s: %w", f, sel.Kind, ErrVersionNotFound)
		}

		return s.byID(ctx, f, id, sel)
	}

	return nil, fmt.Errorf("version selector %q: %w", sel.Kind, ErrInvalidRequest)
}

func (s *Service) byID(ctx context.Context, f domain.FactoryID, id domain.DefinitionVersionID, sel VersionSelector) (*domain.DefinitionVersion, error) {
	v, err := s.store.Version(ctx, id)

	return s.found(ctx, f, v, err, sel)
}

// found maps the store's not found to ErrVersionNotFound, or to ErrFactoryNotFound when the
// factory itself is unknown.
func (s *Service) found(ctx context.Context, f domain.FactoryID, v domain.DefinitionVersion, err error, sel VersionSelector) (*domain.DefinitionVersion, error) {
	if errors.Is(err, ports.ErrNotFound) {
		if _, rerr := s.record(ctx, f); rerr != nil {
			return nil, rerr
		}

		return nil, fmt.Errorf("%s of %s: %w", sel, f, ErrVersionNotFound)
	}

	if err != nil {
		return nil, fmt.Errorf("read %s of %s: %w", sel, f, err)
	}

	return &v, nil
}

// VersionInfo is a version as the history lists it.
type VersionInfo struct {
	domain.DefinitionVersion

	// Current marks the version that runs.
	Current bool
	Counts  Counts
}

// VersionPage is one page of a factory's published versions, newest first.
type VersionPage struct {
	Versions []VersionInfo
	// NextBefore is the before of the next page; 0 when this is the last.
	NextBefore int
}

// Version page sizes.
const (
	DefaultVersionPage = 20
	MaxVersionPage     = 50
)

// Versions lists a factory's published versions numbered below before (all when 0), newest
// first, limit at a time (DefaultVersionPage when 0, at most MaxVersionPage).
func (s *Service) Versions(ctx context.Context, f domain.FactoryID, before, limit int) (VersionPage, error) {
	rec, err := s.record(ctx, f)
	if err != nil {
		return VersionPage{}, err
	}

	if limit <= 0 {
		limit = DefaultVersionPage
	}

	limit = min(limit, MaxVersionPage)

	vs, err := s.store.Versions(ctx, f, before, limit+1)
	if err != nil {
		return VersionPage{}, fmt.Errorf("list versions of %s: %w", f, err)
	}

	page := VersionPage{Versions: make([]VersionInfo, 0, min(len(vs), limit))}
	if len(vs) > limit {
		vs = vs[:limit]
		page.NextBefore = vs[limit-1].Number
	}

	for _, v := range vs {
		info, err := s.versionInfo(ctx, rec, v)
		if err != nil {
			return VersionPage{}, err
		}

		page.Versions = append(page.Versions, info)
	}

	return page, nil
}

func (s *Service) versionInfo(ctx context.Context, rec ports.FactoryRecord, v domain.DefinitionVersion) (VersionInfo, error) {
	m, _, err := s.versionModel(s.tree(ctx, v.Manifest))
	if err != nil && !isParseError(err) {
		return VersionInfo{}, err
	}

	return VersionInfo{
		DefinitionVersion: v,
		Current:           rec.Current != nil && rec.Current.ID == v.ID,
		Counts:            counts(nil, m, Comparison{}),
	}, nil
}

// FileInfo is one file of a version.
type FileInfo struct {
	Path   string
	SHA256 domain.BlobHash
	Size   int64
}

// VersionDetail is a version with its files.
type VersionDetail struct {
	VersionInfo

	Files []FileInfo
}

// Version returns the version sel names with its files. A source that is a starting tree
// only is ErrVersionNotFound here: it is no version.
func (s *Service) Version(ctx context.Context, f domain.FactoryID, sel VersionSelector) (VersionDetail, error) {
	rec, err := s.record(ctx, f)
	if err != nil {
		return VersionDetail{}, err
	}

	v, err := s.selectVersion(ctx, f, sel)
	if err != nil {
		return VersionDetail{}, err
	}

	if v == nil {
		return VersionDetail{}, fmt.Errorf("the source of %s is a starting tree: %w", f, ErrVersionNotFound)
	}

	info, err := s.versionInfo(ctx, rec, *v)
	if err != nil {
		return VersionDetail{}, err
	}

	files, err := s.fileInfos(ctx, v.Manifest)
	if err != nil {
		return VersionDetail{}, err
	}

	return VersionDetail{VersionInfo: info, Files: files}, nil
}

// fileInfos lists a manifest's files by path, with their sizes.
func (s *Service) fileInfos(ctx context.Context, m domain.Manifest) ([]FileInfo, error) {
	sizes, err := s.store.BlobSizes(ctx, m.Hashes())
	if err != nil {
		return nil, fmt.Errorf("read file sizes: %w", err)
	}

	out := make([]FileInfo, 0, len(m))
	for _, p := range slices.Sorted(maps.Keys(m)) {
		out = append(out, FileInfo{Path: p, SHA256: m[p], Size: sizes[m[p]]})
	}

	return out, nil
}

// File is one file's content.
type File struct {
	Content []byte
	SHA256  domain.BlobHash
	// Immutable is true for a published version's file, which never changes.
	Immutable bool
}

// VersionFile returns one file of the version sel names: ErrFileNotFound when it has none at
// path.
func (s *Service) VersionFile(ctx context.Context, f domain.FactoryID, sel VersionSelector, path string) (File, error) {
	snap, err := s.snapshot(ctx, f, sel)
	if err != nil {
		return File{}, err
	}

	b, ok, err := snap.files.file(path)
	if err != nil {
		return File{}, err
	}

	if !ok {
		return File{}, fmt.Errorf("%s of %s has no %s: %w", sel, f, path, ErrFileNotFound)
	}

	immutable := snap.version == nil || snap.version.State != domain.VersionDraft

	return File{Content: b, SHA256: snap.files.manifest[path], Immutable: immutable}, nil
}

// VersionModel returns the factory model of the version sel names, for a read-only Visual
// view: a *ParseError when its factory.yaml does not parse.
func (s *Service) VersionModel(ctx context.Context, f domain.FactoryID, sel VersionSelector) (Model, Ranges, error) {
	snap, err := s.snapshot(ctx, f, sel)
	if err != nil {
		return Model{}, nil, err
	}

	return s.versionModel(snap.files)
}

// versionModel reads the model of a tree's factory.yaml (an empty model when it has none).
func (s *Service) versionModel(t *tree) (Model, Ranges, error) {
	doc, ok, err := t.file(config.FileName)
	if err != nil {
		return Model{}, nil, err
	}

	if !ok {
		return Model{Connections: []any{}}, Ranges{}, nil
	}

	m, r, err := factorymodel.FromYAML(doc, t)
	if t.err != nil {
		return Model{}, nil, t.err
	}

	if err != nil {
		return Model{Connections: []any{}}, Ranges{}, fmt.Errorf("read factory.yaml: %w", err)
	}

	return m, r, nil
}

func isParseError(err error) bool {
	_, ok := errors.AsType[*ParseError](err)

	return ok
}

// CompareResult is the comparison of two versions; From or To is nil for a starting tree that
// no version holds.
type CompareResult struct {
	Comparison

	From, To *domain.DefinitionVersion
}

// Compare compares two versions of a factory (B §4.5): the draft against its base by default
// (zero selectors).
func (s *Service) Compare(ctx context.Context, f domain.FactoryID, from, to VersionSelector) (CompareResult, error) {
	if from.Kind == "" {
		from.Kind = SelectBase
	}

	if to.Kind == "" {
		to.Kind = SelectDraft
	}

	a, err := s.snapshot(ctx, f, from)
	if err != nil {
		return CompareResult{}, err
	}

	b, err := s.snapshot(ctx, f, to)
	if err != nil {
		return CompareResult{}, err
	}

	c := factorymodel.Diff(
		factorymodel.Snapshot{Manifest: a.files.manifest, Files: a.files},
		factorymodel.Snapshot{Manifest: b.files.manifest, Files: b.files},
	)
	if err := errors.Join(a.files.err, b.files.err); err != nil {
		return CompareResult{}, err
	}

	return CompareResult{Comparison: c, From: a.version, To: b.version}, nil
}
