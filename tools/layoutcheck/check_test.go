package main

import (
	"slices"
	"strings"
	"testing"
)

// The files of a small repository that every case starts from.
var baseFiles = []string{
	"AGENTS.md",
	"docs/adr/0013-repository-layout.md",
	"docs/engineering/architecture.md",
	".claude/rules/go.md",
	"cmd/goal/main.go",
	"internal/core/engine/pull.go",
	"internal/core/engine/pull_test.go",
	"packages/api-client/src/client.ts",
	"apps/web/src/a.test.tsx",
	"apps/worker/Dockerfile",
	"terraform/main.tf",
}

const baseAgents = "# Goal\n\nSee [ADR 0013](docs/adr/0013-repository-layout.md) and [the site](https://example.com/x).\n\n" +
	"## Repository map\n\n```\n" +
	".claude/   rules/ (path-scoped rules)\n" +
	"apps/      web/ (React SPA), worker/ (the agent image)\n" +
	"cmd/       binaries: goal (CLI)\n" +
	"docs/      adr/, engineering/\n" +
	"internal/  the Go packages (docs/engineering/architecture.md)\n" +
	"packages/  shared TypeScript: api-client/\n" +
	"terraform/ the platform's own infrastructure\n" +
	"```\n"

const baseADR = "# ADR 0013\n\n## Decision\n\n1. **The target layout.**\n\n   ```\n" +
	"   AGENTS.md                         entry docs\n" +
	"   cmd/ internal/                    Go\n" +
	"   apps/web/ apps/worker/            the pnpm apps\n" +
	"   packages/                         shared TypeScript\n" +
	"   docs/adr/                         decisions\n" +
	"   .claude/rules/                    rules\n" +
	"   ```\n\n" +
	"## History\n\n| Old path | New path |\n| --- | --- |\n| `web/` | `apps/web/` |\n| `terraform/` | `deploy/terraform/` |\n"

const baseRule = "---\npaths:\n  - \"**/*.go\"\n---\n\n# Go\n"

func baseRepo() repo {
	return repo{
		files:  slices.Clone(baseFiles),
		agents: baseAgents,
		adr:    baseADR,
		rules:  map[string]string{".claude/rules/go.md": baseRule},
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit func(r *repo)
		want []string
	}{
		"matching layout": {func(*repo) {}, nil},
		"top-level directory missing from map and layout": {
			func(r *repo) { r.files = append(r.files, "tools/layoutcheck/main.go") },
			[]string{
				"AGENTS.md: top-level directory missing from the repository map: tools/",
				"docs/adr/0013-repository-layout.md: top-level directory missing from the layout and the History: tools/",
			},
		},
		"top-level directory only in the history passes": {
			func(r *repo) {
				r.adr = baseADR + "| `design-system/goal/MASTER.md` | `docs/design/MASTER.md` |\n"
				r.files = append(r.files, "design-system/goal/MASTER.md")
				r.agents = replaceMapLine(r.agents, "docs/", "design-system/ goal/MASTER.md: the design system\ndocs/      adr/, engineering/\n")
			},
			nil,
		},
		"glob that matches nothing": {
			func(r *repo) {
				r.rules[".claude/rules/web.md"] = "---\npaths:\n  - \"apps/web/**\"\n  - 'apps/ui/**'\n  - \"**/*.test.tsx\"\n---\n"
			},
			[]string{".claude/rules/web.md: paths: glob matches no tracked file: apps/ui/**"},
		},
		"glob that is not valid": {
			func(r *repo) { r.rules[".claude/rules/bad.md"] = "---\npaths:\n  - \"apps/[web/**\"\n---\n" },
			[]string{".claude/rules/bad.md: paths: glob is not valid: apps/[web/**"},
		},
		"map token that does not exist": {
			func(r *repo) {
				r.agents = replaceMapLine(r.agents, "packages/", "packages/  shared TypeScript: contracts/ (generated), api-client/, ui/, config/\n")
			},
			[]string{
				"AGENTS.md: names a path that does not exist: packages/contracts/",
				"AGENTS.md: names a path that does not exist: packages/ui/",
				"AGENTS.md: names a path that does not exist: packages/config/",
			},
		},
		"map token resolves at the root, and a route is not a path": {
			func(r *repo) {
				r.agents = replaceMapLine(r.agents, "apps/", "apps/      web/ (React SPA, reached through /api/auth), worker/, see `docs/adr/0013-repository-layout.md`.\n")
			},
			nil,
		},
		"map directory that does not exist": {
			func(r *repo) {
				r.agents = replaceMapLine(r.agents, "cmd/", "cmd/       binaries\nbin/       old binaries\n")
			},
			[]string{"AGENTS.md: names a path that does not exist: bin/"},
		},
		"history path in the map is exempt": {
			func(r *repo) {
				r.agents = replaceMapLine(r.agents, "cmd/", "cmd/       binaries, once web/assets/ (moved)\n")
			},
			nil,
		},
		"link that does not exist": {
			func(r *repo) {
				r.agents += "\nRead [the guide](docs/engineering/frontend.md#tests), [here](#map), [mail](mailto:a@example.com) and [old](web/README.md).\n"
			},
			[]string{"AGENTS.md: names a path that does not exist: docs/engineering/frontend.md"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := baseRepo()
			tt.edit(&r)

			var got []string
			for _, p := range check(r) {
				got = append(got, p.String())
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("problems =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// replaceMapLine replaces the map line that starts with dir.
func replaceMapLine(agents, dir, with string) string {
	var out strings.Builder

	for line := range strings.Lines(agents) {
		if strings.HasPrefix(line, dir) {
			line = with
		}

		out.WriteString(line)
	}

	return out.String()
}

func TestMatchGlob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		glob, path string
		want       bool
	}{
		{"**/Dockerfile", "Dockerfile", true},
		{"**/Dockerfile", "apps/worker/Dockerfile", true},
		{"**/Dockerfile", "apps/worker/Dockerfile.dockerignore", false},
		{"apps/web/**", "apps/web/src/a.tsx", true},
		{"apps/web/**", "apps/webx/a.tsx", false},
		{"apps/web/**", "apps/web", true},
		{"**/*.test.tsx", "apps/web/src/a.test.tsx", true},
		{"**/*.test.tsx", "apps/web/src/a.test.ts", false},
		{"**/*.md", "AGENTS.md", true},
		{"*.md", "docs/a.md", false},
		{"db/sqlc.yaml", "db/sqlc.yaml", true},
		{"internal/**/gen/*.go", "internal/api/gen/api.go", true},
		{"internal/**/gen/*.go", "internal/gen/x/api.go", false},
	}

	for _, tt := range tests {
		if got := matchGlob(tt.glob, tt.path); got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.glob, tt.path, got, tt.want)
		}
	}
}
