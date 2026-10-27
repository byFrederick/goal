package config

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// onSchedule is the event key of a trigger that fires on its cron, not on an event.
const onSchedule = "schedule"

// eventField is a placeholder's or dedupe's event field: names joined by dots (check.name).
var eventField = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// triggers checks board.triggers (ADR 0026): unique names, references to the definition's own
// work types, stations and lanes, a schedule's cron and time zone, and the create templates'
// placeholders. The schema has already checked each trigger's shape.
func (c *checker) triggers() {
	b := c.f.Spec.Board
	dup(c, "trigger", names(b.Triggers, func(t Trigger) string { return t.Name }), "board", "triggers", keyName)

	for i, t := range b.Triggers {
		base := at("spec", "board", "triggers", i)
		create := append(slices.Clone(base), "create")

		if !slices.Contains(b.WorkTypes, t.Create.Type) {
			c.errorf(checkReferences, codeUnknownWorkType, append(slices.Clone(create), "type"),
				"trigger %s: create.type %q is not one of board.workTypes", t.Name, t.Create.Type)
		}

		c.triggerPlace(t, create)

		if t.On == onSchedule {
			c.schedule(t, base)
		}

		for _, tpl := range []struct{ key, text string }{{"title", t.Create.Title}, {"brief", t.Create.Brief}} {
			if bad := badPlaceholder(tpl.text); bad != "" {
				c.errorf(checkTriggers, codePlaceholderInvalid, append(slices.Clone(create), tpl.key),
					"trigger %s: create.%s has a malformed placeholder %q; a placeholder is {{field}} or {{field.sub}}",
					t.Name, tpl.key, bad)
			}
		}
	}
}

// triggerPlace checks that create.station is a station and create.lane a lane of its route.
func (c *checker) triggerPlace(t Trigger, create []string) {
	i := slices.Index(c.stations, t.Create.Station)
	if i < 0 {
		c.errorf(checkReferences, codeUnknownStation, append(slices.Clone(create), "station"),
			"trigger %s: create.station %q is not a station", t.Name, t.Create.Station)

		return
	}

	st := c.f.Spec.Board.Stations[i]
	if _, ok := st.Route[t.Create.Lane]; t.Create.Lane != "" && !ok {
		c.errorf(checkReferences, codeUnknownLane, append(slices.Clone(create), "lane"),
			"trigger %s: create.lane %q is not a lane of station %s; its lanes are the keys of its route",
			t.Name, t.Create.Lane, st.ID)
	}
}

// schedule checks a schedule trigger's cron and time zone. The schema requires the cron.
func (c *checker) schedule(t Trigger, base []string) {
	if why := cronProblem(t.Cron); why != "" {
		c.errorf(checkTriggers, codeCronInvalid, append(slices.Clone(base), "cron"),
			"trigger %s: cron %q must be five fields (minute, hour, day of month, month, day of week): %s",
			t.Name, t.Cron, why)
	}

	if t.TimeZone == "" {
		return
	}

	if _, err := time.LoadLocation(t.TimeZone); err != nil || t.TimeZone == "Local" {
		c.errorf(checkTriggers, codeTimeZoneInvalid, append(slices.Clone(base), "timeZone"),
			"trigger %s: timeZone %q is not an IANA time zone this install knows", t.Name, t.TimeZone)
	}
}

// cronField is one field of a five-field cron: its range and, for months and weekdays, the
// names that stand for lo, lo+1….
type cronField struct {
	name   string
	lo, hi int
	names  []string
}

var cronFields = [5]cronField{
	{name: "minute", lo: 0, hi: 59},
	{name: "hour", lo: 0, hi: 23},
	{name: "day of month", lo: 1, hi: 31},
	{name: "month", lo: 1, hi: 12, names: []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}},
	{name: "day of week", lo: 0, hi: 6, names: []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}},
}

// cronProblem says why expr is not a standard five-field cron, "" when it is. It takes numbers,
// *, ranges (a-b), steps (*/n, a-b/n), lists and month and weekday names, and nothing looser
// (no @descriptors, no ?, no seconds), so the engine's parser may be stricter but never sees
// a cron this check let through that it would read differently.
func cronProblem(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != len(cronFields) {
		return fmt.Sprintf("it has %d", len(fields))
	}

	for i, f := range fields {
		if why := cronFields[i].problem(f); why != "" {
			return cronFields[i].name + " " + why
		}
	}

	return ""
}

func (cf cronField) problem(field string) string {
	for part := range strings.SplitSeq(field, ",") {
		base, step, stepped := strings.Cut(part, "/")
		if stepped {
			if n, err := strconv.Atoi(step); err != nil || n < 1 {
				return fmt.Sprintf("%q has a step that is not a whole number of at least 1", part)
			}
		}

		if base == "*" {
			continue
		}

		from, to, ranged := strings.Cut(base, "-")
		if !ranged && stepped {
			return fmt.Sprintf("%q steps from a single value; use a range or *", part)
		}

		lo, ok := cf.value(from)
		hi := lo

		if ranged {
			var okHi bool
			hi, okHi = cf.value(to)
			ok = ok && okHi
		}

		switch {
		case !ok:
			return fmt.Sprintf("%q is not a value in %d-%d", part, cf.lo, cf.hi)
		case lo > hi:
			return fmt.Sprintf("%q runs backwards", part)
		}
	}

	return ""
}

// value reads a number in the field's range, or one of its names in any case.
func (cf cronField) value(s string) (int, bool) {
	if i := slices.Index(cf.names, strings.ToUpper(s)); i >= 0 {
		return cf.lo + i, true
	}

	n, err := strconv.Atoi(s)

	return n, err == nil && n >= cf.lo && n <= cf.hi
}

// badPlaceholder is the first malformed {{…}} in s, "" when every placeholder is a dotted event
// field (spaces inside the braces are allowed). An unclosed one is the text from its {{, cut short.
func badPlaceholder(s string) string {
	const shown = 40

	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			return ""
		}

		s = s[i:]

		j := strings.Index(s[2:], "}}")
		if j < 0 {
			if r := []rune(s); len(r) > shown {
				return string(r[:shown]) + "…"
			}

			return s
		}

		if !eventField.MatchString(strings.TrimSpace(s[2 : j+2])) {
			return s[:j+4]
		}

		s = s[j+4:]
	}
}
