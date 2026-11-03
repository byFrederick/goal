// Package knowledgetest is the conformance suite for ports.Knowledge (spec 4 §5.6). The in-memory
// fake in porttest and the PostgreSQL adapter run it, so tests on the fake hold for the
// database's rules: sources and their connections, leases that end so another goald takes the
// sync over, documents replaced with their chunks, unseen documents deleted, uploads, the one
// hybrid ranking and its sources filter, organizations kept apart, knowledge use and verdicts
// (uses.go), and ADR 0038's snapshots, current files and daily read counts (files.go).
package knowledgetest

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one Knowledge under test.
type Harness struct {
	Knowledge ports.Knowledge
	// Connections are the connections sources read; the suite syncs its platform kinds.
	Connections ports.Connections
	// Secrets are the secrets synced sources read with.
	Secrets ports.OrgSecrets
	Audit   ports.Audit
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"Sources_CreateUpdateDeleteAudited", testSources},
		{"CreateSource_RulesRefused", testSourceRules},
		{"ConnectionReadBySource_DeleteIsAConflict", testConnectionInUse},
		{"SecretReadBySource_KeptByIDAndDeleteIsAConflict", testSecretSources},
		{"Claim_LeaseEndsAndAnotherTakesOver", testClaimLease},
		{"Claim_TakenOverLeaseChangesNothing", testClaimFenced},
		{"Claim_EveryOrganizationOldestDueFirst", testClaimAcrossOrgs},
		{"Claim_ScheduleAndSyncNow", testClaimSchedule},
		{"FinishSync_AnErrorThatIsNoUTF8", testFinishInvalidUTF8},
		{"Finish_CountsFailuresInARow", testFinishFailures},
		{"Uploads_ReplacedByNameDueAndOrphans", testUploads},
		{"OtherOrg_SeesNothing", testIsolation},
		{"Snapshots_NewReplacesCurrentFilesAndKeepsTheOld", testSnapshots},
		{"Snapshots_RulesAndLostLeaseChangeNothing", testSnapshotRules},
		{"Files_PageInPathOrderByPathOrTitlePrefix", testFilesPage},
		{"Reads_AddToOneRowPerSourcePathFactoryAndDay", testReads},
		{"Files_OtherOrgSeesNothing", testFilesIsolation},
		{"Sources_RepositoryAndConfluenceWithReadWhen", testNewKinds},
		{"ConvertSource_SyncedBecomesItsKind", testConvertSource},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			f := &fixture{Harness: h, t: t, org: h.NewOrg(t)}
			f.setUp(f.org)
			tc.test(f)
		})
	}
}

// Names the suite uses.
const (
	syncKind = "kb-docs"
	liveKind = "kb-chat"
	docsConn = "docs"
	chatConn = "chat"
	// docsSecret is the secret synced sources read with, and readerGitDocs their reader.
	docsSecret    = "docs-token"
	readerGitDocs = "gitdocs"
)

var admin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}

type fixture struct {
	Harness

	t   *testing.T
	org domain.OrgID
}

func (f *fixture) in(org domain.OrgID) context.Context { return domain.WithOrg(f.t.Context(), org) }

func (f *fixture) ctx() context.Context { return f.in(f.org) }

// setUp gives org a connection that syncs (docs) and one that does not (chat).
func (f *fixture) setUp(org domain.OrgID) {
	f.t.Helper()

	err := f.Connections.SyncPlatformKinds(f.t.Context(), []domain.ConnectionKind{
		{Name: syncKind, Title: "Acme docs", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{{Name: "repo", Title: "Repository", Required: true}}, Sync: "gitdocs",
		}},
		{Name: liveKind, Title: "Acme chat", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{{Name: "url", Title: "URL", Required: true}}, MCP: &domain.KindMCP{URL: "{{url}}"},
		}},
	})
	if err != nil {
		f.t.Fatalf("SyncPlatformKinds: %v", err)
	}

	for _, c := range []domain.NewConnection{
		{Name: docsConn, Kind: syncKind, Fields: map[string]string{"repo": "https://git.example.com/acme/docs"}},
		{Name: chatConn, Kind: liveKind, Fields: map[string]string{"url": "https://chat.example.com/mcp"}},
	} {
		if _, err := f.Connections.Create(f.in(org), c, admin); err != nil {
			f.t.Fatalf("Create connection %s: %v", c.Name, err)
		}
	}
}

func (f *fixture) source(org domain.OrgID, n domain.NewKnowledgeSource) domain.KnowledgeSource {
	f.t.Helper()

	if n.Title == "" {
		n.Title = "The " + n.Name
	}

	s, err := f.Knowledge.CreateSource(f.in(org), n, admin)
	if err != nil {
		f.t.Fatalf("CreateSource %s: %v", n.Name, err)
	}

	return s
}

func (f *fixture) upload(name string) domain.KnowledgeSource {
	f.t.Helper()

	return f.source(f.org, domain.NewKnowledgeSource{Name: name, Kind: domain.SourceUpload})
}

