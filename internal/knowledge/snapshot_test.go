package knowledge_test

import (
	"archive/zip"
	"bytes"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/knowledge"
)

const repoSource = "repo"

// repoWorld is a world with only a repository source, "repo", read by docs: the synced source
// is deleted, so every fetch is the repository's.
func repoWorld(t *testing.T) *world {
	t.Helper()

	w := newWorld(t)
	ctx := w.ctx()

	if err := w.kb.DeleteSource(ctx, "runbooks", admin, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := w.kb.CreateSource(ctx, domain.NewKnowledgeSource{
		Name: repoSource, Title: "Repo", Kind: domain.SourceRepository, Config: repoConfig,
	}, admin); err != nil {
		t.Fatal(err)
	}

	return w
}

// snapshots are the source's snapshots, newest first.
func (w *world) snapshots(source string) []domain.KnowledgeSnapshot {
	w.t.Helper()

	s, err := w.kb.Snapshots(w.ctx(), source)
	if err != nil {
		w.t.Fatal(err)
	}

	return s
}

// entry is one file of a snapshot's zip: its Markdown and its compressed bytes as stored.
type entry struct {
	text string
	raw  []byte
	crc  uint32
}

// zipOf reads snapshot s's zip from the bucket.
func (w *world) zipOf(s domain.KnowledgeSnapshot) map[string]entry {
	w.t.Helper()

	obj, err := w.objects.Get(w.ctx(), s.Key)
	if err != nil {
		w.t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(obj.Body), int64(len(obj.Body)))
	if err != nil {
		w.t.Fatal(err)
	}

	out := map[string]entry{}

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			w.t.Fatal(err)
		}

		text, _ := io.ReadAll(rc)
		_ = rc.Close()

		r, err := f.OpenRaw()
		if err != nil {
			w.t.Fatal(err)
		}

		raw, _ := io.ReadAll(r)
		out[f.Name] = entry{text: string(text), raw: raw, crc: f.CRC32}
	}

	return out
}

// objectsUnder counts the bucket's objects under the source's snapshot prefix.
func (w *world) objectsUnder(source string) int {
	w.t.Helper()

	got, err := w.objects.List(w.ctx(), domain.OrgPrefix(w.org)+"knowledge/"+string(w.source(source).ID)+"/")
	if err != nil {
		w.t.Fatal(err)
	}

	return len(got)
}

// A repository source's sync writes one zip whose every file starts with its source line. The
// next sync fetches only what changed and copies the other entries from the previous zip as they
// were stored, compressed bytes and all.
func TestSnapshot_CopiesUnchangedEntriesRaw(t *testing.T) {
	t.Parallel()

	w := repoWorld(t)
	deploy := "# Deploy\n\n" + strings.Repeat("Ship with the release train on Tuesdays. ", 50)
	w.docs.set(map[string]string{deployMD: deploy, oncallMD: "# On call\n\nPage the primary."})

	in := w.ingestor(nil)
	w.syncDue(in)

	snaps := w.snapshots(repoSource)
	if len(snaps) != 1 {
		t.Fatalf("snapshots after the first sync = %+v, want one", snaps)
	}

	first := snaps[0]
	w.want("the first snapshot", first, first.Files == 2, first.Changes.Added == 2, first.Failed == 0, first.ZipBytes > 0,
		w.source(repoSource).CurrentSnapshot == first.ID)

	files := w.zipOf(first)
	w.want(deployMD, files[deployMD].text,
		files[deployMD].text == "---\nsource: https://git.example.com/"+deployMD+"\n---\n\n"+deploy)
	_ = w.docs.fetches()

	w.docs.set(map[string]string{deployMD: deploy, oncallMD: "# On call\n\nPage the primary, then the secondary."})
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(in)

	fetched := w.docs.fetches()
	w.want("fetched (only what changed)", fetched, slices.Equal(fetched, []string{oncallMD}))

	snaps = w.snapshots(repoSource)
	if len(snaps) != 2 {
		t.Fatalf("snapshots after the second sync = %d, want two", len(snaps))
	}

	second := snaps[0]
	w.want("the second snapshot's changes", second.Changes, second.Changes.Changed == 1, second.Changes.Added == 0, second.Changes.Removed == 0)

	again := w.zipOf(second)
	w.want("deploy.md copied raw", again[deployMD].crc,
		bytes.Equal(again[deployMD].raw, files[deployMD].raw), again[deployMD].crc == files[deployMD].crc, again[deployMD].text == files[deployMD].text)
	w.want("oncall.md rewritten", again[oncallMD].text, strings.HasSuffix(again[oncallMD].text, "then the secondary."))

	// deploy.md goes: one removed.
	w.docs.set(map[string]string{oncallMD: "# On call\n\nPage the primary, then the secondary."})
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(in)

	third := w.snapshots(repoSource)[0]
	w.want("the third snapshot", third, third.Files == 1, third.Changes.Removed == 1)
}

