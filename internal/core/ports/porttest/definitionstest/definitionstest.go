// Package definitionstest is the conformance suite for ports.DefinitionStore and
// ports.DefinitionNotices. The in-memory fake in porttest and the PostgreSQL adapter run it, so
// tests on the fake hold for the database's rules: one open draft per factory, compare-and-set
// revisions, gapless version numbers, immutable published versions, a current pointer that
// only names a published version, station moves in the publish transaction, verified blobs,
// an audit entry with every change, a notice after every commit, and row-level isolation.
package definitionstest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// Harness is one definition store under test and the ports of the same database it is checked
// against.
type Harness struct {
	Definitions ports.DefinitionStore
	// Notices announces Definitions' commits.
	Notices ports.DefinitionNotices
	// Objects is the bucket Definitions keeps blobs in (at domain.BlobKey); the in-memory one
	// counts writes, so the suite sees that a blob is written once.
	Objects *porttest.ObjectStore
	// Items is the item store of the same database: publish and restore move its items.
	Items ports.Store
	// Audit reads the audit log Definitions appends to.
	Audit ports.Audit
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
}

// Run runs the suite. Tests run in parallel and call newHarness once each; each test works in
// organizations of its own, so harnesses may share a database.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"CreateFactory_StartsWithADraft", testCreateFactory},
		{"CreateFactory_NameAndIDTaken", testCreateFactoryTaken},
		{"CreateFactory_UnknownBlobCreatesNothing", testCreateFactoryUnknownBlob},
		{"PutBlobs_StoresEachContentOnce", testPutBlobs},
		{"PutBlobs_TooLargeStoresNothing", testPutBlobsTooLarge},
		{"Blob_VerifiesItsHash", testBlobCorrupt},
		{"PutBlobs_RepairsAPlantedObject", testPutBlobsRepairsAPlantedObject},
		{"OpenDraft_OneDraftPerFactory", testOpenDraft},
		{"OpenDraft_FromABaseOrReplacing", testOpenDraftBase},
		{"OpenDraft_ConcurrentCallsOpenOne", testOpenDraftConcurrent},
		{"SaveDraft_CompareAndSetOnRevision", testSaveDraft},
		{"SaveDraft_ReplaceAndUnknownBlob", testSaveDraftReplace},
		{"DiscardDraft_ArchivesAtItsRevision", testDiscardDraft},
		{"Publish_NumbersVersionsAndMovesThePointer", testPublish},
		{"Publish_GuardsChangeNothing", testPublishGuards},
		{"Publish_ConcurrentPublishersOneWins", testPublishConcurrent},
		{"Publish_FirstWithoutStartIsPaused", testPublishPaused},
		{"Publish_RemovedStationsMoveOpenItems", testPublishRemovedStations},
		{"Versions_PublishedNewestFirstAndPaged", testVersions},
		{"Restore_MovesThePointerBack", testRestore},
		{"Restore_Guards", testRestoreGuards},
		{"SetPaused_PausesAndResumes", testSetPaused},
		{"DeleteUnpublished_RefusesAPublishedFactory", testDeleteUnpublished},
		{"Archive_RequiresPausedAndIdle", testArchiveRequiresPausedAndIdle},
		{"Archive_RestoreKeepsItemsAndPause", testArchiveRestoreKeepsItemsAndPause},
		{"Archive_Idempotent", testArchiveIdempotent},
		{"Archive_ListedWithItsStamp", testArchiveListedWithItsStamp},
		{"DeleteArchived_RemovesTheFactoryAndItsRows", testDeleteArchivedRemovesTheFactoryAndItsRows},
		{"DeleteArchived_RefusesALiveOrBusyFactory", testDeleteArchivedRefusesALiveOrBusyFactory},
		{"DeleteArchived_AuditsOnceAndNotifies", testDeleteArchivedAuditsOnceAndNotifies},
		{"Notices_AfterEveryCommit", testNotices},
		{"Tenancy_NoOrgIsAnError", testNoOrg},
		{"Tenancy_OtherOrgSeesNothing", testOtherOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			tc.test(&fixture{Harness: h, t: t, org: h.NewOrg(t)})
		})
	}
}

// Names, actors and times the suite uses.
const (
	factoryYAML = "factory.yaml"
	promptPath  = "agents/engineer.md"
	notesPath   = "notes.md"
	acme        = "acme"
	zeta        = "zeta"
	acmeTitle   = "Acme"
	stQA        = "qa"
	stReview    = "review"
	stBuild     = "build"
	typeTask    = "task"
)

var (
	t0    = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	local = domain.LocalUser
	agent = domain.Actor{Kind: domain.ActorAgent, ID: "investigator", Name: "investigator"}
	blank = domain.VersionSource{Kind: domain.SourceBlank}
)

// fixture is one test's harness and organization. It holds the test's T so the test bodies
// are not mistaken for helpers; its methods fail the test on unexpected errors.
type fixture struct {
	Harness

	t   *testing.T
	org domain.OrgID
}

// ctx is the test's context bound to the fixture's organization.
func (f *fixture) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), f.org)
}

// other is a fixture on the same harness in a new organization.
func (f *fixture) other(t *testing.T) *fixture {
	t.Helper()

	return &fixture{Harness: f.Harness, t: t, org: f.NewOrg(t)}
}

