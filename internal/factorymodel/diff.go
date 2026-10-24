package factorymodel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/domain"
)

// Snapshot is one version of a definition: its manifest and its files.
type Snapshot struct {
	Manifest domain.Manifest
	Files    fs.FS
}

// FileOp is what happened to a file between two versions.
type FileOp string

// File operations.
const (
	FileAdded     FileOp = "added"
	FileRemoved   FileOp = "removed"
	FileModified  FileOp = "modified"
	FileUnchanged FileOp = "unchanged"
)

// FileChange is one path of either version, with both hashes ("" where absent).
type FileChange struct {
	Path     string
	Op       FileOp
	From, To domain.BlobHash
}

// ChangeOp is what happened to a target.
type ChangeOp string

// Change operations.
const (
	Added   ChangeOp = "added"
	Removed ChangeOp = "removed"
	Changed ChangeOp = "changed"
	Moved   ChangeOp = "moved"
)

// Target kinds of a Change.
const (
	TargetStation    = "station"
	TargetAgent      = "agent"
	TargetConnection = "connection"
	TargetSkill      = "skill"
	TargetContract   = "contract"
	TargetField      = "field" // a value of a section that is not an item of a list
	TargetFile       = "file"
)

// Target is what a change is about: a station by id, an agent, connection or skill by name, a
// contract by name, a section field by its model key, or a file by path.
type Target struct {
	Kind, ID string
}

// Change is one meaningful difference between two versions.
type Change struct {
	// Section is a model section, or domain.SectionFiles for files and YAML-only settings.
	Section string
	Op      ChangeOp
	Target  Target
	// Field is the one model field of the target that changed; empty when the target was added,
	// removed or moved, or when several of its fields changed. From and To are that field's
	// values, or the target's (nil where absent).
	Field    string
	From, To any
	// Summary is a short sentence for people: "Build also runs pnpm test".
	Summary string
	// FromLines and ToLines are the target's lines in each version's factory.yaml; zero where it
	// is absent or not in factory.yaml.
	FromLines, ToLines [2]int
}

// Counts sums a comparison up: "3 changes in 2 sections".
type Counts struct {
	Changes, Sections int
}

// Comparison is the semantic difference between two versions of a definition.
type Comparison struct {
	Files   []FileChange
	Changes []Change
	Counts  Counts
}

// version is a snapshot read for comparing.
type version struct {
	snap   Snapshot
	root   *yaml.Node // nil when factory.yaml is absent or does not parse
	model  Model
	ranges Ranges
}

func readVersion(s Snapshot) version {
	v := version{snap: s, model: Model{Connections: []any{}}, ranges: Ranges{}}
	if s.Files == nil {
		return v
	}

	doc, err := fs.ReadFile(s.Files, factoryFile)
	if err != nil {
		return v
	}

	t, err := parse(doc)
	if err != nil {
		return v
	}

	v.root, v.model, v.ranges = t.root, readModel(t.root, s.Files), ranges(t.root)

	return v
}

const factoryFile = "factory.yaml"

// Diff compares two versions: their files by hash, and the factory model section by section,
// stations, agents, skills and connections by identity, so a reorder is one moved change. A
// changed prompt file is a change of its agent, skill files a change of their skill, contract
// files a change of their contract; any other file, and a change of factory.yaml outside the
// sections, is in domain.SectionFiles.
func Diff(from, to Snapshot) Comparison {
	a, b := readVersion(from), readVersion(to)
	c := Comparison{Files: fileChanges(from.Manifest, to.Manifest)}

	if a.root == nil || b.root == nil {
		if from.Manifest[factoryFile] != to.Manifest[factoryFile] {
			c.Changes = append(c.Changes, Change{
				Section: domain.SectionFiles, Op: Changed, Target: Target{Kind: TargetFile, ID: factoryFile},
				Summary: "factory.yaml changed; one of the versions does not parse",
			})
		}
	} else {
		c.Changes = append(c.Changes, modelChanges(a, b)...)
		c.Changes = append(c.Changes, yamlOnlyChanges(a, b)...)
	}

	c.Changes = append(c.Changes, fileTargetChanges(a, b, c.Files)...)

	sections := map[string]bool{}
	for _, ch := range c.Changes {
		sections[ch.Section] = true
	}

	c.Counts = Counts{Changes: len(c.Changes), Sections: len(sections)}

	return c
}

