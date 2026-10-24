package domain

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AttentionItem is an open item that may need a person (the organization's inbox): waiting for a
// decision, blocked, or in the expedite lane.
type AttentionItem struct {
	Item Item
	// BlockedReason is the reason of a blocked item's latest blocked or paused event ("" when it
	// gave none, or the item is not blocked).
	BlockedReason string
	// Paused reports whether a person paused a blocked item (its latest such event is paused).
	Paused bool
}

// Limits of an approval brief.
const (
	maxFacts      = 4
	maxFactLength = 120
)

// briefSkipped are the output fields a brief never shows as facts: the summary and pull request
// it shows on their own, and the fields of verdicts and routing.
var briefSkipped = []string{summaryKey, pullRequestKey, "lane", "decision", "reason", "category"}

// Fact is one short field of an output, shown with an approval.
type Fact struct {
	Key   string
	Value string
}

// ApprovalBrief is what a person reads before deciding on an item: the latest output's summary,
// up to four of its other short fields, the pull request the outputs name and what the item cost.
type ApprovalBrief struct {
	Summary     string
	Facts       []Fact
	PullRequest string
	SpendUSD    float64
}

// Brief builds the item's approval brief from the output of the latest station on line l that has
// one. Facts are the output's other top-level scalars (strings of at most 120 characters,
// numbers, booleans), by key, at most four; nothing in it names a field of a particular factory.
func (it Item) Brief(l Line) ApprovalBrief {
	b := ApprovalBrief{Summary: it.Summary(l), Facts: []Fact{}, PullRequest: it.PullRequest(l), SpendUSD: it.CostUSD}

	out := it.latestObject(l)
	for _, key := range slices.Sorted(maps.Keys(out)) {
		if len(b.Facts) == maxFacts {
			break
		}

		if slices.Contains(briefSkipped, key) {
			continue
		}

		if v, ok := factValue(out[key]); ok {
			b.Facts = append(b.Facts, Fact{Key: key, Value: v})
		}
	}

	return b
}

// factValue writes a scalar as a fact's value; false for anything else, and for a blank or long
// string.
func factValue(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		s := strings.TrimSpace(v)
		return s, s != "" && utf8.RuneCountInString(s) <= maxFactLength
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		if v {
			return "yes", true
		}

		return "no", true
	default:
		return "", false
	}
}

// latestObject is the output of the latest station on the line whose output is a JSON object, or
// nil.
func (it Item) latestObject(l Line) map[string]any {
	for _, st := range slices.Backward(l.Stations) {
		raw, ok := it.Outputs[st.ID]
		if !ok {
			continue
		}

		var out map[string]any
		if json.Unmarshal(raw, &out) == nil && out != nil {
			return out
		}
	}

	return nil
}
