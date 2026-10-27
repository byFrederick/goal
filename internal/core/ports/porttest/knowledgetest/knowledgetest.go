// Package knowledgetest is the conformance suite for ports.Knowledge (spec 4 §5.6). The in-memory
// fake in porttest and the PostgreSQL adapter run it, so tests on the fake hold for the
// database's rules: sources and their connections, leases that end so another goald takes the
// sync over, documents replaced with their chunks, unseen documents deleted, uploads, the one
// hybrid ranking and its sources filter, and organizations kept apart.
package knowledgetest

import (
	"context"
	"errors"
	"math"
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
	Audit       ports.Audit
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
		{"Claim_LeaseEndsAndAnotherTakesOver", testClaimLease},
		{"Claim_TakenOverLeaseChangesNothing", testClaimFenced},
		{"Claim_EveryOrganizationOldestDueFirst", testClaimAcrossOrgs},
		{"Claim_ScheduleAndSyncNow", testClaimSchedule},
		{"FinishSync_AnErrorThatIsNoUTF8", testFinishInvalidUTF8},
		{"Claim_AnotherModelIsDue", testClaimModel},
		{"Documents_ReplacedSeenAndUnseenDeleted", testDocuments},
		{"Uploads_PendingDueAndOrphans", testUploads},
		{"Search_HybridRanking", testSearchRanking},
		{"SearchOnlyListedSources", testSearchSources},
		{"OtherOrg_SeesNothing", testIsolation},
		{"Uses_UpsertIsIdempotent", testUsesUpsert},
		{"Verdicts_LaterReplacesEarlier", testVerdictsReplace},
		{"Usage_WindowCountsOnlyRunsInside", testUsageWindow},
		{"Uses_OtherOrgSeesNothing", testUsesIsolation},
		{"DocumentDelete_CascadesUsesAndVerdicts", testDeleteCascades},
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
	syncKind  = "kb-docs"
	liveKind  = "kb-chat"
	docsConn  = "docs"
	chatConn  = "chat"
	model     = "acme-embed-1"
	otherName = "acme-embed-2"
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

// put stores a document of source with one chunk per text, embedded by vecs when given.
func (f *fixture) put(org domain.OrgID, source domain.KnowledgeSourceID, ext, version string, texts []string, vecs [][]float32) {
	f.t.Helper()

	d := domain.KnowledgeDocument{
		Source: source, ExternalID: ext, Version: version, Title: "Doc " + ext, URL: "https://git.example.com/acme/docs/" + ext,
		MIME: "text/markdown", Bytes: 100, Status: domain.DocumentIndexed, SeenAt: time.Now().UTC(), CreatedBy: domain.EngineActor,
	}

	chunks := make([]domain.Chunk, 0, len(texts))

	for i, text := range texts {
		c := domain.Chunk{Ord: i, Heading: "Section " + ext, Text: text}
		if vecs != nil {
			c.Embedding = vecs[i]
			d.EmbedModel = model
		}

		chunks = append(chunks, c)
	}

	if err := f.Knowledge.PutDocument(f.in(org), d, chunks); err != nil {
		f.t.Fatalf("PutDocument %s: %v", ext, err)
	}
}

func (f *fixture) search(org domain.OrgID, q domain.KnowledgeQuery) []domain.KnowledgeHit {
	f.t.Helper()

	if q.Limit == 0 {
		q.Limit = 10
	}

	got, err := f.Knowledge.Search(f.in(org), q)
	if err != nil {
		f.t.Fatalf("Search %q: %v", q.Text, err)
	}

	return got
}

// axis is the unit vector along dimension i, plus tilt along dimension j (none when j < 0).
func axis(i, j int, tilt float32) []float32 {
	v := make([]float32, domain.EmbeddingDimensions)
	v[i] = 1

	if j >= 0 {
		v[j] = tilt
	}

	return v
}

// texts are the hits' chunk texts in order.
func texts(hits []domain.KnowledgeHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Text)
	}

	return out
}

