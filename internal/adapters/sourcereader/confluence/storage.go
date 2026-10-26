package confluence

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// A page's body in Confluence's storage format: XHTML with Confluence's own elements (ac: for
// macros, links, layouts and tasks; ri: for what they point at) and HTML's named entities. It
// becomes Markdown: headings, paragraphs, lists, task lists, tables, code blocks and web links;
// macros with a body (panels, expands) keep it, the others (a table of contents, a Jira issue,
// raw HTML) are dropped, as are images and emoticons.

// The macros whose body is code.
const (
	macroCode     = "code"
	macroNoformat = "noformat"
)

// maxDepth bounds how deep the page's elements nest in what is rendered: deeper ones keep only
// their text, so a crafted page cannot exhaust goald's stack.
const maxDepth = 256

// node is an element of the page (name its tag with its prefix, "ac:link"; attr its attributes
// so named) or, without a name, a piece of text.
type node struct {
	name string
	attr map[string]string
	text string
	kids []*node
}

// toMarkdown renders a page titled title whose body is storage: the title as the top heading,
// the page's own headings one level under it.
func toMarkdown(title, storage string) (string, error) {
	root, err := parse(storage)
	if err != nil {
		return "", err
	}

	var parts []string

	if t := oneLine(title); t != "" {
		parts = append(parts, "# "+t)
	}

	return strings.Join(append(parts, blocks(root.kids)...), "\n\n") + "\n", nil
}

// parse reads storage into a tree of nodes, tolerating missing end tags and unknown entities.
func parse(storage string) (*node, error) {
	d := xml.NewDecoder(strings.NewReader(storage))
	d.Strict, d.AutoClose, d.Entity = false, voidElements, xml.HTMLEntity

	root := &node{}
	stack := []*node{root}
	// deeper counts the open elements past maxDepth, which are not in the tree.
	deeper := 0

	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return root, nil
		}

		if err != nil {
			return nil, fmt.Errorf("read the page's storage format: %w", err)
		}

		top := stack[len(stack)-1]

		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) > maxDepth {
				deeper++

				continue
			}

			n := &node{name: qualified(t.Name), attr: make(map[string]string, len(t.Attr))}
			for _, a := range t.Attr {
				n.attr[qualified(a.Name)] = a.Value
			}

			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			switch {
			case deeper > 0:
				deeper--
			case len(stack) > 1:
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			top.kids = append(top.kids, &node{text: string(t)})
		}
	}
}

// voidElements are HTML's elements without content that a page may leave open (<br>). Not
// xml.HTMLAutoClose: the decoder matches local names, and that list's link would close ac:link.
var voidElements = []string{"br", "hr", "img", "col"}

// qualified is a name with its prefix ("ac:name"), in lower case. Storage format declares no
// namespaces, so the decoder leaves a prefix as it is.
func qualified(n xml.Name) string {
	if n.Space == "" {
		return strings.ToLower(n.Local)
	}

	return strings.ToLower(n.Space + ":" + n.Local)
}

// The elements by how they render. containers are blocks made of their children's blocks;
// blockElements render as a block of their own; dropped elements render nothing.
var (
	containers = set("p", "div", "section", "article", "main", "header", "footer", "aside", "center", "dl", "dt", "dd",
		"figure", "figcaption", "details", "summary", "ac:layout", "ac:layout-section", "ac:layout-cell", "ac:rich-text-body",
		"ac:adf-fallback")
	blockElements = set("h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "ac:task-list", "table", "pre", "blockquote", "hr",
		"ac:adf-extension")
	inlineElements = set("a", "ac:link", "br", "code", "time")
	dropped        = set("ac:parameter", "ac:emoticon", "ac:image", "ac:placeholder", "ac:adf-node", "ac:adf-attribute",
		"ac:task-id", "ac:task-status", "colgroup", "col", "img", "script", "style")
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}

	return m
}

func isMacro(n *node) bool { return n.name == "ac:structured-macro" || n.name == "ac:macro" }

func isDropped(n *node) bool { return dropped[n.name] || strings.HasPrefix(n.name, "ri:") }

// isBlock reports whether n renders as blocks: a block element, a macro with a body, or any
// other element holding one.
func isBlock(n *node) bool {
	switch {
	case n.name == "" || isDropped(n) || inlineElements[n.name]:
		return false
	case containers[n.name] || blockElements[n.name]:
		return true
	case isMacro(n):
		return macroBody(n) != nil
	}

	return slices.ContainsFunc(n.kids, isBlock)
}