func fileChanges(from, to domain.Manifest) []FileChange {
	paths := make([]string, 0, len(from)+len(to))
	for p := range from {
		paths = append(paths, p)
	}

	for p := range to {
		if _, ok := from[p]; !ok {
			paths = append(paths, p)
		}
	}

	slices.Sort(paths)

	out := make([]FileChange, 0, len(paths))

	for _, p := range paths {
		fc := FileChange{Path: p, From: from[p], To: to[p]}

		switch {
		case fc.From == "":
			fc.Op = FileAdded
		case fc.To == "":
			fc.Op = FileRemoved
		case fc.From != fc.To:
			fc.Op = FileModified
		default:
			fc.Op = FileUnchanged
		}

		out = append(out, fc)
	}

	return out
}

// modelChanges compares the sections in the editor's order.
func modelChanges(a, b version) []Change {
	var out []Change

	for _, s := range Sections {
		if s == Connections {
			out = append(out, entityChanges(a, b, list{Connections, TargetConnection, "/connections", connectionsShape},
				a.model.Connections, b.model.Connections)...)

			continue
		}

		am, _ := a.model.Section(s).(map[string]any)
		bm, _ := b.model.Section(s).(map[string]any)

		for _, bind := range sections[s] {
			if l, ok := lists[bind.key]; ok {
				al, _ := am[bind.key].([]any)
				bl, _ := bm[bind.key].([]any)
				out = append(out, entityChanges(a, b, l, al, bl)...)

				continue
			}

			out = append(out, fieldChange(a, b, s, bind, am[bind.key], bm[bind.key])...)
		}
	}

	return out
}

// list describes a list of identified items of a section.
type list struct {
	section Section
	kind    string
	pointer string // the list's model pointer
	shape   *shape
}

var lists = map[string]list{
	"stations": {Line, TargetStation, "/line/stations", stationsShape},
	keyAgents:  {Agents, TargetAgent, "/agents/agents", agentsShape},
	keySkills:  {Agents, TargetSkill, "/agents/skills", skillsShape},
}

// fieldChange compares one section field that is not a list of items.
func fieldChange(a, b version, s Section, bind binding, from, to any) []Change {
	nf, nt := normalize(from, bind.shape), normalize(to, bind.shape)
	if equal(nf, nt) {
		return nil
	}

	pointer := "/" + string(s) + "/" + bind.key
	ch := Change{
		Section: string(s), Op: opOf(empty(nf), empty(nt)), Target: Target{Kind: TargetField, ID: bind.key},
		Field: bind.key, From: from, To: to,
		FromLines: a.ranges[pointer], ToLines: b.ranges[pointer],
	}
	ch.Summary = fieldSummary(bind.key, ch.Op, from, to)

	return []Change{ch}
}

func opOf(wasAbsent, isAbsent bool) ChangeOp {
	switch {
	case wasAbsent:
		return Added
	case isAbsent:
		return Removed
	}

	return Changed
}

// entityChanges compares two versions of a list of identified items: added, removed, changed
// and moved items.
func entityChanges(a, b version, l list, from, to []any) []Change {
	id := func(v any) string { return l.shape.identityOf(normalize(v, l.shape.elem)) }
	fromIDs, toIDs := ids(from, id), ids(to, id)

	var out []Change

	for i, item := range from {
		if !slices.Contains(toIDs, fromIDs[i]) {
			out = append(out, Change{
				Section: string(l.section), Op: Removed, Target: Target{Kind: l.kind, ID: fromIDs[i]}, From: item,
				Summary:   capitalize(l.kind) + " " + label(l.kind, item, fromIDs[i]) + " removed",
				FromLines: a.ranges[l.pointer+"/"+strconv.Itoa(i)],
			})
		}
	}

	for j, item := range to {
		i := slices.Index(fromIDs, toIDs[j])
		lines := b.ranges[l.pointer+"/"+strconv.Itoa(j)]

		if i < 0 {
			out = append(out, Change{
				Section: string(l.section), Op: Added, Target: Target{Kind: l.kind, ID: toIDs[j]}, To: item,
				Summary: addedSummary(l.kind, item, toIDs[j]), ToLines: lines,
			})

			continue
		}

		if ch, ok := itemChange(a, b, l, from[i], item, toIDs[j]); ok {
			ch.FromLines, ch.ToLines = a.ranges[l.pointer+"/"+strconv.Itoa(i)], lines
			out = append(out, ch)
		}
	}

	return append(out, movedChanges(a, b, l, from, to, fromIDs, toIDs)...)
}

func ids(items []any, id func(any) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = id(it)
	}

	return out
}

