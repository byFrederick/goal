package main

import (
	"slices"
	"strings"
	"testing"
)

// A planCase is a set of changed paths and the plan they must give.
type planCase struct {
	changed []string
	want    []string
	reason  string // a part of one reason
	note    string // a part of one note, or "" for none
}

var goTasks = []string{"lint", "test", "api:permissions-check", "validate"}

func TestPlan(t *testing.T) {
	t.Parallel()

	tests := map[string]planCase{
		"web only":                {[]string{"web/src/routes/board.tsx", "web/package.json"}, []string{"web:check"}, "web (web:check): web/src/routes/board.tsx and 1 more", ""},
		"go file":                 {[]string{"internal/engine/pull.go"}, goTasks, "go (lint, test, api:permissions-check, validate): internal/engine/pull.go", ""},
		"go file outside go dirs": {[]string{"web/webui.go"}, goTasks, "web/webui.go", ""},
		"go fixture":              {[]string{"internal/config/testdata/acme/factory.yaml"}, goTasks, "go (", ""},
		"queries":                 {[]string{"queries/items.sql"}, goTasks, "queries/items.sql", ""},
		"sqlc config":             {[]string{"sqlc.yaml"}, goTasks, "sqlc.yaml", ""},
		"embedded csp":            {[]string{"web/csp.txt"}, goTasks, "web/csp.txt", ""},
		"embedded placeholder":    {[]string{"web/placeholder/index.html"}, goTasks, "web/placeholder/index.html", ""},
		"embedded connection kind": {
			[]string{"internal/connkind/kinds/github.yaml"}, goTasks, "internal/connkind/kinds/github.yaml", "",
		},
		"embedded roles":   {[]string{"internal/adapters/postgres/roles.sql"}, goTasks, "roles.sql", ""},
		"migration":        {[]string{"migrations/00060_x.sql"}, goTasks, "migrations/00060_x.sql", "migrations/00060_x.sql changed: task migrate-check also applies"},
		"compose read":     {[]string{"compose.yaml"}, goTasks, "compose.yaml", ""},
		"env example read": {[]string{".env.example"}, goTasks, ".env.example", ""},
		"worker":           {[]string{"worker/src/run.ts", "worker/Dockerfile"}, []string{"worker:check"}, "worker (worker:check): worker/src/run.ts and 1 more", ""},
		"auth":             {[]string{"auth/src/server.ts"}, []string{"auth:check"}, "auth (auth:check): auth/src/server.ts", "auth/src/server.ts changed: task migrate-check also applies"},
		"generated roles":  {[]string{"auth/src/access.gen.ts"}, append(slices.Clone(goTasks[:3]), "auth:check", "validate"), "auth (auth:check): auth/src/access.gen.ts", "migrate-check"},
		"api client":       {[]string{"packages/api-client/src/client.ts"}, []string{"api-client:check", "web:check"}, "web (web:check): packages/api-client/src/client.ts", ""},
		"control worker":   {[]string{"deploy/cloudflare/src/index.ts"}, []string{"cloudflare:check"}, "cloudflare (cloudflare:check): deploy/cloudflare/src/index.ts", ""},
		"terraform":        {[]string{"terraform/r2.tf"}, []string{"tf:check"}, "terraform (tf:check): terraform/r2.tf", ""},
		"sops rules":       {[]string{".sops.yaml"}, []string{"tf:check"}, ".sops.yaml", ""},
		"nothing to check": {
			[]string{"docs/adr/0001.md", "design-system/goal/MASTER.md", ".claude/skills/x/SKILL.md", ".github/workflows/ci.yml", ".githooks/pre-push", "README.md", "skills-lock.json"},
			nil, "", "",
		},
		"parts keep task check's order": {
			[]string{"terraform/r2.tf", "web/src/a.tsx", "internal/api/org.go", "worker/src/a.ts"},
			[]string{"lint", "test", "api:permissions-check", "worker:check", "web:check", "validate", "tf:check"},
			"worker (worker:check)", "",
		},
	}

	for _, shared := range []string{"go.mod", "go.sum", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "Taskfile.yml", ".golangci.yml", ".mise.toml"} {
		tests["shared "+shared] = planCase{[]string{"web/src/a.tsx", shared}, tasks, "everything (" + strings.Join(tasks, ", ") + "): " + shared + " (shared by every part)", ""}
	}

	tests["contract api"] = struct {
		changed []string
		want    []string
		reason  string
		note    string
	}{[]string{"api/openapi.yaml"}, tasks, "everything (" + strings.Join(tasks, ", ") + "): api/openapi.yaml (a contract", ""}
	tests["contract schema"] = struct {
		changed []string
		want    []string
		reason  string
		note    string
	}{[]string{"schema/factory.schema.json"}, tasks, "schema/factory.schema.json (a contract", ""}

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
