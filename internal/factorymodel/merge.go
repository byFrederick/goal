package factorymodel

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// flowWidth is the widest a flow-style item may be written; a longer one becomes block style.
const flowWidth = 100

// mergeNode returns n with v written into it: n itself, changed in place, when it can hold v,
// otherwise a new node carrying n's comments. An unchanged value leaves n as it is (an alias
// stays an alias).
func mergeNode(n *yaml.Node, s *shape, v any, flow bool) *yaml.Node {
	if equal(normalize(read(n, s, nil), s), normalize(v, s)) {
		return n
	}

	if n.Kind == yaml.AliasNode {
		return withComments(build(v, s, flow || deref(n).Style&yaml.FlowStyle != 0), n)
	}

	if mergeSpecial(n, s, v) || mergeSameKind(n, s, v) {
		return n
	}

	return withComments(build(v, s, flow || n.Style&yaml.FlowStyle != 0), n)
}

// mergeSpecial writes a prompt's path, or a run mode without commands, into the scalar that
// holds it, keeping the scalar form.
func mergeSpecial(n *yaml.Node, s *shape, v any) bool {
	if n.Kind != yaml.ScalarNode {
		return false
	}

	switch s.kind {
	case promptRef:
		if p, ok := promptPath(v); ok {
			setScalar(n, p)

			return true
		}
	case runMode:
		if m, ok := v.(map[string]any); ok && empty(m["commands"]) {
			setScalar(n, m["mode"])

			return true
		}
	case generic, agentItem, connectItem:
	}

	return false
}

// mergeSameKind merges v into n when n is the kind of node v needs.
func mergeSameKind(n *yaml.Node, s *shape, v any) bool {
	switch x := v.(type) {
	case map[string]any:
		if n.Kind == yaml.MappingNode {
			mergeMapping(n, s, x)

			return true
		}
	case []any:
		if n.Kind == yaml.SequenceNode {
			mergeSequence(n, s, x)

			return true
		}
	default:
		if n.Kind == yaml.ScalarNode {
			setScalar(n, v)

			return true
		}
	}

	return false
}

// withComments gives n the comments of the node it replaces. A line comment stays a line
// comment only on a scalar or a flow collection; above a block collection (which yaml.v3 cannot
// write with one) it becomes the last head comment line.
func withComments(n, from *yaml.Node) *yaml.Node {
	n.HeadComment, n.LineComment, n.FootComment = from.HeadComment, from.LineComment, from.FootComment

	if n.LineComment != "" && n.Kind != yaml.ScalarNode && n.Style&yaml.FlowStyle == 0 {
		n.HeadComment = strings.TrimPrefix(n.HeadComment+"\n"+n.LineComment, "\n")
		n.LineComment = ""
	}

	return n
}

// mergeMapping writes the object v into the mapping n: changed keys in place, removed keys
// dropped with their comments, new keys at their canonical place.
func mergeMapping(n *yaml.Node, s *shape, model map[string]any) {
	v := yamlEntries(s, model)
	if mapValue(n, keyName) == nil {
		dropDefaultName(s, model, v)
	}

	flow := n.Style&yaml.FlowStyle != 0
	used := map[string]bool{}
	content := make([]*yaml.Node, 0, len(n.Content))

	for i := 0; i+1 < len(n.Content); i += 2 {
		k, val := n.Content[i], n.Content[i+1]

		nv, present := v[k.Value]

		switch {
		case !owns(s, k.Value, v):
			content = append(content, k, val)
		case present && !empty(nv):
			used[k.Value] = true
			content = append(content, k, mergeNode(val, s.child(k.Value), nv, flow))
		}
	}

	if len(content) > 0 && len(n.Content) > 0 && content[0] != n.Content[0] {
		trimLeadingBlank(content[0])
	}

	n.Content = content

	for _, k := range newKeys(s, v, used) {
		setKey(n, k, build(v[k], s.child(k), flow), s.order)
	}
}

