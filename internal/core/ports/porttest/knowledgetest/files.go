package knowledgetest

import (
	"reflect"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Knowledge as files (ADR 0038 decisions 3 and 15): snapshots, the current files and the daily
// read counts.

// Snapshot ids the tests use (UUIDv7, as goald's).
const (
	snap1 domain.KnowledgeSnapshotID = "01900000-0000-7000-8000-0000000000a1"
	snap2 domain.KnowledgeSnapshotID = "01900000-0000-7000-8000-0000000000a2"
	snap3 domain.KnowledgeSnapshotID = "01900000-0000-7000-8000-0000000000a3"
	snap4 domain.KnowledgeSnapshotID = "01900000-0000-7000-8000-0000000000a4"
)

// Paths of files the tests use.
const (
	fileA  = "a.md"
	fileBA = "b/a.md"
	fileBB = "b/b.md"
)

// Factories that read.
const (
	factoryA domain.FactoryID = "01900000-0000-7000-8000-0000000000f1"
	factoryB domain.FactoryID = "01900000-0000-7000-8000-0000000000f2"
)

func file(p, title, version string, size int64) domain.KnowledgeFile {
	return domain.KnowledgeFile{Path: p, Title: title, Bytes: size, Version: version}
}

// snapshot is snapshot id of source with files, made at at.
func snapshot(id domain.KnowledgeSnapshotID, source domain.KnowledgeSourceID, at time.Time, files ...domain.KnowledgeFile) domain.KnowledgeSnapshot {
	s := domain.KnowledgeSnapshot{ID: id, Source: source, Files: len(files), ZipBytes: 42, CreatedAt: at}
	for _, f := range files {
		s.Bytes += f.Bytes
	}

	return s
}

// claimSource claims the only due source of the harness at now and fails unless it is src.
func (f *fixture) claimSource(src domain.KnowledgeSource, now time.Time) domain.SourceClaim {
	f.t.Helper()

	c := f.claimed("claim "+src.Name, now, 10*time.Minute)
	if c.Source.ID != src.ID {
		f.t.Fatalf("claimed %s, want %s", c.Source.Name, src.Name)
	}

	return c
}

func (f *fixture) putSnapshot(c domain.SourceClaim, s domain.KnowledgeSnapshot, files ...domain.KnowledgeFile) domain.KnowledgeSnapshot {
	f.t.Helper()

	got, err := f.Knowledge.PutSnapshot(f.in(c.Org), c.Lease, s, files)
	if err != nil {
		f.t.Fatalf("PutSnapshot %s: %v", s.ID, err)
	}

	return got
}

func (f *fixture) files(org domain.OrgID, q domain.KnowledgeFileQuery) []domain.KnowledgeFile {
	f.t.Helper()

	got, err := f.Knowledge.Files(f.in(org), q)
	if err != nil {
		f.t.Fatalf("Files %+v: %v", q, err)
	}

	return got
}

func paths(files []domain.KnowledgeFile) []string {
	out := make([]string, 0, len(files))
	for _, x := range files {
		out = append(out, x.Path)
	}

	return out
}

// testSnapshots: a snapshot becomes the source's current one with its key; a newer one replaces
// the current files and keeps the older snapshot's row; deleting the source takes them all.
func testSnapshots(f *fixture) {
	ctx := f.ctx()
	src := f.upload(handbook)
	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	c := f.claimSource(src, t0)

	s1 := snapshot(snap1, src.ID, t0, file(fileA, "A", "v1", 10), file("b/c.md", "C", "v1", 20))
	s1.ReadWhen = "Read before changing the handbook."
	s1.Changes = domain.SnapshotChanges{Added: 2}

	got := f.putSnapshot(c, s1, file(fileA, "A", "v1", 10), file("b/c.md", "C", "v1", 20))
	f.want("PutSnapshot", got, got.Key == domain.KnowledgeSnapshotKey(f.org, src.ID, snap1), got.ID == snap1, got.Files == 2, got.Bytes == 30)

	cur := f.get(f.org, handbook)
	f.want("the source after its first snapshot", cur, cur.CurrentSnapshot == snap1)

	first := f.files(f.org, domain.KnowledgeFileQuery{Source: handbook, Limit: 10})
	f.want("Files", first, reflect.DeepEqual(first, []domain.KnowledgeFile{
		{Source: src.ID, Path: fileA, Title: "A", Bytes: 10, Version: "v1"},
		{Source: src.ID, Path: "b/c.md", Title: "C", Bytes: 20, Version: "v1"},
	}))

	s2 := snapshot(snap2, src.ID, t0.Add(time.Second), file("b/c.md", "C", "v2", 25), file("d.md", "D", "v1", 5))
	s2.Failed = 1
	s2.Changes = domain.SnapshotChanges{Added: 1, Changed: 1, Removed: 1, Failed: []domain.FileFailure{{Path: "e.pdf", Error: "cannot convert"}}}
	f.putSnapshot(c, s2, file("b/c.md", "C", "v2", 25), file("d.md", "D", "v1", 5))

	second := f.files(f.org, domain.KnowledgeFileQuery{Source: handbook, Limit: 10})
	f.want("Files after the second snapshot", second, slices.Equal(paths(second), []string{"b/c.md", "d.md"}), second[0].Version == "v2")

	snaps, err := f.Knowledge.Snapshots(ctx, handbook)
	f.must(err)

	want2 := s2
	want2.Key = domain.KnowledgeSnapshotKey(f.org, src.ID, snap2)
	want1 := s1
	want1.Key = domain.KnowledgeSnapshotKey(f.org, src.ID, snap1)

	if len(snaps) != 2 || !snapshotEqual(snaps[0], want2) || !snapshotEqual(snaps[1], want1) {
		f.t.Errorf("Snapshots = %+v, want the second then the first", snaps)
	}

	cur = f.get(f.org, handbook)
	f.want("the source after its second snapshot", cur, cur.CurrentSnapshot == snap2)

	// An empty snapshot empties the folder.
	f.putSnapshot(c, snapshot(snap3, src.ID, t0.Add(2*time.Second)))
	f.want("Files after an empty snapshot", f.files(f.org, domain.KnowledgeFileQuery{Source: handbook, Limit: 10}),
		len(f.files(f.org, domain.KnowledgeFileQuery{Source: handbook, Limit: 10})) == 0)

	f.must(f.Knowledge.DeleteSource(ctx, handbook, admin, nil))
	f.upload(handbook)

	snaps, err = f.Knowledge.Snapshots(ctx, handbook)
	f.must(err)
	f.want("Snapshots of a source made again with the same name", snaps, len(snaps) == 0)
	f.want("Files of a source made again with the same name", nil,
		len(f.files(f.org, domain.KnowledgeFileQuery{Source: handbook, Limit: 10})) == 0)
}

// snapshotEqual compares snapshots with their times as instants.
func snapshotEqual(a, b domain.KnowledgeSnapshot) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return false
	}

	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}

	return reflect.DeepEqual(a, b)
}

