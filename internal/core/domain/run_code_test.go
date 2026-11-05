package domain

import (
	"reflect"
	"testing"
)

// The run code tests' entries: one repository that writes, the owner's every repository read
// only, and a nested owner's repositories that write.
const (
	codeApp   = "https://github.com/acme/app"
	codeAcme  = "https://github.com/acme/*"
	codeDeep  = "https://gitlab.example.com/acme/platform/*"
	keyApp    = "github.com/acme/app"
	keyLib    = "github.com/acme/lib"
	keyDeepUI = "gitlab.example.com/acme/platform/ui"

	// prefixAcme and prefixDeep are the owner patterns' prefixes.
	prefixAcme = "https://github.com/acme/"
	prefixDeep = "https://gitlab.example.com/acme/platform/"

	namePlatform = "platform"
	nameAll      = "all"

	// The entries' tokens, {{secret}} references.
	tokApp      = "{{app-write}}"
	tokAcme     = "{{acme-read}}"
	tokPlatform = "{{platform}}"
)

// codeEntries: app writes with its token, acme reads with its own, platform writes.
func codeEntries() []RepositoryEntry {
	return []RepositoryEntry{
		{Name: appName, URL: codeApp, Token: tokApp, Access: AccessWrite, Default: true},
		{Name: acmeOwner, URL: codeAcme, Token: tokAcme, Access: AccessRead},
		{Name: namePlatform, URL: codeDeep, Token: tokPlatform, Access: AccessWrite},
	}
}

func codeLine(entries ...RepositoryEntry) Line {
	return Line{Runtime: Runtime{Repositories: entries}}
}

func joined(keys ...string) ItemRepositories {
	out := make(ItemRepositories, len(keys))
	for i, k := range keys {
		out[i] = ItemRepository{Repo: k, Joined: JoinedFiling, Ordinal: i}
	}

	return out
}

