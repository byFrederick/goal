package definitions

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
)

// seedLegacy writes files under a factory's legacy prefix in o, as the CLI pushed them before
// versions, and returns the object version each got.
func (h *harness) seedLegacy(t *testing.T, o domain.OrgID, id domain.FactoryID, files map[string]string) map[string]string {
	t.Helper()

	versions := map[string]string{}

	for p, body := range files {
		info, err := h.objects.Put(t.Context(), factorystore.Prefix(o, string(id))+p, []byte(body))
		if err != nil {
			t.Fatal(err)
		}

		versions[p] = info.Version
	}

	return versions
}

// legacyOrgs makes the import pending for org and other, the organizations from before versions.
func (h *harness) legacyOrgs() {
	h.store.PendLegacyImport(org)
	h.store.PendLegacyImport(other)
}

// valid is authoring's verdict on a definition with no problem.
func valid(context.Context, map[string][]byte) ([]domain.DefinitionProblem, error) { return nil, nil }

func (h *harness) importLegacy(t *testing.T, log *slog.Logger) LegacyImport {
	t.Helper()

	return h.importWith(t, Legacy{Catalog: factorystore.New(h.objects), Validate: valid, Log: log})
}

// importWith runs the import with l's catalog, validation and log over the harness's store and
// organizations.
func (h *harness) importWith(t *testing.T, l Legacy) LegacyImport {
	t.Helper()

	l.Store, l.Orgs = h.store, h.orgs

	res, err := ImportLegacy(t.Context(), l)
	if err != nil {
		t.Fatal(err)
	}

	return res
}

func TestImportLegacy_PublishesVersionOneWithTheSameBytesOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.legacyOrgs()

	files := acme(acmeName, noteContract)
	files["agents/writer.md"] = "# writer\r\n\n  kept exactly, trailing spaces too  \n" // no reformatting
	objectVersions := h.seedLegacy(t, org, acmeID, files)
	h.store.AddLegacyFactory(org, acmeID, "acme-before", "") // the row goald registered before versions

	if res := h.importLegacy(t, nil); res != (LegacyImport{Factories: 1, Published: 1}) {
		t.Fatalf("import = %+v, want one factory published", res)
	}

	ctx := ctxOf(t, org)

	v := h.importedV1(t, files, objectVersions)

	if res := h.importLegacy(t, nil); res != (LegacyImport{}) {
		t.Errorf("second import = %+v, want nothing read: the organizations' imports ended", res)
	}

	if vs, err := h.store.Versions(ctx, acmeID, 0, 10); err != nil || len(vs) != 1 {
		t.Errorf("versions after a second import = %d (%v), want 1", len(vs), err)
	}

	if lines, err := h.cache(t, nil).Refresh(t.Context()); err != nil || len(lines) != 1 || lines[0].VersionID != v.ID {
		t.Errorf("lines = %+v (%v), want the imported v1", lines, err)
	}
}

// importedV1 checks that acme runs version 1 made by the import of files, read at
// objectVersions, with the bytes unchanged, and returns it.
func (h *harness) importedV1(t *testing.T, files, objectVersions map[string]string) domain.DefinitionVersion {
	t.Helper()

	ctx := ctxOf(t, org)

	rec, err := h.store.FactoryRecord(ctx, acmeID)
	if err != nil || rec.Current == nil || rec.Current.Number != 1 || rec.Name != acmeName || rec.Draft != nil {
		t.Fatalf("factory = %+v (%v), want the adopted row on v1, renamed %s", rec, err, acmeName)
	}

	v, err := h.store.Version(ctx, rec.Current.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !fromLegacy(v, objectVersions) {
		t.Errorf("v1 = %+v; want published by the legacy import from the object versions read", v)
	}

	if got, want := slices.Sorted(maps.Keys(v.Manifest)), slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
		t.Errorf("manifest paths = %v, want %v", got, want)
	}

	for p, body := range files {
		got, err := h.store.Blob(ctx, v.Manifest[p])
		if err != nil || !bytes.Equal(got, []byte(body)) {
			t.Errorf("%s = %q (%v), want the legacy bytes %q", p, got, err, body)
		}
	}

	return v
}

