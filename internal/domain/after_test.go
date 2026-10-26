package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// afterField is the output property the tests map to flow.dependencies.after.
const afterField = "after"

func TestAfterFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		field  string
		output string
		want   []ItemRef
		bad    string // a substring of the error, "" for none
	}{
		{name: "no field mapped", field: "", output: `{"after":["acme#1"]}`},
		{name: "absent", field: afterField, output: `{"lane":"backend"}`},
		{name: "null", field: afterField, output: `{"after":null}`},
		{name: "empty", field: afterField, output: `{"after":[]}`},
		{
			name: "refs", field: "follows", output: `{"follows":["acme-web#12","acme-api#3"]}`,
			want: []ItemRef{{Factory: "acme-web", Number: 12}, {Factory: "acme-api", Number: 3}},
		},
		{name: "repeated ref once", field: afterField, output: `{"after":["acme-web#1","acme-web#1"]}`, want: []ItemRef{{Factory: "acme-web", Number: 1}}},
		{name: "not an array", field: afterField, output: `{"after":"acme#1"}`, bad: "array"},
		{name: "not a string", field: afterField, output: `{"after":[12]}`, bad: "array"},
		{name: "bad ref", field: afterField, output: `{"after":["acme#1","acme-12"]}`, bad: `"acme-12"`},
		{name: "not an object", field: afterField, output: `[]`, bad: "after"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := AfterFrom([]byte(tt.output), tt.field)

			if tt.bad != "" {
				if !errors.Is(err, ErrBadAfter) || !strings.Contains(err.Error(), tt.bad) {
					t.Fatalf("AfterFrom(%s) error %v, want ErrBadAfter naming %s", tt.output, err, tt.bad)
				}

				return
			}

			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("AfterFrom(%s) = %v, %v, want %v", tt.output, got, err, tt.want)
			}
		})
	}
}