// Names of the sources, texts and types the tests use.
const (
	handbook   = "handbook"
	runbooks   = "runbooks"
	chatSearch = "chat-search"
	plainText  = "text/plain"
	leaseWord  = "lease"
	newName    = "syn"
	docA       = "docs/a.md"
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
		up.NextSyncAt != nil && !up.NextSyncAt.Before(before), up.Connection == "", up.Documents == 0, up.CreatedBy.Ref() == admin.Ref())

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

// claim claims at now for lease, in no organization (a claim reaches every one), failing the
// test on an error.
func (f *fixture) claim(now time.Time, lease time.Duration, m string) *domain.SourceClaim {
	f.t.Helper()

	c, err := f.Knowledge.ClaimSource(f.t.Context(), now, now.Add(lease), m)
	f.must(err)

	return c
}

// claimed claims at now for lease and fails the test unless it gets a source.
func (f *fixture) claimed(what string, now time.Time, lease time.Duration, m string) domain.SourceClaim {
	f.t.Helper()

	c := f.claim(now, lease, m)
	if c == nil {
		f.t.Fatalf("%s: ClaimSource = nil, want a source", what)
	}

	return *c
}

// none fails the test when a claim at now gets a source.
func (f *fixture) none(what string, now time.Time, m string) {
	f.t.Helper()

	if c := f.claim(now, time.Minute, m); c != nil {
		f.t.Errorf("%s: ClaimSource = %+v, want none", what, c)
	}
}

// finish ends c's sync at the time at, with errText ("" for success) and model.
func (f *fixture) finish(c domain.SourceClaim, at time.Time, errText, m string) {
	f.t.Helper()

	f.must(f.Knowledge.FinishSync(f.in(c.Org), c.Lease, domain.SyncResult{At: at, Due: *c.Source.NextSyncAt, Error: errText, EmbedModel: m}))
}

// testClaimLease: a claimed source is leased; while the lease runs nobody else claims it; a
// goald that died leaves it to the next once the lease ends, and the dead one's renewal fails.
func testClaimLease(f *fixture) {
	ctx := f.ctx()
	src := f.upload(handbook)
	f.source(f.org, domain.NewKnowledgeSource{Name: chatSearch, Kind: domain.SourceLive, Connection: chatConn})

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	lease := 10 * time.Minute

	first := f.claimed("the due source", t0, lease, "")
	f.want("claimed", first, first.Org == f.org, first.Source.ID == src.ID, first.Source.Status == domain.SyncSyncing,
		equalTime(first.Source.LeaseUntil, t0.Add(lease)), first.Lease.Source == src.ID, first.Lease.Token != "")

	f.none("while leased (live sources never)", t0.Add(5*time.Minute), "")
	f.must(f.Knowledge.RenewSource(ctx, first.Lease, t0.Add(5*time.Minute), t0.Add(15*time.Minute)))

	// The holder dies: nothing renews the lease, which ends at t0+15m.
	f.none("before the renewed lease ends", t0.Add(14*time.Minute), "")

	second := f.claimed("after the lease ended", t0.Add(16*time.Minute), lease, "")
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
	src := f.synced(f.org, runbooks)
	f.put(f.org, src.ID, docA, "v1", []string{"alpha"}, nil)
	f.put(f.org, src.ID, "docs/b.md", "v1", []string{"beta"}, nil)

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	lease := 10 * time.Minute
	stale := f.claimed("the first holder", t0, lease, "")

	t1 := t0.Add(lease + time.Minute)
	fresh := f.claimed("after the first lease ended", t1, lease, "")
	f.must(f.Knowledge.MarkSeen(ctx, fresh.Lease, []string{docA}, t1))

	// The first holder wakes, its clock behind: within its own lease by that clock.
	f.refused("the stale holder's RenewSource", f.Knowledge.RenewSource(ctx, stale.Lease, t0.Add(5*time.Minute), t0.Add(15*time.Minute)),
		ports.ErrLeaseLost)
	f.refused("the stale holder's MarkSeen", f.Knowledge.MarkSeen(ctx, stale.Lease, []string{docA}, t0), ports.ErrLeaseLost)

	n, err := f.Knowledge.DeleteUnseen(ctx, stale.Lease, t1.Add(time.Hour))
	f.refused("the stale holder's DeleteUnseen", err, ports.ErrLeaseLost)
	f.want("documents the stale holder deleted", n, n == 0)
	f.refused("the stale holder's FinishSync",
		f.Knowledge.FinishSync(ctx, stale.Lease, domain.SyncResult{At: t0.Add(6 * time.Minute), Due: *stale.Source.NextSyncAt}), ports.ErrLeaseLost)

	// Its document written in flight keeps the newer seen time; so does an earlier mark.
	f.must(f.Knowledge.PutDocument(ctx, domain.KnowledgeDocument{
		Source: src.ID, ExternalID: docA, Version: "v1", Title: "Doc docs/a.md", MIME: "text/markdown", Status: domain.DocumentIndexed,
		SeenAt: t0, CreatedBy: domain.EngineActor,
	}, []domain.Chunk{{Text: "alpha"}}))
	f.must(f.Knowledge.MarkSeen(ctx, fresh.Lease, []string{docA}, t0))

	a := f.docNamed(runbooks, docA)
	f.want("a.md's seen time (the newer holder's)", a.SeenAt, a.SeenAt.Equal(t1))

	got := f.get(f.org, runbooks)
	f.want("the source (the newer holder's sync)", got, got.Status == domain.SyncSyncing, equalTime(got.LeaseUntil, t1.Add(lease)),
		got.LastSyncedAt == nil)

	f.must(f.Knowledge.RenewSource(ctx, fresh.Lease, t1.Add(time.Minute), t1.Add(lease+time.Minute)))

	n, err = f.Knowledge.DeleteUnseen(ctx, fresh.Lease, t1)
	f.must(err)
	f.want("DeleteUnseen of the newer holder (b.md)", n, n == 1)
	f.finish(fresh, t1.Add(2*time.Minute), "", "")
	f.refused("MarkSeen after the sync ended", f.Knowledge.MarkSeen(ctx, fresh.Lease, []string{docA}, t1), ports.ErrLeaseLost)
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
		c := f.claimed("claim", t0, time.Minute, "")
		f.want("claim in due order", []any{i, c.Org, c.Source.Name}, c.Org == want.org, c.Source.ID == want.id, c.Lease.Source == want.id)
	}

	f.none("all three claimed", t0, "")
}

