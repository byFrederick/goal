package domain

import (
	"reflect"
	"testing"
)

func TestEffectiveRules(t *testing.T) {
	t.Parallel()

	everyone := Rule{Name: "everyone"}
	engineers := Rule{Name: "engineers", AppliesTo: RuleScope{Agents: []string{"engineer", "architect"}}}
	building := Rule{Name: "building", AppliesTo: RuleScope{Stations: []string{"build"}}}
	backend := Rule{Name: "backend", AppliesTo: RuleScope{Lanes: []string{"backend"}}}
	incidents := Rule{Name: "incidents", AppliesTo: RuleScope{WorkTypes: []string{"incident"}}}
	narrow := Rule{Name: "narrow", AppliesTo: RuleScope{
		Agents: []string{"engineer"}, Stations: []string{"build"}, Lanes: []string{"backend"}, WorkTypes: []string{"feature"},
	}}
	all := []Rule{everyone, engineers, building, backend, incidents, narrow}

	build := RuleTarget{Agent: "engineer", Station: "build", Lane: "backend", WorkType: "feature"}

	tests := []struct {
		name string
		run  RuleTarget
		want []string
	}{
		{"every list matches", build, []string{"everyone", "engineers", "building", "backend", "narrow"}},
		{"agent misses", RuleTarget{Agent: "reviewer", Station: "build", Lane: "backend", WorkType: "feature"},
			[]string{"everyone", "building", "backend"}},
		{"station misses", RuleTarget{Agent: "engineer", Station: "review", Lane: "backend", WorkType: "feature"},
			[]string{"everyone", "engineers", "backend"}},
		{"lane misses", RuleTarget{Agent: "engineer", Station: "build", Lane: "frontend", WorkType: "feature"},
			[]string{"everyone", "engineers", "building"}},
		{"work type matches and misses", RuleTarget{Agent: "architect", Station: "spec", WorkType: "incident"},
			[]string{"everyone", "engineers", "incidents"}},
		{"a run without a lane or work type", RuleTarget{Agent: "engineer", Station: "build"},
			[]string{"everyone", "engineers", "building"}},
		{"only the rule without appliesTo", RuleTarget{Agent: "reviewer", Station: "review"}, []string{"everyone"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			for _, r := range EffectiveRules(all, tt.run) {
				got = append(got, r.Name)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EffectiveRules(%+v) = %v; want %v", tt.run, got, tt.want)
			}
		})
	}

	if got := EffectiveRules(nil, build); len(got) != 0 {
		t.Errorf("EffectiveRules of no rules = %v", got)
	}
}
