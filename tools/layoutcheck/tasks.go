package main

import (
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The Taskfiles the name check reads, relative to the repository root.
const (
	rootTaskfile = "Taskfile.yml"
	taskfileName = "Taskfile.yml"
	tasksFile    = "tools/layoutcheck/tasks.txt"
)

// The kinds of problem the name check reports.
const (
	kindTaskNotListed   = "task is missing from the list"
	kindTaskGone        = "names a task that no Taskfile defines"
	kindBadTaskfile     = "is not a valid Taskfile"
	kindMissingTaskfile = "includes a Taskfile that is not tracked"
	kindIncludeCycle    = "includes a Taskfile that includes it"
)

// A taskfile is the part of a Taskfile the names depend on.
type taskfile struct {
	Includes map[string]include `yaml:"includes"`
	Tasks    map[string]struct {
		Internal bool `yaml:"internal"`
	} `yaml:"tasks"`
}

// An include is one entry of includes: a path, or a map with taskfile, flatten and internal.
type include struct {
	Taskfile string `yaml:"taskfile"`
	Flatten  bool   `yaml:"flatten"`
	Internal bool   `yaml:"internal"`
}

func (in *include) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		in.Taskfile = n.Value

		return nil
	}

	type plain include

	return n.Decode((*plain)(in))
}

// checkTaskNames reports where the names task --list-all shows (every task of the root Taskfile
// and the ones it includes, but internal ones) differ from the committed list, one name per line.
// taskfiles holds each tracked Taskfile.yml by its path.
func checkTaskNames(taskfiles map[string]string, list string) []problem {
	if _, ok := taskfiles[rootTaskfile]; !ok {
		return nil
	}

	names := map[string]bool{}
	problems := collectTasks(taskfiles, rootTaskfile, "", false, nil, names)

	listed := map[string]bool{}

	for line := range strings.Lines(list) {
		if name := strings.TrimSpace(line); name != "" {
			listed[name] = true
		}
	}

	for _, name := range sortedKeys(names) {
		if !listed[name] {
			problems = append(problems, problem{tasksFile, kindTaskNotListed, name})
		}
	}

	for _, name := range sortedKeys(listed) {
		if !names[name] {
			problems = append(problems, problem{tasksFile, kindTaskGone, name})
		}
	}

	return problems
}

// collectTasks adds the visible task names of the Taskfile at file, and of the ones it includes,
// to names. prefix is the namespace its includer gives it ("" when flattened); hidden is true
// when an include marked it internal; chain is the Taskfiles that include it, root first.
func collectTasks(taskfiles map[string]string, file, prefix string, hidden bool, chain []string, names map[string]bool) []problem {
	var tf taskfile

	if err := yaml.Unmarshal([]byte(taskfiles[file]), &tf); err != nil {
		return []problem{{file, kindBadTaskfile, err.Error()}}
	}

	for name, t := range tf.Tasks {
		if !hidden && !t.Internal {
			names[prefix+name] = true
		}
	}

	var problems []problem

	for _, ns := range sortedKeys(tf.Includes) {
		in := tf.Includes[ns]
		target := includedPath(file, in.Taskfile)

		if _, ok := taskfiles[target]; !ok {
			problems = append(problems, problem{file, kindMissingTaskfile, target})

			continue
		}

		if target == file || slices.Contains(chain, target) {
			problems = append(problems, problem{file, kindIncludeCycle, target})

			continue
		}

		next := prefix
		if !in.Flatten {
			next += ns + ":"
		}

		problems = append(problems, collectTasks(taskfiles, target, next, hidden || in.Internal, append(slices.Clone(chain), file), names)...)
	}

	return problems
}

// includedPath resolves an include's taskfile against the directory of the Taskfile that includes
// it, as Task does: a directory means its Taskfile.yml, and {{.ROOT_DIR}} is the repository root.
func includedPath(from, target string) string {
	if rest, ok := strings.CutPrefix(target, "{{.ROOT_DIR}}"); ok {
		target = strings.TrimPrefix(rest, "/")
	} else {
		target = path.Join(path.Dir(from), target)
	}

	target = path.Clean(target)
	if ext := path.Ext(target); ext != ".yml" && ext != ".yaml" {
		target = path.Join(target, taskfileName)
	}

	return target
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}