// testClaimSchedule: a finished sync is due again after the interval; a failed one says why;
// Sync now during a sync stands.
func testClaimSchedule(f *fixture) {
	ctx := f.ctx()
	f.upload(handbook)

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	t1 := t0.Add(2 * time.Minute)
	f.finish(f.claimed("the new source", t0, 10*time.Minute, ""), t1, "", "")

	got := f.get(f.org, handbook)
	f.want("after FinishSync", got, got.Status == domain.SyncIdle, got.LeaseUntil == nil, equalTime(got.LastSyncedAt, t1),
		equalTime(got.NextSyncAt, t1.Add(time.Hour)), got.LastError == "")

	f.none("before the next sync", t1.Add(30*time.Minute), "")

	t2 := t1.Add(62 * time.Minute)
	f.finish(f.claimed("due again", t1.Add(61*time.Minute), 10*time.Minute, ""), t2, "the bucket is unreachable", "")

	got = f.get(f.org, handbook)
	f.want("after a failed sync", got, got.Status == domain.SyncFailed, got.LastError == "the bucket is unreachable",
		equalTime(got.LastSyncedAt, t1), equalTime(got.NextSyncAt, t2.Add(time.Hour)))

	// Sync now while a sync runs: the source is due again as soon as that one ends.
	s := f.claimed("due after the failure", t2.Add(61*time.Minute), 10*time.Minute, "")
	f.must(f.Knowledge.SyncNow(ctx, handbook))
	f.finish(s, t2.Add(62*time.Minute), "", "")

	got = f.get(f.org, handbook)
	f.want("next sync after Sync now during a sync (now)", got.NextSyncAt, got.NextSyncAt != nil && !got.NextSyncAt.After(time.Now()))
}

