package domain

import (
	"errors"
	"reflect"
	"testing"
)

// The repository tests' entries: one repository, an owner pattern and a nested owner's pattern.
const (
	repoAPI  = "https://github.com/acme/api.git"
	repoAll  = "https://github.com/acme-labs/*"
	repoDeep = "https://gitlab.example.com/acme/platform/*"
)

// repoLine is fileLine with runtime.repositories; def names the default entry ("" for none).
func repoLine(def string) Line {
	l := fileLine()
	l.Runtime.Repositories = []RepositoryEntry{
		{Name: "api", URL: repoAPI, Access: "write", Default: def == "api"},
		{Name: "labs", URL: repoAll, Access: "read"},
		{Name: "platform", URL: repoDeep, Access: "write"},
	}

	return l
}

func fileRepos(t *testing.T, l Line, repos ...string) NewItem {
	t.Helper()

	n, err := File(l, FileRequest{Origin: OriginPerson, By: ada, Title: "Add a flag", At: fileAt, Repositories: repos})
	if err != nil {
		t.Fatal(err)
	}

	return n
}

// Each named repository, a URL or host/owner/name in any case and with or without .git, matches
// an entry, owner patterns included, and joins at filing in the order named, once each.
func TestFile_RepositoriesMatchEntries(t *testing.T) {
	t.Parallel()

	n := fileRepos(t, repoLine("api"),
		"github.com/Acme-Labs/Web", "https://GitHub.com/acme/API", "github.com/acme/api.git",
		"https://gitlab.example.com/acme/platform/infra/deploy.git", "github.com/acme-labs/web/")

	want := []ItemRepository{
		{Repo: "github.com/acme-labs/web", Joined: JoinedFiling},
		{Repo: "github.com/acme/api", Joined: JoinedFiling, Ordinal: 1},
		{Repo: "gitlab.example.com/acme/platform/infra/deploy", Joined: JoinedFiling, Ordinal: 2},
	}
	if !reflect.DeepEqual(n.Repositories, want) {
		t.Errorf("Repositories = %+v\nwant %+v", n.Repositories, want)
	}
}

// A repository no entry covers, one that is not host/owner/name, one with credentials and more
// than MaxItemRepositories are refused.
func TestFile_RepositoriesRefused(t *testing.T) {
	t.Parallel()

	eleven := make([]string, MaxItemRepositories+1)
	for i := range eleven {
		eleven[i] = "github.com/acme-labs/r" + string(rune('a'+i))
	}

	for name, repos := range map[string][]string{
		"not covered":       {"github.com/acme/web"},
		"other host":        {"gitlab.com/acme/api"},
		"owner only":        {"github.com/acme-labs"},
		"a pattern":         {"github.com/acme-labs/*"},
		"credentials":       {"https://x:y@github.com/acme/api"},
		"not https":         {"http://github.com/acme/api"},
		"empty":             {" "},
		"more than the cap": eleven,
	} {
		_, err := File(repoLine("api"), FileRequest{Origin: OriginPerson, Title: "t", At: fileAt, Repositories: repos})
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: File(%v) = %v; want ErrInvalid", name, repos, err)
		}
	}

	// Ten different ones, the cap, are accepted, and a repeat counts once.
	if n := fileRepos(t, repoLine(""), append(eleven[:10:10], eleven[0])...); len(n.Repositories) != MaxItemRepositories {
		t.Errorf("ten repositories = %d rows; want %d", len(n.Repositories), MaxItemRepositories)
	}
}

// An item that names no repository gets the default entry's, joined default; the shorthand's
// single entry is the default too.
func TestFile_DefaultRepositoryWhenNoneNamed(t *testing.T) {
	t.Parallel()

	want := []ItemRepository{{Repo: "github.com/acme/api", Joined: JoinedDefault}}
	if n := fileRepos(t, repoLine("api")); !reflect.DeepEqual(n.Repositories, want) {
		t.Errorf("Repositories = %+v; want %+v", n.Repositories, want)
	}
}

// Without a default entry, or without entries, an item that names none starts with none.
func TestFile_NoRepositoriesWithoutDefault(t *testing.T) {
	t.Parallel()

	if n := fileRepos(t, repoLine("")); n.Repositories != nil {
		t.Errorf("no default: Repositories = %+v; want none", n.Repositories)
	}

	if n := fileRepos(t, fileLine()); n.Repositories != nil {
		t.Errorf("no entries: Repositories = %+v; want none", n.Repositories)
	}

	if _, err := File(fileLine(), FileRequest{Origin: OriginPerson, Title: "t", At: fileAt, Repositories: []string{"github.com/acme/api"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a repository on a line without entries = %v; want ErrInvalid", err)
	}
}

// An item with no rows, filed before repositories or by an old instance, resolves to the line's
// default entry, and to none without one; an item's own rows win.
func TestResolveItemRepositories_OldItemUsesDefault(t *testing.T) {
	t.Parallel()

	got := ResolveItemRepositories(nil, repoLine("api"))
	if want := (ItemRepositories{{Repo: "github.com/acme/api", Joined: JoinedDefault}}); !reflect.DeepEqual(got, want) {
		t.Errorf("no rows = %+v; want %+v", got, want)
	}

	if got := ResolveItemRepositories(nil, repoLine("")); len(got) != 0 {
		t.Errorf("no rows, no default = %+v; want none", got)
	}

	if _, ok := ResolveItemRepositories(nil, repoLine("")).First(); ok {
		t.Error("First of none = ok; want none")
	}

	rows := []ItemRepository{{Repo: "github.com/acme-labs/web", Joined: JoinedFiling}}
	if got := ResolveItemRepositories(rows, repoLine("api")); !reflect.DeepEqual(got, ItemRepositories(rows)) {
		t.Errorf("own rows = %+v; want %+v", got, rows)
	}
}

// The first repository is the first named at filing, else the default entry's, else the first
// that joined (ADR 0041 decision 4).
func TestResolveItemRepositories_FirstIsFirstNamed(t *testing.T) {
	t.Parallel()

	push := ItemRepository{Repo: "github.com/acme-labs/push", Joined: JoinedPush, Ordinal: 0}
	def := ItemRepository{Repo: "github.com/acme/api", Joined: JoinedDefault, Ordinal: 1}
	named := ItemRepository{Repo: "github.com/acme-labs/web", Joined: JoinedFiling, Ordinal: 2}
	output := ItemRepository{Repo: "github.com/acme-labs/out", Joined: JoinedOutput, Ordinal: 3}

	for _, tc := range []struct {
		rows []ItemRepository
		want ItemRepository
	}{
		{[]ItemRepository{push, def, named}, named},
		{[]ItemRepository{push, def, output}, def},
		{[]ItemRepository{output, push}, output},
	} {
		got, ok := ResolveItemRepositories(tc.rows, repoLine("api")).First()
		if !ok || got != tc.want {
			t.Errorf("First(%+v) = %+v, %t; want %+v", tc.rows, got, ok, tc.want)
		}
	}
}
