package domain

import (
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
