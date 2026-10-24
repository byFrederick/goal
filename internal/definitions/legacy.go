package definitions

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
)

// LegacyActor imports the factories of the bucket layout before versions.
var LegacyActor = domain.Actor{Kind: domain.ActorSystem, ID: "legacy-import", Name: "Legacy import"}

// LegacyCatalog lists an organization's factories in the bucket layout before versions
// (orgs/<org-id>/factories/<id>/) and reads one; *factorystore.Catalog satisfies it.
type LegacyCatalog interface {
	List(ctx context.Context, org domain.OrgID) ([]factorystore.Listed, error)
	Read(ctx context.Context, l factorystore.Listed) (factorystore.Entry, error)
}

// LegacyValidator returns a definition's problems as a publish would find them: goald wires
// authoring's validation (authoring.Service.ValidateFiles), so the import runs nothing a publish
// would refuse. Its blocking problems make the import a draft.
type LegacyValidator func(ctx context.Context, files map[string][]byte) ([]domain.DefinitionProblem, error)

// Legacy is what ImportLegacy works with. Log may be nil.
type Legacy struct {
	Catalog  LegacyCatalog
	Store    ports.DefinitionStore
	Orgs     Orgs
	Validate LegacyValidator
	Log      *slog.Logger
}

// LegacyImport counts what ImportLegacy did with the legacy factories it found: Published
// became version 1, Drafts became a draft because they do not validate (they do not run until
// fixed and published), Present had versions already and were left alone, and Problems could
// not be imported (each is logged).
type LegacyImport struct {
	Factories, Published, Drafts, Present, Problems int
}

// ImportLegacy imports the legacy factories of every live organization whose legacy import is
// pending (DefinitionStore.LegacyImportPending: the organizations from before versions, until
// their import ends), then ends that organization's import, so its legacy prefix is never read
// again: a factory deleted afterwards is not imported again, and files written there later are
// ignored. Each factory with no version yet becomes one, keeping the file bytes as they are:
// version 1, published and current, when it loads and authoring's validation finds no blocking
// problem; a draft otherwise. The row goald registered for the factory before versions is
// adopted, so its items and runs stay with it. Only the files of factories not imported yet are
// read. A factory that has versions is left alone; when its legacy objects changed after the
// import (an old CLI pushed), a warning says that those changes are ignored. A factory that
// cannot be imported at all is logged, and is not tried again once the import ends.
//
// It is idempotent and safe to run on several goald instances at once: each factory's import
// is one transaction of the store that does nothing once the factory has a version, and blobs
// uploaded by an import that did not commit are only unreferenced. The legacy objects are left
// in place, so the previous release still reads them. An error means a store or the bucket
// failed: what was imported until then stays imported, and the organization's import stays
// pending, so the next start finishes it.
func ImportLegacy(ctx context.Context, l Legacy) (LegacyImport, error) {
	if l.Log == nil {
		l.Log = slog.New(slog.DiscardHandler)
	}

	var res LegacyImport

	active, err := l.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return res, fmt.Errorf("list organizations: %w", err)
	}

	for _, org := range slices.Sorted(slices.Values(active)) {
		octx := domain.WithOrg(ctx, org)

		pending, err := l.Store.LegacyImportPending(octx)
		if err != nil {
			return res, fmt.Errorf("organization %s: %w", org, err)
		}

		if !pending {
			continue
		}

		if err := l.importOrg(octx, org, &res); err != nil {
			return res, err
		}
	}

	return res, nil
}

// importOrg imports org's legacy factories into res, then ends org's import.
func (l Legacy) importOrg(ctx context.Context, org domain.OrgID, res *LegacyImport) error {
	listed, err := l.Catalog.List(ctx, org)
	if err != nil {
		return fmt.Errorf("list legacy factories of organization %s: %w", org, err)
	}

	problems := res.Problems

	for _, ls := range listed {
		res.Factories++

		f := legacy{Legacy: l, org: org, listed: ls, log: l.Log.With(slog.String("org_id", string(org)), slog.String("factory", ls.ID))}

		outcome, err := f.importOnce(ctx)
		if err != nil {
			return fmt.Errorf("import legacy factory %s: %w", factorystore.Prefix(org, ls.ID), err)
		}

		switch outcome {
		case importedPublished:
			res.Published++
		case importedDraft:
			res.Drafts++
		case alreadyPresent:
			res.Present++
		case notImported:
			res.Problems++
		}
	}

	if err := l.Store.EndLegacyImport(ctx); err != nil {
		return fmt.Errorf("organization %s: %w", org, err)
	}

	if n := res.Problems - problems; n > 0 {
		l.Log.WarnContext(ctx, "legacy import ended with factories not imported; they are not tried again: "+
			"push each with goal factory push", slog.String("org_id", string(org)), slog.Int("not_imported", n))
	}

	return nil
}

type importOutcome int

const (
	importedPublished importOutcome = iota
	importedDraft
	alreadyPresent
	notImported
)

// legacy is one legacy factory being imported.
type legacy struct {
	Legacy

	org    domain.OrgID
	listed factorystore.Listed
	log    *slog.Logger
}

func (l legacy) importOnce(ctx context.Context) (importOutcome, error) {
	rec, err := l.Store.FactoryRecord(ctx, domain.FactoryID(l.listed.ID))

	switch {
	case err == nil && (rec.Current != nil || rec.Draft != nil):
		return alreadyPresent, l.warnIfChanged(ctx, rec)
	case err != nil && !errors.Is(err, ports.ErrNotFound):
		return 0, err
	}

	e, err := l.Catalog.Read(ctx, l.listed)
	if err != nil {
		return 0, err
	}

	if e.Snapshot == nil {
		l.log.ErrorContext(ctx, "legacy factory cannot be read; not imported", slog.Any("error", e.Err))
		return notImported, nil
	}

	in, why, ok, err := l.prepare(ctx, rec, e)
	if err != nil || !ok {
		return notImported, err
	}

	v, imported, err := l.Store.ImportLegacy(ctx, in)

	return l.outcome(ctx, in, why, v, imported, err)
}

