package factorymodel

import (
	"bytes"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// blankSentinel stands in for a blank line while the document is a node tree: yaml.v3 keeps
// comments with their nodes but drops blank lines, so each blank line becomes this comment
// before parsing and a blank line again after encoding. It never reaches storage.
const blankSentinel = "#goal:blank"

// tree is a parsed factory.yaml and the way it is written.
type tree struct {
	doc   *yaml.Node // the document node
	root  *yaml.Node // its top-level mapping
	style style
}

// style is how a document writes YAML, so encoding keeps it: the indentation width, whether
// sequence dashes sit at their parent's column, and whether flow collections are padded
// ("{ a: b }" rather than "{a: b}").
type style struct {
	indent         int
	compact        bool
	padMap, padSeq bool
}

var parseErrorAt = regexp.MustCompile(`line (\d+)(?:: column (\d+))?:?\s*(.*)`)

func parseError(err error) *ParseError {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	if m := parseErrorAt.FindStringSubmatch(msg); m != nil {
		line, _ := strconv.Atoi(m[1])
		col, _ := strconv.Atoi(m[2])

		return &ParseError{Line: line, Column: col, Message: m[3]}
	}

	return &ParseError{Message: msg}
}

// parse reads a document with its blank lines kept as sentinel comments. A document that is
// empty is an empty mapping; one that is not a mapping is a *ParseError.
func parse(doc []byte) (*tree, error) {
	var plain yaml.Node
	if err := yaml.Unmarshal(doc, &plain); err != nil {
		return nil, parseError(err)
	}

	st := detectStyle(doc, &plain)

	if plain.Kind == 0 || len(plain.Content) == 0 {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

		return &tree{doc: &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}, root: root, style: st}, nil
	}

	if plain.Content[0].Kind != yaml.MappingNode {
		return nil, &ParseError{Line: plain.Content[0].Line, Column: plain.Content[0].Column, Message: "factory.yaml must be a mapping"}
	}

	t := &tree{doc: &plain, root: plain.Content[0], style: st}

	var marked yaml.Node
	if err := yaml.Unmarshal(withSentinels(doc, &plain), &marked); err == nil && sameValue(&marked, &plain) {
		t.doc, t.root = &marked, marked.Content[0]
	}

	return t, nil
}

func sameValue(a, b *yaml.Node) bool {
	var va, vb any
	if a.Decode(&va) != nil || b.Decode(&vb) != nil {
		return false
	}

	return reflect.DeepEqual(va, vb)
}

// withSentinels replaces each blank line outside block scalars with the sentinel comment,
// indented like the next line so yaml.v3 attaches it to the next node.
func withSentinels(doc []byte, plain *yaml.Node) []byte {
	lines := strings.Split(string(doc), "\n")
	protected := blockScalarLines(lines, plain)

	for i := range lines {
		if protected[i+1] || strings.TrimSpace(lines[i]) != "" || i == len(lines)-1 {
			continue
		}

		lines[i] = nextIndent(lines, i) + blankSentinel
	}

	return []byte(strings.Join(lines, "\n"))
}

func nextIndent(lines []string, i int) string {
	for _, l := range lines[i+1:] {
		if t := strings.TrimLeft(l, " "); t != "" {
			return l[:len(l)-len(t)]
		}
	}

	return ""
}

// blockScalarLines are the 1-based lines holding the text of literal and folded scalars, where
// a blank line is content.
func blockScalarLines(lines []string, n *yaml.Node) map[int]bool {
	out := map[int]bool{}

	var visit func(*yaml.Node)

	visit = func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode && n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 && n.Line > 0 && n.Line <= len(lines) {
			markBlock(out, lines, n.Line)
		}

		for _, c := range n.Content {
			visit(c)
		}
	}

	visit(n)

	return out
}

// markBlock marks the lines after a block scalar's indicator line that are more indented than
// it, or blank; trailing blank lines are content only with the keep indicator (|+ or >+).
func markBlock(out map[int]bool, lines []string, line int) {
	head := lines[line-1]
	indent := len(head) - len(strings.TrimLeft(head, " "))
	keep := strings.Contains(head, "|+") || strings.Contains(head, ">+")
	last := line

	for i := line; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimLeft(l, " ")

		if t != "" && len(l)-len(t) <= indent {
			break
		}

		if t != "" || keep {
			last = i + 1
		}
	}

	for i := line + 1; i <= last; i++ {
		out[i] = true
	}
}