// fromLegacy reports whether acme's v was published by the import from objectVersions.
func fromLegacy(v domain.DefinitionVersion, objectVersions map[string]string) bool {
	return v.PublishedBy.Ref() == "system:legacy-import" && v.Source.Kind == domain.SourceLegacy &&
		maps.Equal(v.Source.LegacyVersions, objectVersions) && v.Message == "Imported from "+factorystore.Prefix(org, string(acmeID))
}

func TestImportLegacy_AFactoryThatDoesNotValidateBecomesADraft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.legacyOrgs()

	broken := acme(acmeName, noteContract)
	delete(broken, "agents/writer.md") // the prompt is missing: it loads no more
	h.seedLegacy(t, org, acmeID, broken)

	unreadable := map[string]string{config.FileName: "{ not yaml"}
	h.seedLegacy(t, org, otherID, unreadable) // no name to give it, no row to take one from

	var logs bytes.Buffer

	if res := h.importLegacy(t, slog.New(slog.NewTextHandler(&logs, nil))); res != (LegacyImport{Factories: 2, Drafts: 1, Problems: 1}) {
		t.Fatalf("import = %+v, want one draft and one problem", res)
	}

	rec, err := h.store.FactoryRecord(ctxOf(t, org), acmeID)
	if err != nil || rec.Current != nil || rec.Draft == nil || rec.Name != acmeName {
		t.Errorf("factory = %+v (%v), want a draft and no current version", rec, err)
	}

	if _, err := h.store.FactoryRecord(ctxOf(t, org), otherID); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("unreadable factory = %v, want not imported", err)
	}

	if !strings.Contains(logs.String(), "imported as a draft") || !strings.Contains(logs.String(), "no readable name") {
		t.Errorf("logs do not explain the draft and the problem:\n%s", logs.String())
	}

	if lines, err := h.cache(t, nil).Refresh(t.Context()); err != nil || len(lines) != 0 {
		t.Errorf("lines = %+v (%v), want none: a draft does not run", lines, err)
	}
}

