package domain

import "strings"

// PathOverlap is a pair of file globs, one from each list PathsOverlap compares, that could
// match a common path.
type PathOverlap struct {
	A, B string
}

// maxAlternatives caps how many globs one glob's braces expand to; a glob past it is taken to
// match every path, which is conservative.
const maxAlternatives = 256

// PathsOverlap reports the pairs of globs, one of a and one of b, that could match a common
// path, in the order of a and then b. Globs follow doublestar: `*`, `?`, `[...]` (`[!...]` or
// `[^...]` negated) and `\` escapes match within one path segment, `**` as a whole segment
// matches any number of segments (none included, so `web/**` matches `web`), and `{a,b}`
// expands to alternatives, which may span segments. Paths are relative to the repository; a
// leading `./` or `/` is ignored and blank globs match nothing.
//
// It is conservative: it never misses a pair that could match a common path, and it reports
// one that cannot only where a negated class meets another class or braces expand past
// maxAlternatives. Each pair is decided by a product of the two
// globs, segment by segment and character by character, in time polynomial in their lengths.
func PathsOverlap(a, b []string) []PathOverlap {
	ca, cb := compileGlobs(a), compileGlobs(b)

	var out []PathOverlap

	for _, x := range ca {
		for _, y := range cb {
			if x.meets(y) {
				out = append(out, PathOverlap{A: x.src, B: y.src})
			}
		}
	}

	return out
}

// glob is one compiled glob: its alternatives, or every path when they were too many.
type glob struct {
	src   string
	every bool
	alts  [][]segment
}

func compileGlobs(src []string) []glob {
	out := make([]glob, 0, len(src))

	for _, s := range src {
		if strings.TrimSpace(s) == "" {
			continue
		}

		g := glob{src: s}

		alts, ok := expandBraces(strings.TrimSpace(s), nil)
		if !ok {
			g.every = true
		}

		for _, alt := range alts {
			g.alts = append(g.alts, segments(alt))
		}

		out = append(out, g)
	}

	return out
}

func (g glob) meets(h glob) bool {
	if g.every || h.every {
		return true
	}

	for _, x := range g.alts {
		for _, y := range h.alts {
			if product(len(x), len(y),
				func(i int) bool { return x[i].any },
				func(j int) bool { return y[j].any },
				func(i, j int) bool { return segmentsMeet(x[i].toks, y[j].toks) }) {
				return true
			}
		}
	}

	return false
}

// product reports whether two patterns of n and m elements accept a common sequence. An
// element is either a star, which accepts any run of the other side's elements (none
// included), or one that meet says can stand for a common element. It fills the table of
// suffix pairs once, from the ends back, so it is O(n·m) calls of meet.
func product(n, m int, starP, starQ func(int) bool, meet func(i, j int) bool) bool {
	t := table{w: m + 1, ok: make([]bool, (n+1)*(m+1))}
	t.ok[n*t.w+m] = true

	for i := n; i >= 0; i-- {
		for j := m; j >= 0; j-- {
			if i < n || j < m {
				t.ok[i*t.w+j] = t.cell(i, j, i < n, j < m, i < n && starP(i), j < m && starQ(j), meet)
			}
		}
	}

	return t.ok[0]
}

// table is product's table: ok[i*w+j] says whether the suffixes from i and j accept a common
// sequence.
type table struct {
	w  int
	ok []bool
}

// cell decides the suffixes from i and j, given whether each side has an element left and
// whether that element is a star, from the cells after it.
func (t table) cell(i, j int, moreP, moreQ, starP, starQ bool, meet func(i, j int) bool) bool {
	right := moreQ && t.ok[i*t.w+j+1]  // q's element taken
	down := moreP && t.ok[(i+1)*t.w+j] // p's element taken

	if starP || starQ { // a star takes nothing or the other side's element
		return right || down
	}

	return moreP && moreQ && t.ok[(i+1)*t.w+j+1] && meet(i, j)
}

// expandBraces appends the alternatives of s's braces to out, false when they pass
// maxAlternatives. A brace without its match is a literal.
func expandBraces(s string, out []string) ([]string, bool) {
	open, end, alts := firstBraces(s)
	if open < 0 {
		return append(out, s), len(out) < maxAlternatives
	}

	for _, alt := range alts {
		var ok bool
		if out, ok = expandBraces(s[:open]+alt+s[end+1:], out); !ok {
			return out, false
		}
	}

	return out, true
}

