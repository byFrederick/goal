package domain

import (
	"encoding/json"
	"slices"
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

func TestItemPullRequests_EveryStationsPullRequestsOnce(t *testing.T) {
	t.Parallel()

	const (
		app7  = "https://github.com/acme/app/pull/7"
		web3  = "https://gitlab.com/acme/web/-/merge_requests/3"
		docs2 = "https://github.com/acme/docs/pull/2"
	)

	l := Line{Stations: []Station{{ID: stPlan}, {ID: stCode}}}
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }

	tests := []struct {
		name    string
		outputs map[string]json.RawMessage
		want    []string
	}{
		{"none", map[string]json.RawMessage{stPlan: raw(`{"summary":"x"}`)}, []string{}},
		{"one string", map[string]json.RawMessage{stCode: raw(`{"pull_request":"` + app7 + `"}`)}, []string{app7}},
		{"the string, then the list", map[string]json.RawMessage{
			stCode: raw(`{"pull_requests":["` + web3 + `","` + docs2 + `"],"pull_request":"` + app7 + `"}`),
		}, []string{app7, web3, docs2}},
		{"in station order, each once", map[string]json.RawMessage{
			stCode: raw(`{"pull_requests":["` + app7 + `","` + docs2 + `"]}`),
			stPlan: raw(`{"pull_request":"` + app7 + `","pull_requests":["` + web3 + `"]}`),
		}, []string{app7, web3, docs2}},
		{"only https URLs", map[string]json.RawMessage{
			stCode: raw(`{"pull_request":"#7","pull_requests":["http://github.com/acme/app/pull/7",7,null,"` + web3 + `"]}`),
		}, []string{web3}},
		{"a list that is not one", map[string]json.RawMessage{stCode: raw(`{"pull_requests":"` + app7 + `"}`)}, []string{}},
		{"a station not on the line", map[string]json.RawMessage{"gone": raw(`{"pull_request":"` + app7 + `"}`)}, []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := (Item{Outputs: tc.outputs}).PullRequests(l); !slices.Equal(got, tc.want) {
				t.Errorf("PullRequests = %q, want %q", got, tc.want)
			}
		})
	}
}

// A card's summary is the summary of the latest station output on the line that has one.
func TestItemSummary_LatestStationOutputThatHasOne(t *testing.T) {
	t.Parallel()

	l := Line{Stations: []Station{{ID: stPlan}, {ID: stCode}, {ID: "done"}}}
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }

	tests := []struct {
		name    string
		outputs map[string]json.RawMessage
		want    string
	}{
		{"none", nil, ""},
		{"one", map[string]json.RawMessage{stPlan: raw(`{"summary":"Postmortem filed"}`)}, "Postmortem filed"},
		{"the later station wins", map[string]json.RawMessage{
			stPlan: raw(`{"summary":"Planned"}`), stCode: raw(`{"summary":" Closed "}`),
		}, "Closed"},
		{"a later output without one", map[string]json.RawMessage{
			stPlan: raw(`{"summary":"Planned"}`), stCode: raw(`{"pull_request":"https://github.com/acme/app/pull/7"}`),
		}, "Planned"},
		{"not a string", map[string]json.RawMessage{stPlan: raw(`{"summary":{"text":"x"}}`)}, ""},
		{"blank", map[string]json.RawMessage{stPlan: raw(`{"summary":"  "}`)}, ""},
		{"a station not on the line", map[string]json.RawMessage{"gone": raw(`{"summary":"x"}`)}, ""},
	}

	for _, tc := range tests {
		if got := (Item{Outputs: tc.outputs}).Summary(l); got != tc.want {
			t.Errorf("%s: Summary = %q, want %q", tc.name, got, tc.want)
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
