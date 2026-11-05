package knowledge_test

import (
	"cmp"
	"context"
	"errors"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/knowledge"
)

var admin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}

// repoConfig is the config of the repository sources the tests make.
var repoConfig = map[string]string{domain.SourceRepositoryKey: "https://git.example.com/acme/docs"}

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
	// lists counts its listings; codeHost is the code_host config of the last.
	lists    int
	codeHost string
	block   map[string]chan struct{}
	down    bool // every fetch fails, as a server answering 503
	// titles and folders, by path, list a document under another title and in a folder.
	titles, folders map[string]string
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

	r.lists++
	r.codeHost = s.Source.Config[domain.SourceCodeHostKey]

	docs, titles, folders := maps.Clone(r.docs), maps.Clone(r.titles), maps.Clone(r.folders)
	r.mu.Unlock()

	return func(yield func(domain.SourceItem, error) bool) {
		for _, p := range slices.Sorted(maps.Keys(docs)) {
			item := domain.SourceItem{ID: p, Version: string(domain.HashBlob([]byte(docs[p]))), Title: cmp.Or(titles[p], p), Folder: folders[p], URL: "https://git.example.com/" + p}
			if !yield(item, nil) {
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

// archiving is a reader that also reads an archive (ports.ArchiveReader). An archive yields
// every asked item whose content is still the version listed, except those in skip. With
// breakAfter above 0 it fails as a whole after yielding that many.
type archiving struct {
	*reader

	archives   int
	skip       map[string]bool
	breakAfter int
}

func (a *archiving) Archive(_ context.Context, _ domain.SyncSpec, items []domain.SourceItem) iter.Seq2[domain.SourceDoc, error] {
	a.mu.Lock()
	a.archives++
	docs := maps.Clone(a.docs)
	a.mu.Unlock()

	return func(yield func(domain.SourceDoc, error) bool) {
		n := 0

		for _, item := range items {
			body, ok := docs[item.ID]
			if !ok || a.skip[item.ID] || string(domain.HashBlob([]byte(body))) != item.Version {
				continue
			}

			if a.breakAfter > 0 && n == a.breakAfter {
				yield(domain.SourceDoc{}, errors.New("the archive is cut short"))

				return
			}

			n++

			if !yield(domain.SourceDoc{SourceItem: domain.SourceItem{ID: item.ID, MIME: domain.MediaMarkdown}, Body: []byte(body)}, nil) {
				return
			}
		}
	}
}

func (a *archiving) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.archives
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

	// The repository sources' host is a GitHub Enterprise host acme lists.
	if _, err := w.tenancy.SetCodeHosts(ctx, domain.LocalUser, []domain.CodeHost{{Host: "git.example.com", Kind: domain.CodeHostGitHub}}); err != nil {
		t.Fatal(err)
	}

	if _, err := w.secrets.Create(ctx, domain.NewSecret{Name: "docs-token", Value: "s3cret", Scope: domain.SecretScope{All: true}}, admin); err != nil {
		t.Fatal(err)
	}

	for _, n := range []domain.NewKnowledgeSource{
		{Name: "runbooks", Title: "Runbooks", Kind: domain.SourceRepository, Secret: "docs-token", Config: repoConfig},
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

	return w.ingestorReading(kb, w.docs)
}

// ingestorReading syncs kb's repository and Confluence sources with r.
func (w *world) ingestorReading(kb ports.Knowledge, r ports.SourceReader) *knowledge.Ingestor {
	return knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: kb, Secrets: w.secrets, Hosts: w.tenancy, Uploads: w.uploads, Readers: map[string]ports.SourceReader{gitdocsSync: r, "confluence": r},
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
}

// Uploads the knowledge base refuses: a type it does not take, a file over the limit, a source of
// another kind. A refused upload writes nothing to the bucket.
func TestUploads_RefusalsWriteNothing(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

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
		Knowledge: kb, Secrets: w.secrets, Hosts: w.tenancy, Uploads: w.uploads, Readers: map[string]ports.SourceReader{gitdocsSync: slow},
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
		{Name: "runbooks", Title: "Runbooks", Kind: domain.SourceRepository, Secret: "other-token", Config: repoConfig},
		{Name: "scoped", Title: "Scoped", Kind: domain.SourceRepository, Secret: "acme-only", Config: repoConfig},
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

// failingObjects is the bucket whose deletions fail while fail is set.
type failingObjects struct {
	*porttest.ObjectStore

	fail atomic.Bool
}

var errBucketDown = errors.New("the bucket is unavailable")

func (o *failingObjects) Delete(ctx context.Context, key string) error {
	if o.fail.Load() {
		return errBucketDown
	}

	return o.ObjectStore.Delete(ctx, key)
}

func (o *failingObjects) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if o.fail.Load() {
		return 0, errBucketDown
	}

	return o.ObjectStore.DeletePrefix(ctx, prefix)
}

// snapshotIDs are the source's snapshots, newest first.
func (w *world) snapshotIDs(source string) []domain.KnowledgeSnapshotID {
	w.t.Helper()

	snaps, err := w.kb.Snapshots(w.ctx(), source)
	if err != nil {
		w.t.Fatal(err)
	}

	out := make([]domain.KnowledgeSnapshotID, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, s.ID)
	}

	return out
}

// hasZip reports whether the bucket holds the zip of the source's snapshot id.
func (w *world) hasZip(src domain.KnowledgeSource, id domain.KnowledgeSnapshotID) bool {
	_, err := w.objects.Get(w.ctx(), domain.KnowledgeSnapshotKey(w.org, src.ID, id))

	return err == nil
}

// A sync deletes the snapshots retention no longer keeps, the zip before the row; it keeps the
// current one, one a running run pinned and one replaced inside the window, and a deletion that
// fails leaves the row for the next sync without failing this one.
func TestIngestor_RetentionDeletesTheZipBeforeTheRow(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()
	objs := &failingObjects{ObjectStore: w.objects}
	in := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: w.kb, Uploads: w.uploads, Objects: objs, Clock: w.clock, TempDir: t.TempDir(), Keep: 2 * time.Hour,
	})

	// Four versions of the handbook: old at 0, pinned at 1h, young at 2h, current at 7h. With a
	// window of 2h at 7h, old (replaced at 1h) and pinned (replaced at 2h) are past it, young
	// (replaced at 7h) is not.
	fake, ok := w.kb.(*porttest.Knowledge)
	if !ok {
		t.Fatalf("the knowledge base is a %T", w.kb)
	}

	var ids []domain.KnowledgeSnapshotID

	for i, wait := range []time.Duration{0, time.Hour, time.Hour, 5 * time.Hour} {
		w.clock.Advance(wait)

		if i == 3 {
			fake.Pin(w.org, ids[1])
			objs.fail.Store(true)
		}

		if _, err := w.uploads.Upload(ctx, "handbook", "leave.md", []byte("# Leave\n\nVersion "+strconv.Itoa(i)), admin); err != nil {
			t.Fatal(err)
		}

		w.syncDue(in)
		ids = append(ids, w.source("handbook").CurrentSnapshot)
	}

	src := w.source("handbook")
	w.want("the source after a sync whose retention failed", src, src.Status == domain.SyncIdle, src.LastError == "", src.SyncFailures == 0)

	got := w.snapshotIDs("handbook")
	w.want("the snapshots after the zip's deletion failed", got, slices.Equal(got, []domain.KnowledgeSnapshotID{ids[3], ids[2], ids[1], ids[0]}))
	w.want("the old snapshot's zip after its deletion failed", nil, w.hasZip(src, ids[0]))

	// The next sync deletes it, zip and row.
	objs.fail.Store(false)
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(in)

	got = w.snapshotIDs("handbook")
	w.want("the snapshots after retention", got, slices.Equal(got, []domain.KnowledgeSnapshotID{ids[3], ids[2], ids[1]}))
	w.want("the old snapshot's zip after retention", nil, !w.hasZip(src, ids[0]))

	for _, id := range ids[1:] {
		w.want("the zip of a snapshot retention keeps", id, w.hasZip(src, id))
	}
}

