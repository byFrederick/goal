package gitdocs

import (
	"fmt"
	"path"
	"strings"

	"github.com/byFrederick/goal/internal/domain"
)

// globs parses a config's globs, separated by commas or new lines, each into its segments: a
// leading / is the repository's root, as no slash is.
func globs(v string) ([][]string, error) {
	var out [][]string

	for g := range strings.FieldsFuncSeq(v, func(r rune) bool { return r == ',' || r == '\n' }) {
		g = strings.Trim(strings.TrimSpace(g), "/")
		if g == "" {
			continue
		}

		segs := strings.Split(g, "/")
		for _, s := range segs {
			if _, err := path.Match(s, ""); err != nil {
				return nil, fmt.Errorf("glob %q is malformed: %w", g, domain.ErrInvalid)
			}
		}

		out = append(out, segs)
	}

	return out, nil
}

// matchAny reports whether the path name (its segments) matches any of patterns.
func matchAny(patterns [][]string, name []string) bool {
	for _, p := range patterns {
		if match(p, name) {
			return true
		}
	}

	return false
}

// match reports whether the path name matches pattern, segment by segment: ** is any number of
// segments, none included; any other segment matches as path.Match does.
func match(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			for i := range len(name) + 1 {
				if match(pattern[1:], name[i:]) {
					return true
				}
			}

			return false
		}

		if len(name) == 0 {
			return false
		}

		if ok, _ := path.Match(pattern[0], name[0]); !ok {
			return false
		}

		pattern, name = pattern[1:], name[1:]
	}

	return len(name) == 0
}
