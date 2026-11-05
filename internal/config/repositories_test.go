package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// The shorthand reads as one write entry, the default, named after the URL's last segment; a
// list's only entry that names one repository is the default, and an owner pattern never is.
func TestRuntime_RepositoryEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		runtime Runtime
		want    []RepositoryEntry
	}{
		{name: "no code", runtime: Runtime{}},
		{
			name:    "the shorthand",
			runtime: Runtime{Repository: &Repository{URL: "https://github.com/acme/Web-App.git", Token: "{{gh-token}}"}},
			want:    []RepositoryEntry{{Name: "web-app", URL: "https://github.com/acme/Web-App.git", Token: "{{gh-token}}", Access: AccessWrite, Default: true}},
		},
		{
			name:    "the shorthand of a public repository",
			runtime: Runtime{Repository: &Repository{URL: "https://gitlab.com/acme/api/"}},
			want:    []RepositoryEntry{{Name: "api", URL: "https://gitlab.com/acme/api/", Access: AccessWrite, Default: true}},
		},
		{
			name:    "a last segment that is too short for a slug",
			runtime: Runtime{Repository: &Repository{URL: "https://github.com/acme/ui"}},
			want:    []RepositoryEntry{{Name: "repo", URL: "https://github.com/acme/ui", Access: AccessWrite, Default: true}},
		},
		{
			name:    "a last segment with an underscore",
			runtime: Runtime{Repository: &Repository{URL: "https://github.com/acme/my_app"}},
			want:    []RepositoryEntry{{Name: "repo", URL: "https://github.com/acme/my_app", Access: AccessWrite, Default: true}},
		},
		{
			name:    "the only entry, one repository",
			runtime: Runtime{Repositories: []RepositoryEntry{{Name: "app", URL: "https://github.com/acme/app", Token: "{{gh}}", Access: AccessWrite}}},
			want:    []RepositoryEntry{{Name: "app", URL: "https://github.com/acme/app", Token: "{{gh}}", Access: AccessWrite, Default: true}},
		},
		{
			name:    "the only entry, an owner pattern",
			runtime: Runtime{Repositories: []RepositoryEntry{{Name: "acme", URL: "https://github.com/acme/*", Token: "{{gh}}", Access: AccessWrite}}},
			want:    []RepositoryEntry{{Name: "acme", URL: "https://github.com/acme/*", Token: "{{gh}}", Access: AccessWrite}},
		},
		{
			name: "several entries keep the default they say",
			runtime: Runtime{Repositories: []RepositoryEntry{
				{Name: "app", URL: "https://github.com/acme/app", Access: AccessRead},
				{Name: "api", URL: "https://github.com/acme/api", Token: "{{gh}}", Access: AccessWrite, Default: true},
			}},
			want: []RepositoryEntry{
				{Name: "app", URL: "https://github.com/acme/app", Access: AccessRead},
				{Name: "api", URL: "https://github.com/acme/api", Token: "{{gh}}", Access: AccessWrite, Default: true},
			},
		},
		{
			name: "several entries without a default",
			runtime: Runtime{Repositories: []RepositoryEntry{
				{Name: "app", URL: "https://github.com/acme/app", Access: AccessRead},
				{Name: "api", URL: "https://github.com/acme/api", Token: "{{gh}}", Access: AccessWrite},
			}},
			want: []RepositoryEntry{
				{Name: "app", URL: "https://github.com/acme/app", Access: AccessRead},
				{Name: "api", URL: "https://github.com/acme/api", Token: "{{gh}}", Access: AccessWrite},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.runtime.RepositoryEntries(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("entries =\n%+v\nwant\n%+v", got, tt.want)
			}

			want, wantOK := RepositoryEntry{}, false
			if i := slices.IndexFunc(tt.want, func(e RepositoryEntry) bool { return e.Default }); i >= 0 {
				want, wantOK = tt.want[i], true
			}

			if got, ok := tt.runtime.DefaultRepository(); ok != wantOK || got != want {
				t.Errorf("default = %+v, %v; want %+v, %v", got, ok, want, wantOK)
			}
		})
	}
}

