package authoring

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorymodel"
	"github.com/byFrederick/goal/internal/ports"
)

// SectionSummary is one section tab's badge: "1 change, 1 problem", and the lines of
// factory.yaml it spans.
type SectionSummary struct {
	Section  Section
	Changes  int
	Problems int
	Lines    [2]int
}

// Summary is a draft's changes and problems: "3 changes in 2 sections · 1 problem".
type Summary struct {
	ChangeCount  int
	ProblemCount int
	Sections     []SectionSummary
}

// DraftView is a factory's open draft as the editor's header shows it.
type DraftView struct {
	Draft domain.DefinitionVersion
	// NextNumber is the number publishing gives it: "Draft of v9".
	NextNumber int
	// Base is the published version it started from ("based on v8"); nil for a first draft.
	Base *domain.DefinitionVersion
	// Current is the version that runs now, which differs from Base after a restore; nil
	// before the first publish.
	Current *ports.VersionRef
	Summary
}

// Draft returns a factory's open draft with its changes and problems: ErrNoDraft when it has
// none.
func (s *Service) Draft(ctx context.Context, f domain.FactoryID) (DraftView, error) {
	rec, err := s.record(ctx, f)
	if err != nil {
		return DraftView{}, err
	}

	d, err := s.draft(ctx, f)
	if err != nil {
		return DraftView{}, err
	}

	return s.draftView(ctx, rec, d)
}

func (s *Service) draftView(ctx context.Context, rec ports.FactoryRecord, d domain.DefinitionVersion) (DraftView, error) {
	sum, err := s.summary(ctx, rec, d)
	if err != nil {
		return DraftView{}, err
	}

	v := DraftView{Draft: d, NextNumber: rec.LastVersionNumber + 1, Current: rec.Current, Summary: sum}

	if d.BaseID != "" {
		base, err := s.byID(ctx, rec.ID, d.BaseID, VersionSelector{Kind: SelectBase})
		if err != nil {
			return DraftView{}, err
		}

		v.Base = base
	}

	return v, nil
}

// summary validates a draft and compares it with its reference.
func (s *Service) summary(ctx context.Context, rec ports.FactoryRecord, d domain.DefinitionVersion) (Summary, error) {
	c, err := s.validateDraft(ctx, rec, d, nil, nil)
	if err != nil {
		return Summary{}, err
	}

	out := Summary{ChangeCount: c.comparison.Counts.Changes, ProblemCount: len(c.Problems)}

	for _, sec := range factorymodel.Sections {
		ss := SectionSummary{Section: sec}
		ss.Lines[0], ss.Lines[1] = c.ranges.Section(sec)

		for _, ch := range c.comparison.Changes {
			if ch.Section == string(sec) {
				ss.Changes++
			}
		}

		for _, p := range c.Problems {
			if p.Section == string(sec) {
				ss.Problems++
			}
		}

		out.Sections = append(out.Sections, ss)
	}

	return out, nil
}

// validateDraft validates a draft (with overlay written over its files) against the running
// version and its reference.
func (s *Service) validateDraft(
	ctx context.Context, rec ports.FactoryRecord, d domain.DefinitionVersion, overlay map[string][]byte, moves map[string]string,
) (checked, error) {
	ref, err := s.reference(ctx, d)
	if err != nil {
		return checked{}, err
	}

	c := candidate{
		factory: rec.ID, revision: d.Revision, files: s.tree(ctx, d.Manifest).with(overlay, nil),
		reference: ref, moves: moves,
	}

	if rec.Current != nil {
		cur, err := s.byID(ctx, rec.ID, rec.Current.ID, VersionSelector{Kind: SelectCurrent})
		if err != nil {
			return checked{}, err
		}

		c.current = s.tree(ctx, cur.Manifest)
	}

	return s.validate(ctx, c)
}

