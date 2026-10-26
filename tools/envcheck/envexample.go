package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// assignment is one NAME=value token of .env.example.
var assignment = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)=(\S*)$`)

// devOnly marks a placeholder that is safe to commit: compose.yaml's local services use it, and
// goal up refuses it where it would matter.
const devOnly = "dev-only"

// An example is one variable that .env.example sets or shows commented out.
type example struct {
	name, value string
	line        int
}

// envExamples reads the NAME=value lines of .env.example, commented (# NAME=value) or not. A line
// may show several, separated by spaces; the first token that is not one starts its comment.
func envExamples(src []byte) []example {
	var out []example

	for i, line := range strings.Split(string(src), "\n") {
		rest := strings.TrimSpace(line)
		if c, ok := strings.CutPrefix(rest, "#"); ok {
			rest = c
		}

		for tok := range strings.FieldsSeq(rest) {
			m := assignment.FindStringSubmatch(tok)
			if m == nil {
				break
			}

			out = append(out, example{name: m[1], value: m[2], line: i + 1})
		}
	}

	return out
}

// checkEnvExample reports a variable in .env.example that the registry does not declare, and a
// secret there whose value looks real.
func checkEnvExample(reg registry, src []byte) []problem {
	entries := reg.byName()

	var problems []problem

	for _, ex := range envExamples(src) {
		where := fmt.Sprintf("%s:%d", envExampleFile, ex.line)

		e, ok := entries[ex.name]

		switch {
		case !ok:
			problems = append(problems, problem{where, ex.name + " is not declared in " + registryFile})
		case e.Class == classSecret && !placeholder(ex.value):
			problems = append(problems, problem{where, ex.name + " is a secret: leave its value empty or use a dev-only placeholder"})
		}
	}

	return problems
}

// placeholder reports whether a secret's example value holds no real secret: it is empty or
// dev-only, or it is a URL whose password is (a URL without a password holds none).
func placeholder(v string) bool {
	if v == "" || strings.Contains(v, devOnly) {
		return true
	}

	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}

	pw, has := u.User.Password()

	return !has || pw == "" || strings.Contains(pw, devOnly)
}
