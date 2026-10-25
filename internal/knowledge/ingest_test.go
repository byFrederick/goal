package knowledge_test

import (
	"context"
	"errors"
	"hash/fnv"
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/knowledge"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

var admin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}

// Names the tests use.
const (
	embedModel = "acme-embed-1"
	deployMD   = "docs/deploy.md"
	oncallMD   = "docs/oncall.md"
	newMD      = "docs/new.md"
	slowMD     = "docs/slow.md"
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

// embedder embeds a text as its bag of words, each word hashed onto one dimension, so texts
// sharing words are near. It counts its calls and fails while fail is set.
type embedder struct {
	model string
	calls atomic.Int64
	fail  atomic.Bool
}

func (e *embedder) Model() string { return e.model }

func (e *embedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls.Add(1)

	if e.fail.Load() {
		return nil, errors.New("the embedding model is down")
	}

	out := make([][]float32, len(texts))

	for i, t := range texts {
		v := make([]float32, domain.EmbeddingDimensions)

		for w := range strings.FieldsSeq(strings.ToLower(t)) {
			h := fnv.New32a()
			_, _ = h.Write([]byte(w))
			v[h.Sum32()%domain.EmbeddingDimensions]++
		}

		out[i] = v
	}

	return out, nil
}

// reader is a synced source's documents in memory: path → content, versioned by content. It
// records the connection it was given and every fetch; a fetch of a path in block waits until
// the test closes the channel.
type reader struct {
	mu      sync.Mutex
	docs    map[string]string
	fetched []string
	conn    *domain.ResolvedConnection
	block   map[string]chan struct{}
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
	r.conn = s.Connection
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
	wait := r.block[id]
	r.mu.Unlock()

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

	return domain.SourceDoc{SourceItem: domain.SourceItem{ID: id}, MIME: knowledge.TypeMarkdown, Body: []byte(body)}, nil
}

// world is one organization's knowledge base on the port fakes, with a synced source "runbooks"
// read by docs and an upload source "handbook".
type world struct {
	t       *testing.T
	org     domain.OrgID
	tenancy *porttest.Tenancy
	conns   *porttest.Connections
	kb      ports.Knowledge
	objects *porttest.ObjectStore
	uploads *knowledge.Uploads
	clock   *porttest.Clock
	docs    *reader
	embed   *embedder
}

func newWorld(t *testing.T) *world {
	t.Helper()

	// The clock starts a second on, so the sources created now are due.
	w := &world{
		t: t, clock: porttest.NewClock(time.Now().UTC().Add(time.Second)), objects: porttest.NewObjectStore(), docs: &reader{},
		embed: &embedder{model: embedModel},
	}
	w.tenancy = porttest.NewTenancy(w.clock.Now)
	w.conns = porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), w.tenancy)
	w.kb = porttest.NewKnowledge(w.conns, w.tenancy)
	w.uploads = knowledge.NewUploads(w.kb, w.objects, 0)

	o, err := w.tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	w.org = o.ID
	ctx := w.ctx()

	if err := w.conns.SyncPlatformKinds(t.Context(), []domain.ConnectionKind{{Name: "gitdocs-kind", Title: "Docs", Platform: true, Spec: domain.KindSpec{
		Fields: []domain.KindField{{Name: "token", Title: "Token", Secret: true, Required: true}}, Sync: "gitdocs",
	}}}); err != nil {
		t.Fatal(err)
	}

	if _, err := w.conns.Create(ctx, domain.NewConnection{Name: "docs", Kind: "gitdocs-kind", Secrets: map[string]string{"token": "s3cret"}}, admin); err != nil {
		t.Fatal(err)
	}

	for _, n := range []domain.NewKnowledgeSource{
		{Name: "runbooks", Title: "Runbooks", Kind: domain.SourceSynced, Connection: "docs"},
		{Name: "handbook", Title: "Handbook", Kind: domain.SourceUpload},
	} {
		if _, err := w.kb.CreateSource(ctx, n, admin); err != nil {
			t.Fatal(err)
		}
	}

	return w
}