// withRepos is baseFactory with runtime.repositories, its entries the YAML lines below it.
func withRepos(entries ...string) string {
	return edited(runtimeModel, runtimeModel+"    repositories:\n      "+strings.Join(entries, "\n      ")+"\n")
}

const (
	appEntry     = `- { name: app, url: "https://github.com/acme/app", token: "{{gh-token}}", access: write }`
	ownerEntry   = `- { name: acme, url: "https://github.com/acme/*", token: "{{gh-token}}", access: write }`
	mergeStation = "      - id: work\n        kind: human\n        action: merge #here\n"
	workStation  = "      - id: work\n        kind: agent\n        agent: worker\n"
)

// Publish refuses each repositories problem at its key; one write entry, a repository or an
// owner pattern, passes.
func TestCheck_Repositories(t *testing.T) {
	t.Parallel()

	rows := []envRow{
		{name: "one repository that writes", doc: withRepos(appEntry)},
		{name: "an owner pattern that writes", doc: withRepos(ownerEntry)},
		{name: "a merge station with the shorthand", doc: strings.Replace(
			edited(runtimeModel, runtimeModel+`    repository: { url: "https://github.com/acme/app", token: "{{gh-token}}" }`+"\n"), workStation,
			strings.Replace(mergeStation, " #here", "", 1), 1)},
		{
			name: "both forms",
			doc: edited(runtimeModel, runtimeModel+`    repository: { url: "https://github.com/acme/app" }`+"\n"+
				`    repositories: [{ name: app, url: "https://github.com/acme/app", token: "{{gh-token}}", access: write }] #here`+"\n"),
			code: codeRepositoryForms, file: FileName,
		},
		{
			name: "a name used twice",
			doc:  withRepos(appEntry, `- { name: app, url: "https://github.com/acme/api", token: "{{gh-token}}", access: write } #here`),
			code: codeDuplicateName, file: FileName,
		},
		{
			name: "write without a token",
			doc:  withRepos(`- { name: app, url: "https://github.com/acme/app", access: write } #here`),
			code: codeRepositoryToken, file: FileName,
		},
		{
			name: "two defaults",
			doc: withRepos(`- { name: app, url: "https://github.com/acme/app", token: "{{gh-token}}", access: write, default: true }`,
				`- { name: api, url: "https://github.com/acme/api", token: "{{gh-token}}", access: write, default: true } #here`),
			code: codeRepositoryDefaults, file: FileName,
		},
		{
			name: "a default on an owner pattern",
			doc:  withRepos(`- { name: acme, url: "https://github.com/acme/*", token: "{{gh-token}}", access: write, default: true } #here`),
			code: codeRepositoryDefaultPattern, file: FileName,
		},
		{
			name: "an owner without a pattern",
			doc:  withRepos(`- { name: acme, url: "https://github.com/acme", token: "{{gh-token}}", access: write } #here`),
			code: codeRepositoryURL, file: FileName,
		},
		{
			name: "a pattern without an owner",
			doc:  withRepos(`- { name: acme, url: "https://github.com/*", token: "{{gh-token}}", access: write } #here`),
			code: codeRepositoryURL, file: FileName,
		},
		{
			name: "a pattern in the middle",
			doc:  withRepos(`- { name: acme, url: "https://github.com/acme/*/app", token: "{{gh-token}}", access: write } #here`),
			code: codeRepositoryURL, file: FileName,
		},
		{
			name: "a merge station without repositories", doc: edited(workStation, mergeStation),
			code: codeMergeNeedsWrite, file: FileName,
		},
		{
			name: "a merge station whose agents only read",
			doc: strings.Replace(strings.Replace(withRepos(appEntry), agentPrompt, agentPrompt+"      code: read\n", 1),
				workStation, mergeStation, 1),
			code: codeMergeNeedsWrite, file: FileName,
		},
		{
			name: "integration branches without repositories",
			doc:  edited("        kind: done\n", "        kind: done\n    flow:\n      assemblies:\n        integration: { mode: branch } #here\n"),
			code: codeMergeNeedsWrite, file: FileName,
		},
		{
			name: "a second entry",
			doc:  withRepos(appEntry, `- { name: api, url: "https://github.com/acme/api", token: "{{gh-token}}", access: write } #here`),
			code: codeRepositoryNotSupported, file: FileName,
		},
		{
			name: "read access",
			doc:  withRepos(`- { name: app, url: "https://github.com/acme/app", token: "{{gh-token}}", access: read } #here`),
			code: codeRepositoryNotSupported, file: FileName,
		},
		{
			name: "an agent that only reads",
			doc:  strings.Replace(withRepos(appEntry), agentPrompt, agentPrompt+"      code: read #here\n", 1),
			code: codeRepositoryNotSupported, file: FileName,
		},
	}

	runEnvRows(t, rows)
}

