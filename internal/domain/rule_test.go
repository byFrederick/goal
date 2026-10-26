package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Names of the line TestEffectiveRules matches rules on.
const (
	coder   = "coder"
	planner = "planner"
	checker = "checker"
	coding  = "coding"
	api     = "api"
	story   = "story"
	outage  = "outage"
)

func TestEffectiveRules(t *testing.T) {
	t.Parallel()

	rules := []Rule{
		{Name: "everyone"},
		{Name: "agents", AppliesTo: RuleScope{Agents: []string{coder, planner}}},
		{Name: "station", AppliesTo: RuleScope{Stations: []string{coding}}},
		{Name: "lane", AppliesTo: RuleScope{Lanes: []string{api}}},
		{Name: "work-type", AppliesTo: RuleScope{WorkTypes: []string{outage}}},
		{Name: "every-list", AppliesTo: RuleScope{
			Agents: []string{coder}, Stations: []string{coding}, Lanes: []string{api}, WorkTypes: []string{story},
		}},
	}

	// want are the indexes in rules of the effective rules.
	tests := []struct {
		name string
		run  RuleTarget
		want []int
	}{
		{"every list matches", RuleTarget{Agent: coder, Station: coding, Lane: api, WorkType: story}, []int{0, 1, 2, 3, 5}},
		{"agent misses", RuleTarget{Agent: checker, Station: coding, Lane: api, WorkType: story}, []int{0, 2, 3}},
		{"station misses", RuleTarget{Agent: coder, Station: "checking", Lane: api, WorkType: story}, []int{0, 1, 3}},
		{"lane misses", RuleTarget{Agent: coder, Station: coding, Lane: "ui", WorkType: story}, []int{0, 1, 2}},
		{"work type matches and misses", RuleTarget{Agent: planner, Station: "planning", WorkType: outage}, []int{0, 1, 4}},
		{"a run without a lane or work type", RuleTarget{Agent: coder, Station: coding}, []int{0, 1, 2}},
		{"only the rule without appliesTo", RuleTarget{Agent: checker, Station: "checking"}, []int{0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var want []Rule
			for _, i := range tt.want {
				want = append(want, rules[i])
			}

			if got := EffectiveRules(rules, tt.run); !reflect.DeepEqual(got, want) {
				t.Errorf("EffectiveRules(%+v) = %+v; want %+v", tt.run, got, want)
			}
		})
	}

	if got := EffectiveRules(nil, RuleTarget{Agent: coder}); len(got) != 0 {
		t.Errorf("EffectiveRules of no rules = %v", got)
	}
}

func TestViolationsFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   []Violation
	}{
		{name: "none", output: `{"decision":"approve"}`},
		{name: "empty", output: `{"violations":[]}`},
		{name: "not a list", output: `{"violations":{"rule":"tests-first"}}`},
		{name: "not an object", output: `[1,2]`},
		{name: "not json", output: `nope`},
		{
			name:   "in order, skipping what is not an entry",
			output: `{"violations":[{"rule":"tests-first","reason":"no test"},"wrap-errors",{"rule":"wrap-errors","reason":"bare"}]}`,
			want:   []Violation{{Rule: "tests-first", Reason: "no test"}, {Rule: "wrap-errors", Reason: "bare"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ViolationsFrom(json.RawMessage(tt.output)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ViolationsFrom(%s) = %+v, want %+v", tt.output, got, tt.want)
			}
		})
	}
}