func (f *fixture) synced(org domain.OrgID, name string) domain.KnowledgeSource {
	f.t.Helper()

	return f.source(org, domain.NewKnowledgeSource{
		Name: name, Kind: domain.SourceSynced, Connection: docsConn,
		Config: map[string]string{"paths": "docs/**/*.md"},
	})
}

func (f *fixture) get(org domain.OrgID, name string) domain.KnowledgeSource {
	f.t.Helper()

	s, err := f.Knowledge.Source(f.in(org), name)
	if err != nil {
		f.t.Fatalf("Source %s: %v", name, err)
	}

	return s
}

// audit is org's entries of action, newest first.
func (f *fixture) audit(org domain.OrgID, action string) []domain.AuditEntry {
	f.t.Helper()

	got, err := f.Audit.AuditLog(f.in(org), ports.AuditQuery{Limit: 100, Action: action})
	if err != nil {
		f.t.Fatal(err)
	}

	return got
}

func (f *fixture) refused(what string, err, want error) {
	f.t.Helper()

	if !errors.Is(err, want) {
		f.t.Errorf("%s = %v, want %v", what, err, want)
	}
}

// Names of the sources, texts and types the tests use.
const (
	handbook   = "handbook"
	runbooks   = "runbooks"
	chatSearch = "chat-search"
	plainText  = "text/plain"
	newName    = "syn"
)

// want fails the test with what and got unless every condition holds.
func (f *fixture) want(what string, got any, conds ...bool) {
	f.t.Helper()

	for i, ok := range conds {
		if !ok {
			f.t.Errorf("%s = %+v (check %d of %d fails)", what, got, i+1, len(conds))

			return
		}
	}
}

// must stops the test on err.
func (f *fixture) must(err error) {
	f.t.Helper()

	if err != nil {
		f.t.Fatal(err)
	}
}

// equalTime reports whether t is set and at want.
func equalTime(t *time.Time, want time.Time) bool { return t != nil && t.Equal(want) }

// testSources: sources are created due (not a live one), listed by name, changed and deleted,
// each audited.
func testSources(f *fixture) {
	ctx := f.ctx()
	before := time.Now().Add(-time.Minute)

	up := f.upload(handbook)
	f.want("upload source", up, up.Kind == domain.SourceUpload, up.Status == domain.SyncIdle, up.SyncEvery == domain.DefaultSyncEvery,
		up.NextSyncAt != nil && !up.NextSyncAt.Before(before), up.Connection == "", up.CreatedBy.Ref() == admin.Ref())

	synced := f.source(f.org, domain.NewKnowledgeSource{
		Name: runbooks, Title: "Runbooks", Kind: domain.SourceSynced, Connection: docsConn,
		Config: map[string]string{"branch": "main"}, SyncEvery: 30 * time.Minute,
	})
	f.want("synced source", synced, synced.Connection == docsConn, synced.SyncEvery == 30*time.Minute, synced.Config["branch"] == "main")

	live := f.source(f.org, domain.NewKnowledgeSource{Name: chatSearch, Kind: domain.SourceLive, Connection: chatConn})
	f.want("live source (never syncs)", live, live.NextSyncAt == nil, live.Connection == chatConn)

	all, err := f.Knowledge.Sources(ctx)
	f.must(err)

	names := make([]string, 0, len(all))
	for _, s := range all {
		names = append(names, s.Name)
	}

	f.want("Sources", names, slices.Equal(names, []string{chatSearch, handbook, runbooks}))

	title, every := "Run books", 2*time.Hour

	got, err := f.Knowledge.UpdateSource(ctx, runbooks, domain.KnowledgeSourcePatch{
		Title: &title, Config: map[string]string{"branch": "trunk"}, SyncEvery: &every,
	}, admin)
	f.must(err)
	f.want("UpdateSource", got, got.Title == title, got.Config["branch"] == "trunk", got.SyncEvery == every)

	g := f.get(f.org, runbooks)
	f.want("Source after the update", g, g.Title == title, g.SyncEvery == every)

	err = f.Knowledge.DeleteSource(ctx, chatSearch, admin, nil)
	f.must(err)

	_, err = f.Knowledge.Source(ctx, chatSearch)
	f.refused("Source after DeleteSource", err, ports.ErrNotFound)

	for action, n := range map[string]int{
		domain.AuditKnowledgeSourceCreated: 3, domain.AuditKnowledgeSourceUpdated: 1, domain.AuditKnowledgeSourceDeleted: 1,
	} {
		got := f.audit(f.org, action)
		f.want("audit "+action, got, len(got) == n, len(got) > 0 && got[0].Actor.Ref() == admin.Ref(),
			len(got) > 0 && got[0].TargetKind == domain.AuditTargetKnowledgeSource)
	}
}

