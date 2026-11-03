package knowledge_test

import (
	"context"
	"errors"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/knowledge"
)

var admin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}

// Names the tests use.
const (
	deployMD       = "docs/deploy.md"
	oncallMD       = "docs/oncall.md"
	newMD          = "docs/new.md"
	slowMD         = "docs/slow.md"
	shipOnTuesdays = "# Deploy\n\nShip on Tuesdays."
	newPage        = "# New\n\nA new page."
	// gitdocsSync is the test kind's sync adapter.
	gitdocsSync = "gitdocs"
)

// want fails the test with what and got unless every condition holds.
func (w *world) want(what string, got any, conds ...bool) {
	w.t.Helper()

	for i, ok := range conds {
		if !ok {
			w.t.Errorf("%s = %+v (check %d of %d fails)", what, got, i+1, len(conds))

			return
		}
	}
}

// reader is a source's documents in memory: path → content, versioned by content. It
// records the connection it was given and every fetch; a fetch of a path in block waits until
// the test closes the channel.
type reader struct {
	mu      sync.Mutex
	docs    map[string]string
	fetched []string
	secret  string // the value of the secret it was given
	block   map[string]chan struct{}
	down    bool // every fetch fails, as a server answering 503
}

func (r *reader) set(docs map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.docs = maps.Clone(docs)
}

func (r *reader) fetches() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := slices.Clone(r.fetched)
	r.fetched = nil

	slices.Sort(out)

	return out
}

func (r *reader) List(_ context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error] {
	r.mu.Lock()
	if s.Secret != nil {
		r.secret = s.Secret.Value
	}

	docs := maps.Clone(r.docs)
	r.mu.Unlock()

	return func(yield func(domain.SourceItem, error) bool) {
		for _, p := range slices.Sorted(maps.Keys(docs)) {
			if !yield(domain.SourceItem{ID: p, Version: string(domain.HashBlob([]byte(docs[p]))), Title: p, URL: "https://git.example.com/" + p}, nil) {
				return
			}
		}
	}
}

func (r *reader) Fetch(ctx context.Context, _ domain.SyncSpec, id string) (domain.SourceDoc, error) {
	r.mu.Lock()
	r.fetched = append(r.fetched, id)
	body, ok := r.docs[id]
	wait, down := r.block[id], r.down
	r.mu.Unlock()

	if down {
		return domain.SourceDoc{}, errors.New("503 service unavailable")
	}

	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return domain.SourceDoc{}, ctx.Err()
		}
	}

	if !ok {
		return domain.SourceDoc{}, errors.New("gone")
	}

	return domain.SourceDoc{SourceItem: domain.SourceItem{ID: id, MIME: domain.MediaMarkdown}, Body: []byte(body)}, nil
}

// world is one organization's knowledge base on the port fakes, with a repository source
// "runbooks" read by docs and an upload source "handbook".
type world struct {
	t       *testing.T
	org     domain.OrgID
	tenancy *porttest.Tenancy
	conns   *porttest.Connections
	secrets *porttest.OrgSecrets
	kb      ports.Knowledge
	objects *porttest.ObjectStore
	uploads *knowledge.Uploads
	clock   *porttest.Clock
	docs    *reader
}

func newWorld(t *testing.T) *world {
	t.Helper()

	// The clock starts a second on, so the sources created now are due.
	w := &world{
		t: t, clock: porttest.NewClock(time.Now().UTC().Add(time.Second)), objects: porttest.NewObjectStore(), docs: &reader{},
	}
	w.tenancy = porttest.NewTenancy(w.clock.Now)
	w.conns = porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), w.tenancy)
	w.secrets = porttest.NewOrgSecrets(porttest.NewMasterKeys().Key("k1"), w.tenancy, w.conns)
	kb := porttest.NewKnowledge(w.conns, w.tenancy)
	kb.ReadSecrets(w.secrets)
	w.kb = kb
	w.uploads = knowledge.NewUploads(w.kb, w.objects, 0)

	o, err := w.tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	w.org = o.ID
	ctx := w.ctx()

	if _, err := w.secrets.Create(ctx, domain.NewSecret{Name: "docs-token", Value: "s3cret", Scope: domain.SecretScope{All: true}}, admin); err != nil {
		t.Fatal(err)
	}

	for _, n := range []domain.NewKnowledgeSource{
		{Name: "runbooks", Title: "Runbooks", Kind: domain.SourceRepository, Secret: "docs-token", Config: map[string]string{"repository": "https://git.example.com/acme/docs"}},
		{Name: "handbook", Title: "Handbook", Kind: domain.SourceUpload},
	} {
		if _, err := w.kb.CreateSource(ctx, n, admin); err != nil {
			t.Fatal(err)
		}
	}

	return w
}

