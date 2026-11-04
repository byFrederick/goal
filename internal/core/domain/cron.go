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
// (a-b), steps (*/n, a-b/n), lists and month and weekday names, and nothing looser (no
// @descriptors, no ?, no seconds); internal/config checks a definition's cron with CronProblem.
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

	sets, problem := cronSets(expr)
	if problem != "" {
		return Cron{}, fmt.Errorf("cron %q: %s: %w", expr, problem, ErrInvalid)
	}

	fields := strings.Fields(expr)

	return Cron{
		minute: sets[0], hour: sets[1], dom: sets[2], month: sets[3], dow: sets[4],
		domAny: strings.HasPrefix(fields[2], "*"), dowAny: strings.HasPrefix(fields[4], "*"), loc: loc,
	}, nil
}

// CronProblem says why expr is not a standard five-field cron, naming the field, and "" when
// ParseCron reads it: config's check of a schedule trigger's cron, so the two never disagree.
func CronProblem(expr string) string {
	_, problem := cronSets(expr)

	return problem
}

// cronSets are the values each of expr's five fields matches, or why it has none.
func cronSets(expr string) ([5]uint64, string) {
	var sets [5]uint64

	fields := strings.Fields(expr)
	if len(fields) != len(cronSpecs) {
		return sets, fmt.Sprintf("it has %d fields, not 5", len(fields))
	}

	for i, f := range fields {
		set, problem := cronSpecs[i].parse(f)
		if problem != "" {
			return sets, cronSpecs[i].name + " " + problem
		}

		sets[i] = set
	}

	return sets, ""
}

// parse is the set of values field matches, or why it is not a field of cs.
func (cs cronSpec) parse(field string) (uint64, string) {
	var set uint64

	for part := range strings.SplitSeq(field, ",") {
		base, stepText, stepped := strings.Cut(part, "/")
		step := 1

		if stepped {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return 0, fmt.Sprintf("%q has a step that is not a whole number of at least 1", part)
			}

			step = n
		}

		lo, hi := cs.lo, cs.hi

		if base != "*" {
			from, to, ranged := strings.Cut(base, "-")
			if !ranged && stepped {
				return 0, fmt.Sprintf("%q steps from a single value; use a range or *", part)
			}

			if !ranged {
				to = from
			}

			var okLo, okHi bool

			lo, okLo = cs.value(from)
			hi, okHi = cs.value(to)

			switch {
			case !okLo || !okHi:
				return 0, fmt.Sprintf("%q is not a value in %d-%d", part, cs.lo, cs.hi)
			case lo > hi:
				return 0, fmt.Sprintf("%q runs backwards", part)
			}
		}

		for v := lo; v <= hi; v += step {
			set |= 1 << v
		}
	}

	return set, ""
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

// MinInterval is the shortest time between two of c's instants over eight days from a fixed
// Monday, which covers every minute, hour and weekday it names; zero when it fires at most once
// in them.
func (c Cron) MinInterval() time.Duration {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, c.loc).Add(-time.Minute)
	end := start.Add(8 * 24 * time.Hour)

	var (
		least time.Duration
		prev  time.Time
	)

	for t := c.Next(start); !t.IsZero() && t.Before(end); t = c.Next(t) {
		if gap := t.Sub(prev); !prev.IsZero() && (least == 0 || gap < least) {
			least = gap
		}

		prev = t
	}

	return least
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

// Between is how many instants c matches later than after and at or before upTo, at most limit,
// with the first and the last of those it counted: a schedule's gap (ADR 0040 decision 16). n is
// 0, and first and last zero, when there is none.
func (c Cron) Between(after, upTo time.Time, limit int) (first, last time.Time, n int) {
	for t := c.Next(after); !t.IsZero() && !t.After(upTo) && n < limit; t = c.Next(t) {
		if n == 0 {
			first = t
		}

		last, n = t, n+1
	}

	return first, last, n
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