// testSourceRules: what a source may not be.
func testSourceRules(f *fixture) {
	ctx := f.ctx()
	f.upload(handbook)

	for name, tc := range map[string]struct {
		n    domain.NewKnowledgeSource
		want error
	}{
		"name taken":         {domain.NewKnowledgeSource{Name: handbook, Title: "Again", Kind: domain.SourceUpload}, ports.ErrNameTaken},
		"bad name":           {domain.NewKnowledgeSource{Name: "Hand Book", Title: "x", Kind: domain.SourceUpload}, domain.ErrInvalid},
		"no title":           {domain.NewKnowledgeSource{Name: "notitle", Kind: domain.SourceUpload}, domain.ErrInvalid},
		"bad kind":           {domain.NewKnowledgeSource{Name: "odd", Title: "x", Kind: "pushed"}, domain.ErrInvalid},
		"upload connection":  {domain.NewKnowledgeSource{Name: "upl", Title: "x", Kind: domain.SourceUpload, Connection: docsConn}, domain.ErrInvalid},
		"synced without one": {domain.NewKnowledgeSource{Name: newName, Title: "x", Kind: domain.SourceSynced}, domain.ErrInvalid},
		"cannot sync":        {domain.NewKnowledgeSource{Name: newName, Title: "x", Kind: domain.SourceSynced, Connection: chatConn}, domain.ErrConnectionCannotSync},
		"unknown connection": {domain.NewKnowledgeSource{Name: newName, Title: "x", Kind: domain.SourceSynced, Connection: "nope"}, ports.ErrNotFound},
		"too often": {
			domain.NewKnowledgeSource{Name: newName, Title: "x", Kind: domain.SourceSynced, Connection: docsConn, SyncEvery: time.Minute},
			domain.ErrInvalid,
		},
	} {
		_, err := f.Knowledge.CreateSource(ctx, tc.n, admin)
		f.refused("CreateSource "+name, err, tc.want)
	}

	short := 5 * time.Minute
	_, err := f.Knowledge.UpdateSource(ctx, handbook, domain.KnowledgeSourcePatch{SyncEvery: &short}, admin)
	f.refused("UpdateSource too often", err, domain.ErrInvalid)

	title := "x"
	_, err = f.Knowledge.UpdateSource(ctx, "nope", domain.KnowledgeSourcePatch{Title: &title}, admin)
	f.refused("UpdateSource unknown", err, ports.ErrNotFound)

	err = f.Knowledge.DeleteSource(ctx, "nope", admin, nil)
	f.refused("DeleteSource unknown", err, ports.ErrNotFound)

	f.source(f.org, domain.NewKnowledgeSource{Name: chatSearch, Kind: domain.SourceLive, Connection: chatConn})
	f.refused("SyncNow live", f.Knowledge.SyncNow(ctx, chatSearch), domain.ErrInvalid)
	f.refused("SyncNow unknown", f.Knowledge.SyncNow(ctx, "nope"), ports.ErrNotFound)

	created := f.audit(f.org, domain.AuditKnowledgeSourceCreated)
	f.want("sources created (refusals write nothing)", len(created), len(created) == 2)
}

// testConnectionInUse: a connection a source reads cannot be deleted; once the source is gone it
// can.
func testConnectionInUse(f *fixture) {
	ctx := f.ctx()
	f.synced(f.org, runbooks)

	f.refused("Delete a connection a source reads", f.Connections.Delete(ctx, docsConn, admin), ports.ErrConflict)

	err := f.Knowledge.DeleteSource(ctx, runbooks, admin, nil)
	f.must(err)
	f.must(f.Connections.Delete(ctx, docsConn, admin))
}