func (w *world) ctx() context.Context { return domain.WithOrg(w.t.Context(), w.org) }

// ingestor syncs the kb given (w.kb by default).
func (w *world) ingestor(kb ports.Knowledge) *knowledge.Ingestor {
	if kb == nil {
		kb = w.kb
	}

	return knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: kb, Secrets: w.secrets, Uploads: w.uploads, Readers: map[string]ports.SourceReader{gitdocsSync: w.docs, "confluence": w.docs},
		Objects: w.objects, Clock: w.clock, TempDir: w.t.TempDir(),
	})
}

// syncDue runs one pass of in at the clock's time and returns how many sources it synced.
func (w *world) syncDue(in *knowledge.Ingestor) int {
	w.t.Helper()

	n, err := in.SyncDue(w.t.Context())
	if err != nil {
		w.t.Fatal(err)
	}

	return n
}

func (w *world) source(name string) domain.KnowledgeSource {
	w.t.Helper()

	s, err := w.kb.Source(w.ctx(), name)
	if err != nil {
		w.t.Fatal(err)
	}

	return s
}

// files are the paths of the source's current files.
func (w *world) files(source string) []string {
	w.t.Helper()

	files, err := w.kb.Files(w.ctx(), domain.KnowledgeFileQuery{Source: source, Limit: 100})
	if err != nil {
		w.t.Fatal(err)
	}

	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}

	return out
}

func TestIngestor_SyncsIncrementallyAndDropsWhatGoes(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	deploy, oncall := "# Deploy\n\nShip with the release train on Tuesdays.", "# On call\n\nPage the primary, then the secondary."
	w.docs.set(map[string]string{deployMD: deploy, oncallMD: oncall})

	in := w.ingestor(nil)
	n := w.syncDue(in)
	w.want("sources synced (both new ones)", n, n == 2)

	fetched := w.docs.fetches()
	w.want("fetched", fetched, slices.Equal(fetched, []string{deployMD, oncallMD}))
	w.want("the reader's secret", w.docs.secret, w.docs.secret == "s3cret")

	src := w.source("runbooks")
	w.want("runbooks after its sync", src, src.Status == domain.SyncIdle, src.LeaseUntil == nil, src.CurrentSnapshot != "")
	w.want("runbooks' files", w.files("runbooks"), slices.Equal(w.files("runbooks"), []string{deployMD, oncallMD}))

	// An hour on: oncall.md changed, deploy.md did not, a page is new and none went yet.
	oncall = "# On call\n\nPage the primary only."
	w.docs.set(map[string]string{deployMD: deploy, oncallMD: oncall, newMD: newPage})
	w.clock.Advance(time.Hour + time.Minute)

	n = w.syncDue(in)
	w.want("sources synced an hour on (both due again)", n, n >= 1)

	fetched = w.docs.fetches()
	w.want("fetched (only what changed)", fetched, slices.Equal(fetched, []string{newMD, oncallMD}))

	// Then deploy.md goes from the repository.
	w.docs.set(map[string]string{oncallMD: oncall, newMD: newPage})
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(in)

	files := w.files("runbooks")
	w.want("files (deploy.md gone)", files, slices.Equal(files, []string{newMD, oncallMD}))
}

func TestIngestor_AFileThatIsNotTextIsListedFailed(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.docs.set(map[string]string{"docs/good.md": "# Good\n\nfine", "docs/bad.md": "caf\xe9"})

	w.syncDue(w.ingestor(nil))

	snaps, err := w.kb.Snapshots(w.ctx(), "runbooks")
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshots = %v, %v; want one", snaps, err)
	}

	if src := w.source("runbooks"); src.Status != domain.SyncIdle || snaps[0].Failed != 1 || !slices.Equal(w.files("runbooks"), []string{"docs/good.md"}) {
		t.Errorf("runbooks = %+v, snapshot %+v, files %v; want idle, one file failed and good.md only", src, snaps[0], w.files("runbooks"))
	}
}

func TestIngestor_UnknownReaderFailsTheSource(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	in := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: w.kb, Secrets: w.secrets, Uploads: w.uploads, Objects: w.objects, Clock: w.clock,
	})
	w.syncDue(in)

	if src := w.source("runbooks"); src.Status != domain.SyncFailed || !strings.Contains(src.LastError, strconv.Quote(gitdocsSync)) {
		t.Errorf("runbooks without a gitdocs reader = %+v", src)
	}
}

