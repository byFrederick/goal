package definitionstest

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

func testCreateFactory(f *fixture) {
	t := f.t
	m := f.blobs(t, tree(acme))
	src := domain.VersionSource{Kind: domain.SourceTemplate, Template: "cost-review@2", Manifest: m}
	id := newFactoryID()

	r, d, err := f.Definitions.CreateFactory(f.ctx(t), ports.NewFactory{
		ID: id, Name: acme, Title: acmeTitle, Manifest: m, Source: src, By: local,
	})
	if err != nil {
		t.Fatalf("CreateFactory: %v", err)
	}

	checkVersion(t, "first draft", d, domain.DefinitionVersion{
		OrgID: f.org, FactoryID: id, State: domain.VersionDraft, Revision: 1, Manifest: m, Source: src,
		CreatedBy: local, UpdatedBy: local,
	})

	want := ports.FactoryRecord{ID: id, OrgID: f.org, Name: acme, Title: acmeTitle, Draft: ref(d), CreatedBy: local}
	checkRecord(t, "created factory", r, want)
	checkRecord(t, "FactoryRecord", f.record(t, id), want)
	checkVersion(t, "Draft", f.draft(t, id), d)

	if got := f.records(t); len(got) != 1 || got[0].ID != id {
		t.Errorf("FactoryRecords = %+v; want only %s", got, id)
	}

	checkEntry(t, f.audited(t, id, domain.AuditFactoryCreated), local, domain.AuditTargetFactory, string(id),
		map[string]any{"source": "template"})
}

func testCreateFactoryTaken(f *fixture) {
	t := f.t
	r, _ := f.create(t, "zeta")
	f.create(t, acme)

	_, _, err := f.Definitions.CreateFactory(f.ctx(t), ports.NewFactory{
		ID: newFactoryID(), Name: strings.ToUpper(acme), Manifest: f.blobs(t, tree(acme)), Source: blank, By: local,
	})
	expect(t, "CreateFactory with a taken name in another case", err, ports.ErrNameTaken)

	_, _, err = f.Definitions.CreateFactory(f.ctx(t), ports.NewFactory{
		ID: r.ID, Name: "other", Manifest: f.blobs(t, tree("other")), Source: blank, By: local,
	})
	expect(t, "CreateFactory with a taken id", err, ports.ErrConflict)

	other := f.other(t)
	if theirs, _ := other.create(t, acme); theirs.OrgID != other.org {
		t.Errorf("another org's %s = %+v; want it created in %s", acme, theirs, other.org)
	}

	_, _, err = other.Definitions.CreateFactory(other.ctx(t), ports.NewFactory{
		ID: r.ID, Name: "copy", Manifest: other.blobs(t, tree("copy")), Source: blank, By: local,
	})
	expect(t, "CreateFactory with another org's id", err, ports.ErrConflict)

	if got := names(f.records(t)); !slices.Equal(got, []string{acme, "zeta"}) {
		t.Errorf("FactoryRecords = %v; want acme and zeta, by name", got)
	}
}

func testCreateFactoryUnknownBlob(f *fixture) {
	t := f.t
	id := newFactoryID()

	_, _, err := f.Definitions.CreateFactory(f.ctx(t), ports.NewFactory{
		ID: id, Name: acme, Manifest: domain.Manifest{factoryYAML: domain.HashBlob([]byte("never stored"))}, Source: blank, By: local,
	})
	expect(t, "CreateFactory naming an unknown blob", err, ports.ErrUnknownBlob)

	_, err = f.Definitions.FactoryRecord(f.ctx(t), id)
	expect(t, "FactoryRecord after a refused create", err, ports.ErrNotFound)

	if entries := f.auditEntries(t, id, domain.AuditFactoryCreated); len(entries) != 0 {
		t.Errorf("audit = %+v; want nothing for a refused create", entries)
	}
}