// reference is what a draft's changes are counted from: its base, else its starting tree;
// nil when it has neither.
func (s *Service) reference(ctx context.Context, d domain.DefinitionVersion) (*tree, error) {
	id := d.BaseID

	switch {
	case id != "":
	case d.Source.Manifest != nil:
		return s.tree(ctx, d.Source.Manifest), nil
	case d.Source.Version != "":
		id = d.Source.Version
	default:
		return nil, nil
	}

	v, err := s.store.Version(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read the reference of draft %s: %w", d.ID, err)
	}

	return s.tree(ctx, v.Manifest), nil
}

// ValidateRequest asks to validate a factory's draft.
type ValidateRequest struct {
	// Overlay is unsaved YAML-mode text, path → content, written over the draft's files.
	Overlay map[string][]byte
	// StationMoves are the moves the publish will carry, so the items they map are not
	// reported.
	StationMoves map[string]string
}

// ValidateDraft validates the draft at its revision, as Publish will.
func (s *Service) ValidateDraft(ctx context.Context, f domain.FactoryID, r ValidateRequest) (Validation, error) {
	if err := checkFiles(r.Overlay); err != nil {
		return Validation{}, err
	}

	rec, err := s.record(ctx, f)
	if err != nil {
		return Validation{}, err
	}

	d, err := s.draft(ctx, f)
	if err != nil {
		return Validation{}, err
	}

	c, err := s.validateDraft(ctx, rec, d, r.Overlay, r.StationMoves)
	if err != nil {
		return Validation{}, err
	}

	return c.Validation, nil
}

// ValidateFiles validates a definition that belongs to no factory yet (an import preview, the
// CLI validating a new folder).
func (s *Service) ValidateFiles(ctx context.Context, files map[string][]byte) (Validation, error) {
	if err := checkFiles(files); err != nil {
		return Validation{}, err
	}

	c, err := s.validate(ctx, candidate{files: (&tree{manifest: domain.Manifest{}, data: map[string][]byte{}}).with(files, nil)})
	if err != nil {
		return Validation{}, err
	}

	return c.Validation, nil
}

// OpenDraftRequest opens a factory's draft.
type OpenDraftRequest struct {
	// Base is the published version to start from (a number or current); zero means current.
	Base VersionSelector
	// Replace archives an open draft first.
	Replace bool
}

// OpenDraft opens the factory's draft on a published version, or returns the open one with
// created false. A draft open on another base is ErrDraftExists unless the request replaces it.
func (s *Service) OpenDraft(ctx context.Context, f domain.FactoryID, r OpenDraftRequest) (DraftView, bool, error) {
	by, err := actor(ctx)
	if err != nil {
		return DraftView{}, false, err
	}

	rec, err := s.record(ctx, f)
	if err != nil {
		return DraftView{}, false, err
	}

	base, err := s.openBase(ctx, rec, r.Base)
	if err != nil {
		return DraftView{}, false, err
	}

	if rec.Draft != nil && !r.Replace {
		d, err := s.draft(ctx, f)
		if err != nil {
			return DraftView{}, false, err
		}

		if r.Base.Kind != "" && d.BaseID != base {
			return DraftView{}, false, fmt.Errorf("the open draft of %s is on another version: %w", f, ErrDraftExists)
		}

		v, err := s.draftView(ctx, rec, d)

		return v, false, err
	}

	d, created, err := s.store.OpenDraft(ctx, ports.OpenDraft{Factory: f, Base: base, Replace: r.Replace, By: by})
	if err != nil {
		return DraftView{}, false, fmt.Errorf("open draft of %s: %w", f, err)
	}

	if rec, err = s.record(ctx, f); err != nil {
		return DraftView{}, false, err
	}

	v, err := s.draftView(ctx, rec, d)

	return v, created, err
}