// testFinishInvalidUTF8: a sync's error is stored as text even when it is not valid UTF-8 (a
// server's reply cut at a byte; review 4 Go L13), instead of the end failing and the source staying
// syncing until its lease ends.
func testFinishInvalidUTF8(f *fixture) {
	f.upload(handbook)

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	f.finish(f.claimed("the new source", t0, 10*time.Minute, ""), t0, "embed: 500 caf\xc3", "")

	got := f.get(f.org, handbook)
	f.want("after a failed sync", got, got.Status == domain.SyncFailed, utf8.ValidString(got.LastError),
		strings.HasPrefix(got.LastError, "embed: 500 caf"))
}

// testClaimModel: a source whose chunks are of another model is due for a full sync, unless its
// last sync failed.
func testClaimModel(f *fixture) {
	f.upload(handbook)

	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	f.finish(f.claimed("the new source", t0, 10*time.Minute, model), t0, "", model)

	got := f.get(f.org, handbook)
	f.want("EmbedModel", got.EmbedModel, got.EmbedModel == model)

	f.none("the same model before due", t0.Add(time.Minute), model)

	s := f.claimed("another model (a full sync)", t0.Add(time.Minute), 10*time.Minute, otherName)
	f.finish(s, t0.Add(2*time.Minute), "no embedder", "")

	f.none("another model after a failed sync, before due", t0.Add(3*time.Minute), otherName)
}

// docNamed is the document of source with external id ext.
func (f *fixture) docNamed(source, ext string) domain.KnowledgeDocument {
	f.t.Helper()

	docs, err := f.Knowledge.Documents(f.ctx(), source, "", 100)
	f.must(err)

	for _, d := range docs {
		if d.ExternalID == ext {
			return d
		}
	}

	f.t.Fatalf("%s has no document %s", source, ext)

	return domain.KnowledgeDocument{}
}

// testDocuments: a document is replaced with its chunks; versions skip pending ones; documents a
// sync did not see go.
func testDocuments(f *fixture) {
	ctx := f.ctx()
	src := f.synced(f.org, runbooks)

	f.put(f.org, src.ID, docA, "v1", []string{"alpha one", "alpha two"}, nil)
	f.put(f.org, src.ID, "docs/b.md", "v1", []string{"beta"}, nil)

	versions, err := f.Knowledge.Versions(ctx, src.ID)
	f.must(err)
	f.want("Versions", versions, reflect.DeepEqual(versions, map[string]string{docA: "v1", "docs/b.md": "v1"}))

	a := f.docNamed(runbooks, docA)

	_, text, err := f.Knowledge.Document(ctx, a.ID)
	f.must(err)
	f.want("Document text", text, text == "alpha one\n\nalpha two")

	f.put(f.org, src.ID, docA, "v2", []string{"alpha again"}, nil)

	got, text, err := f.Knowledge.Document(ctx, a.ID)
	f.must(err)
	f.want("the same document at v2 with its one chunk", got, got.ID == a.ID, got.Version == "v2", text == "alpha again")

	hits := f.search(f.org, domain.KnowledgeQuery{Text: "two", Sources: []string{runbooks}})
	f.want("a replaced chunk found", hits, len(hits) == 0)

	page, err := f.Knowledge.Documents(ctx, runbooks, "", 1)
	f.must(err)
	f.want("the first page", page, len(page) == 1)

	rest, err := f.Knowledge.Documents(ctx, runbooks, page[0].ID, 10)
	f.must(err)
	f.want("the next page", rest, len(rest) == 1, len(rest) == 1 && rest[0].ID != page[0].ID)

	pass := time.Now().UTC().Add(time.Second).Truncate(time.Millisecond)
	sync := f.claimed("runbooks", pass, time.Minute, "")
	f.must(f.Knowledge.MarkSeen(ctx, sync.Lease, []string{docA}, pass))

	n, err := f.Knowledge.DeleteUnseen(ctx, sync.Lease, pass)
	f.must(err)
	f.want("DeleteUnseen (b.md)", n, n == 1)

	// An external id is bounded in bytes, as its unique index's entry is (review 4 SQL L4):
	// 1,000 characters of three bytes each are too long.
	long := domain.KnowledgeDocument{
		Source: src.ID, ExternalID: "docs/" + strings.Repeat("日", 1000) + ".md", Version: "v1", Title: "x", MIME: plainText,
		Status: domain.DocumentIndexed, SeenAt: time.Now(), CreatedBy: domain.EngineActor,
	}
	f.refused("PutDocument with an external id over MaxExternalID bytes", f.Knowledge.PutDocument(ctx, long, nil), domain.ErrInvalid)

	long.ExternalID = "docs/" + strings.Repeat("日", 600) + ".md"
	f.must(f.Knowledge.PutDocument(ctx, long, nil))
	f.must(f.Knowledge.MarkSeen(ctx, sync.Lease, []string{long.ExternalID}, pass))

	s := f.get(f.org, runbooks)
	f.want("Documents", s.Documents, s.Documents == 2)

	_, err = f.Knowledge.Documents(ctx, "nope", "", 10)
	f.refused("Documents of an unknown source", err, ports.ErrNotFound)

	_, _, err = f.Knowledge.Document(ctx, "01900000-0000-7000-8000-000000000000")
	f.refused("Document unknown", err, ports.ErrNotFound)
}

