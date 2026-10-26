package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestPartWaves(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		needs map[string][]string
		want  map[string]int
	}{
		{
			name:  "chain",
			needs: map[string][]string{"a": nil, "b": {"a"}, "c": {"b"}},
			want:  map[string]int{"a": 0, "b": 1, "c": 2},
		},
		{
			name:  "diamond",
			needs: map[string][]string{"a": nil, "b": {"a"}, "c": {"a"}, "d": {"b", "c"}},
			want:  map[string]int{"a": 0, "b": 1, "c": 1, "d": 2},
		},
		{
			name:  "uneven needs take the largest wave",
			needs: map[string][]string{"a": nil, "b": {"a"}, "c": {"a", "b"}},
			want:  map[string]int{"a": 0, "b": 1, "c": 2},
		},
		{
			name:  "independent parts",
			needs: map[string][]string{"a": nil, "b": {}, "c": nil},
			want:  map[string]int{"a": 0, "b": 0, "c": 0},
		},
		{
			name:  "needs outside the parts are ignored",
			needs: map[string][]string{"a": {"elsewhere"}, "b": {"a", "elsewhere"}},
			want:  map[string]int{"a": 0, "b": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := PartWaves(tt.needs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PartWaves(%v) = %v; want %v", tt.needs, got, tt.want)
			}
		})
	}
}

func TestPlanFrom_AbsentNullOrEmptyIsNoPlan(t *testing.T) {
	t.Parallel()

	for _, output := range []string{`{}`, `{"lane":"backend"}`, `{"plan":null}`, `{"plan":[]}`} {
		parts, err := PlanFrom(json.RawMessage(output))
		if err != nil || parts != nil {
			t.Errorf("PlanFrom(%s) = %v, %v; want no plan", output, parts, err)
		}
	}
}

func TestPlanFrom_ValidPlanBecomesParts(t *testing.T) {
	t.Parallel()

	output := `{"lane":"backend","plan":[
		{"key":"store","title":"Store","lane":"data","brief":"Add the table.","paths":["migrations/"]},
		{"key":"api","title":"API","lane":"data","brief":"Expose it.","paths":["contracts/api/"],"needs":["store"]}
	]}`

	parts, err := PlanFrom(json.RawMessage(output))
	if err != nil {
		t.Fatal(err)
	}

	want := []NewPart{
		{Key: "store", Title: "Store", Lane: "data", Brief: "Add the table.", Paths: []string{"migrations/"}},
		{Key: "api", Title: "API", Lane: "data", Brief: "Expose it.", Paths: []string{"contracts/api/"}, Needs: []string{"store"}},
	}
	if !reflect.DeepEqual(parts, want) {
		t.Errorf("parts = %+v, want %+v", parts, want)
	}
}

func TestPlanFrom_RefusesBadPlans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		also   error
	}{
		{name: "not JSON", output: `{"plan":`},
		{name: "wrong shape", output: `{"plan":{"key":"a"}}`},
		{name: "wrong field type", output: `{"plan":[{"key":"a","title":"A","needs":"b"}]}`},
		{name: "duplicate key", output: `{"plan":[{"key":"a","title":"A"},{"key":"a","title":"B"}]}`, also: ErrInvalid},
		{name: "unknown need", output: `{"plan":[{"key":"a","title":"A","needs":["z"]}]}`, also: ErrInvalid},
		{
			name: "cycle", also: ErrCycle,
			output: `{"plan":[{"key":"a","title":"A","needs":["b"]},{"key":"b","title":"B","needs":["a"]}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parts, err := PlanFrom(json.RawMessage(tt.output))
			if !errors.Is(err, ErrBadPlan) || parts != nil {
				t.Fatalf("PlanFrom = %v, %v; want ErrBadPlan", parts, err)
			}

			if tt.also != nil && !errors.Is(err, tt.also) {
				t.Errorf("err = %v, want it to wrap %v too", err, tt.also)
			}
		})
	}
}

func TestValidatePart(t *testing.T) {
	t.Parallel()

	const api, store, web, title = "api", "store", "web", "Web"

	keys := map[string]bool{api: true, store: true}

	tests := []struct {
		name string
		part NewPart
		want error
	}{
		{name: "valid", part: NewPart{Key: web, Title: title, Needs: []string{api, store}}},
		{name: "no needs", part: NewPart{Key: "docs", Title: "Docs"}},
		{name: "missing key", part: NewPart{Key: " ", Title: title}, want: ErrInvalid},
		{name: "missing title", part: NewPart{Key: web, Title: ""}, want: ErrInvalid},
		{name: "key already used", part: NewPart{Key: api, Title: "API again"}, want: ErrInvalid},
		{name: "unknown need", part: NewPart{Key: web, Title: title, Needs: []string{"db"}}, want: ErrInvalid},
		{name: "needs itself", part: NewPart{Key: web, Title: title, Needs: []string{web}}, want: ErrInvalid},
		{name: "repeated need", part: NewPart{Key: web, Title: title, Needs: []string{api, api}}, want: ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidatePart(tt.part, keys)
			if tt.want == nil && err != nil || tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("ValidatePart(%+v) = %v; want %v", tt.part, err, tt.want)
			}
		})
	}
}