// testSnapshotRules: a snapshot that breaks its rules, of another source than the lease's, or
// under a lease another claim took, changes nothing.
func testSnapshotRules(f *fixture) {
	ctx := f.ctx()
	src := f.upload(handbook)
	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	c := f.claimSource(src, t0)

	a := file(fileA, "A", "v1", 10)
	f.putSnapshot(c, snapshot(snap1, src.ID, t0, a), a)

	other := f.upload(runbooks)

	for what, s := range map[string]domain.KnowledgeSnapshot{
		"a count that is not its files'": {ID: snap2, Source: src.ID, Files: 2, Bytes: 10, CreatedAt: t0},
		"of another source":              snapshot(snap2, other.ID, t0, a),
		"a read-when of two lines":       {ID: snap2, Source: src.ID, Files: 1, Bytes: 10, ReadWhen: "a\nb", CreatedAt: t0},
	} {
		_, err := f.Knowledge.PutSnapshot(ctx, c.Lease, s, []domain.KnowledgeFile{a})
		f.refused("PutSnapshot with "+what, err, domain.ErrInvalid)
	}

	_, err := f.Knowledge.PutSnapshot(ctx, c.Lease, snapshot(snap2, src.ID, t0, a, a), []domain.KnowledgeFile{a, a})
	f.refused("PutSnapshot with a path twice", err, domain.ErrInvalid)

	f.finish(c, t0.Add(time.Minute), "")

	b := file("b.md", "B", "v1", 1)
	_, err = f.Knowledge.PutSnapshot(ctx, c.Lease, snapshot(snap2, src.ID, t0, b), []domain.KnowledgeFile{b})
	f.refused("PutSnapshot after the sync ended", err, ports.ErrLeaseLost)

	_, err = f.Knowledge.PutSnapshot(ctx, domain.SourceLease{Source: src.ID, Token: "not-a-token"}, snapshot(snap2, src.ID, t0, b), []domain.KnowledgeFile{b})
	f.refused("PutSnapshot under a malformed lease", err, ports.ErrLeaseLost)

	got := f.files(f.org, domain.KnowledgeFileQuery{Source: handbook, Limit: 10})
	f.want("Files after the refusals", got, slices.Equal(paths(got), []string{fileA}))

	snaps, err := f.Knowledge.Snapshots(ctx, handbook)
	f.must(err)
	f.want("Snapshots after the refusals", snaps, len(snaps) == 1, snaps[0].ID == snap1)

	_, err = f.Knowledge.Snapshots(ctx, "missing")
	f.refused("Snapshots of an unknown source", err, ports.ErrNotFound)

	_, err = f.Knowledge.Files(ctx, domain.KnowledgeFileQuery{Source: "missing", Limit: 1})
	f.refused("Files of an unknown source", err, ports.ErrNotFound)
}