// add uploads content h to source.
func (f *fixture) add(source string, h domain.BlobHash) domain.KnowledgeDocument {
	f.t.Helper()

	d, err := f.Knowledge.AddUpload(f.ctx(), source, domain.KnowledgeDocument{Title: "notes.md", MIME: "text/markdown", Bytes: 3, Blob: h}, admin)
	f.must(err)

	return d
}

// testUploads: an upload is pending (Uploads makes its source due once its content is
// stored); the same content is the same document; a deletion drops the content no document
// holds any more, and a drop that fails undoes it.
func testUploads(f *fixture) {
	ctx := f.ctx()
	f.upload(handbook)
	f.upload("archive")
	f.synced(f.org, runbooks)

	// The new sources are due: sync them, so only an upload makes one due again.
	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	for s := f.claim(t0, time.Minute, ""); s != nil; s = f.claim(t0, time.Minute, "") {
		f.finish(*s, t0, "", "")
	}

	h1, h2 := domain.HashBlob([]byte("one")), domain.HashBlob([]byte("two"))

	d1 := f.add(handbook, h1)
	f.want("AddUpload", d1, d1.Status == domain.DocumentPending, d1.ExternalID == string(h1), d1.Version == string(h1), d1.Blob == h1,
		d1.Title == "notes.md")

	again := f.add(handbook, h1)
	f.want("the same content again (the same document)", again.ID, again.ID == d1.ID)
	f.none("after an upload, before Sync now", t0.Add(2*time.Minute), "")

	v, err := f.Knowledge.Versions(ctx, d1.Source)
	f.must(err)
	f.want("Versions with a pending upload (none)", v, len(v) == 0)

	d2 := f.add(handbook, h2)
	f.add("archive", h2)

	var dropped []domain.BlobHash

	drop := func(_ context.Context, unheld []domain.BlobHash) error {
		dropped = append(dropped, unheld...)

		return nil
	}
	failing := func(context.Context, []domain.BlobHash) error { return errors.New("the bucket is unreachable") }

	err = f.Knowledge.DeleteDocument(ctx, d1.ID, admin, failing)
	f.want("DeleteDocument whose drop fails", err, err != nil)
	f.want("the document after a failed drop", f.docNamed(handbook, string(h1)).ID, f.docNamed(handbook, string(h1)).ID == d1.ID)

	f.must(f.Knowledge.DeleteDocument(ctx, d1.ID, admin, drop))
	f.want("DeleteDocument of the only holder", dropped, slices.Equal(dropped, []domain.BlobHash{h1}))

	// A sync's write of the deleted upload does not bring it back (review 4 Go L11).
	f.refused("PutDocument of a deleted upload", f.Knowledge.PutDocument(ctx, domain.KnowledgeDocument{
		Source: d1.Source, ExternalID: string(h1), Version: string(h1), Title: "notes.md", MIME: plainText, Status: domain.DocumentIndexed,
		SeenAt: time.Now(), CreatedBy: domain.EngineActor,
	}, []domain.Chunk{{Text: "one"}}), ports.ErrNotFound)

	docs, err := f.Knowledge.Documents(ctx, handbook, "", 10)
	f.must(err)
	f.want("handbook's documents after the deleted upload's write", docs, len(docs) == 1, len(docs) == 1 && docs[0].ID == d2.ID)

	f.must(f.Knowledge.DeleteDocument(ctx, d2.ID, admin, drop))
	f.want("DeleteDocument while archive holds it", dropped, len(dropped) == 1)

	f.must(f.Knowledge.DeleteSource(ctx, "archive", admin, drop))
	f.want("DeleteSource", dropped, slices.Equal(dropped, []domain.BlobHash{h1, h2}))

	uploaded, deleted := f.audit(f.org, domain.AuditKnowledgeDocumentUploaded), f.audit(f.org, domain.AuditKnowledgeDocumentDeleted)
	f.want("audit (the same content again is none)", []int{len(uploaded), len(deleted)}, len(uploaded) == 3, len(deleted) == 2)

	f.uploadRefusals(h1)
}