// openBase is the published version a draft opens on: sel's, or the current one.
func (s *Service) openBase(ctx context.Context, rec ports.FactoryRecord, sel VersionSelector) (domain.DefinitionVersionID, error) {
	switch sel.Kind {
	case "", SelectCurrent:
		return currentID(rec), nil
	case SelectNumber:
		v, err := s.selectVersion(ctx, rec.ID, sel)
		if err != nil {
			return "", err
		}

		return v.ID, nil
	case SelectDraft, SelectBase, SelectSource:
	}

	return "", fmt.Errorf("a draft starts from a published version, not %s: %w", sel, ErrInvalidRequest)
}

// DiscardDraft archives the open draft at revision: ErrDraftConflict when it moved.
func (s *Service) DiscardDraft(ctx context.Context, f domain.FactoryID, revision int64) error {
	by, err := actor(ctx)
	if err != nil {
		return err
	}

	err = s.store.DiscardDraft(ctx, f, revision, by)

	switch {
	case errors.Is(err, ports.ErrConflict):
		return fmt.Errorf("discard draft of %s at revision %d: %w", f, revision, ErrDraftConflict)
	case errors.Is(err, ports.ErrNotFound):
		_, err = s.draft(ctx, f)

		return err
	case err != nil:
		return fmt.Errorf("discard draft of %s: %w", f, err)
	}

	return nil
}

// DraftModel is the factory model of a draft, for the Visual editor.
type DraftModel struct {
	Revision int64
	Model    Model
	// ETags is each section's concurrency token for SaveSection.
	ETags map[Section]string
	// Edited maps model pointers ("/line/stations/3/checks") of what the draft changed against
	// its base (or starting tree) to the value there, nil where it had none: "Edited · Undo".
	Edited map[string]any
	Ranges Ranges
}

// DraftModel reads the draft's model: a *ParseError when its factory.yaml does not parse,
// which makes Visual read-only until it is fixed in YAML mode.
func (s *Service) DraftModel(ctx context.Context, f domain.FactoryID) (DraftModel, error) {
	d, err := s.draft(ctx, f)
	if err != nil {
		return DraftModel{}, err
	}

	m, r, err := s.versionModel(s.tree(ctx, d.Manifest))
	if err != nil {
		return DraftModel{}, err
	}

	ref, err := s.referenceModel(ctx, d)
	if err != nil {
		return DraftModel{}, err
	}

	out := DraftModel{Revision: d.Revision, Model: m, ETags: map[Section]string{}, Edited: map[string]any{}, Ranges: r}

	for _, sec := range factorymodel.Sections {
		out.ETags[sec] = etag(m.Section(sec))

		for p, v := range factorymodel.Edited(ref, m, sec) {
			out.Edited["/"+string(sec)+p] = v
		}
	}

	return out, nil
}

// referenceModel is the model a draft's edits are marked against; empty when it has no
// reference or the reference does not parse.
func (s *Service) referenceModel(ctx context.Context, d domain.DefinitionVersion) (Model, error) {
	ref, err := s.reference(ctx, d)
	if err != nil || ref == nil {
		return Model{Connections: []any{}}, err
	}

	m, _, err := s.versionModel(ref)
	if err != nil && !isParseError(err) {
		return Model{}, err
	}

	return m, nil
}

// etag is the concurrency token of a section's value.
func etag(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:8])
}

// SaveResult is the draft after a save.
type SaveResult struct {
	Revision int64
	// ETag is the saved section's new token (section saves); SHA256 the saved file's hash (file
	// saves).
	ETag      string
	SHA256    domain.BlobHash
	UpdatedAt time.Time
	Summary
}

// SectionSave is one Visual save of a section.
type SectionSave struct {
	Section Section
	// IfMatch is the section's ETag the editor read; empty overwrites whatever is there.
	IfMatch string
	Value   json.RawMessage
	// Files are written in the same revision (an added agent's prompt); RemoveFiles deleted,
	// except a prompt another agent of the new value still uses.
	Files       map[string][]byte
	RemoveFiles []string
}

