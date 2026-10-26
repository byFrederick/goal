package main

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// A planCase is a set of changed paths and the plan they must give.
type planCase struct {
	changed []string
	want    []string
	reason  string // a part of one reason
	note    string // a part of one note, or "" for none
}

const (
	envCheck = "env:check"
	genCheck = "gen:check"
	webCheck = "web:check"
	cfCheck  = "cloudflare:check"
)

var (
	goTasks     = []string{"lint", "test", "gen:check", "validate"}
	layoutTasks = []string{"layout:check"}
	envTasks    = []string{envCheck}
	// goEnvTasks are a Go change's: Go code may read the environment.
	goEnvTasks = append(slices.Clone(envTasks), goTasks...)
)

// includedTaskfiles returns the taskfile: path of every include of the repository's root Taskfile.yml.
func includedTaskfiles(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile("../../Taskfile.yml")
	if err != nil {
		t.Fatalf("read the root Taskfile: %v", err)
	}

	var root struct {
		Includes map[string]struct {
			Taskfile string `yaml:"taskfile"`
		} `yaml:"includes"`
	}
	if err := yaml.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse the root Taskfile: %v", err)
	}

	var paths []string
	for _, inc := range root.Includes {
		paths = append(paths, inc.Taskfile)
	}

	if len(paths) == 0 {
		t.Fatal("the root Taskfile includes no area Taskfile")
	}

	slices.Sort(paths)

	return paths
}

// sharedCases gives a case for each file shared by every part: its change runs all of tasks.
func sharedCases(t *testing.T, tasks []string) map[string]planCase {
	t.Helper()

	shared := []string{"go.mod", "go.sum", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "Taskfile.yml", ".golangci.yml", ".mise.toml"}
	// Every area Taskfile the root includes, read from its includes: so a new area is covered.
	shared = append(shared, includedTaskfiles(t)...)

	cases := map[string]planCase{}

	for _, p := range shared {
		note := ""
		if strings.HasPrefix(p, "apps/auth/") {
			note = "task migrate-check also applies"
		}

		cases["shared "+p] = planCase{[]string{"apps/web/src/a.tsx", p}, tasks, "everything (" + strings.Join(tasks, ", ") + "): " + p + " (shared by every part)", note}
	}

	return cases
}

