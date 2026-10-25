package knowledge

import (
	"bytes"
	"fmt"
	"mime"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/byFrederick/goal/internal/domain"
)

// types are the media types read as each indexed type.
var types = map[string]string{
	domain.MediaText: domain.MediaText, domain.MediaMarkdown: domain.MediaMarkdown, "text/x-markdown": domain.MediaMarkdown,
	domain.MediaHTML: domain.MediaHTML, "application/xhtml+xml": domain.MediaHTML,
	domain.MediaJSON: domain.MediaJSON, domain.MediaYAML: domain.MediaYAML, "application/x-yaml": domain.MediaYAML, "text/yaml": domain.MediaYAML, "text/x-yaml": domain.MediaYAML,
}

// Extract is a document's text to index: text, Markdown, JSON and YAML as they are, HTML as text
// with its headings as Markdown headings ("## Title"), so Chunk sees the same structure in both.
// The body must be UTF-8 (a byte order mark is dropped). domain.ErrUnsupportedDocument for
// another type, domain.ErrInvalid for a body that is not text.
func Extract(mediaType string, body []byte) (string, error) {
	t, err := typeOf(mediaType)
	if err != nil {
		return "", err
	}

	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(body) || bytes.ContainsRune(body, 0) {
		return "", fmt.Errorf("the document is not UTF-8 text: %w", domain.ErrInvalid)
	}

	if t == domain.MediaHTML {
		return htmlText(body)
	}

	return strings.ReplaceAll(string(body), "\r\n", "\n"), nil
}

// typeOf is the indexed type of a media type, parameters ignored.
func typeOf(mediaType string) (string, error) {
	mt, _, err := mime.ParseMediaType(mediaType)
	if err == nil {
		if t, ok := types[strings.ToLower(mt)]; ok {
			return t, nil
		}
	}

	return "", fmt.Errorf("%q is not text, Markdown, HTML, JSON or YAML: %w", mediaType, domain.ErrUnsupportedDocument)
}

// htmlText renders an HTML document's visible text: headings as Markdown headings, blocks and
// line breaks as new lines, list items as "- " lines, whitespace collapsed outside pre. Scripts,
// styles and the head (but the title) are left out.
func htmlText(body []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("the HTML does not parse: %w", domain.ErrInvalid)
	}

	var w htmlWriter

	w.node(doc)

	return strings.TrimSpace(w.lines()), nil
}

type htmlWriter struct {
	b   strings.Builder
	pre int
}

// skipped are elements whose content is not the document's text.
var skipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true, atom.Svg: true,
	atom.Iframe: true, atom.Object: true,
}

// blocks are elements that start and end a line.
var blocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Header: true, atom.Footer: true,
	atom.Main: true, atom.Nav: true, atom.Aside: true, atom.Ul: true, atom.Ol: true, atom.Table: true, atom.Tr: true,
	atom.Blockquote: true, atom.Pre: true, atom.Dl: true, atom.Dt: true, atom.Dd: true, atom.Figure: true, atom.Hr: true,
	atom.Br: true, atom.Body: true,
}

var headings = map[atom.Atom]int{atom.H1: 1, atom.H2: 2, atom.H3: 3, atom.H4: 4, atom.H5: 5, atom.H6: 6}

// starts are what an element's text starts with besides a block's new line.
var starts = map[atom.Atom]string{atom.Li: "\n- ", atom.Td: " | ", atom.Th: " | "}

func (w *htmlWriter) node(n *html.Node) {
	if n.Type == html.TextNode {
		w.text(n.Data)

		return
	}

	if n.Type == html.ElementNode {
		w.element(n)

		return
	}

	if n.Type == html.DocumentNode {
		w.children(n)
	}
}

func (w *htmlWriter) children(n *html.Node) {
	for c := range n.ChildNodes() {
		w.node(c)
	}
}

func (w *htmlWriter) element(n *html.Node) {
	switch {
	case n.DataAtom == atom.Head:
		w.title(n)

		return
	case skipped[n.DataAtom]:
		return
	case headings[n.DataAtom] > 0:
		w.heading(headings[n.DataAtom], textOf(n))

		return
	}

	w.b.WriteString(starts[n.DataAtom])

	if blocks[n.DataAtom] {
		w.b.WriteString("\n")
		defer w.b.WriteString("\n")
	}

	if n.DataAtom == atom.Pre {
		w.pre++
		defer func() { w.pre-- }()
	}

	w.children(n)
}

// title writes the head's title, the document's first heading; the rest of the head is not text.
func (w *htmlWriter) title(head *html.Node) {
	for d := range head.Descendants() {
		if d.DataAtom == atom.Title {
			w.heading(1, textOf(d))

			return
		}
	}
}

// heading writes a Markdown heading of level with text, when it has any.
func (w *htmlWriter) heading(level int, text string) {
	if t := strings.Join(strings.Fields(text), " "); t != "" {
		w.b.WriteString("\n\n" + strings.Repeat("#", level) + " " + t + "\n\n")
	}
}

func (w *htmlWriter) text(s string) {
	if w.pre > 0 {
		w.b.WriteString(s)

		return
	}

	fields := strings.Fields(s)
	if len(fields) == 0 {
		if s != "" {
			w.b.WriteString(" ")
		}

		return
	}

	if s[0] == ' ' || s[0] == '\n' || s[0] == '\t' {
		w.b.WriteString(" ")
	}

	w.b.WriteString(strings.Join(fields, " "))

	if last := s[len(s)-1]; last == ' ' || last == '\n' || last == '\t' {
		w.b.WriteString(" ")
	}
}

// lines is the text with each line trimmed (pre's indentation too) and runs of blank lines made
// one.
func (w *htmlWriter) lines() string {
	var (
		out   strings.Builder
		blank = true
	)

	for line := range strings.SplitSeq(w.b.String(), "\n") {
		line = strings.TrimSpace(line)

		if line == "" {
			if !blank {
				out.WriteString("\n")
			}

			blank = true

			continue
		}

		out.WriteString(line + "\n")

		blank = false
	}

	return out.String()
}

// textOf is the text inside n.
func textOf(n *html.Node) string {
	var b strings.Builder

	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data + " ")
		}
	}

	return b.String()
}
