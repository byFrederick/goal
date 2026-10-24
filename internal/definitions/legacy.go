package definitions

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
)

// LegacyActor imports the factories of the bucket layout before versions.
var LegacyActor = domain.Actor{Kind: domain.ActorSystem, ID: "legacy-import", Name: "Legacy import"}

// LegacyCatalog lists and reads an organization's factories in the bucket layout before
// versions (orgs/<org-id>/factories/<id>/); *factorystore.Catalog satisfies it.
type LegacyCatalog interface {
	List(ctx context.Context, org domain.OrgID) ([]factorystore.Entry, error)
}

// LegacyImport counts what ImportLegacy did with the legacy factories it found: Published
// became version 1, Drafts became a draft because they do not validate (they do not run until
// fixed and published), Present had versions already and were left alone, and Problems could
// not be imported (each is logged).
type LegacyImport struct {
	Factories, Published, Drafts, Present, Problems int
}

// ImportLegacy turns every factory of every live organization's legacy prefix that has no
// version yet into one, keeping the file bytes as they are: version 1, published and current,
// when it validates; a draft otherwise. The row goald registered for the factory before
// versions is adopted, so its items and runs stay with it. A factory that has versions is left
// alone; when its legacy objects changed after the import (an old CLI pushed), a warning says
// that those changes are ignored.
//
// It is idempotent and safe to run on several goald instances at once: each factory's import
// is one transaction of the store that does nothing once the factory has a version, and blobs
// uploaded by an import that did not commit are only unreferenced. The legacy objects are left
// in place, so the previous release still reads them. An error means a store or the bucket
// failed; what was imported until then stays imported.
func ImportLegacy(ctx context.Context, catalog LegacyCatalog, store ports.DefinitionStore, orgs Orgs, log *slog.Logger) (LegacyImport, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	var res LegacyImport

	active, err := orgs.ActiveOrgs(ctx)
	if err != nil {
		return res, fmt.Errorf("list organizations: %w", err)
	}

	for _, org := range slices.Sorted(slices.Values(active)) {
		octx := domain.WithOrg(ctx, org)

		entries, err := catalog.List(octx, org)
		if err != nil {
			return res, fmt.Errorf("list legacy factories of organization %s: %w", org, err)
		}

		for _, e := range entries {
			res.Factories++

			l := legacy{
				store: store, org: org, entry: e,
				log: log.With(slog.String("org_id", string(org)), slog.String("factory", e.ID)),
			}

			outcome, err := l.importOnce(octx)
			if err != nil {
				return res, fmt.Errorf("import legacy factory %s: %w", factorystore.Prefix(org, e.ID), err)
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
	}

	return res, nil
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
	store ports.DefinitionStore
	org   domain.OrgID
	entry factorystore.Entry
	log   *slog.Logger
}

func (l legacy) importOnce(ctx context.Context) (importOutcome, error) {
	e := l.entry
	id := domain.FactoryID(e.ID)

	if e.Snapshot == nil {
		l.log.ErrorContext(ctx, "legacy factory cannot be read; not imported", slog.Any("error", e.Err))
		return notImported, nil
	}

	rec, err := l.store.FactoryRecord(ctx, id)

	switch {
	case err == nil && (rec.Current != nil || rec.Draft != nil):
		return alreadyPresent, l.warnIfChanged(ctx, rec)
	case err != nil && !errors.Is(err, ports.ErrNotFound):
		return 0, err
	}

	in, ok, err := l.prepare(ctx, rec)
	if err != nil || !ok {
		return notImported, err
	}

	v, imported, err := l.store.ImportLegacy(ctx, in)

	return l.outcome(ctx, in, v, imported, err)
}

// outcome reports and logs what the store did with an import.
func (l legacy) outcome(ctx context.Context, in ports.LegacyImport, v domain.DefinitionVersion, imported bool, err error,
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
			slog.String("name", in.Name), slog.Int("files", len(in.Manifest)), slog.String("problems", l.entry.Err.Error()))

		return importedDraft, nil
	}

	l.log.InfoContext(ctx, "legacy factory imported", slog.String("name", in.Name), slog.Int("number", v.Number),
		slog.String("version_id", string(v.ID)), slog.Int("files", len(in.Manifest)))

	return importedPublished, nil
}

// prepare stores the legacy factory's files as blobs and returns its import; ok is false (and
// the reason logged) when it cannot be imported.
func (l legacy) prepare(ctx context.Context, rec ports.FactoryRecord) (in ports.LegacyImport, ok bool, err error) {
	e := l.entry

	name, title, ok := l.identity(rec)
	if !ok {
		l.log.ErrorContext(ctx, "legacy factory has no readable name; not imported", slog.Any("error", e.Err))
		return in, false, nil
	}

	files, err := snapshotFiles(e.Snapshot)
	if err != nil {
		return in, false, err
	}

	manifest, err := l.store.PutBlobs(ctx, files)
	if errors.Is(err, domain.ErrInvalid) {
		l.log.ErrorContext(ctx, "legacy factory has a file versions cannot hold; not imported", slog.Any("error", err))
		return in, false, nil
	}

	if err != nil {
		return in, false, err
	}

	return ports.LegacyImport{
		ID: domain.FactoryID(e.ID), Name: name, Title: title, Manifest: manifest, Publish: e.Err == nil,
		Source:  domain.VersionSource{Kind: domain.SourceLegacy, LegacyVersions: objectVersions(e.Snapshot)},
		Message: "Imported from " + factorystore.Prefix(l.org, e.ID), By: LegacyActor,
	}, true, nil
}

// identity is the name and title the imported factory takes: its definition's, or, when that
// does not even parse, those of the row goald registered for it.
func (l legacy) identity(rec ports.FactoryRecord) (name, title string, ok bool) {
	f := l.entry.Factory
	if f == nil {
		f, _ = config.Check(l.entry.Snapshot.FS())
	}

	switch {
	case f != nil && f.Metadata.Name != "":
		return f.Metadata.Name, f.Metadata.Title, true
	case rec.Name != "":
		return rec.Name, rec.Title, true
	}

	return "", "", false
}

// warnIfChanged warns when the legacy objects are no longer the ones the import read.
func (l legacy) warnIfChanged(ctx context.Context, rec ports.FactoryRecord) error {
	v, err := l.importedVersion(ctx, rec)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if v.Source.Kind == domain.SourceLegacy && !maps.Equal(v.Source.LegacyVersions, objectVersions(l.entry.Snapshot)) {
		l.log.WarnContext(ctx, "legacy factory objects changed after the import and are ignored; "+
			"publish the change as a new version through goald instead",
			slog.String("prefix", factorystore.Prefix(l.org, l.entry.ID)))
	}

	return nil
}

// importedVersion is the version the import made: version 1, or the draft of a factory that
// did not validate.
func (l legacy) importedVersion(ctx context.Context, rec ports.FactoryRecord) (domain.DefinitionVersion, error) {
	if rec.Current != nil {
		return l.store.VersionByNumber(ctx, rec.ID, 1)
	}

	return l.store.Draft(ctx, rec.ID)
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

// objectVersions are the bucket object versions a snapshot read, by path.
func objectVersions(s *factorystore.Snapshot) map[string]string {
	out := make(map[string]string, len(s.Files))
	for _, o := range s.Files {
		out[o.Key] = o.Version
	}

	return out
}