func (w *world) ctx() context.Context { return domain.WithOrg(w.t.Context(), w.org) }

// ingestor syncs with the embedder, or keywords only when it is nil, and the kb given (w.kb by
// default).
func (w *world) ingestor(e ports.Embedder, kb ports.Knowledge) *knowledge.Ingestor {
	if kb == nil {
		kb = w.kb
	}

	return knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: kb, Connections: w.conns, Uploads: w.uploads, Readers: map[string]ports.SourceReader{"gitdocs": w.docs},
		Embedder: e, Clock: w.clock,
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

func (w *world) documents(source string) map[string]domain.KnowledgeDocument {
	w.t.Helper()

	docs, err := w.kb.Documents(w.ctx(), source, "", 100)
	if err != nil {
		w.t.Fatal(err)
	}

	out := map[string]domain.KnowledgeDocument{}
	for _, d := range docs {
		out[d.Title] = d
	}

	return out
}

func (w *world) search(e ports.Embedder, text string) []string {
	w.t.Helper()

	hits, err := knowledge.NewSearcher(w.kb, e, nil).Search(w.ctx(), text, []string{"runbooks", "handbook"}, 5)
	if err != nil {
		w.t.Fatal(err)
	}

	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Title)
	}

	return out
}

func TestIngestor_SyncsIncrementallyAndDeletesWhatGoes(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	deploy, oncall := "# Deploy\n\nShip with the release train on Tuesdays.", "# On call\n\nPage the primary, then the secondary."
	w.docs.set(map[string]string{deployMD: deploy, oncallMD: oncall})

	in := w.ingestor(w.embed, nil)
	n := w.syncDue(in)
	w.want("sources synced (both new ones)", n, n == 2)

	fetched := w.docs.fetches()
	w.want("fetched", fetched, slices.Equal(fetched, []string{deployMD, oncallMD}))
	w.want("the reader's connection (docs opened)", w.docs.conn, w.docs.conn != nil && w.docs.conn.Values["token"] == "s3cret")

	src := w.source("runbooks")
	w.want("runbooks after its sync", src, src.Status == domain.SyncIdle, src.EmbedModel == embedModel, src.Documents == 2, src.LeaseUntil == nil)

	d := w.documents("runbooks")[deployMD]
	w.want(deployMD, d, d.Status == domain.DocumentIndexed, d.EmbedModel == embedModel, d.URL == "https://git.example.com/"+deployMD, d.IndexedAt != nil)

	hits := w.search(w.embed, "when do we ship a release")
	w.want("search (deploy.md first)", hits, len(hits) > 0 && hits[0] == deployMD)

	// An hour on: oncall.md changed, deploy.md did not, a page is new and none went yet.
	oncall = "# On call\n\nPage the primary only."
	w.docs.set(map[string]string{deployMD: deploy, oncallMD: oncall, newMD: "# New\n\nA new page."})
	w.clock.Advance(time.Hour + time.Minute)

	n = w.syncDue(in)
	w.want("sources synced an hour on (both due again)", n, n == 2)

	fetched = w.docs.fetches()
	w.want("fetched (only what changed)", fetched, slices.Equal(fetched, []string{newMD, oncallMD}))

	// Then deploy.md goes from the repository.
	w.docs.set(map[string]string{oncallMD: oncall, newMD: "# New\n\nA new page."})
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(in)

	docs := slices.Sorted(maps.Keys(w.documents("runbooks")))
	w.want("documents (deploy.md gone)", docs, slices.Equal(docs, []string{newMD, oncallMD}))

	hits = w.search(nil, "release train")
	w.want("search after deploy.md went", hits, len(hits) == 0)
}