// SaveSection applies a section's new value to the draft's factory.yaml, keeping everything the
// section does not own (factorymodel.Apply), with its files. A stale IfMatch is a
// *SectionConflictError with the section as it is now; a factory.yaml that does not parse is a
// *ParseError and a value of the wrong shape ErrModelInvalid. Writers of other sections or
// files never conflict: a revision race re-reads and re-applies, up to three times.
func (s *Service) SaveSection(ctx context.Context, f domain.FactoryID, save SectionSave) (SaveResult, error) {
	if !validSection(save.Section) {
		return SaveResult{}, fmt.Errorf("section %q: %w", save.Section, ErrInvalidRequest)
	}

	if err := checkFiles(save.Files); err != nil {
		return SaveResult{}, err
	}

	value := func(Model) (json.RawMessage, error) { return save.Value, nil }

	return s.saveSection(ctx, f, save, value)
}

// RevertSection puts back the base's value at each of pointers (keys of DraftModel.Edited in
// section), the editor's "Undo"; a pointer that is no longer edited is ignored.
func (s *Service) RevertSection(ctx context.Context, f domain.FactoryID, section Section, ifMatch string, pointers []string) (SaveResult, error) {
	if !validSection(section) {
		return SaveResult{}, fmt.Errorf("section %q: %w", section, ErrInvalidRequest)
	}

	prefix := "/" + string(section)

	rel := make([]string, 0, len(pointers))
	for _, p := range pointers {
		r, ok := strings.CutPrefix(p, prefix)
		if !ok || (r != "" && !strings.HasPrefix(r, "/")) {
			return SaveResult{}, fmt.Errorf("pointer %q is not in section %s: %w", p, section, ErrInvalidRequest)
		}

		rel = append(rel, r)
	}

	value := func(draft Model) (json.RawMessage, error) {
		d, err := s.draft(ctx, f)
		if err != nil {
			return nil, err
		}

		ref, err := s.referenceModel(ctx, d)
		if err != nil {
			return nil, err
		}

		return revert(draft.Section(section), factorymodel.Edited(ref, draft, section), rel)
	}

	return s.saveSection(ctx, f, SectionSave{Section: section, IfMatch: ifMatch}, value)
}

// saveSection runs the save cycle: read the draft, check the section's token, apply, store the
// files, compare and set on the revision.
func (s *Service) saveSection(
	ctx context.Context, f domain.FactoryID, save SectionSave, value func(Model) (json.RawMessage, error),
) (SaveResult, error) {
	by, err := actor(ctx)
	if err != nil {
		return SaveResult{}, err
	}

	for range maxSaveAttempts {
		saved, err := s.applySection(ctx, f, save, value, by)
		if errors.Is(err, ports.ErrConflict) {
			continue
		}

		if err != nil {
			return SaveResult{}, err
		}

		res, err := s.saveResult(ctx, f, saved)
		if err != nil {
			return SaveResult{}, err
		}

		after, _, err := s.versionModel(s.tree(ctx, saved.Manifest))
		if err != nil {
			return SaveResult{}, err
		}

		res.ETag = etag(after.Section(save.Section))

		return res, nil
	}

	return SaveResult{}, fmt.Errorf("save %s of %s: the draft kept changing: %w", save.Section, f, ErrDraftConflict)
}

// applySection is one attempt of the save cycle; ErrConflict means the revision moved.
func (s *Service) applySection(
	ctx context.Context, f domain.FactoryID, save SectionSave, value func(Model) (json.RawMessage, error), by domain.Actor,
) (domain.DefinitionVersion, error) {
	d, err := s.draft(ctx, f)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	t := s.tree(ctx, d.Manifest)

	doc, _, err := t.file(config.FileName)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	m, _, err := factorymodel.FromYAML(doc, t)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	if cur := etag(m.Section(save.Section)); save.IfMatch != "" && save.IfMatch != cur {
		return domain.DefinitionVersion{}, &SectionConflictError{Section: save.Section, Value: m.Section(save.Section), ETag: cur}
	}

	v, err := value(m)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	out, err := factorymodel.Apply(doc, save.Section, v)
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("apply %s: %w", save.Section, err)
	}

	files := maps.Clone(save.Files)
	if files == nil {
		files = map[string][]byte{}
	}

	if string(out) != string(doc) {
		files[config.FileName] = out
	}

	return s.commit(ctx, d, files, keptPrompts(out, save.RemoveFiles), by)
}