// uploadRefusals: what an upload and a deletion refuse.
func (f *fixture) uploadRefusals(h domain.BlobHash) {
	ctx := f.ctx()

	_, err := f.Knowledge.AddUpload(ctx, runbooks, domain.KnowledgeDocument{Title: "x", MIME: plainText, Blob: h}, admin)
	f.refused("AddUpload to a synced source", err, domain.ErrNotUploadSource)

	_, err = f.Knowledge.AddUpload(ctx, "nope", domain.KnowledgeDocument{Title: "x", MIME: plainText, Blob: h}, admin)
	f.refused("AddUpload to an unknown source", err, ports.ErrNotFound)

	_, err = f.Knowledge.AddUpload(ctx, handbook, domain.KnowledgeDocument{Title: "x", MIME: plainText, Blob: "nothex"}, admin)
	f.refused("AddUpload without a content hash", err, domain.ErrInvalid)

	f.put(f.org, f.get(f.org, runbooks).ID, docA, "v1", []string{"alpha"}, nil)

	err = f.Knowledge.DeleteDocument(ctx, f.docNamed(runbooks, docA).ID, admin, nil)
	f.refused("DeleteDocument of a synced document", err, domain.ErrNotUploadSource)
}

// Chunk texts of the ranking fixture.
const (
	exact   = "lease claims and their expiry"
	near    = "unrelated words only"
	keyword = "lease renewal lease rotation lease audit"
)

// testSearchRanking: keyword and nearest-neighbour rankings fused by reciprocal rank, each alone
// when the other finds nothing, chunks of another model never ranked by distance.
func testSearchRanking(f *fixture) {
	src := f.synced(f.org, runbooks)
	f.put(f.org, src.ID, "docs/leases.md", "v1", []string{exact, near, keyword},
		[][]float32{axis(0, -1, 0), axis(0, 1, 0.25), axis(5, -1, 0)})

	all := []string{runbooks}
	query := axis(0, -1, 0)

	// Nearest: exact, near, keyword. Keywords: keyword (lease three times), exact (once).
	hybrid := f.search(f.org, domain.KnowledgeQuery{Text: leaseWord, Sources: all, Embedding: query, Model: model})
	if got, want := texts(hybrid), []string{exact, keyword, near}; !slices.Equal(got, want) {
		f.t.Fatalf("hybrid = %q, want %q", got, want)
	}

	for i, want := range []float64{1.0/61 + 1.0/62, 1.0/63 + 1.0/61, 1.0 / 62} {
		f.want("hit's reciprocal rank score (k = 60)", hybrid[i], math.Abs(hybrid[i].Score-want) < 1e-9)
	}

	h := hybrid[0]
	f.want("hit", h, h.Source == runbooks, h.Title == "Doc docs/leases.md", h.URL == "https://git.example.com/acme/docs/docs/leases.md",
		h.Heading == "Section docs/leases.md", h.Document != "", h.Chunk != 0)

	for what, tc := range map[string]struct {
		q    domain.KnowledgeQuery
		want []string
	}{
		"keywords only":          {domain.KnowledgeQuery{Text: leaseWord}, []string{keyword, exact}},
		"nearest only":           {domain.KnowledgeQuery{Text: "zebra", Embedding: query, Model: model}, []string{exact, near, keyword}},
		"another model's query":  {domain.KnowledgeQuery{Text: leaseWord, Embedding: query, Model: otherName}, []string{keyword, exact}},
		"limit 1":                {domain.KnowledgeQuery{Text: leaseWord, Embedding: query, Model: model, Limit: 1}, []string{exact}},
		"a word no chunk has":    {domain.KnowledgeQuery{Text: "the"}, []string{}},
		"no words, no embedding": {domain.KnowledgeQuery{Text: "  "}, []string{}},
	} {
		tc.q.Sources = all
		got := texts(f.search(f.org, tc.q))
		f.want(what, got, slices.Equal(got, tc.want))
	}

	_, err := f.Knowledge.Search(f.ctx(), domain.KnowledgeQuery{Text: "x", Sources: all, Limit: 5, Embedding: []float32{1, 2}, Model: model})
	f.refused("Search with an embedding of other dimensions", err, domain.ErrInvalid)
}

