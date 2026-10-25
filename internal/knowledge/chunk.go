package knowledge

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Chunk sizes, in characters: about 512 tokens a piece, with about 64 tokens of the piece before
// it as context for the embedding (spec 4 §4.8).
const (
	pieceChars   = 2048
	overlapChars = 256
	maxHeading   = 1000
)

// Piece is one chunk of a document's text before it is embedded. Its Text is its own part of the
// document, the pieces in order joining back into the whole (a section's heading line is the
// start of its first piece); Heading is the path of the headings it is under ("Install >
// Docker"); Overlap is the end of the previous piece of its section, which only its embedding
// sees.
type Piece struct {
	Heading string
	Text    string
	Overlap string
}

// EmbedText is what the embedding of p reads: its heading path, the overlap and its text.
func (p Piece) EmbedText() string {
	return strings.TrimSpace(p.Heading + "\n\n" + p.Overlap + p.Text)
}

var (
	headingLine = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t#]*$`)
	fenceLine   = regexp.MustCompile("^[ \t]*(```|~~~)")
)

// Chunk splits text (Markdown, or what Extract made of HTML) into pieces of at most about
// pieceChars characters: at its headings first (none inside a fenced code block), then at blank
// lines, then lines, then spaces, and at any character only when a line has no space. A heading
// with nothing under it but deeper headings joins the next section's first piece. It is pure;
// text without any heading is one section.
func Chunk(text string) []Piece {
	var (
		out     []Piece
		path    []string
		pending string // heading lines waiting for a section with text
		body    []string
		fenced  bool
	)

	flush := func() {
		section := strings.TrimSpace(strings.Join(body, "\n"))
		body = body[:0]

		if section == "" || isOnlyHeadings(section) {
			pending = strings.TrimSpace(pending + "\n" + section)

			return
		}

		heading := truncateRunes(strings.Join(path, " > "), maxHeading)
		out = append(out, split(heading, strings.TrimSpace(pending+"\n\n"+section))...)
		pending = ""
	}

	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if fenceLine.MatchString(line) {
			fenced = !fenced
		}

		if m := headingLine.FindStringSubmatch(line); m != nil && !fenced {
			flush()

			level := len(m[1])
			path = append(path[:min(len(path), level-1)], strings.TrimSpace(m[2]))
		}

		body = append(body, line)
	}

	flush()

	if pending != "" {
		out = append(out, split(truncateRunes(strings.Join(path, " > "), maxHeading), pending)...)
	}

	return out
}

// isOnlyHeadings reports whether a section is heading lines and nothing else.
func isOnlyHeadings(section string) bool {
	for line := range strings.SplitSeq(section, "\n") {
		if strings.TrimSpace(line) != "" && !headingLine.MatchString(line) {
			return false
		}
	}

	return true
}

// split cuts one section into pieces of at most pieceChars characters, each but the first with
// the end of the one before as its overlap.
func split(heading, section string) []Piece {
	var out []Piece

	for i, text := range pack(section) {
		p := Piece{Heading: heading, Text: text}
		if i > 0 {
			p.Overlap = tail(out[i-1].Text, overlapChars) + " … "
		}

		out = append(out, p)
	}

	return out
}

// pack groups a section's paragraphs into pieces of at most pieceChars characters, cutting a
// paragraph that is longer by lines and words.
func pack(section string) []string {
	var (
		out   []string
		cur   strings.Builder
		runes int // cur's characters
	)

	add := func(part, sep string) {
		n := utf8.RuneCountInString(part)

		if cur.Len() > 0 && runes+len(sep)+n > pieceChars {
			out = append(out, cur.String())
			cur.Reset()

			runes = 0
		}

		if cur.Len() > 0 {
			cur.WriteString(sep)

			runes += len(sep)
		}

		cur.WriteString(part)

		runes += n
	}

	for para := range strings.SplitSeq(section, "\n\n") {
		para = strings.Trim(para, "\n")
		if para == "" {
			continue
		}

		if utf8.RuneCountInString(para) <= pieceChars {
			add(para, "\n\n")

			continue
		}

		for i, part := range cut(para) {
			sep := "\n"
			if i == 0 {
				sep = "\n\n"
			}

			add(part, sep)
		}
	}

	if cur.Len() > 0 {
		out = append(out, cur.String())
	}

	return out
}

// cut splits a long paragraph into parts of at most pieceChars characters: at lines, then at
// spaces, then anywhere. Each cut reads only the part it cuts, so a line costs its length.
func cut(para string) []string {
	var out []string

	for line := range strings.SplitSeq(para, "\n") {
		for end := runeOffset(line, pieceChars); end < len(line); end = runeOffset(line, pieceChars) {
			at := strings.LastIndexByte(line[:end], ' ')
			if at <= 0 {
				at = end
			}

			out = append(out, strings.TrimRight(line[:at], " "))
			line = strings.TrimLeft(line[at:], " ")
		}

		if line != "" {
			out = append(out, line)
		}
	}

	return out
}

// runeOffset is the byte offset in s after its first n characters, len(s) when it has no more.
func runeOffset(s string, n int) int {
	for i := range s {
		if n == 0 {
			return i
		}

		n--
	}

	return len(s)
}

// tail is the end of s, at most n characters, starting at a word.
func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}

	t := string(r[len(r)-n:])
	if i := strings.IndexAny(t, " \n"); i >= 0 && i < len(t)-1 {
		t = t[i+1:]
	}

	return t
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}

	return s
}