// itemChange compares one item present in both versions.
func itemChange(a, b version, l list, from, to any, id string) (Change, bool) {
	fields := changedFields(normalize(from, l.shape.elem), normalize(to, l.shape.elem))

	if l.kind == TargetAgent && promptChanged(from, to) && !slices.Contains(fields, "prompt") {
		fields = append(fields, "prompt")
		slices.Sort(fields)
	}

	if len(fields) == 0 && (l.kind != TargetSkill || !skillFilesChanged(a, b, from)) {
		return Change{}, false
	}

	ch := Change{Section: string(l.section), Op: Changed, Target: Target{Kind: l.kind, ID: id}, From: from, To: to}

	if len(fields) == 1 {
		fm, _ := from.(map[string]any)
		tm, _ := to.(map[string]any)
		ch.Field, ch.From, ch.To = fields[0], fm[fields[0]], tm[fields[0]]
	}

	ch.Summary = itemSummary(l.kind, label(l.kind, to, id), fields, from, to)

	return ch, true
}

// promptChanged reports whether an agent's prompt file has other content: the model compares
// prompts by path, the diff by the file's hash too.
func promptChanged(from, to any) bool {
	return valueAt(from, "prompt", "sha256") != valueAt(to, "prompt", "sha256")
}

// changedFields are the keys whose values differ, sorted.
func changedFields(from, to any) []string {
	fm, _ := from.(map[string]any)
	tm, _ := to.(map[string]any)

	var out []string

	for k, v := range fm {
		if w, ok := tm[k]; !ok || !equal(v, w) {
			out = append(out, k)
		}
	}

	for k := range tm {
		if _, ok := fm[k]; !ok {
			out = append(out, k)
		}
	}

	slices.Sort(out)

	return out
}

// movedChanges reports the items that changed place: the common items outside the longest run
// that kept its order.
func movedChanges(a, b version, l list, from, to []any, fromIDs, toIDs []string) []Change {
	var common []string

	for _, id := range toIDs {
		if slices.Contains(fromIDs, id) {
			common = append(common, id)
		}
	}

	var fromOrder []string

	for _, id := range fromIDs {
		if slices.Contains(common, id) {
			fromOrder = append(fromOrder, id)
		}
	}

	// When two items swap, either could have moved; the one that moved up is reported.
	kept := map[int]bool{}

	lcs(common, fromOrder, func(x, y string) bool { return x == y }, func(i, _ int) { kept[i] = true })

	var out []Change

	for k, id := range common {
		if kept[k] {
			continue
		}

		i, j := slices.Index(fromIDs, id), slices.Index(toIDs, id)
		where := "to the top"

		if j > 0 {
			where = "after " + label(l.kind, to[j-1], toIDs[j-1])
		}

		out = append(out, Change{
			Section: string(l.section), Op: Moved, Target: Target{Kind: l.kind, ID: id}, From: from[i], To: to[j],
			Summary:   label(l.kind, to[j], id) + " moves " + where,
			FromLines: a.ranges[l.pointer+"/"+strconv.Itoa(i)], ToLines: b.ranges[l.pointer+"/"+strconv.Itoa(j)],
		})
	}

	return out
}

// skillFilesChanged reports whether a local skill's folder holds other files in b than in a.
func skillFilesChanged(a, b version, skill any) bool {
	m, _ := skill.(map[string]any)
	src, _ := m["source"].(string)

	if !strings.HasPrefix(src, "./") {
		return false
	}

	dir := path.Clean(src) + "/"

	return !equalUnder(a.snap.Manifest, b.snap.Manifest, dir)
}

func equalUnder(x, y domain.Manifest, dir string) bool {
	for p, h := range x {
		if strings.HasPrefix(p, dir) && y[p] != h {
			return false
		}
	}

	for p := range y {
		if _, ok := x[p]; strings.HasPrefix(p, dir) && !ok {
			return false
		}
	}

	return true
}

// yamlOnlyChanges reports a change of factory.yaml outside the sections (runtime.image,
// knowledgeBase, contracts, ...), which only the YAML editor changes.
func yamlOnlyChanges(a, b version) []Change {
	x, y := yamlOnly(a.root), yamlOnly(b.root)

	var paths []string

	diffPaths(x, y, "spec", &paths)
	diffPaths(x, y, "", &paths)

	if len(paths) == 0 {
		return nil
	}

	slices.Sort(paths)
	paths = slices.Compact(paths)

	return []Change{{
		Section: domain.SectionFiles, Op: Changed, Target: Target{Kind: TargetField, ID: factoryFile},
		Summary: "Settings edited in YAML change: " + strings.Join(firstN(paths, 3), ", "),
	}}
}