// Deleting a source deletes its snapshots' zips first, and only its own: another source's zips
// and the uploads' originals another source still holds stay. When the bucket fails, the source
// stays.
func TestUploads_DeleteSourceDeletesItsSnapshotZips(t *testing.T) {
	t.Parallel()

	const manuals = "manuals"

	w := newWorld(t)
	ctx := w.ctx()

	if _, err := w.kb.CreateSource(ctx, domain.NewKnowledgeSource{Name: manuals, Title: "Manuals", Kind: domain.SourceUpload}, admin); err != nil {
		t.Fatal(err)
	}

	body := []byte("# Policy\n\nBe kind.")

	for _, source := range []string{"handbook", manuals} {
		if _, err := w.uploads.Upload(ctx, source, "policy.md", body, admin); err != nil {
			t.Fatal(err)
		}
	}

	w.syncDue(w.ingestor(nil))

	handbook, other := w.source("handbook"), w.source(manuals)
	w.want("the zips after the sync", nil, w.hasZip(handbook, handbook.CurrentSnapshot), w.hasZip(other, other.CurrentSnapshot))

	objs := &failingObjects{ObjectStore: w.objects}
	uploads := knowledge.NewUploads(w.kb, objs, 0)

	objs.fail.Store(true)

	if err := uploads.DeleteSource(ctx, "handbook", admin); !errors.Is(err, errBucketDown) {
		t.Errorf("DeleteSource with the bucket down = %v, want %v", err, errBucketDown)
	}

	w.source("handbook")

	objs.fail.Store(false)

	if err := uploads.DeleteSource(ctx, "handbook", admin); err != nil {
		t.Fatal(err)
	}

	if _, err := w.kb.Source(ctx, "handbook"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Source after its deletion = %v, want %v", err, ports.ErrNotFound)
	}

	left, err := w.objects.List(ctx, domain.KnowledgeSourcePrefix(w.org, handbook.ID))
	if err != nil {
		t.Fatal(err)
	}

	w.want("the deleted source's zips", left, len(left) == 0)
	w.want("the other source's zip", nil, w.hasZip(other, other.CurrentSnapshot))

	if _, err := w.objects.Get(ctx, domain.KnowledgeKey(w.org, domain.HashBlob(body))); err != nil {
		t.Errorf("the original the other source still holds: %v", err)
	}
}

