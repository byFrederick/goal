package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckFilePath_RelativeCleanAndBounded(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"a.md", "runbooks/pager/alert.md", "Über uns.md", strings.Repeat("a", MaxFilePath)} {
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
	s := KnowledgeSnapshot{ID: "s1", Source: "src", Files: len(files), CreatedAt: time.Unix(1, 0)}
	for _, f := range files {
		s.Bytes += f.Bytes
	}

	return s
}

func TestKnowledgeSnapshot_Check(t *testing.T) {
	t.Parallel()

	a := KnowledgeFile{Path: "a.md", Title: "A", Bytes: 10, Version: "v1"}
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
		"no id":                    {KnowledgeSnapshot{Source: "src", Files: 1, Bytes: 10, CreatedAt: time.Unix(1, 0)}, []KnowledgeFile{a}},
		"no source":                {KnowledgeSnapshot{ID: "s", Files: 1, Bytes: 10, CreatedAt: time.Unix(1, 0)}, []KnowledgeFile{a}},
		"no time":                  {KnowledgeSnapshot{ID: "s", Source: "src", Files: 1, Bytes: 10}, []KnowledgeFile{a}},
		"a count that is not len":  {snapshotOf(a, b), []KnowledgeFile{a}},
		"bytes that are not a sum": {KnowledgeSnapshot{ID: "s", Source: "src", Files: 1, Bytes: 11, CreatedAt: time.Unix(1, 0)}, []KnowledgeFile{a}},
		"a path twice":             {snapshotOf(a, a), []KnowledgeFile{a, a}},
		"a bad path":               {snapshotOf(KnowledgeFile{Path: "../a.md", Version: "v"}), []KnowledgeFile{{Path: "../a.md", Version: "v"}}},
		"a file with no version":   {snapshotOf(KnowledgeFile{Path: "a.md"}), []KnowledgeFile{{Path: "a.md"}}},
		"a file too big":           {snapshotOf(KnowledgeFile{Path: "a.md", Version: "v", Bytes: MaxFileBytes + 1}), []KnowledgeFile{{Path: "a.md", Version: "v", Bytes: MaxFileBytes + 1}}},
		"more than 50 MB":          {snapshotOf(manyBig...), manyBig},
		"more than 20,000 files":   {tooMany, nil},
		"a read-when of two lines": {KnowledgeSnapshot{ID: "s", Source: "src", ReadWhen: "a\nb", CreatedAt: time.Unix(1, 0)}, nil},
		"fewer failed than listed": {KnowledgeSnapshot{ID: "s", Source: "src", CreatedAt: time.Unix(1, 0), Changes: SnapshotChanges{Failed: []FileFailure{{Path: "a.md", Error: "x"}}}}, nil},
		"negative counts":          {KnowledgeSnapshot{ID: "s", Source: "src", CreatedAt: time.Unix(1, 0), Changes: SnapshotChanges{Removed: -1}}, nil},
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

	ok := KnowledgeFileReads{Source: "src", Path: "a.md", Factory: "f", Day: time.Unix(1, 0), Reads: 2}
	if err := ok.Check(); err != nil {
		t.Errorf("Check = %v", err)
	}

	for what, r := range map[string]KnowledgeFileReads{
		"no source":         {Path: "a.md", Factory: "f", Day: time.Unix(1, 0), Reads: 1},
		"no factory":        {Source: "src", Path: "a.md", Day: time.Unix(1, 0), Reads: 1},
		"no day":            {Source: "src", Path: "a.md", Factory: "f", Reads: 1},
		"a bad path":        {Source: "src", Path: "/a.md", Factory: "f", Day: time.Unix(1, 0), Reads: 1},
		"negative reads":    {Source: "src", Path: "a.md", Factory: "f", Day: time.Unix(1, 0), Reads: -1, Searches: 2},
		"neither one":       {Source: "src", Path: "a.md", Factory: "f", Day: time.Unix(1, 0)},
		"negative searches": {Source: "src", Path: "a.md", Factory: "f", Day: time.Unix(1, 0), Reads: 1, Searches: -1},
	} {
		if err := r.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check of reads with %s = %v, want ErrInvalid", what, err)
		}
	}

	late := time.Date(2026, 11, 3, 23, 30, 0, 0, time.FixedZone("x", -2*3600))
	if got := KnowledgeDay(late); !got.Equal(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("KnowledgeDay = %s, want the UTC date", got)
	}
}

func TestNewKnowledgeSource_RepositoryAndConfluence(t *testing.T) {
	t.Parallel()

	for what, n := range map[string]NewKnowledgeSource{
		"a public repository":      {Name: "docs", Title: "Docs", Kind: SourceRepository},
		"a repository on a secret": {Name: "docs", Title: "Docs", Kind: SourceRepository, Secret: "gh-token", ReadWhen: "Read for setup."},
		"confluence on a secret":   {Name: "wiki", Title: "Wiki", Kind: SourceConfluence, Secret: "wiki-token"},
	} {
		if err := n.Check(); err != nil {
			t.Errorf("Check of %s = %v", what, err)
		}
	}

	for what, n := range map[string]NewKnowledgeSource{
		"confluence without a secret":  {Name: "wiki", Title: "Wiki", Kind: SourceConfluence},
		"a repository on a connection": {Name: "docs", Title: "Docs", Kind: SourceRepository, Connection: "docs"},
		"a read-when too long":         {Name: "docs", Title: "Docs", Kind: SourceUpload, ReadWhen: strings.Repeat("x", MaxReadWhen+1)},
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