func testPutBlobs(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	const same = "same content"

	files := map[string]string{factoryYAML: "kind: Factory\n", "a.md": same, "b.md": same, "empty.md": ""}
	m := f.blobs(t, files)

	want := domain.Manifest{}
	for path, content := range files {
		want[path] = domain.HashBlob([]byte(content))
	}

	if !maps.Equal(m, want) || !m["a.md"].Valid() {
		t.Errorf("PutBlobs = %v; want the sha256 of each file's content: %v", m, want)
	}

	key := domain.BlobKey(f.org, m["a.md"])

	first, err := f.Objects.Get(ctx, key, "")
	if err != nil || string(first.Body) != same {
		t.Fatalf("object %s = %+v, %v; want the blob's content", key, first, err)
	}

	f.blobs(t, map[string]string{"c.md": same})

	if after, err := f.Objects.Get(ctx, key, ""); err != nil || after.Version != first.Version {
		t.Errorf("object %s after a second put = %+v, %v; want version %s untouched", key, after, err, first.Version)
	}

	for path, content := range files {
		if got, err := f.Definitions.Blob(ctx, m[path]); err != nil || string(got) != content {
			t.Errorf("Blob(%s) = %q, %v; want %q", path, got, err, content)
		}
	}

	unknown := domain.HashBlob([]byte("unknown"))

	sizes, err := f.Definitions.BlobSizes(ctx, []domain.BlobHash{m[factoryYAML], m["empty.md"], unknown})
	if want := map[domain.BlobHash]int64{m[factoryYAML]: int64(len(files[factoryYAML])), m["empty.md"]: 0}; err != nil || !maps.Equal(sizes, want) {
		t.Errorf("BlobSizes = %v, %v; want %v", sizes, err, want)
	}

	_, err = f.Definitions.Blob(ctx, unknown)
	expect(t, "Blob of an unknown hash", err, ports.ErrNotFound)
}

func testPutBlobsTooLarge(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	small := []byte("small")
	large := bytes.Repeat([]byte("x"), domain.MaxBlobSize+1)

	if _, err := f.Definitions.PutBlobs(ctx, map[string][]byte{"max.md": large[:domain.MaxBlobSize]}); err != nil {
		t.Errorf("PutBlobs of exactly %d bytes: %v", domain.MaxBlobSize, err)
	}

	_, err := f.Definitions.PutBlobs(ctx, map[string][]byte{"small.md": small, "large.md": large})
	expect(t, "PutBlobs over the size limit", err, domain.ErrInvalid)

	_, err = f.Definitions.Blob(ctx, domain.HashBlob(small))
	expect(t, "Blob of a file in a refused put", err, ports.ErrNotFound)
}

func testBlobCorrupt(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	m := f.blobs(t, map[string]string{factoryYAML: "kind: Factory\n"})

	if _, err := f.Objects.Put(ctx, domain.BlobKey(f.org, m[factoryYAML]), []byte("kind: Tampered\n")); err != nil {
		t.Fatal(err)
	}

	_, err := f.Definitions.Blob(ctx, m[factoryYAML])
	expect(t, "Blob whose content changed", err, ports.ErrBlobCorrupt)
}

// An object already at a blob's key that does not hold the blob's bytes (written by something
// other than a put, the same size or not) is replaced, never adopted.
func testPutBlobsRepairsAPlantedObject(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	content := []byte("kind: Factory\n")
	h := domain.HashBlob(content)

	if _, err := f.Objects.Put(ctx, domain.BlobKey(f.org, h), []byte("kind: Tampers\n")); err != nil { // same size
		t.Fatal(err)
	}

	if _, err := f.Definitions.PutBlobs(ctx, map[string][]byte{factoryYAML: content}); err != nil {
		t.Fatal(err)
	}

	if got, err := f.Definitions.Blob(ctx, h); err != nil || !bytes.Equal(got, content) {
		t.Errorf("Blob after the put = %q, %v; want the content put", got, err)
	}
}

