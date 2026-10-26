package definitionstest

import (
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// pausedFactory creates and publishes a factory, then pauses it, as archiving requires.
func (f *fixture) pausedFactory(t *testing.T, name string) ports.FactoryRecord {
	t.Helper()

	r, _ := f.create(t, name)
	f.publish(t, r.ID, "first")

	if _, err := f.Definitions.SetPaused(f.ctx(t), r.ID, true, local); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	return f.record(t, r.ID)
}

func testArchiveRequiresPausedAndIdle(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)
	f.publish(t, r.ID, "first")

	_, err := f.Definitions.ArchiveFactory(ctx, r.ID, agent)
	expect(t, "ArchiveFactory of a running factory", err, ports.ErrFactoryRunning)

	if _, err := f.Definitions.SetPaused(ctx, r.ID, true, local); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	f.item(t, r.ID, stQA, domain.StateQueued)
	it, lease := f.claim(t, r.ID, stQA)

	_, err = f.Definitions.ArchiveFactory(ctx, r.ID, agent)
	expect(t, "ArchiveFactory while an item holds a lease", err, ports.ErrFactoryBusy)

	if rec := f.record(t, r.ID); rec.Archived() {
		t.Errorf("record after refused archives = %+v; want it not archived", rec)
	}

	if n := len(f.auditEntries(t, r.ID, domain.AuditFactoryArchived)); n != 0 {
		t.Errorf("%d factory.archived entries after refused archives; want 0", n)
	}

	f.finish(t, it, lease)

	got, err := f.Definitions.ArchiveFactory(ctx, r.ID, agent)
	if err != nil {
		t.Fatalf("ArchiveFactory once the run ended: %v", err)
	}

	if !got.Archived() || !got.Paused() {
		t.Errorf("archived record = %+v; want it archived and paused", got)
	}

	_, err = f.Definitions.ArchiveFactory(ctx, newFactoryID(), local)
	expect(t, "ArchiveFactory of an unknown factory", err, ports.ErrNotFound)
	_, err = f.Definitions.RestoreArchivedFactory(ctx, newFactoryID(), local)
	expect(t, "RestoreArchivedFactory of an unknown factory", err, ports.ErrNotFound)
}

func testArchiveRestoreKeepsItemsAndPause(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r := f.pausedFactory(t, acme)
	queued := f.item(t, r.ID, stQA, domain.StateQueued)
	blocked := f.item(t, r.ID, stBuild, domain.StateBlocked)

	archived, err := f.Definitions.ArchiveFactory(ctx, r.ID, agent)
	if err != nil {
		t.Fatalf("ArchiveFactory: %v", err)
	}

	want := r
	want.ArchivedAt, want.ArchivedBy = archivedSince, agent
	checkRecord(t, "archived", archived, want)

	restored, err := f.Definitions.RestoreArchivedFactory(ctx, r.ID, local)
	if err != nil {
		t.Fatalf("RestoreArchivedFactory: %v", err)
	}

	checkRecord(t, "restored", restored, r)
	checkRecord(t, "FactoryRecord after restore", f.record(t, r.ID), r)

	if !restored.PausedAt.Equal(r.PausedAt) {
		t.Errorf("restored paused at %v; want still paused since %v", restored.PausedAt, r.PausedAt)
	}

	for _, it := range []domain.Item{queued, blocked} {
		if got := f.itemNow(t, it.ID); got.Station != it.Station || got.State != it.State {
			t.Errorf("item #%d = %s at %s; want it still %s at %s", it.Number, got.State, got.Station, it.State, it.Station)
		}
	}

	name := map[string]any{"name": acme}
	checkEntry(t, f.audited(t, r.ID, domain.AuditFactoryArchived), agent, domain.AuditTargetFactory, string(r.ID), name)
	checkEntry(t, f.audited(t, r.ID, domain.AuditFactoryRestored), local, domain.AuditTargetFactory, string(r.ID), name)
}

func testArchiveIdempotent(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r := f.pausedFactory(t, acme)

	same, err := f.Definitions.RestoreArchivedFactory(ctx, r.ID, local)
	if err != nil {
		t.Fatalf("RestoreArchivedFactory of a factory that is not archived: %v", err)
	}

	checkRecord(t, "restored while not archived", same, r)

	first, err := f.Definitions.ArchiveFactory(ctx, r.ID, agent)
	if err != nil {
		t.Fatalf("ArchiveFactory: %v", err)
	}

	again, err := f.Definitions.ArchiveFactory(ctx, r.ID, local)
	if err != nil {
		t.Fatalf("ArchiveFactory again: %v", err)
	}

	want := r
	want.ArchivedAt, want.ArchivedBy = archivedSince, agent
	checkRecord(t, "archived again", again, want)

	if !again.ArchivedAt.Equal(first.ArchivedAt) {
		t.Errorf("archived again at %v; want still archived since %v", again.ArchivedAt, first.ArchivedAt)
	}

	if n := len(f.auditEntries(t, r.ID, domain.AuditFactoryArchived)); n != 1 {
		t.Errorf("%d factory.archived entries; want 1", n)
	}

	if n := len(f.auditEntries(t, r.ID, domain.AuditFactoryRestored)); n != 0 {
		t.Errorf("%d factory.restored entries; want 0", n)
	}
}

