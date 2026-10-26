package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
)

func TestAfterFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		field  string
		output string
		want   []domain.ItemRef
		bad    string // a substring of the error, "" for none
	}{
		{name: "no field mapped", field: "", output: `{"after":["acme#1"]}`},
		{name: "absent", field: "after", output: `{"lane":"backend"}`},
		{name: "null", field: "after", output: `{"after":null}`},
		{name: "empty", field: "after", output: `{"after":[]}`},
		{
			name: "refs", field: "follows", output: `{"follows":["acme#12","acme-api#3"]}`,
			want: []domain.ItemRef{{Factory: "acme", Number: 12}, {Factory: "acme-api", Number: 3}},
		},
		{name: "repeated ref once", field: "after", output: `{"after":["acme#1","acme#1"]}`, want: []domain.ItemRef{{Factory: "acme", Number: 1}}},
		{name: "not an array", field: "after", output: `{"after":"acme#1"}`, bad: "array"},
		{name: "not a string", field: "after", output: `{"after":[12]}`, bad: "array"},
		{name: "bad ref", field: "after", output: `{"after":["acme#1","acme-12"]}`, bad: `"acme-12"`},
		{name: "not an object", field: "after", output: `[]`, bad: "after"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.AfterFrom([]byte(tt.output), tt.field)

			if tt.bad != "" {
				if !errors.Is(err, domain.ErrBadAfter) || !strings.Contains(err.Error(), tt.bad) {
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
