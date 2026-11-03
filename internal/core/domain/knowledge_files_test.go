package domain

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	toolRead  = "Read"
	toolGrep  = "Grep"
	toolBash  = "Bash"
	fileA     = "a.md"
	srcID     = "src"
	docsTitle = "Docs"
	docsName  = "docs"
)

func TestCheckFilePath_RelativeCleanAndBounded(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{fileA, "runbooks/pager/alert.md", "Über uns.md", strings.Repeat("a", MaxFilePath)} {
		if err := CheckFilePath(ok); err != nil {
			t.Errorf("CheckFilePath(%.40q) = %v", ok, err)
		}
	}

	for _, bad := range []string{
		"", "/etc/passwd", "../up.md", "a/../b.md", "./a.md", "a//b.md", "a/", "a\\b.md", "a\x00.md", "\xff.md",
		strings.Repeat("a", MaxFilePath+1),
	} {
		if err := CheckFilePath(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("CheckFilePath(%.40q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestCheckReadWhen_OneLineOfAtMost160(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "How to fix what pages us. Read before acting on an alert.", strings.Repeat("é", MaxReadWhen)} {
		if err := CheckReadWhen(ok); err != nil {
			t.Errorf("CheckReadWhen(%.40q) = %v", ok, err)
		}
	}

	for _, bad := range []string{strings.Repeat("x", MaxReadWhen+1), "two\nlines", "a\rb"} {
		if err := CheckReadWhen(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("CheckReadWhen(%.40q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func snapshotOf(files ...KnowledgeFile) KnowledgeSnapshot {
	s := KnowledgeSnapshot{ID: "s1", Source: srcID, Files: len(files), CreatedAt: time.Unix(1, 0)}
	for _, f := range files {
		s.Bytes += f.Bytes
	}

	return s
}

func TestKnowledgeSnapshot_Check(t *testing.T) {
	t.Parallel()

	a := KnowledgeFile{Path: fileA, Title: "A", Bytes: 10, Version: "v1"}
	b := KnowledgeFile{Path: "b/c.md", Title: "C", Bytes: 20, Version: "v1"}

	if err := snapshotOf(a, b).Check([]KnowledgeFile{a, b}); err != nil {
		t.Errorf("Check of a snapshot of two files = %v", err)
	}

	if err := snapshotOf().Check(nil); err != nil {
		t.Errorf("Check of an empty snapshot = %v", err)
	}

	failed := snapshotOf(a)
	failed.Failed = 2
	failed.Changes = SnapshotChanges{Added: 1, Failed: []FileFailure{{Path: "x.pdf", Error: "cannot convert"}, {Path: "k.md", Key: true}}}

	if err := failed.Check([]KnowledgeFile{a}); err != nil {
		t.Errorf("Check of a snapshot with failures = %v", err)
	}

	big := KnowledgeFile{Path: "big.md", Title: "Big", Bytes: MaxFileBytes, Version: "v"}
	manyBig := make([]KnowledgeFile, 0, MaxSourceBytes/MaxFileBytes+1)

	for i := range MaxSourceBytes/MaxFileBytes + 1 {
		f := big
		f.Path = strings.Repeat("d", i+1) + ".md"
		manyBig = append(manyBig, f)
	}

	tooMany := snapshotOf()
	tooMany.Files = MaxSourceFiles + 1

	for what, tc := range map[string]struct {
		s     KnowledgeSnapshot
		files []KnowledgeFile
	}{
		"no id":                    {KnowledgeSnapshot{Source: srcID, Files: 1, Bytes: 10, CreatedAt: time.Unix(1, 0)}, []KnowledgeFile{a}},
		"no source":                {KnowledgeSnapshot{ID: "s", Files: 1, Bytes: 10, CreatedAt: time.Unix(1, 0)}, []KnowledgeFile{a}},
		"no time":                  {KnowledgeSnapshot{ID: "s", Source: srcID, Files: 1, Bytes: 10}, []KnowledgeFile{a}},
		"a count that is not len":  {snapshotOf(a, b), []KnowledgeFile{a}},
		"bytes that are not a sum": {KnowledgeSnapshot{ID: "s", Source: srcID, Files: 1, Bytes: 11, CreatedAt: time.Unix(1, 0)}, []KnowledgeFile{a}},
		"a path twice":             {snapshotOf(a, a), []KnowledgeFile{a, a}},
		"a bad path":               {snapshotOf(KnowledgeFile{Path: "../a.md", Version: "v"}), []KnowledgeFile{{Path: "../a.md", Version: "v"}}},
		"a file with no version":   {snapshotOf(KnowledgeFile{Path: fileA}), []KnowledgeFile{{Path: fileA}}},
		"a file too big":           {snapshotOf(KnowledgeFile{Path: fileA, Version: "v", Bytes: MaxFileBytes + 1}), []KnowledgeFile{{Path: fileA, Version: "v", Bytes: MaxFileBytes + 1}}},
		"more than 50 MB":          {snapshotOf(manyBig...), manyBig},
		"more than 20,000 files":   {tooMany, nil},
		"a read-when of two lines": {KnowledgeSnapshot{ID: "s", Source: srcID, ReadWhen: "a\nb", CreatedAt: time.Unix(1, 0)}, nil},
		"fewer failed than listed": {KnowledgeSnapshot{ID: "s", Source: srcID, CreatedAt: time.Unix(1, 0), Changes: SnapshotChanges{Failed: []FileFailure{{Path: fileA, Error: "x"}}}}, nil},
		"negative counts":          {KnowledgeSnapshot{ID: "s", Source: srcID, CreatedAt: time.Unix(1, 0), Changes: SnapshotChanges{Removed: -1}}, nil},
	} {
		if err := tc.s.Check(tc.files); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check of a snapshot with %s = %v, want ErrInvalid", what, err)
		}
	}
}

func TestKnowledgeSnapshotKey(t *testing.T) {
	t.Parallel()

	if got := KnowledgeSnapshotKey("o1", "s1", "n1"); got != "orgs/o1/knowledge/s1/n1.zip" {
		t.Errorf("KnowledgeSnapshotKey = %s", got)
	}
}

func TestKnowledgeFileReads_Check(t *testing.T) {
	t.Parallel()

	ok := KnowledgeFileReads{Source: srcID, Path: fileA, Factory: "f", Day: time.Unix(1, 0), Reads: 2}
	if err := ok.Check(); err != nil {
		t.Errorf("Check = %v", err)
	}

	for what, r := range map[string]KnowledgeFileReads{
		"no source":          {Path: fileA, Factory: "f", Day: time.Unix(1, 0), Reads: 1},
		"without a factory":  {Source: srcID, Path: fileA, Day: time.Unix(1, 0), Reads: 1},
		"no day":             {Source: srcID, Path: fileA, Factory: "f", Reads: 1},
		"a bad path":         {Source: srcID, Path: "/a.md", Factory: "f", Day: time.Unix(1, 0), Reads: 1},
		"negative reads":     {Source: srcID, Path: fileA, Factory: "f", Day: time.Unix(1, 0), Reads: -1, Searches: 2},
		"neither one":        {Source: srcID, Path: fileA, Factory: "f", Day: time.Unix(1, 0)},
		"negative searches":  {Source: srcID, Path: fileA, Factory: "f", Day: time.Unix(1, 0), Reads: 1, Searches: -1},
		"a read of the root": {Source: srcID, Path: KnowledgeSourceRoot, Factory: "f", Day: time.Unix(1, 0), Reads: 1},
	} {
		if err := r.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check of reads with %s = %v, want ErrInvalid", what, err)
		}
	}

	root := KnowledgeFileReads{Source: srcID, Path: KnowledgeSourceRoot, Factory: "f", Day: time.Unix(1, 0), Searches: 1}
	if err := root.Check(); err != nil {
		t.Errorf("Check of a search of the whole source = %v", err)
	}

	late := time.Date(2026, 11, 3, 23, 30, 0, 0, time.FixedZone("x", -2*3600))
	if got := KnowledgeDay(late); !got.Equal(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("KnowledgeDay = %s, want the UTC date", got)
	}
}

func TestNewKnowledgeSource_RepositoryAndConfluence(t *testing.T) {
	t.Parallel()

	for what, n := range map[string]NewKnowledgeSource{
		"a public repository":      {Name: docsName, Title: docsTitle, Kind: SourceRepository},
		"a repository on a secret": {Name: docsName, Title: docsTitle, Kind: SourceRepository, Secret: "gh-token", ReadWhen: "Read for setup."},
		"confluence on a secret":   {Name: "wiki", Title: "Wiki", Kind: SourceConfluence, Secret: "wiki-token"},
	} {
		if err := n.Check(); err != nil {
			t.Errorf("Check of %s = %v", what, err)
		}
	}

	for what, n := range map[string]NewKnowledgeSource{
		"confluence without a secret":  {Name: "wiki", Title: "Wiki", Kind: SourceConfluence},
		"a repository on a connection": {Name: docsName, Title: docsTitle, Kind: SourceRepository, Connection: docsName},
		"a read-when too long":         {Name: docsName, Title: docsTitle, Kind: SourceUpload, ReadWhen: strings.Repeat("x", MaxReadWhen+1)},
	} {
		if err := n.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check of %s = %v, want ErrInvalid", what, err)
		}
	}

	rw := "two\nlines"
	if err := (KnowledgeSourcePatch{ReadWhen: &rw}).Check(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Check of a patch with a read-when of two lines = %v, want ErrInvalid", err)
	}

	secret := "gh-token"
	if err := (KnowledgeSourcePatch{Secret: &secret}).CheckOn(KnowledgeSource{Kind: SourceRepository}); err != nil {
		t.Errorf("CheckOn a repository given a secret = %v", err)
	}
}

func TestKnowledgeTouch_CountsAReadAndAGrep(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		tool, path string
		want       KnowledgeTouch
		ok         bool
	}{
		{tool: toolRead, path: "/knowledge/runbooks/db/disk.md", want: KnowledgeTouch{Source: srcRunbooks, Path: "db/disk.md"}, ok: true},
		{tool: toolGrep, path: "/knowledge/runbooks", want: KnowledgeTouch{Source: srcRunbooks, Path: KnowledgeSourceRoot, Search: true}, ok: true},
		{tool: "Glob", path: "/knowledge/runbooks/db/", want: KnowledgeTouch{Source: srcRunbooks, Path: "db", Search: true}, ok: true},
		{tool: toolRead, path: "/knowledge/runbooks/x/../db/disk.md", want: KnowledgeTouch{Source: srcRunbooks, Path: "db/disk.md"}, ok: true},
		{tool: toolRead, path: "/knowledge/runbooks"},
		{tool: toolGrep, path: "/knowledge"},
		{tool: toolRead, path: "/workspace/knowledge/runbooks/a.md"},
		{tool: toolRead, path: "knowledge/runbooks/a.md"},
		{tool: toolRead, path: "/knowledge/../etc/passwd"},
		{tool: toolBash, path: "/knowledge/runbooks/a.md"},
		{tool: toolRead},
	} {
		got, ok := TouchOfKnowledge(tt.tool, tt.path)
		if ok != tt.ok || got != tt.want {
			t.Errorf("TouchOfKnowledge(%s, %q) = %+v, %t; want %+v, %t", tt.tool, tt.path, got, ok, tt.want, tt.ok)
		}
	}

	day := time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)
	tally := NewKnowledgeTally([]KnowledgePin{{Source: srcRunbooks, Snapshot: "r1"}, {Source: "handbook", Snapshot: "h1"}})

	for _, c := range [][2]string{
		{toolRead, "/knowledge/runbooks/db/disk.md"},
		{toolRead, "/knowledge/runbooks/db/disk.md"},
		{toolGrep, "/knowledge/runbooks"},
		{toolRead, "/knowledge/unpinned/a.md"},
		{toolRead, "/workspace/README.md"},
	} {
		tally.Add(c[0], c[1])
	}

	if got := tally.Sources(); !reflect.DeepEqual(got, []string{srcRunbooks}) {
		t.Errorf("Sources = %v, want only the touched pinned source", got)
	}

	got := tally.Reads(map[string]KnowledgeSourceID{srcRunbooks: "r-id"}, "f", day)
	want := []KnowledgeFileReads{
		{Source: "r-id", Path: KnowledgeSourceRoot, Factory: "f", Day: KnowledgeDay(day), Searches: 1},
		{Source: "r-id", Path: "db/disk.md", Factory: "f", Day: KnowledgeDay(day), Reads: 2},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Reads = %+v\nwant %+v", got, want)
	}

	if got := tally.Reads(nil, "f", day); len(got) != 0 {
		t.Errorf("Reads without the sources' ids = %+v, want none", got)
	}
}