func testSetPaused(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)
	v1 := f.publish(t, r.ID, "first")
	want := ports.FactoryRecord{
		ID: r.ID, OrgID: f.org, Name: acme, Title: acmeTitle, Current: ref(v1), LastVersionNumber: 1,
		PausedAt: paused, PausedBy: agent, CreatedBy: local,
	}

	got, err := f.Definitions.SetPaused(ctx, r.ID, true, agent)
	if err != nil {
		t.Fatalf("SetPaused(true): %v", err)
	}

	checkRecord(t, "paused", got, want)

	again, err := f.Definitions.SetPaused(ctx, r.ID, true, local)
	if err != nil || !again.PausedAt.Equal(got.PausedAt) {
		t.Errorf("SetPaused(true) again = %+v, %v; want paused since %v", again, err, got.PausedAt)
	}

	checkRecord(t, "paused again", again, want)

	resumed, err := f.Definitions.SetPaused(ctx, r.ID, false, local)
	if err != nil {
		t.Fatalf("SetPaused(false): %v", err)
	}

	want.PausedAt, want.PausedBy = running, domain.Actor{}
	checkRecord(t, "resumed", resumed, want)
	checkRecord(t, "FactoryRecord", f.record(t, r.ID), want)

	if n := len(f.auditEntries(t, r.ID, domain.AuditFactoryPaused)); n != 1 {
		t.Errorf("%d factory.paused entries; want 1", n)
	}

	checkEntry(t, f.audited(t, r.ID, domain.AuditFactoryPaused), agent, domain.AuditTargetFactory, string(r.ID), nil)
	checkEntry(t, f.audited(t, r.ID, domain.AuditFactoryResumed), local, domain.AuditTargetFactory, string(r.ID), nil)

	_, err = f.Definitions.SetPaused(ctx, newFactoryID(), true, local)
	expect(t, "SetPaused of an unknown factory", err, ports.ErrNotFound)
}

func testDeleteUnpublished(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	never, d := f.create(t, acme)
	published, _ := f.create(t, "zeta")
	f.publish(t, published.ID, "first")

	if err := f.Definitions.DeleteUnpublished(ctx, never.ID, local); err != nil {
		t.Fatalf("DeleteUnpublished: %v", err)
	}

	_, err := f.Definitions.FactoryRecord(ctx, never.ID)
	expect(t, "FactoryRecord of a deleted factory", err, ports.ErrNotFound)

	_, err = f.Definitions.Version(ctx, d.ID)
	expect(t, "Version of a deleted factory's draft", err, ports.ErrNotFound)

	checkEntry(t, f.audited(t, never.ID, domain.AuditFactoryDeleted), local, domain.AuditTargetFactory, string(never.ID),
		map[string]any{"name": acme})

	f.create(t, acme) // the name is free again

	expect(t, "DeleteUnpublished of a published factory", f.Definitions.DeleteUnpublished(ctx, published.ID, local), ports.ErrConflict)

	if r := f.record(t, published.ID); r.Current == nil {
		t.Errorf("published factory = %+v; want it kept", r)
	}

	expect(t, "DeleteUnpublished of an unknown factory", f.Definitions.DeleteUnpublished(ctx, newFactoryID(), local), ports.ErrNotFound)
}