func testSecretSources(f *fixture) {
	ctx := f.ctx()

	if _, err := f.Secrets.Create(ctx, domain.NewSecret{Name: docsSecret, Value: "test-token", Scope: domain.SecretScope{All: true}}, admin); err != nil {
		f.t.Fatalf("Create secret: %v", err)
	}

	onSecret := domain.NewKnowledgeSource{
		Name: runbooks, Title: "Runbooks", Kind: domain.SourceSynced, Secret: docsSecret,
		Config: map[string]string{domain.SourceReaderKey: readerGitDocs, "repository": "https://github.com/acme/docs"},
	}

	for _, bad := range []struct {
		what string
		n    domain.NewKnowledgeSource
		want error
	}{
		{"a secret source without a reader", domain.NewKnowledgeSource{Name: "no-reader", Title: "t", Kind: domain.SourceSynced, Secret: docsSecret}, domain.ErrInvalid},
		{"an unknown secret", domain.NewKnowledgeSource{
			Name: "no-secret", Title: "t", Kind: domain.SourceSynced, Secret: "gone",
			Config: onSecret.Config,
		}, ports.ErrNotFound},
		{"an upload source with a secret", domain.NewKnowledgeSource{Name: "up", Title: "t", Kind: domain.SourceUpload, Secret: docsSecret}, domain.ErrInvalid},
	} {
		_, err := f.Knowledge.CreateSource(ctx, bad.n, admin)
		f.refused("CreateSource with "+bad.what, err, bad.want)
	}

	src := f.source(f.org, onSecret)
	if src.Secret != docsSecret || src.Connection != "" || src.Config[domain.SourceReaderKey] != readerGitDocs {
		f.t.Errorf("source = %+v; want it on secret %s with its reader", src, docsSecret)
	}

	// Renaming the secret keeps the source on it.
	renamed := "docs-token-2"
	if _, err := f.Secrets.Patch(ctx, docsSecret, domain.SecretPatch{Name: &renamed}, admin); err != nil {
		f.t.Fatalf("rename the secret: %v", err)
	}

	if got, err := f.Knowledge.Source(ctx, runbooks); err != nil || got.Secret != renamed {
		f.t.Errorf("after the rename: source secret %q, %v; want %q", got.Secret, err, renamed)
	}

	f.refused("Delete a secret a source reads with", f.Secrets.Delete(ctx, renamed, admin), ports.ErrConflict)

	// A synced source on its connection moves over to a secret; another kind takes none.
	f.synced(f.org, handbook)

	moved, err := f.Knowledge.UpdateSource(ctx, handbook, domain.KnowledgeSourcePatch{
		Secret: &renamed, Config: map[string]string{domain.SourceReaderKey: readerGitDocs},
	}, admin)
	if err != nil || moved.Secret != renamed || moved.Connection != docsConn {
		f.t.Errorf("move over = %+v, %v; want the secret and the connection it keeps", moved, err)
	}

	_, err = f.Knowledge.UpdateSource(ctx, runbooks, domain.KnowledgeSourcePatch{Config: map[string]string{}}, admin)
	f.refused("UpdateSource dropping a secret source's reader", err, domain.ErrInvalid)

	gone := "gone"
	_, err = f.Knowledge.UpdateSource(ctx, runbooks, domain.KnowledgeSourcePatch{Secret: &gone}, admin)
	f.refused("UpdateSource to an unknown secret", err, ports.ErrNotFound)

	f.upload("notes")

	_, err = f.Knowledge.UpdateSource(ctx, "notes", domain.KnowledgeSourcePatch{Secret: &renamed}, admin)
	f.refused("UpdateSource giving an upload source a secret", err, domain.ErrInvalid)

	f.must(f.Knowledge.DeleteSource(ctx, runbooks, admin, nil))
	f.must(f.Knowledge.DeleteSource(ctx, handbook, admin, nil))
	f.must(f.Secrets.Delete(ctx, renamed, admin))
}

// claim claims at now for lease, in no organization (a claim reaches every one), failing the
// test on an error.
func (f *fixture) claim(now time.Time, lease time.Duration) *domain.SourceClaim {
	f.t.Helper()

	c, err := f.Knowledge.ClaimSource(f.t.Context(), now, now.Add(lease))
	f.must(err)

	return c
}

// claimed claims at now for lease and fails the test unless it gets a source.
func (f *fixture) claimed(what string, now time.Time, lease time.Duration) domain.SourceClaim {
	f.t.Helper()

	c := f.claim(now, lease)
	if c == nil {
		f.t.Fatalf("%s: ClaimSource = nil, want a source", what)
	}

	return *c
}

// none fails the test when a claim at now gets a source.
func (f *fixture) none(what string, now time.Time) {
	f.t.Helper()

	if c := f.claim(now, time.Minute); c != nil {
		f.t.Errorf("%s: ClaimSource = %+v, want none", what, c)
	}
}

// finish ends c's sync at the time at, with errText ("" for success).
func (f *fixture) finish(c domain.SourceClaim, at time.Time, errText string) {
	f.t.Helper()

	f.must(f.Knowledge.FinishSync(f.in(c.Org), c.Lease, domain.SyncResult{At: at, Due: *c.Source.NextSyncAt, Error: errText}))
}

// testClaimLease: a claimed source is leased; while the lease runs nobody else claims it; a
// goald that died leaves it to the next once the lease ends, and the dead one's renewal fails.
func testClaimLease(f *fixture) {
	ctx := f.ctx()
	src := f.upload(handbook)
	f.source(f.org, domain.NewKnowledgeSource{Name: chatSearch, Kind: domain.SourceLive, Connection: chatConn})

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	lease := 10 * time.Minute

	first := f.claimed("the due source", t0, lease)
	f.want("claimed", first, first.Org == f.org, first.Source.ID == src.ID, first.Source.Status == domain.SyncSyncing,
		equalTime(first.Source.LeaseUntil, t0.Add(lease)), first.Lease.Source == src.ID, first.Lease.Token != "")

	f.none("while leased (live sources never)", t0.Add(5*time.Minute))
	f.must(f.Knowledge.RenewSource(ctx, first.Lease, t0.Add(5*time.Minute), t0.Add(15*time.Minute)))

	// The holder dies: nothing renews the lease, which ends at t0+15m.
	f.none("before the renewed lease ends", t0.Add(14*time.Minute))

	second := f.claimed("after the lease ended", t0.Add(16*time.Minute), lease)
	f.want("taken over", second, second.Source.ID == src.ID, second.Lease.Token != first.Lease.Token)

	f.refused("RenewSource after the lease ended",
		f.Knowledge.RenewSource(ctx, first.Lease, t0.Add(30*time.Minute), t0.Add(40*time.Minute)), ports.ErrLeaseLost)
	f.refused("RenewSource of another organization's source",
		f.Knowledge.RenewSource(f.in(f.NewOrg(f.t)), second.Lease, t0.Add(17*time.Minute), t0.Add(20*time.Minute)), ports.ErrLeaseLost)
}

