package domain

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Cron is a standard five-field cron (minute, hour, day of the month, month and weekday) read in a
// time zone, as a schedule trigger declares it (ADR 0026 decision 1). It takes numbers, *, ranges
// (a-b), steps (*/n, a-b/n), lists and month and weekday names, like internal/config's check.
// When both the day of month and the day of week are restricted (neither starts with *), a day matching either matches,
// as in cron(8).
type Cron struct {
	minute, hour, dom, month, dow uint64
	domAny, dowAny                bool
	loc                           *time.Location
}

// cronSpec is one field of a cron: its range and, for months and weekdays, the names that stand
// for lo, lo+1….
type cronSpec struct {
	name   string
	lo, hi int
	names  []string
}

var cronSpecs = [5]cronSpec{
	{name: "minute", lo: 0, hi: 59},
	{name: "hour", lo: 0, hi: 23},
	{name: "day of month", lo: 1, hi: 31},
	{name: "month", lo: 1, hi: 12, names: []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}},
	{name: "day of week", lo: 0, hi: 6, names: []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}},
}

// ParseCron reads expr in timeZone (an IANA name, "" for UTC). The error wraps ErrInvalid.
func ParseCron(expr, timeZone string) (Cron, error) {
	loc := time.UTC

	if timeZone != "" {
		l, err := time.LoadLocation(timeZone)
		if err != nil || timeZone == "Local" {
			return Cron{}, fmt.Errorf("time zone %q is not an IANA time zone: %w", timeZone, ErrInvalid)
		}

		loc = l
	}

	fields := strings.Fields(expr)
	if len(fields) != len(cronSpecs) {
		return Cron{}, fmt.Errorf("cron %q has %d fields, not 5: %w", expr, len(fields), ErrInvalid)
	}

	var sets [5]uint64

	for i, f := range fields {
		set, err := cronSpecs[i].parse(f)
		if err != nil {
			return Cron{}, fmt.Errorf("cron %q: %s %w", expr, cronSpecs[i].name, err)
		}

		sets[i] = set
	}

	return Cron{
		minute: sets[0], hour: sets[1], dom: sets[2], month: sets[3], dow: sets[4],
		domAny: strings.HasPrefix(fields[2], "*"), dowAny: strings.HasPrefix(fields[4], "*"), loc: loc,
	}, nil
}

func (cs cronSpec) parse(field string) (uint64, error) {
	var set uint64

	for part := range strings.SplitSeq(field, ",") {
		base, stepText, stepped := strings.Cut(part, "/")
		step := 1

		if stepped {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("%q has a step that is not a whole number of at least 1: %w", part, ErrInvalid)
			}

			step = n
		}

		lo, hi := cs.lo, cs.hi

		if base != "*" {
			from, to, ranged := strings.Cut(base, "-")
			if !ranged && stepped {
				return 0, fmt.Errorf("%q steps from a single value: %w", part, ErrInvalid)
			}

			if !ranged {
				to = from
			}

			var okLo, okHi bool

			lo, okLo = cs.value(from)
			hi, okHi = cs.value(to)

			switch {
			case !okLo || !okHi:
				return 0, fmt.Errorf("%q is not a value in %d-%d: %w", part, cs.lo, cs.hi, ErrInvalid)
			case lo > hi:
				return 0, fmt.Errorf("%q runs backwards: %w", part, ErrInvalid)
			}
		}

		for v := lo; v <= hi; v += step {
			set |= 1 << v
		}
	}

	return set, nil
}

func (cs cronSpec) value(s string) (int, bool) {
	if i := slices.Index(cs.names, strings.ToUpper(s)); i >= 0 {
		return cs.lo + i, true
	}

	n, err := strconv.Atoi(s)

	return n, err == nil && n >= cs.lo && n <= cs.hi
}

// cronHorizon is how far Next looks for an instant before it gives up (a cron such as 30 Feb
// never matches).
const cronHorizon = 5 * 366 * 24 * time.Hour

// Next is the first instant c matches that is later than after, in UTC; the zero time when none comes
// within five years.
func (c Cron) Next(after time.Time) time.Time {
	t := after.In(c.loc).Truncate(time.Minute).Add(time.Minute)
	end := after.Add(cronHorizon)

	for t.Before(end) {
		y, mo, d := t.Date()

		switch {
		case c.month&(1<<uint(mo)) == 0:
			t = time.Date(y, mo+1, 1, 0, 0, 0, 0, c.loc)
		case !c.dayMatches(t):
			t = time.Date(y, mo, d+1, 0, 0, 0, 0, c.loc)
		case c.hour&(1<<uint(t.Hour())) == 0:
			// To the next hour of the clock by elapsed time, so an hour the clock repeats is
			// seen at its first pass.
			t = t.Add(time.Duration(60-t.Minute()) * time.Minute)
		case c.minute&(1<<uint(t.Minute())) == 0:
			t = t.Add(time.Minute)
		default:
			return t.UTC()
		}
	}

	return time.Time{}
}

// Latest is the newest instant c matches that is later than after and at or before upTo, in UTC; the zero
// time when there is none.
func (c Cron) Latest(after, upTo time.Time) time.Time {
	var last time.Time

	for t := c.Next(after); !t.IsZero() && !t.After(upTo); t = c.Next(t) {
		last = t
	}

	return last
}

func (c Cron) dayMatches(t time.Time) bool {
	dom := c.dom&(1<<uint(t.Day())) != 0
	dow := c.dow&(1<<uint(t.Weekday())) != 0

	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dow
	case c.dowAny:
		return dom
	default:
		return dom || dow
	}
}