// render encodes a tree in its document's style.
func render(t *tree) ([]byte, error) {
	var b bytes.Buffer

	e := yaml.NewEncoder(&b)
	e.SetIndent(t.style.indent)

	if t.style.compact {
		e.CompactSeqIndent()
	}

	if err := e.Encode(t.doc); err != nil {
		return nil, err
	}

	if err := e.Close(); err != nil {
		return nil, err
	}

	out := restoreBlanks(dropFoldedBlank(b.String()))
	out = padFlow(out, t.style)

	return []byte(out), nil
}

// dropFoldedBlank removes the empty line yaml.v3 writes after a folded (>) scalar's text: it
// is not part of the value (clip chomping drops it on reading) and would add a blank line on
// every encode. Blank lines a document has are sentinels at this point, not empty lines.
func dropFoldedBlank(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])

		t := strings.TrimRight(lines[i], " ")
		if !strings.HasSuffix(t, ">") && !strings.HasSuffix(t, ">-") {
			continue
		}

		indent := len(t) - len(strings.TrimLeft(t, " "))
		j := i + 1

		for j < len(lines) && (lines[j] == "" || len(lines[j])-len(strings.TrimLeft(lines[j], " ")) > indent) {
			j++
		}

		// lines[i+1:j] is the scalar's text; an empty last line there is the extra one.
		if j > i+1 && lines[j-1] == "" {
			out = append(out, lines[i+1:j-1]...)
		} else {
			out = append(out, lines[i+1:j]...)
		}

		i = j - 1
	}

	return strings.Join(out, "\n")
}

func restoreBlanks(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		switch t := strings.TrimSpace(l); {
		case t == blankSentinel:
			lines[i] = ""
		case strings.HasSuffix(l, " "+blankSentinel):
			lines[i] = strings.TrimRight(strings.TrimSuffix(l, blankSentinel), " ")
		}
	}

	return strings.Join(lines, "\n")
}

// Format writes a whole document the way Apply writes the lines it changes: the document's own
// indentation, sequence style and flow padding, one space before line comments, blank lines
// between entries kept. Formatting a formatted document changes nothing.
func Format(doc []byte) ([]byte, error) {
	t, err := parse(doc)
	if err != nil {
		return nil, err
	}

	return render(t)
}

// detectStyle reads a document's indentation (from its first nested block mapping), sequence
// style (from its first block sequence) and flow padding.
func detectStyle(doc []byte, n *yaml.Node) style {
	st := style{indent: 2}

	if k, v := firstEntry(n, func(k, v *yaml.Node) bool {
		d := blockChildColumn(v, yaml.MappingNode) - k.Column

		return v.Line > k.Line && d >= 2 && d <= 8
	}); k != nil {
		st.indent = v.Content[0].Column - k.Column
	}

	// yaml.v3 writes a sequence's dashes indent columns right of its key, or indent-2 when
	// compact; a document that writes them closer is compact.
	if k, v := firstEntry(n, func(_, v *yaml.Node) bool { return blockChildColumn(v, yaml.SequenceNode) > 0 }); k != nil {
		st.compact = v.Content[0].Column-2-k.Column < st.indent
	}

	padded, tight := flowSpacing(string(doc), '{')
	st.padMap = padded > tight
	padded, tight = flowSpacing(string(doc), '[')
	st.padSeq = padded > tight

	return st
}

// blockChildColumn is the column of the first child of v when v is a non-empty block
// collection of the given kind, else 0.
func blockChildColumn(v *yaml.Node, kind yaml.Kind) int {
	if v.Kind != kind || v.Style&yaml.FlowStyle != 0 || len(v.Content) == 0 {
		return 0
	}

	return v.Content[0].Column
}

// firstEntry is the first mapping entry, in document order, for which match holds.
func firstEntry(n *yaml.Node, match func(k, v *yaml.Node) bool) (key, val *yaml.Node) {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if match(n.Content[i], n.Content[i+1]) {
				return n.Content[i], n.Content[i+1]
			}

			if k, v := firstEntry(n.Content[i+1], match); k != nil {
				return k, v
			}
		}

		return nil, nil
	}

	for _, c := range n.Content {
		if k, v := firstEntry(c, match); k != nil {
			return k, v
		}
	}

	return nil, nil
}

