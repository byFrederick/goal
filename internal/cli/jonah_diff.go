package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// diffContext is how many unchanged lines a hunk keeps around each change, as diff -u does.
const diffContext = 3

// diffOp is one line of a diff: kept (' '), removed ('-') or added ('+'), with its newline when
// it has one.
type diffOp struct {
	kind byte
	line string
}

// hunk is ops[from:to], which starts at line baseStart of the base and nextStart of the new file
// (from 1) and covers baseLen and nextLen of their lines.
type hunk struct {
	from, to           int
	baseStart, baseLen int
	nextStart, nextLen int
}

// unifiedDiff is the unified diff from base to next of the file at path, with --- a/ and +++ b/
// headers (--- /dev/null when base is empty, a new file); empty when they are equal. Proposal
// files are small definition files, so it compares lines by a quadratic longest common
// subsequence.
func unifiedDiff(path, base, next string) string {
	ops := diffLines(splitLines(base), splitLines(next))
	hs := hunks(ops)

	if len(hs) == 0 {
		return ""
	}

	from := "a/" + path
	if base == "" {
		from = "/dev/null"
	}

	var b strings.Builder

	fmt.Fprintf(&b, "--- %s\n+++ b/%s\n", from, path)

	for _, h := range hs {
		fmt.Fprintf(&b, "@@ -%s +%s @@\n", hunkRange(h.baseStart, h.baseLen), hunkRange(h.nextStart, h.nextLen))

		for _, op := range ops[h.from:h.to] {
			b.WriteByte(op.kind)

			if text, ok := strings.CutSuffix(op.line, "\n"); ok {
				b.WriteString(text + "\n")
			} else {
				b.WriteString(op.line + "\n\\ No newline at end of file\n")
			}
		}
	}

	return b.String()
}

// splitLines is s's lines, each with its newline (the last one may have none).
func splitLines(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// diffLines turns a into b along their longest common subsequence, removals before additions.
func diffLines(a, b []string) []diffOp {
	// lcs[i][j] is the length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}

	for i, line := range slices.Backward(a) {
		for j := len(b) - 1; j >= 0; j-- {
			if line == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	ops := make([]diffOp, 0, len(a)+len(b))
	i, j := 0, 0

	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case j == len(b) || (i < len(a) && lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}

	return ops
}

// hunks groups the changes of ops with diffContext kept lines around each, joining changes no
// more than twice that apart.
func hunks(ops []diffOp) []hunk {
	var out []hunk

	for i := 0; i < len(ops); i++ {
		if ops[i].kind == ' ' {
			continue
		}

		from, last := max(0, i-diffContext), i

		for j := i + 1; j < len(ops) && j <= last+2*diffContext; j++ {
			if ops[j].kind != ' ' {
				last = j
			}
		}

		out = append(out, hunk{from: from, to: min(len(ops), last+1+diffContext)})
		i = last
	}

	// The line numbers: count each side's lines before and inside every hunk.
	base, next, at := 0, 0, 0

	for k := range out {
		h := &out[k]

		for ; at < h.from; at++ {
			base, next = advance(ops[at].kind, base, next)
		}

		h.baseStart, h.nextStart = base+1, next+1

		for ; at < h.to; at++ {
			base, next = advance(ops[at].kind, base, next)
		}

		h.baseLen, h.nextLen = base-h.baseStart+1, next-h.nextStart+1
	}

	return out
}

// advance counts a line of kind on the side or sides it belongs to.
func advance(kind byte, base, next int) (int, int) {
	if kind != '+' {
		base++
	}

	if kind != '-' {
		next++
	}

	return base, next
}

// hunkRange is a hunk's start and length as diff -u writes them: a length of 1 is left out, and
// an empty range starts at the line before it.
func hunkRange(start, n int) string {
	switch n {
	case 0:
		return strconv.Itoa(start-1) + ",0"
	case 1:
		return strconv.Itoa(start)
	default:
		return strconv.Itoa(start) + "," + strconv.Itoa(n)
	}
}
