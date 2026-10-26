package domain

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// pullRequestKey is the output field in which an agent station reports the pull request it
// opened, and pullRequestsKey the one in which it reports several (in several repositories, say).
// They are conventions of station contracts, not station or factory names.
const (
	pullRequestKey  = "pull_request"
	pullRequestsKey = "pull_requests"
)

// summaryKey is the output field in which a station reports what it did in a sentence
// ("Postmortem filed"). Like pull_request, it is a convention of station contracts.
const summaryKey = "summary"

// PullRequest returns the pull request URL the item's outputs name: the pull_request field of
// the output of the latest station on the line that has one, or "" when none does.
func (it Item) PullRequest(l Line) string {
	return it.latestOutput(l, func(out map[string]any) string {
		if url, _ := out[pullRequestKey].(string); strings.HasPrefix(url, "https://") {
			return url
		}

		return ""
	})
}

// PullRequests returns every pull request URL the item's outputs name, the ones a merge station
// merges (spec 4 §4.7): the pull_request string and the pull_requests array of each station's
// latest output, in the line's station order, each once. Only https URLs count.
func (it Item) PullRequests(l Line) []string {
	out := []string{}

	add := func(v any) {
		if url, _ := v.(string); strings.HasPrefix(url, "https://") && !slices.Contains(out, url) {
			out = append(out, url)
		}
	}

	for _, st := range l.Stations {
		raw, ok := it.Outputs[st.ID]
		if !ok {
			continue
		}

		var fields map[string]any
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}

		add(fields[pullRequestKey])

		if many, ok := fields[pullRequestsKey].([]any); ok {
			for _, v := range many {
				add(v)
			}
		}
	}

	return out
}

// Summary returns what the item's outputs say was done: the summary field, trimmed, of the
// output of the latest station on the line that has a non-blank one, or "" when none does.
func (it Item) Summary(l Line) string {
	return it.latestOutput(l, func(out map[string]any) string {
		s, _ := out[summaryKey].(string)

		return strings.TrimSpace(s)
	})
}

// latestOutput returns the first non-empty value field gives for the item's station outputs
// that are JSON objects, from the last station of the line to the first.
func (it Item) latestOutput(l Line, field func(map[string]any) string) string {
	for _, st := range slices.Backward(l.Stations) {
		raw, ok := it.Outputs[st.ID]
		if !ok {
			continue
		}

		var out map[string]any
		if json.Unmarshal(raw, &out) != nil {
			continue
		}

		if v := field(out); v != "" {
			return v
		}
	}

	return ""
}

// StationLoad is the work at one station now: items waiting to be pulled, items being worked
// and items waiting for a person's decision. A blocked item is in none of them.
type StationLoad struct {
	Station      string
	Waiting      int
	InProgress   int
	WaitingHuman int
}

// Busy is the work in progress at the station: items being worked, and items waiting for a
// person's decision, which hold a human station's WIP the same way.
func (ld StationLoad) Busy() int { return ld.InProgress + ld.WaitingHuman }

// Constraint returns the agent station whose load is highest relative to its capacity,
// (waiting + in progress) / capacity, from a snapshot of the line's loads. Ties go to the
// earlier station; a line with no load at any agent station has no constraint. This is the
// simple phase 2 heuristic; constraint detection over time replaces it.
func (l Line) Constraint(loads map[string]StationLoad) (string, bool) {
	best, ratio := "", 0.0

	for _, st := range l.Stations {
		if st.Kind != KindAgent {
			continue
		}

		ld := loads[st.ID]
		if r := float64(ld.Waiting+ld.InProgress) / float64(st.Capacity()); r > ratio {
			best, ratio = st.ID, r
		}
	}

	return best, best != ""
}

// FlowMetrics describe a factory's flow over a window of time.
type FlowMetrics struct {
	// DonePerDay counts items that reached done in the window, by UTC day, ascending; days
	// without any are left out.
	DonePerDay []DayCount
	// ItemsDone is how many items reached done in the window, Reworked how many of those were
	// sent back at least once.
	ItemsDone int
	Reworked  int
	// LeadTimeP50 and LeadTimeP85 are percentiles (linear interpolation) of created → done of
	// the items done in the window; zero when none were.
	LeadTimeP50 time.Duration
	LeadTimeP85 time.Duration
	// AssembliesDone is how many assemblies (items with parts) reached done in the window, and
	// AssemblyLeadTimeP50 and AssemblyLeadTimeP85 the percentiles (linear interpolation) of
	// their created → last part done; zero when none were. Cancelled assemblies are left out.
	AssembliesDone      int
	AssemblyLeadTimeP50 time.Duration
	AssemblyLeadTimeP85 time.Duration
	// WIP is how many items are in the line now, neither done nor cancelled.
	WIP int
	// SpendUSD is the cost of the agent runs started in the window.
	SpendUSD float64
	// LongWaits is how many items waited over LongWait in front of the station asked about.
	LongWaits int
}

// LongWait is how long an item may wait in front of a station before the Floor calls it out.
const LongWait = 24 * time.Hour

// DayCount is a count for one UTC day (midnight).
type DayCount struct {
	Day   time.Time
	Count int
}
