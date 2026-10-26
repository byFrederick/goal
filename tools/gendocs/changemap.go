package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// pageFile is the hand-written page around the rows; rowsMarker is the line the rows replace.
const (
	pageFile   = changeMapDir + "/_page.md"
	rowsMarker = "<!-- change-map rows -->"
)

// entrySections are the sections of an entry file, in order: one per column of the table.
var entrySections = []string{"When you change", "Also change", "Run"}

var (
	errNoMarker = errors.New("needs exactly one " + rowsMarker + " line")
	errEntry    = errors.New("not a change-map entry")
)

// renderChangeMap renders docs/engineering/change-map.md: the page with its marker line replaced by
// one table row per entry file, in file-name order.
func renderChangeMap(fsys fs.FS) (string, error) {
	page, err := fs.ReadFile(fsys, pageFile)
	if err != nil {
		return "", fmt.Errorf("read the change-map page: %w", err)
	}

	names, err := fs.Glob(fsys, changeMapDir+"/*.md")
	if err != nil {
		return "", fmt.Errorf("list the change-map entries: %w", err)
	}

	slices.Sort(names)

	var rows strings.Builder

	for _, name := range names {
		if strings.HasPrefix(path.Base(name), "_") {
			continue
		}

		text, err := fs.ReadFile(fsys, name)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}

		cells, err := parseEntry(string(text))
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}

		rows.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}

	lines := strings.SplitAfter(string(page), "\n")

	i := slices.IndexFunc(lines, func(l string) bool { return strings.TrimSpace(l) == rowsMarker })
	if i < 0 || slices.IndexFunc(lines[i+1:], func(l string) bool { return strings.TrimSpace(l) == rowsMarker }) >= 0 {
		return "", fmt.Errorf("%s: %w", pageFile, errNoMarker)
	}

	return generatedNote + "\n" + strings.Join(lines[:i], "") + rows.String() + strings.Join(lines[i+1:], ""), nil
}

// parseEntry returns the cells of an entry file: the text of its three sections, in order. Each
// section holds one line, which becomes a table cell, so a line break or a | is refused.
func parseEntry(text string) ([]string, error) {
	var (
		cells   []string
		section = -1
	)

	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)

		if title, ok := strings.CutPrefix(line, "## "); ok {
			section++
			if section >= len(entrySections) || title != entrySections[section] {
				return nil, fmt.Errorf("%w: section %q where %s is expected", errEntry, line, expected(section))
			}

			cells = append(cells, "")

			continue
		}

		if line == "" {
			continue
		}

		switch {
		case section < 0:
			return nil, fmt.Errorf("%w: text before the first section, want %q", errEntry, "## "+entrySections[0])
		case cells[section] != "":
			return nil, fmt.Errorf("%w: ## %s holds more than one line; a cell is one line", errEntry, entrySections[section])
		case strings.Contains(line, "|"):
			return nil, fmt.Errorf("%w: ## %s holds a |, which would split the table cell", errEntry, entrySections[section])
		}

		cells[section] = line
	}

	if len(cells) < len(entrySections) {
		return nil, fmt.Errorf("%w: no section ## %s", errEntry, entrySections[len(cells)])
	}

	for i, c := range cells {
		if c == "" {
			return nil, fmt.Errorf("%w: ## %s is empty", errEntry, entrySections[i])
		}
	}

	return cells, nil
}

func expected(section int) string {
	if section >= len(entrySections) {
		return "no other section"
	}

	return fmt.Sprintf("%q", "## "+entrySections[section])
}
