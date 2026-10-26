package definitionstest

import (
	"context"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// noticeWait bounds how long a committed change may take to be announced.
const noticeWait = 10 * time.Second

func testNotices(f *fixture) {
	t := f.t
	got := f.listen(t)
	r, d := f.create(t, acme)
	d = f.save(t, r.ID, d.Revision, map[string]string{notesPath: "second revision"})
	v := f.publish(t, r.ID, "first")
	mine := func(kind string) func(ports.DefinitionNotice) bool {
		return func(n ports.DefinitionNotice) bool { return n.FactoryID == r.ID && n.Kind == kind }
	}

	// Each kind in commit order: created, saved twice (the second by publish's helper), published.
	for _, want := range []ports.DefinitionNotice{
		{Kind: ports.NoticeFactory},
		{Kind: ports.NoticeDraft, VersionID: d.ID, Revision: 1, By: local.Ref()},
		{Kind: ports.NoticeDraft, VersionID: d.ID, Revision: 2, By: agent.Ref()},
		{Kind: ports.NoticeDraft, VersionID: d.ID, Revision: 3, By: agent.Ref()},
		{Kind: ports.NoticeDraft, VersionID: d.ID, Number: 1, Revision: 3, By: local.Ref()},
		{Kind: ports.NoticeCurrent, VersionID: v.ID, Number: 1},
	} {
		want.OrgID, want.FactoryID = f.org, r.ID

		if n := next(t, got, mine(want.Kind)); n != want {
			t.Errorf("notice = %+v; want %+v", n, want)
		}
	}

	// Pausing, archiving and restoring each move the factory: every goald refreshes its lines.
	if _, err := f.Definitions.SetPaused(f.ctx(t), r.ID, true, local); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	if _, err := f.Definitions.ArchiveFactory(f.ctx(t), r.ID, local); err != nil {
		t.Fatalf("ArchiveFactory: %v", err)
	}

	if _, err := f.Definitions.RestoreArchivedFactory(f.ctx(t), r.ID, local); err != nil {
		t.Fatalf("RestoreArchivedFactory: %v", err)
	}

	for _, change := range []string{"paused", "archived", "restored"} {
		want := ports.DefinitionNotice{OrgID: f.org, FactoryID: r.ID, Kind: ports.NoticeCurrent, VersionID: v.ID, Number: 1}
		if n := next(t, got, mine(ports.NoticeCurrent)); n != want {
			t.Errorf("notice once %s = %+v; want %+v", change, n, want)
		}
	}
}

// listen listens to Notices until the test ends and returns what they announce once listening
// (after the first notice, Resync).
func (f *fixture) listen(t *testing.T) <-chan ports.DefinitionNotice {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	got := make(chan ports.DefinitionNotice, 1024)
	done := make(chan error, 1)

	go func() {
		done <- f.Notices.Listen(ctx, func(n ports.DefinitionNotice) {
			select {
			case got <- n:
			default:
			}
		})
	}()

	t.Cleanup(func() {
		cancel()

		if err := <-done; err != nil {
			t.Errorf("Listen: %v", err)
		}
	})

	if n := next(t, got, func(n ports.DefinitionNotice) bool { return n.Resync }); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync", n)
	}

	return got
}

// next returns the next notice that matches, failing the test when none comes in time.
func next(t *testing.T, got <-chan ports.DefinitionNotice, match func(ports.DefinitionNotice) bool) ports.DefinitionNotice {
	t.Helper()

	timeout := time.NewTimer(noticeWait)
	defer timeout.Stop()

	for {
		select {
		case n := <-got:
			if match(n) {
				return n
			}
		case <-timeout.C:
			t.Fatal("no matching definition notice in time")

			return ports.DefinitionNotice{}
		}
	}
}

func testNoOrg(f *fixture) {
	t := f.t
	r, d := f.create(t, acme)
	m := d.Manifest
	ctx := t.Context() // no organization
	s := f.Definitions

	_, _, err := s.CreateFactory(ctx, ports.NewFactory{ID: newFactoryID(), Name: "x", Manifest: m, Source: blank, By: local})
	expect(t, "CreateFactory", err, domain.ErrNoOrg)
	_, err = s.FactoryRecord(ctx, r.ID)
	expect(t, "FactoryRecord", err, domain.ErrNoOrg)
	_, err = s.FactoryRecords(ctx)
	expect(t, "FactoryRecords", err, domain.ErrNoOrg)
	expect(t, "DeleteUnpublished", s.DeleteUnpublished(ctx, r.ID, local), domain.ErrNoOrg)
	_, err = s.SetPaused(ctx, r.ID, true, local)
	expect(t, "SetPaused", err, domain.ErrNoOrg)
	_, err = s.ArchiveFactory(ctx, r.ID, local)
	expect(t, "ArchiveFactory", err, domain.ErrNoOrg)
	expect(t, "DeleteArchivedFactory", s.DeleteArchivedFactory(ctx, r.ID, local), domain.ErrNoOrg)
	_, err = s.RestoreArchivedFactory(ctx, r.ID, local)
	expect(t, "RestoreArchivedFactory", err, domain.ErrNoOrg)
	_, err = s.PutBlobs(ctx, map[string][]byte{"x.md": []byte("x")})
	expect(t, "PutBlobs", err, domain.ErrNoOrg)
	_, err = s.Blob(ctx, m[factoryYAML])
	expect(t, "Blob", err, domain.ErrNoOrg)
	_, err = s.BlobSizes(ctx, m.Hashes())
	expect(t, "BlobSizes", err, domain.ErrNoOrg)
	_, err = s.Version(ctx, d.ID)
	expect(t, "Version", err, domain.ErrNoOrg)
	_, err = s.VersionByNumber(ctx, r.ID, 1)
	expect(t, "VersionByNumber", err, domain.ErrNoOrg)
	_, err = s.Draft(ctx, r.ID)
	expect(t, "Draft", err, domain.ErrNoOrg)
	_, err = s.Versions(ctx, r.ID, 0, 10)
	expect(t, "Versions", err, domain.ErrNoOrg)
	_, err = s.CurrentVersions(ctx)
	expect(t, "CurrentVersions", err, domain.ErrNoOrg)
	_, _, err = s.OpenDraft(ctx, ports.OpenDraft{Factory: r.ID, Replace: true, By: local})
	expect(t, "OpenDraft", err, domain.ErrNoOrg)
	_, err = s.SaveDraft(ctx, ports.DraftChange{Factory: r.ID, ExpectedRevision: 1, Remove: []string{factoryYAML}, By: local})
	expect(t, "SaveDraft", err, domain.ErrNoOrg)
	expect(t, "DiscardDraft", s.DiscardDraft(ctx, r.ID, 1, local), domain.ErrNoOrg)
	_, err = s.Publish(ctx, publishing(r, d, "x"))
	expect(t, "Publish", err, domain.ErrNoOrg)
	_, err = s.Restore(ctx, ports.Restore{Factory: r.ID, Version: d.ID, By: local})
	expect(t, "Restore", err, domain.ErrNoOrg)

	if got := f.draft(t, r.ID); got.Revision != 1 || got.State != domain.VersionDraft {
		t.Errorf("draft = %+v; want it untouched", got)
	}
}

