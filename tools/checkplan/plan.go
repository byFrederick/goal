package main

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// tasks is the order task check runs its tasks in; a plan keeps it.
var tasks = []string{
	"layout:check", "env:check", "docs:check", "lint", "test", "gen:check", "worker:check", "auth:check",
	"api-client:check", "web:check", "cloudflare:check", "validate", "tf:check",
}

// A part is a piece of the repository and the tasks that check it.
type part struct {
	name  string
	tasks []string
}

var (
	partGo         = part{"go", []string{"lint", "test", "gen:check", "validate"}}
	partGen        = part{"gen", []string{"gen:check"}}
	partWorker     = part{"worker", []string{"worker:check"}}
	partAuth       = part{"auth", []string{"auth:check"}}
	partAPIClient  = part{"api-client", []string{"api-client:check"}}
	partWeb        = part{"web", []string{"web:check"}}
	partCloudflare = part{"cloudflare", []string{"cloudflare:check"}}
	partTerraform  = part{"terraform", []string{"tf:check"}}
	partLayout     = part{"layout", []string{"layout:check"}}
	partEnv        = part{"env", []string{"env:check"}}
	partDocs       = part{"docs", []string{"docs:check"}}
	partEverything = part{"everything", tasks}
)

// parts lists every part in report order; everything comes first since it covers the rest.
var parts = []part{partEverything, partGo, partGen, partWorker, partAuth, partAPIClient, partWeb, partCloudflare, partTerraform, partLayout, partEnv, partDocs}

// A rule maps the paths it matches to the parts they select; no parts means nothing to check.
type rule struct {
	match func(p string) bool
	parts []part
	why   string
}

func exact(names ...string) func(string) bool {
	return func(p string) bool { return slices.Contains(names, p) }
}

func under(dirs ...string) func(string) bool {
	return func(p string) bool {
		for _, d := range dirs {
			if strings.HasPrefix(p, d+"/") {
				return true
			}
		}

		return false
	}
}

func goFile(p string) bool { return strings.HasSuffix(p, ".go") }

// generated reports a generated file: its name has .gen. before the extension (ADR 0013, decision 4).
func generated(p string) bool { return strings.Contains(path.Base(p), ".gen.") }

// taskfile reports the root Taskfile.yml or one it includes (apps/web/Taskfile.yml...); a test's
// fixture under testdata/ is not one.
func taskfile(p string) bool { return path.Base(p) == "Taskfile.yml" && !isTestdata(p) }

func isTestdata(p string) bool {
	return strings.HasPrefix(p, "testdata/") || strings.Contains(p, "/testdata/")
}

func rootMarkdown(p string) bool { return !strings.Contains(p, "/") && path.Ext(p) == ".md" }

// rules are tried in order and the first match wins. A path that matches none runs everything:
// only an explicit rule may say a file needs no check.
var rules = []rule{
	{exact("go.mod", "go.sum", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml",
		".golangci.yml", ".mise.toml", "mise.toml", ".tool-versions"), []part{partEverything}, "shared by every part"},
	// The root Taskfile includes each area's, flattened: any of them can change a task another part runs.
	{taskfile, []part{partEverything}, "shared by every part"},
	{under("contracts"), []part{partEverything}, "a contract every part builds on"},
	// tools/layoutcheck is Go, and the layout check it runs.
	{under("tools/layoutcheck"), []part{partGo, partLayout}, ""},
	// tools/envcheck is Go, and the environment check it runs.
	{under("tools/envcheck"), []part{partGo, partEnv}, ""},
	// The files tools/envcheck compares with contracts/env.yaml, besides the code below.
	{exact("docs/reference/environment.md"), []part{partEnv}, ""},
	{exact("deploy/cloudflare/wrangler.jsonc"), []part{partCloudflare, partEnv}, ""},
	// tools/gendocs is Go, and the check of the docs it generates.
	{under("tools/gendocs"), []part{partGo, partDocs}, ""},
	// ADR 0013 is also a source of the ADR index that tools/gendocs generates.
	{exact("docs/adr/0013-repository-layout.md"), []part{partLayout, partDocs}, ""},
	// The descriptions of the layout that tools/layoutcheck compares with the tracked files.
	{exact("AGENTS.md"), []part{partLayout}, ""},
	{under(".claude/rules"), []part{partLayout}, ""},
	// Go code anywhere, and the files Go embeds (go:embed) or its tests read. Go code may read
	// the environment, which tools/envcheck checks.
	{goFile, []part{partGo, partEnv}, ""},
	{under("cmd", "internal", "db", "apps/web/placeholder"), []part{partGo}, ""},
	// The Go repository tools whose output is not a check of their own.
	{under("tools/checkplan", "tools/permgen"), []part{partGo}, ""},
	{exact("apps/web/csp.txt"), []part{partGo}, ""},
	{exact("compose.yaml", ".env.example"), []part{partGo, partEnv}, ""},
	{under("apps/worker"), []part{partWorker, partEnv}, ""},
	{under("apps/auth"), []part{partAuth, partEnv}, ""},
	// web depends on @goal/api-client.
	{under("packages/api-client"), []part{partAPIClient, partWeb}, ""},
	// packages/config holds the Biome config and tsconfig bases every TypeScript part extends.
	{under("packages/config"), []part{partWorker, partAuth, partAPIClient, partWeb, partCloudflare}, "shared by every TypeScript part"},
	{under("apps/web"), []part{partWeb}, ""},
	// The control Worker's code, and its wrangler config and deploy scripts.
	{under("apps/control-worker", "deploy/cloudflare"), []part{partCloudflare, partEnv}, ""},
	{under("deploy/terraform"), []part{partTerraform}, ""},
	// The generated change map and ADR index, and their sources (tools/gendocs).
	{exact("docs/engineering/change-map.md"), []part{partDocs}, ""},
	{under("docs/engineering/change-map", "docs/adr"), []part{partDocs}, ""},
	{under("docs", ".claude", ".github", ".githooks"), nil, ""},
	{exact("skills-lock.json"), nil, ""},
	{rootMarkdown, nil, ""},
}