// testClaimFenced: a holder that stalled past its lease while another claim took the source over
// renews, marks seen, deletes and finishes nothing, even by a clock that lags (review 4 SQL M1);
// and a document's seen time never goes back, so the new holder's sync stands.
func testClaimFenced(f *fixture) {
	ctx := f.ctx()
	f.synced(f.org, runbooks)

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	lease := 10 * time.Minute
	stale := f.claimed("the first holder", t0, lease)

	t1 := t0.Add(lease + time.Minute)
	fresh := f.claimed("after the first lease ended", t1, lease)

	// The first holder wakes, its clock behind: within its own lease by that clock.
	f.refused("the stale holder's RenewSource", f.Knowledge.RenewSource(ctx, stale.Lease, t0.Add(5*time.Minute), t0.Add(15*time.Minute)),
		ports.ErrLeaseLost)
	f.refused("the stale holder's FinishSync",
		f.Knowledge.FinishSync(ctx, stale.Lease, domain.SyncResult{At: t0.Add(6 * time.Minute), Due: *stale.Source.NextSyncAt}), ports.ErrLeaseLost)

	got := f.get(f.org, runbooks)
	f.want("the source (the newer holder's sync)", got, got.Status == domain.SyncSyncing, equalTime(got.LeaseUntil, t1.Add(lease)),
		got.LastSyncedAt == nil)

	f.must(f.Knowledge.RenewSource(ctx, fresh.Lease, t1.Add(time.Minute), t1.Add(lease+time.Minute)))
	f.finish(fresh, t1.Add(2*time.Minute), "")
}

// testClaimAcrossOrgs: one claim reaches every organization's sources, the one due first first,
// so no organization waits for another's to drain.
func testClaimAcrossOrgs(f *fixture) {
	other := f.NewOrg(f.t)
	f.setUp(other)

	// Due in the order they are created: next_sync_at is their creation time.
	first := f.upload(handbook)
	second := f.source(other, domain.NewKnowledgeSource{Name: handbook, Kind: domain.SourceUpload})
	third := f.synced(f.org, runbooks)

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)

	for i, want := range []struct {
		org domain.OrgID
		id  domain.KnowledgeSourceID
	}{{f.org, first.ID}, {other, second.ID}, {f.org, third.ID}} {
		c := f.claimed("claim", t0, time.Minute)
		f.want("claim in due order", []any{i, c.Org, c.Source.Name}, c.Org == want.org, c.Source.ID == want.id, c.Lease.Source == want.id)
	}

	f.none("all three claimed", t0)
}

// testClaimSchedule: a finished sync is due again after the interval; a failed one says why;
// Sync now during a sync stands.
func testClaimSchedule(f *fixture) {
	ctx := f.ctx()
	f.upload(handbook)

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	t1 := t0.Add(2 * time.Minute)
	f.finish(f.claimed("the new source", t0, 10*time.Minute), t1, "")

	got := f.get(f.org, handbook)
	f.want("after FinishSync", got, got.Status == domain.SyncIdle, got.LeaseUntil == nil, equalTime(got.LastSyncedAt, t1),
		equalTime(got.NextSyncAt, t1.Add(time.Hour)), got.LastError == "")

	f.none("before the next sync", t1.Add(30*time.Minute))

	t2 := t1.Add(62 * time.Minute)
	f.finish(f.claimed("due again", t1.Add(61*time.Minute), 10*time.Minute), t2, "the bucket is unreachable")

	got = f.get(f.org, handbook)
	f.want("after a failed sync", got, got.Status == domain.SyncFailed, got.LastError == "the bucket is unreachable",
		equalTime(got.LastSyncedAt, t1), equalTime(got.NextSyncAt, t2.Add(time.Hour)))

	// Sync now while a sync runs: the source is due again as soon as that one ends.
	s := f.claimed("due after the failure", t2.Add(61*time.Minute), 10*time.Minute)
	f.must(f.Knowledge.SyncNow(ctx, handbook))
	f.finish(s, t2.Add(62*time.Minute), "")

	got = f.get(f.org, handbook)
	f.want("next sync after Sync now during a sync (now)", got.NextSyncAt, got.NextSyncAt != nil && !got.NextSyncAt.After(time.Now()))
}

