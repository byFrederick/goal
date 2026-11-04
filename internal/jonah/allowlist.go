package jonah

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// DefinitionCheck checks the files a proposal's edits give (ADR 0039 decision 6);
// config.ProposalCheck satisfies it, and cmd/goald wires it.
type DefinitionCheck interface {
	// Problems are the messages of the blocking problems config.Check reports on files, a
	// definition's files by path.
	Problems(files map[string][]byte) []string
	// ParseFactory is factory.yaml's raw content as a tree of map[string]any, []any and JSON
	// scalars.
	ParseFactory(raw []byte) (any, error)
}

// keyedLists are the lists of factory.yaml whose entries are matched by a key, not by index, and
// that key: a reorder or an entry added or removed is a change at the list or the entry.
var keyedLists = map[string]string{
	"spec.agents":         "name",
	"spec.skills":         "name",
	"spec.rules":          "name",
	"spec.board.stations": "id",
}

// localSkill is the source prefix of a skill in the definition's own skills/ folder.
const localSkill = "./skills/"

// factoryChange is one difference between two factory.yaml trees: at path, before became after
// (absent when the key or entry is not there).
type factoryChange struct {
	path          []string
	before, after any
	absent        [2]bool
}

// FactoryChangeAllowed reports whether every difference between factory.yaml's trees before and
// after is at a path a proposal may change (ADR 0039 decision 6), and the first that is not:
//
//   - spec.agents[*].model, to a model in models, the organization policy's allowed models, or
//     removed;
//   - spec.agents[*].skills;
//   - spec.skills[*], an entry whose source is a local ./skills/ folder before and after, and
//     their order;
//   - spec.rules and spec.contracts;
//   - spec.board.stations[*].wip, .buffer and .gate.maxRework.
//
// Agents and skills are matched by name and stations by id, so a reorder of agents or stations
// is a change to a path no proposal may change.
func FactoryChangeAllowed(before, after any, models []string) (string, bool) {
	var changes []factoryChange

	diffTree(nil, before, after, false, false, &changes)

	for _, c := range changes {
		if !changeAllowed(c, models) {
			return strings.Join(c.path, "."), false
		}
	}

	return "", true
}

// changeAllowed reports whether c is at a path a proposal may change.
func changeAllowed(c factoryChange, models []string) bool {
	p := c.path

	switch {
	case at(p, "spec", "rules"), at(p, "spec", "contracts"):
		return true
	case len(p) >= 4 && at(p, "spec", "agents") && p[3] == "skills":
		return true
	case len(p) == 4 && at(p, "spec", "agents") && p[3] == "model":
		m, ok := c.after.(string)

		return c.absent[1] || ok && slices.Contains(models, m)
	case len(p) == 2 && at(p, "spec", "skills"): // their order
		return true
	case len(p) >= 3 && at(p, "spec", "skills"):
		return (c.absent[0] || localSource(c.before, p)) && (c.absent[1] || localSource(c.after, p))
	case len(p) == 5 && at(p, "spec", "board") && p[2] == "stations" && (p[4] == "wip" || p[4] == "buffer"):
		return true
	case len(p) == 6 && at(p, "spec", "board") && p[2] == "stations" && p[4] == "gate" && p[5] == "maxRework":
		return true
	default:
		return false
	}
}

// localSource reports whether v, the skill entry at p or a field of it, belongs to a local
// ./skills/ skill: an entry's source, or the field source itself.
func localSource(v any, p []string) bool {
	if len(p) == 4 && p[3] == "source" {
		s, ok := v.(string)

		return ok && strings.HasPrefix(s, localSkill)
	}

	entry, ok := v.(map[string]any)
	if !ok {
		return false
	}

	s, ok := entry["source"].(string)

	return ok && strings.HasPrefix(s, localSkill)
}

// at reports whether p is prefix or a path under it.
func at(p []string, prefix ...string) bool {
	return len(p) >= len(prefix) && slices.Equal(p[:len(prefix)], prefix)
}

// diffTree appends to out every difference between a and b at path. noA and noB say a side is
// absent.
func diffTree(path []string, a, b any, noA, noB bool, out *[]factoryChange) {
	change := func() {
		*out = append(*out, factoryChange{path: slices.Clone(path), before: a, after: b, absent: [2]bool{noA, noB}})
	}

	if noA || noB {
		if noA != noB {
			change()
		}

		return
	}

	ma, okA := a.(map[string]any)
	mb, okB := b.(map[string]any)

	if okA && okB {
		for _, k := range unionKeys(ma, mb) {
			va, inA := ma[k]
			vb, inB := mb[k]
			diffTree(append(path, k), va, vb, !inA, !inB, out)
		}

		return
	}

	la, okA := a.([]any)
	lb, okB := b.([]any)

	if key, keyed := keyedLists[strings.Join(path, ".")]; keyed && okA && okB {
		if diffKeyed(path, key, la, lb, out) {
			return
		}
	}

	if !reflect.DeepEqual(a, b) {
		change()
	}
}

// diffKeyed appends the differences between two lists whose entries are matched by key: one per
// entry added, removed or changed, at path and the entry's key, and a change at path when the
// order of the entries both have differs. It reports false when an entry has no string key or one
// twice, so the lists are compared whole.
func diffKeyed(path []string, key string, a, b []any, out *[]factoryChange) bool {
	ia, orderA, okA := byKey(a, key)
	ib, orderB, okB := byKey(b, key)

	if !okA || !okB {
		return false
	}

	both := func(order []string, other map[string]any) []string {
		return slices.DeleteFunc(slices.Clone(order), func(k string) bool { _, ok := other[k]; return !ok })
	}

	if !slices.Equal(both(orderA, ib), both(orderB, ia)) {
		*out = append(*out, factoryChange{path: slices.Clone(path), before: a, after: b})
	}

	for _, k := range unionKeys(ia, ib) {
		va, inA := ia[k]
		vb, inB := ib[k]
		diffTree(append(slices.Clone(path), k), va, vb, !inA, !inB, out)
	}

	return true
}

// byKey is list's entries by their key and the keys in order; false when an entry has no string
// key or a key is there twice.
func byKey(list []any, key string) (map[string]any, []string, bool) {
	out := make(map[string]any, len(list))
	order := make([]string, 0, len(list))

	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, nil, false
		}

		k, ok := m[key].(string)
		if _, dup := out[k]; !ok || dup {
			return nil, nil, false
		}

		out[k], order = e, append(order, k)
	}

	return out, order, true
}

// unionKeys are the keys of a and b, sorted, each once.
func unionKeys(a, b map[string]any) []string {
	keys := slices.Collect(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}

	slices.Sort(keys)

	return keys
}

// factoryProblem is why the edit of factory.yaml in files is refused, or "" when it is allowed:
// a path no proposal may change, or a factory.yaml that does not parse before or after.
func factoryProblem(check DefinitionCheck, before, after []byte, models []string) string {
	a, err := check.ParseFactory(before)
	if err != nil {
		return fmt.Sprintf("the version's factory.yaml does not parse: %v", err)
	}

	b, err := check.ParseFactory(after)
	if err != nil {
		return fmt.Sprintf("factory.yaml does not parse after the edit: %v", err)
	}

	if path, ok := FactoryChangeAllowed(a, b, models); !ok {
		return "factory.yaml path " + path + " is not one a proposal may change"
	}

	return ""
}