// A plan is the tasks to run, in task check's order, and why each part is in it.
type plan struct {
	tasks   []string
	reasons []string
	notes   []string
}

// planFor plans the checks of the changed paths (relative to the repository root, slash-separated).
func planFor(changed []string) plan {
	selected, whys := selectParts(changed)

	var pl plan

	wanted := map[string]bool{}

	for _, pt := range parts {
		paths := selected[pt.name]
		if len(paths) == 0 {
			continue
		}

		for _, t := range pt.tasks {
			wanted[t] = true
		}

		pl.reasons = append(pl.reasons, fmt.Sprintf("%s (%s): %s", pt.name, strings.Join(pt.tasks, ", "), describe(paths, whys[pt.name])))
	}

	for _, t := range tasks {
		if wanted[t] {
			pl.tasks = append(pl.tasks, t)
		}
	}

	if i := slices.IndexFunc(changed, needsMigrateCheck); i >= 0 {
		pl.notes = append(pl.notes, changed[i]+" changed: task migrate-check also applies")
	}

	return pl
}

// selectParts maps each part's name to the paths that select it, and to the why of the rule
// that selected it first, if that rule has one. A generated file also selects gen, whatever
// its rule, since task gen:check reruns its generator.
func selectParts(changed []string) (selected map[string][]string, whys map[string]string) {
	selected, whys = map[string][]string{}, map[string]string{}

	for _, p := range changed {
		r, ok := ruleFor(p)
		if !ok {
			r = rule{parts: []part{partEverything}, why: "matches no rule"}
		}

		if generated(p) {
			r.parts = append(slices.Clone(r.parts), partGen)
		}

		for _, pt := range r.parts {
			if len(selected[pt.name]) == 0 && r.why != "" {
				whys[pt.name] = r.why
			}

			selected[pt.name] = append(selected[pt.name], p)
		}
	}

	return selected, whys
}

// needsMigrateCheck reports a path that the pre-push hook also runs task migrate-check for.
func needsMigrateCheck(p string) bool {
	return strings.HasPrefix(p, "db/migrations/") || strings.HasPrefix(p, "apps/auth/")
}

// everything plans every task for a reason that is not a changed file (no base to compare with).
func everything(why string) plan {
	return plan{tasks: tasks, reasons: []string{fmt.Sprintf("everything (%s): %s", strings.Join(tasks, ", "), why)}}
}

func ruleFor(p string) (rule, bool) {
	for _, r := range rules {
		if r.match(p) {
			return r, true
		}
	}

	return rule{}, false
}

func describe(paths []string, why string) string {
	s := paths[0]
	if why != "" {
		s += " (" + why + ")"
	}

	if n := len(paths) - 1; n > 0 {
		s += fmt.Sprintf(" and %d more", n)
	}

	return s
}