// testFinishInvalidUTF8: a sync's error is stored as text even when it is not valid UTF-8 (a
// server's reply cut at a byte; review 4 Go L13), instead of the end failing and the source staying
// syncing until its lease ends.
func testFinishInvalidUTF8(f *fixture) {
	f.upload(handbook)

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	f.finish(f.claimed("the new source", t0, 10*time.Minute), t0, "embed: 500 caf\xc3")

	got := f.get(f.org, handbook)
	f.want("after a failed sync", got, got.Status == domain.SyncFailed, utf8.ValidString(got.LastError),
		strings.HasPrefix(got.LastError, "embed: 500 caf"))
}

// testFinishFailures: failed syncs in a row are counted, and a successful one starts again (a source
// at three shows in Needs you).
func testFinishFailures(f *fixture) {
	f.upload(handbook)

	sync := func(errText string, wantFailures int) {
		f.t.Helper()
		f.must(f.Knowledge.SyncNow(f.ctx(), handbook))

		at := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
		f.finish(f.claimed("a source made due", at, 10*time.Minute), at.Add(time.Minute), errText)

		got := f.get(f.org, handbook)
		f.want("SyncFailures after a sync", got.SyncFailures, got.SyncFailures == wantFailures)
	}

	sync("the host is down", 1)
	sync("the host is down", 2)
	sync("the host is down", 3)
	sync("", 0)
}

// Chunk texts of the ranking fixture.
const (
	exact   = "lease claims and their expiry"
	near    = "unrelated words only"
	keyword = "lease renewal lease rotation lease audit"
)

// testIsolation: another organization sees none of the sources, documents and chunks, by name,
// id, claim or search, and writes none (spec 4 §11.2 row 1).
func testIsolation(f *fixture) {
	f.synced(f.org, runbooks)

	other := f.NewOrg(f.t)
	f.setUp(other)
	in := f.in(other)

	all, err := f.Knowledge.Sources(in)
	f.must(err)
	f.want("Sources of another org", all, len(all) == 0)

	_, err = f.Knowledge.Source(in, runbooks)
	f.refused("Source of another org", err, ports.ErrNotFound)

	_, err = f.Knowledge.Uploads(in, runbooks)
	f.refused("Uploads of another org", err, ports.ErrNotFound)

	// A lease is its organization's: under another, it renews, marks, deletes and ends nothing.
	t0 := time.Now().UTC().Add(time.Minute)
	c := f.claimed("runbooks", t0, time.Minute)
	f.want("the claim's organization", c.Org, c.Org == f.org)
	f.refused("RenewSource in another org", f.Knowledge.RenewSource(in, c.Lease, t0, t0.Add(time.Minute)), ports.ErrLeaseLost)
	f.refused("FinishSync in another org", f.Knowledge.FinishSync(in, c.Lease, domain.SyncResult{At: t0, Due: *c.Source.NextSyncAt}),
		ports.ErrLeaseLost)
	f.finish(c, t0, "")
}

// testNoOrg: every method but ClaimSource needs an organization in ctx.
func testNoOrg(f *fixture) {
	ctx := f.t.Context()
	now := time.Now()
	id := domain.KnowledgeSourceID("01900000-0000-7000-8000-000000000000")
	lease := domain.SourceLease{Source: id, Token: "01900000-0000-4000-8000-000000000000"}

	calls := map[string]func() error{
		"Sources": func() error { _, err := f.Knowledge.Sources(ctx); return err },
		"Source":  func() error { _, err := f.Knowledge.Source(ctx, "x"); return err },
		"CreateSource": func() error {
			_, err := f.Knowledge.CreateSource(ctx, domain.NewKnowledgeSource{Name: "abc", Title: "x", Kind: domain.SourceUpload}, admin)
			return err
		},
		"SyncNow":     func() error { return f.Knowledge.SyncNow(ctx, "abc") },
		"RenewSource": func() error { return f.Knowledge.RenewSource(ctx, lease, now, now) },
		"FinishSync":  func() error { return f.Knowledge.FinishSync(ctx, lease, domain.SyncResult{At: now, Due: now}) },
		"PutSnapshot": func() error {
			_, err := f.Knowledge.PutSnapshot(ctx, lease, domain.KnowledgeSnapshot{ID: snap1, Source: id, CreatedAt: now}, nil)
			return err
		},
		"Snapshots": func() error { _, err := f.Knowledge.Snapshots(ctx, "abc"); return err },
		"Files": func() error {
			_, err := f.Knowledge.Files(ctx, domain.KnowledgeFileQuery{Source: "abc", Limit: 1})
			return err
		},
		"AddReads": func() error {
			return f.Knowledge.AddReads(ctx, []domain.KnowledgeFileReads{{Source: id, Path: fileA, Factory: factoryA, Day: now, Reads: 1}})
		},
		"FileReads": func() error { _, err := f.Knowledge.FileReads(ctx, "abc", now); return err },
		"AddUpload": func() error {
			_, err := f.Knowledge.AddUpload(ctx, "abc", domain.KnowledgeUpload{Name: "x", Blob: domain.HashBlob([]byte("x"))}, admin, nil)
			return err
		},
		"DeleteUpload": func() error { return f.Knowledge.DeleteUpload(ctx, "abc", "x", admin, nil) },
		"Uploads":      func() error { _, err := f.Knowledge.Uploads(ctx, "abc"); return err },
	}

	for name, call := range calls {
		f.refused(name+" without an organization", call(), domain.ErrNoOrg)
	}
}