// commit stores files and saves them, with remove deleted, on the draft at its revision; a
// change that changes nothing is not saved. ErrConflict is a revision race.
func (s *Service) commit(
	ctx context.Context, d domain.DefinitionVersion, files map[string][]byte, remove []string, by domain.Actor,
) (domain.DefinitionVersion, error) {
	var gone []string

	for _, p := range remove {
		if _, ok := d.Manifest[p]; ok {
			gone = append(gone, p)
		}
	}

	set := map[string][]byte{}

	for p, b := range files {
		if d.Manifest[p] != domain.HashBlob(b) {
			set[p] = b
		}
	}

	if len(set) == 0 && len(gone) == 0 {
		return d, nil
	}

	m, err := s.putFiles(ctx, set)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	saved, err := s.store.SaveDraft(ctx, ports.DraftChange{Factory: d.FactoryID, ExpectedRevision: d.Revision, Set: m, Remove: gone, By: by})

	switch {
	case errors.Is(err, ports.ErrConflict):
		return domain.DefinitionVersion{}, err
	case errors.Is(err, ports.ErrNotFound):
		return domain.DefinitionVersion{}, fmt.Errorf("the draft of %s was discarded: %w", d.FactoryID, ErrNoDraft)
	case err != nil:
		return domain.DefinitionVersion{}, fmt.Errorf("save draft of %s: %w", d.FactoryID, err)
	}

	return saved, nil
}

// saveResult summarizes a saved draft.
func (s *Service) saveResult(ctx context.Context, f domain.FactoryID, d domain.DefinitionVersion) (SaveResult, error) {
	rec, err := s.record(ctx, f)
	if err != nil {
		return SaveResult{}, err
	}

	sum, err := s.summary(ctx, rec, d)
	if err != nil {
		return SaveResult{}, err
	}

	return SaveResult{Revision: d.Revision, UpdatedAt: d.UpdatedAt, Summary: sum}, nil
}

// keptPrompts drops from remove every prompt an agent of doc still uses.
func keptPrompts(doc []byte, remove []string) []string {
	if len(remove) == 0 {
		return nil
	}

	used := map[string]bool{}

	if m, _, err := factorymodel.FromYAML(doc, nil); err == nil {
		agents, _ := m.Agents["agents"].([]any)
		for _, a := range agents {
			am, _ := a.(map[string]any)
			pm, _ := am["prompt"].(map[string]any)

			if p, ok := pm["path"].(string); ok {
				used[strings.TrimPrefix(p, "./")] = true
			}
		}
	}

	out := make([]string, 0, len(remove))

	for _, p := range remove {
		if !used[p] {
			out = append(out, p)
		}
	}

	return out
}

func validSection(s Section) bool { return slices.Contains(factorymodel.Sections, s) }

// FileWrite is one file saved in the draft.
type FileWrite struct {
	Path    string
	Content []byte
	// IfMatch is the hash the editor read; empty writes whatever is there. Create (If-None-Match:
	// *) refuses a path that exists.
	IfMatch domain.BlobHash
	Create  bool
}

// DraftFiles lists the draft's files.
func (s *Service) DraftFiles(ctx context.Context, f domain.FactoryID) ([]FileInfo, error) {
	d, err := s.draft(ctx, f)
	if err != nil {
		return nil, err
	}

	return s.fileInfos(ctx, d.Manifest)
}

