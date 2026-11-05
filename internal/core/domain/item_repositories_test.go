package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The repository tests' entries: one repository, an owner pattern and a nested owner's pattern.
const (
	repoAPI  = "https://github.com/acme/api.git"
	repoAll  = "https://github.com/acme-labs/*"
	repoDeep = "https://gitlab.example.com/acme/platform/*"

	// entryAPI is the repoAPI entry's name and keyAPI its key.
	entryAPI  = "api"
	entryLabs = "labs"
	keyAPI    = "github.com/acme/api"
	// keyLabsWeb is a repository under the repoAll pattern.
	keyLabsWeb = "github.com/acme-labs/web"
)

// repoLine is fileLine with runtime.repositories; def names the default entry ("" for none).
func repoLine(def string) Line {
	l := fileLine()
	l.Runtime.Repositories = []RepositoryEntry{
		{Name: entryAPI, URL: repoAPI, Access: "write", Default: def == entryAPI},
		{Name: entryLabs, URL: repoAll, Access: "read"},
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

	n := fileRepos(t, repoLine(entryAPI),
		"github.com/Acme-Labs/Web", "https://GitHub.com/acme/API", "github.com/acme/api.git",
		"https://gitlab.example.com/acme/platform/infra/deploy.git", "github.com/acme-labs/web/")

	want := []ItemRepository{
		{Repo: keyLabsWeb, Joined: JoinedFiling},
		{Repo: keyAPI, Joined: JoinedFiling, Ordinal: 1},
		{Repo: "gitlab.example.com/acme/platform/infra/deploy", Joined: JoinedFiling, Ordinal: 2},
	}
	if !reflect.DeepEqual(n.Repositories, want) {
		t.Errorf("Repositories = %+v\nwant %+v", n.Repositories, want)
	}

	// An entry's name gives its repository, and owner/name or group/sub/name without a host is
	// matched under each entry's host; values that resolve to the same key count once.
	n = fileRepos(t, repoLine(""), "Acme-Labs/web", entryAPI, "acme/platform/infra/deploy", "acme/api", "github.com/acme/api")

	want = []ItemRepository{
		{Repo: keyLabsWeb, Joined: JoinedFiling},
		{Repo: keyAPI, Joined: JoinedFiling, Ordinal: 1},
		{Repo: "gitlab.example.com/acme/platform/infra/deploy", Joined: JoinedFiling, Ordinal: 2},
	}
	if !reflect.DeepEqual(n.Repositories, want) {
		t.Errorf("names and owner/name: Repositories = %+v\nwant %+v", n.Repositories, want)
	}
}

// Every refusal quotes the value as the person gave it; naming an owner pattern's entry says to
// name owner/name, and an owner/name more than one host covers lists the hosts.
func TestFile_RepositoriesRefusalNamesTheValue(t *testing.T) {
	t.Parallel()

	ambiguous := repoLine("")
	ambiguous.Runtime.Repositories = append(ambiguous.Runtime.Repositories,
		RepositoryEntry{Name: "mirror", URL: "https://git.example.com/acme/*", Access: "read"})

	for name, tc := range map[string]struct {
		line Line
		repo string
		want []string
	}{
		"unknown name":  {repoLine(""), "Web", []string{`"Web"`}},
		"pattern entry": {repoLine(""), entryLabs, []string{`"labs"`, "owner/name"}},
		"unknown owner": {repoLine(""), "Acme/Web", []string{`"Acme/Web"`}},
		"unknown host":  {repoLine(""), "GitHub.com/acme/web", []string{`"GitHub.com/acme/web"`}},
		"two hosts":     {ambiguous, "acme/api", []string{`"acme/api"`, gitHubDotCom, hostGitAcme}},
		"owner only":    {repoLine(""), "acme-labs", []string{`"acme-labs"`}},
		"malformed":     {repoLine(""), "https://x:y@github.com/acme/api", []string{`"https://x:y@github.com/acme/api"`}},
	} {
		_, err := File(tc.line, FileRequest{Origin: OriginPerson, Title: "t", At: fileAt, Repositories: []string{tc.repo}})
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: File(%q) = %v; want ErrInvalid", name, tc.repo, err)
			continue
		}

		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q does not name %s", name, err, w)
			}
		}
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
		"not covered":        {"github.com/acme/web"},
		"other host":         {"gitlab.com/acme/api"},
		"owner only":         {"github.com/acme-labs"},
		"a pattern":          {"github.com/acme-labs/*"},
		"credentials":        {"https://x:y@github.com/acme/api"},
		"not https":          {"http://github.com/acme/api"},
		"blank":              {" "},
		"owner/name nowhere": {"acme/web"},
		"a pattern's name":   {entryLabs},
		"an unknown name":    {"mobile-app"},
		"more than the cap":  eleven,
	} {
		_, err := File(repoLine(entryAPI), FileRequest{Origin: OriginPerson, Title: "t", At: fileAt, Repositories: repos})
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

	want := []ItemRepository{{Repo: keyAPI, Joined: JoinedDefault}}
	if n := fileRepos(t, repoLine(entryAPI)); !reflect.DeepEqual(n.Repositories, want) {
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

	if _, err := File(fileLine(), FileRequest{Origin: OriginPerson, Title: "t", At: fileAt, Repositories: []string{keyAPI}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a repository on a line without entries = %v; want ErrInvalid", err)
	}
}

// An item with no rows, filed before repositories or by an old instance, resolves to the line's
// default entry, and to none without one; an item's own rows win.
func TestResolveItemRepositories_OldItemUsesDefault(t *testing.T) {
	t.Parallel()

	got := ResolveItemRepositories(nil, repoLine(entryAPI))
	if want := (ItemRepositories{{Repo: keyAPI, Joined: JoinedDefault}}); !reflect.DeepEqual(got, want) {
		t.Errorf("no rows = %+v; want %+v", got, want)
	}

	if got := ResolveItemRepositories(nil, repoLine("")); len(got) != 0 {
		t.Errorf("no rows, no default = %+v; want none", got)
	}

	if _, ok := ResolveItemRepositories(nil, repoLine("")).First(); ok {
		t.Error("First of none = ok; want none")
	}

	rows := []ItemRepository{{Repo: keyLabsWeb, Joined: JoinedFiling}}
	if got := ResolveItemRepositories(rows, repoLine(entryAPI)); !reflect.DeepEqual(got, ItemRepositories(rows)) {
		t.Errorf("own rows = %+v; want %+v", got, rows)
	}
}

// The first repository is the first named at filing, else the default entry's, else the first
// that joined (ADR 0041 decision 4).
func TestResolveItemRepositories_FirstIsFirstNamed(t *testing.T) {
	t.Parallel()

	push := ItemRepository{Repo: "github.com/acme-labs/push", Joined: JoinedPush, Ordinal: 0}
	def := ItemRepository{Repo: keyAPI, Joined: JoinedDefault, Ordinal: 1}
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
		got, ok := ResolveItemRepositories(tc.rows, repoLine(entryAPI)).First()
		if !ok || got != tc.want {
			t.Errorf("First(%+v) = %+v, %t; want %+v", tc.rows, got, ok, tc.want)
		}
	}
}

// ownerAPI is keyAPI's owner/name.
const ownerAPI = "acme/api"

// A trigger's or a check's repository value resolves against the line's entries, owner
// patterns included and in any case: an entry's name, host/owner/name, an https URL or
// owner/name, on the sender's own host when entries on several hosts cover it. An empty value
// and one no entry covers resolve to nothing.
func TestTriggerRepositories_Resolve(t *testing.T) {
	t.Parallel()

	twoHosts := fileLine()
	twoHosts.Runtime.Repositories = []RepositoryEntry{
		{Name: "lab", URL: "https://gitlab.com/acme/*"},
		{Name: "hub", URL: "https://github.com/acme/*"},
	}

	tests := []struct {
		name   string
		l      Line
		value  string
		sender WebhookSender
		want   string
	}{
		{"an entry's name", repoLine(""), entryAPI, "", keyAPI},
		{"an entry's name in any case", repoLine(""), "API", "", keyAPI},
		{"an owner pattern's name", repoLine(""), entryLabs, "", ""},
		{"host/owner/name", repoLine(""), "GitHub.com/acme/api", "", keyAPI},
		{"an https URL with .git", repoLine(""), "https://github.com/acme/API.git", "", keyAPI},
		{"owner/name", repoLine(""), ownerAPI, SenderGitHub, keyAPI},
		{"owner/name an owner pattern covers", repoLine(""), "Acme-Labs/Web", SenderGitHub, keyLabsWeb},
		{"a nested owner's owner/name", repoLine(""), "acme/platform/infra", SenderGitLab, "gitlab.example.com/acme/platform/infra"},
		{"owner/name on the sender's host", twoHosts, "acme/app", SenderGitHub, "github.com/acme/app"},
		{"owner/name on the other sender's host", twoHosts, "acme/app", SenderGitLab, "gitlab.com/acme/app"},
		{"owner/name on the first entry's host", twoHosts, "acme/app", SenderPagerDuty, "gitlab.com/acme/app"},
		{"a repository no entry covers", repoLine(""), "byFrederick/legacy", SenderGitHub, ""},
		{"a URL no entry covers", repoLine(""), "https://example.com/acme/api", "", ""},
		{"an empty value", repoLine(""), " ", "", ""},
		{"a line without entries", fileLine(), ownerAPI, SenderGitHub, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tc.l.ResolveRepository(tc.value, tc.sender)
			if got != tc.want || ok != (tc.want != "") {
				t.Errorf("ResolveRepository(%q) = %q, %v; want %q", tc.value, got, ok, tc.want)
			}
		})
	}
}

// Resolving a list skips empty values, keeps each match once, and stops at the first value no
// entry covers with NotTheFactorysRepository's reason.
func TestTriggerRepositories_ResolveList(t *testing.T) {
	t.Parallel()

	l := repoLine("")
	l.Name = "SRE"

	keys, skip := l.ResolveRepositories([]string{"", ownerAPI, entryAPI, "acme-labs/web"}, SenderGitHub)
	if want := []string{keyAPI, keyLabsWeb}; !reflect.DeepEqual(keys, want) || skip != "" {
		t.Errorf("ResolveRepositories = %v, %q; want %v", keys, skip, want)
	}

	if keys, _ := l.ResolveRepositories([]string{"", " "}, SenderPagerDuty); keys != nil {
		t.Errorf("empty values resolve to %v, want none", keys)
	}

	_, skip = l.ResolveRepositories([]string{ownerAPI, "byFrederick/legacy"}, SenderGitHub)
	if want := "byFrederick/legacy is not one of SRE's repositories"; skip != want {
		t.Errorf("skip = %q, want %q", skip, want)
	}
}