func TestIngestor_UploadsBecomeFilesOfTheSnapshot(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	if _, err := w.uploads.Upload(ctx, "handbook", "leave.md", []byte("# Leave\n\nAsk your lead two weeks ahead."), admin); err != nil {
		t.Fatal(err)
	}

	if _, err := w.uploads.Upload(ctx, "handbook", "page.html", []byte("<h1>Expenses</h1><p>Receipts within 30 days.</p>"), admin); err != nil {
		t.Fatal(err)
	}

	w.syncDue(w.ingestor(nil))

	files := w.files("handbook")
	w.want("handbook's files", files, slices.Equal(files, []string{"leave.md", "page.md"}))

	first := w.source("handbook").CurrentSnapshot

	// Unchanged, an upload source writes no new snapshot.
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(w.ingestor(nil))

	if got := w.source("handbook").CurrentSnapshot; got != first {
		t.Errorf("snapshot after a sync with no change = %s, want %s", got, first)
	}

	// A file deleted leaves the next snapshot.
	if err := w.uploads.DeleteUpload(ctx, "handbook", "leave.md", admin); err != nil {
		t.Fatal(err)
	}

	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(w.ingestor(nil))

	files = w.files("handbook")
	w.want("handbook's files after a deletion", files, slices.Equal(files, []string{"page.md"}))
}

func TestUploads_StoredOnceAndDeletedWithTheLastHolder(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	if _, err := w.kb.CreateSource(ctx, domain.NewKnowledgeSource{Name: "archive", Title: "Archive", Kind: domain.SourceUpload}, admin); err != nil {
		t.Fatal(err)
	}

	body := []byte("# Policy\n\nBe kind.")
	key := domain.KnowledgeKey(w.org, domain.HashBlob(body))

	if _, err := w.uploads.Upload(ctx, "handbook", "policy.md", body, admin); err != nil {
		t.Fatal(err)
	}

	if _, err := w.uploads.Upload(ctx, "archive", "policy-copy.md", body, admin); err != nil {
		t.Fatal(err)
	}

	if n := w.objects.Writes(key); n != 1 {
		t.Errorf("the content was written %d times, want once", n)
	}

	if err := w.uploads.DeleteUpload(ctx, "handbook", "policy.md", admin); err != nil {
		t.Fatal(err)
	}

	if _, err := w.objects.Get(ctx, key); err != nil {
		t.Errorf("the object went while archive still holds it: %v", err)
	}

	if err := w.uploads.DeleteSource(ctx, "archive", admin); err != nil {
		t.Fatal(err)
	}

	if _, err := w.objects.Get(ctx, key); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Get after the last holder went = %v, want it deleted", err)
	}

	// Another content under a name replaces the file and drops the original it held.
	if _, err := w.uploads.Upload(ctx, "handbook", "policy.md", body, admin); err != nil {
		t.Fatal(err)
	}

	if _, err := w.uploads.Upload(ctx, "handbook", "policy.md", []byte("# Policy\n\nBe kinder."), admin); err != nil {
		t.Fatal(err)
	}

	if _, err := w.objects.Get(ctx, key); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Get after the file was replaced = %v, want the old original deleted", err)
	}

	_, err := w.uploads.Upload(ctx, "handbook", "slides.key", []byte("PK"), admin)
	if !errors.Is(err, domain.ErrUnsupportedDocument) {
		t.Errorf("Upload of slides = %v, want unsupported", err)
	}

	small := knowledge.NewUploads(w.kb, w.objects, 4)
	if _, err := small.Upload(ctx, "handbook", "big.md", []byte("12345"), admin); !errors.Is(err, ports.ErrTooLarge) {
		t.Errorf("Upload over the limit = %v, want too large", err)
	}

	other := []byte("# Other")
	if _, err := w.uploads.Upload(ctx, "runbooks", "x.md", other, admin); !errors.Is(err, domain.ErrNotUploadSource) {
		t.Errorf("Upload to a repository source = %v", err)
	}

	if n := w.objects.Writes(domain.KnowledgeKey(w.org, domain.HashBlob(other))); n != 0 {
		t.Errorf("a refused upload wrote its content %d times, want none", n)
	}
}

// racing is a bucket that runs then right after a PutIfAbsent and get right after a Get: what
// another request does meanwhile.
type racing struct {
	*porttest.ObjectStore

	then, get func()
}

func (r *racing) Get(ctx context.Context, key string) (ports.Object, error) {
	obj, err := r.ObjectStore.Get(ctx, key)
	if r.get != nil {
		r.get()
	}

	return obj, err
}

func (r *racing) PutIfAbsent(ctx context.Context, key string, body []byte) (ports.ObjectInfo, bool, error) {
	info, created, err := r.ObjectStore.PutIfAbsent(ctx, key, body)
	if r.then != nil {
		r.then()
	}

	return info, created, err
}

