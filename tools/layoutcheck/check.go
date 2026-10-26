package main

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The files the check reads, relative to the repository root.
const (
	agentsFile = "AGENTS.md"
	adrFile    = "docs/adr/0013-repository-layout.md"
	rulesDir   = ".claude/rules/"
)

// The kinds of problem the check reports.
const (
	kindNotInMap    = "top-level directory missing from the repository map"
	kindNotInLayout = "top-level directory missing from the layout and the History"
	kindDeadGlob    = "paths: glob matches no tracked file"
	kindBadGlob     = "paths: glob is not valid"
	kindNoSuchPath  = "names a path that does not exist"
)

// A repo is what the check reads: the tracked files and the text of the files that describe them.
type repo struct {
	files  []string          // tracked files, slash-separated, relative to the root
	agents string            // AGENTS.md
	adr    string            // ADR 0013
	rules  map[string]string // each .claude/rules/*.md by its path
	// Each tracked Taskfile.yml by its path, and the committed list of task names (tasks.txt).
	taskfiles map[string]string
	taskNames string
}

// A problem is one place where the descriptions and the tree disagree.
type problem struct {
	file string // the file that is wrong
	kind string
	path string // the directory, glob or path it is about
}

func (p problem) String() string { return fmt.Sprintf("%s: %s: %s", p.file, p.kind, p.path) }

// check reports every problem, ordered by file and then as found.
func check(r repo) []problem {
	t := newTree(r.files)
	history := historyPaths(r.adr)

	var problems []problem

	problems = append(problems, checkTopLevel(t, r.agents, r.adr, history)...)
	problems = append(problems, checkAgentsPaths(t, r.agents, history)...)
	problems = append(problems, checkRules(r.files, r.rules)...)
	problems = append(problems, checkTaskNames(r.taskfiles, r.taskNames)...)

	slices.SortStableFunc(problems, func(a, b problem) int { return strings.Compare(a.file, b.file) })

	return problems
}

// A tree answers whether a path is a tracked file or a directory holding one.
type tree struct {
	files, dirs map[string]bool
	top         []string // top-level directories, sorted
}

func newTree(files []string) tree {
	t := tree{files: map[string]bool{}, dirs: map[string]bool{}}

	for _, f := range files {
		t.files[f] = true

		for d := path.Dir(f); d != "."; d = path.Dir(d) {
			if !t.dirs[d] && !strings.Contains(d, "/") {
				t.top = append(t.top, d)
			}

			t.dirs[d] = true
		}
	}

	slices.Sort(t.top)

	return t
}

func (t tree) exists(p string) bool {
	p = strings.TrimSuffix(p, "/")

	return t.files[p] || t.dirs[p]
}

// checkTopLevel reports a top-level directory that the AGENTS.md map or ADR 0013 does not name.
// For the ADR, an old path in its History counts: the directory is still waiting for its move.
func checkTopLevel(t tree, agents, adr string, history []string) []problem {
	inMap := map[string]bool{}

	for _, l := range mapLines(agents) {
		inMap[topSegment(l.dir)] = true
	}

	inLayout := map[string]bool{}

	for tok := range strings.FieldsSeq(firstFence(adr)) {
		if strings.Contains(tok, "/") {
			inLayout[topSegment(tok)] = true
		}
	}

	for _, h := range history {
		inLayout[topSegment(h)] = true
	}

	var problems []problem

	for _, d := range t.top {
		if !inMap[d] {
			problems = append(problems, problem{agentsFile, kindNotInMap, d + "/"})
		}

		if !inLayout[d] {
			problems = append(problems, problem{adrFile, kindNotInLayout, d + "/"})
		}
	}

	return problems
}

// checkAgentsPaths reports a path in the AGENTS.md map or a relative link in AGENTS.md that does
// not exist. A map token resolves under its line's directory or at the root; an old path of the
// ADR's History is exempt.
func checkAgentsPaths(t tree, agents string, history []string) []problem {
	var problems []problem

	report := func(p string) {
		problems = append(problems, problem{agentsFile, kindNoSuchPath, p})
	}

	for _, l := range mapLines(agents) {
		if !t.exists(l.dir) {
			report(l.dir)
		}

		for _, tok := range l.tokens {
			if isHistory(tok, history) || t.exists(l.dir+tok) || t.exists(tok) {
				continue
			}

			report(l.dir + tok)
		}
	}

	for _, target := range linkTargets(agents) {
		if !isHistory(target, history) && !t.exists(target) {
			report(target)
		}
	}

	return problems
}