// firstBraces finds the first `{` that has its matching `}` and splits what is between them at
// its top-level commas.
func firstBraces(s string) (int, int, []string) {
	for open := 0; open < len(s); open++ {
		switch s[open] {
		case '\\':
			open++
		case '{':
			if end, alts := matchBrace(s, open); end >= 0 {
				return open, end, alts
			}
		}
	}

	return -1, -1, nil
}

func matchBrace(s string, open int) (int, []string) {
	depth, from := 0, open+1

	var alts []string

	for i := open; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case ',':
			if depth == 1 {
				alts = append(alts, s[from:i])
				from = i + 1
			}
		case '}':
			if depth--; depth == 0 {
				return i, append(alts, s[from:i])
			}
		}
	}

	return -1, nil
}

// segment is one path segment of a glob: `**` (any) or a pattern of characters.
type segment struct {
	any  bool
	toks []token
}

func segments(s string) []segment {
	for strings.HasPrefix(s, "./") {
		s = s[2:]
	}

	var out []segment

	for part := range strings.SplitSeq(strings.TrimLeft(s, "/"), "/") {
		switch part {
		case "", ".":
		case "**":
			out = append(out, segment{any: true})
		default:
			out = append(out, segment{toks: tokens(part)})
		}
	}

	return out
}

type tokenKind uint8

const (
	tokLiteral tokenKind = iota
	tokAnyChar
	tokClass
	tokStar
)

// token is one element of a segment's pattern: a literal, `?`, a class or `*`.
type token struct {
	kind    tokenKind
	lit     rune
	negated bool
	ranges  [][2]rune
}

func tokens(seg string) []token {
	rs := []rune(seg)

	var out []token

	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '\\' && i+1 < len(rs):
			i++
			out = append(out, token{kind: tokLiteral, lit: rs[i]})
		case r == '*':
			if len(out) == 0 || out[len(out)-1].kind != tokStar {
				out = append(out, token{kind: tokStar})
			}
		case r == '?':
			out = append(out, token{kind: tokAnyChar})
		case r == '[':
			if t, next, ok := class(rs, i); ok {
				out = append(out, t)
				i = next
			} else {
				out = append(out, token{kind: tokLiteral, lit: r})
			}
		default:
			out = append(out, token{kind: tokLiteral, lit: r})
		}
	}

	return out
}

// class parses the class that opens at rs[open], returning the index of its `]`; false when it
// does not close, and the `[` is then a literal.
func class(rs []rune, open int) (token, int, bool) {
	t := token{kind: tokClass}

	i := open + 1
	if i < len(rs) && (rs[i] == '!' || rs[i] == '^') {
		t.negated = true
		i++
	}

	for ; i < len(rs); i++ {
		lo := rs[i]

		switch {
		case lo == ']' && len(t.ranges) > 0:
			return t, i, true
		case lo == '\\' && i+1 < len(rs):
			i++
			lo = rs[i]
		}

		hi := lo
		if i+2 < len(rs) && rs[i+1] == '-' && rs[i+2] != ']' {
			hi = rs[i+2]
			i += 2
		}

		t.ranges = append(t.ranges, [2]rune{lo, hi})
	}

	return token{}, 0, false
}

func segmentsMeet(p, q []token) bool {
	return product(len(p), len(q),
		func(i int) bool { return p[i].kind == tokStar },
		func(j int) bool { return q[j].kind == tokStar },
		func(i, j int) bool { return p[i].meets(q[j]) })
}

// meets reports whether two single-character tokens could match one character; two classes
// of which one is negated always could.
func (t token) meets(u token) bool {
	switch {
	case t.kind == tokAnyChar || u.kind == tokAnyChar:
		return true
	case t.kind == tokLiteral && u.kind == tokLiteral:
		return t.lit == u.lit
	case t.kind == tokLiteral:
		return u.contains(t.lit)
	case u.kind == tokLiteral:
		return t.contains(u.lit)
	case t.negated || u.negated:
		return true
	}

	for _, a := range t.ranges {
		for _, b := range u.ranges {
			if a[0] <= b[1] && b[0] <= a[1] {
				return true
			}
		}
	}

	return false
}

func (t token) contains(r rune) bool {
	for _, rg := range t.ranges {
		if rg[0] <= r && r <= rg[1] {
			return !t.negated
		}
	}

	return t.negated
}