// Review 4 SQL L1 / Go L19: an upload of content whose only other holder is deleted meanwhile
// keeps its object. The upload found the object there and wrote nothing, the deletion then
// removed it as held by none, and the upload's document pointed at nothing.
func TestUploads_ADeleteOfTheSameContentMeanwhileKeepsTheObject(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	if _, err := w.kb.CreateSource(ctx, domain.NewKnowledgeSource{Name: "archive", Title: "Archive", Kind: domain.SourceUpload}, admin); err != nil {
		t.Fatal(err)
	}

	body := []byte("# Policy\n\nBe kind.")
	key := domain.KnowledgeKey(w.org, domain.HashBlob(body))

	if _, err := w.uploads.Upload(ctx, "handbook", "policy.md", body, admin); err != nil {
		t.Fatal(err)
	}

	bucket := &racing{ObjectStore: w.objects}
	uploads := knowledge.NewUploads(w.kb, bucket, 0)
	bucket.then = func() {
		bucket.then = nil

		if err := uploads.DeleteUpload(ctx, "handbook", "policy.md", admin); err != nil {
			t.Error(err)
		}
	}

	again, err := uploads.Upload(ctx, "archive", "policy-copy.md", body, admin)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.objects.Get(ctx, key); err != nil {
		t.Errorf("the object of archive's %s after the other holder went: %v", again.Name, err)
	}

	if src := w.source("archive"); src.NextSyncAt == nil || src.NextSyncAt.After(time.Now()) {
		t.Errorf("archive after an upload = %+v, want due", src)
	}
}

// renewals is a knowledge base that reports each lease renewal.
type renewals struct {
	ports.Knowledge

	renewed chan error
}

func (r *renewals) RenewSource(ctx context.Context, l domain.SourceLease, now, until time.Time) error {
	err := r.Knowledge.RenewSource(ctx, l, now, until)
	r.renewed <- err

	return err
}

// stalled is a knowledge base whose lease renewals hang until their sync ends: a goald stalled
// past its lease (a renewal stuck on a dead connection, a paused VM).
type stalled struct {
	ports.Knowledge

	renewing chan struct{}
}

func (s *stalled) RenewSource(ctx context.Context, _ domain.SourceLease, _, _ time.Time) error {
	s.renewing <- struct{}{}

	<-ctx.Done()

	return ctx.Err()
}

// Review 4 SQL M1: a goald that stalled past its lease while another took the source over and
// synced it records nothing when it wakes: no deletions and no end of its own over the newer
// sync's.
func TestIngestor_ATakenOverSyncRecordsNothing(t *testing.T) {
	t.Parallel()

	w := newWorld(t)

	if err := w.kb.DeleteSource(w.ctx(), "handbook", admin, nil); err != nil {
		t.Fatal(err)
	}

	docs := map[string]string{deployMD: shipOnTuesdays, slowMD: "# Slow\n\nA slow page."}
	w.docs.set(docs)

	release := make(chan struct{})
	slow := &reader{docs: docs, block: map[string]chan struct{}{slowMD: release}}
	kb := &stalled{Knowledge: w.kb, renewing: make(chan struct{}, 1)}
	first := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: kb, Secrets: w.secrets, Uploads: w.uploads, Readers: map[string]ports.SourceReader{gitdocsSync: slow},
		Objects: w.objects, Clock: w.clock,
	})
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = first.SyncDue(t.Context())
	}()

	// The first goald holds runbooks, waits on slow.md and its renewal hangs past the lease.
	if err := w.clock.WaitTickers(t.Context(), 1); err != nil {
		t.Fatal(err)
	}

	w.clock.Advance(knowledge.Lease + time.Second)
	<-kb.renewing

	if n := w.syncDue(w.ingestor(nil)); n != 1 {
		t.Fatalf("a second goald synced %d sources after the first's lease ended, want runbooks", n)
	}

	synced := w.source("runbooks")

	w.clock.Advance(time.Minute)
	close(release)
	<-done

	got := w.source("runbooks")
	w.want("runbooks after the first goald woke (the second's sync)", got, got.Status == domain.SyncIdle, len(w.files("runbooks")) == 2, len(snapshotsOf(w, "runbooks")) == 1,
		got.LastSyncedAt != nil && got.LastSyncedAt.Equal(*synced.LastSyncedAt), got.NextSyncAt.Equal(*synced.NextSyncAt))
}

