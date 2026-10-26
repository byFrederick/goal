package definitionstest

import (
	"maps"
	"sync"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

func testOpenDraft(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, first := f.create(t, acme)

	d, created, err := f.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: r.ID, By: agent})
	if err != nil || created {
		t.Errorf("OpenDraft with a draft open = %v, %v; want the open draft, not created", created, err)
	}

	checkVersion(t, "OpenDraft with a draft open", d, first)

	v1 := f.publish(t, r.ID, "first")

	_, err = f.Definitions.Draft(ctx, r.ID)
	expect(t, "Draft after publish", err, ports.ErrNotFound)

	d, created, err = f.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: r.ID, By: agent})
	if err != nil || !created {
		t.Fatalf("OpenDraft = %v, %v; want a new draft", created, err)
	}

	checkVersion(t, "draft on v1", d, domain.DefinitionVersion{
		OrgID: f.org, FactoryID: r.ID, State: domain.VersionDraft, BaseID: v1.ID, Revision: 1, Manifest: v1.Manifest,
		Source: domain.VersionSource{Kind: domain.SourceEdit, Version: v1.ID}, CreatedBy: agent, UpdatedBy: agent,
	})
	checkRecord(t, "factory with a draft on v1", f.record(t, r.ID), ports.FactoryRecord{
		ID: r.ID, OrgID: f.org, Name: acme, Title: acmeTitle, Current: ref(v1), Draft: ref(d), LastVersionNumber: 1, CreatedBy: local,
	})

	_, _, err = f.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: newFactoryID(), By: local})
	expect(t, "OpenDraft of an unknown factory", err, ports.ErrNotFound)
}

func testOpenDraftBase(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)
	v1 := f.publish(t, r.ID, "first")
	f.publish(t, r.ID, "second")
	open := f.openDraft(t, r.ID)
	open = f.save(t, r.ID, open.Revision, map[string]string{"wip.md": "unsaved idea"})

	// An open draft is returned as it is, whatever base is asked for, unless replaced.
	d, created, err := f.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: r.ID, Base: v1.ID, By: local})
	if err != nil || created || d.ID != open.ID {
		t.Errorf("OpenDraft on v1 with a draft open = %+v, %v, %v; want the open draft", d, created, err)
	}

	d, created, err = f.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: r.ID, Base: v1.ID, Replace: true, By: local})
	if err != nil || !created || d.ID == open.ID || d.BaseID != v1.ID || !maps.Equal(d.Manifest, v1.Manifest) {
		t.Errorf("OpenDraft on v1 replacing = %+v, %v, %v; want a new draft with v1's manifest", d, created, err)
	}

	archived := open
	archived.State, archived.UpdatedBy = domain.VersionArchived, local
	checkVersion(t, "replaced draft", f.version(t, open.ID), archived)
	checkEntry(t, f.audited(t, r.ID, domain.AuditDraftDiscarded), local, domain.AuditTargetVersion, string(open.ID),
		map[string]any{"revision": open.Revision})

	other, _ := f.create(t, zeta)
	otherV1 := f.publish(t, other.ID, "first")

	for _, base := range []domain.DefinitionVersionID{d.ID, open.ID, otherV1.ID, "00000000-0000-7000-8000-ffffffffffff"} {
		_, _, err := f.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: r.ID, Base: base, Replace: true, By: local})
		expect(t, "OpenDraft on "+string(base)+", not a published version of the factory", err, ports.ErrNotFound)
	}

	checkVersion(t, "draft after the refused opens", f.draft(t, r.ID), d)
}

func testOpenDraftConcurrent(f *fixture) {
	t := f.t
	r, _ := f.create(t, acme)
	f.publish(t, r.ID, "first")

	const callers = 8

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ids     = map[domain.DefinitionVersionID]int{}
		created int
	)

	for range callers {
		wg.Go(func() {
			d, c, err := f.Definitions.OpenDraft(f.ctx(t), ports.OpenDraft{Factory: r.ID, By: local})
			if err != nil {
				t.Errorf("OpenDraft: %v", err)

				return
			}

			mu.Lock()
			defer mu.Unlock()

			ids[d.ID]++

			if c {
				created++
			}
		})
	}

	wg.Wait()

	if len(ids) != 1 || created != 1 {
		t.Errorf("concurrent OpenDraft returned drafts %v, %d created; want one draft, created once", ids, created)
	}
}