// flowSpacing counts flow collections opened with open that are padded ("{ a") and tight ("{a").
func flowSpacing(doc string, open byte) (padded, tight int) {
	scanFlowLines(strings.Split(doc, "\n"), func(_ int, line string, i int) {
		if line[i] != open || i+1 >= len(line) {
			return
		}

		switch line[i+1] {
		case ' ':
			padded++
		case '}', ']':
		default:
			tight++
		}
	})

	return padded, tight
}

// padFlow pads flow collections the way the document does.
func padFlow(doc string, st style) string {
	if !st.padMap && !st.padSeq {
		return doc
	}

	lines := strings.Split(doc, "\n")
	spaces := make([][]int, len(lines)) // byte offsets where a space goes, per line
	scanFlowLines(lines, func(n int, line string, i int) {
		if at, ok := padAt(line, i, st); ok {
			spaces[n] = append(spaces[n], at)
		}
	})

	for i, offs := range spaces {
		lines[i] = insertSpaces(lines[i], offs)
	}

	return strings.Join(lines, "\n")
}

// padAt is where a space goes next to the bracket at line[i], if the style wants one there.
func padAt(line string, i int, st style) (int, bool) {
	c := line[i]

	switch {
	case !st.padMap && (c == '{' || c == '}'), !st.padSeq && (c == '[' || c == ']'):
		return 0, false
	case c == '{' || c == '[':
		return i + 1, i+1 < len(line) && line[i+1] != ' ' && line[i+1] != closing(c)
	}

	return i, i > 0 && line[i-1] != ' ' && line[i-1] != opening(c)
}

func insertSpaces(line string, offs []int) string {
	if len(offs) == 0 {
		return line
	}

	var b strings.Builder

	prev := 0

	for _, o := range offs {
		b.WriteString(line[prev:o])
		b.WriteByte(' ')

		prev = o
	}

	b.WriteString(line[prev:])

	return b.String()
}

func closing(c byte) byte {
	if c == '{' {
		return '}'
	}

	return ']'
}

func opening(c byte) byte {
	if c == '}' {
		return '{'
	}

	return '['
}

// scanFlowLines calls fn for every flow bracket outside quotes, comments and block scalars, in
// order: n is the line's index, line the line and i the bracket's byte offset.
func scanFlowLines(lines []string, fn func(n int, line string, i int)) {
	blockIndent := -1

	for n, line := range lines {
		t := strings.TrimLeft(line, " ")
		indent := len(line) - len(t)

		if blockIndent >= 0 {
			if t == "" || indent > blockIndent {
				continue
			}

			blockIndent = -1
		}

		if scanLine(line, func(i int) { fn(n, line, i) }) {
			blockIndent = indent
		}
	}
}

// blockIndicators end a line that opens a block scalar.
var blockIndicators = []string{"|", ">", "|-", ">-", "|+", ">+"}

// scanLine calls fn for each bracket outside quotes and comments, and reports whether the line
// opens a block scalar.
func scanLine(line string, fn func(i int)) (opensBlock bool) {
	var q quoting

	for i := 0; i < len(line); i++ {
		c := line[i]

		switch {
		case q.step(line, i):
			if q.escaped {
				i++
				q.escaped = false
			}
		case c == '#' && (i == 0 || line[i-1] == ' '):
			return false
		case c == '{' || c == '}' || c == '[' || c == ']':
			fn(i)
		}
	}

	t := strings.TrimRight(line, " ")

	return q.open == 0 && slices.ContainsFunc(blockIndicators, func(s string) bool { return strings.HasSuffix(t, s) })
}

// quoting follows the quoted scalars of one line.
type quoting struct {
	open    byte // the quote that is open, 0 outside quotes
	escaped bool // the next byte is escaped
}

// step consumes line[i] when it opens, closes or is inside a quoted scalar.
func (q *quoting) step(line string, i int) bool {
	c := line[i]

	switch {
	case q.open == '"' && c == '\\':
		q.escaped = true
	case q.open != 0:
		if c == q.open {
			q.open = 0
		}
	case (c == '"' || c == '\'') && (i == 0 || strings.ContainsRune(" [{,:-", rune(line[i-1]))):
		q.open = c
	default:
		return false
	}

	return true
}