// owns reports whether a mapping's key k is the model's to change. An agent's custom tools
// stay unless the agent switches to builtins, which replace them; a merge key stays.
func owns(s *shape, k string, v map[string]any) bool {
	switch {
	case k == "<<":
		return false
	case s.kind == agentItem && k == keyTools:
		_, builtins := v["builtins"]

		return builtins
	}

	return true
}

// yamlEntries is the object v of the model with its keys as factory.yaml writes them: renamed,
// read-only keys dropped and a connection's settings back at the top level.
func yamlEntries(s *shape, v map[string]any) map[string]any {
	out := make(map[string]any, len(v))

	for k, val := range v {
		switch {
		case s.kind == agentItem && k == "custom_tools":
		case s.kind == connectItem && (k == keyLegacy || k == keySettings):
		default:
			out[s.yamlKey(k)] = val
		}
	}

	if settings, ok := v[keySettings].(map[string]any); ok && s.kind == connectItem {
		maps.Copy(out, settings)
	}

	return out
}

// dropDefaultName leaves out a connector-form connection's name when it is the connector's.
func dropDefaultName(s *shape, v, entries map[string]any) {
	if s.kind != connectItem {
		return
	}

	connector, _ := v[keyConnector].(string)
	name, _, _ := strings.Cut(connector, "@")

	if connector != "" && v[keyName] == name {
		delete(entries, keyName)
	}
}

// newKeys are v's keys n does not have, in canonical order, then the rest sorted.
func newKeys(s *shape, v map[string]any, used map[string]bool) []string {
	var keys []string

	for k, val := range v {
		if !used[k] && !empty(val) {
			keys = append(keys, k)
		}
	}

	sort.SliceStable(keys, func(i, j int) bool {
		pi, pj := slices.Index(s.order, keys[i]), slices.Index(s.order, keys[j])

		switch {
		case pi >= 0 && pj >= 0:
			return pi < pj
		case pi >= 0 || pj >= 0:
			return pi >= 0
		}

		return keys[i] < keys[j]
	})

	return keys
}

// mergeSequence writes the list v into the sequence n. Items with an identity are matched by
// it, so a reorder moves nodes; an item whose identity changed takes the node at its old index
// (a rename keeps the item's comments). Others are matched by index.
func mergeSequence(n *yaml.Node, s *shape, v []any) {
	old := n.Content
	match := matchItems(old, s, v)
	flow := n.Style&yaml.FlowStyle != 0
	flowItems := len(old) > 0 && deref(old[0]).Style&yaml.FlowStyle != 0
	content := make([]*yaml.Node, len(v))

	for j, item := range v {
		if i := match[j]; i >= 0 {
			content[j] = mergeNode(old[i], s.child(""), item, flow)
		} else {
			content[j] = build(item, s.child(""), flow || flowItems)
		}

		fitFlow(content[j], n)
	}

	if len(content) > 0 && (len(old) == 0 || content[0] != old[0]) {
		trimLeadingBlank(content[0])
	}

	n.Content = content
}

// trimLeadingBlank drops the blank lines before a node that became the first child of its
// collection: they separated it from the sibling that used to precede it.
func trimLeadingBlank(n *yaml.Node) {
	for strings.HasPrefix(n.HeadComment, blankSentinel) {
		n.HeadComment = strings.TrimPrefix(strings.TrimPrefix(n.HeadComment, blankSentinel), "\n")
	}
}

// matchItems pairs each item of v with an index of old, or -1 for a new item.
func matchItems(old []*yaml.Node, s *shape, v []any) []int {
	match := make([]int, len(v))
	taken := make([]bool, len(old))

	for j := range match {
		match[j] = -1
		if s.identity == nil && j < len(old) {
			match[j], taken[j] = j, true
		}
	}

	if s.identity == nil {
		return match
	}

	ids := make([]string, len(v))
	for j, item := range v {
		ids[j] = s.identityOf(item)
	}

	for j, id := range ids {
		if i := unmatched(old, taken, s, id); i >= 0 {
			match[j], taken[i] = i, true
		}
	}

	// An item whose identity is new takes the node at its index when that node's identity is
	// gone: a rename.
	for j := range v {
		if match[j] < 0 && j < len(old) && !taken[j] && !slices.Contains(ids, s.identity(old[j])) {
			match[j], taken[j] = j, true
		}
	}

	return match
}

