package main

import (
	"maps"
	"strings"
	"testing"
)

// A root Taskfile with an internal task, a flattened include, a namespaced one and, inside the
// flattened one, an include of its own resolved against its directory.
var baseTaskfiles = map[string]string{
	"Taskfile.yml": "version: \"3\"\nincludes:\n" +
		"  web:\n    taskfile: apps/web/Taskfile.yml\n    flatten: true\n    dir: \"{{.ROOT_DIR}}\"\n" +
		"  docs: docs\n" +
		"tasks:\n  check:\n    cmds: [task web:check]\n  tf:run:\n    internal: true\n",
	"apps/web/Taskfile.yml": "version: \"3\"\nincludes:\n  e2e:\n    taskfile: ./e2e/Taskfile.yml\n    flatten: true\n" +
		"tasks:\n  web:check:\n    dir: apps/web\n    cmds: [pnpm test]\n",
	"apps/web/e2e/Taskfile.yml": "version: \"3\"\ntasks:\n  web:e2e:\n    cmds: [pnpm e2e]\n",
	"docs/Taskfile.yml":         "version: \"3\"\ntasks:\n  build:\n    cmds: [make]\n",
}

const baseTaskList = "check\ndocs:build\nweb:check\nweb:e2e\n"

func TestTaskNames(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit func(taskfiles map[string]string) string // returns the list
		want []string
	}{
		"the list matches": {
			func(map[string]string) string { return baseTaskList },
			nil,
		},
		"a renamed task in an included Taskfile": {
			func(tf map[string]string) string {
				tf["apps/web/Taskfile.yml"] = strings.Replace(tf["apps/web/Taskfile.yml"], "web:check:", "web:verify:", 1)

				return baseTaskList
			},
			[]string{
				"tools/layoutcheck/tasks.txt: task is missing from the list: web:verify",
				"tools/layoutcheck/tasks.txt: names a task that no Taskfile defines: web:check",
			},
		},
		"an include losing flatten renames its tasks": {
			func(tf map[string]string) string {
				tf["Taskfile.yml"] = strings.Replace(tf["Taskfile.yml"], "    flatten: true\n", "", 1)

				return baseTaskList
			},
			[]string{
				"tools/layoutcheck/tasks.txt: task is missing from the list: web:web:check",
				"tools/layoutcheck/tasks.txt: task is missing from the list: web:web:e2e",
				"tools/layoutcheck/tasks.txt: names a task that no Taskfile defines: web:check",
				"tools/layoutcheck/tasks.txt: names a task that no Taskfile defines: web:e2e",
			},
		},
		"an internal task in the list": {
			func(map[string]string) string { return baseTaskList + "tf:run\n" },
			[]string{"tools/layoutcheck/tasks.txt: names a task that no Taskfile defines: tf:run"},
		},
		"an included Taskfile that is not tracked": {
			func(tf map[string]string) string {
				delete(tf, "docs/Taskfile.yml")

				return "check\nweb:check\nweb:e2e\n"
			},
			[]string{"Taskfile.yml: includes a Taskfile that is not tracked: docs/Taskfile.yml"},
		},
		"an included Taskfile that does not parse": {
			func(tf map[string]string) string {
				tf["docs/Taskfile.yml"] = "tasks: [\n"

				return "check\nweb:check\nweb:e2e\n"
			},
			[]string{"docs/Taskfile.yml: is not a valid Taskfile: yaml: line 1: did not find expected node content"},
		},
		"an include cycle": {
			func(tf map[string]string) string {
				tf["docs/Taskfile.yml"] = "version: \"3\"\nincludes:\n  again: ..\ntasks:\n  build: {}\n"

				return baseTaskList
			},
			[]string{"docs/Taskfile.yml: includes a Taskfile that includes it: Taskfile.yml"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			taskfiles := maps.Clone(baseTaskfiles)
			list := tt.edit(taskfiles)

			var got []string
			for _, p := range checkTaskNames(taskfiles, list) {
				got = append(got, p.String())
			}

			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("problems:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(tt.want, "\n  "))
			}
		})
	}
}