// A definition that loads but that authoring would refuse to publish, such as one reading one of
// goald's own settings, is imported as a draft: the import runs nothing a publish would not.
func TestImportLegacy_WhatAuthoringRefusesBecomesADraft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.legacyOrgs()
	h.seedLegacy(t, org, acmeID, acme(acmeName, noteContract))

	var asked atomic.Int32

	refuse := func(_ context.Context, files map[string][]byte) ([]domain.DefinitionProblem, error) {
		asked.Add(1)

		if _, ok := files[config.FileName]; !ok {
			t.Errorf("validated files %v, want the factory's", slices.Sorted(maps.Keys(files)))
		}

		return []domain.DefinitionProblem{{Code: "env-reserved", Severity: domain.SeverityError, Message: "env:GOAL_DATABASE_URL is reserved"}}, nil
	}

	var logs bytes.Buffer

	res := h.importWith(t, Legacy{Catalog: factorystore.New(h.objects), Validate: refuse, Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if res != (LegacyImport{Factories: 1, Drafts: 1}) || asked.Load() != 1 {
		t.Fatalf("import = %+v after %d validations, want one draft after one", res, asked.Load())
	}

	if rec, err := h.store.FactoryRecord(ctxOf(t, org), acmeID); err != nil || rec.Current != nil || rec.Draft == nil {
		t.Errorf("factory = %+v (%v), want a draft and no current version", rec, err)
	}

	if !strings.Contains(logs.String(), "env:GOAL_DATABASE_URL is reserved") {
		t.Errorf("logs do not say why it is a draft:\n%s", logs.String())
	}
}

func TestImportLegacy_ImportsEachOrganizationsFactories(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.legacyOrgs()
	h.seedLegacy(t, org, acmeID, acme(acmeName, noteContract))
	h.seedLegacy(t, other, otherID, withID(acme(acmeName, noteContract), otherID))

	if res := h.importLegacy(t, nil); res != (LegacyImport{Factories: 2, Published: 2}) {
		t.Fatalf("import = %+v, want both published", res)
	}

	for o, id := range map[domain.OrgID]domain.FactoryID{org: acmeID, other: otherID} {
		if rec, err := h.store.FactoryRecord(ctxOf(t, o), id); err != nil || rec.Current == nil {
			t.Errorf("factory %s in %s = %+v (%v), want v1 in its own organization", id, o, rec, err)
		}
	}
}

// Once an organization's import ran, deleting a factory it imported as a draft deletes it for
// good: the next start does not import it again.
func TestImportLegacy_ADeletedImportedDraftIsNotRecreated(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.legacyOrgs()

	broken := acme(acmeName, noteContract)
	delete(broken, "agents/writer.md")
	h.seedLegacy(t, org, acmeID, broken)

	if res := h.importLegacy(t, nil); res != (LegacyImport{Factories: 1, Drafts: 1}) {
		t.Fatalf("import = %+v, want one draft", res)
	}

	if err := h.store.DeleteUnpublished(ctxOf(t, org), acmeID, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if res := h.importLegacy(t, nil); res != (LegacyImport{}) {
		t.Errorf("import after the delete = %+v, want nothing", res)
	}

	if _, err := h.store.FactoryRecord(ctxOf(t, org), acmeID); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("deleted draft = %v, want it gone", err)
	}
}

// goald ignores definition files written to the bucket directly: files planted under a legacy
// prefix after the organization's import, or under that of an organization created after
// versions, never become a factory.
func TestImportLegacy_FilesPlantedLaterAreIgnored(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.store.PendLegacyImport(org) // other was created after versions: it has no import to do
	h.seedLegacy(t, org, acmeID, acme(acmeName, noteContract))

	if res := h.importLegacy(t, nil); res != (LegacyImport{Factories: 1, Published: 1}) {
		t.Fatalf("import = %+v, want acme published", res)
	}

	h.seedLegacy(t, org, otherID, withID(acme("planted", noteContract), otherID))
	h.seedLegacy(t, other, otherID, withID(acme("planted", noteContract), otherID))

	if res := h.importLegacy(t, nil); res != (LegacyImport{}) {
		t.Errorf("import after the planting = %+v, want nothing", res)
	}

	for _, o := range []domain.OrgID{org, other} {
		if _, err := h.store.FactoryRecord(ctxOf(t, o), otherID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("planted factory in %s = %v, want not imported", o, err)
		}
	}
}

// countingObjects counts the objects read from a bucket.
type countingObjects struct {
	ports.ObjectStore

	gets atomic.Int32
}

func (c *countingObjects) Get(ctx context.Context, key, version string) (ports.Object, error) {
	c.gets.Add(1)

	return c.ObjectStore.Get(ctx, key, version)
}

// An import that did not end (goald stopped part-way) is finished by the next start, which reads
// only the files of the factories it has yet to import, and warns when the legacy objects of an
// imported factory changed meanwhile (an old CLI pushed).
func TestImportLegacy_AnUnfinishedImportReadsOnlyWhatIsLeft(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.legacyOrgs()
	h.seedLegacy(t, org, acmeID, acme(acmeName, noteContract))
	h.importLegacy(t, nil)

	h.store.PendLegacyImport(org) // as if goald had stopped before the end of the import
	h.seedLegacy(t, org, otherID, withID(acme("notes", noteContract), otherID))

	objects := &countingObjects{ObjectStore: h.objects}

	var quiet bytes.Buffer

	res := h.importWith(t, Legacy{Catalog: factorystore.New(objects), Validate: valid, Log: slog.New(slog.NewTextHandler(&quiet, nil))})
	if res != (LegacyImport{Factories: 2, Published: 1, Present: 1}) {
		t.Fatalf("import = %+v, want acme left alone and the other factory published", res)
	}

	if got, want := int(objects.gets.Load()), len(acme("notes", noteContract)); got != want {
		t.Errorf("read %d objects, want only the %d of the factory not imported yet", got, want)
	}

	h.store.PendLegacyImport(org)
	h.seedLegacy(t, org, acmeID, map[string]string{"agents/writer.md": "# writer, pushed by an old CLI\n"})

	var logs bytes.Buffer
	if res := h.importLegacy(t, slog.New(slog.NewTextHandler(&logs, nil))); res != (LegacyImport{Factories: 2, Present: 2}) {
		t.Errorf("import = %+v, want both left alone", res)
	}

	const warning = "changed after the import"
	if strings.Contains(quiet.String(), warning) || !strings.Contains(logs.String(), warning) {
		t.Errorf("warnings: unchanged %q, changed %q; want one only for the change", quiet.String(), logs.String())
	}
}