// testSearchSources: a search ranks the chunks of the named sources only, however near or
// relevant the others' are (spec 4 §11.2 row 8).
func testSearchSources(f *fixture) {
	listed := f.synced(f.org, runbooks)
	unlisted := f.synced(f.org, "secrets")

	f.put(f.org, listed.ID, "docs/far.md", "v1", []string{"rotation schedule"}, [][]float32{axis(3, -1, 0)})
	f.put(f.org, unlisted.ID, "docs/key.md", "v1", []string{"lease ledger lease log lease"}, [][]float32{axis(0, -1, 0)})

	q := domain.KnowledgeQuery{Text: "lease rotation", Sources: []string{runbooks}, Embedding: axis(0, -1, 0), Model: model}

	hits := f.search(f.org, q)
	f.want("Search over runbooks (its one chunk only)", hits, len(hits) == 1, len(hits) == 1 && hits[0].Source == runbooks,
		len(hits) == 1 && hits[0].Text == "rotation schedule")

	for what, tc := range map[string]struct {
		sources []string
		n       int
	}{
		"no source": {nil, 0}, "an unknown source": {[]string{"unknown"}, 0}, "both": {[]string{runbooks, "secrets"}, 2},
	} {
		q.Sources = tc.sources
		hits := f.search(f.org, q)
		f.want("Search over "+what, hits, len(hits) == tc.n)
	}
}