// yamlOnly is factory.yaml's value without what the sections own.
func yamlOnly(root *yaml.Node) map[string]any {
	var v map[string]any
	if root.Decode(&v) != nil {
		return nil
	}

	b, _ := json.Marshal(v)
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()

	out := map[string]any{}
	_ = d.Decode(&out)

	for _, binds := range sections {
		for _, bind := range binds {
			deletePath(out, bind.path)
		}
	}

	deletePath(out, []string{keyMetadata, keyDescription})

	return out
}

func deletePath(m map[string]any, p []string) {
	for _, k := range p[:len(p)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			return
		}

		m = next
	}

	delete(m, p[len(p)-1])
}

// diffPaths lists the dotted paths, two levels deep below prefix, where x and y differ.
func diffPaths(x, y map[string]any, prefix string, out *[]string) {
	xs, ys := x, y

	if prefix != "" {
		xs, _ = x[prefix].(map[string]any)
		ys, _ = y[prefix].(map[string]any)
	}

	for _, k := range unionKeys(xs, ys) {
		if prefix == "" && k == "spec" {
			continue
		}

		if equal(xs[k], ys[k]) {
			continue
		}

		p := k
		if prefix != "" {
			p = prefix + "." + k
		}

		xm, xok := xs[k].(map[string]any)
		ym, yok := ys[k].(map[string]any)

		if xok && yok {
			for _, kk := range unionKeys(xm, ym) {
				if !equal(xm[kk], ym[kk]) {
					*out = append(*out, p+"."+kk)
				}
			}

			continue
		}

		*out = append(*out, p)
	}
}

func unionKeys(x, y map[string]any) []string {
	var keys []string
	for k := range x {
		keys = append(keys, k)
	}

	for k := range y {
		if _, ok := x[k]; !ok {
			keys = append(keys, k)
		}
	}

	slices.Sort(keys)

	return keys
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}

	return append(slices.Clone(s[:n]), fmt.Sprintf("%d more", len(s)-n))
}

// fileTargetChanges reports the files that are not factory.yaml, a prompt or a local skill's:
// contracts as contract changes, everything else as file changes.
func fileTargetChanges(a, b version, files []FileChange) []Change {
	owned := ownedFiles(a)
	for p := range ownedFiles(b) {
		owned[p] = true
	}

	contracts := contractFiles(a)
	maps.Copy(contracts, contractFiles(b))

	var out []Change

	for _, fc := range files {
		if fc.Op == FileUnchanged || fc.Path == factoryFile || owned[fc.Path] {
			continue
		}

		op := map[FileOp]ChangeOp{FileAdded: Added, FileRemoved: Removed, FileModified: Changed}[fc.Op]

		if name, ok := contracts[fc.Path]; ok {
			out = append(out, Change{
				Section: string(Line), Op: op, Target: Target{Kind: TargetContract, ID: name},
				Summary: "Contract " + name + " " + pastTense(op),
			})

			continue
		}

		out = append(out, Change{
			Section: domain.SectionFiles, Op: op, Target: Target{Kind: TargetFile, ID: fc.Path},
			Summary: fc.Path + " " + pastTense(op),
		})
	}

	return out
}

func pastTense(op ChangeOp) string {
	switch op {
	case Added:
		return "added"
	case Removed:
		return "removed"
	case Changed, Moved:
	}

	return "changed"
}

// ownedFiles are the files a version's model accounts for: agents' prompts and the files of
// local skills.
func ownedFiles(v version) map[string]bool {
	out := map[string]bool{}

	agents, _ := v.model.Agents[keyAgents].([]any)
	for _, a := range agents {
		if p, ok := promptPath(valueAt(a, "prompt")); ok {
			out[path.Clean(p)] = true
		}
	}

	skills, _ := v.model.Agents[keySkills].([]any)
	for _, s := range skills {
		src, _ := valueAt(s, "source").(string)
		if !strings.HasPrefix(src, "./") {
			continue
		}

		dir := path.Clean(src) + "/"

		for p := range v.snap.Manifest {
			if strings.HasPrefix(p, dir) {
				out[p] = true
			}
		}
	}

	return out
}

// contractFiles maps each contract file of a version to its contract's name.
func contractFiles(v version) map[string]string {
	out := map[string]string{}

	c := lookup(v.root, []string{keySpec, "contracts"})
	if c == nil || c.Kind != yaml.MappingNode {
		return out
	}

	for i := 0; i+1 < len(c.Content); i += 2 {
		out[path.Clean(c.Content[i+1].Value)] = c.Content[i].Value
	}

	return out
}

func valueAt(v any, keys ...string) any {
	for _, k := range keys {
		m, _ := v.(map[string]any)
		v = m[k]
	}

	return v
}