// countedSecrets counts the secrets it opens.
type countedSecrets struct {
	knowledge.Secrets

	resolves atomic.Int32
}

func (c *countedSecrets) Resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error) {
	c.resolves.Add(1)

	return c.Secrets.Resolve(ctx, names, use)
}

// listings are how many listings r made and the code_host config of the last.
func (r *reader) listings() (int, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.lists, r.codeHost
}

// replaceRunbooks replaces runbooks with sources, each on docs-token, reading repository.
func (w *world) replaceRunbooks(repository string, sources ...domain.NewKnowledgeSource) {
	w.t.Helper()

	ctx := w.ctx()

	if err := w.kb.DeleteSource(ctx, "runbooks", admin, nil); err != nil {
		w.t.Fatal(err)
	}

	for _, n := range sources {
		n.Secret, n.Config = "docs-token", maps.Clone(n.Config)
		if n.Config == nil {
			n.Config = map[string]string{}
		}

		n.Config[domain.SourceRepositoryKey] = repository

		if _, err := w.kb.CreateSource(ctx, n, admin); err != nil {
			w.t.Fatal(err)
		}
	}
}

// A repository source, and a synced source gitdocs reads with a secret, on a host acme doesn't
// list fail their sync naming the host and Code hosts, with the secret never opened and the
// reader never called; that another organization lists the host changes nothing.
func TestIngest_UnlistedHostOpensNoSecret(t *testing.T) {
	t.Parallel()

	w := newWorld(t)

	labs, err := w.tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "labs", Name: "Example Labs"})
	if err != nil {
		t.Fatal(err)
	}

	const unlisted = "docs.example.com"

	if _, err := w.tenancy.SetCodeHosts(domain.WithOrg(t.Context(), labs.ID), domain.LocalUser,
		[]domain.CodeHost{{Host: unlisted, Kind: domain.CodeHostGitHub}}); err != nil {
		t.Fatal(err)
	}

	w.replaceRunbooks("https://"+unlisted+"/acme/docs",
		domain.NewKnowledgeSource{Name: "runbooks", Title: "Runbooks", Kind: domain.SourceRepository,
			Config: map[string]string{domain.SourceCodeHostKey: "github"}},
		domain.NewKnowledgeSource{Name: "wiki", Title: "Wiki", Kind: domain.SourceSynced,
			Config: map[string]string{domain.SourceReaderKey: gitdocsSync, domain.SourceCodeHostKey: "github"}},
	)

	secrets := &countedSecrets{Secrets: w.secrets}
	w.docs.set(map[string]string{deployMD: shipOnTuesdays})
	w.syncDue(knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: w.kb, Secrets: secrets, Hosts: w.tenancy, Uploads: w.uploads, Readers: map[string]ports.SourceReader{gitdocsSync: w.docs},
		Objects: w.objects, Clock: w.clock, TempDir: t.TempDir(),
	}))

	for _, name := range []string{"runbooks", "wiki"} {
		src := w.source(name)
		w.want(name+" on an unlisted host", src, src.Status == domain.SyncFailed,
			strings.Contains(src.LastError, unlisted), strings.Contains(src.LastError, "Code hosts"))
	}

	lists, _ := w.docs.listings()
	w.want("secrets opened and listings made", []int{int(secrets.resolves.Load()), lists}, secrets.resolves.Load() == 0, lists == 0)
}

// A repository source on a GitHub Enterprise host acme lists as github opens its secret and
// syncs, read as github even where its config says gitlab: the listed kind wins.
func TestIngest_ListedEnterpriseHostSyncs(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.replaceRunbooks("https://GIT.example.com/acme/docs",
		domain.NewKnowledgeSource{Name: "runbooks", Title: "Runbooks", Kind: domain.SourceRepository,
			Config: map[string]string{domain.SourceCodeHostKey: "gitlab"}})

	w.docs.set(map[string]string{deployMD: shipOnTuesdays})
	w.syncDue(w.ingestor(nil))

	src := w.source("runbooks")
	w.want("runbooks on a listed host", src, src.Status == domain.SyncIdle, len(w.files("runbooks")) == 1)

	lists, kind := w.docs.listings()
	w.want("the reader's secret and code host", []any{w.docs.secret, lists, kind}, w.docs.secret == "s3cret", lists == 1, kind == "github")
}
