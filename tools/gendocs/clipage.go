package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// descColumn is the column where the usage text's descriptions start: a line indented this far
// continues the description above it.
const descColumn = 50

var errUsageText = errors.New("not the CLI's usage text")

// A command is one section of the CLI page: the forms of one command name, in usage order.
type command struct {
	name         string
	forms        []string
	descriptions []string
}

func (c *command) description() string { return strings.Join(c.descriptions, "\n\n") }

var (
	commandWord = regexp.MustCompile(`^[a-z][a-z-]*$`)
	flagToken   = regexp.MustCompile(`--[a-z][a-z0-9-]*(?:[ =][^\s\[\]]+)?`)
)

// parseUsage reads the CLI's usage text: the command lines up to the first blank line, each a
// form with its description, and the paragraphs after it, which hold the conventions every command
// shares.
func parseUsage(usage string) ([]*command, []string, error) {
	lines := strings.Split(usage, "\n")
	if len(lines) == 0 || lines[0] != "usage:" {
		return nil, nil, fmt.Errorf("%w: its first line is not the usage line", errUsageText)
	}

	var (
		commands []*command
		byName   = map[string]*command{}
		current  *command
		i        = 1
	)

	for ; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
		line := lines[i]

		if continuation, ok := strings.CutPrefix(line, strings.Repeat(" ", descColumn)); ok {
			if current == nil {
				return nil, nil, fmt.Errorf("%w: line %d continues no command", errUsageText, i+1)
			}

			last := len(current.descriptions) - 1
			current.descriptions[last] = strings.TrimSpace(current.descriptions[last] + " " + strings.TrimSpace(continuation))

			continue
		}

		form, description := splitForm(line)
		if !strings.HasPrefix(form, "goal ") {
			return nil, nil, fmt.Errorf("%w: line %d is not a goal command: %q", errUsageText, i+1, line)
		}

		name := commandName(form)

		current = byName[name]
		if current == nil {
			current = &command{name: name}
			byName[name] = current
			commands = append(commands, current)
		}

		current.forms = append(current.forms, form)
		current.descriptions = append(current.descriptions, description)
	}

	if len(commands) == 0 {
		return nil, nil, fmt.Errorf("%w: it lists no command", errUsageText)
	}

	var (
		conventions []string
		paragraph   []string
	)

	for _, line := range append(lines[i:], "") {
		if strings.TrimSpace(line) != "" {
			paragraph = append(paragraph, strings.TrimSpace(line))

			continue
		}

		if len(paragraph) > 0 {
			conventions = append(conventions, strings.Join(paragraph, " "))
			paragraph = nil
		}
	}

	return commands, conventions, nil
}

// splitForm splits a command line into its form and the description on the same line, if any. The
// description starts at descColumn after a space; a form long enough to reach that column puts its
// description on the next lines, so a split that leaves the form's brackets open, or a description
// that starts like an argument, is not one.
func splitForm(line string) (form, description string) {
	if len(line) <= descColumn || line[descColumn-1] != ' ' || line[descColumn] == ' ' {
		return strings.TrimSpace(line), ""
	}

	form = strings.TrimSpace(line[:descColumn])
	if strings.ContainsAny(line[descColumn:descColumn+1], `[-<"`) ||
		strings.Count(form, "[") != strings.Count(form, "]") || strings.Count(form, "<") != strings.Count(form, ">") {
		return strings.TrimSpace(line), ""
	}

	return form, strings.TrimSpace(line[descColumn:])
}

// commandName is the words of form up to its first argument or flag: goal factory push.
func commandName(form string) string {
	words := strings.Fields(form)

	n := 1
	for n < len(words) && commandWord.MatchString(words[n]) {
		n++
	}

	return strings.Join(words[:n], " ")
}

// flags are the flags of a command's forms, each with its value, in first-use order.
func (c *command) flags() []string {
	var flags []string

	for _, form := range c.forms {
		for _, f := range flagToken.FindAllString(form, -1) {
			if !slices.Contains(flags, f) {
				flags = append(flags, f)
			}
		}
	}

	return flags
}

// renderCLIPage renders the CLI reference: one section per command, after the conventions.
func renderCLIPage(commands []*command, conventions []string) string {
	var b strings.Builder

	b.WriteString(pageHead("CLI reference", "Every goal command with its forms, what it does and its flags.", cliSource))
	b.WriteString("Every command of the `goal` CLI, in the order its usage text lists them: each section shows the command's forms, " +
		"what it does and its flags. `goal` without a valid command prints the same text and exits 2.\n")

	if len(conventions) > 0 {
		b.WriteString("\n## Conventions\n")

		for _, p := range conventions {
			b.WriteString("\n" + escapeProse(p) + "\n")
		}
	}

	for _, c := range commands {
		b.WriteString("\n## " + c.name + "\n\n```text\n" + strings.Join(c.forms, "\n") + "\n```\n")

		for _, d := range c.descriptions {
			if d != "" {
				b.WriteString("\n" + escapeProse(d) + "\n")
			}
		}

		if flags := c.flags(); len(flags) > 0 {
			b.WriteString("\nFlags: `" + strings.Join(flags, "`, `") + "`.\n")
		}
	}

	return b.String()
}
