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
		"web only":                {[]string{"apps/web/src/routes/board.tsx", "apps/web/package.json"}, []string{"web:check"}, "web (web:check): apps/web/src/routes/board.tsx and 1 more", ""},
		"go file":                 {[]string{"internal/engine/pull.go"}, goTasks, "go (lint, test, api:permissions-check, validate): internal/engine/pull.go", ""},
		"go file outside go dirs": {[]string{"apps/web/webui.go"}, goTasks, "apps/web/webui.go", ""},
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
		"compose read":     {[]string{"compose.yaml"}, goTasks, "compose.yaml", ""},
		"env example read": {[]string{".env.example"}, goTasks, ".env.example", ""},
		"worker":           {[]string{"apps/worker/src/run.ts", "apps/worker/Dockerfile"}, []string{"worker:check"}, "worker (worker:check): apps/worker/src/run.ts and 1 more", ""},
		"auth":             {[]string{"apps/auth/src/server.ts"}, []string{"auth:check"}, "auth (auth:check): apps/auth/src/server.ts", "apps/auth/src/server.ts changed: task migrate-check also applies"},
		"generated roles":  {[]string{"apps/auth/src/access.gen.ts"}, append(slices.Clone(goTasks[:3]), "auth:check", "validate"), "auth (auth:check): apps/auth/src/access.gen.ts", "migrate-check"},
		"api client":       {[]string{"packages/api-client/src/client.ts"}, []string{"api-client:check", "web:check"}, "web (web:check): packages/api-client/src/client.ts", ""},
		"control worker":   {[]string{"deploy/cloudflare/src/index.ts"}, []string{"cloudflare:check"}, "cloudflare (cloudflare:check): deploy/cloudflare/src/index.ts", ""},
		"terraform":        {[]string{"deploy/terraform/r2.tf"}, []string{"tf:check"}, "terraform (tf:check): deploy/terraform/r2.tf", ""},
		"sops rules":       {[]string{".sops.yaml"}, []string{"tf:check"}, ".sops.yaml", ""},
		"nothing to check": {
			[]string{"docs/adr/0001.md", "design-system/goal/MASTER.md", ".claude/skills/x/SKILL.md", ".github/workflows/ci.yml", ".githooks/pre-push", "README.md", "skills-lock.json"},
			nil, "", "",
		},
		"parts keep task check's order": {
			[]string{"deploy/terraform/r2.tf", "apps/web/src/a.tsx", "internal/api/org.go", "apps/worker/src/a.ts"},
			[]string{"lint", "test", "api:permissions-check", "worker:check", "web:check", "validate", "tf:check"},
			"worker (worker:check)", "",
		},
	}

	for _, shared := range []string{"go.mod", "go.sum", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "Taskfile.yml", ".golangci.yml", ".mise.toml"} {
		tests["shared "+shared] = planCase{[]string{"apps/web/src/a.tsx", shared}, tasks, "everything (" + strings.Join(tasks, ", ") + "): " + shared + " (shared by every part)", ""}
	}

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