// testIsolation: another organization sees none of the sources, documents and chunks, by name,
// id, claim or search, and writes none (spec 4 §11.2 row 1).
func testIsolation(f *fixture) {
	src := f.synced(f.org, runbooks)
	f.put(f.org, src.ID, docA, "v1", []string{"lease expiry"}, [][]float32{axis(0, -1, 0)})
	doc := f.docNamed(runbooks, docA)

	other := f.NewOrg(f.t)
	f.setUp(other)
	in := f.in(other)

	all, err := f.Knowledge.Sources(in)
	f.must(err)
	f.want("Sources of another org", all, len(all) == 0)

	_, err = f.Knowledge.Source(in, runbooks)
	f.refused("Source of another org", err, ports.ErrNotFound)

	_, _, err = f.Knowledge.Document(in, doc.ID)
	f.refused("Document of another org", err, ports.ErrNotFound)

	err = f.Knowledge.DeleteDocument(in, doc.ID, admin, nil)
	f.refused("DeleteDocument of another org", err, ports.ErrNotFound)

	v, err := f.Knowledge.Versions(in, src.ID)
	f.must(err)
	f.want("Versions of another org's source", v, len(v) == 0)

	err = f.Knowledge.PutDocument(in, domain.KnowledgeDocument{
		Source: src.ID, ExternalID: "docs/x.md", Version: "v1", Title: "x", MIME: plainText, Status: domain.DocumentIndexed,
		SeenAt: time.Now(), CreatedBy: domain.EngineActor,
	}, nil)
	f.want("PutDocument into another org's source", err, err != nil)

	// A lease is its organization's: under another, it renews, marks, deletes and ends nothing.
	t0 := time.Now().UTC().Add(time.Minute)
	c := f.claimed("runbooks", t0, time.Minute, model)
	f.want("the claim's organization", c.Org, c.Org == f.org)
	f.refused("RenewSource in another org", f.Knowledge.RenewSource(in, c.Lease, t0, t0.Add(time.Minute)), ports.ErrLeaseLost)
	f.refused("MarkSeen in another org", f.Knowledge.MarkSeen(in, c.Lease, []string{docA}, t0), ports.ErrLeaseLost)

	_, err = f.Knowledge.DeleteUnseen(in, c.Lease, t0.Add(time.Hour))
	f.refused("DeleteUnseen in another org", err, ports.ErrLeaseLost)
	f.refused("FinishSync in another org", f.Knowledge.FinishSync(in, c.Lease, domain.SyncResult{At: t0, Due: *c.Source.NextSyncAt}),
		ports.ErrLeaseLost)
	f.finish(c, t0, "", model)

	// The other org's own source of the same name, nearest to nothing: a search there never
	// reaches the first org's chunk however near it is.
	mine := f.synced(other, runbooks)
	f.put(other, mine.ID, "docs/b.md", "v1", []string{"lease renewal"}, [][]float32{axis(9, -1, 0)})

	hits := texts(f.search(other, domain.KnowledgeQuery{Text: "lease expiry", Sources: []string{runbooks}, Embedding: axis(0, -1, 0), Model: model}))
	f.want("Search in another org (its own chunk only)", hits, slices.Equal(hits, []string{"lease renewal"}))

	hits = texts(f.search(f.org, domain.KnowledgeQuery{Text: leaseWord, Sources: []string{runbooks}}))
	f.want("Search in the first org (its own chunk only)", hits, slices.Equal(hits, []string{"lease expiry"}))
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
		"SyncNow":      func() error { return f.Knowledge.SyncNow(ctx, "abc") },
		"RenewSource":  func() error { return f.Knowledge.RenewSource(ctx, lease, now, now) },
		"FinishSync":   func() error { return f.Knowledge.FinishSync(ctx, lease, domain.SyncResult{At: now, Due: now}) },
		"MarkSeen":     func() error { return f.Knowledge.MarkSeen(ctx, lease, []string{"x"}, now) },
		"DeleteUnseen": func() error { _, err := f.Knowledge.DeleteUnseen(ctx, lease, now); return err },
		"Versions":     func() error { _, err := f.Knowledge.Versions(ctx, id); return err },
		"Documents":    func() error { _, err := f.Knowledge.Documents(ctx, "abc", "", 1); return err },
		"Search": func() error {
			_, err := f.Knowledge.Search(ctx, domain.KnowledgeQuery{Text: "x", Sources: []string{"abc"}, Limit: 1})
			return err
		},
		"RecordUse": func() error {
			return f.Knowledge.RecordUse(ctx, newRun().use(domain.KnowledgeDocument{ID: "01900000-0000-7000-8000-000000000001", Source: id},
				domain.KnowledgeRead, nil, now))
		},
		"GiveVerdict": func() error {
			_, err := f.Knowledge.GiveVerdict(ctx, domain.KnowledgeVerdict{
				Document: "01900000-0000-7000-8000-000000000001", Verdict: domain.KnowledgeHelped, By: admin,
			})
			return err
		},
		"SourceUsage":  func() error { _, err := f.Knowledge.SourceUsage(ctx, "abc", 7); return err },
		"DocumentUses": func() error { _, err := f.Knowledge.DocumentUses(ctx, "01900000-0000-7000-8000-000000000001", "", 1); return err },
		"RunUses":      func() error { _, err := f.Knowledge.RunUses(ctx, newRun().run); return err },
	}

	for name, call := range calls {
		f.refused(name+" without an organization", call(), domain.ErrNoOrg)
	}
}