// Moving over (ADR 0038 decision 16) turns a synced source on a secret into its kind: gitdocs's a
// repository source off its connection, which no source reads any more, confluence's a
// Confluence source; each keeps its secret and its config but the reader, is due at once and is
// audited. Nothing else converts.
func testConvertSource(f *fixture) {
	ctx := f.ctx()

	if _, err := f.Secrets.Create(ctx, domain.NewSecret{Name: docsSecret, Value: "test-token", Scope: domain.SecretScope{All: true}}, admin); err != nil {
		f.t.Fatalf("Create secret: %v", err)
	}

	f.synced(f.org, handbook)

	secret := docsSecret
	if _, err := f.Knowledge.UpdateSource(ctx, handbook, domain.KnowledgeSourcePatch{
		Secret: &secret, Config: map[string]string{domain.SourceReaderKey: readerGitDocs, "paths": "docs/**/*.md"},
	}, admin); err != nil {
		f.t.Fatalf("point %s at its secret: %v", handbook, err)
	}

	wiki := f.source(f.org, domain.NewKnowledgeSource{
		Name: "wiki", Kind: domain.SourceSynced, Secret: docsSecret, Config: map[string]string{domain.SourceReaderKey: "confluence", "spaces": "ENG"},
	})
	f.synced(f.org, runbooks)
	f.upload("notes")

	before := len(f.audit(f.org, domain.AuditKnowledgeSourceUpdated))

	for _, bad := range []struct {
		what, name string
		kind       domain.KnowledgeSourceKind
		want       error
	}{
		{"a synced source on its connection", runbooks, domain.SourceRepository, domain.ErrInvalid},
		{"an upload source", "notes", domain.SourceRepository, domain.ErrInvalid},
		{"a gitdocs source into Confluence", handbook, domain.SourceConfluence, domain.ErrInvalid},
		{"a source into synced", handbook, domain.SourceSynced, domain.ErrInvalid},
		{"an unknown source", "ghost", domain.SourceRepository, ports.ErrNotFound},
	} {
		_, err := f.Knowledge.ConvertSource(ctx, bad.name, bad.kind, admin)
		f.refused("ConvertSource of "+bad.what, err, bad.want)
	}

	repo, err := f.Knowledge.ConvertSource(ctx, handbook, domain.SourceRepository, admin)
	f.must(err)
	f.want("the converted repository source", repo, repo.Kind == domain.SourceRepository, repo.Connection == "", repo.Secret == docsSecret,
		reflect.DeepEqual(repo.Config, map[string]string{"paths": "docs/**/*.md"}), repo.NextSyncAt != nil && !repo.NextSyncAt.After(time.Now()))
	f.want("the source as read", f.get(f.org, handbook), f.get(f.org, handbook).Kind == domain.SourceRepository)

	conf, err := f.Knowledge.ConvertSource(ctx, wiki.Name, domain.SourceConfluence, admin)
	f.must(err)
	f.want("the converted Confluence source", conf, conf.Kind == domain.SourceConfluence, conf.Secret == docsSecret,
		reflect.DeepEqual(conf.Config, map[string]string{"spaces": "ENG"}))

	_, err = f.Knowledge.ConvertSource(ctx, handbook, domain.SourceRepository, admin)
	f.refused("ConvertSource of a converted source", err, domain.ErrInvalid)

	entries := f.audit(f.org, domain.AuditKnowledgeSourceUpdated)
	if len(entries) != before+2 || entries[0].Data["kind"] != string(domain.SourceConfluence) || entries[1].Data["kind"] != string(domain.SourceRepository) {
		f.t.Errorf("updated entries %+v; want one a conversion, with its kind", entries)
	}

	// Only runbooks reads the connection now.
	f.must(f.Knowledge.DeleteSource(ctx, runbooks, admin, nil))
	f.must(f.Connections.Delete(ctx, docsConn, admin))
}

// uploadFile uploads content h as name to source.
func (f *fixture) uploadFile(source, name string, h domain.BlobHash) domain.KnowledgeUpload {
	f.t.Helper()

	u, err := f.Knowledge.AddUpload(f.ctx(), source, domain.KnowledgeUpload{Name: name, MIME: "text/markdown", Bytes: 3, Blob: h}, admin, nil)
	f.must(err)

	return u
}