// A factory with its versions and blobs in one organization does not exist for another, even
// when the other names its ids and hashes.
func testOtherOrg(f *fixture) {
	t := f.t
	r, _ := f.create(t, acme)
	v1 := f.publish(t, r.ID, "first")
	d := f.openDraft(t, r.ID)
	other := f.other(t)
	ctx := other.ctx(t)
	s := f.Definitions

	_, err := s.FactoryRecord(ctx, r.ID)
	expect(t, "FactoryRecord from another org", err, ports.ErrNotFound)
	_, err = s.Version(ctx, v1.ID)
	expect(t, "Version from another org", err, ports.ErrNotFound)
	_, err = s.VersionByNumber(ctx, r.ID, 1)
	expect(t, "VersionByNumber from another org", err, ports.ErrNotFound)
	_, err = s.Draft(ctx, r.ID)
	expect(t, "Draft from another org", err, ports.ErrNotFound)
	_, err = s.Blob(ctx, v1.Manifest[factoryYAML])
	expect(t, "Blob from another org", err, ports.ErrNotFound)
	_, _, err = s.OpenDraft(ctx, ports.OpenDraft{Factory: r.ID, Replace: true, By: local})
	expect(t, "OpenDraft from another org", err, ports.ErrNotFound)
	_, err = s.SaveDraft(ctx, ports.DraftChange{Factory: r.ID, ExpectedRevision: d.Revision, Remove: []string{factoryYAML}, By: local})
	expect(t, "SaveDraft from another org", err, ports.ErrNotFound)
	expect(t, "DiscardDraft from another org", s.DiscardDraft(ctx, r.ID, d.Revision, local), ports.ErrNotFound)
	_, err = s.Publish(ctx, publishing(f.record(t, r.ID), d, "theirs"))
	expect(t, "Publish from another org", err, ports.ErrNotFound)
	_, err = s.Restore(ctx, ports.Restore{Factory: r.ID, Version: v1.ID, ExpectedCurrent: v1.ID, Name: acme, By: local})
	expect(t, "Restore from another org", err, ports.ErrNotFound)
	_, err = s.SetPaused(ctx, r.ID, true, local)
	expect(t, "SetPaused from another org", err, ports.ErrNotFound)
	_, err = s.ArchiveFactory(ctx, r.ID, local)
	expect(t, "ArchiveFactory from another org", err, ports.ErrNotFound)
	expect(t, "DeleteArchivedFactory from another org", s.DeleteArchivedFactory(ctx, r.ID, local), ports.ErrNotFound)
	_, err = s.RestoreArchivedFactory(ctx, r.ID, local)
	expect(t, "RestoreArchivedFactory from another org", err, ports.ErrNotFound)
	expect(t, "DeleteUnpublished from another org", s.DeleteUnpublished(ctx, r.ID, local), ports.ErrNotFound)

	other.seesNothing(t, r.ID, v1.Manifest)

	if got := f.record(t, r.ID); got.Current == nil || got.Current.ID != v1.ID || got.Draft == nil || got.Draft.ID != d.ID || got.Paused() || got.Archived() {
		t.Errorf("record = %+v; want it untouched by the other org", got)
	}
}

// seesNothing checks every listing of the fixture's org finds nothing of factory and m.
func (f *fixture) seesNothing(t *testing.T, factory domain.FactoryID, m domain.Manifest) {
	t.Helper()

	ctx := f.ctx(t)

	if list, err := f.Definitions.FactoryRecords(ctx); err != nil || len(list) != 0 {
		t.Errorf("FactoryRecords = %+v, %v; want nothing", list, err)
	}

	if vs, err := f.Definitions.Versions(ctx, factory, 0, 10); err != nil || len(vs) != 0 {
		t.Errorf("Versions = %+v, %v; want nothing", vs, err)
	}

	if vs, err := f.Definitions.CurrentVersions(ctx); err != nil || len(vs) != 0 {
		t.Errorf("CurrentVersions = %+v, %v; want nothing", vs, err)
	}

	if sizes, err := f.Definitions.BlobSizes(ctx, m.Hashes()); err != nil || len(sizes) != 0 {
		t.Errorf("BlobSizes = %v, %v; want nothing", sizes, err)
	}
}