// A goald killed mid-sync leaves the source leased: no one takes it while the lease, renewed as
// long as the sync ran, holds; once it ends, another goald syncs it from the start.
func TestIngestor_LeasesSurviveAKilledGoald(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	release := make(chan struct{})
	w.docs.block = map[string]chan struct{}{slowMD: release}
	w.docs.set(map[string]string{slowMD: "# Slow\n\nA slow page."})

	// The upload source is due too; sync it first, so only runbooks is left.
	if err := w.kb.DeleteSource(w.ctx(), "handbook", admin, nil); err != nil {
		t.Fatal(err)
	}

	killed, kill := context.WithCancel(t.Context())
	first := &renewals{Knowledge: w.kb, renewed: make(chan error, 1)}
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = w.ingestor(first).SyncDue(killed)
	}()

	// The first goald holds runbooks and renews its lease every third of it.
	if err := w.clock.WaitTickers(t.Context(), 1); err != nil {
		t.Fatal(err)
	}

	w.clock.Advance(knowledge.Lease/3 + time.Second)

	if err := <-first.renewed; err != nil {
		t.Fatalf("renewal = %v", err)
	}

	second := w.ingestor(nil)

	// Past the first lease, but the renewal holds it: nobody else claims it.
	w.clock.Advance(knowledge.Lease - knowledge.Lease/3)

	if err := <-first.renewed; err != nil {
		t.Fatalf("renewal = %v", err)
	}

	kill()
	<-done

	if n := w.syncDue(second); n != 0 {
		t.Fatalf("a second goald synced %d sources while the first's lease holds", n)
	}

	if src := w.source("runbooks"); src.Status != domain.SyncSyncing || len(snapshotsOf(w, "runbooks")) != 0 {
		t.Errorf("runbooks after the kill = %+v, want still syncing with nothing stored", src)
	}

	// The lease ends: the second goald takes the source over and syncs it.
	w.clock.Advance(knowledge.Lease)
	close(release)

	if n := w.syncDue(second); n != 1 {
		t.Fatalf("synced %d after the lease ended, want runbooks", n)
	}

	if src := w.source("runbooks"); src.Status != domain.SyncIdle || len(w.files("runbooks")) != 1 {
		t.Errorf("runbooks = %+v, want synced by the second goald", src)
	}
}

// A repository source on a secret is read by its kind's reader, with the secret opened at
// sync time: a value replaced since the source was made is the one read. A secret limited to some
// factories fails the source's sync, naming the secret.
func TestIngest_SyncOpensTheSecret(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	for _, n := range []domain.NewSecret{
		{Name: "other-token", Value: "test-token-1", Scope: domain.SecretScope{All: true}},
		{Name: "acme-only", Value: "test-token-2", Scope: domain.SecretScope{Factories: []domain.FactoryID{"0b6f1c9e-0000-7000-8000-000000000001"}}},
	} {
		if _, err := w.secrets.Create(ctx, n, admin); err != nil {
			t.Fatal(err)
		}
	}

	if err := w.kb.DeleteSource(ctx, "runbooks", admin, nil); err != nil {
		t.Fatal(err)
	}

	for _, n := range []domain.NewKnowledgeSource{
		{Name: "runbooks", Title: "Runbooks", Kind: domain.SourceRepository, Secret: "other-token", Config: map[string]string{"repository": "https://git.example.com/acme/docs"}},
		{Name: "scoped", Title: "Scoped", Kind: domain.SourceRepository, Secret: "acme-only", Config: map[string]string{"repository": "https://git.example.com/acme/docs"}},
	} {
		if _, err := w.kb.CreateSource(ctx, n, admin); err != nil {
			t.Fatal(err)
		}
	}

	replaced := "test-token-3"
	if _, err := w.secrets.Patch(ctx, "other-token", domain.SecretPatch{Replace: &replaced}, admin); err != nil {
		t.Fatal(err)
	}

	w.docs.set(map[string]string{deployMD: shipOnTuesdays})
	w.syncDue(w.ingestor(nil))

	w.want("the reader's secret (opened at sync time)", w.docs.secret == replaced, w.docs.secret == replaced)

	src := w.source("runbooks")
	w.want("runbooks after its sync", src, src.Status == domain.SyncIdle, len(w.files("runbooks")) == 1)

	scoped := w.source("scoped")
	w.want("the source on a scoped secret", scoped, scoped.Status == domain.SyncFailed, strings.Contains(scoped.LastError, "acme-only"))
}

// snapshotsOf are the source's snapshots.
func snapshotsOf(w *world, source string) []domain.KnowledgeSnapshot {
	w.t.Helper()

	s, err := w.kb.Snapshots(w.ctx(), source)
	if err != nil {
		w.t.Fatal(err)
	}

	return s
}
