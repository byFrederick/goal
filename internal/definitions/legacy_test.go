package definitions

import (
	"bytes"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
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

func (h *harness) importLegacy(t *testing.T, log *slog.Logger) LegacyImport {
	t.Helper()

	res, err := ImportLegacy(t.Context(), factorystore.New(h.objects), h.store, h.orgs, log)
	if err != nil {
		t.Fatal(err)
	}

	return res
}

func TestImportLegacy_PublishesVersionOneWithTheSameBytesOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	files := acme(acmeName, noteContract)
	files["agents/writer.md"] = "# writer\r\n\n  kept exactly, trailing spaces too  \n" // no reformatting
	objectVersions := h.seedLegacy(t, org, acmeID, files)
	h.store.AddLegacyFactory(org, acmeID, "acme-before", "") // the row goald registered before versions

	if res := h.importLegacy(t, nil); res != (LegacyImport{Factories: 1, Published: 1}) {
		t.Fatalf("import = %+v, want one factory published", res)
	}

	ctx := ctxOf(t, org)

	v := h.importedV1(t, files, objectVersions)

	if res := h.importLegacy(t, nil); res != (LegacyImport{Factories: 1, Present: 1}) {
		t.Errorf("second import = %+v, want the factory left alone", res)
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

	if res := h.importLegacy(t, nil); res != (LegacyImport{Factories: 2, Present: 1, Problems: 1}) {
		t.Errorf("second import = %+v, want the draft left alone", res)
	}
}

func TestImportLegacy_ImportsEachOrganizationsFactories(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
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

func TestImportLegacy_WarnsWhenLegacyObjectsChangeAfterTheImport(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seedLegacy(t, org, acmeID, acme(acmeName, noteContract))
	h.importLegacy(t, nil)

	var quiet bytes.Buffer
	h.importLegacy(t, slog.New(slog.NewTextHandler(&quiet, nil)))

	h.seedLegacy(t, org, acmeID, map[string]string{"agents/writer.md": "# writer, pushed by an old CLI\n"})

	var logs bytes.Buffer
	if res := h.importLegacy(t, slog.New(slog.NewTextHandler(&logs, nil))); res != (LegacyImport{Factories: 1, Present: 1}) {
		t.Errorf("import = %+v, want the factory left alone", res)
	}

	const warning = "changed after the import"
	if strings.Contains(quiet.String(), warning) || !strings.Contains(logs.String(), warning) {
		t.Errorf("warnings: unchanged %q, changed %q; want one only for the change", quiet.String(), logs.String())
	}
}