func testArchiveListedWithItsStamp(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r := f.pausedFactory(t, acme)
	kept := f.pausedFactory(t, zeta)

	if _, err := f.Definitions.ArchiveFactory(ctx, r.ID, agent); err != nil {
		t.Fatalf("ArchiveFactory: %v", err)
	}

	want := r
	want.ArchivedAt, want.ArchivedBy = archivedSince, agent

	list := f.records(t)
	if got := names(list); !slices.Equal(got, []string{acme, zeta}) {
		t.Fatalf("FactoryRecords = %v; want acme and zeta, archived ones included", got)
	}

	checkRecord(t, "listed archived factory", list[0], want)
	checkRecord(t, "listed factory", list[1], kept)
	checkRecord(t, "FactoryRecord", f.record(t, r.ID), want)
}

// archivedFactory creates, publishes, pauses and archives a factory with one item that ran at
// stQA and moved on, and returns it with that item.
func (f *fixture) archivedFactory(t *testing.T, name string) (ports.FactoryRecord, domain.Item) {
	t.Helper()

	r := f.pausedFactory(t, name)
	f.item(t, r.ID, stQA, domain.StateQueued)
	it, lease := f.claim(t, r.ID, stQA)
	f.finish(t, it, lease)

	archived, err := f.Definitions.ArchiveFactory(f.ctx(t), r.ID, local)
	if err != nil {
		t.Fatalf("ArchiveFactory: %v", err)
	}

	return archived, it
}

func testDeleteArchivedRemovesTheFactoryAndItsRows(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, it := f.archivedFactory(t, acme)
	kept := f.pausedFactory(t, zeta)

	if err := f.Definitions.DeleteArchivedFactory(ctx, r.ID, local); err != nil {
		t.Fatalf("DeleteArchivedFactory: %v", err)
	}

	_, err := f.Definitions.FactoryRecord(ctx, r.ID)
	expect(t, "FactoryRecord of a deleted factory", err, ports.ErrNotFound)
	_, err = f.Definitions.Version(ctx, current(r))
	expect(t, "Version of a deleted factory", err, ports.ErrNotFound)
	_, err = f.Items.Item(ctx, it.ID)
	expect(t, "Item of a deleted factory", err, ports.ErrNotFound)

	if got := names(f.records(t)); !slices.Equal(got, []string{zeta}) {
		t.Errorf("FactoryRecords = %v; want only zeta", got)
	}

	checkRecord(t, "FactoryRecord of the other factory", f.record(t, kept.ID), kept)

	expect(t, "DeleteArchivedFactory of a deleted factory", f.Definitions.DeleteArchivedFactory(ctx, r.ID, local), ports.ErrNotFound)
	expect(t, "DeleteArchivedFactory of an unknown factory", f.Definitions.DeleteArchivedFactory(ctx, newFactoryID(), local), ports.ErrNotFound)
}

func testDeleteArchivedRefusesALiveOrBusyFactory(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r := f.pausedFactory(t, acme)
	queued := f.item(t, r.ID, stQA, domain.StateQueued)

	expect(t, "DeleteArchivedFactory of a factory that is not archived",
		f.Definitions.DeleteArchivedFactory(ctx, r.ID, local), ports.ErrFactoryNotArchived)

	if _, err := f.Definitions.ArchiveFactory(ctx, r.ID, local); err != nil {
		t.Fatalf("ArchiveFactory: %v", err)
	}

	f.claim(t, r.ID, stQA)

	expect(t, "DeleteArchivedFactory while an item holds a lease",
		f.Definitions.DeleteArchivedFactory(ctx, r.ID, local), ports.ErrFactoryBusy)

	if rec := f.record(t, r.ID); !rec.Archived() {
		t.Errorf("record after refused deletes = %+v; want it still archived", rec)
	}

	f.itemNow(t, queued.ID)

	if n := len(f.auditEntries(t, r.ID, domain.AuditFactoryDeleted)); n != 0 {
		t.Errorf("%d factory.deleted entries after refused deletes; want 0", n)
	}
}

func testDeleteArchivedAuditsOnceAndNotifies(f *fixture) {
	t := f.t
	r, _ := f.archivedFactory(t, acme)
	got := f.listen(t)

	if err := f.Definitions.DeleteArchivedFactory(f.ctx(t), r.ID, agent); err != nil {
		t.Fatalf("DeleteArchivedFactory: %v", err)
	}

	entries := f.auditEntries(t, r.ID, domain.AuditFactoryDeleted)
	if len(entries) != 1 {
		t.Fatalf("%d factory.deleted entries; want 1", len(entries))
	}

	checkEntry(t, entries[0], agent, domain.AuditTargetFactory, string(r.ID), map[string]any{"name": acme})

	want := ports.DefinitionNotice{OrgID: f.org, FactoryID: r.ID, Kind: ports.NoticeFactory}
	if n := next(t, got, func(n ports.DefinitionNotice) bool { return n.FactoryID == r.ID && n.Kind == ports.NoticeFactory }); n != want {
		t.Errorf("notice = %+v; want %+v", n, want)
	}
}