// testSnapshotRetention: retention keeps a source's current snapshot, one a running run pinned
// and one replaced inside the window, and deletes the rest; it never deletes the current one,
// and a lease another claim took changes nothing.
func testSnapshotRetention(f *fixture) {
	ctx := f.ctx()
	src := f.upload(handbook)
	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	c := f.claimSource(src, t0)
	a := file(fileA, "A", "v1", 10)

	// old is replaced at t0+1h, pinned at t0+2h, young at t0+5h; current is current.
	for _, s := range []struct {
		id domain.KnowledgeSnapshotID
		at time.Duration
	}{{snap1, 0}, {snap2, time.Hour}, {snap3, 2 * time.Hour}, {snap4, 5 * time.Hour}} {
		f.putSnapshot(c, snapshot(s.id, src.ID, t0.Add(s.at), a), a)
	}

	f.Pin(f.t, f.org, snap2)

	expired, err := f.Knowledge.ExpiredSnapshots(ctx, c.Lease, t0.Add(3*time.Hour))
	f.must(err)
	f.want("ExpiredSnapshots", expired, len(expired) == 1, len(expired) == 1 && expired[0].ID == snap1,
		len(expired) == 1 && expired[0].Key == domain.KnowledgeSnapshotKey(f.org, src.ID, snap1))

	early, err := f.Knowledge.ExpiredSnapshots(ctx, c.Lease, t0.Add(time.Hour-time.Microsecond))
	f.must(err)
	f.want("ExpiredSnapshots before the old one was replaced", early, len(early) == 0)

	// The current snapshot is never deleted, even when asked.
	f.must(f.Knowledge.DeleteSnapshots(ctx, c.Lease, []domain.KnowledgeSnapshotID{snap1, snap4}))

	snaps, err := f.Knowledge.Snapshots(ctx, handbook)
	f.must(err)

	ids := make([]domain.KnowledgeSnapshotID, 0, len(snaps))
	for _, s := range snaps {
		ids = append(ids, s.ID)
	}

	f.want("Snapshots after the deletion", ids, slices.Equal(ids, []domain.KnowledgeSnapshotID{snap4, snap3, snap2}))
	f.want("the source after the deletion", nil, f.get(f.org, handbook).CurrentSnapshot == snap4)

	again, err := f.Knowledge.ExpiredSnapshots(ctx, c.Lease, t0.Add(3*time.Hour))
	f.must(err)
	f.want("ExpiredSnapshots after the deletion", again, len(again) == 0)

	// Another claim takes the source once the lease ends.
	f.claimSource(src, t0.Add(11*time.Minute))

	_, err = f.Knowledge.ExpiredSnapshots(ctx, c.Lease, t0.Add(24*time.Hour))
	f.refused("ExpiredSnapshots under a lease another claim took", err, ports.ErrLeaseLost)
	f.refused("DeleteSnapshots under a lease another claim took",
		f.Knowledge.DeleteSnapshots(ctx, c.Lease, []domain.KnowledgeSnapshotID{snap3}), ports.ErrLeaseLost)

	snaps, err = f.Knowledge.Snapshots(ctx, handbook)
	f.must(err)
	f.want("Snapshots after the refusals", snaps, len(snaps) == 3)
}