func newFactoryID() domain.FactoryID { return domain.FactoryID(uuid.NewString()) }

// blobs stores files and returns their manifest.
func (f *fixture) blobs(t *testing.T, files map[string]string) domain.Manifest {
	t.Helper()

	raw := map[string][]byte{}
	for path, content := range files {
		raw[path] = []byte(content)
	}

	m, err := f.Definitions.PutBlobs(f.ctx(t), raw)
	if err != nil {
		t.Fatalf("PutBlobs: %v", err)
	}

	return m
}

// tree is a small definition named name.
func tree(name string) map[string]string {
	return map[string]string{factoryYAML: "metadata:\n  name: " + name + "\n", promptPath: "You build things.\n"}
}

// create creates a factory named name with a blank definition and returns it with its draft.
func (f *fixture) create(t *testing.T, name string) (ports.FactoryRecord, domain.DefinitionVersion) {
	t.Helper()

	r, v, err := f.Definitions.CreateFactory(f.ctx(t), ports.NewFactory{
		ID: newFactoryID(), Name: name, Title: acmeTitle, Manifest: f.blobs(t, tree(name)), Source: blank, By: local,
	})
	if err != nil {
		t.Fatalf("CreateFactory %s: %v", name, err)
	}

	return r, v
}

func (f *fixture) record(t *testing.T, id domain.FactoryID) ports.FactoryRecord {
	t.Helper()

	r, err := f.Definitions.FactoryRecord(f.ctx(t), id)
	if err != nil {
		t.Fatalf("FactoryRecord: %v", err)
	}

	return r
}

func (f *fixture) version(t *testing.T, id domain.DefinitionVersionID) domain.DefinitionVersion {
	t.Helper()

	v, err := f.Definitions.Version(f.ctx(t), id)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}

	return v
}

func (f *fixture) draft(t *testing.T, id domain.FactoryID) domain.DefinitionVersion {
	t.Helper()

	v, err := f.Definitions.Draft(f.ctx(t), id)
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}

	return v
}

// openDraft returns the factory's open draft, opening one on the current version if needed.
func (f *fixture) openDraft(t *testing.T, id domain.FactoryID) domain.DefinitionVersion {
	t.Helper()

	v, _, err := f.Definitions.OpenDraft(f.ctx(t), ports.OpenDraft{Factory: id, By: local})
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}

	return v
}

// save sets files in the open draft at revision.
func (f *fixture) save(t *testing.T, id domain.FactoryID, revision int64, files map[string]string) domain.DefinitionVersion {
	t.Helper()

	v, err := f.Definitions.SaveDraft(f.ctx(t), ports.DraftChange{
		Factory: id, ExpectedRevision: revision, Set: f.blobs(t, files), By: agent,
	})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	return v
}

// publish publishes the factory's next version: the open draft (one is opened on the current
// version if none is), with a file changed so versions differ.
func (f *fixture) publish(t *testing.T, id domain.FactoryID, note string) domain.DefinitionVersion {
	t.Helper()

	r := f.record(t, id)
	d := f.openDraft(t, id)
	d = f.save(t, id, d.Revision, map[string]string{notesPath: note})

	v, err := f.Definitions.Publish(f.ctx(t), publishing(r, d, note))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	return v
}

// publishing is the request that publishes d on top of r's current version.
func publishing(r ports.FactoryRecord, d domain.DefinitionVersion, note string) ports.Publish {
	return ports.Publish{
		Factory: r.ID, DraftRevision: d.Revision, ExpectedCurrent: current(r), Name: r.Name, Title: r.Title,
		Message: note, Start: true, By: local,
	}
}

// current is the id of r's current version, or "".
func current(r ports.FactoryRecord) domain.DefinitionVersionID {
	if r.Current == nil {
		return ""
	}

	return r.Current.ID
}

// audited returns the newest audit entry of the factory with action, failing when there is none.
func (f *fixture) audited(t *testing.T, factory domain.FactoryID, action string) domain.AuditEntry {
	t.Helper()

	entries := f.auditEntries(t, factory, action)
	if len(entries) == 0 {
		t.Fatalf("no %s audit entry for factory %s", action, factory)
	}

	return entries[0]
}

func (f *fixture) auditEntries(t *testing.T, factory domain.FactoryID, action string) []domain.AuditEntry {
	t.Helper()

	entries, err := f.Audit.AuditLog(f.ctx(t), ports.AuditQuery{FactoryID: factory, Action: action, Limit: 100})
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}

	return entries
}

// checkEntry fails unless e has the actor, target and data (compared as text, since data goes
// through JSON) want.
func checkEntry(t *testing.T, e domain.AuditEntry, by domain.Actor, kind, target string, data map[string]any) {
	t.Helper()

	if e.Actor != by || e.TargetKind != kind || e.TargetID != target {
		t.Errorf("%s entry = %+v; want actor %+v, target %s %s", e.Action, e, by, kind, target)
	}

	for k, want := range data {
		if got := fmt.Sprint(e.Data[k]); got != fmt.Sprint(want) {
			t.Errorf("%s entry data[%s] = %s; want %v", e.Action, k, got, want)
		}
	}
}

// expect fails the test unless err wraps want.
func expect(t *testing.T, what string, err, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Errorf("%s = %v; want %v", what, err, want)
	}
}