// A sync where nothing was added, changed or removed writes no snapshot and nothing to the
// bucket.
func TestSnapshot_NothingChangedWritesNoSnapshot(t *testing.T) {
	t.Parallel()

	w := repoWorld(t)
	w.docs.set(map[string]string{deployMD: shipOnTuesdays})

	in := w.ingestor(nil)
	w.syncDue(in)
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(in)

	src := w.source(repoSource)
	w.want("the source after two syncs", src, src.Status == domain.SyncIdle, len(w.snapshots(repoSource)) == 1, w.objectsUnder(repoSource) == 1)
}

// A file that fails to convert keeps its last good version, one that never had one stays out,
// both are listed, and the sync goes through.
func TestSnapshot_AFailedFileKeepsItsLastGoodVersion(t *testing.T) {
	t.Parallel()

	w := repoWorld(t)
	w.docs.set(map[string]string{deployMD: shipOnTuesdays})

	in := w.ingestor(nil)
	w.syncDue(in)

	before := w.zipOf(w.snapshots(repoSource)[0])

	w.docs.set(map[string]string{deployMD: "caf\xe9", "docs/bad.md": "caf\xe9", newMD: newPage})
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(in)

	src := w.source(repoSource)
	w.want("the source", src, src.Status == domain.SyncIdle, src.LastError == "")

	snap := w.snapshots(repoSource)[0]
	failed := map[string]domain.FileFailure{}

	for _, f := range snap.Changes.Failed {
		failed[f.Path] = f
	}

	w.want("the snapshot", snap, snap.Files == 2, snap.Failed == 2, snap.Changes.Added == 1, snap.Changes.Changed == 0,
		strings.Contains(failed[deployMD].Error, "not UTF-8"), strings.Contains(failed["docs/bad.md"].Error, "not UTF-8"))

	after := w.zipOf(snap)
	_, bad := after["docs/bad.md"]
	w.want("the zip", after, after[deployMD].text == before[deployMD].text, !bad, after[newMD].text != "")
}

// A file that looks like it holds a key stays out and is flagged.
func TestSnapshot_AKeyLikeFileIsLeftOut(t *testing.T) {
	t.Parallel()

	w := repoWorld(t)
	leaky := "docs/leaky.md"
	w.docs.set(map[string]string{
		deployMD: "# Deploy\n\nA risk-free release.",
		leaky:    "# Setup\n\nexport GH_TOKEN=ghp_" + strings.Repeat("0", 36) + "\n",
	})

	w.syncDue(w.ingestor(nil))

	snap := w.snapshots(repoSource)[0]
	w.want("the snapshot", snap, snap.Files == 1, snap.Failed == 1, len(snap.Changes.Failed) == 1,
		snap.Changes.Failed[0].Path == leaky, snap.Changes.Failed[0].Key)

	_, in := w.zipOf(snap)[leaky]
	w.want("leaky.md in the zip", in, !in)
}

// A sync past a source's limits fails, naming the limit, and writes nothing.
func TestSnapshot_PastALimitFailsNamingIt(t *testing.T) {
	t.Parallel()

	many := func() map[string]string {
		docs := make(map[string]string, domain.MaxSourceFiles+1)
		for i := range domain.MaxSourceFiles + 1 {
			docs["docs/"+strconv.Itoa(i)+".md"] = "x"
		}

		return docs
	}

	big := func() map[string]string {
		docs := map[string]string{}
		for i := range 3 {
			docs["docs/"+strconv.Itoa(i)+".md"] = strings.Repeat(string(rune('a'+i)), 400<<10)
		}

		return docs
	}

	// The byte limits are 1 MB here, as compressing 50 MB under the race detector takes a minute;
	// the file count is the real one.
	const mb = 1 << 20

	for _, tc := range []struct {
		name         string
		docs         func() map[string]string
		source, file int64
		says         string
	}{
		{"files", many, domain.MaxSourceFiles, domain.MaxFileBytes, "over the limit of 20000 files"},
		{"bytes", big, mb, mb, "over the limit of 1 MB of Markdown"},
		{"file", func() map[string]string { return map[string]string{deployMD: strings.Repeat("a", mb)} }, domain.MaxSourceBytes, mb, "over the limit of 1 MB per file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := repoWorld(t)
			w.docs.set(tc.docs())

			in := w.ingestor(nil)
			in.SetByteLimits(tc.source, tc.file)
			w.syncDue(in)

			src := w.source(repoSource)
			w.want("the source", src.LastError, src.Status == domain.SyncFailed, strings.Contains(src.LastError, tc.says))
			w.want("snapshots and objects", src, len(w.snapshots(repoSource)) == 0, w.objectsUnder(repoSource) == 0, src.CurrentSnapshot == "")
		})
	}
}