// testFilesPage: files page in byte order of their paths, after a path, and filter by a prefix
// of the path or, in any case, of the title.
func testFilesPage(f *fixture) {
	src := f.upload(handbook)
	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	c := f.claimSource(src, t0)

	all := []domain.KnowledgeFile{
		file("Z.md", "Zebra", "v", 1), file(fileA, "Overview", "v", 1), file(fileBA, "Alpha", "v", 1),
		file(fileBB, "Beta", "v", 1), file("c.md", "alpine guide", "v", 1),
	}
	// Given out of order: the store orders them.
	f.putSnapshot(c, snapshot(snap1, src.ID, t0, all...), all[4], all[2], all[0], all[3], all[1])

	for what, tc := range map[string]struct {
		q    domain.KnowledgeFileQuery
		want []string
	}{
		"every file":           {domain.KnowledgeFileQuery{Limit: 10}, []string{"Z.md", fileA, fileBA, fileBB, "c.md"}},
		"the first page":       {domain.KnowledgeFileQuery{Limit: 2}, []string{"Z.md", fileA}},
		"the page after a.md":  {domain.KnowledgeFileQuery{After: fileA, Limit: 2}, []string{fileBA, fileBB}},
		"the last page":        {domain.KnowledgeFileQuery{After: fileBB, Limit: 2}, []string{"c.md"}},
		"a path prefix":        {domain.KnowledgeFileQuery{Prefix: "b/", Limit: 10}, []string{fileBA, fileBB}},
		"a title prefix":       {domain.KnowledgeFileQuery{Prefix: "AL", Limit: 10}, []string{fileBA, "c.md"}},
		"a prefix after":       {domain.KnowledgeFileQuery{Prefix: "b/", After: fileBA, Limit: 10}, []string{fileBB}},
		"a prefix of nothing":  {domain.KnowledgeFileQuery{Prefix: "x", Limit: 10}, []string{}},
		"no limit, no files":   {domain.KnowledgeFileQuery{}, []string{}},
		"a prefix with a % in": {domain.KnowledgeFileQuery{Prefix: "%", Limit: 10}, []string{}},
	} {
		tc.q.Source = handbook

		if got := paths(f.files(f.org, tc.q)); !slices.Equal(got, tc.want) {
			f.t.Errorf("Files of %s = %v, want %v", what, got, tc.want)
		}
	}
}