func testSaveDraft(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, d := f.create(t, acme)

	saved := f.save(t, r.ID, 1, map[string]string{promptPath: "You build better things.\n", "skills/go.md": "Use Go.\n"})
	want := d
	want.Revision, want.UpdatedBy = 2, agent
	want.Manifest = domain.Manifest{
		factoryYAML: d.Manifest[factoryYAML], promptPath: domain.HashBlob([]byte("You build better things.\n")),
		"skills/go.md": domain.HashBlob([]byte("Use Go.\n")),
	}
	checkVersion(t, "saved draft", saved, want)

	_, err := f.Definitions.SaveDraft(ctx, ports.DraftChange{Factory: r.ID, ExpectedRevision: 1, Remove: []string{factoryYAML}, By: local})
	expect(t, "SaveDraft at a stale revision", err, ports.ErrConflict)
	checkVersion(t, "draft after a stale save", f.draft(t, r.ID), want)

	removed, err := f.Definitions.SaveDraft(ctx, ports.DraftChange{
		Factory: r.ID, ExpectedRevision: 2, Remove: []string{"skills/go.md", "never/there.md"}, By: local,
	})
	if err != nil {
		t.Fatalf("SaveDraft removing: %v", err)
	}

	want.Revision, want.UpdatedBy = 3, local
	delete(want.Manifest, "skills/go.md")
	checkVersion(t, "draft after removing", removed, want)
	checkRecord(t, "factory", f.record(t, r.ID), ports.FactoryRecord{
		ID: r.ID, OrgID: f.org, Name: acme, Title: acmeTitle, Draft: ref(removed), CreatedBy: local,
	})

	_, err = f.Definitions.SaveDraft(ctx, ports.DraftChange{Factory: newFactoryID(), ExpectedRevision: 1, By: local})
	expect(t, "SaveDraft of an unknown factory", err, ports.ErrNotFound)

	v1 := f.publish(t, r.ID, "first")

	_, err = f.Definitions.SaveDraft(ctx, ports.DraftChange{Factory: r.ID, ExpectedRevision: v1.Revision, Remove: []string{promptPath}, By: local})
	expect(t, "SaveDraft with no open draft", err, ports.ErrNotFound)
	checkVersion(t, "published version after a save", f.version(t, v1.ID), v1)
}

func testSaveDraftReplace(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, d := f.create(t, acme)
	whole := f.blobs(t, map[string]string{factoryYAML: "kind: Factory\n", "contracts/spec.json": "{}"})

	saved, err := f.Definitions.SaveDraft(ctx, ports.DraftChange{
		Factory: r.ID, ExpectedRevision: 1, Set: whole, Remove: []string{factoryYAML}, Replace: true, By: local,
	})
	if err != nil {
		t.Fatalf("SaveDraft replacing: %v", err)
	}

	want := d
	want.Revision, want.Manifest = 2, whole
	checkVersion(t, "replaced tree", saved, want)

	_, err = f.Definitions.SaveDraft(ctx, ports.DraftChange{
		Factory: r.ID, ExpectedRevision: 2, Set: domain.Manifest{"x.md": domain.HashBlob([]byte("never stored"))}, By: local,
	})
	expect(t, "SaveDraft naming an unknown blob", err, ports.ErrUnknownBlob)

	// Another organization's blob is unknown here.
	other := f.other(t)
	theirs := other.blobs(t, map[string]string{"theirs.md": "only theirs"})

	_, err = f.Definitions.SaveDraft(ctx, ports.DraftChange{Factory: r.ID, ExpectedRevision: 2, Set: theirs, By: local})
	expect(t, "SaveDraft naming another org's blob", err, ports.ErrUnknownBlob)
	checkVersion(t, "draft after refused saves", f.draft(t, r.ID), want)
}

func testDiscardDraft(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, d := f.create(t, acme)
	d = f.save(t, r.ID, d.Revision, map[string]string{notesPath: "draft"})

	expect(t, "DiscardDraft at a stale revision", f.Definitions.DiscardDraft(ctx, r.ID, 1, local), ports.ErrConflict)

	if err := f.Definitions.DiscardDraft(ctx, r.ID, d.Revision, agent); err != nil {
		t.Fatalf("DiscardDraft: %v", err)
	}

	archived := d
	archived.State = domain.VersionArchived
	checkVersion(t, "discarded draft", f.version(t, d.ID), archived)
	checkRecord(t, "factory", f.record(t, r.ID), ports.FactoryRecord{ID: r.ID, OrgID: f.org, Name: acme, Title: acmeTitle, CreatedBy: local})
	checkEntry(t, f.audited(t, r.ID, domain.AuditDraftDiscarded), agent, domain.AuditTargetVersion, string(d.ID),
		map[string]any{"revision": d.Revision})

	expect(t, "DiscardDraft with no draft", f.Definitions.DiscardDraft(ctx, r.ID, d.Revision, local), ports.ErrNotFound)

	// A never-published factory without a draft opens an empty one.
	empty, created, err := f.Definitions.OpenDraft(ctx, ports.OpenDraft{Factory: r.ID, By: local})
	if err != nil || !created {
		t.Fatalf("OpenDraft after discarding the first draft = %v, %v; want a new draft", created, err)
	}

	checkVersion(t, "empty draft", empty, domain.DefinitionVersion{
		OrgID: f.org, FactoryID: r.ID, State: domain.VersionDraft, Revision: 1,
		Source: domain.VersionSource{Kind: domain.SourceEdit}, CreatedBy: local, UpdatedBy: local,
	})

	_, err = f.Definitions.Publish(ctx, ports.Publish{Factory: r.ID, DraftRevision: d.Revision, Name: acme, Start: true, By: local})
	expect(t, "Publish at the discarded draft's revision", err, ports.ErrConflict)
}
