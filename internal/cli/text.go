package cli

import (
	"fmt"
	"strings"
)

// plural is n with noun, made plural unless n is 1.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}

	if stem, ok := strings.CutSuffix(noun, "y"); ok {
		return fmt.Sprintf("%d %sies", n, stem)
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

// trimLines drops the spaces tabwriter pads an empty last cell with.
func trimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}

	return strings.Join(lines, "\n")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