// testReads: counts add to one row per source, path, factory and day, a day being a UTC date; a
// count of an unknown source adds nothing; and each count's rules hold.
func testReads(f *fixture) {
	ctx := f.ctx()
	src := f.upload(handbook)
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)

	f.must(f.Knowledge.AddReads(ctx, []domain.KnowledgeFileReads{
		{Source: src.ID, Path: fileA, Factory: factoryA, Day: day.Add(9 * time.Hour), Reads: 2},
		{Source: src.ID, Path: fileA, Factory: factoryA, Day: day.Add(10 * time.Hour), Searches: 1},
		{Source: src.ID, Path: fileA, Factory: factoryB, Day: day.Add(11 * time.Hour), Reads: 1},
	}))
	f.must(f.Knowledge.AddReads(ctx, []domain.KnowledgeFileReads{
		// 23:30 at UTC-2 is the next UTC day.
		{Source: src.ID, Path: fileA, Factory: factoryA, Day: time.Date(2026, 11, 3, 23, 30, 0, 0, time.FixedZone("acme", -2*3600)), Reads: 4},
		{Source: src.ID, Path: fileA, Factory: factoryA, Day: day.Add(23 * time.Hour), Reads: 1, Searches: 2},
		{Source: src.ID, Path: "b.md", Factory: factoryA, Day: day, Reads: 1},
		// A search of the whole source counts at its root.
		{Source: src.ID, Path: domain.KnowledgeSourceRoot, Factory: factoryA, Day: day.Add(30 * time.Hour), Searches: 1},
	}))

	got, err := f.Knowledge.FileReads(ctx, handbook, day.Add(-24*time.Hour))
	f.must(err)

	next := day.Add(24 * time.Hour)
	want := []domain.KnowledgeFileReads{
		{Source: src.ID, Path: fileA, Factory: factoryA, Day: day, Reads: 3, Searches: 3},
		{Source: src.ID, Path: fileA, Factory: factoryB, Day: day, Reads: 1},
		{Source: src.ID, Path: "b.md", Factory: factoryA, Day: day, Reads: 1},
		{Source: src.ID, Path: domain.KnowledgeSourceRoot, Factory: factoryA, Day: next, Searches: 1},
		{Source: src.ID, Path: fileA, Factory: factoryA, Day: next, Reads: 4},
	}

	if !readsEqual(got, want) {
		f.t.Errorf("FileReads = %+v, want %+v", got, want)
	}

	later, err := f.Knowledge.FileReads(ctx, handbook, next.Add(5*time.Hour))
	f.must(err)
	f.want("FileReads since the next day", later, readsEqual(later, want[3:]))

	err = f.Knowledge.AddReads(ctx, []domain.KnowledgeFileReads{
		{Source: src.ID, Path: fileA, Factory: factoryA, Day: day, Reads: 100},
		{Source: "01900000-0000-7000-8000-0000000000ee", Path: fileA, Factory: factoryA, Day: day, Reads: 1},
	})
	f.refused("AddReads of an unknown source", err, ports.ErrNotFound)

	err = f.Knowledge.AddReads(ctx, []domain.KnowledgeFileReads{{Source: src.ID, Path: "../a.md", Factory: factoryA, Day: day, Reads: 1}})
	f.refused("AddReads of a bad path", err, domain.ErrInvalid)

	err = f.Knowledge.AddReads(ctx, []domain.KnowledgeFileReads{{Source: src.ID, Path: domain.KnowledgeSourceRoot, Factory: factoryA, Day: day, Reads: 1}})
	f.refused("AddReads of a read of the source's root", err, domain.ErrInvalid)

	again, err := f.Knowledge.FileReads(ctx, handbook, day)
	f.must(err)
	f.want("FileReads after the refusals", again, readsEqual(again, want))

	_, err = f.Knowledge.FileReads(ctx, "missing", day)
	f.refused("FileReads of an unknown source", err, ports.ErrNotFound)
}

func readsEqual(got, want []domain.KnowledgeFileReads) bool {
	return slices.EqualFunc(got, want, func(a, b domain.KnowledgeFileReads) bool {
		return a.Day.Equal(b.Day) && a.Source == b.Source && a.Path == b.Path && a.Factory == b.Factory &&
			a.Reads == b.Reads && a.Searches == b.Searches
	})
}

// testFilesIsolation: another organization sees none of a source's snapshots, files or reads,
// and cannot write them.
func testFilesIsolation(f *fixture) {
	src := f.upload(handbook)
	t0 := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	c := f.claimSource(src, t0)
	a := file(fileA, "A", "v1", 10)
	f.putSnapshot(c, snapshot(snap1, src.ID, t0, a), a)

	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	f.must(f.Knowledge.AddReads(f.ctx(), []domain.KnowledgeFileReads{{Source: src.ID, Path: fileA, Factory: factoryA, Day: day, Reads: 1}}))

	other := f.NewOrg(f.t)
	octx := f.in(other)

	_, err := f.Knowledge.Snapshots(octx, handbook)
	f.refused("Snapshots in another organization", err, ports.ErrNotFound)
	_, err = f.Knowledge.Files(octx, domain.KnowledgeFileQuery{Source: handbook, Limit: 10})
	f.refused("Files in another organization", err, ports.ErrNotFound)
	_, err = f.Knowledge.FileReads(octx, handbook, day)
	f.refused("FileReads in another organization", err, ports.ErrNotFound)

	b := file("b.md", "B", "v1", 1)
	_, err = f.Knowledge.PutSnapshot(octx, c.Lease, snapshot(snap2, src.ID, t0, b), []domain.KnowledgeFile{b})
	f.refused("PutSnapshot in another organization", err, ports.ErrLeaseLost)

	err = f.Knowledge.AddReads(octx, []domain.KnowledgeFileReads{{Source: src.ID, Path: fileA, Factory: factoryA, Day: day, Reads: 1}})
	f.refused("AddReads in another organization", err, ports.ErrNotFound)

	// The other organization's own source of the same name holds nothing of the first's.
	f.source(other, domain.NewKnowledgeSource{Name: handbook, Kind: domain.SourceUpload})

	files := f.files(other, domain.KnowledgeFileQuery{Source: handbook, Limit: 10})
	f.want("Files of the other organization's own source", files, len(files) == 0)

	got := f.files(f.org, domain.KnowledgeFileQuery{Source: handbook, Limit: 10})
	f.want("Files in the first organization", got, slices.Equal(paths(got), []string{fileA}))

	reads, err := f.Knowledge.FileReads(f.ctx(), handbook, day)
	f.must(err)
	f.want("FileReads in the first organization", reads, len(reads) == 1, reads[0].Reads == 1)
}

