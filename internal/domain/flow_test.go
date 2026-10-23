package domain

import (
	"encoding/json"
	"testing"
)

// Stations of the test lines.
const (
	stCode = "code"
	stPlan = "plan"
)

func TestParseItemRef_FactoryHashNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want ItemRef
		ok   bool
	}{
		{"acme#12", ItemRef{Factory: "acme", Number: 12}, true},
		{"8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b#1", ItemRef{Factory: "8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b", Number: 1}, true},
		{"8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b", ItemRef{}, false},
		{"acme#0", ItemRef{}, false},
		{"acme#-3", ItemRef{}, false},
		{"acme#012", ItemRef{}, false},
		{"acme#", ItemRef{}, false},
		{"#4", ItemRef{}, false},
		{"acme#4#5", ItemRef{}, false},
		{"12", ItemRef{}, false},
	}

	for _, tc := range tests {
		got, ok := ParseItemRef(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseItemRef(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}

		if ok && got.String() != tc.in {
			t.Errorf("ParseItemRef(%q).String() = %q", tc.in, got.String())
		}
	}
}

func TestItemPullRequest_LatestStationOutputThatNamesOne(t *testing.T) {
	t.Parallel()

	const pr7, pr9 = `{"pull_request":"https://github.com/acme/app/pull/7"}`, `{"pull_request":"https://github.com/acme/app/pull/9"}`

	l := Line{Stations: []Station{{ID: stPlan}, {ID: stCode}, {ID: "fix"}, {ID: "merge"}, {ID: "done"}}}
	outputs := func(m map[string]string) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for station, raw := range m {
			out[station] = json.RawMessage(raw)
		}

		return out
	}

	tests := []struct {
		name    string
		outputs map[string]json.RawMessage
		want    string
	}{
		{"none", outputs(map[string]string{stPlan: `{"summary":"x"}`}), ""},
		{"one", outputs(map[string]string{stCode: pr7}), "https://github.com/acme/app/pull/7"},
		{"the later station wins", outputs(map[string]string{stCode: pr7, "fix": pr9}), "https://github.com/acme/app/pull/9"},
		{"not a URL", outputs(map[string]string{stCode: `{"pull_request":"#7"}`}), ""},
		{"not an object", outputs(map[string]string{stCode: `["https://github.com/acme/app/pull/7"]`}), ""},
		{"a station not on the line", outputs(map[string]string{"gone": pr7}), ""},
	}

	for _, tc := range tests {
		if got := (Item{Outputs: tc.outputs}).PullRequest(l); got != tc.want {
			t.Errorf("%s: PullRequest = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLineConstraint_HighestLoadPerSlot(t *testing.T) {
	t.Parallel()

	l := Line{Stations: []Station{
		{ID: "inbox", Kind: KindQueue},
		{ID: stPlan, Kind: KindAgent, Agents: 1},
		{ID: "approve", Kind: KindHuman},
		{ID: stCode, Kind: KindAgent, Agents: 3, WIP: 3},
		{ID: "done", Kind: KindDone},
	}}

	tests := []struct {
		name  string
		loads map[string]StationLoad
		want  string
	}{
		{"idle line", map[string]StationLoad{"inbox": {Waiting: 9}, "approve": {Waiting: 4}}, ""},
		{"most work per slot", map[string]StationLoad{stPlan: {Waiting: 1, InProgress: 1}, stCode: {Waiting: 4, InProgress: 3}}, stCode},
		{"queue and human stations never constrain", map[string]StationLoad{stPlan: {InProgress: 1}, "inbox": {Waiting: 50}}, stPlan},
		{"ties go upstream", map[string]StationLoad{stPlan: {InProgress: 1}, stCode: {InProgress: 3}}, stPlan},
	}

	for _, tc := range tests {
		got, ok := l.Constraint(tc.loads)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: Constraint = %q, %v; want %q", tc.name, got, ok, tc.want)
		}
	}
}