// uploadsOf are the names of source's uploads.
func (f *fixture) uploadsOf(source string) []string {
	f.t.Helper()

	files, err := f.Knowledge.Uploads(f.ctx(), source)
	f.must(err)

	out := make([]string, 0, len(files))
	for _, u := range files {
		out = append(out, u.Name)
	}

	return out
}

// testUploads: an upload is a file by name; the same content under the same name is the same
// file and another content replaces it; a deletion and a replacement drop the content no upload
// holds any more, and a drop that fails undoes the change.
func testUploads(f *fixture) {
	ctx := f.ctx()
	f.upload(handbook)
	f.upload("archive")
	f.synced(f.org, runbooks)

	h1, h2, h3 := domain.HashBlob([]byte("one")), domain.HashBlob([]byte("two")), domain.HashBlob([]byte("three"))

	u1 := f.uploadFile(handbook, "notes.md", h1)
	f.want("AddUpload", u1, u1.Blob == h1, u1.Name == "notes.md", u1.Bytes == 3, u1.CreatedBy.Ref() == admin.Ref())

	again := f.uploadFile(handbook, "notes.md", h1)
	f.want("the same content again (the same file)", again.ID, again.ID == u1.ID)

	f.uploadFile(handbook, "guide.md", h2)
	f.uploadFile("archive", "guide.md", h2)
	f.want("Uploads, by name", f.uploadsOf(handbook), slices.Equal(f.uploadsOf(handbook), []string{"guide.md", "notes.md"}))

	var dropped []domain.BlobHash

	drop := func(_ context.Context, unheld []domain.BlobHash) error {
		dropped = append(dropped, unheld...)

		return nil
	}
	failing := func(context.Context, []domain.BlobHash) error { return errors.New("the bucket is unreachable") }

	err := f.Knowledge.DeleteUpload(ctx, handbook, "notes.md", admin, failing)
	f.want("DeleteUpload whose drop fails", err, err != nil)
	f.want("the file after a failed drop", f.uploadsOf(handbook), slices.Contains(f.uploadsOf(handbook), "notes.md"))

	f.must(f.Knowledge.DeleteUpload(ctx, handbook, "notes.md", admin, drop))
	f.want("DeleteUpload of the only holder", dropped, slices.Equal(dropped, []domain.BlobHash{h1}))

	// Another content under a name replaces the file and drops the content it held.
	replaced, err := f.Knowledge.AddUpload(ctx, handbook, domain.KnowledgeUpload{Name: "guide.md", MIME: "text/markdown", Bytes: 5, Blob: h3}, admin, drop)
	f.must(err)
	f.want("AddUpload over a name", replaced, replaced.Blob == h3, replaced.Bytes == 5, len(dropped) == 1)
	f.want("the one file", f.uploadsOf(handbook), len(f.uploadsOf(handbook)) == 1)

	f.must(f.Knowledge.DeleteUpload(ctx, "archive", "guide.md", admin, drop))
	f.want("DeleteUpload of the content a replaced file held", dropped, slices.Equal(dropped, []domain.BlobHash{h1, h2}))

	f.must(f.Knowledge.DeleteSource(ctx, handbook, admin, drop))
	f.want("DeleteSource", dropped, slices.Equal(dropped, []domain.BlobHash{h1, h2, h3}))

	uploaded, deleted := f.audit(f.org, domain.AuditKnowledgeDocumentUploaded), f.audit(f.org, domain.AuditKnowledgeDocumentDeleted)
	f.want("audit (the same content again is none)", []int{len(uploaded), len(deleted)}, len(uploaded) == 4, len(deleted) == 2)

	f.uploadRefusals(h1)
}

// uploadRefusals: what an upload and a deletion refuse.
func (f *fixture) uploadRefusals(h domain.BlobHash) {
	ctx := f.ctx()
	f.upload(handbook)

	_, err := f.Knowledge.AddUpload(ctx, runbooks, domain.KnowledgeUpload{Name: "x", MIME: plainText, Blob: h}, admin, nil)
	f.refused("AddUpload to a synced source", err, domain.ErrNotUploadSource)

	_, err = f.Knowledge.AddUpload(ctx, "nope", domain.KnowledgeUpload{Name: "x", MIME: plainText, Blob: h}, admin, nil)
	f.refused("AddUpload to an unknown source", err, ports.ErrNotFound)

	_, err = f.Knowledge.AddUpload(ctx, handbook, domain.KnowledgeUpload{Name: "x", MIME: plainText, Blob: "nothex"}, admin, nil)
	f.refused("AddUpload without a content hash", err, domain.ErrInvalid)

	f.refused("DeleteUpload of an unknown file", f.Knowledge.DeleteUpload(ctx, handbook, "nope.md", admin, nil), ports.ErrNotFound)
	f.refused("DeleteUpload of a synced source", f.Knowledge.DeleteUpload(ctx, runbooks, "x", admin, nil), domain.ErrNotUploadSource)

	_, err = f.Knowledge.Uploads(ctx, runbooks)
	f.refused("Uploads of a synced source", err, domain.ErrNotUploadSource)
}