// keepOriginalLines writes edited (the encoded, edited tree) reusing the original text for every
// line the edit did not change: norm is the original tree encoded the same way, so a line of
// edited that equals a line of norm stands for the original line norm came from. Untouched
// lines keep their exact spacing, quotes and comment alignment. A run of norm lines that has
// no counterpart line by line (a folded scalar encoding reflowed, a multi-line flow collection
// joined) stands for the original lines between the neighbouring matches, as long as the edit
// left the whole run unchanged.
func keepOriginalLines(orig, norm, edited []byte) []byte {
	o, a, b := splitLines(orig), splitLines(norm), splitLines(edited)
	oa := align(o, a, func(x, y string) bool { return loose(x) == loose(y) })
	ba := align(a, b, func(x, y string) bool { return x == y })

	ab := make([]int, len(a)) // norm line → edited line, or -1
	for i := range ab {
		ab[i] = -1
	}

	for j, i := range ba {
		if i >= 0 {
			ab[i] = j
		}
	}

	out := make([]string, 0, len(b))

	for j := 0; j < len(b); j++ {
		i := ba[j]

		switch {
		case i < 0:
			out = append(out, b[j])
		case oa[i] >= 0:
			out = append(out, o[oa[i]])
		default:
			lines, n := originalRun(o, oa, ab, i, j)
			if n == 0 {
				out = append(out, b[j])

				continue
			}

			out = append(out, lines...)
			j += n - 1
		}
	}

	return []byte(strings.Join(out, "\n") + "\n")
}

// originalRun returns, for the unmatched norm line i that edited line j keeps, the original
// lines of its whole unmatched run and the run's length, or n = 0 when the run does not start
// at i or the edit changed part of it.
func originalRun(o []string, oa, ab []int, i, j int) (lines []string, n int) {
	if i > 0 && oa[i-1] < 0 {
		return nil, 0
	}

	end := i
	for end < len(oa) && oa[end] < 0 {
		if ab[end] != j+(end-i) {
			return nil, 0
		}

		end++
	}

	from, to := 0, len(o)
	if i > 0 {
		from = oa[i-1] + 1
	}

	if end < len(oa) {
		to = oa[end]
	}

	return o[from:to], end - i
}

func splitLines(b []byte) []string {
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// loose is a line with its spacing and quotes removed, which is what encoding may change in a
// line it does not otherwise touch.
func loose(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '"' || r == '\'' {
			return -1
		}

		return r
	}, s)
}

// maxAlignCells bounds the line diff's table; past it lines in the changed middle are not
// matched, which only costs reformatting.
const maxAlignCells = 4 << 20

// align matches the lines of y to lines of x (a longest common subsequence after trimming the
// common prefix and suffix) and returns, for each line of y, the index of its line in x or -1.
func align(x, y []string, eq func(a, b string) bool) []int {
	out := make([]int, len(y))
	for i := range out {
		out[i] = -1
	}

	pre := 0
	for pre < len(x) && pre < len(y) && eq(x[pre], y[pre]) {
		out[pre] = pre
		pre++
	}

	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && eq(x[len(x)-1-suf], y[len(y)-1-suf]) {
		out[len(y)-1-suf] = len(x) - 1 - suf
		suf++
	}

	xm, ym := x[pre:len(x)-suf], y[pre:len(y)-suf]
	if len(xm)*len(ym) == 0 || len(xm)*len(ym) > maxAlignCells {
		return out
	}

	lcs(xm, ym, eq, func(i, j int) { out[pre+j] = pre + i })

	return out
}

// lcs calls match for each pair of a longest common subsequence of x and y.
func lcs(x, y []string, eq func(a, b string) bool, match func(i, j int)) {
	w := len(y) + 1
	t := make([]int32, (len(x)+1)*w)

	for i, xi := range slices.Backward(x) {
		for j, yj := range slices.Backward(y) {
			switch {
			case eq(xi, yj):
				t[i*w+j] = t[(i+1)*w+j+1] + 1
			case t[(i+1)*w+j] >= t[i*w+j+1]:
				t[i*w+j] = t[(i+1)*w+j]
			default:
				t[i*w+j] = t[i*w+j+1]
			}
		}
	}

	for i, j := 0, 0; i < len(x) && j < len(y); {
		switch {
		case eq(x[i], y[j]):
			match(i, j)
			i++
			j++
		case t[(i+1)*w+j] >= t[i*w+j+1]:
			i++
		default:
			j++
		}
	}
}