// blocks renders nodes as Markdown blocks: each block element its own, the inline content
// between them a paragraph.
func blocks(nodes []*node) []string {
	var (
		out  []string
		para strings.Builder
	)

	flush := func() {
		if t := clean(para.String()); t != "" {
			out = append(out, t)
		}

		para.Reset()
	}

	for _, n := range nodes {
		if !isBlock(n) {
			inline(&para, n)

			continue
		}

		flush()

		for _, b := range block(n) {
			if b != "" {
				out = append(out, b)
			}
		}
	}

	flush()

	return out
}

// block renders an element isBlock says is one.
func block(n *node) []string {
	switch n.name {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := min(int(n.name[1]-'0')+1, 6)
		if t := oneLine(inlineText(n.kids)); t != "" {
			return []string{strings.Repeat("#", level) + " " + t}
		}

		return nil
	case "ul", "ol":
		return []string{list(n)}
	case "ac:task-list":
		return []string{tasks(n)}
	case "table":
		return []string{table(n)}
	case "pre":
		return []string{fence("", rawText(n))}
	case "blockquote":
		return []string{quote(blocks(n.kids))}
	case "hr":
		return nil
	case "ac:structured-macro", "ac:macro":
		return macro(n)
	case "ac:adf-extension":
		// The cloud editor's nodes come with their rendering in storage format as a fallback.
		return blocks(children(n, "ac:adf-fallback"))
	}

	return blocks(n.kids)
}

// macroBody is what of a macro is rendered: the code of a code or noformat macro, the rich text
// body of any other; nil for a macro rendered as nothing.
func macroBody(n *node) *node {
	switch strings.ToLower(n.attr["ac:name"]) {
	case macroCode, macroNoformat:
		return child(n, "ac:plain-text-body")
	case "html":
		// Raw HTML is the page's markup, not its text.
		return nil
	}

	return child(n, "ac:rich-text-body")
}

// macro renders a macro with a body: a code block, or the body under the macro's title.
func macro(n *node) []string {
	body := macroBody(n)
	if body == nil {
		return nil
	}

	switch name := strings.ToLower(n.attr["ac:name"]); name {
	case macroCode, macroNoformat:
		lang := ""
		if name == macroCode {
			lang = language(param(n, "language"))
		}

		return []string{fence(lang, rawText(body))}
	}

	var out []string

	if t := oneLine(param(n, "title")); t != "" {
		out = append(out, t)
	}

	return append(out, blocks(body.kids)...)
}

// list renders a ul or ol, its items' own blocks indented under their marker.
func list(n *node) string {
	var items []string

	for _, li := range n.kids {
		if li.name != "li" {
			continue
		}

		marker := "- "
		if n.name == "ol" {
			marker = strconv.Itoa(len(items)+1) + ". "
		}

		if body := blocks(li.kids); len(body) > 0 {
			items = append(items, item(marker, body))
		}
	}

	return strings.Join(items, "\n")
}

// tasks renders a task list as a Markdown task list.
func tasks(n *node) string {
	var items []string

	for _, t := range n.kids {
		if t.name != "ac:task" {
			continue
		}

		marker := "- [ ] "
		if strings.TrimSpace(rawText(child(t, "ac:task-status"))) == "complete" {
			marker = "- [x] "
		}

		if body := blocks(children(t, "ac:task-body")); len(body) > 0 {
			items = append(items, item(marker, body))
		}
	}

	return strings.Join(items, "\n")
}

// item is a list item: marker before its first line, its other lines indented as far.
func item(marker string, body []string) string {
	lines := strings.Split(strings.Join(body, "\n"), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = strings.Repeat(" ", len(marker)) + lines[i]
	}

	return marker + strings.Join(lines, "\n")
}

// table renders a table's rows as a Markdown table, the first row as its header.
func table(n *node) string {
	var rows [][]string

	var walk func(*node)

	walk = func(m *node) {
		for _, k := range m.kids {
			switch k.name {
			case "tr":
				rows = append(rows, cells(k))
			case "thead", "tbody", "tfoot":
				walk(k)
			}
		}
	}

	walk(n)

	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}

	if width == 0 {
		return ""
	}

	lines := make([]string, 0, len(rows)+1)
	for i, r := range rows {
		r = append(r, make([]string, width-len(r))...)
		lines = append(lines, "| "+strings.Join(r, " | ")+" |")

		if i == 0 {
			lines = append(lines, "|"+strings.Repeat(" --- |", width))
		}
	}

	return strings.Join(lines, "\n")
}

// cells are a row's cells as one line each, their pipes escaped.
func cells(tr *node) []string {
	var out []string

	for _, c := range tr.kids {
		if c.name == "th" || c.name == "td" {
			out = append(out, strings.ReplaceAll(oneLine(strings.Join(blocks(c.kids), " ")), "|", `\|`))
		}
	}

	return out
}