// outcome reports and logs what the store did with an import; why says why it is a draft.
func (l legacy) outcome(ctx context.Context, in ports.LegacyImport, why string, v domain.DefinitionVersion, imported bool, err error,
) (importOutcome, error) {
	switch {
	case errors.Is(err, ports.ErrNameTaken):
		l.log.ErrorContext(ctx, "another factory of the organization has the legacy factory's name; not imported",
			slog.String("name", in.Name))

		return notImported, nil
	case errors.Is(err, ports.ErrConflict):
		// Another goald imported it meanwhile, or the id belongs to another organization.
		l.log.WarnContext(ctx, "legacy factory not imported: its id is taken", slog.Any("error", err))
		return notImported, nil
	case err != nil:
		return 0, err
	case !imported:
		return alreadyPresent, nil
	case !in.Publish:
		l.log.WarnContext(ctx, "legacy factory does not validate: imported as a draft; it runs once fixed and published",
			slog.String("name", in.Name), slog.Int("files", len(in.Manifest)), slog.String("problems", why))

		return importedDraft, nil
	}

	l.log.InfoContext(ctx, "legacy factory imported", slog.String("name", in.Name), slog.Int("number", v.Number),
		slog.String("version_id", string(v.ID)), slog.Int("files", len(in.Manifest)))

	return importedPublished, nil
}

// prepare stores the legacy factory's files as blobs and returns its import, published only when
// e loads and validates (why says otherwise why not); ok is false (and the reason logged) when it
// cannot be imported.
func (l legacy) prepare(ctx context.Context, rec ports.FactoryRecord, e factorystore.Entry) (in ports.LegacyImport, why string, ok bool, err error) {
	name, title, ok := identity(e, rec)
	if !ok {
		l.log.ErrorContext(ctx, "legacy factory has no readable name; not imported", slog.Any("error", e.Err))
		return in, "", false, nil
	}

	files, err := snapshotFiles(e.Snapshot)
	if err != nil {
		return in, "", false, err
	}

	manifest, err := l.Store.PutBlobs(ctx, files)
	if errors.Is(err, domain.ErrInvalid) {
		l.log.ErrorContext(ctx, "legacy factory has a file versions cannot hold; not imported", slog.Any("error", err))
		return in, "", false, nil
	}

	if err != nil {
		return in, "", false, err
	}

	if e.Err != nil {
		why = e.Err.Error()
	} else if why, err = l.refusal(ctx, files); err != nil {
		return in, "", false, err
	}

	return ports.LegacyImport{
		ID: domain.FactoryID(e.ID), Name: name, Title: title, Manifest: manifest, Publish: why == "",
		Source:  domain.VersionSource{Kind: domain.SourceLegacy, LegacyVersions: l.listed.Versions()},
		Message: "Imported from " + factorystore.Prefix(l.org, e.ID), By: LegacyActor,
	}, why, true, nil
}

// refusal is why a publish would refuse files, from their blocking problems; empty when it
// would not.
func (l legacy) refusal(ctx context.Context, files map[string][]byte) (string, error) {
	problems, err := l.Validate(ctx, files)
	if err != nil {
		return "", fmt.Errorf("validate: %w", err)
	}

	var why []string

	for _, p := range problems {
		if p.Blocking() {
			why = append(why, p.Code+": "+p.Message)
		}
	}

	return strings.Join(why, "; "), nil
}

// identity is the name and title the imported factory takes: its definition's, or, when that
// does not even parse, those of the row goald registered for it.
func identity(e factorystore.Entry, rec ports.FactoryRecord) (name, title string, ok bool) {
	f := e.Factory
	if f == nil {
		f, _ = config.Check(e.Snapshot.FS())
	}

	switch {
	case f != nil && f.Metadata.Name != "":
		return f.Metadata.Name, f.Metadata.Title, true
	case rec.Name != "":
		return rec.Name, rec.Title, true
	}

	return "", "", false
}

// warnIfChanged warns when the legacy objects listed are no longer the ones the import read.
func (l legacy) warnIfChanged(ctx context.Context, rec ports.FactoryRecord) error {
	v, err := l.importedVersion(ctx, rec)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if v.Source.Kind == domain.SourceLegacy && !maps.Equal(v.Source.LegacyVersions, l.listed.Versions()) {
		l.log.WarnContext(ctx, "legacy factory objects changed after the import and are ignored; "+
			"publish the change as a new version through goald instead",
			slog.String("prefix", factorystore.Prefix(l.org, l.listed.ID)))
	}

	return nil
}

// importedVersion is the version the import made: version 1, or the draft of a factory that
// did not validate.
func (l legacy) importedVersion(ctx context.Context, rec ports.FactoryRecord) (domain.DefinitionVersion, error) {
	if rec.Current != nil {
		return l.Store.VersionByNumber(ctx, rec.ID, 1)
	}

	return l.Store.Draft(ctx, rec.ID)
}

// snapshotFiles reads every file of a snapshot.
func snapshotFiles(s *factorystore.Snapshot) (map[string][]byte, error) {
	files := make(map[string][]byte, len(s.Files))

	for _, o := range s.Files {
		b, err := fs.ReadFile(s.FS(), o.Key)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", o.Key, err)
		}

		files[o.Key] = b
	}

	return files, nil
}
