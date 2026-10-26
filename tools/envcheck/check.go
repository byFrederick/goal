package main

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// A repo is what the check reads: the tracked files and a view of the checkout.
type repo struct {
	fsys  fs.FS
	files []string // tracked files, slash-separated, relative to the root
}

func (r repo) read(name string) ([]byte, error) {
	b, err := fs.ReadFile(r.fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}

	return b, nil
}

// A problem is one place where the registry and the repository disagree.
type problem struct {
	where string // the file, or file:line
	what  string
}

func (p problem) String() string { return p.where + ": " + p.what }

// check reports every problem, ordered by place.
func check(r repo) ([]problem, error) {
	reg, err := loadRegistry(r)
	if err != nil {
		return nil, err
	}

	reads, w, err := scanRepo(r)
	if err != nil {
		return nil, err
	}

	problems := checkOrder(reg)
	problems = append(problems, checkReads(reg, reads)...)

	unread, err := checkUnread(r, reg, reads)
	if err != nil {
		return nil, err
	}

	problems = append(problems, unread...)
	problems = append(problems, checkSecretBindings(reg, w)...)

	example, err := r.read(envExampleFile)
	if err != nil {
		return nil, err
	}

	problems = append(problems, checkEnvExample(reg, example)...)

	if got, err := fs.ReadFile(r.fsys, referenceFile); err != nil || string(got) != reference(reg) {
		problems = append(problems, problem{referenceFile, "differs from the one " + registryFile + " generates: run task env:check -- -write"})
	}

	slices.SortStableFunc(problems, func(a, b problem) int { return strings.Compare(a.where, b.where) })

	return problems, nil
}

// scanRepo finds every read in the tracked files.
func scanRepo(r repo) ([]read, wrangler, error) {
	reads, err := scanSources(r)
	if err != nil {
		return nil, wrangler{}, err
	}

	w, err := wranglerOf(r)
	if err != nil {
		return nil, wrangler{}, err
	}

	// A Durable Object namespace on env is a binding, not a variable.
	bindings := w.bindings()
	reads = slices.DeleteFunc(reads, func(rd read) bool { return rd.reader == readerControlWorker && bindings[rd.name] })
	reads = append(reads, w.reads()...)

	for _, d := range []struct{ file, reader string }{{composeFile, readerCompose}, {taskfileFile, readerTaskfile}} {
		if !slices.Contains(r.files, d.file) {
			continue
		}

		src, err := r.read(d.file)
		if err != nil {
			return nil, wrangler{}, err
		}

		reads = append(reads, scanDollar(d.file, src, d.reader)...)
	}

	return reads, w, nil
}

// scanSources finds the reads in the Go and TypeScript sources, Go one package at a time so a
// read may name a constant of another file of its package.
func scanSources(r repo) ([]read, error) {
	packages := map[string]map[string][]byte{}

	var reads []read

	for _, f := range r.files {
		if !isGoSource(f) && !isTSSource(f) {
			continue
		}

		src, err := r.read(f)
		if err != nil {
			return nil, err
		}

		if isTSSource(f) {
			reads = append(reads, scanTS(f, src)...)
			continue
		}

		dir := path.Dir(f)
		if packages[dir] == nil {
			packages[dir] = map[string][]byte{}
		}

		packages[dir][f] = src
	}

	for _, dir := range sortedKeys(packages) {
		found, err := scanGo(packages[dir])
		if err != nil {
			return nil, err
		}

		reads = append(reads, found...)
	}

	return reads, nil
}

func wranglerOf(r repo) (wrangler, error) {
	if !slices.Contains(r.files, wranglerFile) {
		return wrangler{}, nil
	}

	src, err := r.read(wranglerFile)
	if err != nil {
		return wrangler{}, err
	}

	return parseWrangler(src)
}

// checkReads reports a read the registry does not declare, or whose process the entry does not
// list. A name is reported once per process, at its first read.
func checkReads(reg registry, reads []read) []problem {
	entries := reg.byName()

	var problems []problem

	reported := map[string]bool{}

	for _, rd := range reads {
		e, ok := entries[rd.name]

		key := rd.name + "\x00" + rd.reader
		if reported[key] {
			continue
		}

		switch {
		case !ok:
			reported[key] = true

			problems = append(problems, problem{rd.where, rd.name + " is read but not declared in " + registryFile})
		case rd.reader != "" && !slices.Contains(e.Readers, rd.reader):
			reported[key] = true

			problems = append(problems, problem{rd.where, fmt.Sprintf("%s is read by %s, which its entry in %s does not list among its readers", rd.name, rd.reader, registryFile)})
		}
	}

	return problems
}

// checkUnread reports a declared variable that nothing reads: no scanned read, no quoted
// mention in its indirect file, and no mention in compose.yaml or Taskfile.yml when it lists them.
func checkUnread(r repo, reg registry, reads []read) ([]problem, error) {
	isRead := map[string]bool{}
	for _, rd := range reads {
		isRead[rd.name] = true
	}

	texts := map[string]string{}

	text := func(name string) (string, error) {
		if t, ok := texts[name]; ok {
			return t, nil
		}

		b, err := r.read(name)
		if err != nil {
			return "", err
		}

		texts[name] = string(b)

		return texts[name], nil
	}

	var problems []problem

	for _, e := range reg.Variables {
		if isRead[e.Name] {
			continue
		}

		found, err := mentioned(e, text)
		if err != nil {
			return nil, err
		}

		if !found {
			problems = append(problems, problem{registryFile, e.Name + " is declared but nothing reads it" + unreadHint(e)})
		}
	}

	return problems, nil
}

// mentioned reports whether a variable without a scanned read is read where its entry says.
func mentioned(e entry, text func(string) (string, error)) (bool, error) {
	if e.Indirect != "" {
		t, err := text(e.Indirect)
		if err != nil {
			return false, fmt.Errorf("%s's indirect: %w", e.Name, err)
		}

		for _, q := range []string{`"`, `'`, "`"} {
			if strings.Contains(t, q+e.Name+q) {
				return true, nil
			}
		}
	}

	for _, d := range []struct{ reader, file string }{{readerCompose, composeFile}, {readerTaskfile, taskfileFile}} {
		if !slices.Contains(e.Readers, d.reader) {
			continue
		}

		t, err := text(d.file)
		if err != nil {
			return false, err
		}

		if strings.Contains(t, e.Name) {
			return true, nil
		}
	}

	return false, nil
}

func unreadHint(e entry) string {
	if e.Indirect != "" {
		return " (its name does not appear quoted in " + e.Indirect + ")"
	}

	return ""
}

// checkSecretBindings reports a Secrets Store binding whose entry is not a secret: whatever is
// kept in the store is one.
func checkSecretBindings(reg registry, w wrangler) []problem {
	entries := reg.byName()

	var problems []problem

	for _, s := range w.SecretsStoreSecrets {
		if e, ok := entries[s.Binding]; ok && e.Class != classSecret {
			problems = append(problems, problem{registryFile, fmt.Sprintf("%s is a Secrets Store binding in %s but is classified %s, not secret", s.Binding, wranglerFile, e.Class)})
		}
	}

	return problems
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}