// checkRules reports each paths: glob of a rule that matches no tracked file.
func checkRules(files []string, rules map[string]string) []problem {
	names := make([]string, 0, len(rules))
	for name := range rules {
		names = append(names, name)
	}

	slices.Sort(names)

	var problems []problem

	for _, name := range names {
		for _, glob := range frontmatterPaths(rules[name]) {
			if !validGlob(glob) {
				problems = append(problems, problem{name, kindBadGlob, glob})

				continue
			}

			if !slices.ContainsFunc(files, func(f string) bool { return matchGlob(glob, f) }) {
				problems = append(problems, problem{name, kindDeadGlob, glob})
			}
		}
	}

	return problems
}

// A mapLine is one line of the AGENTS.md repository map: its directory (with a trailing slash)
// and the tokens of its description that name a path.
type mapLine struct {
	dir    string
	tokens []string
}

// mapLines parses the first fenced block under "## Repository map". A description token names a
// path when it contains a slash; punctuation around it is trimmed, and a token starting with a
// slash (a URL route such as /api/auth) or holding a scheme is not a repository path.
func mapLines(agents string) []mapLine {
	_, section, ok := strings.Cut(agents, "## Repository map")
	if !ok {
		return nil
	}

	var lines []mapLine

	for line := range strings.Lines(firstFence(section)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		l := mapLine{dir: fields[0]}

		for _, f := range fields[1:] {
			tok := strings.TrimRight(strings.TrimLeft(f, "(`\"'"), "),:;.`\"'")
			if !strings.Contains(tok, "/") || strings.HasPrefix(tok, "/") || strings.Contains(tok, "://") {
				continue
			}

			l.tokens = append(l.tokens, tok)
		}

		lines = append(lines, l)
	}

	return lines
}

// firstFence returns the text inside the first ``` fenced block, or "".
func firstFence(text string) string {
	_, rest, ok := strings.Cut(text, "```")
	if !ok {
		return ""
	}

	// Skip the info string on the fence line.
	_, rest, _ = strings.Cut(rest, "\n")
	body, _, _ := strings.Cut(rest, "```")

	return body
}

var linkRE = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// linkTargets returns the repository paths that Markdown links in text point at, relative to the
// root: URLs, mail links and anchors are skipped, and a fragment is dropped.
func linkTargets(text string) []string {
	var targets []string

	for _, m := range linkRE.FindAllStringSubmatch(text, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
			continue
		}

		target, _, _ = strings.Cut(target, "#")
		targets = append(targets, path.Clean(target))
	}

	return targets
}

var historyRE = regexp.MustCompile("(?m)^\\| `([^`]+)` \\|")

// historyPaths returns the old paths of the History table of ADR 0013.
func historyPaths(adr string) []string {
	_, section, ok := strings.Cut(adr, "## History")
	if !ok {
		return nil
	}

	var old []string

	for _, m := range historyRE.FindAllStringSubmatch(section, -1) {
		old = append(old, m[1])
	}

	return old
}

// isHistory reports whether p is an old path of the History or lies under one.
func isHistory(p string, history []string) bool {
	p = strings.TrimSuffix(p, "/")

	return slices.ContainsFunc(history, func(h string) bool {
		h = strings.TrimSuffix(h, "/")

		return p == h || strings.HasPrefix(p, h+"/")
	})
}

func topSegment(p string) string {
	first, _, _ := strings.Cut(p, "/")

	return first
}

// frontmatterPaths returns the list under paths: in a file's leading --- frontmatter.
func frontmatterPaths(text string) []string {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil
	}

	front, _, _ := strings.Cut(rest, "\n---")

	var (
		globs []string
		in    bool
	)

	for line := range strings.Lines(front) {
		line = strings.TrimRight(line, "\r\n")

		switch item, isItem := strings.CutPrefix(strings.TrimSpace(line), "- "); {
		case strings.TrimSpace(line) == "paths:":
			in = true
		case in && isItem:
			globs = append(globs, strings.Trim(strings.TrimSpace(item), `"'`))
		default:
			in = false
		}
	}

	return globs
}

// validGlob reports whether every segment of glob is a valid path.Match pattern.
func validGlob(glob string) bool {
	for seg := range strings.SplitSeq(glob, "/") {
		if _, err := path.Match(seg, ""); err != nil {
			return false
		}
	}

	return true
}

// matchGlob matches a slash-separated path against a glob: ** matches zero or more whole
// segments, and any other segment is a path.Match pattern (*, ?, [...]) within one segment.
func matchGlob(glob, p string) bool {
	return matchSegments(strings.Split(glob, "/"), strings.Split(p, "/"))
}

func matchSegments(glob, p []string) bool {
	if len(glob) == 0 {
		return len(p) == 0
	}

	if glob[0] == "**" {
		for i := 0; i <= len(p); i++ {
			if matchSegments(glob[1:], p[i:]) {
				return true
			}
		}

		return false
	}

	if len(p) == 0 {
		return false
	}

	ok, err := path.Match(glob[0], p[0])

	return err == nil && ok && matchSegments(glob[1:], p[1:])
}