// An agent that only reads warns when the only token for an entry that writes would be its
// token, because no read entry matches; a read entry that matches, one repository or an owner
// pattern, keeps it quiet, and so does an agent whose code is the default, write.
func TestCheck_ReaderWithWriteToken(t *testing.T) {
	t.Parallel()

	const reviewer = "    - name: reviewer\n      prompt: ./agents/worker.md\n      code: read\n"

	tests := []struct {
		name    string
		entries []string
		agents  string
		want    []string
	}{
		{
			name:    "a read entry for the same repository",
			entries: []string{appEntry, `- { name: app-read, url: "https://github.com/acme/app", token: "{{gh-read}}", access: read }`},
			agents:  reviewer,
		},
		{
			name:    "a read owner pattern over a write repository",
			entries: []string{appEntry, `- { name: acme-read, url: "https://github.com/acme/*", token: "{{gh-read}}", access: read }`},
			agents:  reviewer,
		},
		{
			name: "a write owner pattern and only one repository to read",
			entries: []string{
				`- { name: services, url: "https://github.com/acme/*", token: "{{gh-token}}", access: write }`,
				`- { name: app-read, url: "https://github.com/acme/app", token: "{{gh-read}}", access: read }`,
			},
			agents: reviewer,
			want:   []string{"reviewer only reads, but the only token for services can write."},
		},
		{
			name:    "a write owner pattern of another owner",
			entries: []string{`- { name: services, url: "https://github.com/acme/*", token: "{{gh-token}}", access: write }`, `- { name: other-read, url: "https://github.com/example/*", token: "{{gh-read}}", access: read }`},
			agents:  reviewer,
			want:    []string{"reviewer only reads, but the only token for services can write."},
		},
		{
			name:    "the default code writes",
			entries: []string{appEntry},
			agents:  "    - name: reviewer\n      prompt: ./agents/worker.md\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := strings.Replace(withRepos(tt.entries...), specAgents, specAgents+tt.agents, 1)

			_, problems := Check(checkTree(doc, nil))

			var got []string

			for _, p := range problems {
				if p.Code != codeReaderWriteToken {
					continue
				}

				if p.Severity != domain.SeverityWarning || p.Line == 0 {
					t.Errorf("problem = %+v, want a located warning", p)
				}

				got = append(got, p.Message)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("warnings = %q, want %q", got, tt.want)
			}
		})
	}
}

// Code resolves to write when the factory has an entry that writes, and to read otherwise.
func TestFactory_AgentCode(t *testing.T) {
	t.Parallel()

	write := &Factory{Spec: Spec{Runtime: Runtime{Repository: &Repository{URL: "https://github.com/acme/app"}}}}
	none := &Factory{}
	read := &Factory{Spec: Spec{Runtime: Runtime{Repositories: []RepositoryEntry{{Name: "app", URL: "https://github.com/acme/app", Access: AccessRead}}}}}

	for _, tt := range []struct {
		f    *Factory
		a    Agent
		want string
	}{
		{write, Agent{}, CodeWrite},
		{write, Agent{Code: CodeRead}, CodeRead},
		{none, Agent{}, CodeRead},
		{read, Agent{}, CodeRead},
	} {
		if got := tt.f.AgentCode(tt.a); got != tt.want {
			t.Errorf("AgentCode(%+v) = %s, want %s", tt.a, got, tt.want)
		}
	}
}
