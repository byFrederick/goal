package factorymodel

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// label names an item for people: a station by its name, else its id written as words ("Approve
// spec"); anything else by its name.
func label(kind string, item any, id string) string {
	if kind == TargetStation {
		if name, ok := valueAt(item, keyName).(string); ok && name != "" {
			return name
		}

		return capitalize(strings.ReplaceAll(id, "-", " "))
	}

	return id
}

func capitalize(s string) string {
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}

func addedSummary(kind string, item any, id string) string {
	if kind == TargetStation {
		if k, ok := valueAt(item, "kind").(string); ok {
			return "New " + k + " station " + label(kind, item, id)
		}
	}

	return "New " + kind + " " + label(kind, item, id)
}

// itemSummary describes the changed fields of an item, one clause per field.
func itemSummary(kind, name string, fields []string, from, to any) string {
	if len(fields) == 0 {
		return capitalize(kind) + " " + name + " files changed"
	}

	if kind == TargetConnection {
		if s, ok := connectionSummary(name, fields, from, to); ok {
			return s
		}
	}

	clauses := make([]string, 0, len(fields))
	for _, f := range fields {
		clauses = append(clauses, fieldClause(kind, name, f, valueAt(from, f), valueAt(to, f)))
	}

	return strings.Join(clauses, "; ")
}

// fieldClause describes one changed field of an item.
func fieldClause(kind, name, field string, from, to any) string {
	switch {
	case field == "checks":
		return checksClause(name, from, to)
	case field == "action" && to == "merge":
		return name + " becomes a merge step"
	case field == "action" && from == "merge":
		return name + " is no longer a merge step"
	case field == "prompt" && kind == TargetAgent:
		return "Prompt of " + name + " changed"
	case field == "skills" || field == keyAgents || field == "connections":
		return listClause(name, field, from, to)
	case isScalar(from) && isScalar(to):
		return fmt.Sprintf("%s: %s %s → %s", name, words(field), show(from), show(to))
	}

	return name + ": " + words(field) + " changed"
}

func checksClause(name string, from, to any) string {
	added, removed := listDelta(commands(from), commands(to))

	switch {
	case len(added) > 0 && len(removed) == 0:
		return name + " also runs " + joinWords(added)
	case len(removed) > 0 && len(added) == 0:
		return name + " no longer runs " + joinWords(removed)
	}

	return name + ": checks changed"
}

// commands are a station's checks as people name them: the command line, or the check kind.
func commands(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))

	for _, c := range l {
		switch x := c.(type) {
		case string:
			out = append(out, x)
		case map[string]any:
			if run, ok := x["run"].(string); ok {
				out = append(out, run)
			} else if k, ok := x["kind"].(string); ok {
				out = append(out, k)
			}
		}
	}

	return out
}

func listClause(name, field string, from, to any) string {
	added, removed := listDelta(strs(from), strs(to))

	var parts []string
	if len(added) > 0 {
		parts = append(parts, "gains "+words(singular(field, len(added)))+" "+joinWords(added))
	}

	if len(removed) > 0 {
		parts = append(parts, "loses "+words(singular(field, len(removed)))+" "+joinWords(removed))
	}

	if len(parts) == 0 {
		return name + ": " + words(field) + " reordered"
	}

	return name + " " + strings.Join(parts, " and ")
}

func singular(field string, n int) string {
	if n == 1 {
		return strings.TrimSuffix(field, "s")
	}

	return field
}

// connectionSummary says who and what a connection gains or loses: "browser gets
// frontend-engineer and two write tools".
func connectionSummary(name string, fields []string, from, to any) (string, bool) {
	if slices.ContainsFunc(fields, func(f string) bool { return f != keyAgents && f != keyTools }) {
		return "", false
	}

	gotAgents, lostAgents := listDelta(strs(valueAt(from, keyAgents)), strs(valueAt(to, keyAgents)))
	fromTools, _ := valueAt(from, keyTools).(map[string]any)
	toTools, _ := valueAt(to, keyTools).(map[string]any)

	var newTools, writeTools int

	for t, rule := range toTools {
		if _, had := fromTools[t]; !had {
			newTools++

			if valueAt(rule, "access") == "write" {
				writeTools++
			}
		}
	}

	var gets []string
	if len(gotAgents) > 0 {
		gets = append(gets, joinWords(gotAgents))
	}

	if newTools > 0 {
		kind := keyTools
		if writeTools == newTools {
			kind = "write tools"
		}

		gets = append(gets, count(newTools, strings.TrimSuffix(kind, "s"), kind))
	}

	switch {
	case len(gets) > 0 && len(lostAgents) == 0:
		return name + " gets " + strings.Join(gets, " and "), true
	case len(gets) == 0 && len(lostAgents) > 0:
		return name + " loses " + joinWords(lostAgents), true
	}

	return "", false
}

// fieldSummary describes a change of a section field.
func fieldSummary(key string, op ChangeOp, from, to any) string {
	l := fieldLabels[key]
	if l == "" {
		l = capitalize(words(key))
	}

	switch {
	case op == Added && isScalar(to):
		return l + " set to " + show(to)
	case op == Removed:
		return l + " removed"
	case isScalar(from) && isScalar(to) && len(show(from))+len(show(to)) <= 60:
		return l + ": " + show(from) + " → " + show(to)
	}

	return l + " changed"
}

var fieldLabels = map[string]string{
	"id": "Factory id", "work_types": "Work types", "default_model": "Default model",
	keyLibrarian: "The Done station's worker", "flow": "Flow policy",
}

func isScalar(v any) bool { return !isCollection(v) }

func show(v any) string {
	if v == nil {
		return "none"
	}

	return fmt.Sprint(v)
}

// words writes a model key as words: max_rework → max rework.
func words(key string) string { return strings.ReplaceAll(key, "_", " ") }

func strs(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))

	for _, e := range l {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

// listDelta is what to has that from lacks, and the reverse, in order.
func listDelta(from, to []string) (added, removed []string) {
	for _, s := range to {
		if !slices.Contains(from, s) {
			added = append(added, s)
		}
	}

	for _, s := range from {
		if !slices.Contains(to, s) {
			removed = append(removed, s)
		}
	}

	return added, removed
}

// joinWords joins names as a sentence does: "a", "a and b", "a, b and c".
func joinWords(s []string) string {
	if len(s) <= 1 {
		return strings.Join(s, "")
	}

	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

var numberWords = []string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

func count(n int, one, many string) string {
	w := strconv.Itoa(n)
	if n < len(numberWords) {
		w = numberWords[n]
	}

	if n == 1 {
		return w + " " + one
	}

	return w + " " + many
}