func TestIngestor_KeywordsOnlyWithoutAnEmbedder(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.docs.set(map[string]string{deployMD: "# Deploy\n\nShip with the release train."})

	w.syncDue(w.ingestor(nil, nil))

	if src := w.source("runbooks"); src.Status != domain.SyncIdle || src.EmbedModel != "" {
		t.Errorf("runbooks = %+v, want synced without a model", src)
	}

	if got := w.search(nil, "train"); !slices.Equal(got, []string{deployMD}) {
		t.Errorf("keyword search = %v", got)
	}

	// An embedder that fails at search time leaves the keywords.
	w.embed.fail.Store(true)

	if got := w.search(w.embed, "train"); !slices.Equal(got, []string{deployMD}) {
		t.Errorf("search with a failing embedder = %v, want the keyword hit", got)
	}

	// Once there is a model, every source is due for a full sync, changed or not.
	w.embed.fail.Store(false)

	if n := w.syncDue(w.ingestor(w.embed, nil)); n != 2 {
		t.Fatalf("synced %d with a new model, want both sources", n)
	}

	if got := w.docs.fetches(); !slices.Equal(got, []string{deployMD, deployMD}) {
		t.Errorf("fetched %v, want deploy.md again", got)
	}

	if src := w.source("runbooks"); src.EmbedModel != embedModel {
		t.Errorf("runbooks model = %q", src.EmbedModel)
	}
}

func TestIngestor_FailuresAreRecorded(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.docs.set(map[string]string{"docs/good.md": "# Good\n\nfine", "docs/bad.md": "caf\xe9"})

	w.embed.fail.Store(true)
	w.syncDue(w.ingestor(w.embed, nil))

	if src := w.source("runbooks"); src.Status != domain.SyncFailed || !strings.Contains(src.LastError, "the embedding model is down") {
		t.Errorf("runbooks with the model down = %+v, want failed with the reason", src)
	}

	if _, ok := w.documents("runbooks")["docs/good.md"]; ok {
		t.Error("good.md was stored without its embeddings")
	}

	w.embed.fail.Store(false)
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(w.ingestor(w.embed, nil))

	docs := w.documents("runbooks")
	if src := w.source("runbooks"); src.Status != domain.SyncIdle || docs["docs/good.md"].Status != domain.DocumentIndexed {
		t.Errorf("runbooks = %+v, good.md %+v", src, docs["docs/good.md"])
	}

	if bad := docs["docs/bad.md"]; bad.Status != domain.DocumentFailed || !strings.Contains(bad.Error, "not UTF-8") {
		t.Errorf("bad.md = %+v, want failed as not text", bad)
	}
}

func TestIngestor_UnknownSyncKindFailsTheSource(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	in := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: w.kb, Connections: w.conns, Uploads: w.uploads, Clock: w.clock,
	})
	w.syncDue(in)

	if src := w.source("runbooks"); src.Status != domain.SyncFailed || !strings.Contains(src.LastError, `"gitdocs"`) {
		t.Errorf("runbooks without a gitdocs reader = %+v", src)
	}
}