// quote renders blocks as a block quote.
func quote(body []string) string {
	lines := strings.Split(strings.Join(body, "\n\n"), "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}

	return strings.Join(lines, "\n")
}

// fence renders code as a fenced code block in lang, its fence longer than any in the code; ""
// for no code.
func fence(lang, code string) string {
	code = strings.Trim(strings.ReplaceAll(code, "\r\n", "\n"), "\n")
	if strings.TrimSpace(code) == "" {
		return ""
	}

	f := "```"
	for strings.Contains(code, f) {
		f += "`"
	}

	return f + lang + "\n" + code + "\n" + f
}

// language is a code macro's language when it is a plain name ("go", "c++", "objective-c").
func language(s string) string {
	s = strings.TrimSpace(s)

	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("+#._-", r) {
			return ""
		}
	}

	return s
}

// inline writes n as inline Markdown: text with its white space as spaces, line breaks, links,
// code spans and times; macros and dropped elements write nothing.
func inline(b *strings.Builder, n *node) {
	switch {
	case n.name == "":
		b.WriteString(strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return ' '
			}

			return r
		}, n.text))
	case isDropped(n) || isMacro(n):
	case n.name == "br":
		b.WriteString("\n")
	case n.name == "a":
		link(b, n.attr["href"], inlineText(n.kids))
	case n.name == "ac:link":
		pageLink(b, n)
	case n.name == "code":
		code(b, oneLine(inlineText(n.kids)))
	case n.name == "time":
		b.WriteString(n.attr["datetime"])
	default:
		for _, k := range n.kids {
			inline(b, k)
		}
	}
}

// link writes a link to href with text when href is a web or mail address, else only the text.
func link(b *strings.Builder, href, text string) {
	href = strings.TrimSpace(href)

	u, err := url.Parse(href)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") {
		b.WriteString(text)

		return
	}

	if text = oneLine(text); text == "" {
		text = href
	}

	b.WriteString("[" + text + "](" + strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(href) + ")")
}

// pageLink writes Confluence's link element: its body as text, else the name of what it points
// at; a link to a web address as a link.
func pageLink(b *strings.Builder, n *node) {
	var text, target, name string

	if body := child(n, "ac:plain-text-link-body"); body != nil {
		text = oneLine(rawText(body))
	} else if body := child(n, "ac:link-body"); body != nil {
		text = oneLine(inlineText(body.kids))
	}

	for _, k := range n.kids {
		switch k.name {
		case "ri:page", "ri:blog-post":
			name = k.attr["ri:content-title"]
		case "ri:attachment":
			name = k.attr["ri:filename"]
		case "ri:space":
			name = k.attr["ri:space-key"]
		case "ri:url":
			target = k.attr["ri:value"]
		}
	}

	if target != "" {
		link(b, target, text)

		return
	}

	if text == "" {
		text = name
	}

	b.WriteString(text)
}

// code writes a code span of t, with double backticks when it holds one.
func code(b *strings.Builder, t string) {
	switch {
	case t == "":
	case strings.Contains(t, "`"):
		b.WriteString("`` " + t + " ``")
	default:
		b.WriteString("`" + t + "`")
	}
}

// inlineText is nodes' inline Markdown, its line breaks kept.
func inlineText(nodes []*node) string {
	var b strings.Builder
	for _, n := range nodes {
		inline(&b, n)
	}

	return clean(b.String())
}

// rawText is n's text as it is, line breaks as new lines (code, a macro's parameter).
func rawText(n *node) string {
	if n == nil {
		return ""
	}

	if n.name == "br" {
		return "\n"
	}

	var b strings.Builder

	b.WriteString(n.text)

	for _, k := range n.kids {
		b.WriteString(rawText(k))
	}

	return b.String()
}

// clean collapses each line's spaces and drops empty lines.
func clean(s string) string {
	var lines []string

	for l := range strings.SplitSeq(s, "\n") {
		if l = oneLine(l); l != "" {
			lines = append(lines, l)
		}
	}

	return strings.Join(lines, "\n")
}

// oneLine is s on one line, its white space collapsed.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// child is n's first child element named name, nil when it has none.
func child(n *node, name string) *node {
	if n == nil {
		return nil
	}

	for _, k := range n.kids {
		if k.name == name {
			return k
		}
	}

	return nil
}

// children are the children of n's child elements named name, in order.
func children(n *node, name string) []*node {
	var out []*node

	for _, k := range n.kids {
		if k.name == name {
			out = append(out, k.kids...)
		}
	}

	return out
}

// param is the value of a macro's parameter named name.
func param(n *node, name string) string {
	for _, k := range n.kids {
		if k.name == "ac:parameter" && k.attr["ac:name"] == name {
			return strings.TrimSpace(rawText(k))
		}
	}

	return ""
}