// PutFile writes one file of the draft as typed (YAML mode stores text exactly). A stale
// IfMatch, or Create on a path that exists, is a *FileConflictError.
func (s *Service) PutFile(ctx context.Context, f domain.FactoryID, w FileWrite) (SaveResult, error) {
	if err := checkFiles(map[string][]byte{w.Path: w.Content}); err != nil {
		return SaveResult{}, err
	}

	return s.saveFile(ctx, f, w.Path, w.IfMatch, w.Create, func(domain.DefinitionVersion) (map[string][]byte, []string) {
		return map[string][]byte{w.Path: w.Content}, nil
	})
}

// DeleteFile deletes one file of the draft: ErrFileNotFound when it has none at path, a
// *FileConflictError when ifMatch is stale.
func (s *Service) DeleteFile(ctx context.Context, f domain.FactoryID, path string, ifMatch domain.BlobHash) (SaveResult, error) {
	return s.saveFile(ctx, f, path, ifMatch, false, func(domain.DefinitionVersion) (map[string][]byte, []string) {
		return nil, []string{path}
	})
}

func (s *Service) saveFile(
	ctx context.Context, f domain.FactoryID, path string, ifMatch domain.BlobHash, create bool,
	change func(domain.DefinitionVersion) (map[string][]byte, []string),
) (SaveResult, error) {
	by, err := actor(ctx)
	if err != nil {
		return SaveResult{}, err
	}

	for range maxSaveAttempts {
		d, err := s.draft(ctx, f)
		if err != nil {
			return SaveResult{}, err
		}

		cur, exists := d.Manifest[path]

		set, remove := change(d)

		switch {
		case create && exists, ifMatch != "" && ifMatch != cur:
			return SaveResult{}, &FileConflictError{Path: path, SHA256: cur}
		case set == nil && !exists:
			return SaveResult{}, fmt.Errorf("the draft of %s has no %s: %w", f, path, ErrFileNotFound)
		}

		saved, err := s.commit(ctx, d, set, remove, by)
		if errors.Is(err, ports.ErrConflict) {
			continue
		}

		if err != nil {
			return SaveResult{}, err
		}

		res, err := s.saveResult(ctx, f, saved)
		if err != nil {
			return SaveResult{}, err
		}

		res.SHA256 = saved.Manifest[path]

		return res, nil
	}

	return SaveResult{}, fmt.Errorf("save %s of %s: the draft kept changing: %w", path, f, ErrDraftConflict)
}

// ReplaceTree makes files the draft's whole tree (goal factory push). With expected set, a
// draft at another revision is ErrDraftConflict; without, the latest draft is replaced.
func (s *Service) ReplaceTree(ctx context.Context, f domain.FactoryID, files map[string][]byte, expected *int64) (SaveResult, error) {
	by, err := actor(ctx)
	if err != nil {
		return SaveResult{}, err
	}

	if err := checkFiles(files); err != nil {
		return SaveResult{}, err
	}

	m, err := s.putFiles(ctx, files)
	if err != nil {
		return SaveResult{}, err
	}

	for range maxSaveAttempts {
		d, err := s.draft(ctx, f)
		if err != nil {
			return SaveResult{}, err
		}

		if expected != nil && *expected != d.Revision {
			return SaveResult{}, fmt.Errorf("the draft of %s is at revision %d, not %d: %w", f, d.Revision, *expected, ErrDraftConflict)
		}

		saved := d
		if !maps.Equal(d.Manifest, m) {
			saved, err = s.store.SaveDraft(ctx, ports.DraftChange{Factory: f, ExpectedRevision: d.Revision, Set: m, Replace: true, By: by})
		}

		if errors.Is(err, ports.ErrConflict) && expected == nil {
			continue
		}

		if errors.Is(err, ports.ErrConflict) {
			return SaveResult{}, fmt.Errorf("replace the draft of %s: %w", f, ErrDraftConflict)
		}

		if err != nil {
			return SaveResult{}, fmt.Errorf("replace the draft of %s: %w", f, err)
		}

		return s.saveResult(ctx, f, saved)
	}

	return SaveResult{}, fmt.Errorf("replace the draft of %s: the draft kept changing: %w", f, ErrDraftConflict)
}