// Touched lists each touched pinned source's reads and searches once, sorted, and past
// MaxKnowledgeTouchedPaths in all leaves the rest out, counted in More.
func TestKnowledgeTally_Touched(t *testing.T) {
	t.Parallel()

	tally := NewKnowledgeTally([]KnowledgePin{{Source: srcRunbooks, Snapshot: "r1"}, {Source: "handbook", Snapshot: "h1"}})

	for _, c := range [][2]string{
		{toolRead, "/knowledge/runbooks/db/disk.md"},
		{toolRead, "/knowledge/runbooks/db/disk.md"},
		{toolRead, "/knowledge/runbooks/a.md"},
		{toolGrep, "/knowledge/runbooks"},
		{toolGrep, "/knowledge/handbook/hr"},
		{toolRead, "/knowledge/unpinned/a.md"},
	} {
		tally.Add(c[0], c[1])
	}

	want := []KnowledgeTouched{
		{Source: "handbook", Searches: []string{"hr"}},
		{Source: srcRunbooks, Reads: []string{fileA, "db/disk.md"}, Searches: []string{KnowledgeSourceRoot}},
	}
	if got := tally.Touched(); !reflect.DeepEqual(got, want) {
		t.Errorf("Touched = %+v\nwant %+v", got, want)
	}

	if got := NewKnowledgeTally(nil).Touched(); len(got) != 0 {
		t.Errorf("Touched of no touches = %+v, want none", got)
	}

	big := NewKnowledgeTally([]KnowledgePin{{Source: srcRunbooks, Snapshot: "r1"}, {Source: "handbook", Snapshot: "h1"}})
	for i := range 500 {
		big.Add(toolRead, fmt.Sprintf("/knowledge/handbook/f%03d.md", i))
		big.Add(toolRead, fmt.Sprintf("/knowledge/runbooks/f%03d.md", i))
	}

	got := big.Touched()
	if len(got) != 2 {
		t.Fatalf("Touched of two sources = %d sources", len(got))
	}

	listed, more := 0, 0
	for _, s := range got {
		listed += len(s.Reads) + len(s.Searches)
		more += s.More

		if len(s.Reads)+len(s.Searches)+s.More != 500 {
			t.Errorf("source %s lists %d and leaves out %d, want 500 in all", s.Source, len(s.Reads)+len(s.Searches), s.More)
		}
	}

	if listed != MaxKnowledgeTouchedPaths || more != 1000-MaxKnowledgeTouchedPaths {
		t.Errorf("Touched lists %d paths and leaves out %d; want %d and %d", listed, more, MaxKnowledgeTouchedPaths, 1000-MaxKnowledgeTouchedPaths)
	}

	if got[0].Reads[0] != "f000.md" {
		t.Errorf("first listed read = %q, want the first by path", got[0].Reads[0])
	}
}