// testNewKinds: repository and Confluence sources are created, read their secret by name and
// carry their read-when line, which a patch changes: a new line makes the source due at once, a
// patch of the title or of the same line does not.
func testNewKinds(f *fixture) {
	ctx := f.ctx()

	if _, err := f.Secrets.Create(ctx, domain.NewSecret{Name: docsSecret, Value: secretValue, Scope: domain.SecretScope{All: true}}, admin); err != nil {
		f.t.Fatal(err)
	}

	repo := f.source(f.org, domain.NewKnowledgeSource{
		Name: "repo-docs", Kind: domain.SourceRepository, ReadWhen: "Read before changing the API.",
		Config: map[string]string{"url": "https://git.example.com/acme/docs"},
	})
	f.want("a public repository source", repo, repo.Kind == domain.SourceRepository, repo.Secret == "",
		repo.ReadWhen == "Read before changing the API.", repo.CurrentSnapshot == "", repo.NextSyncAt != nil)

	wiki := f.source(f.org, domain.NewKnowledgeSource{
		Name: "wiki", Kind: domain.SourceConfluence, Secret: docsSecret, Config: map[string]string{"site": "acme.example.com"},
	})
	f.want("a Confluence source", wiki, wiki.Kind == domain.SourceConfluence, wiki.Secret == docsSecret)

	// Sync both once, so neither is due.
	now := time.Now().Add(time.Second)
	for c := f.claim(now, time.Minute); c != nil; c = f.claim(now, time.Minute) {
		f.finish(*c, now, "")
	}

	due := f.get(f.org, "repo-docs").NextSyncAt
	f.want("repo-docs after its sync", due, due != nil, due.After(now))

	title, same := "Repository docs", repo.ReadWhen

	got, err := f.Knowledge.UpdateSource(ctx, "repo-docs", domain.KnowledgeSourcePatch{Title: &title}, admin)
	f.must(err)
	f.want("UpdateSource of only the title keeps the next sync", got.NextSyncAt, equalTime(got.NextSyncAt, *due))

	got, err = f.Knowledge.UpdateSource(ctx, "repo-docs", domain.KnowledgeSourcePatch{ReadWhen: &same}, admin)
	f.must(err)
	f.want("UpdateSource of the same read-when line keeps the next sync", got.NextSyncAt, equalTime(got.NextSyncAt, *due))

	line := "Read for how we deploy."

	got, err = f.Knowledge.UpdateSource(ctx, "repo-docs", domain.KnowledgeSourcePatch{ReadWhen: &line}, admin)
	f.must(err)
	f.want("UpdateSource of the read-when line", got, got.ReadWhen == line, f.get(f.org, "repo-docs").ReadWhen == line,
		got.NextSyncAt != nil, !got.NextSyncAt.After(time.Now()))

	_, err = f.Knowledge.CreateSource(ctx, domain.NewKnowledgeSource{Name: "wiki-two", Title: "Wiki", Kind: domain.SourceConfluence}, admin)
	f.refused("CreateSource of Confluence without a secret", err, domain.ErrInvalid)
}