func unmatched(old []*yaml.Node, taken []bool, s *shape, id string) int {
	if id == "" {
		return -1
	}

	for i, o := range old {
		if !taken[i] && s.identity(o) == id {
			return i
		}
	}

	return -1
}

// fitFlow turns a flow-style item of a block sequence into block style when it no longer fits
// flowWidth columns.
func fitFlow(item, seq *yaml.Node) {
	if seq.Style&yaml.FlowStyle != 0 || item.Kind != yaml.MappingNode || item.Style&yaml.FlowStyle == 0 {
		return
	}

	b, err := yaml.Marshal(item)
	if err != nil || item.Column+len(strings.TrimSpace(string(b))) <= flowWidth {
		return
	}

	blockStyle(item)
}

func blockStyle(n *yaml.Node) {
	n.Style &^= yaml.FlowStyle
	for _, c := range n.Content {
		if c.Kind == yaml.MappingNode || (c.Kind == yaml.SequenceNode && hasCollections(c)) {
			blockStyle(c)
		}
	}
}

func hasCollections(n *yaml.Node) bool {
	return slices.ContainsFunc(n.Content, func(c *yaml.Node) bool { return c.Kind != yaml.ScalarNode })
}

// build makes a new node for v at a position of shape s. Mappings take the canonical key order;
// lists of plain values and anything inside a flow collection are written in flow style.
func build(v any, s *shape, flow bool) *yaml.Node {
	if n := buildSpecial(v, s); n != nil {
		return n
	}

	switch x := v.(type) {
	case map[string]any:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if flow {
			n.Style = yaml.FlowStyle
		}

		entries := yamlEntries(s, x)
		dropDefaultName(s, x, entries)

		for _, k := range newKeys(s, entries, nil) {
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: k},
				build(entries[k], s.child(k), flow))
		}

		return n
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if flow || !slices.ContainsFunc(x, isCollection) {
			n.Style = yaml.FlowStyle
		}

		for _, e := range x {
			n.Content = append(n.Content, build(e, s.child(""), n.Style&yaml.FlowStyle != 0))
		}

		return n
	}

	return scalarNode(v)
}

// buildSpecial writes a prompt as its path and a run mode without commands as the mode alone.
func buildSpecial(v any, s *shape) *yaml.Node {
	switch s.kind {
	case promptRef:
		if p, ok := promptPath(v); ok {
			return scalarNode(p)
		}
	case runMode:
		if m, ok := v.(map[string]any); ok && empty(m["commands"]) {
			return scalarNode(m["mode"])
		}
	case generic, agentItem, connectItem:
	}

	return nil
}

func isCollection(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}

	return false
}

func promptPath(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case map[string]any:
		p, ok := x["path"].(string)

		return p, ok
	}

	return "", false
}

func scalarNode(v any) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode}
	setScalar(n, v)

	return n
}

// setScalar writes a plain value into a scalar node, keeping its quote style while it stays a
// single-line string.
func setScalar(n *yaml.Node, v any) {
	tag, value := tagStr, ""

	switch x := v.(type) {
	case nil:
		tag, value = "!!null", "null"
	case bool:
		tag, value = "!!bool", strconv.FormatBool(x)
	case json.Number:
		tag, value = "!!float", x.String()
		if _, err := x.Int64(); err == nil {
			tag = "!!int"
		}
	case string:
		value = x
	default:
		value = fmt.Sprint(x)
	}

	if tag != tagStr || strings.Contains(value, "\n") != strings.Contains(n.Value, "\n") {
		n.Style &^= yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle
	}

	n.Kind, n.Tag, n.Value = yaml.ScalarNode, tag, value
}