func TestIngestor_UploadsIndexedFromTheBucket(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	d, err := w.uploads.Upload(ctx, "handbook", "leave.md", []byte("# Leave\n\nAsk your lead two weeks ahead."), admin)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.uploads.Upload(ctx, "handbook", "page.html", []byte("<h1>Expenses</h1><p>Receipts within 30 days.</p>"), admin); err != nil {
		t.Fatal(err)
	}

	w.syncDue(w.ingestor(w.embed, nil))

	docs := w.documents("handbook")
	if got := docs["leave.md"]; got.ID != d.ID || got.Status != domain.DocumentIndexed || got.Blob != d.Blob || got.MIME != knowledge.TypeMarkdown {
		t.Errorf("leave.md after the sync = %+v", got)
	}

	if got := w.search(w.embed, "receipts"); len(got) == 0 || got[0] != "page.html" {
		t.Errorf("search = %v, want the HTML upload", got)
	}

	_, text, err := w.kb.Document(ctx, d.ID)
	if err != nil || text != "# Leave\n\nAsk your lead two weeks ahead." {
		t.Errorf("Document text = %q, %v", text, err)
	}

	// Unchanged, an upload is never fetched or embedded again.
	calls := w.embed.calls.Load()
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(w.ingestor(w.embed, nil))

	if w.embed.calls.Load() != calls {
		t.Errorf("an unchanged upload was embedded again")
	}
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

	a, err := w.uploads.Upload(ctx, "handbook", "policy.md", body, admin)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.uploads.Upload(ctx, "archive", "policy-copy.md", body, admin); err != nil {
		t.Fatal(err)
	}

	if n := w.objects.Writes(key); n != 1 {
		t.Errorf("the content was written %d times, want once", n)
	}

	if err := w.uploads.DeleteDocument(ctx, a.ID, admin); err != nil {
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

	_, err = w.uploads.Upload(ctx, "handbook", "slides.pptx", []byte("PK"), admin)
	if !errors.Is(err, domain.ErrUnsupportedDocument) {
		t.Errorf("Upload of slides = %v, want unsupported", err)
	}

	small := knowledge.NewUploads(w.kb, w.objects, 4)
	if _, err := small.Upload(ctx, "handbook", "big.md", []byte("12345"), admin); !errors.Is(err, ports.ErrTooLarge) {
		t.Errorf("Upload over the limit = %v, want too large", err)
	}

	other := []byte("# Other")
	if _, err := w.uploads.Upload(ctx, "runbooks", "x.md", other, admin); !errors.Is(err, domain.ErrNotUploadSource) {
		t.Errorf("Upload to a synced source = %v", err)
	}

	if n := w.objects.Writes(domain.KnowledgeKey(w.org, domain.HashBlob(other))); n != 0 {
		t.Errorf("a refused upload wrote its content %d times, want none", n)
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

	if _, err := w.kb.DeleteSource(w.ctx(), "handbook", admin); err != nil {
		t.Fatal(err)
	}

	docs := map[string]string{deployMD: "# Deploy\n\nShip on Tuesdays.", slowMD: "# Slow\n\nA slow page."}
	w.docs.set(docs)

	release := make(chan struct{})
	slow := &reader{docs: docs, block: map[string]chan struct{}{slowMD: release}}
	kb := &stalled{Knowledge: w.kb, renewing: make(chan struct{}, 1)}
	first := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: kb, Connections: w.conns, Uploads: w.uploads, Readers: map[string]ports.SourceReader{"gitdocs": slow},
		Embedder: w.embed, Clock: w.clock,
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

	if n := w.syncDue(w.ingestor(w.embed, nil)); n != 1 {
		t.Fatalf("a second goald synced %d sources after the first's lease ended, want runbooks", n)
	}

	synced := w.source("runbooks")

	w.clock.Advance(time.Minute)
	close(release)
	<-done

	got := w.source("runbooks")
	w.want("runbooks after the first goald woke (the second's sync)", got, got.Status == domain.SyncIdle, got.Documents == 2,
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
	if _, err := w.kb.DeleteSource(w.ctx(), "handbook", admin); err != nil {
		t.Fatal(err)
	}

	killed, kill := context.WithCancel(t.Context())
	first := &renewals{Knowledge: w.kb, renewed: make(chan error, 1)}
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = w.ingestor(w.embed, first).SyncDue(killed)
	}()

	// The first goald holds runbooks and renews its lease every third of it.
	if err := w.clock.WaitTickers(t.Context(), 1); err != nil {
		t.Fatal(err)
	}

	w.clock.Advance(knowledge.Lease/3 + time.Second)

	if err := <-first.renewed; err != nil {
		t.Fatalf("renewal = %v", err)
	}

	second := w.ingestor(w.embed, nil)

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

	if src := w.source("runbooks"); src.Status != domain.SyncSyncing || src.Documents != 0 {
		t.Errorf("runbooks after the kill = %+v, want still syncing with nothing stored", src)
	}

	// The lease ends: the second goald takes the source over and syncs it.
	w.clock.Advance(knowledge.Lease)
	close(release)

	if n := w.syncDue(second); n != 1 {
		t.Fatalf("synced %d after the lease ended, want runbooks", n)
	}

	if src := w.source("runbooks"); src.Status != domain.SyncIdle || src.Documents != 1 {
		t.Errorf("runbooks = %+v, want synced by the second goald", src)
	}
}
