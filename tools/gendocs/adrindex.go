package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

var (
	adrName    = regexp.MustCompile(`^([0-9]{4})-.+\.md$`)
	markdownLn = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	errADR     = errors.New("not an ADR")
)

// statusWithin is how many lines after its heading an ADR's Status: line may come.
const statusWithin = 3

type adr struct {
	number, title, status string
}

// renderADRIndex renders docs/adr/README.md: one line per docs/adr/NNNN-*.md, in number order.
func renderADRIndex(fsys fs.FS) (string, error) {
	entries, err := fs.ReadDir(fsys, adrDir)
	if err != nil {
		return "", fmt.Errorf("list the ADRs: %w", err)
	}

	var b strings.Builder

	b.WriteString(generatedNote + "\n")
	b.WriteString("# Architecture decision records\n\n")
	b.WriteString("One line per ADR: its number, its title and the first part of its status. ")
	b.WriteString("To add one, write `docs/adr/NNNN-<slug>.md` with the next number and run `task docs:gen`.\n\n")

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && adrName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}

	slices.Sort(names)

	for _, name := range names {
		text, err := fs.ReadFile(fsys, path.Join(adrDir, name))
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}

		a, err := parseADR(name, string(text))
		if err != nil {
			return "", fmt.Errorf("%s/%s: %w", adrDir, name, err)
		}

		fmt.Fprintf(&b, "- [ADR %s](%s): %s. Status: %s.\n", a.number, name, a.title, a.status)
	}

	return b.String(), nil
}

// parseADR reads an ADR's number from its file name, its title from its first line
// ("# ADR NNNN — <title>") and its status from the first " · " part of the Status: line just
// below, with markdown links reduced to their text.
func parseADR(name, text string) (adr, error) {
	number := adrName.FindStringSubmatch(name)[1]
	lines := strings.Split(text, "\n")

	title, ok := strings.CutPrefix(lines[0], "# ADR "+number+" — ")
	if title = strings.TrimSpace(title); !ok || title == "" {
		return adr{}, fmt.Errorf("%w: the first line is not %q", errADR, "# ADR "+number+" — <title>")
	}

	for _, line := range lines[1:min(len(lines), 1+statusWithin)] {
		status, ok := strings.CutPrefix(line, "Status:")
		if !ok {
			continue
		}

		status, _, _ = strings.Cut(status, " · ")
		status = strings.TrimSpace(markdownLn.ReplaceAllString(status, "$1"))

		if status == "" {
			return adr{}, fmt.Errorf("%w: the Status: line is empty", errADR)
		}

		return adr{number: number, title: title, status: status}, nil
	}

	return adr{}, fmt.Errorf("%w: no Status: line within %d lines of the heading", errADR, statusWithin)
}