func TestPlan(t *testing.T) {
	t.Parallel()

	tests := map[string]planCase{
		"web only":                {[]string{"apps/web/src/routes/board.tsx", "apps/web/package.json"}, []string{webCheck}, "web (web:check): apps/web/src/routes/board.tsx and 1 more", ""},
		"shared tsconfig":         {[]string{"packages/config/tsconfig.base.json"}, []string{"worker:check", "auth:check", "api-client:check", webCheck, cfCheck}, "shared by every TypeScript part", ""},
		"go file":                 {[]string{"internal/engine/pull.go"}, goEnvTasks, "go (lint, test, gen:check, validate): internal/engine/pull.go", ""},
		"cli command":             {[]string{"internal/cli/main.go"}, goEnvTasks, "go (lint, test, gen:check, validate): internal/cli/main.go", ""},
		"go file outside go dirs": {[]string{"apps/web/webui.go"}, goEnvTasks, "apps/web/webui.go", ""},
		"go fixture":              {[]string{"internal/config/testdata/acme/factory.yaml"}, goTasks, "go (", ""},
		"queries":                 {[]string{"db/queries/items.sql"}, goTasks, "db/queries/items.sql", ""},
		"sqlc config":             {[]string{"db/sqlc.yaml"}, goTasks, "db/sqlc.yaml", ""},
		"embedded csp":            {[]string{"apps/web/csp.txt"}, goTasks, "apps/web/csp.txt", ""},
		"embedded placeholder":    {[]string{"apps/web/placeholder/index.html"}, goTasks, "apps/web/placeholder/index.html", ""},
		"embedded connection kind": {
			[]string{"internal/connkind/kinds/github.yaml"}, goTasks, "internal/connkind/kinds/github.yaml", "",
		},
		"embedded roles":   {[]string{"internal/adapters/postgres/roles.sql"}, goTasks, "roles.sql", ""},
		"migration":        {[]string{"db/migrations/00060_x.sql"}, goTasks, "db/migrations/00060_x.sql", "db/migrations/00060_x.sql changed: task migrate-check also applies"},
		"compose read":     {[]string{"compose.yaml"}, goEnvTasks, "env (env:check): compose.yaml", ""},
		"env example read": {[]string{".env.example"}, goEnvTasks, "env (env:check): .env.example", ""},
		"worker":           {[]string{"apps/worker/src/run.ts", "apps/worker/Dockerfile"}, []string{envCheck, "worker:check"}, "worker (worker:check): apps/worker/src/run.ts and 1 more", ""},
		"auth":             {[]string{"apps/auth/src/server.ts"}, []string{envCheck, "auth:check"}, "auth (auth:check): apps/auth/src/server.ts", "apps/auth/src/server.ts changed: task migrate-check also applies"},
		"generated roles":  {[]string{"apps/auth/src/access.gen.ts"}, []string{envCheck, genCheck, "auth:check"}, "gen (gen:check): apps/auth/src/access.gen.ts", "migrate-check"},
		"generated permission table": {
			[]string{"internal/api/permissions.gen.go"}, goEnvTasks, "gen (gen:check): internal/api/permissions.gen.go", "",
		},
		"generated api client types": {
			[]string{"packages/api-client/src/schema.gen.ts"}, []string{genCheck, "api-client:check", webCheck}, "gen (gen:check): packages/api-client/src/schema.gen.ts", "",
		},
		"generated route tree": {[]string{"apps/web/src/routeTree.gen.ts"}, []string{genCheck, webCheck}, "gen (gen:check): apps/web/src/routeTree.gen.ts", ""},
		"generated queries":    {[]string{"internal/adapters/postgres/db/runs.sql.gen.go"}, goEnvTasks, "gen (gen:check): internal/adapters/postgres/db/runs.sql.gen.go", ""},
		"api client":           {[]string{"packages/api-client/src/client.ts"}, []string{"api-client:check", webCheck}, "web (web:check): packages/api-client/src/client.ts", ""},
		"control worker":       {[]string{"apps/control-worker/src/index.ts"}, []string{envCheck, cfCheck}, "cloudflare (cloudflare:check): apps/control-worker/src/index.ts", ""},
		"deploy scripts":       {[]string{"deploy/cloudflare/scripts/deploy.sh"}, []string{envCheck, cfCheck}, "cloudflare (cloudflare:check): deploy/cloudflare/scripts/deploy.sh", ""},
		"terraform":            {[]string{"deploy/terraform/r2.tf"}, []string{"tf:check"}, "terraform (tf:check): deploy/terraform/r2.tf", ""},
		"sops rules":           {[]string{"deploy/terraform/.sops.yaml"}, []string{"tf:check"}, "terraform (tf:check): deploy/terraform/.sops.yaml", ""},
		"agents map":           {[]string{"AGENTS.md"}, layoutTasks, "layout (layout:check): AGENTS.md", ""},
		"layout adr":           {[]string{"docs/adr/0013-repository-layout.md"}, layoutTasks, "layout (layout:check): docs/adr/0013-repository-layout.md", ""},
		"rule globs":           {[]string{".claude/rules/go.md"}, layoutTasks, "layout (layout:check): .claude/rules/go.md", ""},
		"layout check": {
			[]string{"tools/layoutcheck/check.go", "tools/layoutcheck/testdata/x.md"},
			append(slices.Clone(layoutTasks), goTasks...), "layout (layout:check): tools/layoutcheck/check.go and 1 more", "",
		},
		"env check": {
			[]string{"tools/envcheck/check.go", "tools/envcheck/testdata/repo/compose.yaml", "tools/envcheck/testdata/repo/apps/web/Taskfile.yml"},
			goEnvTasks, "env (env:check): tools/envcheck/check.go and 2 more", "",
		},
		"repository tools": {
			[]string{"tools/permgen/table.go", "tools/checkplan/testdata/x.txt"},
			goEnvTasks, "env (env:check): tools/permgen/table.go", "",
		},
		"env reference":     {[]string{"docs/reference/environment.md"}, envTasks, "env (env:check): docs/reference/environment.md", ""},
		"wrangler bindings": {[]string{"deploy/cloudflare/wrangler.jsonc"}, []string{envCheck, cfCheck}, "env (env:check): deploy/cloudflare/wrangler.jsonc", ""},
		"nothing to check": {
			[]string{"docs/adr/0001.md", "docs/design/MASTER.md", ".claude/skills/x/SKILL.md", ".github/workflows/ci.yml", ".githooks/pre-push", "README.md", "skills-lock.json"},
			nil, "", "",
		},
		"parts keep task check's order": {
			[]string{"deploy/terraform/r2.tf", "apps/web/src/a.tsx", "internal/api/org.go", "apps/worker/src/a.ts"},
			[]string{envCheck, "lint", "test", "gen:check", "worker:check", webCheck, "validate", "tf:check"},
			"worker (worker:check)", "",
		},
	}

	maps.Copy(tests, sharedCases(t, tasks))

	tests["contract api"] = struct {
		changed []string
		want    []string
		reason  string
		note    string
	}{[]string{"contracts/api/openapi.yaml"}, tasks, "everything (" + strings.Join(tasks, ", ") + "): contracts/api/openapi.yaml (a contract", ""}
	tests["contract schema"] = struct {
		changed []string
		want    []string
		reason  string
		note    string
	}{[]string{"contracts/definitions/factory.schema.json"}, tasks, "contracts/definitions/factory.schema.json (a contract", ""}

	for _, unknown := range []string{"Makefile", "packages/ui/src/button.tsx", "deploy/aws/main.tf", ".editorconfig"} {
		tests["unknown "+unknown] = planCase{[]string{unknown}, tasks, "everything (" + strings.Join(tasks, ", ") + "): " + unknown + " (matches no rule)", ""}
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := planFor(tt.changed)

			if !slices.Equal(got.tasks, tt.want) {
				t.Errorf("tasks = %v, want %v", got.tasks, tt.want)
			}

			if tt.want != nil && !containsPart(got.reasons, tt.reason) {
				t.Errorf("reasons = %q, want one containing %q", got.reasons, tt.reason)
			}

			if tt.want == nil && len(got.reasons) != 0 {
				t.Errorf("reasons = %q, want none", got.reasons)
			}

			if tt.note == "" && len(got.notes) != 0 {
				t.Errorf("notes = %q, want none", got.notes)
			}

			if tt.note != "" && !containsPart(got.notes, tt.note) {
				t.Errorf("notes = %q, want one containing %q", got.notes, tt.note)
			}
		})
	}
}

func containsPart(lines []string, part string) bool {
	return slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, part) })
}