// An upload source's sync writes its snapshot too: each upload at its file name, HTML as
// Markdown, its source line the file's path.
func TestSnapshot_UploadsWriteTheirFiles(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	if _, err := w.uploads.Upload(ctx, "handbook", "leave.md", []byte("# Leave\n\nAsk your lead."), admin); err != nil {
		t.Fatal(err)
	}

	if _, err := w.uploads.Upload(ctx, "handbook", "page.html", []byte("<h1>Expenses</h1><p>Receipts within 30 days.</p>"), admin); err != nil {
		t.Fatal(err)
	}

	w.syncDue(w.ingestor(nil))

	files := w.zipOf(w.snapshots("handbook")[0])
	w.want("leave.md", files["leave.md"].text, files["leave.md"].text == "---\nsource: leave.md\n---\n\n# Leave\n\nAsk your lead.")
	w.want("page.md", files["page.md"].text, strings.Contains(files["page.md"].text, "# Expenses"), strings.Contains(files["page.md"].text, "Receipts within 30 days."))
}

// confluenceWorld is a world with only a Confluence source, "wiki", read by docs with the secret
// wiki-token.
func confluenceWorld(t *testing.T) *world {
	t.Helper()

	w := repoWorld(t)
	ctx := w.ctx()

	if _, err := w.secrets.Create(ctx, domain.NewSecret{Name: "wiki-token", Value: "test-token", Scope: domain.SecretScope{All: true}}, admin); err != nil {
		t.Fatal(err)
	}

	if err := w.kb.DeleteSource(ctx, repoSource, admin, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := w.kb.CreateSource(ctx, domain.NewKnowledgeSource{
		Name: "wiki", Title: "Wiki", Kind: domain.SourceConfluence, Secret: "wiki-token", Config: map[string]string{"site": "acme.example.com"},
	}, admin); err != nil {
		t.Fatal(err)
	}

	return w
}

// A Confluence source is read by its kind's reader with its secret opened at sync time, and
// syncs into a snapshot.
func TestSnapshot_AConfluenceSourceReadsWithItsSecret(t *testing.T) {
	t.Parallel()

	w := confluenceWorld(t)

	w.docs.set(map[string]string{deployMD: shipOnTuesdays})
	w.syncDue(w.ingestor(nil))

	src := w.source("wiki")
	w.want("the wiki", src, src.Status == domain.SyncIdle, src.CurrentSnapshot != "", w.docs.secret == "test-token")

	files := w.zipOf(w.snapshots("wiki")[0])
	w.want("its files", files, len(files) == 1)
}

// A Confluence page lands at the path of its page tree: its folder, then its title's slug and its
// id. A page without a folder lands at the top, and one whose folder would not make a file's path
// (not clean, or too long) lands at the top too.
func TestSnapshot_ConfluencePagesLandAtTheirTreePaths(t *testing.T) {
	t.Parallel()

	w := confluenceWorld(t)

	w.docs.set(map[string]string{"11": "# Runbooks", "12": "# Deploy", "13": "# Paging", "15": "# Odd", "16": "# Deep"})
	w.docs.titles = map[string]string{"11": "Runbooks", "12": "Deploy", "13": "Paging: On-call", "15": "Odd", "16": "Deep"}
	w.docs.folders = map[string]string{"11": "ENG", "13": "ENG/runbooks", "15": "ENG/../up", "16": "ENG/" + strings.Repeat("a", domain.MaxFilePath)}
	w.syncDue(w.ingestor(nil))

	got := w.files("wiki")
	slices.Sort(got)

	want := []string{"ENG/runbooks-11.md", "ENG/runbooks/paging-on-call-13.md", "deep-16.md", "deploy-12.md", "odd-15.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}

	for _, p := range got {
		if err := domain.CheckFilePath(p); err != nil {
			t.Errorf("file %s: %v", p, err)
		}
	}
}

// manyDocs is n Markdown files, docs/page-<i>.md.
func manyDocs(n int) map[string]string {
	out := make(map[string]string, n)
	for i := range n {
		out["docs/page-"+strconv.Itoa(i)+".md"] = "# Page " + strconv.Itoa(i) + "\n\nSome text."
	}

	return out
}

// A sync that changes at least knowledge.ArchiveFrom files reads them from one archive and
// fetches none of them; the per-file rules still hold, so a key-like file stays out. A later
// sync that changes one file fetches it by itself.
func TestSnapshot_ManyChangesReadTheArchive(t *testing.T) {
	t.Parallel()

	w := repoWorld(t)
	docs := manyDocs(knowledge.ArchiveFrom - 1)
	leaky := "docs/leaky.md"
	docs[leaky] = "export GH_TOKEN=ghp_" + strings.Repeat("0", 36) + "\n"
	r := &archiving{reader: w.docs}
	w.docs.set(docs)

	in := w.ingestorReading(w.kb, r)
	w.syncDue(in)

	snap := w.snapshots(repoSource)[0]
	w.want("the first snapshot", snap, snap.Files == knowledge.ArchiveFrom-1, snap.Failed == 1, snap.Changes.Failed[0].Key)
	w.want("the reads", r.count(), r.count() == 1)
	fetched := w.docs.fetches()
	w.want("the fetches", fetched, len(fetched) == 0)

	page := w.zipOf(snap)["docs/page-0.md"]
	w.want("page-0.md", page, strings.HasPrefix(page.text, "---\nsource: https://git.example.com/docs/page-0.md\n"))

	docs["docs/page-0.md"] = "# Page 0\n\nNew text."
	w.docs.set(docs)
	w.clock.Advance(time.Hour + time.Minute)
	w.syncDue(in)

	w.want("the reads", r.count(), r.count() == 1)
	// The key-like file has no last good version, so it is read again.
	fetched = w.docs.fetches()
	w.want("the fetches", fetched, slices.Equal(fetched, []string{leaky, "docs/page-0.md"}))
}

// Fewer changed files than knowledge.ArchiveFrom are fetched one by one, archive or not.
func TestSnapshot_FewChangesFetchFileByFile(t *testing.T) {
	t.Parallel()

	w := repoWorld(t)
	r := &archiving{reader: w.docs}
	w.docs.set(manyDocs(knowledge.ArchiveFrom - 1))

	w.syncDue(w.ingestorReading(w.kb, r))

	snap := w.snapshots(repoSource)[0]
	w.want("the snapshot", snap, snap.Files == knowledge.ArchiveFrom-1)
	w.want("the reads", r.count(), r.count() == 0)
	fetched := w.docs.fetches()
	w.want("the fetches", fetched, len(fetched) == knowledge.ArchiveFrom-1)
}

// An archive that fails as a whole after some files, or skips a file, leaves the rest to be
// fetched one by one, and the sync succeeds with every file.
func TestSnapshot_AFailedArchiveFallsBackToFiles(t *testing.T) {
	t.Parallel()

	w := repoWorld(t)
	docs := manyDocs(knowledge.ArchiveFrom)
	r := &archiving{reader: w.docs, skip: map[string]bool{"docs/page-0.md": true}, breakAfter: 3}
	w.docs.set(docs)

	w.syncDue(w.ingestorReading(w.kb, r))

	src := w.source(repoSource)
	w.want("the source", src, src.Status == domain.SyncIdle, src.LastError == "")

	snap := w.snapshots(repoSource)[0]
	w.want("the snapshot", snap, snap.Files == knowledge.ArchiveFrom, snap.Failed == 0)
	w.want("the reads", r.count(), r.count() == 1)

	fetched := w.docs.fetches()
	// page-0 was skipped; page-1, -10 and -11 came from the archive before it broke.
	w.want("the fetches", fetched, len(fetched) == knowledge.ArchiveFrom-3, slices.Contains(fetched, "docs/page-0.md"),
		!slices.Contains(fetched, "docs/page-1.md"), !slices.Contains(fetched, "docs/page-11.md"))
}