func TestRunCode_ClonesInJoinedOrderWithEachAccess(t *testing.T) {
	t.Parallel()

	l := codeLine(codeEntries()...)

	tests := []struct {
		name  string
		code  string
		repos ItemRepositories
		want  []RunClone
	}{
		{
			name: "the first at /workspace, each other under /repos", code: CodeWrite, repos: joined(keyApp, keyLib, keyDeepUI),
			want: []RunClone{
				{Repo: keyApp, URL: "https://" + keyApp, Path: WorkspacePath, Token: tokApp, Write: true},
				{Repo: keyLib, URL: "https://" + keyLib, Path: "/repos/acme/lib", Token: tokAcme},
				{Repo: keyDeepUI, URL: "https://" + keyDeepUI, Path: "/repos/acme/platform/ui", Token: tokPlatform, Write: true},
			},
		},
		{
			name: "an agent that only reads writes none", code: CodeRead, repos: joined(keyLib, keyApp),
			want: []RunClone{
				{Repo: keyLib, URL: "https://" + keyLib, Path: WorkspacePath, Token: tokAcme},
				{Repo: keyApp, URL: "https://" + keyApp, Path: "/repos/acme/app", Token: tokAcme},
			},
		},
		{
			name: "an agent without code writes, as before code", repos: joined(keyApp),
			want: []RunClone{{Repo: keyApp, URL: "https://" + keyApp, Path: WorkspacePath, Token: tokApp, Write: true}},
		},
		{name: "an item with none clones none", code: CodeWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := l.RunCode(AgentSpec{Name: "builder", Code: tt.code}, tt.repos).Clones
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clones = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRunCode_NoRowsUseTheDefaultEntry(t *testing.T) {
	t.Parallel()

	l := codeLine(codeEntries()...)

	got := l.RunCode(AgentSpec{Code: CodeWrite}, ResolveItemRepositories(nil, l)).Clones
	want := []RunClone{{Repo: keyApp, URL: "https://" + keyApp, Path: WorkspacePath, Token: tokApp, Write: true}}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("clones = %+v, want %+v", got, want)
	}
}

func TestRunCode_NoRowsAndNoDefaultCloneNothing(t *testing.T) {
	t.Parallel()

	entries := codeEntries()
	entries[0].Default = false
	l := codeLine(entries...)

	code := l.RunCode(AgentSpec{Code: CodeWrite}, ResolveItemRepositories(nil, l))
	if len(code.Clones) != 0 {
		t.Errorf("clones = %+v, want none", code.Clones)
	}

	if len(code.Logins) != len(entries) {
		t.Errorf("logins = %+v, want one per entry", code.Logins)
	}
}

func TestRunCode_LoginsPickEachEntrysToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []RepositoryEntry
		code    string
		want    []RunLogin
	}{
		{
			name: "a writer gets each entry's own token", entries: codeEntries(), code: CodeWrite,
			want: []RunLogin{
				{Prefix: codeApp, Entry: appName, TokenEntry: appName, Token: tokApp, Write: true},
				{Prefix: prefixAcme, Entry: acmeOwner, TokenEntry: acmeOwner, Token: tokAcme},
				{Prefix: prefixDeep, Entry: namePlatform, TokenEntry: namePlatform, Token: tokPlatform, Write: true},
			},
		},
		{
			name: "a reader takes a matching read entry's token", entries: codeEntries(), code: CodeRead,
			want: []RunLogin{
				{Prefix: codeApp, Entry: appName, TokenEntry: acmeOwner, Token: tokAcme},
				{Prefix: prefixAcme, Entry: acmeOwner, TokenEntry: acmeOwner, Token: tokAcme},
				{Prefix: prefixDeep, Entry: namePlatform, TokenEntry: namePlatform, Token: tokPlatform},
			},
		},
		{
			name: "one repository before an owner pattern, and a deeper owner before its parent",
			entries: []RepositoryEntry{
				{Name: nameAll, URL: "https://gitlab.example.com/acme/*", Token: "{{all}}", Access: AccessWrite},
				{Name: namePlatform, URL: codeDeep, Token: tokPlatform, Access: AccessWrite},
				{Name: "ui", URL: "https://gitlab.example.com/acme/platform/ui.git", Token: "{{ui}}", Access: AccessWrite},
			},
			code: CodeWrite,
			want: []RunLogin{
				{Prefix: "https://gitlab.example.com/acme/", Entry: nameAll, TokenEntry: nameAll, Token: "{{all}}", Write: true},
				{Prefix: prefixDeep, Entry: namePlatform, TokenEntry: namePlatform, Token: tokPlatform, Write: true},
				{Prefix: "https://gitlab.example.com/acme/platform/ui", Entry: "ui", TokenEntry: "ui", Token: "{{ui}}", Write: true},
			},
		},
		{
			name:    "an entry without a token signs in nowhere",
			entries: []RepositoryEntry{{Name: docsName, URL: "https://github.com/acme/docs", Access: AccessRead}},
			code:    CodeWrite,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := codeLine(tt.entries...).RunCode(AgentSpec{Code: tt.code}, nil).Logins
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("logins = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestRunCode_ReadOnlyPrefixes(t *testing.T) {
	t.Parallel()

	l := codeLine(codeEntries()...)

	tests := []struct {
		name string
		code string
		want []string
	}{
		{name: "a writer may not write a read entry", code: CodeWrite, want: []string{prefixAcme}},
		{
			name: "a reader may write none", code: CodeRead,
			want: []string{codeApp, prefixAcme, prefixDeep},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := l.RunCode(AgentSpec{Code: tt.code}, nil).ReadOnly
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("read only = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunCode_TokenEntryPicksTheMostSpecific(t *testing.T) {
	t.Parallel()

	entries := codeEntries()

	tests := []struct {
		target string
		reads  bool
		want   string
		found  bool
	}{
		{target: keyApp, want: appName, found: true},
		{target: codeApp + ".git", want: appName, found: true},
		{target: keyApp, reads: true, want: acmeOwner, found: true},
		{target: keyLib, want: acmeOwner, found: true},
		{target: codeAcme, want: acmeOwner, found: true},
		{target: keyDeepUI, reads: true, want: namePlatform, found: true},
		{target: "github.com/other/app"},
		{target: "not a url"},
	}

	for _, tt := range tests {
		got, ok := TokenEntry(entries, tt.target, tt.reads)
		if ok != tt.found || got.Name != tt.want {
			t.Errorf("TokenEntry(%q, reads %v) = %q, %v; want %q, %v", tt.target, tt.reads, got.Name, ok, tt.want, tt.found)
		}
	}
}

func TestRunCode_RepositoryTokenEnvNamesTheEntry(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{appName: "GOAL_REPOSITORY_TOKEN_APP", "acme-labs": "GOAL_REPOSITORY_TOKEN_ACME_LABS"} {
		if got := RepositoryEntryTokenEnv(name); got != want {
			t.Errorf("RepositoryEntryTokenEnv(%q) = %q, want %q", name, got, want)
		}
	}
}