func testImportLegacy(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	id := newFactoryID()
	f.LegacyFactory(t, f.org, id, "legacy-name")

	imp := ports.LegacyImport{
		ID: id, Name: acme, Title: acmeTitle, Manifest: f.blobs(t, tree(acme)), Publish: true, By: importer,
		Source:  domain.VersionSource{Kind: domain.SourceLegacy, LegacyVersions: map[string]string{factoryYAML: "v1", promptPath: "v7"}},
		Message: "Imported from orgs/" + string(f.org) + "/factories/" + string(id) + "/",
	}

	v, imported, err := f.Definitions.ImportLegacy(ctx, imp)
	if err != nil || !imported {
		t.Fatalf("ImportLegacy = %v, %v; want imported", imported, err)
	}

	checkVersion(t, "imported version", v, domain.DefinitionVersion{
		OrgID: f.org, FactoryID: id, State: domain.VersionPublished, Number: 1, Revision: 1, Manifest: imp.Manifest,
		Message: imp.Message, Source: imp.Source, CreatedBy: importer, UpdatedBy: importer, PublishedBy: importer,
	})

	// The adopted row keeps who created it: nobody recorded that before versions.
	checkRecord(t, "adopted factory", f.record(t, id), ports.FactoryRecord{
		ID: id, OrgID: f.org, Name: acme, Title: acmeTitle, Current: ref(v), LastVersionNumber: 1,
	})

	again, imported, err := f.Definitions.ImportLegacy(ctx, imp)
	if err != nil || imported || again.ID != "" {
		t.Errorf("ImportLegacy again = %+v, %v, %v; want a no-op", again, imported, err)
	}

	if vs, err := f.Definitions.Versions(ctx, id, 0, 10); err != nil || len(vs) != 1 {
		t.Errorf("Versions = %+v, %v; want only v1", vs, err)
	}

	checkEntry(t, f.audited(t, id, domain.AuditDefinitionPublished), importer, domain.AuditTargetVersion, string(v.ID),
		map[string]any{"number": 1})

	// A factory with no row yet gets one.
	imp.ID, imp.Name = newFactoryID(), "fresh"

	fresh, imported, err := f.Definitions.ImportLegacy(ctx, imp)
	if err != nil || !imported {
		t.Fatalf("ImportLegacy of a factory with no row = %v, %v; want imported", imported, err)
	}

	checkRecord(t, "new factory", f.record(t, imp.ID), ports.FactoryRecord{
		ID: imp.ID, OrgID: f.org, Name: "fresh", Title: acmeTitle, Current: ref(fresh), LastVersionNumber: 1, CreatedBy: importer,
	})
	checkEntry(t, f.audited(t, imp.ID, domain.AuditFactoryCreated), importer, domain.AuditTargetFactory, string(imp.ID),
		map[string]any{"source": "legacy"})
}

func testImportLegacyDraft(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	id := newFactoryID()
	f.LegacyFactory(t, f.org, id, acme)
	imp := ports.LegacyImport{
		ID: id, Name: acme, Manifest: f.blobs(t, map[string]string{factoryYAML: "not: [valid\n"}),
		Source: domain.VersionSource{Kind: domain.SourceLegacy}, By: importer,
	}

	v, imported, err := f.Definitions.ImportLegacy(ctx, imp)
	if err != nil || !imported {
		t.Fatalf("ImportLegacy = %v, %v; want imported", imported, err)
	}

	checkVersion(t, "imported draft", v, domain.DefinitionVersion{
		OrgID: f.org, FactoryID: id, State: domain.VersionDraft, Revision: 1, Manifest: imp.Manifest, Source: imp.Source,
		CreatedBy: importer, UpdatedBy: importer,
	})
	checkRecord(t, "factory", f.record(t, id), ports.FactoryRecord{ID: id, OrgID: f.org, Name: acme, Draft: ref(v)})

	imp.Publish = true

	if _, imported, err := f.Definitions.ImportLegacy(ctx, imp); err != nil || imported {
		t.Errorf("ImportLegacy of a factory with a draft = %v, %v; want a no-op", imported, err)
	}

	other := f.other(t)
	imp.Manifest = other.blobs(t, tree(acme))

	_, _, err = other.Definitions.ImportLegacy(other.ctx(t), imp)
	expect(t, "ImportLegacy of another org's factory", err, ports.ErrConflict)
}

// Only an organization from before versions has a legacy import to do, until it ends; a new one
// never has, and one organization's import is not another's.
func testLegacyImportPending(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	pending := func(what string, ctx context.Context, want bool) {
		t.Helper()

		if got, err := f.Definitions.LegacyImportPending(ctx); err != nil || got != want {
			t.Errorf("%s: LegacyImportPending = %v, %v; want %v", what, got, err, want)
		}
	}

	pending("a new organization", ctx, false)

	f.LegacyOrg(t, f.org)

	other := f.NewOrg(t)
	otherCtx := domain.WithOrg(t.Context(), other)

	pending("an organization from before versions", ctx, true)
	pending("another new organization", otherCtx, false)

	if err := f.Definitions.EndLegacyImport(otherCtx); err != nil {
		t.Fatal(err)
	}

	pending("after another organization's end", ctx, true)

	for range 2 {
		if err := f.Definitions.EndLegacyImport(ctx); err != nil {
			t.Fatal(err)
		}

		pending("after its end", ctx, false)
	}
}
